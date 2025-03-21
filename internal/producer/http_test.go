package producer

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPProducer(t *testing.T) {
	tests := []struct {
		name    string
		env     []string
		wantErr bool
	}{
		{
			name: "valid configuration",
			env: []string{
				"HTTP_ENDPOINT=http://localhost:8080",
				"HTTP_CLIENT_TYPE=fasthttp",
				"HTTP_CONTENT_TYPE=application/json",
				"HTTP_KEY_HEADER=X-Write-Key",
			},
			wantErr: false,
		},
		{
			name: "valid configuration with compression",
			env: []string{
				"HTTP_ENDPOINT=http://localhost:8080",
				"HTTP_COMPRESSION=true",
			},
			wantErr: false,
		},
		{
			name: "invalid endpoint",
			env: []string{
				"HTTP_ENDPOINT=://example.com",
			},
			wantErr: true,
		},
		{
			name: "invalid client type",
			env: []string{
				"HTTP_ENDPOINT=http://localhost:8080",
				"HTTP_CLIENT_TYPE=invalid",
			},
			wantErr: true,
		},
		{
			name: "missing required endpoint",
			env: []string{
				"HTTP_CLIENT_TYPE=fasthttp",
			},
			wantErr: true,
		},
		{
			name: "invalid timeout format",
			env: []string{
				"HTTP_ENDPOINT=http://localhost:8080",
				"HTTP_READ_TIMEOUT=invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			producer, err := NewHTTPProducer(tt.env)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, producer)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, producer)
		})
	}
}

func TestHTTPProducer_PublishTo(t *testing.T) {
	tests := []struct {
		name          string
		compression   bool
		key           string
		keyHeader     string
		message       []byte
		extra         map[string]string
		serverStatus  int
		serverHandler func(w http.ResponseWriter, r *http.Request)
		wantErr       bool
	}{
		{
			name:        "successful publish",
			compression: false,
			key:         "test-key",
			keyHeader:   "X-Write-Key",
			message:     []byte(`{"test":"data"}`),
			extra: map[string]string{
				"auth":         "write-key",
				"anonymous_id": "user123",
			},
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Verify headers
				assert.Equal(t, "text/plain; charset=utf-8", r.Header.Get("Content-Type"))
				assert.Equal(t, "test-key", r.Header.Get("X-Write-Key"))
				assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("write-key:")), r.Header.Get("Authorization"))
				assert.Equal(t, "user123", r.Header.Get("AnonymousId"))
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name:        "server error",
			compression: false,
			message:     []byte(`{"test":"data"}`),
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, "internal error")
			},
			wantErr: true,
		},
		{
			name:        "with compression",
			compression: true,
			message:     []byte(`{"test":"data"}`),
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
			defer server.Close()

			// Create producer
			env := []string{
				"HTTP_ENDPOINT=" + server.URL,
				fmt.Sprintf("HTTP_COMPRESSION=%v", tt.compression),
			}
			if tt.keyHeader != "" {
				env = append(env, "HTTP_KEY_HEADER="+tt.keyHeader)
			}

			producer, err := NewHTTPProducer(env)
			require.NoError(t, err)
			defer producer.Close()

			// Publish message
			n, err := producer.PublishTo(context.Background(), tt.key, tt.message, tt.extra)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if !tt.compression {
				assert.Equal(t, len(tt.message), n)
			}
		})
	}
}

func TestHTTPProducer_Close(t *testing.T) {
	producer, err := NewHTTPProducer([]string{"HTTP_ENDPOINT=http://localhost:8080"})
	require.NoError(t, err)

	err = producer.Close()
	assert.NoError(t, err)
}

func TestHTTPProducer_ConnectionFailure(t *testing.T) {
	producer, err := NewHTTPProducer([]string{"HTTP_ENDPOINT=http://localhost:12345"}) // Unlikely to be running
	require.NoError(t, err)

	_, err = producer.PublishTo(context.Background(), "key", []byte("test"), nil)
	assert.Error(t, err)
}
