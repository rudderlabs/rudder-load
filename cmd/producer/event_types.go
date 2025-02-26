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

	"github.com/google/uuid"
)

// TODO: make trackEventNames configurable
var trackEventNames = []string{"checkout", "view", "add_to_cart", "event_1", "event_2", "event_3"}

type eventGenerator func(t *template.Template, userID, loadRunID string, n int, values []int) []byte

var eventGenerators = map[string]eventGenerator{
	"page":     pageFunc,
	"track":    trackFunc,
	"identify": identifyFunc,
}

var (
	pageFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents":        n,
			"Name":              "Home",
			"MessageID":         uuid.New().String(),
			"AnonymousID":       userID,
			"OriginalTimestamp": time.Now().Format(time.RFC3339),
			"SentAt":            time.Now().Format(time.RFC3339),
			"LoadRunID":         loadRunID,
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute page template: %w", err))
		}
		return buf.Bytes()
	}

	trackFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents": n,
			"UserID":     userID,
			"Event":      trackEventNames[rand.Intn(len(trackEventNames))],
			"Timestamp":  time.Now().Format(time.RFC3339),
			"LoadRunID":  loadRunID,
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute page template: %w", err))
		}
		return buf.Bytes()
	}

	identifyFunc eventGenerator = func(t *template.Template, userID, loadRunID string, n int, _ []int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents":        n,
			"MessageID":         uuid.New().String(),
			"AnonymousID":       userID,
			"OriginalTimestamp": time.Now().Format(time.RFC3339),
			"SentAt":            time.Now().Format(time.RFC3339),
			"LoadRunID":         loadRunID,
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
			return eventGenerators[et.Type](templates[et.Type], userID, loadRunID, n, et.Values)
		}
		for i := startID; i < hotEventPercentage+startID; i++ {
			eventsConcentration[i] = f
		}
		startID += hotEventPercentage
	}

	return eventsConcentration
}

func registerCustomEventGenerators(eventTypes []eventType) {
	for _, eventType := range eventTypes {
		// check if the event type start with "custom"
		if !strings.HasPrefix(eventType.Type, "custom") {
			continue
		}

		// check if the event type is already registered
		if _, ok := eventGenerators[eventType.Type]; ok {
			continue
		}

		eventGenerators[eventType.Type] = createCustomEventGenerator(eventType.Type)
	}
}

func createCustomEventGenerator(eventType string) eventGenerator {
	return func(t *template.Template, userID, loadRunID string, n int, _ []int) []byte {
		var buf bytes.Buffer
		err := t.Execute(&buf, map[string]any{
			"NoOfEvents": n,
			"UserID":     userID,
			"Event":      eventType,
			"Timestamp":  time.Now().Format(time.RFC3339),
			"LoadRunID":  loadRunID,
		})
		if err != nil {
			panic(fmt.Errorf("cannot execute custom '%s' template: %w", eventType, err))
		}
		return buf.Bytes()
	}
}
