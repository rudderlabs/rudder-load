package main

import (
	"bytes"
	"os"
	"strconv"
	"testing"

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
		err := os.WriteFile(invalidTemplatePath, []byte("{{.InvalidSyntax}"), 0644)
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

//func TestGetEventTypesConcentration(t *testing.T) {
//	eventTypes := []eventType{
//		{Type: "page", Values: nil},
//		{Type: "batch", Values: []int{1, 2, 3}},
//	}
//	eventGenerators := map[string]eventGenerator{
//		"page": func(tmpl *template.Template, userID, loadRunID string, values []int) []byte {
//			return []byte(fmt.Sprintf("page-%s-%s-%+v", userID, loadRunID, values))
//		},
//		"batch": func(tmpl *template.Template, userID, loadRunID string, values []int) []byte {
//			return []byte(fmt.Sprintf("batch-%s-%s-%+v", userID, loadRunID, values))
//		},
//	}
//	templates := map[string]*template.Template{
//		"page":  nil,
//		"batch": nil,
//	}
//	eventsConcentration := getEventTypesConcentration("xxx", eventTypes, []int{50, 50}, eventGenerators, templates)
//	require.Len(t, eventsConcentration, 100)
//
//	repeat := 10000
//	for i := 0; i < repeat; i++ {
//		for k := 0; k < 50; k++ { // 1st group (0-49)
//			event := eventsConcentration[k]("123")
//			require.Equal(t, "page-123-xxx-[]", string(event))
//		}
//		for k := 50; k < 100; k++ { // 2nd group (50-99)
//			event := eventsConcentration[k]("123")
//			require.Equal(t, "batch-123-xxx-[1 2 3]", string(event))
//		}
//	}
//
//	for { // repeat until you get a page and then again until you get a batch
//		event := eventsConcentration[rand.Intn(100)]("123")
//		if string(event) == "page-123-xxx-[]" {
//			break
//		}
//	}
//	for { // repeat until you get a page and then again until you get a batch
//		event := eventsConcentration[rand.Intn(100)]("123")
//		if string(event) == "batch-123-xxx-[1 2 3]" {
//			break
//		}
//	}
//}
//
//func TestEventGenerators(t *testing.T) {
//	templates, err := getTemplates("./../../templates/")
//	require.NoError(t, err)
//
//	require.Contains(t, templates, "batch")
//	require.Contains(t, templates, "page")
//
//	t.Run("page", func(t *testing.T) {
//		data := pageFunc(templates["page"], "123", "456", nil)
//		t.Logf("page: %s", data)
//	})
//
//	// TODO update the tests
//	//t.Run("batch", func(t *testing.T) {
//	//	data := batchFunc(templates["batch"], "123", "456", []int{2, 3})
//	//	t.Logf("batch: %s", data)
//	//})
//}
