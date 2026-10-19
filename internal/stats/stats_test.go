package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

var errPublish = errors.New("publishing")

type fakePublisher struct {
	delay time.Duration
	err   error
}

func (p *fakePublisher) PublishTo(context.Context, string, []byte, map[string]string) ([]byte, error) {
	time.Sleep(p.delay)
	return nil, p.err
}

func (p *fakePublisher) Close() error {
	return nil
}

func TestPublishDurationSeries(t *testing.T) {
	f, err := NewFactory(prometheus.NewRegistry(), Data{Prefix: "test_", Concurrency: 3})
	require.NoError(t, err)

	// Three slots, each with its own client, as the producer runs with one client per slot
	_, err = f.New(&fakePublisher{}).PublishTo(t.Context(), "key", []byte("{}"), nil)
	require.NoError(t, err)
	_, err = f.New(&fakePublisher{}).PublishTo(t.Context(), "key", []byte("{}"), nil)
	require.NoError(t, err)
	_, err = f.New(&fakePublisher{err: errPublish}).PublishTo(t.Context(), "key", []byte("{}"), nil)
	require.ErrorIs(t, err, errPublish)

	require.Equal(t, 2, testutil.CollectAndCount(f.publishDurationSeconds),
		"the histogram has one series per outcome, whatever the number of slots")
}

func TestPublishDurationBuckets(t *testing.T) {
	reg := prometheus.NewRegistry()
	f, err := NewFactory(reg, Data{Prefix: "test_"})
	require.NoError(t, err)

	_, err = f.New(&fakePublisher{delay: 20 * time.Millisecond}).PublishTo(t.Context(), "key", []byte("{}"), nil)
	require.NoError(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	counts := make(map[float64]uint64)
	for _, mf := range mfs {
		if mf.GetName() != "test_publish_duration_seconds" {
			continue
		}
		require.Len(t, mf.GetMetric(), 1, "one publish, one series")
		for _, b := range mf.GetMetric()[0].GetHistogram().GetBucket() {
			counts[b.GetUpperBound()] = b.GetCumulativeCount()
		}
	}
	// The histogram records seconds, so its buckets must be in seconds too
	require.Contains(t, counts, 0.01, "a 10ms bucket")
	require.Contains(t, counts, 1.0, "a 1s bucket")
	require.Equal(t, uint64(0), counts[0.01], "a 20ms publish is above the 10ms bucket")
	require.Equal(t, uint64(1), counts[1], "a 20ms publish is within the 1s bucket")
}
