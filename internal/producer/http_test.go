package producer

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

func TestRetryReason(t *testing.T) {
	publish := func(t *testing.T, endpoint string, payload []byte, env ...string) error {
		t.Helper()
		p, err := NewHTTPProducer("test-slot", append([]string{"HTTP_ENDPOINT=" + endpoint}, env...))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		_, err = p.PublishTo(context.Background(), "key", payload, nil)
		require.Error(t, err)
		return err
	}
	// listen hands each accepted connection to handle, until the test ends.
	listen := func(t *testing.T, handle func(*net.TCPConn)) string {
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
				go handle(conn.(*net.TCPConn))
			}
		}()
		return "http://" + l.Addr().String()
	}
	readRequest := func(conn *net.TCPConn) {
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err == nil {
			_, _ = io.Copy(io.Discard, req.Body)
		}
	}
	requireReason := func(t *testing.T, want string, err error) {
		t.Helper()
		require.Equal(t, want, RetryReason(err))
		require.Equal(t, want != "", IsRetryable(err))
	}

	t.Run("connection refused", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		addr := l.Addr().String()
		require.NoError(t, l.Close())

		requireReason(t, "dial", publish(t, "http://"+addr, []byte("test")))
	})

	t.Run("dial timeout", func(t *testing.T) {
		requireReason(t, "dial", fmt.Errorf("http request failed: %w", fasthttp.ErrDialTimeout))
	})

	t.Run("DNS lookup timeout", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "i/o timeout", Name: "ingestion.example", IsTimeout: true}
		requireReason(t, "dns", fmt.Errorf("http request failed: %w", dnsErr))
		requireReason(t, "dns", &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}) // as net/http wraps it
	})

	t.Run("host not found", func(t *testing.T) {
		dnsErr := &net.DNSError{Err: "no such host", Name: "ingestion.example", IsNotFound: true}
		requireReason(t, "", fmt.Errorf("http request failed: %w", dnsErr))
		requireReason(t, "", &net.OpError{Op: "dial", Net: "tcp", Err: dnsErr}) // as net/http wraps it
	})

	t.Run("connection reset while sending the request", func(t *testing.T) {
		endpoint := listen(t, func(conn *net.TCPConn) {
			_ = conn.SetLinger(0) // reset rather than close
			_ = conn.Close()
		})

		// Bigger than the socket buffers, so the write is still going when the reset arrives.
		err := publish(t, endpoint, make([]byte, 32<<20))
		var opErr *net.OpError
		require.ErrorAs(t, err, &opErr)
		require.Equal(t, "write", opErr.Op)
		requireReason(t, "write", err)
	})

	t.Run("server closes the connection before responding", func(t *testing.T) {
		endpoint := listen(t, func(conn *net.TCPConn) {
			readRequest(conn)
			_ = conn.Close()
		})

		err := publish(t, endpoint, []byte("test"))
		require.ErrorIs(t, err, fasthttp.ErrConnectionClosed)
		requireReason(t, "connection_closed", err)
	})

	t.Run("connection reset after the response started", func(t *testing.T) {
		endpoint := listen(t, func(conn *net.TCPConn) {
			readRequest(conn)
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-"))
			_ = conn.SetLinger(0)
			_ = conn.Close()
		})

		err := publish(t, endpoint, []byte("test"))
		var opErr *net.OpError
		require.ErrorAs(t, err, &opErr)
		require.Equal(t, "read", opErr.Op)
		requireReason(t, "", err)
	})

	t.Run("read timeout after the request was sent", func(t *testing.T) {
		endpoint := listen(t, func(conn *net.TCPConn) {
			readRequest(conn)
			_, _ = io.Copy(io.Discard, conn) // never answers, until the client gives up
		})

		err := publish(t, endpoint, []byte("test"), "HTTP_READ_TIMEOUT=200ms")
		require.ErrorIs(t, err, fasthttp.ErrTimeout)
		requireReason(t, "", err)
	})

	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		err := publish(t, srv.URL, []byte("test"))
		require.ErrorContains(t, err, "status code: 500")
		requireReason(t, "", err)
	})

	t.Run("no error", func(t *testing.T) {
		requireReason(t, "", nil)
	})
}

// The response body must stay valid after PublishTo returns, when the response goes back to fasthttp's pool and
// other requests reuse it.
func TestHTTPProducer_PublishToResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, r.Body) // echoes the request
	}))
	t.Cleanup(srv.Close)
	p, err := NewHTTPProducer("test-slot", []string{"HTTP_ENDPOINT=" + srv.URL})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	var wg sync.WaitGroup
	var overwritten atomic.Int64
	for g := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				want := fmt.Sprintf("payload-%d-%d", g, i)
				rb, err := p.PublishTo(context.Background(), "key", []byte(want), nil)
				if err != nil {
					t.Errorf("publish: %v", err)
					return
				}
				time.Sleep(time.Millisecond) // other goroutines' responses reuse the pool meanwhile
				if string(rb) != want {
					overwritten.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	require.Zero(t, overwritten.Load(), "response bodies changed after PublishTo returned")
}
