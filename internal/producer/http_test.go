package producer

import (
	"bytes"
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
				_, _ = fmt.Fprint(w, "internal error")
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
			defer func() { _ = producer.Close() }()

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

func TestValidator(t *testing.T) {
	tests := []struct {
		name          string
		serverStatus  int
		serverBody    string
		customHeader  string
		validator     func(headers map[string]string, statusCode int, body []byte) error
		expectError   bool
		errorContains string
	}{
		{
			name:         "successful validation with 201 status",
			serverStatus: http.StatusCreated,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if statusCode != http.StatusCreated {
					return fmt.Errorf("expected status code 201, got %d", statusCode)
				}
				return nil
			},
			expectError: false,
		},
		{
			name:         "failed validation with non-201 status",
			serverStatus: http.StatusBadRequest,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if statusCode != http.StatusCreated {
					return fmt.Errorf("expected status code 201, got %d", statusCode)
				}
				return nil
			},
			expectError:   true,
			errorContains: "expected status code 201, got 400",
		},
		{
			name:         "validation based on response body success",
			serverStatus: http.StatusOK,
			serverBody:   `"success"`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if !bytes.Contains(body, []byte("success")) {
					return fmt.Errorf("response does not contain success message")
				}
				return nil
			},
			expectError: false,
		},
		{
			name:         "validation based on response body failure",
			serverStatus: http.StatusOK,
			serverBody:   `"operation failed"`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if !bytes.Contains(body, []byte("success")) {
					return fmt.Errorf("response does not contain success message")
				}
				return nil
			},
			expectError:   true,
			errorContains: "does not contain success message",
		},
		{
			name:         "validation checks for content type",
			serverStatus: http.StatusOK,
			serverBody:   `{"success":true}`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				foundHeader := false
				for k, v := range headers {
					if k == "content-type" && v == "application/json" {
						foundHeader = true
						break
					}
				}
				if !foundHeader {
					return fmt.Errorf("missing or invalid Content-Type header")
				}
				return nil
			},
			expectError: false,
		},
		{
			name:         "validation for missing header",
			serverStatus: http.StatusOK,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				foundHeader := false
				for k, v := range headers {
					if k == "x-custom-header" && v == "not foo-bar" {
						foundHeader = true
						break
					}
				}
				if !foundHeader {
					return fmt.Errorf("x-custom-header not foo-bar")
				}
				return nil
			},
			expectError:   true,
			errorContains: "x-custom-header not foo-bar",
		},
		{
			name:         "validation for custom header",
			serverStatus: http.StatusOK,
			customHeader: "foo-bar",
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				foundHeader := false
				for k, v := range headers {
					if k == "x-custom-header" && v == "foo-bar" {
						foundHeader = true
						break
					}
				}
				if !foundHeader {
					return fmt.Errorf("x-custom-header is not foo-bar")
				}
				return nil
			},
			expectError: false,
		},
		{
			name:         "combined validation of status code and body",
			serverStatus: http.StatusCreated,
			serverBody:   `{"id":"123","status":"created"}`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if statusCode != http.StatusCreated {
					return fmt.Errorf("expected status code 201, got %d", statusCode)
				}
				if !bytes.Contains(body, []byte("id")) {
					return fmt.Errorf("response missing id field")
				}
				return nil
			},
			expectError: false,
		},
		{
			name:         "nil validator should not cause errors",
			serverStatus: http.StatusOK,
			serverBody:   `{"success":true}`,
			validator:    nil,
			expectError:  false,
		},
		{
			name:         "validation with 5xx server error",
			serverStatus: http.StatusInternalServerError,
			serverBody:   `{"error":"internal server error"}`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if statusCode >= 500 && statusCode < 600 {
					return fmt.Errorf("server error: status code %d", statusCode)
				}
				return nil
			},
			expectError:   true,
			errorContains: "server error: status code 500",
		},
		{
			name:         "validation with 4xx client error",
			serverStatus: http.StatusForbidden,
			serverBody:   `{"error":"access denied"}`,
			validator: func(headers map[string]string, statusCode int, body []byte) error {
				if statusCode >= 400 && statusCode < 500 {
					return fmt.Errorf("client error: status code %d", statusCode)
				}
				return nil
			},
			expectError:   true,
			errorContains: "client error: status code 403",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Custom-Header", tt.customHeader)
				w.WriteHeader(tt.serverStatus)
				_, _ = w.Write([]byte(tt.serverBody))
			}))
			defer server.Close()

			// Create producer with validator
			env := []string{"HTTP_ENDPOINT=" + server.URL}
			producer, err := NewHTTPProducer(env, WithValidate(tt.validator))
			require.NoError(t, err)
			defer func() { _ = producer.Close() }()

			// Send request
			_, err = producer.PublishTo(context.Background(), "test-key", []byte(`{"test":"data"}`), nil)

			// Check results
			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
