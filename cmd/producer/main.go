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
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"rudder-load/internal/producer"
	"rudder-load/internal/stats"

	"github.com/rudderlabs/rudder-go-kit/profiler"
	kitsync "github.com/rudderlabs/rudder-go-kit/sync"
)

const (
	modeStdout = "stdout"
	modeHTTP   = "http"

	hostnameSep = "rudder-load-"

	templatesPath      = "./templates/"
	templatesExtension = ".json.tmpl"
)

type publisher interface {
	PublishTo(ctx context.Context, key string, messages []byte, extra map[string]string) (int, error)
}

type closer interface {
	Close() error
}

type publisherCloser interface {
	publisher
	closer
}

func main() {
	var (
		hostname              = mustString("HOSTNAME")
		mode                  = mustString("MODE")
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
	)

	sourcesList := strings.Split(os.Getenv("SOURCES"), ",")
	if len(sourcesList) < 1 {
		fatal(fmt.Errorf("invalid number of sources [<1]: %d", len(sourcesList)))
	}

	if strings.Index(hostname, hostnameSep) != 0 {
		fatal(fmt.Errorf("hostname should start with %s", hostnameSep))
	}

	re := regexp.MustCompile(`rudder-load-(\d+)`)
	match := re.FindStringSubmatch(hostname)
	if len(match) <= 1 {
		fatal(fmt.Errorf("hostname is invalid: %s", hostname))
	}

	instanceNumber, err := strconv.Atoi(match[1])
	if err != nil {
		fatal(fmt.Errorf("error getting instance number from hostname: %v", err))
	}
	if len(sourcesList) < (instanceNumber + 1) {
		fatal(fmt.Errorf("instance number %d is greater than the number of sources %d", instanceNumber, len(sourcesList)))
	}
	if concurrency < 1 {
		fatal(fmt.Errorf("concurrency has to be greater than zero: %d", concurrency))
	}

	var newMemoryLimit int64
	if enableSoftMemoryLimit {
		// set up the memory limit to be 80% of the SOFT_MEMORY_LIMIT value
		newMemoryLimit = int64(float64(softMemoryLimit) * 0.8)
		_ = debug.SetMemoryLimit(newMemoryLimit)
	}

	if len(eventTypes) == 0 {
		fatal(fmt.Errorf("event types cannot be empty"))
	}
	if len(eventTypes) != len(hotEventTypes) {
		fatal(fmt.Errorf("event types and hot event types should have the same length"))
	}
	hotEventTypesPercentage := 0
	for _, v := range hotEventTypes {
		hotEventTypesPercentage += v
	}
	if hotEventTypesPercentage != 100 {
		fatal(fmt.Errorf("hot event types should sum to 100"))
	}
	if len(hotUserGroups) < 1 {
		fatal(fmt.Errorf("hot user groups should have at least one element"))
	}
	hotUserGroupsPercentage := 0
	for _, v := range hotUserGroups {
		hotUserGroupsPercentage += v
	}
	if hotUserGroupsPercentage != 100 {
		fatal(fmt.Errorf("hot user groups should sum to 100"))
	}
	if totalUsers&len(hotUserGroups) != 0 {
		fatal(fmt.Errorf("total users should be a multiple of the number of hot user groups"))
	}
	parsedEventTypes, err := parseEventTypes(eventTypes)
	if err != nil {
		fatal(fmt.Errorf("error parsing event types: %v", err))
	}

	writeKey := sourcesList[instanceNumber]

	fmt.Printf("Hostname: %s\n", hostname)
	fmt.Printf("CPUs: %d\n", runtime.GOMAXPROCS(-1))
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Concurrency: %d\n", concurrency)
	fmt.Printf("Message generators: %d\n", messageGenerators)
	fmt.Printf("Use one client per slot: %v\n", useOneClientPerSlot)
	fmt.Printf("Instance number: %d\n", instanceNumber)
	fmt.Printf("WriteKey handled by this replica: %s\n", writeKey)
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
	publishRatePerSecond := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "publish_rate_per_second",
		Help: "Publish rate per second",
		ConstLabels: map[string]string{
			// TODO updates other stats
			"mode":        mode,                            // publisher type: e.g. http, stdout, etc...
			"concurrency": strconv.Itoa(concurrency),       // number of go routines publishing messages
			"msg_gen":     strconv.Itoa(messageGenerators), // number of go routines generating messages for the "slots"
			"total_users": strconv.Itoa(totalUsers),        // total number of unique userIDs used in the generated messages
		},
	})
	reg.MustRegister(publishRatePerSecond)
	// PROMETHEUS REGISTRY - END

	// Setting up dependencies for publishers - START
	publisherFactory := func(clientID string) (publisherCloser, error) {
		switch mode {
		case modeHTTP:
			return producer.NewHTTPProducer(os.Environ())
		case modeStdout:
			return producer.NewStdoutPublisher(), nil
		default:
			return nil, fmt.Errorf("unknown mode: %s", mode)
		}
	}

	var client publisherCloser
	if !useOneClientPerSlot {
		sf, err := stats.NewFactory(reg, stats.Data{
			Mode:        mode,
			Concurrency: concurrency,
			TotalUsers:  totalUsers,
		})
		if err != nil {
			fatal(fmt.Errorf("cannot create stats factory: %v", err))
		}

		p, err := publisherFactory(os.Getenv("HOSTNAME"))
		if err != nil {
			fatal(fmt.Errorf("cannot create publisher: %v", err))
		}

		client = sf.New(p)
	}
	// Setting up dependencies for publishers - END

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

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
		client := client
		if useOneClientPerSlot {
			sf, err := stats.NewFactory(reg, stats.Data{
				Mode:        mode,
				Concurrency: concurrency,
				TotalUsers:  totalUsers,
			})
			if err != nil {
				printErr(fmt.Errorf("cannot create stats factory: %v", err))
				return
			}

			p, err := publisherFactory(os.Getenv("HOSTNAME") + "_" + strconv.Itoa(i))
			if err != nil {
				printErr(fmt.Errorf("cannot create publisher: %v", err))
				return
			}
			client = sf.New(p)
		}

		wg.Add(1)
		go func(ch chan *message, client publisherCloser) {
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

					n, err := client.PublishTo(ctx, msg.UserID, msg.Payload, map[string]string{
						"auth":         writeKey,
						"anonymous_id": msg.UserID,
					})
					if ctx.Err() != nil {
						printErr(ctx.Err())
						continue
					}
					if err == nil {
						publishedMessages.Add(1)
						sentBytes.Add(int64(n))
						continue
					}

					switch mode {
					case modeHTTP:
						if strings.Contains(err.Error(), "i/o timeout") {
							printLeakyErr(leakyErrors, fmt.Errorf("error for slot %d: %w", i, err), true)
							continue
						}
					}
					printErr(fmt.Errorf("[non-retryable] error for slot %d: %w", i, err))
					break
				}
			}
		}(messages, client)
	}
	// Starting the go routines - END

	templates, err := getTemplates(templatesPath)
	if err != nil {
		printErr(fmt.Errorf("cannot get templates: %w", err))
		return
	}
	userIDsConcentration := getUserIDsConcentration(totalUsers, hotUserGroups, true)
	eventTypesConcentration := getEventTypesConcentration(loadRunID, parsedEventTypes, hotEventTypes, eventGenerators, templates)

	fmt.Printf("Publishing messages...\n")
	startPublishingTime = time.Now()
	group, gCtx := kitsync.NewEagerGroup(ctx, messageGenerators)
	for i := 0; i < messageGenerators; i++ {
		group.Go(func() error {
			for {
				userID := userIDsConcentration[rand.Intn(100)]()
				msg := eventTypesConcentration[rand.Intn(100)](userID)
				processedBytes.Add(int64(len(msg)))

				select {
				case <-gCtx.Done():
				case messages <- &message{
					Payload: msg,
					UserID:  userID,
				}:
				}
				return nil
			}
		})
	}
	if err := group.Wait(); err != nil {
		printErr(fmt.Errorf("error generating messages: %w", err))
	}
	close(messages)
}
