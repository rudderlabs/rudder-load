package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

type message struct {
	Payload     []byte
	AnonymousID string
}

type eventType struct {
	Type   string
	Values []int
}

func parseEvents(input string) []eventType {
	parts := strings.Split(input, ",")
	var events []eventType

	for _, part := range parts {
		if strings.Contains(part, "(") && strings.Contains(part, ")") {
			// Extract type and values
			split := strings.Split(part, "(")
			eventTypeStr := split[0]
			valuesStr := strings.TrimSuffix(split[1], ")")
			valuesStrList := strings.Split(valuesStr, ",")

			var values []int
			for _, v := range valuesStrList {
				val, _ := strconv.Atoi(v)
				values = append(values, val)
			}
			events = append(events, eventType{Type: eventTypeStr, Values: values})
		} else {
			// Just a type without values
			events = append(events, eventType{Type: part, Values: nil})
		}
	}

	return events
}

func getRudderEvent(tmpl *template.Template, loadRunID string) ([]byte, string) {
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
