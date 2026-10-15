package grafana

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL+"/", "test-token", WithBackoff(func(int) time.Duration { return 0 }))
	require.NoError(t, err, "creating the client")
	return c
}

// capture hands the requests a test server got to the test goroutine.
func capture(n int) (chan *http.Request, func(*http.Request)) {
	ch := make(chan *http.Request, n)
	return ch, func(r *http.Request) {
		select {
		case ch <- r.Clone(context.Background()):
		default:
		}
	}
}

func TestQuery(t *testing.T) {
	reqs, record := capture(1)
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"pod":"broker-0","version":"4.0.12.5"},"value":[1792072751.877,"1"]},` +
			`{"metric":{"pod":"broker-1","version":"4.0.12.5"},"value":[1792072751.877,"1792072712.245"]}]}}`))
	}))

	at := time.Unix(1792072751, 877_000_000)
	samples, err := c.Query(context.Background(), "uid-1", `up{a="b"}`, at)
	require.NoError(t, err, "running the query")
	r := <-reqs
	require.Equal(t, "/api/datasources/proxy/uid/uid-1/api/v1/query", r.URL.Path, "datasource proxy path")
	require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"), "authorization header")
	require.Equal(t, `up{a="b"}`, r.URL.Query().Get("query"), "query parameter")
	require.Equal(t, "1792072751.877", r.URL.Query().Get("time"), "time parameter")
	require.Len(t, samples, 2, "samples")
	require.Equal(t, map[string]string{"pod": "broker-0", "version": "4.0.12.5"}, samples[0].Labels, "labels")
	require.Equal(t, 1.0, samples[0].Value, "value")
	require.Equal(t, time.UnixMilli(1792072751877).UTC(), samples[0].Time, "sample time")
	require.Equal(t, time.UnixMilli(1792072712245).UTC(), SecondsToTime(samples[1].Value), "a timestamp() value")
}

func TestQueryRetries(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		wantCalls int32
	}{
		{name: "5xx is retried", status: http.StatusBadGateway, wantCalls: DefaultAttempts},
		{name: "429 is retried", status: http.StatusTooManyRequests, wantCalls: DefaultAttempts},
		{name: "4xx is not retried", status: http.StatusBadRequest, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("{\"status\":\"error\",\"error\":\"bad\nthing\"}"))
			}))
			_, err := c.Query(context.Background(), "uid", "up", time.Now())
			require.Error(t, err, "the query fails")
			require.NotContains(t, err.Error(), "\n", "the error stays on one line")
			require.Equal(t, tt.wantCalls, calls.Load(), "calls")
		})
	}

	t.Run("a retry that succeeds", func(t *testing.T) {
		var calls atomic.Int32
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
		}))
		samples, err := c.Query(context.Background(), "uid", "up", time.Now())
		require.NoError(t, err, "the second attempt succeeds")
		require.Empty(t, samples, "no samples")
		require.Equal(t, int32(2), calls.Load(), "calls")
	})
}

func TestQueryRejectsBadAnswers(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "error status", body: `{"status":"error","error":"boom"}`},
		{name: "matrix", body: `{"status":"success","data":{"resultType":"matrix","result":[]}}`},
		{name: "not JSON", body: `<html>`},
		{name: "bad value", body: `{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{},"value":[1,"x"]}]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			}))
			_, err := c.Query(context.Background(), "uid", "up", time.Now())
			require.Error(t, err, "the answer is rejected")
		})
	}
}

func TestLatestLog(t *testing.T) {
	reqs, record := capture(1)
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[` +
			`{"stream":{"a":"b"},"values":[["1792072693792743795","line"]]},` +
			`{"stream":{"a":"c"},"values":[["1792072697276175798","line"]]}]}}`))
	}))

	start, end := time.Unix(100, 0), time.Unix(200, 0)
	latest, ok, err := c.LatestLog(context.Background(), "logs", `{namespace="ns"}`, start, end)
	require.NoError(t, err, "querying Loki")
	require.True(t, ok, "a line was found")
	require.Equal(t, time.Unix(0, 1792072697276175798).UTC(), latest, "the newest line")
	r := <-reqs
	require.Equal(t, "/api/datasources/proxy/uid/logs/loki/api/v1/query_range", r.URL.Path, "path")
	got := map[string]string{}
	for k := range r.URL.Query() {
		got[k] = r.URL.Query().Get(k)
	}
	require.Equal(t, map[string]string{
		"query": `{namespace="ns"}`, "start": "100000000000", "end": "200000000000",
		"limit": "1", "direction": "backward",
	}, got, "query parameters")

	t.Run("no lines", func(t *testing.T) {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
		}))
		_, ok, err := c.LatestLog(context.Background(), "logs", `{namespace="ns"}`, start, end)
		require.NoError(t, err, "querying Loki")
		require.False(t, ok, "no line was found")
	})
}

func TestNew(t *testing.T) {
	for _, u := range []string{"", "example.com", "ftp://example.com", "https://"} {
		_, err := New(u, "token")
		require.Error(t, err, "rejects %q", u)
	}
}
