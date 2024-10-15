package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"golang.org/x/sync/errgroup"

	"github.com/rudderlabs/rudder-go-kit/profiler"
	kitrand "github.com/rudderlabs/rudder-go-kit/testhelper/rand"

	"rudder-ingester/internal/producer"
	"rudder-ingester/internal/stats"
)

const (
	modeStdout = "stdout"
	modeHTTP   = "http"

	hostnameSep = "rudder-ingester-"
)

type publisher interface {
	PublishTo(ctx context.Context, key string, messages []byte, extra map[string]string) error
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
		hostname                      = mustString("HOSTNAME")
		mode                          = mustString("MODE")
		concurrency                   = mustInt("CONCURRENCY")
		bufferingPerSlot              = optionalInt("BUFFERING_PER_SLOT", 1)
		randomKeyNames                = mustBool("RANDOM_KEY_NAMES", false)
		keysPerSlotMap                = mustMap("KEYS_PER_SLOT_MAP")
		trafficDistributionPercentage = mustMap("TRAFFIC_DISTRIBUTION_PERCENTAGE")
		useOneClientPerSlot           = optionalBool("USE_ONE_CLIENT_PER_SLOT", false)
		compressBeforePublish         = optionalBool("COMPRESS_BEFORE_PUBLISH", false)
		enableSoftMemoryLimit         = optionalBool("ENABLE_SOFT_MEMORY_LIMIT", false)
		totalMessages                 = mustInt("TOTAL_MESSAGES")
		totalDuration                 = optionalDuration("TOTAL_DURATION", 0) // 0 means complete as fast as possible
		softMemoryLimit               = mustBytes("SOFT_MEMORY_LIMIT")
		rudderEventsBatchSize         = optionalInt("RUDDER_EVENTS_BATCH_SIZE", 0) // 0 means send as single events
	)

	sourcesList := strings.Split(os.Getenv("SOURCES"), ",")
	if len(sourcesList) < 1 {
		fatal(fmt.Errorf("invalid number of sources [<1]: %d", len(sourcesList)))
	}

	if strings.Index(hostname, hostnameSep) != 0 {
		fatal(fmt.Errorf("hostname should start with %s", hostnameSep))
	}

	re := regexp.MustCompile(`rudder-ingester-(\d+)`)
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

	var newMemoryLimit int64
	if enableSoftMemoryLimit {
		// set up the memory limit to be 80% of the SOFT_MEMORY_LIMIT value
		newMemoryLimit = int64(float64(softMemoryLimit) * 0.8)
		_ = debug.SetMemoryLimit(newMemoryLimit)
	}

	fmt.Printf("CPUs: %d\n", runtime.NumCPU())
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Instance number: %d\n", instanceNumber)
	fmt.Printf("Buffering per source: %d\n", bufferingPerSlot)
	fmt.Printf("Keys per source map: %v\n", keysPerSlotMap)
	fmt.Printf("Traffic distribution percentage: %v\n", trafficDistributionPercentage)
	fmt.Printf("Random key names: %v\n", randomKeyNames)
	fmt.Printf("Use one client per slot: %v\n", useOneClientPerSlot)
	fmt.Printf("Compress before publish: %v\n", compressBeforePublish)
	if enableSoftMemoryLimit {
		fmt.Printf("Soft memory limit at 80%% of %s: %s\n", byteCount(uint64(softMemoryLimit)), byteCount(uint64(newMemoryLimit)))
	}
	fmt.Printf("Total messages: %d\n", totalMessages)
	fmt.Printf("Total duration: %s\n", totalDuration)

	slots, totalKeys := getSlots(sourcesList[instanceNumber], concurrency, keysPerSlotMap, randomKeyNames)
	fmt.Printf("Slots: %d\n", len(slots))
	fmt.Printf("Total keys: %d\n", totalKeys)
	for i, s := range slots {
		fmt.Printf("Slot %d has %d keys\n", i+1, len(s.keys))
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
			"mode":       mode,
			"slots":      strconv.Itoa(len(slots)),
			"total_keys": strconv.Itoa(totalKeys),
		},
	})
	reg.MustRegister(publishRatePerSecond)
	// PROMETHEUS REGISTRY - END

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

	var startPublishingTime time.Time

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	var (
		wg                       sync.WaitGroup
		httpServersWG            sync.WaitGroup
		publishedMessages        atomic.Int64
		processedBytes           atomic.Int64
		processedCompressedBytes atomic.Int64
		printer                  = make(chan struct{})
		leakyErrors              = make(chan error, 1)
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

	defer func() {
		fmt.Printf("Waiting for all routines to return...\n")
		wg.Wait()

		close(printer)
		fmt.Printf("Closing producers...\n")
		if !useOneClientPerSlot {
			if err := slots[0].client.Close(); err != nil {
				printErr(err)
			}
		} else {
			for i, slot := range slots {
				if err := slot.client.Close(); err != nil {
					printErr(fmt.Errorf("slot %d: %w", i, err))
				}
			}
		}

		fmt.Printf("Time to publish: %s\n", time.Since(startPublishingTime).Round(time.Millisecond))
		fmt.Printf("Published messages: %d\n", publishedMessages.Load())
		fmt.Printf("Processed bytes (%d): %s\n", processedBytes.Load(), byteCount(uint64(processedBytes.Load())))
		fmt.Printf("Processed compressed bytes (%d): %s\n", processedCompressedBytes.Load(), byteCount(uint64(processedCompressedBytes.Load())))
		fmt.Printf("Publishing rate (msg/s): %.2f\n",
			float64(publishedMessages.Load())/time.Since(startPublishingTime).Seconds(),
		)

		fmt.Printf("Waiting for termination signal to close HTTP metrics server...\n")
		httpServersWG.Wait()
	}()

	// HTTP METRICS SERVER - START
	if !useOneClientPerSlot {
		sf, err := stats.NewFactory(reg, stats.Data{
			Mode:        mode,
			Concurrency: concurrency,
			// TotalTopics: noOfTopics, // TODO clean up
			TotalKeys: totalKeys,
		})
		if err != nil {
			fatal(fmt.Errorf("cannot create stats factory: %v", err))
		}

		p, err := publisherFactory(os.Getenv("HOSTNAME"))
		if err != nil {
			fatal(fmt.Errorf("cannot create publisher: %v", err))
		}

		pub := sf.New(p)
		for i := range slots {
			slots[i].client = pub
		}
	} else {
		sf, err := stats.NewFactory(reg, stats.Data{
			Mode:        mode,
			Concurrency: concurrency,
			// TotalTopics: noOfTopics, // TODO clean up
			TotalKeys: totalKeys,
		})
		if err != nil {
			printErr(fmt.Errorf("cannot create stats factory: %v", err))
			return
		}

		for i := range slots {
			p, err := publisherFactory(os.Getenv("HOSTNAME") + "_" + strconv.Itoa(i))
			if err != nil {
				printErr(fmt.Errorf("cannot create publisher: %v", err))
				return
			}
			slots[i].client = sf.New(p)
		}
	}

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

	fmt.Printf("Preparing %d slots with concurrency %d...\n", len(slots), concurrency)

	messages := make(chan *message, concurrency)

	for i := 0; i < len(slots); i++ {
		wg.Add(1)
		go func(s *slot, i int) {
			defer wg.Done()

			buffer := make([]*message, 0, bufferingPerSlot)

			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-messages:
					if !ok {
						return
					}

					publishRatePerSecond.Set(
						float64(publishedMessages.Load()) / time.Since(startPublishingTime).Seconds(),
					)

					buffer = append(buffer, msg)
					if len(buffer) < bufferingPerSlot {
						continue
					}

					for _, msg := range buffer {
						key := s.keys[rand.Intn(len(s.keys))]
						err := s.client.PublishTo(ctx, key, msg.payload, map[string]string{
							"auth":         s.writeKey,
							"anonymous_id": msg.anonymousID,
						})
						if ctx.Err() != nil {
							printErr(ctx.Err())
							break
						}
						if err == nil {
							publishedMessages.Add(1)
							break
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

				buffer = make([]*message, 0, bufferingPerSlot)

				if totalDuration > 0 { // Calculate the expected finish time for this message
					expectedFinishTime := startPublishingTime.Add(
						totalDuration / time.Duration(totalMessages) * time.Duration(publishedMessages.Load()),
					)
					sleepDuration := time.Until(expectedFinishTime)
					if sleepDuration > 0 {
						select {
						case <-ctx.Done():
							return
						case <-time.After(sleepDuration):
						}
					}
				}
			}
		}(slots[i], i)
	}

	samplePayload, err := os.ReadFile("./samples/page.json")
	if err != nil {
		printErr(fmt.Errorf("cannot read sample.json file: %w", err))
		return
	}
	buf := bytes.NewBuffer(nil)
	if err = json.Compact(buf, samplePayload); err != nil {
		printErr(fmt.Errorf("cannot compact sample.json: %w", err))
		return
	}
	samplePayload = buf.Bytes()
	sampleMsg, _ := getRudderEvent(samplePayload, rudderEventsBatchSize)
	fmt.Printf("Estimated total data size: %s\n", byteCount(uint64(len(sampleMsg)*totalMessages)))

	startPublishingTime = time.Now()
	group, gCtx := errgroup.WithContext(ctx)
	group.SetLimit(concurrency + 1)

	fmt.Printf("Publishing %d messages...\n", totalMessages)

	for i := 0; i < totalMessages; i++ {
		group.Go(func() error {
			msg, anonymousID := getRudderEvent(samplePayload, rudderEventsBatchSize)

			processedBytes.Add(int64(len(msg)))

			select {
			case <-gCtx.Done():
				close(messages)
				return gCtx.Err()
			case messages <- &message{
				payload:     msg,
				anonymousID: anonymousID,
			}:
			}

			return nil
		})
	}

	if err := group.Wait(); err != nil {
		fatal(fmt.Errorf("cannot generate messages: %v", err))
	}
	close(messages)
}

type slot struct {
	writeKey string
	keys     []string
	client   publisherCloser
}

type message struct {
	payload     []byte
	anonymousID string
}

func getRudderEvent(payload []byte, batchSize int) ([]byte, string) {
	anonID := []byte(uuid.New().String())
	process := func(payload, anonID []byte) []byte {
		msgID := []byte(uuid.New().String())

		timestamp := time.Now().Format("2006-01-02T15:04:05.999Z")

		buf := make([]byte, len(payload))
		_ = copy(buf, payload)

		buf = bytes.ReplaceAll(buf, []byte(`<MSG_ID>`), msgID)
		buf = bytes.ReplaceAll(buf, []byte(`<ANON_ID>`), anonID)
		buf = bytes.ReplaceAll(buf, []byte(`<TIMESTAMP>`), []byte(timestamp))

		return buf
	}

	if batchSize < 1 {
		return process(payload, anonID), string(anonID)
	}

	buf := bytes.NewBuffer(nil)
	write := func(s []byte) {
		_, err := buf.Write(s)
		if err != nil {
			panic(fmt.Errorf("cannot write to buffer: %w", err))
		}
	}
	write([]byte(`{"batch":[`))
	for i := 0; i < batchSize; i++ {
		payloadCopy := make([]byte, len(payload))
		_ = copy(payloadCopy, payload)
		write(process(payloadCopy, anonID))
		if i < batchSize-1 {
			write([]byte(`,`))
		}
	}
	write([]byte(`]}`))

	return buf.Bytes(), string(anonID)
}

// getSlots evenly distributes topics among slots, respecting the traffic distribution
func getSlots(writeKey string, concurrency int, keysPerSlotMap []int, randomKeyNames bool) ([]*slot, int) {
	slots := make([]*slot, concurrency)
	for i := range slots {
		slots[i] = &slot{writeKey: writeKey}

		keysForCurrentSlot := keysPerSlotMap[i%len(keysPerSlotMap)]
		if keysForCurrentSlot == 0 {
			panic(fmt.Sprintf("keysPerSlotMap[%d] is 0", i%len(keysPerSlotMap)))
		}

		keys := make([]string, 0, keysForCurrentSlot)
		for j := 0; j < keysForCurrentSlot; j++ {
			if randomKeyNames {
				keys = append(keys, kitrand.UniqueString(10))
			} else {
				keys = append(keys, writeKey+"-key-"+strconv.Itoa(i)+"-"+strconv.Itoa(j))
			}
		}
	}
	return slots, 0
}

func byteCount(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// convertToBytes converts text representation (like "1kb", "2mb") to bytes.
func convertToBytes(input string) (int, error) {
	input = strings.ToLower(input) // Make sure the input is lowercase for easier parsing

	// Identify the unit and numeric part of the input
	var unit string
	var numberPart string
	for i, char := range input {
		if char < '0' || char > '9' {
			unit = input[i:]
			numberPart = input[:i]
			break
		}
	}

	// Convert the number part to an integer
	number, err := strconv.Atoi(numberPart)
	if err != nil {
		return 0, err
	}

	switch unit {
	case "kb":
		return number * 1000, nil
	case "kib":
		return number * 1024, nil
	case "mb":
		return number * 1000000, nil
	case "mib":
		return number * 1048576, nil
	case "gb":
		return number * 1000000000, nil
	case "gi":
		return number * 1073741824, nil
	case "tb":
		return number * 1000000000000, nil
	case "tib":
		return number * 1099511627776, nil
	default:
		return 0, fmt.Errorf("unrecognized unit: %s", unit)
	}
}

func mustBytes(s string) int {
	v := os.Getenv(s)
	if v == "" {
		fatal(fmt.Errorf("invalid bytes: %s", s))
	}
	i, err := convertToBytes(v)
	if err != nil {
		fatal(fmt.Errorf("invalid bytes: %s: %v", s, err))
	}
	return i
}

func mustInt(s string) int {
	i, err := strconv.Atoi(os.Getenv(s))
	if err != nil {
		fatal(fmt.Errorf("invalid int: %s: %v", s, err))
	}
	return i
}

func optionalInt(s string, def int) int {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func optionalDuration(s string, def time.Duration) time.Duration {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func optionalBool(s string, def bool) bool {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func optionalBytes(s string, def int) int {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	i, err := convertToBytes(v)
	if err != nil {
		return def
	}
	return i
}

func mustString(s string) string {
	v := os.Getenv(s)
	if v == "" {
		fatal(fmt.Errorf("invalid string: %s", s))
	}
	return v
}

func mustBool(s string, def bool) bool {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		fatal(fmt.Errorf("invalid bool: %s", s))
	}
	return b
}

func mustMap(s string) []int {
	v := strings.Split(os.Getenv(s), "_")
	var (
		err error
		r   = make([]int, len(v))
	)
	for i := range v {
		r[i], err = strconv.Atoi(v[i])
		if err != nil {
			fatal(err)
		}
	}
	return r
}

func printErr(err error, retry ...bool) {
	if len(retry) > 0 && retry[0] == true {
		_, _ = fmt.Fprintf(os.Stdout, "error: %v (retrying...)\n\n", err)
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "error: %v\n\n", err)
}

func printLeakyErr(ch chan<- error, err error, retry ...bool) {
	if len(retry) > 0 && retry[0] == true {
		err = fmt.Errorf("%v (retrying...)", err)
	}
	select {
	case ch <- err:
	default:
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stdout, "fatal: %v\n\n", err)
	os.Exit(1)
}
