package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

func TestGetTemplates(t *testing.T) {
	templates, err := getTemplates("./../../templates/")
	require.NoError(t, err)

	require.Contains(t, templates, "batch")
	require.Contains(t, templates, "page")

	t.Run("page", func(t *testing.T) {
		var buf bytes.Buffer
		err = templates["page"].Execute(&buf, map[string]string{
			"Name":              "Home",
			"MessageID":         "123",
			"AnonymousID":       "456",
			"OriginalTimestamp": "2021-01-01T00:00:00Z",
			"SentAt":            "2021-02-03T12:34:56Z",
			"LoadRunID":         "987",
		})
		require.NoError(t, err)
		require.JSONEq(t, `{
			"type": "page",
			"name": "Home",
			"properties": {
				"title": "Home | RudderStack",
				"url": "https://www.rudderstack.com"
			},
			"messageId": "123",
			"anonymousId": "456",
			"channel": "android-sdk",
			"context": {
				"load_run_id": "987",
				"app": {
					"build": "1",
					"name": "RudderAndroidClient",
					"namespace": "com.rudderlabs.android.sdk",
					"version": "1.0"
				}
			},
			"originalTimestamp": "2021-01-01T00:00:00Z",
			"sentAt": "2021-02-03T12:34:56Z"
		}`, buf.String())
	})
	t.Run("batch single page", func(t *testing.T) {
		var buf bytes.Buffer
		err = templates["batch"].Execute(&buf, map[string]any{
			"LoadRunID": "111222333",
			"Pages": []map[string]string{
				{
					"Name":              "Home",
					"MessageID":         "123",
					"AnonymousID":       "456",
					"OriginalTimestamp": "2021-01-01T00:00:00Z",
					"SentAt":            "2021-02-03T12:34:56Z",
				},
			},
		})
		require.NoError(t, err)
		require.JSONEq(t, `{
			"batch": [
				{
					"type": "page",
					"name": "Home",
					"properties": {
						"title": "Home | RudderStack",
						"url": "https://www.rudderstack.com"
					},
					"messageId": "123",
					"anonymousId": "456",
					"channel": "android-sdk",
					"context": {
						"load_run_id": "111222333",
						"app": {
							"build": "1",
							"name": "RudderAndroidClient",
							"namespace": "com.rudderlabs.android.sdk",
							"version": "1.0"
						}
					},
					"originalTimestamp": "2021-01-01T00:00:00Z",
					"sentAt": "2021-02-03T12:34:56Z"
				}
			]
		}`, buf.String())
	})
	t.Run("batch single track", func(t *testing.T) {
		var buf bytes.Buffer
		err = templates["batch"].Execute(&buf, map[string]any{
			"LoadRunID": "111222333",
			"Tracks": []map[string]string{
				{
					"UserID":    "123",
					"Event":     "some-event",
					"Timestamp": "2021-01-01T00:00:00Z",
				},
			},
		})
		require.NoError(t, err)
		require.JSONEq(t, `{
			"batch": [
				{
					"type": "track",
					"userId": "123",
					"event": "some-event",
					"properties": {
						"name": "Rubik's Cube",
						"revenue": 4.99
					},
					"context": {
						"load_run_id": "111222333",
						"ip": "14.5.67.21"
					},
					"timestamp": "2021-01-01T00:00:00Z"
				}
			]
		}`, buf.String())
	})
	t.Run("batch pages and tracks", func(t *testing.T) {
		var buf bytes.Buffer
		err = templates["batch"].Execute(&buf, map[string]any{
			"LoadRunID": "111222333",
			"Pages": []map[string]string{
				{
					"Name":              "Home1",
					"MessageID":         "123",
					"AnonymousID":       "456",
					"OriginalTimestamp": "2022-01-01T00:00:00Z",
					"SentAt":            "2023-02-03T12:34:56Z",
				},
				{
					"Name":              "Home2",
					"MessageID":         "124",
					"AnonymousID":       "457",
					"OriginalTimestamp": "2024-01-01T00:00:00Z",
					"SentAt":            "2025-02-03T12:34:56Z",
				},
			},
			"Tracks": []map[string]string{
				{
					"UserID":    "111",
					"Event":     "some-event1",
					"Timestamp": "2026-01-01T00:00:00Z",
				},
				{
					"UserID":    "222",
					"Event":     "some-event2",
					"Timestamp": "2027-01-01T00:00:00Z",
				},
			},
		})
		require.NoError(t, err)
		require.JSONEq(t, `{
			"batch": [
				{
					"type": "page",
					"name": "Home1",
					"properties": {
						"title": "Home | RudderStack",
						"url": "https://www.rudderstack.com"
					},
					"messageId": "123",
					"anonymousId": "456",
					"channel": "android-sdk",
					"context": {
						"load_run_id": "111222333",
						"app": {
							"build": "1",
							"name": "RudderAndroidClient",
							"namespace": "com.rudderlabs.android.sdk",
							"version": "1.0"
						}
					},
					"originalTimestamp": "2022-01-01T00:00:00Z",
					"sentAt": "2023-02-03T12:34:56Z"
				},
				{
					"type": "page",
					"name": "Home2",
					"properties": {
						"title": "Home | RudderStack",
						"url": "https://www.rudderstack.com"
					},
					"messageId": "124",
					"anonymousId": "457",
					"channel": "android-sdk",
					"context": {
						"load_run_id": "111222333",
						"app": {
							"build": "1",
							"name": "RudderAndroidClient",
							"namespace": "com.rudderlabs.android.sdk",
							"version": "1.0"
						}
					},
					"originalTimestamp": "2024-01-01T00:00:00Z",
					"sentAt": "2025-02-03T12:34:56Z"
				},
				{
					"type": "track",
					"userId": "111",
					"event": "some-event1",
					"properties": {
						"name": "Rubik's Cube",
						"revenue": 4.99
					},
					"context": {
						"load_run_id": "111222333",
						"ip": "14.5.67.21"
					},
					"timestamp": "2026-01-01T00:00:00Z"
				},
				{
					"type": "track",
					"userId": "222",
					"event": "some-event2",
					"properties": {
						"name": "Rubik's Cube",
						"revenue": 4.99
					},
					"context": {
						"load_run_id": "111222333",
						"ip": "14.5.67.21"
					},
					"timestamp": "2027-01-01T00:00:00Z"
				}
			]
		}`, buf.String())
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

func TestGetEventTypesConcentration(t *testing.T) {
	eventTypes := []eventType{
		{Type: "page", Values: nil},
		{Type: "batch", Values: []int{1, 2, 3}},
	}
	eventGenerators := map[string]eventGenerator{
		"page": func(tmpl *template.Template, userID, loadRunID string, values []int) []byte {
			return []byte(fmt.Sprintf("page-%s-%s-%+v", userID, loadRunID, values))
		},
		"batch": func(tmpl *template.Template, userID, loadRunID string, values []int) []byte {
			return []byte(fmt.Sprintf("batch-%s-%s-%+v", userID, loadRunID, values))
		},
	}
	templates := map[string]*template.Template{
		"page":  nil,
		"batch": nil,
	}
	eventsConcentration := getEventTypesConcentration("xxx", eventTypes, []int{50, 50}, eventGenerators, templates)
	require.Len(t, eventsConcentration, 100)

	repeat := 10000
	for i := 0; i < repeat; i++ {
		for k := 0; k < 50; k++ { // 1st group (0-49)
			event := eventsConcentration[k]("123")
			require.Equal(t, "page-123-xxx-[]", string(event))
		}
		for k := 50; k < 100; k++ { // 2nd group (50-99)
			event := eventsConcentration[k]("123")
			require.Equal(t, "batch-123-xxx-[1 2 3]", string(event))
		}
	}

	for { // repeat until you get a page and then again until you get a batch
		event := eventsConcentration[rand.Intn(100)]("123")
		if string(event) == "page-123-xxx-[]" {
			break
		}
	}
	for { // repeat until you get a page and then again until you get a batch
		event := eventsConcentration[rand.Intn(100)]("123")
		if string(event) == "batch-123-xxx-[1 2 3]" {
			break
		}
	}
}

func TestEventGenerators(t *testing.T) {
	templates, err := getTemplates("./../../templates/")
	require.NoError(t, err)

	require.Contains(t, templates, "batch")
	require.Contains(t, templates, "page")

	t.Run("page", func(t *testing.T) {
		data := pageFunc(templates["page"], "123", "456", nil)
		t.Logf("page: %s", data)
	})

	t.Run("batch", func(t *testing.T) {
		data := batchFunc(templates["batch"], "123", "456", []int{2, 3})
		t.Logf("batch: %s", data)
	})
}
