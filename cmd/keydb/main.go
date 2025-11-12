package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rudderlabs/keydb/client"
	kitconfig "github.com/rudderlabs/rudder-go-kit/config"
	"github.com/rudderlabs/rudder-go-kit/logger"
	kitsync "github.com/rudderlabs/rudder-go-kit/sync"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"
)

const (
	metricsPrefix = "rudder_load_keydb_"
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

	// Read configuration parameters
	addresses := conf.GetStringSlice("ADDRESSES", []string{"localhost:50051"})
	batchSize := conf.GetInt("BATCH_SIZE", 100)
	workers := conf.GetInt("WORKERS", 10)
	duplicatePercentage := conf.GetInt("DUPLICATE_PERCENTAGE", 50)
	ttl := conf.GetDuration("TTL", 24, time.Hour)
	totalHashRanges := conf.GetInt("TOTAL_HASH_RANGES", 271)
	retryInitialInterval := conf.GetDuration("RETRY_INITIAL_INTERVAL", 100, time.Millisecond)
	retryMultiplier := conf.GetFloat64("RETRY_MULTIPLIER", 2.0)
	retryMaxInterval := conf.GetDuration("RETRY_MAX_INTERVAL", 5, time.Second)

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
	keydbClient, err := client.NewClient(clientConfig, log)
	if err != nil {
		log.Errorn("creating KeyDB client", obskit.Error(err))
		return 1
	}
	defer func() {
		if err := keydbClient.Close(); err != nil {
			log.Errorn("closing KeyDB client", obskit.Error(err))
		}
	}()

	// PROMETHEUS REGISTRY - START
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	constLabels := map[string]string{
		"workers":    strconv.Itoa(workers),
		"batch_size": strconv.Itoa(batchSize),
	}

	getOperationsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "get_operations_count",
		Help:        "Total number of Get operations",
		ConstLabels: constLabels,
	})
	putOperationsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "put_operations_count",
		Help:        "Total number of Put operations",
		ConstLabels: constLabels,
	})
	getErrorsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "get_errors_count",
		Help:        "Total number of Get errors",
		ConstLabels: constLabels,
	})
	putErrorsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "put_errors_count",
		Help:        "Total number of Put errors",
		ConstLabels: constLabels,
	})
	keysFoundCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "keys_found_count",
		Help:        "Total number of keys found during Get operations",
		ConstLabels: constLabels,
	})
	keysNotFoundCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "keys_not_found_count",
		Help:        "Total number of keys not found during Get operations",
		ConstLabels: constLabels,
	})
	operationsPerSecond := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        metricsPrefix + "operations_per_second",
		Help:        "Operations per second",
		ConstLabels: constLabels,
	})

	reg.MustRegister(getOperationsCounter)
	reg.MustRegister(putOperationsCounter)
	reg.MustRegister(getErrorsCounter)
	reg.MustRegister(putErrorsCounter)
	reg.MustRegister(keysFoundCounter)
	reg.MustRegister(keysNotFoundCounter)
	reg.MustRegister(operationsPerSecond)
	// PROMETHEUS REGISTRY - END

	// HTTP METRICS SERVER - START
	var httpServersWG sync.WaitGroup
	httpServersWG.Add(1)
	go func() {
		defer httpServersWG.Done()

		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
			Registry:          reg,
			EnableOpenMetrics: true,
		}))
		srv := http.Server{
			Addr:    ":9102",
			Handler: mux,
		}

		httpServersWG.Add(1)
		go func() {
			defer httpServersWG.Done()
			<-ctx.Done()
			log.Infon("shutting down HTTP metrics server")
			if err := srv.Shutdown(context.Background()); err != nil {
				log.Errorn("HTTP server shutdown", obskit.Error(err))
			}
		}()

		log.Infon("starting HTTP metrics server", logger.NewStringField("addr", ":9102"))
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorn("HTTP server", obskit.Error(err))
		}
	}()
	// HTTP METRICS SERVER - END

	// Statistics tracking
	var (
		totalOperations atomic.Int64
		startTime       = time.Now()
	)

	// Start workers
	log.Infon("starting workers", logger.NewIntField("count", int64(workers)))
	group, gCtx := kitsync.NewEagerGroup(ctx, workers)

	for workerID := 0; workerID < workers; workerID++ {
		workerID := workerID
		group.Go(func() error {
			log.Infon("worker started", logger.NewIntField("workerID", int64(workerID)))
			defer log.Infon("worker stopped", logger.NewIntField("workerID", int64(workerID)))

			for {
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				default:
				}

				// Generate batch of keys
				keys := make([]string, batchSize)
				for j := 0; j < batchSize; j++ {
					// Determine if this key should be a duplicate based on percentage
					if duplicatePercentage > 0 && rand.Intn(100) < duplicatePercentage {
						// Use a key from a limited pool to create duplicates
						poolSize := 1000
						keys[j] = fmt.Sprintf("duplicate-key-%d", rand.Intn(poolSize))
					} else {
						// Generate unique key
						keys[j] = fmt.Sprintf("unique-key-%s", uuid.New().String())
					}
				}

				// Perform Get operation
				exists, err := keydbClient.Get(gCtx, keys)
				if err != nil {
					log.Errorn("Get operation",
						logger.NewIntField("workerID", int64(workerID)),
						obskit.Error(err),
					)
					getErrorsCounter.Inc()
					continue
				}
				getOperationsCounter.Inc()

				// Count found and not found keys
				var keysToPut []string
				for idx, exist := range exists {
					if exist {
						keysFoundCounter.Inc()
					} else {
						keysNotFoundCounter.Inc()
						keysToPut = append(keysToPut, keys[idx])
					}
				}

				// Put keys that don't exist
				if len(keysToPut) > 0 {
					err = keydbClient.Put(gCtx, keysToPut, ttl)
					if err != nil {
						log.Errorn("Put operation",
							logger.NewIntField("workerID", int64(workerID)),
							logger.NewIntField("keyCount", int64(len(keysToPut))),
							obskit.Error(err),
						)
						putErrorsCounter.Inc()
						continue
					}
					putOperationsCounter.Inc()
				}

				// Update statistics
				ops := totalOperations.Add(1)
				if ops%100 == 0 {
					elapsed := time.Since(startTime).Seconds()
					if elapsed > 0 {
						operationsPerSecond.Set(float64(ops) / elapsed)
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

	log.Infon("waiting for HTTP metrics server to shutdown")
	httpServersWG.Wait()

	return 0
}
