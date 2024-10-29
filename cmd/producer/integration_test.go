package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/rudderlabs/rudder-go-kit/testhelper/httptest"
)

func TestIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		cancel()
	}))
	t.Cleanup(srv.Close)

	t.Setenv("MODE", "http")
	t.Setenv("HOSTNAME", "rudder-load-0-test")
	t.Setenv("CONCURRENCY", "200")
	t.Setenv("MESSAGE_GENERATORS", "10")
	t.Setenv("SOURCES", "2lNXnjJU9xrbUERT3Uy3Po8jKbr")
	t.Setenv("USE_ONE_CLIENT_PER_SLOT", "true")
	t.Setenv("ENABLE_SOFT_MEMORY_LIMIT", "true")
	t.Setenv("SOFT_MEMORY_LIMIT", "256mb")
	t.Setenv("TOTAL_USERS", "100000")
	t.Setenv("HOT_USER_GROUPS", "100")
	t.Setenv("EVENT_TYPES", "track,page,identify")
	t.Setenv("HOT_EVENT_TYPES", "33,33,34")
	t.Setenv("EVENT_TYPES", "track")
	t.Setenv("HOT_EVENT_TYPES", "100")
	t.Setenv("BATCH_SIZES", "1,2,3")
	t.Setenv("HOT_BATCH_SIZES", "40,30,30")
	t.Setenv("HTTP_COMPRESSION", "true")
	t.Setenv("HTTP_READ_TIMEOUT", "5s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "5s")
	t.Setenv("HTTP_MAX_IDLE_CONN", "1h")
	t.Setenv("HTTP_MAX_CONNS_PER_HOST", "5000")
	t.Setenv("HTTP_CONCURRENCY", "1000")
	t.Setenv("HTTP_CONTENT_TYPE", "application/json")
	t.Setenv("HTTP_ENDPOINT", srv.URL)
	t.Setenv("TEMPLATES_PATH", "./../../templates/")

	done := make(chan struct{})
	go func() {
		defer close(done)
		if exitCode := run(ctx); exitCode != 0 {
			t.Errorf("run exited with %d", exitCode)
		}
	}()
	<-done
}
