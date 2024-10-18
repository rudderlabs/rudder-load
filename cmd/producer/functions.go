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

	"github.com/google/uuid"
)

type message struct {
	Payload []byte
	UserID  string
}

func getTemplates(templatesPath string) (map[string]*template.Template, error) {
	files, err := os.ReadDir(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read templates directory: %w", err)
	}

	funcMap := template.FuncMap{
		"Sub1": func(n int) int { return n - 1 },
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
	v := strings.Split(os.Getenv(s), ",")
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
