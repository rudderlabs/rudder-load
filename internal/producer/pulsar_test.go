package producer

import (
	"context"
	"testing"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/require"

	dockerPulsar "github.com/rudderlabs/rudder-go-kit/testhelper/docker/resource/pulsar"
)

func TestPulsarProducer(t *testing.T) {
	const receiveMsgTimeout = 10 * time.Second

	pool, err := dockertest.NewPool("")
	require.NoError(t, err)
	pool.MaxWait = time.Minute

	pulsarResource, err := dockerPulsar.Setup(pool, t, dockerPulsar.WithTag("3.3.6"))
	require.NoError(t, err)

	pulsarURL := pulsarResource.URL

	// Test the simplest setup
	t.Run("Simplest setup", func(t *testing.T) {
		// Create a topic name for testing
		topic := "test-topic-simple"

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

		// Create a producer using our PulsarProducer implementation
		producer, err := NewPulsarProducer("test-slot", []string{
			"PULSAR_URL=" + pulsarURL,
			"PULSAR_TOPIC=" + topic,
			"PULSAR_BATCHING_ENABLED=false",
		})
		require.NoError(t, err)
		defer func() {
			require.NoError(t, producer.Close())
		}()

		// Verify producer options
		require.Equal(t, topic, producer.producerOptions.Topic)
		require.True(t, producer.producerOptions.DisableBatching)
		require.Equal(t, pulsar.NoCompression, producer.producerOptions.CompressionType)

		// Send a message
		ctx := context.Background()
		message := []byte(`{"test":"data"}`)
		key := "test-key"
		extra := map[string]string{"property1": "value1"}

		_, err = producer.PublishTo(ctx, key, message, extra)
		require.NoError(t, err)

		// Receive the message
		ctx, cancel := context.WithTimeout(context.Background(), receiveMsgTimeout)
		msg, err := consumer.Receive(ctx)
		cancel()
		require.NoError(t, err)

		// Verify the message
		require.Equal(t, message, msg.Payload())
		require.Equal(t, key, msg.Key())
		require.Equal(t, "value1", msg.Properties()["property1"])
		require.Equal(t, "test-slot", msg.Properties()["slotName"])

		// Acknowledge the message
		require.NoError(t, consumer.Ack(msg))
	})

	// Test with compression enabled
	t.Run("Compression enabled", func(t *testing.T) {
		// Create a topic name for testing
		topic := "test-topic-compression"

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

		// Create a producer using our PulsarProducer implementation with compression
		producer, err := NewPulsarProducer("compression-slot", []string{
			"PULSAR_URL=" + pulsarURL,
			"PULSAR_TOPIC=" + topic,
			"PULSAR_COMPRESSION_TYPE=zstd",
		})
		require.NoError(t, err)
		defer func() {
			require.NoError(t, producer.Close())
		}()

		// Verify producer options
		require.Equal(t, topic, producer.producerOptions.Topic)
		require.False(t, producer.producerOptions.DisableBatching)
		require.Equal(t, pulsar.ZSTD, producer.producerOptions.CompressionType)

		// Send a message
		ctx := context.Background()
		message := []byte(`{"test":"data with compression"}`)
		key := "test-key-compression"

		_, err = producer.PublishTo(ctx, key, message, nil)
		require.NoError(t, err)

		// Receive the message
		ctx, cancel := context.WithTimeout(context.Background(), receiveMsgTimeout)
		defer cancel()

		msg, err := consumer.Receive(ctx)
		require.NoError(t, err)

		// Verify the message
		require.Equal(t, message, msg.Payload())
		require.Equal(t, key, msg.Key())
		require.Equal(t, "compression-slot", msg.Properties()["slotName"])

		// Acknowledge the message
		require.NoError(t, consumer.Ack(msg))
	})

	// Test with batching enabled
	t.Run("Batching enabled", func(t *testing.T) {
		// Create a topic name for testing
		topic := "test-topic-batching"

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

		// Create a producer using our PulsarProducer implementation with batching
		producer, err := NewPulsarProducer("batching-slot", []string{
			"PULSAR_URL=" + pulsarURL,
			"PULSAR_TOPIC=" + topic,
			"PULSAR_BATCHING_ENABLED=true",
			"PULSAR_BATCHING_MAX_MESSAGES=10",
			"PULSAR_BATCHING_MAX_PUBLISH_DELAY=100ms",
		})
		require.NoError(t, err)
		defer func() {
			require.NoError(t, producer.Close())
		}()

		// Verify producer options
		require.Equal(t, topic, producer.producerOptions.Topic)
		require.False(t, false, producer.producerOptions.DisableBatching)
		require.Equal(t, uint(10), producer.producerOptions.BatchingMaxMessages)
		require.Equal(t, 100*time.Millisecond, producer.producerOptions.BatchingMaxPublishDelay)
		require.Equal(t, pulsar.NoCompression, producer.producerOptions.CompressionType)

		// Send multiple messages
		ctx := context.Background()
		numMessages := 5

		for i := 0; i < numMessages; i++ {
			message := []byte(`{"test":"data with batching", "index":` + string(rune(i+'0')) + `}`)
			key := "test-key-batching-" + string(rune(i+'0'))

			_, err = producer.PublishTo(ctx, key, message, nil)
			require.NoError(t, err)
		}

		// Receive all messages
		for i := 0; i < numMessages; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), receiveMsgTimeout)
			msg, err := consumer.Receive(ctx)
			cancel()
			require.NoError(t, err)

			// Verify slotName is set in message properties
			require.Equal(t, "batching-slot", msg.Properties()["slotName"])

			// Acknowledge the message
			require.NoError(t, consumer.Ack(msg))
		}
	})

	// Test with slotName as topic explicitly enabled
	t.Run("SlotName as topic enabled", func(t *testing.T) {
		// Create a slotName to use as topic
		slotName := "slot-as-topic"

		// Create a fallback topic (should not be used)
		fallbackTopic := "fallback-topic"

		// Create a consumer client
		consumerClient, err := pulsar.NewClient(pulsar.ClientOptions{
			URL: pulsarURL,
		})
		require.NoError(t, err)
		defer consumerClient.Close()

		// Create a consumer that subscribes to the slotName as topic
		consumer, err := consumerClient.Subscribe(pulsar.ConsumerOptions{
			Topic:            slotName,
			SubscriptionName: "test-subscription",
			Type:             pulsar.Exclusive,
		})
		require.NoError(t, err)
		defer consumer.Close()

		// Create a producer using our PulsarProducer implementation with slotName as topic
		producer, err := NewPulsarProducer(slotName, []string{
			"PULSAR_URL=" + pulsarURL,
			"PULSAR_TOPIC=" + fallbackTopic,
			"PULSAR_USE_SLOT_NAME_AS_TOPIC=true",
		})
		require.NoError(t, err)
		defer func() {
			require.NoError(t, producer.Close())
		}()

		// Verify producer options
		require.Equal(t, slotName, producer.producerOptions.Topic)
		require.NotEqual(t, fallbackTopic, producer.producerOptions.Topic)

		// Send a message
		ctx := context.Background()
		message := []byte(`{"test":"data with slotName as topic"}`)
		key := "test-key-slot-topic"

		_, err = producer.PublishTo(ctx, key, message, nil)
		require.NoError(t, err)

		// Receive the message
		ctx, cancel := context.WithTimeout(context.Background(), receiveMsgTimeout)
		msg, err := consumer.Receive(ctx)
		cancel()
		require.NoError(t, err)

		// Verify the message
		require.Equal(t, message, msg.Payload())
		require.Equal(t, key, msg.Key())
		require.Equal(t, slotName, msg.Properties()["slotName"])

		// Acknowledge the message
		require.NoError(t, consumer.Ack(msg))
	})
}
