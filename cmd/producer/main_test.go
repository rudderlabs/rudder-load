package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetSamples(t *testing.T) {
	samples, err := getSamples("./../../samples/")
	require.NoError(t, err)

	require.Contains(t, samples, "batch")
	require.Contains(t, samples, "page")

	t.Run("page", func(t *testing.T) {
		var buf bytes.Buffer
		err = samples["page"].Execute(&buf, map[string]string{
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
		err = samples["batch"].Execute(&buf, map[string]any{
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
		err = samples["batch"].Execute(&buf, map[string]any{
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
		err = samples["batch"].Execute(&buf, map[string]any{
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
