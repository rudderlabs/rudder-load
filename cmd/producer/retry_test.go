package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rejectingListener closes the first connections it accepts after reading the start of their request, like
// ingestion pods going away, then accepts normally.
type rejectingListener struct {
	net.Listener
	toReject atomic.Int64
}

func (l *rejectingListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || l.toReject.Add(-1) < 0 {
			return conn, err
		}
		go func() {
			_, _ = conn.Read(make([]byte, 4096))
			_ = conn.Close()
		}()
	}
}

func TestRunRetriesConnectionErrors(t *testing.T) {
	tests := []struct {
		name       string
		maxRetries string
		wantDrops  bool
	}{
		{name: "retries them", maxRetries: "10", wantDrops: false},
		{name: "drops them with retries disabled", maxRetries: "0", wantDrops: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			l := &rejectingListener{Listener: raw}
			l.toReject.Store(50)
			var served atomic.Int64
			srv := &http.Server{
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					served.Add(1)
					w.WriteHeader(http.StatusOK)
				}),
				ReadHeaderTimeout: 5 * time.Second,
			}
			go func() { _ = srv.Serve(l) }()
			t.Cleanup(func() { _ = srv.Close() })

			env := map[string]string{
				"MODE":                     "http",
				"HOSTNAME":                 "rudder-load-0-retry-test",
				"CONCURRENCY":              "20",
				"MESSAGE_GENERATORS":       "2",
				"MAX_EVENTS_PER_SECOND":    "0",
				"SOURCES":                  "write-key-1",
				"USE_ONE_CLIENT_PER_SLOT":  "false", // one client shared by every slot
				"ENABLE_SOFT_MEMORY_LIMIT": "false",
				"SOFT_MEMORY_LIMIT":        "256mb",
				"TOTAL_USERS":              "1000",
				"HOT_USER_GROUPS":          "100",
				"EVENT_TYPES":              "track",
				"HOT_EVENT_TYPES":          "100",
				"BATCH_SIZES":              "1,2",
				"HOT_BATCH_SIZES":          "50,50",
				"HTTP_COMPRESSION":         "true",
				"HTTP_READ_TIMEOUT":        "5s",
				"HTTP_WRITE_TIMEOUT":       "5s",
				"HTTP_ENDPOINT":            "http://" + raw.Addr().String(),
				"HTTP_MAX_RETRIES":         tt.maxRetries,
				"TEMPLATES_PATH":           "./../../templates/",
			}
			for k, v := range env {
				t.Setenv(k, v)
			}

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int)
			go func() { done <- run(ctx) }()

			require.Eventually(t, func() bool {
				return l.toReject.Load() < 0 && served.Load() >= 200
			}, 10*time.Second, 10*time.Millisecond, "the server should reject 50 connections, then serve")
			metrics := scrapeMetrics(t)
			cancel()
			require.Equal(t, 0, <-done)

			retried := sumMetric(metrics, "rudder_load_retried_requests_count", "")
			dropped := sumMetric(metrics, "rudder_load_number_of_requests_count", `error="true"`)
			if tt.wantDrops {
				require.Positive(t, dropped)
				require.Zero(t, retried)
			} else {
				require.Positive(t, retried)
				require.Zero(t, dropped)
			}
		})
	}
}

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	resp, err := http.Get("http://127.0.0.1:9102/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// sumMetric adds up the samples of a metric whose labels contain labelFilter.
func sumMetric(metrics, name, labelFilter string) float64 {
	var total float64
	for _, line := range strings.Split(metrics, "\n") {
		if !strings.HasPrefix(line, name+"{") || !strings.Contains(line, labelFilter) {
			continue
		}
		fields := strings.Fields(line)
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err == nil {
			total += v
		}
	}
	return total
}
