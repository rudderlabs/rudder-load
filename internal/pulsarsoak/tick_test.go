package pulsarsoak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"rudder-load/internal/pulsarsoak/helm"
	"rudder-load/internal/pulsarsoak/kube"
	"rudder-load/internal/pulsarsoak/slack"
)

// fakeStore keeps the state in memory with versions, like the Secret.
type fakeStore struct {
	data    []byte
	version int
	saves   int
	// failSave fails the save with this number, counting from 1.
	failSave map[int]error
}

func (s *fakeStore) Load(context.Context) ([]byte, string, error) {
	if s.version == 0 {
		return nil, "", nil
	}
	return bytes.Clone(s.data), strconv.Itoa(s.version), nil
}

func (s *fakeStore) Save(ctx context.Context, data []byte, version string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.saves++
	if err := s.failSave[s.saves]; err != nil {
		return "", err
	}
	current := ""
	if s.version > 0 {
		current = strconv.Itoa(s.version)
	}
	if version != current {
		return "", kube.ErrConflict
	}
	s.version++
	s.data = bytes.Clone(data)
	return strconv.Itoa(s.version), nil
}

type sentMessage struct {
	text, thread, ts string
}

// fakeSlack shows every message it gets, unless the next result is ErrNotPosted. Any other result error means the
// message was shown but its answer was lost.
type fakeSlack struct {
	shown    []sentMessage
	attempts int
	results  []error
	// onPost, if set, runs while a message is being posted.
	onPost func()
}

func (f *fakeSlack) PostMessage(_ context.Context, text, threadTS string) (string, error) {
	f.attempts++
	if f.onPost != nil {
		f.onPost()
	}
	var err error
	if len(f.results) > 0 {
		err, f.results = f.results[0], f.results[1:]
	}
	if errors.Is(err, slack.ErrNotPosted) {
		return "", err
	}
	ts := fakeTS(len(f.shown) + 1)
	f.shown = append(f.shown, sentMessage{text: text, thread: threadTS, ts: ts})
	if err != nil {
		return "", err
	}
	return ts, nil
}

type fakeHelm struct {
	installs     []helm.Install
	uninstalls   int
	installed    bool
	installErr   error
	uninstallErr error
}

func (f *fakeHelm) UpgradeInstall(_ context.Context, in helm.Install) error {
	f.installs = append(f.installs, in)
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = true
	return nil
}

func (f *fakeHelm) Uninstall(_ context.Context, release, namespace string) error {
	f.uninstalls++
	if f.uninstallErr != nil {
		return f.uninstallErr
	}
	f.installed = false
	return nil
}

type observerFunc func(context.Context, time.Time) Observations

func (f observerFunc) Observe(ctx context.Context, now time.Time) Observations { return f(ctx, now) }

// harness runs ticks against fakes.
type harness struct {
	t        *testing.T
	cfg      Config
	store    *fakeStore
	slack    *fakeSlack
	helm     *fakeHelm
	obs      Observations
	now      time.Time
	brief    string
	briefErr error
	threads  []string
	// ctx is the tick's context, Background when nil.
	ctx context.Context
}

func newHarness(t *testing.T) *harness {
	return &harness{
		t: t, cfg: testConfig(), store: &fakeStore{failSave: map[int]error{}}, slack: &fakeSlack{},
		helm: &fakeHelm{}, brief: testBrief,
	}
}

// tick runs a tick at now with the observations.
func (h *harness) tick(now time.Time, obs Observations) error {
	h.now, h.obs = now, obs
	tk := NewTicker(h.cfg, Deps{
		Store:     h.store,
		Helm:      h.helm,
		Slack:     h.slack,
		Observer:  observerFunc(func(context.Context, time.Time) Observations { return h.obs }),
		ReadBrief: func() (string, error) { return h.brief, h.briefErr },
		OnAnnounce: func(ts string) error {
			h.threads = append(h.threads, ts)
			return nil
		},
		Now: func() time.Time { return h.now },
	})
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return tk.Tick(ctx)
}

// ok runs a tick that must succeed.
func (h *harness) ok(now time.Time, obs Observations) {
	h.t.Helper()
	require.NoError(h.t, h.tick(now, obs), "the tick at %s", now)
}

func (h *harness) state() State {
	h.t.Helper()
	st, err := DecodeState(h.store.data)
	require.NoError(h.t, err, "decoding the stored state")
	return st
}

// messages returns the messages Slack showed, each as "<thread ts>|<kind>".
func (h *harness) messages() []string {
	kinds := []struct{ marker, kind string }{
		{"New Pulsar version detected", "announce"}, {"<@U0ASSIST>", "brief"}, {"<!subteam^", "tag"},
		{"blocked: another load test", "blocked-load"}, {"blocked: src-router", "blocked-discard"},
		{"▶️ started", "started"}, {"🛑 aborted", "aborted"}, {"superseded by", "superseded"},
	}
	var out []string
	for _, m := range h.slack.shown {
		kind := "other"
		for _, k := range kinds {
			if strings.Contains(m.text, k.marker) {
				kind = k.kind
				break
			}
		}
		out = append(out, m.thread+"|"+kind)
	}
	return out
}

// startRun runs the ticks from a baseline of v1 to a running run for v2 started at tStart.
func (h *harness) startRun() {
	h.t.Helper()
	h.ok(t0.Add(-time.Hour), healthy(v1, t0.Add(-time.Hour)))
	h.ok(t0, healthy(v2, t0))
	h.ok(t0.Add(15*time.Minute), healthy(v2, t0.Add(15*time.Minute)))
	h.ok(tStart, healthy(v2, tStart))
	require.True(h.t, h.helm.installed, "rudder-load is installed")
}

func TestTickFirstTickOnlyRecordsTheVersion(t *testing.T) {
	h := newHarness(t)
	h.ok(t0, healthy(v1, t0))
	require.Empty(t, h.slack.shown, "nothing posted")
	require.Empty(t, h.helm.installs, "nothing installed")
	require.Equal(t, v1, h.state().Version, "the baseline is stored")
	require.Equal(t, 1, h.store.saves, "one save")

	h.ok(t0.Add(15*time.Minute), healthy(v1, t0))
	require.Equal(t, 1, h.store.saves, "a tick that changes nothing writes nothing")
}

func TestTickFullRun(t *testing.T) {
	h := newHarness(t)
	h.startRun()

	require.Equal(t, []string{"|announce"}, h.messages(), "the announcement")
	require.Equal(t, []string{fakeTS(1)}, h.threads, "the thread is reported")
	require.Len(t, h.helm.installs, 1, "one install")
	in := h.helm.installs[0]
	require.Equal(t, InstallRequest(h.cfg, h.state().Run), in, "the install request")

	for at := tStart.Add(15 * time.Minute); at.Before(tStart.Add(24 * time.Hour)); at = at.Add(time.Hour) {
		h.ok(at, healthy(v2, at))
	}
	require.Len(t, h.slack.shown, 1, "nothing posted during the run")
	require.Len(t, h.helm.installs, 1, "no other install")

	stop := tStart.Add(24*time.Hour + 5*time.Minute)
	h.ok(stop, healthy(v2, stop))
	require.False(t, h.helm.installed, "uninstalled at the deadline")
	require.Equal(t, PhaseStopped, h.state().Run.Phase, "handing off")

	h.ok(stop.Add(15*time.Minute), healthy(v2, stop.Add(15*time.Minute)))
	require.Equal(t, []string{"|announce", fakeTS(1) + "|brief", fakeTS(1) + "|tag"}, h.messages(),
		"the brief and the tag in the thread")
	st := h.state()
	require.Nil(t, st.Run, "the run is over")
	require.Equal(t, EndCompleted, st.Last.EndReason, "completed")
	require.Equal(t, PostPosted, st.Last.Posts[MsgTag].Status, "every message is recorded as posted")
	require.Equal(t, 1, h.helm.uninstalls, "one uninstall")
}

func TestTickRerunAfterACrashDoesNotPostTwice(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	// The tick posts the announcement, then dies before it records it as posted.
	h.store.failSave[h.store.saves+2] = errors.New("the runner was cancelled")
	err := h.tick(tStart, healthy(v2, tStart))
	require.Error(t, err, "the tick fails")
	require.Len(t, h.slack.shown, 1, "the announcement is out")
	require.Empty(t, h.helm.installs, "nothing more happened")
	require.Equal(t, PostPending, h.state().Run.Posts[MsgAnnounce].Status, "it's recorded as about to be posted")

	blocked := healthy(v2, tStart)
	blocked.OtherLoad.Deployments = []string{"rudder-load-other-0"}
	h.ok(tStart.Add(15*time.Minute), blocked)
	h.ok(tStart.Add(30*time.Minute), blocked)
	free := tStart.Add(45 * time.Minute)
	h.ok(free, healthy(v2, free))

	require.Equal(t, []string{"|announce", "|blocked-load", "|started"}, h.messages(),
		"the announcement went out once")
	require.Len(t, h.helm.installs, 1, "the run started")
	runID := h.state().Run.ID
	require.Len(t, h.slack.shown, 3, "the announcement, the blocked reply and the started reply")
	blockedMsg, startedMsg := h.slack.shown[1], h.slack.shown[2]
	require.Empty(t, blockedMsg.thread, "without the thread's ts, replies go to the channel")
	require.Equal(t, "Pulsar soak test `"+runID+"`: blocked: another load test is running in soak-ns", blockedMsg.text,
		"naming the run")
	require.Empty(t, startedMsg.thread, "in the channel")
	require.True(t, strings.HasPrefix(startedMsg.text, "Pulsar soak test `"+runID+"`: ▶️ started at "),
		"naming the run")
	require.Empty(t, h.threads, "no thread to report")
}

func TestTickCancelledWhilePostingRecordsThePost(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	// The workflow is cancelled while the announcement is being posted, and Slack posts it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.ctx, h.slack.onPost = ctx, cancel
	require.ErrorIs(t, h.tick(tStart, healthy(v2, tStart)), context.Canceled, "the tick reports the cancellation")

	post := h.state().Run.Posts[MsgAnnounce]
	require.Equal(t, PostPosted, post.Status, "the post is still recorded")
	require.Equal(t, fakeTS(1), post.TS, "with its ts")
	require.Empty(t, h.helm.installs, "but nothing new is started")
}

func TestTickLostSlackAnswerIsNotRetried(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	h.slack.results = []error{errors.New("context deadline exceeded")}
	err := h.tick(tStart, healthy(v2, tStart))
	require.ErrorContains(t, err, "won't be tried again", "the tick reports the lost answer")
	require.Equal(t, PostUnknown, h.state().Run.Posts[MsgAnnounce].Status, "recorded as unknown")
	require.True(t, h.helm.installed, "the run still started")

	h.ok(tStart.Add(15*time.Minute), healthy(v2, tStart))
	require.Equal(t, 1, h.slack.attempts, "chat.postMessage was called once")
}

func TestTickMessageSlackDidNotPostIsPostedLater(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	h.slack.results = []error{fmt.Errorf("%w: slack answered channel_not_found", slack.ErrNotPosted)}
	err := h.tick(tStart, healthy(v2, tStart))
	require.ErrorContains(t, err, "channel_not_found", "the tick fails")
	require.Empty(t, h.slack.shown, "nothing was shown")
	require.Empty(t, h.helm.installs, "the run waits for its announcement")
	require.False(t, h.state().Run.Attempted(MsgAnnounce), "the announcement isn't recorded")
	require.Equal(t, 1, h.slack.attempts, "not tried again in the same tick")

	h.ok(tStart.Add(15*time.Minute), healthy(v2, tStart))
	require.Len(t, h.slack.shown, 1, "the next tick posts it")
	require.True(t, h.helm.installed, "and starts the run")
}

func TestTickAnotherLoadTestBlocksWithOneMessage(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	blocked := func(at time.Time) Observations {
		obs := healthy(v2, at)
		obs.OtherLoad.Deployments = []string{"rudder-load-other-0"}
		return obs
	}
	for at := tStart; at.Before(tStart.Add(12 * time.Hour)); at = at.Add(15 * time.Minute) {
		h.ok(at, blocked(at))
	}
	require.Equal(t, []string{"|announce", fakeTS(1) + "|blocked-load"}, h.messages(),
		"one blocked message over 12 hours of ticks")
	require.Empty(t, h.helm.installs, "nothing installed while blocked")

	free := tStart.Add(12 * time.Hour)
	h.ok(free, healthy(v2, free))
	require.True(t, h.helm.installed, "installed once the other load test is gone")
	require.Len(t, h.slack.shown, 3, "and the started reply")
	require.Equal(t, fakeTS(1), h.slack.shown[2].thread, "in the thread")
	require.Equal(t, free.Add(24*time.Hour), h.state().Run.Deadline, "the deadline counts from the real start")
}

func TestTickDiscardFlipsMidRunAborts(t *testing.T) {
	h := newHarness(t)
	h.startRun()

	at := tStart.Add(5 * time.Hour)
	obs := healthy(v2, at)
	obs.Discard = Discard{State: DiscardFailed, Reason: "pod srcrouter-abc: sets it to \"false\""}
	h.ok(at, obs)

	require.False(t, h.helm.installed, "uninstalled")
	require.Equal(t, 1, h.helm.uninstalls, "once")
	require.Equal(t, sentMessage{text: "🛑 aborted: src-router discard is no longer true", thread: fakeTS(1),
		ts: fakeTS(2)}, h.slack.shown[1], "the abort reply")
	require.Len(t, h.slack.shown, 2, "no brief for an aborted run")
	require.Equal(t, EndAborted, h.state().Last.EndReason, "aborted")

	h.ok(at.Add(15*time.Minute), obs)
	require.Len(t, h.slack.shown, 2, "nothing more")
	require.Equal(t, 1, h.helm.uninstalls, "no second uninstall")
}

func TestTickNewVersionMidRunSupersedes(t *testing.T) {
	h := newHarness(t)
	h.startRun()

	seen := tStart.Add(3 * time.Hour)
	h.ok(seen, healthy(v3, seen))
	h.ok(seen.Add(StableFor), healthy(v3, seen.Add(StableFor)))

	require.Equal(t, 1, h.helm.uninstalls, "the old run is uninstalled")
	require.Len(t, h.helm.installs, 2, "and the new one installed")
	require.True(t, h.helm.installed, "the new run is going")
	require.Equal(t, []string{"|announce", "|announce", fakeTS(1) + "|superseded"}, h.messages(),
		"the new announcement, then the old thread points at it")
	require.Contains(t, h.slack.shown[2].text, "/p"+strings.Replace(fakeTS(2), ".", "", 1), "the link")
	require.Equal(t, []string{fakeTS(1), fakeTS(2)}, h.threads, "both threads are reported")
	st := h.state()
	require.Equal(t, v3, st.Run.Version, "the run is for the new version")
	require.Contains(t, st.Run.ID, v3, "with its own ID")
	require.Equal(t, "deployment.env.LOAD_RUN_ID", h.helm.installs[1].SetString[1].Key, "the run ID value")
	require.Equal(t, st.Run.ID, h.helm.installs[1].SetString[1].Value, "the new run's ID")
}

func TestTickHandOffWaitsForTheMetrics(t *testing.T) {
	h := newHarness(t)
	h.startRun()
	stop := tStart.Add(24*time.Hour + time.Minute)
	h.ok(stop, healthy(v2, stop))

	lagging := healthy(v2, stop)
	lagging.Fresh.Logs = stop.Add(-time.Minute)
	for _, after := range []time.Duration{2 * time.Minute, 7 * time.Minute, HandOffMax - time.Second} {
		h.ok(stop.Add(after), lagging)
		require.Len(t, h.slack.shown, 1, "no brief %s after the stop while the logs lag", after)
	}
	h.ok(stop.Add(HandOffMax), lagging)
	require.Len(t, h.slack.shown, 3, "after 15 minutes the brief and the tag go out anyway")
}

func TestTickDryRun(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))

	h.cfg.DryRun, h.cfg.RunLength = true, time.Hour
	h.ok(t0, healthy(v1, t0))
	require.True(t, h.helm.installed, "a dry run starts without a version change")
	require.True(t, strings.HasPrefix(h.slack.shown[0].text, "🧪 Dry run: 🔍 New Pulsar version detected"),
		"the announcement is marked")
	require.Equal(t, t0.Add(time.Hour).Format(time.RFC3339), h.helm.installs[0].SetString[2].Value,
		"RUN_DEADLINE is an hour away")

	h.cfg = testConfig()
	stop := t0.Add(time.Hour + 2*time.Minute)
	h.ok(stop, healthy(v1, stop))
	require.False(t, h.helm.installed, "stopped after the dry run's hour")
	h.ok(stop.Add(HandOffMax), healthy(v1, stop.Add(HandOffMax)))

	require.Len(t, h.slack.shown, 2, "the announcement and the brief, without the tag")
	require.True(t, strings.HasPrefix(h.slack.shown[1].text, "🧪 Dry run: <@U0ASSIST> assess"), "the brief is marked")
	for _, m := range h.slack.shown {
		require.NotContains(t, m.text, "subteam", "no @server-eng tag")
	}
	require.Equal(t, v1, h.state().Version, "the version is unchanged")
}

func TestTickBriefOverTheLimitPostsNothing(t *testing.T) {
	long := "@RUDDER_ASSIST@ " + strings.Repeat("x", BriefLimit)

	t.Run("before a run", func(t *testing.T) {
		h := newHarness(t)
		h.ok(t0.Add(-time.Hour), healthy(v1, t0))
		h.brief = long
		require.ErrorContains(t, h.tick(t0, healthy(v2, t0)), "bytes once filled", "every tick checks the brief")
		require.ErrorContains(t, h.tick(tStart, healthy(v2, tStart)), "bytes once filled", "still broken")
		require.Empty(t, h.slack.shown, "no run is announced with a broken brief")
		require.Empty(t, h.helm.installs, "nor started")

		h.brief = testBrief
		h.ok(tStart.Add(15*time.Minute), healthy(v2, tStart))
		require.True(t, h.helm.installed, "the run starts once it's fixed")
	})

	t.Run("at the hand-off", func(t *testing.T) {
		h := newHarness(t)
		h.startRun()
		h.brief = long
		stop := tStart.Add(24 * time.Hour)
		require.Error(t, h.tick(stop, healthy(v2, stop)), "the tick reports the brief")
		require.False(t, h.helm.installed, "a broken brief doesn't keep the load running")
		err := h.tick(stop.Add(HandOffMax), healthy(v2, stop.Add(HandOffMax)))
		require.ErrorContains(t, err, "bytes once filled", "the hand-off fails")
		require.Len(t, h.slack.shown, 1, "nothing is posted")

		h.brief = testBrief
		h.ok(stop.Add(HandOffMax+15*time.Minute), healthy(v2, stop))
		require.Len(t, h.slack.shown, 3, "the brief and the tag go out once it's fixed")
	})
}

func TestTickInstallFailureUninstalls(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	h.helm.installErr = errors.New("exit status 1")
	require.ErrorContains(t, h.tick(tStart, healthy(v2, tStart)), "installing rudder-load", "the tick fails")
	require.Equal(t, 1, h.helm.uninstalls, "whatever helm did is uninstalled")
	require.Equal(t, PhasePending, h.state().Run.Phase, "the run is still pending")

	h.helm.installErr = nil
	h.ok(tStart.Add(15*time.Minute), healthy(v2, tStart))
	require.True(t, h.helm.installed, "the next tick installs")
	require.Len(t, h.slack.shown, 1, "the announcement went out once")
}

func TestTickConflictPostsNothing(t *testing.T) {
	h := newHarness(t)
	h.ok(t0.Add(-time.Hour), healthy(v1, t0))
	h.ok(t0, healthy(v2, t0))

	h.store.failSave[h.store.saves+1] = kube.ErrConflict
	require.ErrorIs(t, h.tick(tStart, healthy(v2, tStart)), kube.ErrConflict, "another tick wrote the state")
	require.Zero(t, h.slack.attempts, "nothing is posted without recording it first")
}

func TestTickFailsOnObservationErrors(t *testing.T) {
	h := newHarness(t)
	obs := healthy(v1, t0)
	obs.Fresh.Err = errors.New("loki: 401")
	require.ErrorContains(t, h.tick(t0, obs), "loki: 401", "a broken datasource fails the tick")
	require.Equal(t, v1, h.state().Version, "what could be done was done")
}

func TestInstallRequest(t *testing.T) {
	cfg := testConfig()
	run := &Run{ID: "pulsar-soak-test-4.0.12.6-20261015T123000Z", Deadline: tStart.Add(24 * time.Hour)}
	in := InstallRequest(cfg, run)
	require.Equal(t, helm.Install{
		Release:     "rudder-load-pulsar-soak-test",
		Chart:       "artifacts/helm",
		Namespace:   "soak-ns",
		ValuesFiles: []string{"artifacts/helm/pulsar_soak_test_values.yaml"},
		Set: []helm.Value{
			{Key: "namespace", Value: "soak-ns"}, {Key: "deployment.name", Value: "rudder-load-pulsar-soak-test-oz"},
		},
		SetString: []helm.Value{
			{Key: "deployment.env.HTTP_ENDPOINT", Value: "https://ingest.example.com"},
			{Key: "deployment.env.LOAD_RUN_ID", Value: "pulsar-soak-test-4.0.12.6-20261015T123000Z"},
			{Key: "deployment.env.RUN_DEADLINE", Value: "2026-10-16T12:30:00Z"},
		},
		SecretValues: map[string]any{
			"deployment": map[string]any{"env": map[string]any{"SOURCES": "wk-secret-1,wk-secret-2"}},
		},
	}, in, "the install")

	args, err := helm.UpgradeInstallArgs(in, "/tmp/secret.yaml")
	require.NoError(t, err, "building helm's arguments")
	require.NotContains(t, strings.Join(args, " "), "wk-secret", "the write keys aren't on the command line")
	require.Contains(t, args, "/tmp/secret.yaml", "they come from the secret values file")

	cfg.DeploymentName = ""
	require.Equal(t, []helm.Value{{Key: "namespace", Value: "soak-ns"}}, InstallRequest(cfg, run).Set,
		"without a deployment name, the profile's stands")
}

func TestWriteKeysNeverPrint(t *testing.T) {
	cfg := testConfig()
	for _, s := range []string{
		fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg), fmt.Sprint(cfg.WriteKeys),
	} {
		require.NotContains(t, s, "wk-secret", "formatting hides the keys")
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err, "encoding")
	require.NotContains(t, string(data), "wk-secret", "JSON hides the keys")
	require.Equal(t, "wk-secret-1,wk-secret-2", cfg.WriteKeys.Joined(), "the keys are still there to use")
}
