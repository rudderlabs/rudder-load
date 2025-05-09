package main

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	dockerPulsar "github.com/rudderlabs/rudder-go-kit/testhelper/docker/resource/pulsar"
	"github.com/rudderlabs/rudder-go-kit/testhelper/httptest"
)

func TestHTTPIntegration(t *testing.T) {
	writeKey := "2lNXnjJU9xrbUERT3Uy3Po8jKbr"
	expectedAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(writeKey+":"))

	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		require.Equal(t, expectedAuthHeader, r.Header.Get("Authorization"))
		cancel() // cancelling the context to terminate the test. this makes sure at least one request is received.
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MODE", "http")
	t.Setenv("HOSTNAME", "rudder-load-0-baseline-test")
	t.Setenv("CONCURRENCY", "200")
	t.Setenv("MESSAGE_GENERATORS", "1")
	t.Setenv("MAX_EVENTS_PER_SECOND", "100000")
	t.Setenv("SOURCES", writeKey)
	t.Setenv("USE_ONE_CLIENT_PER_SLOT", "true")
	t.Setenv("ENABLE_SOFT_MEMORY_LIMIT", "true")
	t.Setenv("SOFT_MEMORY_LIMIT", "256mb")
	t.Setenv("TOTAL_USERS", "100000")
	t.Setenv("HOT_USER_GROUPS", "100")
	t.Setenv("EVENT_TYPES", "track,page,identify")
	t.Setenv("HOT_EVENT_TYPES", "33,33,34")
	t.Setenv("EVENT_TYPES", "track")
	t.Setenv("HOT_EVENT_TYPES", "100")
	t.Setenv("BATCH_SIZES", "1,2,3")
	t.Setenv("HOT_BATCH_SIZES", "40,30,30")
	t.Setenv("HTTP_COMPRESSION", "true")
	t.Setenv("HTTP_READ_TIMEOUT", "5s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "5s")
	t.Setenv("HTTP_MAX_IDLE_CONN", "1h")
	t.Setenv("HTTP_MAX_CONNS_PER_HOST", "5000")
	t.Setenv("HTTP_CONCURRENCY", "1000")
	t.Setenv("HTTP_CONTENT_TYPE", "application/json")
	t.Setenv("HTTP_ENDPOINT", srv.URL)
	t.Setenv("TEMPLATES_PATH", "./../../templates/")

	done := make(chan struct{})
	go func() {
		defer close(done)
		if exitCode := run(ctx); exitCode != 0 {
			t.Errorf("run exited with %d", exitCode)
		}
	}()
	<-done
}

func TestHTTP2Integration(t *testing.T) {
	writeKey := "2lNXnjJU9xrbUERT3Uy3Po8jKbr"
	expectedAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(writeKey+":"))

	ctx, cancel := context.WithCancel(context.Background())
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		require.Equal(t, expectedAuthHeader, r.Header.Get("Authorization"))
		cancel() // cancelling the context to terminate the test. this makes sure at least one request is received.
	})

	// Create a listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Create an HTTP server with the h2c handler
	server := &http.Server{
		Handler: h2c.NewHandler(handler, &http2.Server{}),
	}

	// Start the server in a goroutine
	go func() {
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("HTTP2 server error: %v", err)
		}
	}()

	// Clean up the server when the test is done
	t.Cleanup(func() {
		require.NoError(t, server.Close())
	})

	t.Setenv("MODE", "http2")
	t.Setenv("HOSTNAME", "rudder-load-0-baseline-test")
	t.Setenv("CONCURRENCY", "200")
	t.Setenv("MESSAGE_GENERATORS", "1")
	t.Setenv("MAX_EVENTS_PER_SECOND", "100000")
	t.Setenv("SOURCES", writeKey)
	t.Setenv("USE_ONE_CLIENT_PER_SLOT", "true")
	t.Setenv("ENABLE_SOFT_MEMORY_LIMIT", "true")
	t.Setenv("SOFT_MEMORY_LIMIT", "256mb")
	t.Setenv("TOTAL_USERS", "100000")
	t.Setenv("HOT_USER_GROUPS", "100")
	t.Setenv("EVENT_TYPES", "track")
	t.Setenv("HOT_EVENT_TYPES", "100")
	t.Setenv("BATCH_SIZES", "1,2,3")
	t.Setenv("HOT_BATCH_SIZES", "40,30,30")
	t.Setenv("HTTP2_COMPRESSION", "true")
	t.Setenv("HTTP2_TIMEOUT", "5s")
	t.Setenv("HTTP2_IDLE_CONN_TIMEOUT", "1h")
	t.Setenv("HTTP2_CONTENT_TYPE", "application/json")
	t.Setenv("HTTP2_ENDPOINT", "http://"+listener.Addr().String())
	t.Setenv("TEMPLATES_PATH", "./../../templates/")

	done := make(chan struct{})
	go func() {
		defer close(done)
		if exitCode := run(ctx); exitCode != 0 {
			t.Errorf("run exited with %d", exitCode)
		}
	}()
	<-done
}

func TestPulsarIntegration(t *testing.T) {
	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pool.MaxWait = time.Minute

	pulsarResource, err := dockerPulsar.Setup(pool, t)
	require.NoError(t, err)

	pulsarURL := pulsarResource.URL
	topic := "persistent://public/default/test-topic-integration"
	writeKey := "2lNXnjJU9xrbUERT3Uy3Po8jKbr"

	// Create a context that can be cancelled when we receive a message
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a consumer client
	consumerClient, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: pulsarURL,
	})
	require.NoError(t, err)
	defer consumerClient.Close()

	// Create a consumer
	consumer, err := consumerClient.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: "test-subscription",
		Type:             pulsar.Exclusive,
	})
	require.NoError(t, err)
	defer consumer.Close()

	// Set environment variables for the test
	t.Setenv("MODE", "pulsar")
	t.Setenv("HOSTNAME", "rudder-load-0-baseline-test")
	t.Setenv("CONCURRENCY", "200")
	t.Setenv("MESSAGE_GENERATORS", "1")
	t.Setenv("MAX_EVENTS_PER_SECOND", "100000")
	t.Setenv("SOURCES", writeKey)
	t.Setenv("USE_ONE_CLIENT_PER_SLOT", "true") // Required for Pulsar mode
	t.Setenv("ENABLE_SOFT_MEMORY_LIMIT", "true")
	t.Setenv("SOFT_MEMORY_LIMIT", "256mb")
	t.Setenv("TOTAL_USERS", "100000")
	t.Setenv("HOT_USER_GROUPS", "100")
	t.Setenv("EVENT_TYPES", "track")
	t.Setenv("HOT_EVENT_TYPES", "100")
	t.Setenv("BATCH_SIZES", "1,2,3")
	t.Setenv("HOT_BATCH_SIZES", "40,30,30")
	t.Setenv("PULSAR_URL", pulsarURL)
	t.Setenv("PULSAR_TOPIC", topic)
	t.Setenv("PULSAR_BATCHING_ENABLED", "false") // Disable batching for simpler testing
	t.Setenv("TEMPLATES_PATH", "./../../templates/")

	// Start a goroutine to receive messages
	messageReceived := make(chan struct{})
	go func() {
		defer close(messageReceived)
		// Set a timeout for receiving the message
		receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer receiveCancel()

		// Wait for a message
		msg, err := consumer.Receive(receiveCtx)
		if err != nil {
			t.Errorf("Failed to receive message: %v", err)
			return
		}

		// Acknowledge the message
		require.NoError(t, consumer.Ack(msg))

		// Cancel the context to terminate the test
		cancel()
	}()

	// Run the application
	done := make(chan struct{})
	go func() {
		defer close(done)
		if exitCode := run(ctx); exitCode != 0 {
			t.Errorf("run exited with %d", exitCode)
		}
	}()

	// Wait for the application to finish
	<-done
}

func TestPulsarIntegrationWithSlotNameAsTopic(t *testing.T) {
	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pool.MaxWait = time.Minute

	pulsarResource, err := dockerPulsar.Setup(pool, t)
	require.NoError(t, err)

	pulsarURL := pulsarResource.URL
	originalTopic := "persistent://public/default/will-be-replaced-by-slot-name"

	// Define the desired slotName
	// In main.go, slotName is constructed as loadRunID + "-" + i
	// With CONCURRENCY=2, two producers are created
	// So we set LOAD_RUN_ID to "custom-slot-name" to get slotNames of "custom-slot-name-0" and "custom-slot-name-1"
	loadRunID := "custom-slot-name"
	slotName0 := loadRunID + "-0" // First producer's slotName
	slotName1 := loadRunID + "-1" // Second producer's slotName

	// Extract namespace from the original topic and combine with slotNames
	// This mimics the behavior in pulsar.go's extractNamespaceFromTopic function
	namespace := "persistent://public/default/"
	expectedTopic0 := namespace + slotName0
	expectedTopic1 := namespace + slotName1

	// Create a context that can be cancelled when we receive messages from both topics
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a consumer client
	consumerClient, err := pulsar.NewClient(pulsar.ClientOptions{
		URL: pulsarURL,
	})
	require.NoError(t, err)
	defer consumerClient.Close()

	// Create consumers that subscribe to the expected topics (namespace + slotName)
	consumer0, err := consumerClient.Subscribe(pulsar.ConsumerOptions{
		Topic:            expectedTopic0,
		SubscriptionName: "test-subscription-slot-name-0",
		Type:             pulsar.Exclusive,
	})
	require.NoError(t, err)
	defer consumer0.Close()

	consumer1, err := consumerClient.Subscribe(pulsar.ConsumerOptions{
		Topic:            expectedTopic1,
		SubscriptionName: "test-subscription-slot-name-1",
		Type:             pulsar.Exclusive,
	})
	require.NoError(t, err)
	defer consumer1.Close()

	// Set environment variables for the test
	t.Setenv("MODE", "pulsar")
	t.Setenv("HOSTNAME", "rudder-load-0-baseline-test")
	t.Setenv("CONCURRENCY", "2") // Set to 2 to create two producers with different slotNames
	t.Setenv("MESSAGE_GENERATORS", "1")
	t.Setenv("MAX_EVENTS_PER_SECOND", "100000")
	t.Setenv("SOURCES", "2")
	t.Setenv("USE_ONE_CLIENT_PER_SLOT", "true") // Required for Pulsar mode
	t.Setenv("ENABLE_SOFT_MEMORY_LIMIT", "true")
	t.Setenv("SOFT_MEMORY_LIMIT", "256mb")
	t.Setenv("TOTAL_USERS", "100000")
	t.Setenv("HOT_USER_GROUPS", "100")
	t.Setenv("EVENT_TYPES", "track")
	t.Setenv("HOT_EVENT_TYPES", "100")
	t.Setenv("BATCH_SIZES", "1,2,3")
	t.Setenv("HOT_BATCH_SIZES", "40,30,30")
	t.Setenv("LOAD_RUN_ID", loadRunID) // Set LOAD_RUN_ID to control the slotName
	t.Setenv("PULSAR_URL", pulsarURL)
	t.Setenv("PULSAR_TOPIC", originalTopic)
	t.Setenv("PULSAR_USE_SLOT_NAME_AS_TOPIC", "true") // Enable using slotName as topic
	t.Setenv("PULSAR_BATCHING_ENABLED", "false")      // Disable batching for simpler testing
	t.Setenv("TEMPLATES_PATH", "./../../templates/")

	// Start goroutines to receive messages from both topics
	messagesReceived := sync.WaitGroup{}
	messagesReceived.Add(2) // We expect messages from both topics

	// Receive from the first topic
	go func() {
		defer messagesReceived.Done()
		// Set a timeout for receiving the message
		receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer receiveCancel()

		// Wait for a message
		msg, err := consumer0.Receive(receiveCtx)
		if err != nil {
			t.Errorf("Failed to receive message from topic 0: %v", err)
			return
		}

		// Verify that the slotName is set in the message properties
		require.Equal(t, slotName0, msg.Properties()["slotName"])

		// Acknowledge the message
		require.NoError(t, consumer0.Ack(msg))
	}()

	// Receive from the second topic
	go func() {
		defer messagesReceived.Done()
		// Set a timeout for receiving the message
		receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer receiveCancel()

		// Wait for a message
		msg, err := consumer1.Receive(receiveCtx)
		if err != nil {
			t.Errorf("Failed to receive message from topic 1: %v", err)
			return
		}

		// Verify that the slotName is set in the message properties
		require.Equal(t, slotName1, msg.Properties()["slotName"])

		// Acknowledge the message
		require.NoError(t, consumer1.Ack(msg))
	}()

	// Run the application
	done := make(chan struct{})
	go func() {
		defer close(done)
		if exitCode := run(ctx); exitCode != 0 {
			t.Errorf("run exited with %d", exitCode)
		}
	}()

	messagesReceived.Wait()
	// Cancel the context to terminate the test
	cancel()

	// Wait for the application to finish
	<-done
}
