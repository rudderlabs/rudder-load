package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/rudderlabs/rudder-go-kit/profiler"
	kitsync "github.com/rudderlabs/rudder-go-kit/sync"
	kitrand "github.com/rudderlabs/rudder-go-kit/testhelper/rand"

	"rudder-load/internal/producer"
	"rudder-load/internal/stats"
)

const (
	modeStdout = "stdout"
	modeHTTP   = "http"

	hostnameSep = "rudder-load-"

	samplesPath      = "./samples/"
	samplesExtension = ".json.tmpl"
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
		hostname                      = mustString("HOSTNAME")
		mode                          = mustString("MODE")
		concurrency                   = mustInt("CONCURRENCY")
		messageGenerators             = mustInt("MESSAGE_GENERATORS")
		randomKeyNames                = mustBool("RANDOM_KEY_NAMES", false)
		keysPerSlotMap                = mustMap("KEYS_PER_SLOT_MAP")
		trafficDistributionPercentage = mustMap("TRAFFIC_DISTRIBUTION_PERCENTAGE")
		useOneClientPerSlot           = optionalBool("USE_ONE_CLIENT_PER_SLOT", false)
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

	totalPercentage := 0
	for _, v := range trafficDistributionPercentage {
		totalPercentage += v
	}
	if totalPercentage != 100 {
		fatal(fmt.Errorf("total percentage is not 100: %d", totalPercentage))
	}
	if concurrency%100 != 0 {
		fatal(fmt.Errorf("concurrency is not a multiple of 100: %d", concurrency))
	}
	if concurrency < 100 {
		fatal(fmt.Errorf("concurrency is less than 100: %d", concurrency))
	}

	var newMemoryLimit int64
	if enableSoftMemoryLimit {
		// set up the memory limit to be 80% of the SOFT_MEMORY_LIMIT value
		newMemoryLimit = int64(float64(softMemoryLimit) * 0.8)
		_ = debug.SetMemoryLimit(newMemoryLimit)
	}

	fmt.Printf("Hostname: %s\n", hostname)
	fmt.Printf("CPUs: %d\n", runtime.GOMAXPROCS(-1))
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Concurrency: %d\n", concurrency)
	fmt.Printf("Message generators: %d\n", messageGenerators)
	fmt.Printf("Random key names: %v\n", randomKeyNames)
	fmt.Printf("Keys per slot map: %v\n", keysPerSlotMap)
	fmt.Printf("Traffic distribution percentage: %v\n", trafficDistributionPercentage)
	fmt.Printf("Use one client per slot: %v\n", useOneClientPerSlot)
	fmt.Printf("Instance number: %d\n", instanceNumber)
	fmt.Printf("WriteKey handled by this replica: %s\n", sourcesList[instanceNumber])
	fmt.Printf("Rudder events batch size: %d\n", rudderEventsBatchSize)
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
			"mode":       mode,                            // publisher type: e.g. http, stdout, etc...
			"msg_gen":    strconv.Itoa(messageGenerators), // number of go routines generating messages for the "slots"
			"slots":      strconv.Itoa(len(slots)),        // number of concurrent go routines publishing for the same writeKey
			"total_keys": strconv.Itoa(totalKeys),         // total number of unique keys used across all go routines
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
		wg                sync.WaitGroup
		httpServersWG     sync.WaitGroup
		publishedMessages atomic.Int64
		processedBytes    atomic.Int64
		sentBytes         atomic.Int64
		printer           = make(chan struct{})
		leakyErrors       = make(chan error, 1)
	)

	channels := make([]chan *message, 0, 100)
	for i := 0; i < 100; i++ {
		channels = append(channels, make(chan *message, 1))
	}

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
		fmt.Printf("Sent bytes (%d): %s\n", sentBytes.Load(), byteCount(uint64(sentBytes.Load())))
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
			TotalKeys:   totalKeys,
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
			TotalKeys:   totalKeys,
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

	startIdx := 0
	for _, percentage := range trafficDistributionPercentage {
		// Calculate how many goroutines for this percentage
		groupCount := (concurrency * percentage) / 100

		// Launch goroutines for the current group
		for i := 0; i < groupCount; i++ {
			ch := channels[startIdx+(i%percentage)]  // Cycle through the assigned channels
			slotIndex := startIdx + (i % percentage) // Correct slot index based on the group

			wg.Add(1)
			go func(ch chan *message, s *slot) {
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

						key := s.keys[rand.Intn(len(s.keys))]
						n, err := s.client.PublishTo(ctx, key, msg.payload, map[string]string{
							"auth":         s.writeKey,
							"anonymous_id": msg.anonymousID,
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
			}(ch, slots[slotIndex]) // Pass the correct slot
		}

		startIdx += percentage // Update the starting index for the next distribution
	}

	samples, err := getSamples(samplesPath)
	startPublishingTime = time.Now()
	fmt.Printf("Publishing %d messages...\n", totalMessages)

	group, gCtx := kitsync.NewEagerGroup(ctx, messageGenerators)

	for i, j := 0, 0; i < totalMessages; i++ {
		group.Go(func() error {
			msg, anonymousID := getRudderEvent(samples["page"], rudderEventsBatchSize)
			processedBytes.Add(int64(len(msg)))

			select {
			case <-gCtx.Done():
				return nil
			case channels[j] <- &message{
				payload:     msg,
				anonymousID: anonymousID,
			}:
			}
			return nil
		})

		j++
		if j == 100 {
			j = 0
		}
	}
	if err := group.Wait(); err != nil {
		printErr(fmt.Errorf("error generating messages: %w", err))
	}
	for _, ch := range channels {
		close(ch)
	}
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

// TODO add property to distinguish between runs e.g. load_run_id
// TODO message_generators decide userId concentration etc...
// TODO add a way to have diversity in the events that we send (could be event size, could be type, etc)
// TODO use range for batch events
func getRudderEvent(tmpl *template.Template, batchSize int) ([]byte, string) {
	var (
		buf       bytes.Buffer
		anonID    = uuid.New().String()
		timestamp = time.Now().Format("2006-01-02T15:04:05.999Z")
	)
	err := tmpl.Execute(&buf, map[string]string{
		"MessageID":         uuid.New().String(),
		"AnonymousID":       anonID,
		"OriginalTimestamp": timestamp,
		"SentAt":            timestamp,
	})
	if err != nil {
		panic(fmt.Errorf("cannot execute template: %w", err))
	}
	return buf.Bytes(), anonID
}

func getSamples(samplesPath string) (map[string]*template.Template, error) {
	files, err := os.ReadDir(samplesPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read samples directory: %w", err)
	}

	funcMap := template.FuncMap{
		"Sub1": func(n int) int { return n - 1 },
	}

	samples := make(map[string]*template.Template)
	for _, file := range files {
		if !file.IsDir() {
			tmpl, err := template.New(file.Name()).Funcs(funcMap).ParseFiles(filepath.Join(samplesPath, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("cannot parse template file: %w", err)
			}

			eventType := strings.Replace(file.Name(), samplesExtension, "", 1)
			samples[eventType] = tmpl
		}
	}

	return samples, nil
}

func getSlots(writeKey string, concurrency int, keysPerSlotMap []int, randomKeyNames bool) ([]*slot, int) {
	var (
		totalKeys int
		slots     = make([]*slot, concurrency)
	)
	for i := range slots {
		slots[i] = &slot{writeKey: writeKey}

		keysForCurrentSlot := keysPerSlotMap[i%len(keysPerSlotMap)]
		if keysForCurrentSlot < 1 {
			panic(fmt.Sprintf("keysPerSlotMap[%d] is < 1", i%len(keysPerSlotMap)))
		}

		totalKeys += keysForCurrentSlot
		slots[i].keys = make([]string, 0, keysForCurrentSlot)
		for j := 0; j < keysForCurrentSlot; j++ {
			if randomKeyNames {
				slots[i].keys = append(slots[i].keys, kitrand.UniqueString(10))
			} else {
				slots[i].keys = append(slots[i].keys, writeKey+"-key-"+strconv.Itoa(i)+"-"+strconv.Itoa(j))
			}
		}
	}
	return slots, totalKeys
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
