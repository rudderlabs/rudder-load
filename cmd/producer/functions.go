package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"rudder-load/internal/producer"

	"github.com/google/uuid"
)

type message struct {
	Payload    []byte
	UserID     string
	NoOfEvents int64
	WriteKey   string
}

func getTemplates(templatesPath string) (map[string]*template.Template, error) {
	files, err := os.ReadDir(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read templates directory: %w", err)
	}

	funcMap := template.FuncMap{
		"uuid":    func() string { return uuid.New().String() },
		"sub":     func(a, b int) int { return a - b },
		"nowNano": func() int64 { return time.Now().UnixNano() },
		"loop": func(n int) <-chan int {
			ch := make(chan int)
			go func() {
				for i := 0; i < n; i++ {
					ch <- i
				}
				close(ch)
			}()
			return ch
		},
	}

	templates := make(map[string]*template.Template)
	for _, file := range files {
		if !file.IsDir() {
			tmpl, err := template.New(file.Name()).Funcs(funcMap).ParseFiles(filepath.Join(templatesPath, file.Name()))
			if err != nil {
				return nil, fmt.Errorf("cannot parse template file: %w", err)
			}

			eventType := strings.Replace(file.Name(), templatesExtension, "", 1)
			templates[eventType] = tmpl
		}
	}

	return templates, nil
}

func getUserIDsConcentration(totalUsers int, hotUserGroups []int, random bool) []func() string {
	totalPercentage := 0
	for _, percentage := range hotUserGroups {
		totalPercentage += percentage
	}
	if totalPercentage != 100 {
		panic("hot user groups percentages do not sum up to 100")
	}
	if totalUsers%len(hotUserGroups) != 0 {
		panic("total users must be divisible by the number of hot user groups")
	}

	userIDs := make([]string, totalUsers)
	for i := 0; i < totalUsers; i++ {
		if random {
			userIDs[i] = uuid.New().String()
		} else {
			userIDs[i] = strconv.Itoa(i)
		}
	}

	var (
		startUserID          = 0
		startConcentration   = 0
		userIDsConcentration = make([]func() string, 100)
	)
	for _, hotUserGroup := range hotUserGroups {
		usersInGroup := totalUsers * hotUserGroup / 100
		startUserIDCopy := startUserID
		f := func() string {
			return userIDs[rand.Intn(usersInGroup)+startUserIDCopy]
		}
		for i := startConcentration; i < hotUserGroup+startConcentration; i++ {
			userIDsConcentration[i] = f
		}
		startUserID += usersInGroup
		startConcentration += hotUserGroup
	}

	return userIDsConcentration
}

func getBatchSizesConcentration(batchSizes, hotBatchSizes []int) []int {
	totalPercentage := 0
	for _, percentage := range hotBatchSizes {
		totalPercentage += percentage
	}
	if totalPercentage != 100 {
		panic("hot batch sizes percentages do not sum up to 100")
	}
	if len(batchSizes) != len(hotBatchSizes) {
		panic("batch sizes and hot batch sizes must have the same length")
	}

	var (
		startID                 = 0
		batchSizesConcentration = make([]int, 100)
	)
	for i, hotBatchPercentage := range hotBatchSizes {
		batchSize := batchSizes[i]
		for j := startID; j < hotBatchPercentage+startID; j++ {
			batchSizesConcentration[j] = batchSize
		}
		startID += hotBatchPercentage
	}

	return batchSizesConcentration
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
		panic(fmt.Errorf("invalid bytes: %s", s))
	}
	i, err := convertToBytes(v)
	if err != nil {
		panic(fmt.Errorf("invalid bytes: %s: %v", s, err))
	}
	return i
}

func mustInt(s string) int {
	i, err := strconv.Atoi(os.Getenv(s))
	if err != nil {
		panic(fmt.Errorf("invalid int: %s: %v", s, err))
	}
	return i
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

func optionalString(s, def string) string {
	v := os.Getenv(s)
	if v == "" {
		return def
	}
	return v
}

func mustString(s string) string {
	v := os.Getenv(s)
	if v == "" {
		panic(fmt.Errorf("invalid string: %s", s))
	}
	return v
}

func mustMap(s string) []int {
	v := strings.Split(os.Getenv(s), ",")
	var (
		err error
		r   = make([]int, len(v))
	)
	for i := range v {
		r[i], err = strconv.Atoi(v[i])
		if err != nil {
			panic(fmt.Errorf("invalid map: %s: %v", s, err))
		}
	}
	return r
}

func mustList(s string) []string {
	v := strings.Split(os.Getenv(s), ",")
	if len(v) < 1 {
		panic(fmt.Errorf("invalid list: %s", s))
	}
	return v
}

// mustSourcesList reads the SOURCES environment variable and returns a list of sources.
// If the environment variable is a single integer, it generates that many sources with
// predictable names like "source-0", "source-1", etc.
// If the environment variable is a comma-separated list, it returns that list.
func mustSourcesList(s string) []string {
	v := os.Getenv(s)
	if v == "" {
		panic(fmt.Errorf("invalid sources: %q", s))
	}

	// Check if the value is a single integer
	count, err := strconv.Atoi(v)
	if err == nil && count > 0 {
		// Generate sources with predictable names
		sources := make([]string, count)
		for i := 0; i < count; i++ {
			sources[i] = "source-" + strconv.Itoa(i)
		}
		return sources
	}

	// Otherwise, treat it as a comma-separated list
	sources := strings.Split(v, ",")
	if len(sources) < 1 {
		panic(fmt.Errorf("invalid sources: %q", s))
	}
	for _, source := range sources {
		if source == "" {
			panic(fmt.Errorf("got an empty source: %q", s))
		}
	}
	return sources
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

// getSourcesConcentration creates a slice of functions that return source write keys based on the configured hot percentages
func getSourcesConcentration(sources []string, hotSources []int) []func() string {
	totalPercentage := 0
	for _, percentage := range hotSources {
		totalPercentage += percentage
	}
	if totalPercentage != 100 {
		panic("hot sources percentages do not sum up to 100")
	}
	if len(sources) != len(hotSources) {
		panic("sources and hot sources must have the same length")
	}

	var (
		startID              = 0
		sourcesConcentration = make([]func() string, 100)
	)
	for i, hotSourcePercentage := range hotSources {
		source := sources[i]
		f := func() string {
			return source
		}
		for j := startID; j < hotSourcePercentage+startID; j++ {
			sourcesConcentration[j] = f
		}
		startID += hotSourcePercentage
	}

	return sourcesConcentration
}

// optionalMap reads a comma-separated list of integers from an environment variable.
// If the environment variable is empty, it creates an equally distributed list of
// percentages based on the provided slice length. The percentages will sum up to 100.
func optionalMap(s string, items []string) []int {
	v := os.Getenv(s)
	if v == "" {
		// Calculate equal distribution
		count := len(items)
		if count == 0 {
			panic(fmt.Errorf("cannot create distribution for empty slice"))
		}

		result := make([]int, count)
		basePercentage := 100 / count
		remainder := 100 % count

		// Distribute base percentage to all items
		for i := range result {
			result[i] = basePercentage
		}

		// Distribute remaining percentage points one by one
		// to the first 'remainder' items to reach exactly 100
		for i := 0; i < remainder; i++ {
			result[i]++
		}

		return result
	}

	// Parse provided percentages
	return mustMap(s)
}

func mustProducerMode(s string) producerMode {
	v := strings.ToLower(strings.Trim(os.Getenv(s), " "))
	switch v {
	case "stdout":
		return modeStdout
	case "http":
		return modeHTTP
	case "http2":
		return modeHTTP2
	case "pulsar":
		return modePulsar
	default:
		panic(fmt.Errorf("producer mode out of the known domain: %s", s))
	}
}

func newProducer(slotName string, mode producerMode, useOneClientPerSlot bool) (publisherCloser, error) {
	switch mode {
	case modeHTTP:
		return producer.NewHTTPProducer(slotName, os.Environ())
	case modeHTTP2:
		return producer.NewHTTP2Producer(slotName, os.Environ())
	case modeStdout:
		return producer.NewStdoutPublisher(slotName), nil
	case modePulsar:
		if !useOneClientPerSlot {
			return nil, fmt.Errorf("pulsar mode requires useOneClientPerSlot to be true")
		}
		return producer.NewPulsarProducer(slotName, os.Environ())
	default:
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
}
