package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rudderlabs/keydb/client"
	kitconfig "github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/logger"
	_ "github.com/rudderlabs/rudder-go-kit/maxprocs"
	"github.com/rudderlabs/rudder-go-kit/profiler"
	"github.com/rudderlabs/rudder-go-kit/stats"
	svcMetric "github.com/rudderlabs/rudder-go-kit/stats/metric"
	kitsync "github.com/rudderlabs/rudder-go-kit/sync"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx))
}

func run(ctx context.Context) int {
	// Initialize config with service name prefix
	conf := kitconfig.New(kitconfig.WithEnvPrefix("KEYDB"))

	// Initialize logger
	logFactory := logger.NewFactory(conf)
	log := logFactory.NewLogger()

	// Initialize stats
	registerer := prometheus.DefaultRegisterer
	gatherer := prometheus.DefaultGatherer
	statsOptions := []stats.Option{
		stats.WithServiceName("keydb-load-test"),
		stats.WithDefaultHistogramBuckets(defaultHistogramBuckets),
		stats.WithPrometheusRegistry(registerer, gatherer),
	}
	for histogramName, buckets := range customBuckets {
		statsOptions = append(statsOptions, stats.WithHistogramBuckets(histogramName, buckets))
	}
	stat := stats.NewStats(conf, logFactory, svcMetric.NewManager(), statsOptions...)
	defer stat.Stop()
	if err := stat.Start(ctx, stats.DefaultGoRoutineFactory); err != nil {
		log.Errorn("Failed to start Stats", obskit.Error(err))
		return 1
	}

	// Read configuration parameters
	addresses := strings.Split(conf.GetString("addresses", ""), ",")
	batchSize := conf.GetInt("batchSize", 1000)
	workers := conf.GetInt("workers", 10)
	keyPoolSize := conf.GetInt("keyPoolSize", 300000)
	duplicatePercentage := conf.GetInt("duplicatePercentage", 50)
	totalHashRanges := conf.GetInt("totalHashRanges", int(client.DefaultTotalHashRanges))
	ttl := conf.GetDuration("ttl", 24, time.Hour)
	retryInitialInterval := conf.GetDuration("retryInitialInterval", 100, time.Millisecond)
	retryMultiplier := conf.GetFloat64("retryMultiplier", 2.0)
	retryMaxInterval := conf.GetDuration("retryMaxInterval", 5, time.Second)

	// Validate configuration
	if len(addresses) == 0 {
		log.Errorn("addresses cannot be empty")
		return 1
	}
	if batchSize < 1 {
		log.Errorn("batch size must be greater than zero", logger.NewIntField("batchSize", int64(batchSize)))
		return 1
	}
	if workers < 1 {
		log.Errorn("workers must be greater than zero", logger.NewIntField("workers", int64(workers)))
		return 1
	}
	if duplicatePercentage < 0 || duplicatePercentage > 100 {
		log.Errorn("duplicate percentage must be between 0 and 100", logger.NewIntField("duplicatePercentage", int64(duplicatePercentage)))
		return 1
	}

	log.Infon("starting KeyDB load test",
		logger.NewStringField("addresses", strings.Join(addresses, ",")),
		logger.NewIntField("batchSize", int64(batchSize)),
		logger.NewIntField("workers", int64(workers)),
		logger.NewIntField("duplicatePercentage", int64(duplicatePercentage)),
		logger.NewIntField("keyPoolSize", int64(keyPoolSize)),
		logger.NewIntField("cpus", int64(runtime.GOMAXPROCS(-1))),
	)

	// Create KeyDB client configuration
	clientConfig := client.Config{
		Addresses:       addresses,
		TotalHashRanges: uint32(totalHashRanges),
		RetryPolicy: client.RetryPolicy{
			Disabled:        false,
			InitialInterval: retryInitialInterval,
			Multiplier:      retryMultiplier,
			MaxInterval:     retryMaxInterval,
		},
	}

	// Initialize KeyDB client
	keydbClient, err := client.NewClient(clientConfig, log, client.WithStats(stat))
	if err != nil {
		log.Errorn("creating KeyDB client", obskit.Error(err))
		return 1
	}
	defer func() {
		if err := keydbClient.Close(); err != nil {
			log.Errorn("closing KeyDB client", obskit.Error(err))
		}
	}()

	// Starting profiler
	profilerDone := make(chan struct{})
	go func() {
		defer close(profilerDone)
		if err := profiler.StartServer(ctx, conf.GetInt("Profiler.Port", 7777)); err != nil {
			log.Errorn("profiler server error", obskit.Error(err))
		}
	}()
	defer func() { <-profilerDone }()

	// Statistics tracking
	var (
		totalOperations      atomic.Int64
		startTime            = time.Now()
		getErrorsCounter     = stat.NewStat("rudder_load_keydb_get_errors_count", stats.CountType)
		getOperationsCounter = stat.NewStat("rudder_load_keydb_get_operations_count", stats.CountType)
		keysFoundCounter     = stat.NewStat("rudder_load_keydb_keys_found_count", stats.CountType)
		keysNotFoundCounter  = stat.NewStat("rudder_load_keydb_keys_not_found_count", stats.CountType)
		putErrorsCounter     = stat.NewStat("rudder_load_keydb_put_errors_count", stats.CountType)
		putOperationsCounter = stat.NewStat("rudder_load_keydb_put_operations_count", stats.CountType)
		operationsPerSecond  = stat.NewStat("rudder_load_keydb_operations_per_second", stats.GaugeType)
	)

	// Create key pool for duplicates
	keyPool := make([]string, keyPoolSize)
	for i := 0; i < keyPoolSize; i++ {
		keyPool[i] = uuid.New().String()
	}
	log.Infon("created key pool", logger.NewIntField("size", int64(keyPoolSize)))

	// Start workers
	log.Infon("starting workers", logger.NewIntField("count", int64(workers)))
	group, gCtx := kitsync.NewEagerGroup(ctx, workers)

	for workerID := 0; workerID < workers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			log := log.Withn(logger.NewIntField("workerID", int64(workerID)))
			log.Infon("worker started")
			defer log.Infon("worker stopped")

			for {
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				default:
				}

				// Generate batch of keys (ensuring no duplicates within the same batch)
				keys := make([]string, 0, batchSize)
				keysInBatch := make(map[string]struct{}, batchSize)
				for len(keys) < batchSize {
					var key string
					// Determine if this key should come from the pool (duplicate) or be unique
					if duplicatePercentage > 0 && rand.Intn(100) < duplicatePercentage {
						key = keyPool[rand.Intn(keyPoolSize)]
					} else { // Generate unique key
						key = uuid.New().String()
					}

					// Only add if not already in this batch
					if _, exists := keysInBatch[key]; !exists {
						keys = append(keys, key)
						keysInBatch[key] = struct{}{}
					}
				}

				// Perform Get operation
				exists, err := keydbClient.Get(gCtx, keys)
				if err != nil {
					log.Errorn("Get operation", obskit.Error(err))
					getErrorsCounter.Increment()
					continue
				}
				getOperationsCounter.Increment()

				// Count found and not found keys
				var keysToPut []string
				for idx, exist := range exists {
					if exist {
						keysFoundCounter.Increment()
					} else {
						keysNotFoundCounter.Increment()
						keysToPut = append(keysToPut, keys[idx])
					}
				}

				// Put keys that don't exist
				if len(keysToPut) > 0 {
					err = keydbClient.Put(gCtx, keysToPut, ttl)
					if err != nil {
						log.Errorn("Put operation",
							logger.NewIntField("keyCount", int64(len(keysToPut))),
							obskit.Error(err),
						)
						putErrorsCounter.Increment()
						continue
					}
					putOperationsCounter.Increment()
				}

				// Update statistics
				ops := totalOperations.Add(1)
				if ops%100 == 0 {
					elapsed := time.Since(startTime).Seconds()
					if elapsed > 0 {
						operationsPerSecond.Gauge(float64(ops) / elapsed)
					}
				}
			}
		})
	}

	// Wait for workers to complete
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		log.Errorn("workers", obskit.Error(err))
	}

	// Print final statistics
	elapsed := time.Since(startTime)
	totalOps := totalOperations.Load()
	log.Infon("load test completed",
		logger.NewIntField("totalOperations", totalOps),
		logger.NewStringField("duration", elapsed.Round(time.Millisecond).String()),
		logger.NewStringField("operationsPerSecond", fmt.Sprintf("%.2f", float64(totalOps)/elapsed.Seconds())),
	)

	return 0
}
