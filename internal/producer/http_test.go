package producer

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
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
			producer, err := NewHTTPProducer("test-slot", tt.env)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, producer)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, producer)
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
				require.Equal(t, "text/plain; charset=utf-8", r.Header.Get("Content-Type"))
				require.Equal(t, "test-key", r.Header.Get("X-Write-Key"))
				require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("write-key:")), r.Header.Get("Authorization"))
				require.Equal(t, "user123", r.Header.Get("AnonymousId"))
				require.Equal(t, "test-slot", r.Header.Get("X-SlotName"))
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
		{
			name:        "server error",
			compression: false,
			message:     []byte(`{"test":"data"}`),
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "test-slot", r.Header.Get("X-SlotName"))
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
		{
			name:        "with compression",
			compression: true,
			message:     []byte(`{"test":"data"}`),
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
				require.Equal(t, "test-slot", r.Header.Get("X-SlotName"))
				gz, err := gzip.NewReader(r.Body)
				require.NoError(t, err)
				body, err := io.ReadAll(gz)
				require.NoError(t, err, "body must be a complete gzip stream")
				require.Equal(t, `{"test":"data"}`, string(body))
				w.WriteHeader(http.StatusOK)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
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

			producer, err := NewHTTPProducer("test-slot", env)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, producer.Close())
			}()

			// Publish message
			response, err := producer.PublishTo(context.Background(), tt.key, tt.message, tt.extra)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if !tt.compression {
				require.NotNil(t, response)
			}
		})
	}
}

func TestHTTPProducer_Close(t *testing.T) {
	producer, err := NewHTTPProducer("test-slot", []string{"HTTP_ENDPOINT=http://localhost:8080"})
	require.NoError(t, err)

	err = producer.Close()
	require.NoError(t, err)
}

func TestHTTPProducer_ConnectionFailure(t *testing.T) {
	producer, err := NewHTTPProducer("test-slot", []string{"HTTP_ENDPOINT=http://localhost:12345"}) // Unlikely to be running
	require.NoError(t, err)

	_, err = producer.PublishTo(context.Background(), "key", []byte("test"), nil)
	require.Error(t, err)
}

func TestIsRetryable(t *testing.T) {
	publish := func(t *testing.T, endpoint string) error {
		t.Helper()
		p, err := NewHTTPProducer("test-slot", []string{"HTTP_ENDPOINT=" + endpoint, "HTTP_READ_TIMEOUT=200ms"})
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		_, err = p.PublishTo(context.Background(), "key", []byte("test"), nil)
		require.Error(t, err)
		return err
	}
	// listen accepts connections and hands each one to handle, until the test ends.
	listen := func(t *testing.T, handle func(net.Conn)) string {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				go handle(conn)
			}
		}()
		return "http://" + l.Addr().String()
	}

	t.Run("connection refused", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := l.Addr().String()
		require.NoError(t, l.Close())

		require.True(t, IsRetryable(publish(t, "http://"+addr)))
	})

	t.Run("server closes the connection before responding", func(t *testing.T) {
		endpoint := listen(t, func(conn net.Conn) {
			_, _ = conn.Read(make([]byte, 1024))
			_ = conn.Close()
		})

		err := publish(t, endpoint)
		require.ErrorIs(t, err, fasthttp.ErrConnectionClosed)
		require.True(t, IsRetryable(err))
	})

	t.Run("DNS lookup and dial timeouts", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "i/o timeout", Name: "ingestion.example", IsTimeout: true}
		require.True(t, IsRetryable(fmt.Errorf("http request failed: %w", dnsErr)))
		require.True(t, IsRetryable(fmt.Errorf("http request failed: %w", fasthttp.ErrDialTimeout)))
	})

	t.Run("read timeout after the request was sent", func(t *testing.T) {
		endpoint := listen(t, func(conn net.Conn) {
			_, _ = conn.Read(make([]byte, 1024))
			time.Sleep(time.Second) // never answers within the read timeout
			_ = conn.Close()
		})

		require.False(t, IsRetryable(publish(t, endpoint)))
	})

	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		require.False(t, IsRetryable(publish(t, srv.URL)))
	})

	t.Run("no error", func(t *testing.T) {
		require.False(t, IsRetryable(nil))
	})
}
