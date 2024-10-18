package main

import (
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

type eventGenerator func(t *template.Template, userID string, n []int) string

var eventGenerators = map[string]eventGenerator{
	"page":  pageFunc,
	"batch": batchFunc,
}

var (
	pageFunc eventGenerator = func(t *template.Template, userID string, n []int) string {
		return ""
	}
	batchFunc eventGenerator = func(t *template.Template, userID string, n []int) string {
		return ""
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
