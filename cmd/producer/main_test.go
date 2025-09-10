package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetTemplates(t *testing.T) {
	t.Run("valid templates directory", func(t *testing.T) {
		templates, err := getTemplates("./../../templates/")
		require.NoError(t, err)

		require.Contains(t, templates, "page")
		require.Contains(t, templates, "track")
		require.Contains(t, templates, "identify")

		t.Run("page template execution", func(t *testing.T) {
			var buf bytes.Buffer
			err = templates["page"].Execute(&buf, map[string]any{
				"NoOfEvents":        1,
				"Name":              "TestPage",
				"MessageID":         "test-message-id",
				"AnonymousID":       "test-anonymous-id",
				"OriginalTimestamp": "2023-01-01T00:00:00Z",
				"SentAt":            "2023-01-01T00:00:00Z",
				"LoadRunID":         "test-load-run-id",
			})
			require.NoError(t, err)

			// Verify output contains expected values
			output := buf.String()
			require.Contains(t, output, "TestPage")
			require.Contains(t, output, "test-anonymous-id")
			require.Contains(t, output, "test-load-run-id")
		})

		t.Run("track template execution", func(t *testing.T) {
			var buf bytes.Buffer
			err = templates["track"].Execute(&buf, map[string]any{
				"NoOfEvents": 1,
				"UserID":     "test-user-id",
				"Event":      "test-event",
				"Timestamp":  "2023-01-01T00:00:00Z",
				"LoadRunID":  "test-load-run-id",
			})
			require.NoError(t, err)

			// Verify output contains expected values
			output := buf.String()
			require.Contains(t, output, "test-user-id")
			require.Contains(t, output, "test-event")
			require.Contains(t, output, "test-load-run-id")
		})

		t.Run("identify template execution", func(t *testing.T) {
			var buf bytes.Buffer
			err = templates["identify"].Execute(&buf, map[string]any{
				"NoOfEvents":        1,
				"MessageID":         "test-message-id",
				"AnonymousID":       "test-anonymous-id",
				"OriginalTimestamp": "2023-01-01T00:00:00Z",
				"SentAt":            "2023-01-01T00:00:00Z",
				"LoadRunID":         "test-load-run-id",
			})
			require.NoError(t, err)

			// Verify output contains expected values
			output := buf.String()
			require.Contains(t, output, "test-anonymous-id")
			require.Contains(t, output, "test-load-run-id")
		})
	})

	t.Run("non-existent directory", func(t *testing.T) {
		// Test with a non-existent directory
		templates, err := getTemplates("./non-existent-directory/")
		require.Error(t, err)
		require.Nil(t, templates)
		require.Contains(t, err.Error(), "cannot read templates directory")
	})

	t.Run("empty directory", func(t *testing.T) {
		// Create a temporary empty directory
		tempDir := t.TempDir()

		// Test with the empty directory
		templates, err := getTemplates(tempDir)
		require.NoError(t, err)
		require.Empty(t, templates)
	})

	t.Run("directory with invalid template", func(t *testing.T) {
		// Create a temporary directory
		tempDir := t.TempDir()

		// Create an invalid template file
		invalidTemplatePath := tempDir + "/invalid.json.tmpl"
		err := os.WriteFile(invalidTemplatePath, []byte("{{.InvalidSyntax}"), 0o644)
		require.NoError(t, err)

		// Test with the directory containing an invalid template
		templates, err := getTemplates(tempDir)
		require.Error(t, err)
		require.Nil(t, templates)
		require.Contains(t, err.Error(), "cannot parse template file")
	})
}

func TestParseEventTypes(t *testing.T) {
	t.Run("single event type", func(t *testing.T) {
		events, err := parseEventTypes("page")
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, "page", events[0].Type)
		require.Nil(t, events[0].Values)
	})
	t.Run("single event type with values", func(t *testing.T) {
		events, err := parseEventTypes("page(1,2,3)")
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, "page", events[0].Type)
		require.Equal(t, []int{1, 2, 3}, events[0].Values)
	})
	t.Run("multiple event types", func(t *testing.T) {
		events, err := parseEventTypes("page,batch(1,2,3)")
		require.NoError(t, err)
		require.Len(t, events, 2)
		require.Equal(t, "page", events[0].Type)
		require.Nil(t, events[0].Values)
		require.Equal(t, "batch", events[1].Type)
		require.Equal(t, []int{1, 2, 3}, events[1].Values)
	})
	t.Run("hyphenated event types", func(t *testing.T) {
		events, err := parseEventTypes("custom-track,track,page,identify")
		require.NoError(t, err)
		require.Len(t, events, 4)
		require.Equal(t, "custom-track", events[0].Type)
		require.Equal(t, "track", events[1].Type)
		require.Equal(t, "page", events[2].Type)
		require.Equal(t, "identify", events[3].Type)
	})
}

func TestGetUserIDs(t *testing.T) {
	userIDs := getUserIDsConcentration(1000, []int{50, 20, 20, 10}, false)
	require.Len(t, userIDs, 100)

	repeat := 10000
	for i := 0; i < repeat; i++ {
		for k := 0; k < 50; k++ { // 1st group (0-49)
			userID, err := strconv.Atoi(userIDs[k]())
			require.NoError(t, err)
			require.True(t, userID >= 0 && userID < 500, userID)
		}
		for k := 50; k < 70; k++ { // 2nd group (50-69)
			userID, err := strconv.Atoi(userIDs[k]())
			require.NoError(t, err)
			require.True(t, userID >= 500 && userID < 700, userID)
		}
		for k := 70; k < 90; k++ { // 3rd group (70-89)
			userID, err := strconv.Atoi(userIDs[k]())
			require.NoError(t, err)
			require.True(t, userID >= 700 && userID < 900, userID)
		}
		for k := 90; k < 100; k++ { // 4th group (90-99)
			userID, err := strconv.Atoi(userIDs[k]())
			require.NoError(t, err)
			require.True(t, userID >= 900 && userID < 1000, userID)
		}
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		wantExitCode int
		timeout      time.Duration
	}{
		{
			name: "valid configuration",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1",
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
			wantExitCode: 0,
			timeout:      2 * time.Second,
		},
		{
			name: "valid sources configuration",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-1-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1,write-key-2",
				"HOT_SOURCES":              "60,40", // Valid configuration
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
			wantExitCode: 0, // Should succeed with valid configuration
			timeout:      2 * time.Second,
		},
		{
			name: "invalid hostname",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "invalid-hostname",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "hostname without instance number",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "more batch sizes than hot batch sizes",
			env: map[string]string{
				"MODE":                  "http",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1,2",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "hostname with instance number higher than sources length",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-1-test",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 0,
			timeout:      1 * time.Second,
		},
		{
			name: "more event types than hot event types",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track,identify",
				"HOT_EVENT_TYPES":       "60", // Should have equal length as event types
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "invalid event types distribution",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "60", // Should sum to 100
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "hot user groups not sum to 100",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "60,30", // Should sum to 100
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "total users not multiple of hot user groups",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100", // Should be multiple of hot user groups
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "70,20,10",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "zero message generators",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "0",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100", // Should sum to 100
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "invalid concurrency",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "0",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
		{
			name: "no templates path",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-0-test",
				"CONCURRENCY":           "1",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
			wantExitCode: 0,
			timeout:      1 * time.Second,
		},
		{
			name: "wrong templates path",
			env: map[string]string{
				"MODE":                  "stdout",
				"HOSTNAME":              "rudder-load-test-0",
				"CONCURRENCY":           "1",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
				"TEMPLATES_PATH":        "invalid-templates-path",
			},
			wantExitCode: 1,
			timeout:      1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			// Run the function
			exitCode := run(ctx)
			require.Equal(t, tt.wantExitCode, exitCode)
		})
	}
}

func TestRunCancellation(t *testing.T) {
	// Setup valid environment
	env := map[string]string{
		"MODE":                     "stdout",
		"HOSTNAME":                 "rudder-load-0-test",
		"CONCURRENCY":              "2",
		"MESSAGE_GENERATORS":       "1",
		"TOTAL_USERS":              "100",
		"SOURCES":                  "write-key-1",
		"EVENT_TYPES":              "track",
		"HOT_EVENT_TYPES":          "100",
		"HOT_USER_GROUPS":          "100",
		"BATCH_SIZES":              "1",
		"HOT_BATCH_SIZES":          "100",
		"MAX_EVENTS_PER_SECOND":    "100",
		"SOFT_MEMORY_LIMIT":        "1GB",
		"TEMPLATES_PATH":           "../../templates/",
		"ENABLE_SOFT_MEMORY_LIMIT": "true",
	}

	for k, v := range env {
		t.Setenv(k, v)
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Run in goroutine
	done := make(chan int)
	go func() {
		done <- run(ctx)
	}()

	// Cancel after short delay
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Check if program exits gracefully
	select {
	case exitCode := <-done:
		require.Equal(t, 0, exitCode)
	case <-time.After(2 * time.Second):
		t.Fatal("run did not exit after cancellation")
	}
}

func TestMustSourcesList(t *testing.T) {
	t.Run("integer input", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "3")

		// Call the function
		sources := mustSourcesList("SOURCES")

		// Verify the result
		require.Len(t, sources, 3)
		require.Equal(t, "source-0", sources[0])
		require.Equal(t, "source-1", sources[1])
		require.Equal(t, "source-2", sources[2])
	})

	t.Run("list input", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "source-a,source-b,source-c")

		// Call the function
		sources := mustSourcesList("SOURCES")

		// Verify the result
		require.Len(t, sources, 3)
		require.Equal(t, "source-a", sources[0])
		require.Equal(t, "source-b", sources[1])
		require.Equal(t, "source-c", sources[2])
	})

	t.Run("empty input", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "")

		// Verify that the function panics
		require.Panics(t, func() {
			mustSourcesList("SOURCES")
		})
	})

	t.Run("zero integer input", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "0")

		// Verify that the function treats it as a list with one item
		sources := mustSourcesList("SOURCES")
		require.Len(t, sources, 1)
		require.Equal(t, "0", sources[0])
	})

	t.Run("negative integer input", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "-5")

		// Verify that the function treats it as a list with one item
		sources := mustSourcesList("SOURCES")
		require.Len(t, sources, 1)
		require.Equal(t, "-5", sources[0])
	})

	t.Run("list with empty source", func(t *testing.T) {
		// Set up environment
		t.Setenv("SOURCES", "source-a,,source-c")

		// Verify that the function panics
		require.Panics(t, func() {
			mustSourcesList("SOURCES")
		})
	})

	t.Run("non-existent environment variable", func(t *testing.T) {
		// Unset the environment variable
		t.Setenv("SOURCES", "")

		// Verify that the function panics
		require.Panics(t, func() {
			mustSourcesList("NON_EXISTENT_VAR")
		})
	})
}

func TestRunPanics(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "invalid mode",
			env: map[string]string{
				"MODE":                  "invalid",
				"HOSTNAME":              "rudder-load-0-test",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
		},
		{
			name: "empty write key in sources",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  ",,", // All sources are empty
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "1000",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
		},
		{
			name: "more hot sources than total sources",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1",
				"HOT_SOURCES":              "60,40", // Should panic: more hot sources than actual sources
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
		},
		{
			name: "hot sources percentages don't sum to 100",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-1-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1,write-key-2",
				"HOT_SOURCES":              "60,20", // Should panic: doesn't sum to 100
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
		},
		{
			name: "hot batch sizes don't sum to 100",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1",
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1,2",
				"HOT_BATCH_SIZES":          "80,10",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
		},
		{
			name: "missing hot user groups",
			env: map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "2",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "100",
				"SOURCES":                  "write-key-1",
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"BATCH_SIZES":              "1,2",
				"HOT_BATCH_SIZES":          "80,10",
				"MAX_EVENTS_PER_SECOND":    "100",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "true",
			},
		},
		{
			name: "missing mode",
			env: map[string]string{
				// mode is missing
				"HOSTNAME":              "rudder-load-0-test",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               "write-key-1",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
		},
		{
			name: "invalid sources with a comma",
			env: map[string]string{
				"MODE":                  "http",
				"HOSTNAME":              "rudder-load-0-test",
				"CONCURRENCY":           "2",
				"MESSAGE_GENERATORS":    "1",
				"TOTAL_USERS":           "100",
				"SOURCES":               ",",
				"EVENT_TYPES":           "track",
				"HOT_EVENT_TYPES":       "100",
				"HOT_USER_GROUPS":       "100",
				"BATCH_SIZES":           "1",
				"HOT_BATCH_SIZES":       "100",
				"MAX_EVENTS_PER_SECOND": "100",
				"SOFT_MEMORY_LIMIT":     "1GB",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			// Assert that the function panics
			require.Panics(t, func() {
				run(ctx)
			}, "Expected run() to panic for test case: %s", tt.name)
		})
	}
}

func TestMaxData(t *testing.T) {
	tests := []struct {
		name         string
		maxData      string
		timeout      time.Duration
		wantExitCode int
	}{
		{
			name:         "max data disabled (default)",
			maxData:      "",
			timeout:      500 * time.Millisecond,
			wantExitCode: 0,
		},
		{
			name:         "max data disabled (explicit zero)",
			maxData:      "0",
			timeout:      500 * time.Millisecond,
			wantExitCode: 0,
		},
		{
			name:         "max data enabled with small limit",
			maxData:      "1000",
			timeout:      2 * time.Second,
			wantExitCode: 0,
		},
		{
			name:         "max data enabled with very small limit",
			maxData:      "100",
			timeout:      2 * time.Second,
			wantExitCode: 0,
		},
		{
			name:         "invalid max data value",
			maxData:      "invalid",
			timeout:      500 * time.Millisecond,
			wantExitCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{
				"MODE":                     "stdout",
				"HOSTNAME":                 "rudder-load-0-test",
				"CONCURRENCY":              "1",
				"MESSAGE_GENERATORS":       "1",
				"TOTAL_USERS":              "10",
				"SOURCES":                  "write-key-1",
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"HOT_USER_GROUPS":          "100",
				"BATCH_SIZES":              "1",
				"HOT_BATCH_SIZES":          "100",
				"MAX_EVENTS_PER_SECOND":    "1000",
				"SOFT_MEMORY_LIMIT":        "1GB",
				"TEMPLATES_PATH":           "../../templates/",
				"ENABLE_SOFT_MEMORY_LIMIT": "false",
			}

			if tt.maxData != "" {
				env["MAX_DATA"] = tt.maxData
			}

			for k, v := range env {
				t.Setenv(k, v)
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			exitCode := run(ctx)
			require.Equal(t, tt.wantExitCode, exitCode)
		})
	}
}

func TestHostname(t *testing.T) {
	type testCase struct {
		name                   string
		hostname               string
		expectedDeploymentName string
		expectedInstanceNumber int
		expectedError          error
	}

	tests := []testCase{
		{
			name:                   "valid hostname with alphanumeric deployment name",
			hostname:               "rudder-load-0-68d8995d6c-9td2n",
			expectedDeploymentName: "68d8995d6c-9td2n",
			expectedInstanceNumber: 0,
			expectedError:          nil,
		},
		{
			name:                   "valid hostname with different instance number",
			hostname:               "rudder-load-42-deployment-name",
			expectedDeploymentName: "deployment-name",
			expectedInstanceNumber: 42,
			expectedError:          nil,
		},
		{
			name:                   "valid hostname with special characters in deployment name",
			hostname:               "rudder-load-7-special_chars.123!@#",
			expectedDeploymentName: "special_chars.123!@#",
			expectedInstanceNumber: 7,
			expectedError:          nil,
		},
		{
			name:                   "invalid hostname format - missing rudder-load prefix",
			hostname:               "invalid-0-deployment",
			expectedDeploymentName: "",
			expectedInstanceNumber: 0,
			expectedError:          fmt.Errorf("hostname is invalid: invalid-0-deployment"),
		},
		{
			name:                   "invalid hostname format - missing instance number",
			hostname:               "rudder-load-deployment",
			expectedDeploymentName: "",
			expectedInstanceNumber: 0,
			expectedError:          fmt.Errorf("hostname is invalid: rudder-load-deployment"),
		},
		{
			name:                   "invalid hostname format - non-numeric instance number",
			hostname:               "rudder-load-abc-deployment",
			expectedDeploymentName: "",
			expectedInstanceNumber: 0,
			expectedError:          fmt.Errorf("hostname is invalid: rudder-load-abc-deployment"),
		},
		{
			name:                   "invalid hostname format - empty deployment name",
			hostname:               "rudder-load-123-",
			expectedDeploymentName: "",
			expectedInstanceNumber: 0,
			expectedError:          fmt.Errorf("hostname is invalid: rudder-load-123-%s", ""),
		},
		{
			name:                   "empty hostname",
			hostname:               "",
			expectedDeploymentName: "",
			expectedInstanceNumber: 0,
			expectedError:          fmt.Errorf("hostname is invalid: %s", ""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deploymentName, instanceNumber, err := getHostname(tt.hostname)

			if tt.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, tt.expectedError.Error(), err.Error())
				require.Equal(t, 0, instanceNumber)
				require.Empty(t, deploymentName)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedDeploymentName, deploymentName)
				require.Equal(t, tt.expectedInstanceNumber, instanceNumber)
			}
		})
	}
}
