package pulsarsoak

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStateRoundTrip(t *testing.T) {
	_, st := running(t, testConfig())
	st.Candidate = &Candidate{Version: v3, Since: tStart.Add(time.Hour)}
	data, err := st.Encode()
	require.NoError(t, err, "encoding")
	require.Contains(t, string(data), `"length": "24h0m0s"`, "durations are readable")
	require.NotContains(t, string(data), "wk-secret", "the state holds no write keys")

	got, err := DecodeState(data)
	require.NoError(t, err, "decoding")
	require.Equal(t, st, got, "the same state")
}

func TestDecodeState(t *testing.T) {
	st, err := DecodeState(nil)
	require.NoError(t, err, "no data")
	require.Equal(t, State{Schema: StateSchema}, st, "is the empty state")

	_, err = DecodeState([]byte(`{"schema":1,"surprise":true}`))
	require.Error(t, err, "unknown fields are an error, not data loss")
	_, err = DecodeState([]byte(`{"schema":2}`))
	require.ErrorContains(t, err, "schema 2", "another schema")
	_, err = DecodeState([]byte(`{"schema":1,"run":{"length":3}}`))
	require.Error(t, err, "a length that isn't a duration string")
}

func TestStateCloneIsDeep(t *testing.T) {
	_, st := running(t, testConfig())
	c := st.Clone()
	c.Run.Posts[MsgBrief] = Post{Status: PostPosted}
	c.Run.Phase = PhaseStopped
	require.False(t, st.Run.Attempted(MsgBrief), "the original's posts are untouched")
	require.Equal(t, PhaseRunning, st.Run.Phase, "and its run")
	require.True(t, strings.HasPrefix(st.Run.ID, "pulsar-soak-test-"), "run IDs say what they are")
}
