package main

import (
	"context"
	"errors"
	"fmt"
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
	duplicatePercentage := conf.GetInt("duplicatePercentage", 50)
	ttl := conf.GetDuration("ttl", 24, time.Hour)

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

	// batchSize : 100 = keyPoolSize : duplicatePercentage
	keyPoolSize := batchSize * duplicatePercentage / 100

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
		Addresses:          addresses,
		TotalHashRanges:    conf.GetInt64("KeyDB.Dedup.TotalHashRanges", client.DefaultTotalHashRanges),
		ConnectionPoolSize: conf.GetInt("KeyDB.Dedup.ConnectionPoolSize", client.DefaultConnectionPoolSize),
		RetryPolicy: client.RetryPolicy{
			Disabled:        conf.GetBool("KeyDB.Dedup.RetryPolicy.Disabled", false),
			InitialInterval: conf.GetDuration("KeyDB.Dedup.RetryPolicy.InitialInterval", 100, time.Millisecond),
			Multiplier:      conf.GetFloat64("KeyDB.Dedup.RetryPolicy.Multiplier", 1.5),
			MaxInterval:     conf.GetDuration("KeyDB.Dedup.RetryPolicy.MaxInterval", 30, time.Second),
			// No MaxElapsedTime, the client will retry forever.
			// To detect issues monitor the client metrics:
			// https://github.com/rudderlabs/keydb/blob/v0.4.2-alpha/client/client.go#L160
		},
		GrpcConfig: client.GrpcConfig{
			// After a duration of this time if the client doesn't see any activity it
			// pings the server to see if the transport is still alive.
			KeepAliveTime: conf.GetDuration("KeyDB.Dedup.GrpcConfig.KeepAliveTime", 10, time.Second),
			// After having pinged for keepalive check, the client waits for a duration
			// of Timeout and if no activity is seen even after that the connection is
			// closed.
			KeepAliveTimeout: conf.GetDuration("KeyDB.Dedup.GrpcConfig.KeepAliveTimeout", 2, time.Second),
			// If false, client sends keepalive pings even with no active RPCs. If true,
			// when there are no active RPCs, KeepAliveTime and KeepAliveTimeout will be ignored and no
			// keepalive pings will be sent.
			DisableKeepAlivePermitWithoutStream: conf.GetBool("KeyDB.Dedup.GrpcConfig.DisableKeepAlivePermitWithoutStream", false),
			// BackoffBaseDelay is the amount of time to backoff after the first failure.
			BackoffBaseDelay: conf.GetDuration("KeyDB.Dedup.GrpcConfig.BackoffBaseDelay", 1, time.Second),
			// BackoffMultiplier is the factor with which to multiply backoffs after a
			// failed retry. Should ideally be greater than 1.
			BackoffMultiplier: conf.GetFloat64("KeyDB.Dedup.GrpcConfig.BackoffMultiplier", 1.6),
			// BackoffJitter is the factor with which backoffs are randomized.
			BackoffJitter: conf.GetFloat64("KeyDB.Dedup.GrpcConfig.BackoffJitter", 0.2),
			// BackoffMaxDelay is the upper bound of backoff delay.
			BackoffMaxDelay: conf.GetDuration("KeyDB.Dedup.GrpcConfig.BackoffMaxDelay", 2, time.Minute),
			// MinConnectTimeout is the minimum amount of time we are willing to give a
			// connection to complete.
			MinConnectTimeout: conf.GetDuration("KeyDB.Dedup.GrpcConfig.MinConnectTimeout", 20, time.Second),
		},
	}

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
		totalOperations      atomic.Uint64
		startTime            = time.Now()
		getErrorsCounter     = stat.NewStat("rudder_load_keydb_get_errors_count", stats.CountType)
		getOperationsCounter = stat.NewStat("rudder_load_keydb_get_operations_count", stats.CountType)
		keysFoundCounter     = stat.NewStat("rudder_load_keydb_keys_found_count", stats.CountType)
		keysNotFoundCounter  = stat.NewStat("rudder_load_keydb_keys_not_found_count", stats.CountType)
		putErrorsCounter     = stat.NewStat("rudder_load_keydb_put_errors_count", stats.CountType)
		putOperationsCounter = stat.NewStat("rudder_load_keydb_put_operations_count", stats.CountType)
		operationsPerSecond  = stat.NewStat("rudder_load_keydb_operations_per_second", stats.GaugeType)
		batchCreationLatency = stat.NewStat("rudder_load_keydb_batch_creation_latency", stats.TimerType)
	)

	// Create key pool for duplicates
	keyPool := make([]string, keyPoolSize)
	for i := 0; i < keyPoolSize; i++ {
		keyPool[i] = uuid.New().String()
	}
	log.Infon("created key pool", logger.NewIntField("size", int64(keyPoolSize)))

	// Clients pool
	clientPool := make([]*client.Client, workers)
	for i := 0; i < workers; i++ {
		keydbClient, err := client.NewClient(clientConfig, log, client.WithStats(stat))
		if err != nil {
			log.Errorn("creating KeyDB client", obskit.Error(err))
			return 1
		}
		clientPool[i] = keydbClient
	}
	defer func() {
		for _, c := range clientPool {
			if err := c.Close(); err != nil {
				log.Errorn("closing KeyDB client", obskit.Error(err))
			}
		}
	}()

	// Start workers
	log.Infon("starting workers", logger.NewIntField("count", int64(workers)))
	group, gCtx := kitsync.NewEagerGroup(ctx, workers)

	for workerID := 0; workerID < workers; workerID++ {
		// Create per-worker random source to avoid global rand lock contention
		workerID := int64(workerID)
		group.Go(func() error {
			log := log.Withn(logger.NewIntField("workerID", workerID))
			log.Infon("worker started")
			defer log.Infon("worker stopped")

			for {
				select {
				case <-gCtx.Done():
					log.Warn("worker stopped")
					return gCtx.Err()
				default:
				}

				// Generate batch of keys with pre-calculated distribution
				startBatch := time.Now()
				keys := make([]string, batchSize)
				for i := 0; i < keyPoolSize; i++ {
					keys[i] = keyPool[i]
				}
				for i := keyPoolSize; i < batchSize; i++ {
					keys[i] = uuid.New().String()
				}
				batchCreationLatency.Since(startBatch)

				// Perform Get operation
				keydbClient := clientPool[workerID]
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
					operationsPerSecond.Gauge(float64(ops) / elapsed)
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
		logger.NewIntField("totalOperations", int64(totalOps)),
		logger.NewStringField("duration", elapsed.Round(time.Millisecond).String()),
		logger.NewStringField("operationsPerSecond", fmt.Sprintf("%.2f", float64(totalOps)/elapsed.Seconds())),
	)

	return 0
}
