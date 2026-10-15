package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/rudderlabs/rudder-go-kit/httputil"
	"github.com/rudderlabs/rudder-go-kit/logger"
)

func TestOptionalTime(t *testing.T) {
	def := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	deadline := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	t.Run("unset", func(t *testing.T) {
		t.Setenv("RUN_DEADLINE", "") // restores the variable after the test
		require.NoError(t, os.Unsetenv("RUN_DEADLINE"))
		require.Equal(t, def, optionalTime("RUN_DEADLINE", def))
	})

	valid := []struct {
		name  string
		value string
		want  time.Time
	}{
		{name: "empty", value: "", want: def},
		{name: "UTC", value: "2026-10-16T12:00:00Z", want: deadline},
		{name: "with an offset", value: "2026-10-16T14:00:00+02:00", want: deadline},
		{name: "with fractional seconds", value: "2026-10-16T12:00:00.5Z", want: deadline.Add(500 * time.Millisecond)},
	}
	for _, tt := range valid {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RUN_DEADLINE", tt.value)
			got := optionalTime("RUN_DEADLINE", def)
			require.True(t, tt.want.Equal(got), "want %s, got %s", tt.want, got)
		})
	}

	invalid := []struct {
		name  string
		value string
	}{
		{name: "a duration", value: "24h"},
		{name: "a date", value: "2026-10-16"},
		{name: "no time zone", value: "2026-10-16T12:00:00"},
		{name: "a space for T", value: "2026-10-16 12:00:00Z"},
		{name: "a Unix timestamp", value: "1792152000"},
		{name: "surrounding spaces", value: " 2026-10-16T12:00:00Z "},
	}
	for _, tt := range invalid {
		t.Run("invalid: "+tt.name, func(t *testing.T) {
			t.Setenv("RUN_DEADLINE", tt.value)
			err := panicError(func() { optionalTime("RUN_DEADLINE", def) })
			var parseErr *time.ParseError
			require.ErrorAs(t, err, &parseErr, "it should panic with the parsing error")
			require.ErrorContains(t, err, "RUN_DEADLINE", "the error should name the variable")
		})
	}
}

// panicError calls f and returns the error it panics with, or nil if it doesn't panic with an error.
func panicError(f func()) (err error) {
	defer func() {
		if e, ok := recover().(error); ok {
			err = e
		}
	}()
	f()
	return nil
}

func TestWithRunDeadline(t *testing.T) {
	t.Run("without a deadline it returns ctx itself", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		log := &recordingLogger{Logger: logger.NOP}

		stopCtx, stop := withRunDeadline(ctx, log, time.Time{})
		stop()

		require.True(t, stopCtx == ctx, "without a deadline the load should stop on ctx alone, as before")
		require.NoError(t, ctx.Err(), "stop should leave ctx alone")
		require.Empty(t, log.messages())
	})

	t.Run("a deadline already past is reached at once", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		log := &recordingLogger{Logger: logger.NOP}

		stopCtx, stop := withRunDeadline(ctx, log, time.Now().Add(-time.Hour))
		require.ErrorIs(t, stopCtx.Err(), context.DeadlineExceeded, "the load should never start")
		require.True(t, isRunDeadlineReached(stopCtx))
		cancel()
		require.True(t, isRunDeadlineReached(stopCtx), "a later SIGTERM shouldn't change why the load stopped")

		stop() // waits for the goroutine that logs
		requireLogged(t, log, "Run deadline set", "Run deadline reached")
	})

	t.Run("a deadline is reached when it passes", func(t *testing.T) {
		log := &recordingLogger{Logger: logger.NOP}

		stopCtx, stop := withRunDeadline(context.Background(), log, time.Now().Add(50*time.Millisecond))
		require.Eventually(t, func() bool { return stopCtx.Err() != nil }, 10*time.Second, time.Millisecond,
			"the context should be done once the deadline passes")
		require.True(t, isRunDeadlineReached(stopCtx))

		stop()
		requireLogged(t, log, "Run deadline set", "Run deadline reached")
	})

	t.Run("SIGTERM before the deadline", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		log := &recordingLogger{Logger: logger.NOP}

		stopCtx, stop := withRunDeadline(ctx, log, time.Now().Add(time.Hour))
		require.NoError(t, stopCtx.Err())
		cancel()
		require.ErrorIs(t, stopCtx.Err(), context.Canceled, "SIGTERM should stop the load")
		require.False(t, isRunDeadlineReached(stopCtx))

		stop()
		requireLogged(t, log, "Run deadline set")
	})

	t.Run("stop before the deadline", func(t *testing.T) {
		log := &recordingLogger{Logger: logger.NOP}

		stopCtx, stop := withRunDeadline(context.Background(), log, time.Now().Add(time.Hour))
		stop()
		require.ErrorIs(t, stopCtx.Err(), context.Canceled)
		require.False(t, isRunDeadlineReached(stopCtx))
		requireLogged(t, log, "Run deadline set")
	})
}

// TestRunDeadline runs the producer with RUN_DEADLINE set. It waits for rudder_load_run_deadline_reached, which is set
// once the generators and publishers have all returned, so that what it checks after that can no longer change.
func TestRunDeadline(t *testing.T) {
	t.Run("a deadline already past at startup sends nothing", func(t *testing.T) {
		for _, mode := range []string{"http", "stdout"} {
			t.Run(mode, func(t *testing.T) {
				var served atomic.Int64
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					served.Add(1)
					w.WriteHeader(http.StatusOK)
				}))
				t.Cleanup(srv.Close)

				deployment := deadlineTestDeployment("past-" + mode)
				returned, sigterm := startRun(t, deadlineTestEnv(mode, deployment, srv.URL, time.Now().Add(-time.Hour)))

				requireLoadStopped(t, deployment)
				metrics, err := producerMetrics()
				require.NoError(t, err)
				ours := `deployment="` + deployment + `"`
				require.Zero(t, sumMetric(metrics, "rudder_load_published_messages_count", ours))
				require.Zero(t, sumMetric(metrics, "rudder_load_publish_messages_total", ours))
				require.Zero(t, served.Load(), "no request should reach the server")
				require.Never(t, returned.Load, 300*time.Millisecond, 10*time.Millisecond,
					"the producer should stay up, idle, until SIGTERM")
				require.Equal(t, 0, sigterm())
			})
		}
	})

	t.Run("stops sending once the deadline passes", func(t *testing.T) {
		var served atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			served.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		deployment := deadlineTestDeployment("passes")
		deadline := time.Now().Add(2 * time.Second)
		returned, sigterm := startRun(t, deadlineTestEnv("http", deployment, srv.URL, deadline))

		require.Eventually(t, func() bool { return served.Load() > 0 }, 10*time.Second, 10*time.Millisecond,
			"the producer should send requests before the deadline")
		requireLoadStopped(t, deployment)
		require.False(t, time.Now().Before(deadline), "the load should stop at the deadline, not before")

		sent := served.Load()
		require.Never(t, func() bool { return served.Load() != sent || returned.Load() }, 500*time.Millisecond,
			10*time.Millisecond, "the producer should send nothing more, and stay up until SIGTERM")
		metrics, err := producerMetrics()
		require.NoError(t, err)
		ours := `deployment="` + deployment + `"`
		require.EqualValues(t, sent, sumMetric(metrics, "rudder_load_published_messages_count", ours),
			"every request the server answered should be counted, the ones in flight at the deadline too")
		require.Zero(t, sumMetric(metrics, "rudder_load_number_of_requests_count", ours, `error="true"`))
		require.Zero(t, sumMetric(metrics, "rudder_load_publish_rate_per_second", ours),
			"an idle producer shouldn't report its last publish rate")
		require.Equal(t, 0, sigterm())
	})

	t.Run("starts no retry after the deadline", func(t *testing.T) {
		raw, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		deadline := time.Now().Add(2 * time.Second)
		// A connection dialled just before the deadline can be accepted just after it.
		l := &closingListener{Listener: raw, late: deadline.Add(100 * time.Millisecond)}
		srv := &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(l) }()
		t.Cleanup(func() { _ = srv.Close() })

		deployment := deadlineTestDeployment("retries")
		env := deadlineTestEnv("http", deployment, "http://"+raw.Addr().String(), deadline)
		env["HTTP_MAX_RETRIES"] = "10"
		env["MAX_EVENTS_PER_SECOND"] = "0"
		_, sigterm := startRun(t, env)

		requireLoadStopped(t, deployment)
		metrics, err := producerMetrics()
		require.NoError(t, err)
		require.Positive(t, sumMetric(metrics, "rudder_load_retried_requests_count", `deployment="`+deployment+`"`),
			"the producer should retry before the deadline")
		require.Zero(t, l.lateAccepted.Load(), "the producer should open no connection after the deadline")
		require.Equal(t, 0, sigterm())
	})

	t.Run("lets the requests in flight at the deadline finish", func(t *testing.T) {
		deadline := time.Now().Add(2 * time.Second)
		var arrived, answered, cancelled atomic.Int64
		// HTTP2 mode, unlike HTTP mode, would abort the request if its context were done.
		srv := httptest.NewServer(h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			arrived.Add(1)
			// Hold the request until after the deadline, so that it's in flight when the deadline passes.
			select {
			case <-r.Context().Done():
				cancelled.Add(1)
				return
			case <-time.After(time.Until(deadline) + 200*time.Millisecond):
			}
			answered.Add(1)
			w.WriteHeader(http.StatusOK)
		}), &http2.Server{}))
		t.Cleanup(srv.Close)

		deployment := deadlineTestDeployment("in-flight")
		env := deadlineTestEnv("http2", deployment, srv.URL, deadline)
		env["CONCURRENCY"] = "1" // a single publisher: its request in flight at the deadline is the only one
		env["MESSAGE_GENERATORS"] = "1"
		_, sigterm := startRun(t, env)

		require.Eventually(t, func() bool { return arrived.Load() == 1 }, 10*time.Second, 10*time.Millisecond,
			"the producer should send a request before the deadline")
		requireLoadStopped(t, deployment)

		require.Zero(t, cancelled.Load(), "the deadline shouldn't cut the request in flight short")
		require.EqualValues(t, 1, answered.Load())
		require.EqualValues(t, 1, arrived.Load(), "no request should start after the deadline")
		metrics, err := producerMetrics()
		require.NoError(t, err)
		ours := `deployment="` + deployment + `"`
		require.EqualValues(t, 1, sumMetric(metrics, "rudder_load_published_messages_count", ours),
			"the request in flight at the deadline should be counted as published")
		require.Zero(t, sumMetric(metrics, "rudder_load_number_of_requests_count", ours, `error="true"`))
		require.Equal(t, 0, sigterm())
	})
}

// recordingLogger records the messages logged at Info level.
type recordingLogger struct {
	logger.Logger
	mu    sync.Mutex
	infos []string
}

func (l *recordingLogger) Infon(msg string, _ ...logger.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, msg)
}

func (l *recordingLogger) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.infos...)
}

// requireLogged requires log to have logged one message per prefix, in order, each starting with it.
func requireLogged(t *testing.T, log *recordingLogger, prefixes ...string) {
	t.Helper()
	messages := log.messages()
	require.Len(t, messages, len(prefixes), "logged: %q", messages)
	for i, prefix := range prefixes {
		require.True(t, strings.HasPrefix(messages[i], prefix), "logged: %q", messages)
	}
}

// closingListener closes every connection it accepts after reading the start of its request, and counts those it
// accepts after late.
type closingListener struct {
	net.Listener
	late         time.Time
	lateAccepted atomic.Int64
}

func (l *closingListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return conn, err
		}
		if time.Now().After(l.late) {
			l.lateAccepted.Add(1)
		}
		go func() {
			_, _ = conn.Read(make([]byte, 4096))
			_ = conn.Close()
		}()
	}
}

// deadlineTestDeployment returns a deployment name of the test's own, so the metrics it reads on :9102 can't come
// from another process.
func deadlineTestDeployment(name string) string {
	return "deadline-test-" + strconv.Itoa(os.Getpid()) + "-" + name
}

// deadlineTestEnv returns the settings for a producer in mode publishing to endpoint, with RUN_DEADLINE at deadline.
func deadlineTestEnv(mode, deployment, endpoint string, deadline time.Time) map[string]string {
	return map[string]string{
		"MODE":                     mode,
		"HOSTNAME":                 "rudder-load-0-" + deployment,
		"CONCURRENCY":              "5",
		"MESSAGE_GENERATORS":       "2",
		"MAX_EVENTS_PER_SECOND":    "500",
		"SOURCES":                  "write-key-1",
		"USE_ONE_CLIENT_PER_SLOT":  "false",
		"ENABLE_SOFT_MEMORY_LIMIT": "false",
		"SOFT_MEMORY_LIMIT":        "256mb",
		"TOTAL_USERS":              "100",
		"HOT_USER_GROUPS":          "100",
		"EVENT_TYPES":              "track",
		"HOT_EVENT_TYPES":          "100",
		"BATCH_SIZES":              "1",
		"HOT_BATCH_SIZES":          "100",
		"STDOUT_DISCARD":           "true",
		"HTTP_ENDPOINT":            endpoint,
		"HTTP_READ_TIMEOUT":        "5s",
		"HTTP_WRITE_TIMEOUT":       "5s",
		"HTTP2_ENDPOINT":           endpoint,
		"HTTP2_TIMEOUT":            "10s",
		"TEMPLATES_PATH":           "./../../templates/",
		"RUN_DEADLINE":             deadline.Format(time.RFC3339Nano),
	}
}

// startRun sets env and runs the producer in the background, as main does. sigterm cancels its context, as SIGTERM
// does, and returns its exit code; returned reports whether run has returned. The test's cleanup calls sigterm too,
// so that a failed test doesn't leave the producer holding :9102.
func startRun(t *testing.T, env map[string]string) (returned *atomic.Bool, sigterm func() int) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned = &atomic.Bool{}
	done := make(chan int, 1)
	go func() {
		exitCode := run(ctx)
		returned.Store(true)
		done <- exitCode
	}()

	var (
		once     sync.Once
		exitCode int
	)
	sigterm = func() int {
		once.Do(func() {
			cancel()
			select {
			case exitCode = <-done:
			case <-time.After(10 * time.Second):
				exitCode = -1 // run didn't return after SIGTERM
			}
		})
		return exitCode
	}
	t.Cleanup(func() { sigterm() })
	return returned, sigterm
}

// requireLoadStopped waits for the producer of deployment to set rudder_load_run_deadline_reached.
func requireLoadStopped(t *testing.T, deployment string) {
	t.Helper()
	ours := `deployment="` + deployment + `"`
	require.Eventually(t, func() bool {
		metrics, err := producerMetrics()
		return err == nil && sumMetric(metrics, "rudder_load_run_deadline_reached", ours) == 1
	}, 10*time.Second, 10*time.Millisecond, "the producer should stop at the deadline")
}

// producerMetrics returns the producer's metrics from :9102. Unlike scrapeMetrics it doesn't use t, so that
// require.Eventually can call it.
func producerMetrics() (string, error) {
	resp, err := http.Get("http://127.0.0.1:9102/metrics")
	if err != nil {
		return "", err
	}
	defer func() { httputil.CloseResponse(resp) }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
