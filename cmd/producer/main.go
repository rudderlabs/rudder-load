package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"runtime/debug"
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

	"rudder-load/internal/stats"
	"rudder-load/internal/validator"

	"github.com/rudderlabs/rudder-go-kit/profiler"
	kitsync "github.com/rudderlabs/rudder-go-kit/sync"
	"github.com/rudderlabs/rudder-go-kit/throttling"
)

var (
	modeStdout producerMode = producerModeImpl{"stdout"}
	modeHTTP   producerMode = producerModeImpl{"http"}
	modeHTTP2  producerMode = producerModeImpl{"http2"}
	modePulsar producerMode = producerModeImpl{"pulsar"}

	// hostnameRE matches hostnames in the following formats:
	// - rudder-load-0-68d8995d6c-9td2n
	// - rudder-load-http-0-68d8995d6c-9td2n
	// - rudder-load-0-test
	// - rudder-load-http-0-test
	// - rudder-load-7-special_chars.123!@#
	hostnameRE = regexp.MustCompile(`rudder-load-(?:[a-z0-9]+-)?(\d+)-([^/]+)`)
)

const (
	hostnameSep        = "rudder-load-"
	templatesExtension = ".json.tmpl"
	metricsPrefix      = "rudder_load_"
)

type producerMode interface {
	protected()
	String() string
}

type producerModeImpl struct{ value string }

func (p producerModeImpl) protected()     {}
func (p producerModeImpl) String() string { return p.value }

type publisher interface {
	PublishTo(ctx context.Context, key string, messages []byte, extra map[string]string) ([]byte, error)
}

type closer interface {
	Close() error
}

type publisherCloser interface {
	publisher
	closer
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx))
}

func run(ctx context.Context) int {
	var (
		hostname              = mustString("HOSTNAME")
		mode                  = mustProducerMode("MODE")
		loadRunID             = optionalString("LOAD_RUN_ID", uuid.New().String())
		concurrency           = mustInt("CONCURRENCY")
		messageGenerators     = mustInt("MESSAGE_GENERATORS")
		useOneClientPerSlot   = optionalBool("USE_ONE_CLIENT_PER_SLOT", false)
		enableSoftMemoryLimit = optionalBool("ENABLE_SOFT_MEMORY_LIMIT", false)
		softMemoryLimit       = mustBytes("SOFT_MEMORY_LIMIT")
		totalUsers            = mustInt("TOTAL_USERS")
		hotUserGroups         = mustMap("HOT_USER_GROUPS")
		eventTypes            = mustString("EVENT_TYPES")
		hotEventTypes         = mustMap("HOT_EVENT_TYPES")
		batchSizes            = mustMap("BATCH_SIZES")
		hotBatchSizes         = mustMap("HOT_BATCH_SIZES")
		maxEventsPerSecond    = mustInt("MAX_EVENTS_PER_SECOND")
		templatesPath         = optionalString("TEMPLATES_PATH", "../../templates/")
		sourcesList           = mustSourcesList("SOURCES")
		hotSourcesList        = optionalMap("HOT_SOURCES", sourcesList)
		validatorType         = optionalString("VALIDATOR_TYPE", "")
	)

	if strings.Index(hostname, hostnameSep) != 0 {
		printErr(fmt.Errorf("hostname should start with %s", hostnameSep))
		return 1
	}

	deploymentName, instanceNumber, err := getHostname(hostname)
	if err != nil {
		printErr(err)
		return 1
	}

	if concurrency < 1 {
		printErr(fmt.Errorf("concurrency has to be greater than zero: %d", concurrency))
		return 1
	}

	var newMemoryLimit int64
	if enableSoftMemoryLimit {
		// set up the memory limit to be 80% of the SOFT_MEMORY_LIMIT value
		newMemoryLimit = int64(float64(softMemoryLimit) * 0.8)
		_ = debug.SetMemoryLimit(newMemoryLimit)
	}

	if len(eventTypes) == 0 {
		printErr(fmt.Errorf("event types cannot be empty"))
		return 1
	}
	parsedEventTypes, err := parseEventTypes(eventTypes)
	if err != nil {
		printErr(fmt.Errorf("error parsing event types: %v", err))
		return 1
	}
	registerCustomEventGenerators(parsedEventTypes)
	if len(parsedEventTypes) != len(hotEventTypes) {
		printErr(fmt.Errorf("event types and hot event types should have the same length: %+v - %+v", parsedEventTypes, hotEventTypes))
		return 1
	}
	hotEventTypesPercentage := 0
	for _, v := range hotEventTypes {
		hotEventTypesPercentage += v
	}
	if hotEventTypesPercentage != 100 {
		printErr(fmt.Errorf("hot event types should sum to 100"))
		return 1
	}
	if len(batchSizes) != len(hotBatchSizes) {
		printErr(fmt.Errorf("batch sizes and hot batch sizes should have the same length: %+v - %+v", batchSizes, hotBatchSizes))
		return 1
	}
	if len(hotUserGroups) < 1 {
		printErr(fmt.Errorf("hot user groups should have at least one element"))
		return 1
	}
	hotUserGroupsPercentage := 0
	for _, v := range hotUserGroups {
		hotUserGroupsPercentage += v
	}
	if hotUserGroupsPercentage != 100 {
		printErr(fmt.Errorf("hot user groups should sum to 100"))
		return 1
	}

	if totalUsers%len(hotUserGroups) != 0 {
		printErr(fmt.Errorf("total users should be a multiple of the number of hot user groups"))
		return 1
	}
	if messageGenerators < 1 {
		printErr(fmt.Errorf("message generators has to be greater than zero: %d", messageGenerators))
		return 1
	}

	// Creating validator
	validatorFunc := validator.ValidateResponseBody(validatorType)

	// Creating throttler
	throttler, err := throttling.New(throttling.WithInMemoryGCRA(int64(maxEventsPerSecond)))
	if err != nil {
		printErr(fmt.Errorf("cannot create throttler: %v", err))
		return 1
	}

	fmt.Printf("Hostname: %s\n", hostname)
	fmt.Printf("CPUs: %d\n", runtime.GOMAXPROCS(-1))
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Concurrency: %d\n", concurrency)
	fmt.Printf("Message generators: %d\n", messageGenerators)
	fmt.Printf("Use one client per slot: %v\n", useOneClientPerSlot)
	fmt.Printf("Instance number: %d\n", instanceNumber)
	fmt.Printf("Total users: %d\n", totalUsers)
	if enableSoftMemoryLimit {
		fmt.Printf("Soft memory limit at 80%% of %s: %s\n", byteCount(uint64(softMemoryLimit)), byteCount(uint64(newMemoryLimit)))
	}

	// PROMETHEUS REGISTRY - START
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	constLabels := map[string]string{
		"mode":        mode.String(), // publisher type: e.g. http, stdout, etc...
		"deployment":  deploymentName,
		"concurrency": strconv.Itoa(concurrency),       // number of go routines publishing messages
		"msg_gen":     strconv.Itoa(messageGenerators), // number of go routines generating messages for the "slots"
		"total_users": strconv.Itoa(totalUsers),        // total number of unique userIDs used in the generated messages
	}
	publishRatePerSecond := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        metricsPrefix + "publish_rate_per_second",
		Help:        "Publish rate per second",
		ConstLabels: constLabels,
	})
	publishedMessagesCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "published_messages_count",
		Help:        "Total number of published messages",
		ConstLabels: constLabels,
	})
	numberOfRequestsCounter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        metricsPrefix + "number_of_requests_count",
		Help:        "Total number of requests",
		ConstLabels: constLabels,
	}, []string{"error"})
	msgGenLag := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "msg_generation_lag",
		Help:        "If less than a ms then this is increased meaning there are not enough generators per publishers.",
		ConstLabels: constLabels,
	})
	throttled := prometheus.NewCounter(prometheus.CounterOpts{
		Name:        metricsPrefix + "throttled",
		Help:        "Number of times we get throttled",
		ConstLabels: constLabels,
	})
	reg.MustRegister(publishRatePerSecond)
	reg.MustRegister(publishedMessagesCounter)
	reg.MustRegister(numberOfRequestsCounter)
	reg.MustRegister(msgGenLag)
	reg.MustRegister(throttled)
	// PROMETHEUS REGISTRY - END

	// Setting up dependencies for publishers - START
	statsFactory, err := stats.NewFactory(reg, stats.Data{
		Prefix:         metricsPrefix,
		DeploymentName: deploymentName,
		Mode:           mode.String(),
		Concurrency:    concurrency,
		TotalUsers:     totalUsers,
	})
	if err != nil {
		printErr(fmt.Errorf("cannot create stats factory: %v", err))
		return 1
	}

	var client publisherCloser
	if !useOneClientPerSlot {
		p, err := newProducer(loadRunID, mode, useOneClientPerSlot)
		if err != nil {
			printErr(fmt.Errorf("cannot create publisher: %v", err))
			return 1
		}
		client = statsFactory.New(p, "global")
	}
	// Setting up dependencies for publishers - END

	var (
		wg                  sync.WaitGroup
		httpServersWG       sync.WaitGroup
		publishedMessages   atomic.Int64
		processedBytes      atomic.Int64
		sentBytes           atomic.Int64
		startPublishingTime time.Time
		printer             = make(chan struct{})
		leakyErrors         = make(chan error, 1)
		messages            = make(chan *message, concurrency)
	)

	go func() {
		for {
			select {
			case <-printer:
				return
			case <-time.After(time.Second):
				select {
				case <-printer:
					return
				case err := <-leakyErrors:
					printErr(err)
				}
			}
		}
	}()

	// HTTP METRICS SERVER - START
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
			fmt.Printf("Shutting down the HTTP metrics server...\n")
			if err := srv.Shutdown(context.Background()); err != nil {
				printErr(fmt.Errorf("HTTP server shutdown: %w", err))
			}
		}()

		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			printErr(fmt.Errorf("HTTP server: %w", err))
		}
	}()
	// HTTP METRICS SERVER - END

	// PROFILER SERVER - START
	httpServersWG.Add(1)
	go func() {
		defer httpServersWG.Done()

		err := profiler.StartServer(ctx, 7777)
		if err != nil {
			printErr(fmt.Errorf("profiler server error: %w", err))
		}
	}()
	// PROFILER SERVER - END

	defer func() {
		fmt.Printf("Waiting for all routines to return...\n")
		wg.Wait()

		close(printer)

		fmt.Printf("Time to publish: %s\n", time.Since(startPublishingTime).Round(time.Millisecond))
		fmt.Printf("Published messages: %d\n", publishedMessages.Load())
		fmt.Printf("Processed bytes (%d): %s\n", processedBytes.Load(), byteCount(uint64(processedBytes.Load())))
		fmt.Printf("Sent bytes (%d): %s\n", sentBytes.Load(), byteCount(uint64(sentBytes.Load())))
		fmt.Printf("Publishing rate (msg/s): %.2f\n",
			float64(publishedMessages.Load())/time.Since(startPublishingTime).Seconds(),
		)

		fmt.Printf("Waiting for termination signal to close HTTP metrics server...\n")
		httpServersWG.Wait()
	}()

	// Starting the go routines - START
	fmt.Printf("Starting %d go routines...\n", concurrency)

	for i := 0; i < concurrency; i++ {
		var localClient publisherCloser
		if !useOneClientPerSlot {
			localClient = client
		} else {
			slotName := loadRunID + "-" + strconv.Itoa(i)
			p, err := newProducer(slotName, mode, useOneClientPerSlot)
			if err != nil {
				printErr(fmt.Errorf("cannot create publisher: %v", err))
				return 1
			}
			localClient = statsFactory.New(p, slotName)
		}

		wg.Add(1)
		go func(ch chan *message, client publisherCloser, i int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-ch:
					if !ok {
						return
					}

					publishRatePerSecond.Set(
						float64(publishedMessages.Load()) / time.Since(startPublishingTime).Seconds(),
					)

					if maxEventsPerSecond > 0 {
						for {
							allowed, after, _, err := throttler.AllowAfter(ctx, msg.NoOfEvents, int64(maxEventsPerSecond), 1, "key")
							if err != nil {
								panic(fmt.Errorf("error getting allowed events: %w", err))
							}
							if allowed {
								break
							}
							throttled.Inc()
							select {
							case <-ctx.Done():
								return
							case <-time.After(after):
							}
						}
					}

					rb, err := client.PublishTo(ctx, msg.UserID, msg.Payload, map[string]string{
						"auth":         msg.WriteKey,
						"anonymous_id": msg.UserID,
					})
					if ctx.Err() != nil {
						printErr(ctx.Err())
						continue
					}
					if err == nil && validatorFunc != nil {
						if _, err = validatorFunc(rb); err != nil {
							printErr(fmt.Errorf("error validating response body: %w", err))
							continue // publishedMessages and other metrics won't be updated
						}
					}
					if err == nil {
						publishedMessages.Add(msg.NoOfEvents)
						publishedMessagesCounter.Add(float64(msg.NoOfEvents))
						numberOfRequestsCounter.WithLabelValues("false").Inc()
						sentBytes.Add(int64(len(rb)))
						continue
					} else {
						numberOfRequestsCounter.WithLabelValues("true").Inc()
					}

					switch mode {
					case modeHTTP:
						if strings.Contains(err.Error(), "i/o timeout") {
							printLeakyErr(leakyErrors, fmt.Errorf("error for producer %d: %w", i, err), true)
							continue
						}
					}
					printErr(fmt.Errorf("[non-retryable] error for producer %d: %w", i, err))
					break
				}
			}
		}(messages, localClient, i)
	}
	// Starting the go routines - END

	fmt.Printf("Getting templates...\n")
	templates, err := getTemplates(templatesPath)
	if err != nil {
		printErr(fmt.Errorf("cannot get templates: %w", err))
		return 1
	}
	fmt.Printf("Building users concentration...\n")
	userIDsConcentration := getUserIDsConcentration(totalUsers, hotUserGroups, true)
	fmt.Printf("Building event types concentration...\n")
	eventTypesConcentration := getEventTypesConcentration(loadRunID, parsedEventTypes, hotEventTypes, eventGenerators, templates)
	fmt.Printf("Building batch sizes concentration...\n")
	batchSizesConcentration := getBatchSizesConcentration(batchSizes, hotBatchSizes)

	fmt.Printf("Building sources concentration...\n")
	sourcesConcentration := getSourcesConcentration(sourcesList, hotSourcesList)

	fmt.Printf("Publishing messages with %d generators...\n", messageGenerators)
	startPublishingTime = time.Now()
	group, gCtx := kitsync.NewEagerGroup(ctx, messageGenerators)
	for i := 0; i < messageGenerators; i++ {
		group.Go(func() error {
			defer fmt.Printf("Message generator %d is done\n", i)
			for {
				random := rand.Intn(100)
				userID := userIDsConcentration[random]()
				batchSize := batchSizesConcentration[random]
				writeKey := sourcesConcentration[random]()
				if writeKey == "" {
					printErr(fmt.Errorf("empty write key: current index: %d", random))
				}
				msg := eventTypesConcentration[random](userID, batchSize)
				processedBytes.Add(int64(len(msg)))

				start := time.Now()
				select {
				case <-gCtx.Done():
					return gCtx.Err()
				case messages <- &message{
					Payload:    msg,
					UserID:     userID,
					NoOfEvents: int64(batchSize),
					WriteKey:   writeKey,
				}:
					// Check if delta between now and start is less than 1ms then increment the counter
					if time.Since(start) < time.Millisecond {
						msgGenLag.Inc()
					}
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		printErr(fmt.Errorf("error generating messages: %w", err))
	}
	close(messages)

	return 0
}
