package stats

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

var errPublish = errors.New("publishing")

type fakePublisher struct {
	err error
}

func (p *fakePublisher) PublishTo(context.Context, string, []byte, map[string]string) ([]byte, error) {
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
