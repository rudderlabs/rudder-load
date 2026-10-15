package slack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type captured struct {
	auth, contentType string
	body              map[string]any
}

func newServer(t *testing.T, status int, answer string) (*Client, chan captured, *atomic.Int32) {
	t.Helper()
	got := make(chan captured, 1)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		raw, err := io.ReadAll(r.Body)
		if err == nil {
			err = json.Unmarshal(raw, &body)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		c := captured{auth: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: body}
		select {
		case got <- c:
		default:
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return New("xoxb-test", "C123", WithEndpoint(srv.URL)), got, &calls
}

func TestPostMessage(t *testing.T) {
	c, got, _ := newServer(t, http.StatusOK, `{"ok":true,"channel":"C123","ts":"1792072700.000100"}`)

	ts, err := c.PostMessage(context.Background(), "hello <@U1>", "1792072600.000200")
	require.NoError(t, err, "posting")
	require.Equal(t, "1792072700.000100", ts, "the new message's ts")
	req := <-got
	require.Equal(t, "Bearer xoxb-test", req.auth, "authorization header")
	require.Equal(t, "application/json; charset=utf-8", req.contentType, "content type")
	require.Equal(t, map[string]any{
		"channel": "C123", "text": "hello <@U1>", "thread_ts": "1792072600.000200",
		"unfurl_links": false, "unfurl_media": false,
	}, req.body, "request body")
}

func TestPostMessageTopLevel(t *testing.T) {
	c, got, _ := newServer(t, http.StatusOK, `{"ok":true,"ts":"1.2"}`)
	_, err := c.PostMessage(context.Background(), "hi", "")
	require.NoError(t, err, "posting")
	req := <-got
	_, hasThread := req.body["thread_ts"]
	require.False(t, hasThread, "a top-level message has no thread_ts")
}

func TestPostMessageFailures(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		answer        string
		wantNotPosted bool
	}{
		{name: "slack error", status: http.StatusOK, answer: `{"ok":false,"error":"channel_not_found"}`,
			wantNotPosted: true},
		{name: "rate limited", status: http.StatusTooManyRequests, answer: ``, wantNotPosted: true},
		{name: "4xx", status: http.StatusBadRequest, answer: ``, wantNotPosted: true},
		{name: "5xx", status: http.StatusBadGateway, answer: ``, wantNotPosted: false},
		{name: "not JSON", status: http.StatusOK, answer: `<html>`, wantNotPosted: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, calls := newServer(t, tt.status, tt.answer)
			_, err := c.PostMessage(context.Background(), "hi", "")
			require.Error(t, err, "the post fails")
			require.Equal(t, tt.wantNotPosted, isNotPosted(err), "whether it's certain nothing was posted")
			require.Equal(t, int32(1), calls.Load(), "chat.postMessage is never retried")
		})
	}
}

func TestPostMessageOKWithoutTS(t *testing.T) {
	c, _, _ := newServer(t, http.StatusOK, `{"ok":true,"ts":"not a ts"}`)
	ts, err := c.PostMessage(context.Background(), "hi", "")
	require.NoError(t, err, "the message was posted")
	require.Empty(t, ts, "an invalid ts is dropped")
}

func TestPostMessageTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	c := New("xoxb-test", "C123", WithEndpoint(srv.URL),
		WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))

	_, err := c.PostMessage(context.Background(), "hi", "")
	require.Error(t, err, "the post times out")
	require.False(t, isNotPosted(err), "a timeout leaves the outcome unknown")
}

func TestPostMessageConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	c := New("xoxb-test", "C123", WithEndpoint(u))

	_, err := c.PostMessage(context.Background(), "hi", "")
	require.Error(t, err, "nothing listens")
	require.True(t, isNotPosted(err), "a refused connection posts nothing")
}

func TestPostMessageWithACancelledContext(t *testing.T) {
	c, _, calls := newServer(t, http.StatusOK, `{"ok":true,"ts":"1.2"}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.PostMessage(ctx, "hi", "")
	require.ErrorIs(t, err, context.Canceled, "the post is cancelled")
	require.True(t, isNotPosted(err), "before anything was sent")
	require.Zero(t, calls.Load(), "Slack isn't called")
}

func TestPostMessageRejectsBadThread(t *testing.T) {
	c, _, calls := newServer(t, http.StatusOK, `{"ok":true,"ts":"1.2"}`)
	_, err := c.PostMessage(context.Background(), "hi", "1.2\nthread_ts=3")
	require.Error(t, err, "an invalid thread ts is rejected")
	require.True(t, isNotPosted(err), "nothing was posted")
	require.Zero(t, calls.Load(), "Slack isn't called")
}

func TestValidTS(t *testing.T) {
	require.True(t, ValidTS("1792072700.000100"), "a real ts")
	for _, s := range []string{"", "1792072700", "abc.def", "1.2\n", "1.2 "} {
		require.False(t, ValidTS(s), "rejects %q", s)
	}
}

func isNotPosted(err error) bool {
	return errors.Is(err, ErrNotPosted)
}
