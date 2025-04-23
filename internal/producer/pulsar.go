package producer

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
)

// PulsarProducer implements a producer for Apache Pulsar using the SendAsync API.
// It follows the same semantics and contracts as other producers in this package.
//
// Configuration is done via environment variables with the PULSAR_ prefix:
//
// Required settings:
// - PULSAR_SERVICE_URL: The Pulsar service URL (e.g., "pulsar://localhost:6650")
// - PULSAR_TOPIC: The topic to produce messages to
//
// Optional client settings:
// - PULSAR_OPERATION_TIMEOUT: Timeout for operations (default: 30s)
// - PULSAR_CONNECTION_TIMEOUT: Timeout for connections (default: 30s)
// - PULSAR_ENABLE_TLS: Enable TLS (default: false)
// - PULSAR_TLS_ALLOW_INSECURE: Allow insecure TLS connections (default: false)
// - PULSAR_TLS_TRUST_CERTS_FILE_PATH: Path to the TLS trust certificates file
//
// Optional producer settings:
// - PULSAR_BATCHING_ENABLED: Enable message batching (default: true)
// - PULSAR_BATCHING_MAX_MESSAGES: Maximum number of messages in a batch (default: 1000)
// - PULSAR_BATCHING_MAX_SIZE: Maximum size of a batch in bytes (default: 128KB)
// - PULSAR_BATCHING_MAX_PUBLISH_DELAY: Maximum delay for publishing a batch (default: 10ms)
// - PULSAR_COMPRESSION_TYPE: Compression type (none, zlib, lz4, zstd) (default: none)
type PulsarProducer struct {
	topic           string
	client          pulsar.Client
	producer        pulsar.Producer
	producerOptions pulsar.ProducerOptions
}

// NewPulsarProducer creates a new Pulsar producer with the given environment variables.
// It reads configuration from environment variables with the PULSAR_ prefix.
func NewPulsarProducer(environ []string) (*PulsarProducer, error) {
	conf, err := readConfiguration("PULSAR_", environ)
	if err != nil {
		return nil, fmt.Errorf("cannot read pulsar configuration: %v", err)
	}

	// Required settings
	pulsarURL, err := getRequiredStringSetting(conf, "url")
	if err != nil {
		return nil, err
	}

	topic, err := getRequiredStringSetting(conf, "topic")
	if err != nil {
		return nil, err
	}

	// Optional client settings
	operationTimeout, err := getOptionalDurationSetting(conf, "operation_timeout", 30*time.Second)
	if err != nil {
		return nil, err
	}

	connectionTimeout, err := getOptionalDurationSetting(conf, "connection_timeout", 30*time.Second)
	if err != nil {
		return nil, err
	}

	// Optional TLS settings
	enableTLS, err := getOptionalBoolSetting(conf, "enable_tls", false)
	if err != nil {
		return nil, err
	}

	// Create client options
	clientOptions := pulsar.ClientOptions{
		URL:               pulsarURL,
		OperationTimeout:  operationTimeout,
		ConnectionTimeout: connectionTimeout,
	}

	// Configure TLS if enabled
	if enableTLS {
		tlsAllowInsecure, err := getOptionalBoolSetting(conf, "tls_allow_insecure", false)
		if err != nil {
			return nil, err
		}

		clientOptions.TLSAllowInsecureConnection = tlsAllowInsecure

		tlsTrustCertsFilePath, err := getOptionalStringSetting(conf, "tls_trust_certs_file_path", "")
		if err != nil {
			return nil, err
		}

		if tlsTrustCertsFilePath != "" {
			clientOptions.TLSTrustCertsFilePath = tlsTrustCertsFilePath
		}
	}

	// Create client
	client, err := pulsar.NewClient(clientOptions)
	if err != nil {
		return nil, fmt.Errorf("could not create pulsar client: %v", err)
	}

	// Optional producer settings
	batchingEnabled, err := getOptionalBoolSetting(conf, "batching_enabled", true)
	if err != nil {
		return nil, err
	}

	batchingMaxMessages, err := getOptionalIntSetting(conf, "batching_max_messages", 1000)
	if err != nil {
		return nil, err
	}

	batchingMaxSize, err := getOptionalIntSetting(conf, "batching_max_size", 128*1024) // 128KB default
	if err != nil {
		return nil, err
	}

	batchingMaxPublishDelay, err := getOptionalDurationSetting(conf, "batching_max_publish_delay", 10*time.Millisecond)
	if err != nil {
		return nil, err
	}

	// Compression settings
	compressionType, err := getOptionalStringSetting(conf, "compression_type", "none")
	if err != nil {
		return nil, err
	}

	// Create producer options
	producerOptions := pulsar.ProducerOptions{
		Topic: topic,
	}

	// Set batching options if enabled
	if batchingEnabled {
		producerOptions.BatchingMaxPublishDelay = batchingMaxPublishDelay
		producerOptions.BatchingMaxMessages = uint(batchingMaxMessages)
		producerOptions.BatchingMaxSize = uint(batchingMaxSize)
	} else {
		producerOptions.DisableBatching = true
	}

	// Set compression type
	switch compressionType {
	case "zlib":
		producerOptions.CompressionType = pulsar.ZLib
	case "lz4":
		producerOptions.CompressionType = pulsar.LZ4
	case "zstd":
		producerOptions.CompressionType = pulsar.ZSTD
	default:
		producerOptions.CompressionType = pulsar.NoCompression
	}

	// Create producer
	producer, err := client.CreateProducer(producerOptions)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("could not create pulsar producer: %v", err)
	}

	return &PulsarProducer{
		client:          client,
		producer:        producer,
		topic:           topic,
		producerOptions: producerOptions,
	}, nil
}

// PublishTo sends a message to the Pulsar topic asynchronously.
// It uses the SendAsync API of the Pulsar client and waits for the operation to complete.
// The key is used as the message key, and extra map entries are added as message properties.
// It returns an empty byte array and nil error on success, or an error if the operation fails.
func (p *PulsarProducer) PublishTo(ctx context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	// Create message
	msg := pulsar.ProducerMessage{
		Payload: message,
		Key:     key,
	}

	// Add properties from extra map
	if len(extra) > 0 {
		properties := make(map[string]string)
		for k, v := range extra {
			properties[k] = v
		}
		msg.Properties = properties
	}

	// Create a channel to receive the result
	resultCh := make(chan error, 1)

	// Send message asynchronously
	p.producer.SendAsync(ctx, &msg, func(msgID pulsar.MessageID, producerMessage *pulsar.ProducerMessage, err error) {
		resultCh <- err
	})

	// Wait for the result or context cancellation
	select {
	case err := <-resultCh:
		if err != nil {
			return nil, fmt.Errorf("failed to publish message: %w", err)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Return empty response as we're using async API
	return nil, nil
}

// Close closes the Pulsar producer and client, releasing all resources.
// It should be called when the producer is no longer needed.
func (p *PulsarProducer) Close() error {
	p.producer.Close()
	p.client.Close()
	return nil
}
