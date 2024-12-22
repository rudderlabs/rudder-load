package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unsafe"

	"github.com/google/uuid"
)

var trackEventNames = []string{"event_1", "event_2", "event_3", "event_4", "event_5"}

type eventGenerator func(t *template.Template, userID, loadRunID string, n int, values []int, columns int) []byte

var eventGenerators = map[string]eventGenerator{
	"page":     pageFunc,
	"track":    trackFunc,
	"identify": identifyFunc,
}

const (
	letterBytes   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var (
	src = rand.NewSource(time.Now().UnixNano())
)

func RandString(n int) string {
	b := make([]byte, n)
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return *(*string)(unsafe.Pointer(&b)) // skipcq: GSC-G103
}

func additionalColumns(columns int) string {
	additionalColumnsMap := make(map[string]string, columns)
	for i := 0; i < columns; i++ {
		additionalColumnsMap[fmt.Sprintf("column_%d", rand.Intn(columns))] = RandString(12)
	}

	additionalPropertiesSlice := make([]string, 0, len(additionalColumnsMap))
	for k, v := range additionalColumnsMap {
		additionalPropertiesSlice = append(additionalPropertiesSlice, fmt.Sprintf(`"%s": "%s"`, k, v))
	}
	if len(additionalPropertiesSlice) > 0 {
		return strings.Join(additionalPropertiesSlice, ",") + ","
	}
	return ""
}

var (
	pageFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int, columns int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents":           n,
			"Name":                 "Home",
			"MessageID":            uuid.New().String(),
			"AnonymousID":          userID,
			"OriginalTimestamp":    time.Now().Format(time.RFC3339),
			"SentAt":               time.Now().Format(time.RFC3339),
			"LoadRunID":            loadRunID,
			"AdditionalProperties": additionalColumns(columns),
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute page template: %w", err))
		}
		return buf.Bytes()
	}

	trackFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int, columns int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents":           n,
			"UserID":               userID,
			"Event":                trackEventNames[rand.Intn(len(trackEventNames))],
			"Timestamp":            time.Now().Format(time.RFC3339),
			"LoadRunID":            loadRunID,
			"AdditionalProperties": additionalColumns(columns),
			"AdditionalContext":    additionalColumns(columns),
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute page template: %w", err))
		}
		return buf.Bytes()
	}

	identifyFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int, columns int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents":        n,
			"MessageID":         uuid.New().String(),
			"AnonymousID":       userID,
			"OriginalTimestamp": time.Now().Format(time.RFC3339),
			"SentAt":            time.Now().Format(time.RFC3339),
			"LoadRunID":         loadRunID,
			"AdditionalContext": additionalColumns(columns),
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute page template: %w", err))
		}
		return buf.Bytes()
	}

	eventTypesRegexp = regexp.MustCompile(`(\w+)(\(([\d,]+)\))?`)
)

type eventType struct {
	Type   string
	Values []int
}

func parseEventTypes(input string) ([]eventType, error) {
	matches := eventTypesRegexp.FindAllStringSubmatch(input, -1)
	events := make([]eventType, 0, len(matches))
	for _, match := range matches {
		et := match[1] // First group: the type (e.g., 'page', 'batch')
		var values []int
		if match[3] != "" { // Third group: the comma-separated numbers inside parentheses
			valuesSplit := strings.Split(match[3], ",")
			values = make([]int, 0, len(valuesSplit))
			for _, v := range valuesSplit {
				val, err := strconv.Atoi(v)
				if err != nil {
					return nil, err
				}
				values = append(values, val)
			}
		}
		events = append(events, eventType{Type: et, Values: values})
	}
	return events, nil
}

func getEventTypesConcentration(
	loadRunID string,
	eventTypes []eventType,
	hotEventTypes []int,
	eventGenerators map[string]eventGenerator,
	templates map[string]*template.Template,
	columns int,
) []func(userID string, n int) []byte {
	totalPercentage := 0
	for _, percentage := range hotEventTypes {
		totalPercentage += percentage
	}
	if totalPercentage != 100 {
		panic("hot event types percentages do not sum up to 100")
	}
	if len(eventTypes) != len(hotEventTypes) {
		panic("event types and hot event types must have the same length")
	}

	var (
		startID             = 0
		eventsConcentration = make([]func(string, int) []byte, 100)
	)
	for i, hotEventPercentage := range hotEventTypes {
		et := eventTypes[i]
		f := func(userID string, n int) []byte {
			return eventGenerators[et.Type](templates[et.Type], userID, loadRunID, n, et.Values, columns)
		}
		for i := startID; i < hotEventPercentage+startID; i++ {
			eventsConcentration[i] = f
		}
		startID += hotEventPercentage
	}

	return eventsConcentration
}
