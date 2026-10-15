package pulsarsoak

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// tStart is when a run for v2, first seen at t0, starts.
var tStart = t0.Add(StableFor)

// running returns the state of a run for v2 that started at tStart, and the settler that made it.
func running(t *testing.T, cfg Config) (*settler, State) {
	t.Helper()
	s := &settler{t: t, cfg: cfg}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
	st, actions, _ := s.settle(st, inputs(tStart, healthy(v2, tStart)))
	require.Equal(t, []string{"post:announce", "install"}, actions, "the run starts")
	return s, st
}

// stopped returns the state of the run for v2 after it was uninstalled at the deadline, and when that was.
func stopped(t *testing.T, cfg Config) (*settler, State, time.Time) {
	t.Helper()
	s, st := running(t, cfg)
	stop := st.Run.Deadline.Add(5 * time.Minute)
	st, actions, wait := s.settle(st, inputs(stop, healthy(v2, stop)))
	require.Equal(t, []string{"uninstall"}, actions, "the run stops")
	require.Contains(t, wait, "hand-off waits", "the hand-off waits")
	return s, st, stop
}

func slackTimeOf(t time.Time) string {
	return "<!date^" + strconv.FormatInt(t.Unix(), 10) + "^{date_short_pretty} {time}|" + FormatWindowTime(t) + ">"
}

func TestFirstTickRecordsABaseline(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, actions, _ := s.settle(State{Schema: StateSchema}, inputs(t0, healthy(v1, t0)))
	require.Empty(t, actions, "the first tick only records the version")
	require.Equal(t, v1, st.Version, "the baseline")
	require.Nil(t, st.Candidate, "no candidate")
	require.Nil(t, st.Run, "no run")

	st, actions, _ = s.settle(st, inputs(t0.Add(time.Hour), healthy(v1, t0.Add(time.Hour))))
	require.Empty(t, actions, "the same version starts nothing")
	require.Nil(t, st.Candidate, "no candidate")
}

func TestFirstTickWaitsForTheBrokersToAgree(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	mixed := healthy("", t0)
	mixed.Pulsar.Stable, mixed.Pulsar.Ready = false, false
	st, actions, _ := s.settle(State{Schema: StateSchema}, inputs(t0, mixed))
	require.Empty(t, actions, "nothing to do")
	require.Empty(t, st.Version, "no baseline while the brokers disagree")

	unknown := Observations{Pulsar: Pulsar{Err: errors.New("grafana is down")}}
	st, _, _ = s.settle(st, inputs(t0, unknown))
	require.Empty(t, st.Version, "no baseline without an answer")
}

func TestNewVersionStartsARunOnceStable(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}

	st, actions, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
	require.Empty(t, actions, "a new version waits")
	require.Equal(t, &Candidate{Version: v2, Since: t0}, st.Candidate, "the candidate")
	require.Equal(t, v1, st.Version, "the version is still the old one")

	at := t0.Add(StableFor - time.Second)
	st, actions, wait := s.settle(st, inputs(at, healthy(v2, at)))
	require.Empty(t, actions, "still waiting just before 30 minutes")
	require.Contains(t, wait, "stable for 30m0s", "the wait reason")

	st, actions, _ = s.settle(st, inputs(tStart, healthy(v2, tStart)))
	require.Equal(t, []string{"post:announce", "install"}, actions, "after 30 minutes the run is announced and starts")
	r := st.Run
	require.Equal(t, "pulsar-soak-test-4.0.12.6-20261015T123000Z", r.ID, "run ID")
	require.Equal(t, v2, r.Version, "the run's version")
	require.Equal(t, v1, r.FromVersion, "the previous version")
	require.Equal(t, PhaseRunning, r.Phase, "running")
	require.Equal(t, tStart, r.StartedAt, "start")
	require.Equal(t, tStart.Add(24*time.Hour), r.Deadline, "deadline")
	require.Equal(t, v2, st.Version, "the new version is recorded")
	require.Nil(t, st.Candidate, "no candidate left")

	announce := s.lastSent(MsgAnnounce)
	require.Empty(t, announce.ThreadTS, "the announcement is a top-level message")
	require.Equal(t, "🔍 New Pulsar version detected: `4.0.12.5` → `4.0.12.6`\n"+
		"Pulsar soak test `pulsar-soak-test-4.0.12.6-20261015T123000Z` in `soak-ns`: starts "+slackTimeOf(tStart)+
		", expected end "+slackTimeOf(tStart.Add(24*time.Hour))+
		" · <https://github.com/org/repo/actions/runs/42|workflow run>", announce.Text, "the announcement")
}

func TestNoRunUntilEverythingIsReady(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Inputs)
	}{
		{name: "another version in the last 30 minutes", change: func(in *Inputs) { in.Obs.Pulsar.Stable = false }},
		{name: "a pod isn't ready", change: func(in *Inputs) { in.Obs.Pulsar.Ready = false }},
		{name: "Pulsar is unknown", change: func(in *Inputs) { in.Obs.Pulsar = Pulsar{Err: errors.New("down")} }},
		{name: "the brief template is broken", change: func(in *Inputs) { in.BriefErr = errors.New("too long") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &settler{t: t, cfg: testConfig()}
			st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))

			in := inputs(tStart, healthy(v2, tStart))
			tt.change(&in)
			st, actions, _ := s.settle(st, in)
			require.Empty(t, actions, "no run starts")
			require.Nil(t, st.Run, "no run")
			require.Equal(t, v1, st.Version, "the version is unchanged")

			at := tStart.Add(15 * time.Minute)
			_, actions, _ = s.settle(st, inputs(at, healthy(v2, at)))
			require.Equal(t, []string{"post:announce", "install"}, actions, "the run starts once it's resolved")
		})
	}
}

func TestVersionFlapRestartsTheWait(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))

	mixed := healthy("", t0)
	mixed.Pulsar.Stable, mixed.Pulsar.Ready = false, false
	st, _, _ = s.settle(st, inputs(t0.Add(15*time.Minute), mixed))
	require.Nil(t, st.Candidate, "disagreeing brokers reset the candidate")

	at := t0.Add(30 * time.Minute)
	st, actions, _ := s.settle(st, inputs(at, healthy(v2, at)))
	require.Empty(t, actions, "the wait starts again")
	require.Equal(t, &Candidate{Version: v2, Since: at}, st.Candidate, "a new candidate")

	at = at.Add(StableFor)
	_, actions, _ = s.settle(st, inputs(at, healthy(v2, at)))
	require.Equal(t, []string{"post:announce", "install"}, actions, "30 minutes after the flap the run starts")
}

func TestAnotherLoadTestBlocksWithOneMessage(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))

	blocked := func(at time.Time) Inputs {
		in := inputs(at, healthy(v2, at))
		in.Obs.OtherLoad.Deployments = []string{"rudder-load-other-0"}
		return in
	}
	st, actions, wait := s.settle(st, blocked(tStart))
	require.Equal(t, []string{"post:announce", "post:" + MsgBlockedLoad}, actions, "announced, then blocked")
	require.Contains(t, wait, "rudder-load-other-0", "the wait names the other load test")
	msg := s.lastSent(MsgBlockedLoad)
	require.Equal(t, "blocked: another load test is running in soak-ns", msg.Text, "the blocked message")
	require.Equal(t, fakeTS(1), msg.ThreadTS, "a reply in the run's thread")

	for at := tStart.Add(15 * time.Minute); at.Before(tStart.Add(6 * time.Hour)); at = at.Add(15 * time.Minute) {
		st, actions, _ = s.settle(st, blocked(at))
		require.Empty(t, actions, "no more messages at %s", at)
	}
	require.Equal(t, PhasePending, st.Run.Phase, "still waiting")

	unknown := inputs(tStart.Add(6*time.Hour), healthy(v2, tStart.Add(6*time.Hour)))
	unknown.Obs.OtherLoad.Err = errors.New("forbidden")
	st, actions, _ = s.settle(st, unknown)
	require.Empty(t, actions, "a failed check doesn't start the run")

	free := tStart.Add(6*time.Hour + 15*time.Minute)
	st, actions, _ = s.settle(st, inputs(free, healthy(v2, free)))
	require.Equal(t, []string{"install", "post:" + MsgStarted}, actions, "it starts as soon as the other is gone")
	require.Equal(t, free, st.Run.StartedAt, "the start is when it really started")
	require.Equal(t, free.Add(24*time.Hour), st.Run.Deadline, "the deadline counts from the start")
	require.Equal(t, "▶️ started at "+slackTimeOf(free)+"; the load stops at "+slackTimeOf(free.Add(24*time.Hour)),
		s.lastSent(MsgStarted).Text, "the started message")

	_, actions, _ = s.settle(st, blocked(free.Add(15*time.Minute)))
	require.Empty(t, actions, "another load test showing up after the start changes nothing")
}

func TestSrcRouterMustDiscardBeforeTheStart(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))

	withDiscard := func(at time.Time, d Discard) Inputs {
		in := inputs(at, healthy(v2, at))
		in.Obs.Discard = d
		return in
	}
	failed := Discard{State: DiscardFailed, Reason: "pod x: no container sets X"}
	st, actions, wait := s.settle(st, withDiscard(tStart, failed))
	require.Equal(t, []string{"post:announce", "post:" + MsgBlockedDiscard}, actions, "announced, then blocked")
	require.Contains(t, wait, "pod x", "the wait has the reason")
	require.Equal(t, "blocked: src-router discard is not true in soak-ns", s.lastSent(MsgBlockedDiscard).Text,
		"the blocked message")

	st, actions, _ = s.settle(st, withDiscard(tStart.Add(15*time.Minute), failed))
	require.Empty(t, actions, "one message only")
	st, actions, wait = s.settle(st, withDiscard(tStart.Add(30*time.Minute), Discard{Err: errors.New("down")}))
	require.Empty(t, actions, "an unknown discard doesn't start the run")
	require.Contains(t, wait, "can't tell", "the wait reason")

	at := tStart.Add(45 * time.Minute)
	_, actions, _ = s.settle(st, withDiscard(at, Discard{State: DiscardOK}))
	require.Equal(t, []string{"install", "post:" + MsgStarted}, actions, "it starts once src-router discards")
}

func TestDiscardFlipsMidRunAborts(t *testing.T) {
	s, st := running(t, testConfig())

	at := tStart.Add(3 * time.Hour)
	in := inputs(at, healthy(v2, at))
	in.Obs.Discard = Discard{State: DiscardFailed, Reason: "pod y sets it to \"false\""}
	st, actions, _ := s.settle(st, in)
	require.Equal(t, []string{"uninstall", "post:" + MsgAborted}, actions, "uninstalled at once, then the reply")
	msg := s.lastSent(MsgAborted)
	require.Equal(t, "🛑 aborted: src-router discard is no longer true", msg.Text, "the abort message")
	require.Equal(t, fakeTS(1), msg.ThreadTS, "in the run's thread")
	require.Nil(t, st.Run, "the run is over")
	require.Equal(t, EndAborted, st.Last.EndReason, "it was aborted")
	require.Equal(t, at, st.Last.StoppedAt, "when it stopped")

	_, actions, _ = s.settle(st, inputs(at.Add(15*time.Minute), healthy(v2, at)))
	require.Empty(t, actions, "nothing starts again for the same version")
}

func TestUnverifiedDiscardAbortsAfter30Minutes(t *testing.T) {
	s, st := running(t, testConfig())
	unknown := func(at time.Time) Inputs {
		in := inputs(at, healthy(v2, at))
		in.Obs.Discard = Discard{Err: errors.New("mimir is down")}
		return in
	}

	first := tStart.Add(time.Hour)
	st, actions, _ := s.settle(st, unknown(first))
	require.Empty(t, actions, "one unknown check doesn't abort")
	require.Equal(t, first, st.Run.UnverifiedSince, "the start of the unverified time")

	st, actions, _ = s.settle(st, inputs(first.Add(15*time.Minute), healthy(v2, first)))
	require.Empty(t, actions, "a good check")
	require.True(t, st.Run.UnverifiedSince.IsZero(), "resets the unverified time")

	second := first.Add(30 * time.Minute)
	st, _, _ = s.settle(st, unknown(second))
	st, actions, _ = s.settle(st, unknown(second.Add(unverifiedMax-time.Second)))
	require.Empty(t, actions, "just under 30 minutes unverified")

	st, actions, _ = s.settle(st, unknown(second.Add(unverifiedMax)))
	require.Equal(t, []string{"uninstall", "post:" + MsgAborted}, actions, "30 minutes unverified aborts")
	require.Equal(t, "🛑 aborted: src-router discard could not be verified for 30m0s", s.lastSent(MsgAborted).Text,
		"the abort message")
	require.Equal(t, EndUnverified, st.Last.EndReason, "why")
}

func TestDeadlineStopsAndHandsOff(t *testing.T) {
	s, st, stop := stopped(t, testConfig())
	require.Equal(t, PhaseStopped, st.Run.Phase, "stopped")
	require.Equal(t, stop, st.Run.StoppedAt, "the uninstall time")

	at := stop.Add(HandOffMax)
	st, actions, _ := s.settle(st, inputs(at, healthy(v2, at.Add(-time.Minute))))
	require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag}, actions, "the brief, then the tag")

	brief := s.lastSent(MsgBrief)
	require.Equal(t, "<@U0ASSIST> assess the soak from `2026-10-15T12:30:00Z` to `2026-10-16T12:30:00Z`.", brief.Text,
		"the window ends at the deadline, when the load stopped, not at the uninstall")
	require.Equal(t, fakeTS(1), brief.ThreadTS, "in the run's thread")
	tag := s.lastSent(MsgTag)
	require.Equal(t, "<!subteam^S0SERVERENG> the Pulsar soak test of `4.0.12.6` is done, and it's yours from here.",
		tag.Text, "the tag")
	require.Equal(t, fakeTS(1), tag.ThreadTS, "in the run's thread")
	require.Nil(t, st.Run, "the run is over")
	require.Equal(t, EndCompleted, st.Last.EndReason, "completed")

	_, actions, _ = s.settle(st, inputs(at.Add(time.Hour), healthy(v2, at.Add(time.Hour))))
	require.Empty(t, actions, "nothing more")
}

func TestHandOffWaitsForTheMetrics(t *testing.T) {
	fresh := func(stop, at time.Time, lagging ...string) Inputs {
		in := inputs(at, healthy(v2, at))
		for _, l := range lagging {
			switch l {
			case "app":
				in.Obs.Fresh.AppMetrics = stop.Add(-time.Second)
			case "kube":
				in.Obs.Fresh.KubeMetrics = time.Time{}
			case "logs":
				in.Obs.Fresh.Logs = stop
			}
		}
		return in
	}

	t.Run("caught up after 2 minutes", func(t *testing.T) {
		s, st, stop := stopped(t, testConfig())
		st, actions, _ := s.settle(st, fresh(stop, stop.Add(HandOffMin-time.Second)))
		require.Empty(t, actions, "not before 2 minutes, even with fresh data")
		_, actions, _ = s.settle(st, fresh(stop, stop.Add(HandOffMin)))
		require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag}, actions, "at 2 minutes with fresh data")
	})

	for _, lagging := range []string{"app", "kube", "logs"} {
		t.Run("waits while "+lagging+" lags, then goes ahead after 15 minutes", func(t *testing.T) {
			s, st, stop := stopped(t, testConfig())
			for _, after := range []time.Duration{3 * time.Minute, 10 * time.Minute, HandOffMax - time.Second} {
				var actions []string
				st, actions, _ = s.settle(st, fresh(stop, stop.Add(after), lagging))
				require.Empty(t, actions, "waits %s after the stop", after)
			}
			_, actions, _ := s.settle(st, fresh(stop, stop.Add(HandOffMax), lagging))
			require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag}, actions, "hands off at 15 minutes")
		})
	}

	t.Run("an unknown freshness counts as lagging", func(t *testing.T) {
		s, st, stop := stopped(t, testConfig())
		in := inputs(stop.Add(5*time.Minute), healthy(v2, stop.Add(5*time.Minute)))
		in.Obs.Fresh = Freshness{Err: errors.New("down")}
		st, actions, _ := s.settle(st, in)
		require.Empty(t, actions, "waits")
		in.Now = stop.Add(HandOffMax)
		_, actions, _ = s.settle(st, in)
		require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag}, actions, "hands off at 15 minutes")
	})
}

func TestNewVersionMidRunSupersedes(t *testing.T) {
	s, st := running(t, testConfig())
	seen := tStart.Add(2 * time.Hour)
	st, actions, _ := s.settle(st, inputs(seen, healthy(v3, seen)))
	require.Empty(t, actions, "the new version waits for 30 minutes")
	require.Equal(t, v3, st.Candidate.Version, "the candidate")

	at := seen.Add(StableFor)
	st, actions, _ = s.settle(st, inputs(at, healthy(v3, at)))
	require.Equal(t, []string{"uninstall", "post:announce", "post:" + MsgSuperseded + "@closing", "install"}, actions,
		"uninstall, announce the new version, point the old thread at it, then start")

	require.Nil(t, st.Closing, "the old run is closed")
	require.Equal(t, EndSuperseded, st.Last.EndReason, "superseded")
	require.Equal(t, v3, st.Last.SupersededBy, "by the new version")
	require.Equal(t, at, st.Last.StoppedAt, "stopped then")
	require.Equal(t, v3, st.Run.Version, "the new run")
	require.Equal(t, v2, st.Run.FromVersion, "from the old version")
	require.Equal(t, PhaseRunning, st.Run.Phase, "running")
	require.Equal(t, v3, st.Version, "the version")

	announce := s.lastSent(MsgAnnounce)
	require.Empty(t, announce.ThreadTS, "a new top-level message")
	require.Contains(t, announce.Text, "`4.0.12.6` → `4.0.13.1`", "old → new")
	superseded := s.lastSent(MsgSuperseded)
	require.Equal(t, fakeTS(1), superseded.ThreadTS, "in the old thread")
	require.Equal(t,
		"superseded by `4.0.13.1`: <https://example.slack.com/archives/C0CHANNEL/p1792072800000002|new thread>",
		superseded.Text, "links the new thread")
}

func TestNewVersionWhileBlockedSupersedes(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
	blocked := func(version string, at time.Time) Inputs {
		in := inputs(at, healthy(version, at))
		in.Obs.OtherLoad.Deployments = []string{"rudder-load-other-0"}
		return in
	}
	st, _, _ = s.settle(st, blocked(v2, tStart))
	seen := tStart.Add(time.Hour)
	st, _, _ = s.settle(st, blocked(v3, seen))

	st, actions, _ := s.settle(st, blocked(v3, seen.Add(StableFor)))
	require.Equal(t, []string{"post:announce", "post:" + MsgSuperseded + "@closing", "post:" + MsgBlockedLoad},
		actions, "nothing to uninstall; the new run is announced and blocked too")
	require.Equal(t, PhasePending, st.Run.Phase, "the new run waits")
	require.Equal(t, v3, st.Run.Version, "for the new version")
}

func TestNewVersionDuringTheHandOffWaitsForIt(t *testing.T) {
	s, st, stop := stopped(t, testConfig())
	st.Candidate = &Candidate{Version: v3, Since: stop.Add(-time.Hour)}

	lagging := func(at time.Time) Inputs {
		in := inputs(at, healthy(v3, at))
		in.Obs.Fresh = Freshness{}
		return in
	}
	st, actions, _ := s.settle(st, lagging(stop.Add(5*time.Minute)))
	require.Empty(t, actions, "the finished run isn't superseded")

	_, actions, _ = s.settle(st, lagging(stop.Add(HandOffMax)))
	require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag, "post:announce", "install"}, actions,
		"the hand-off, then the run for the new version")
}

func TestDryRun(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun, cfg.RunLength = true, time.Hour
	s := &settler{t: t, cfg: cfg}

	st, actions, _ := s.settle(baseline(), inputs(t0, healthy(v1, t0)))
	require.Equal(t, []string{"post:announce", "install"}, actions, "a dry run starts without a version change")
	r := st.Run
	require.True(t, r.DryRun, "a dry run")
	require.Equal(t, "pulsar-soak-test-dry-run-4.0.12.5-20261015T120000Z", r.ID, "run ID")
	require.Equal(t, t0.Add(time.Hour), r.Deadline, "the dry run's length")
	require.Equal(t, v1, st.Version, "the version doesn't change")
	require.Equal(t, "🧪 Dry run: 🔍 New Pulsar version detected: `4.0.12.5` → `4.0.12.5`\n"+
		"Pulsar soak test `pulsar-soak-test-dry-run-4.0.12.5-20261015T120000Z` in `soak-ns`: starts "+slackTimeOf(t0)+
		", expected end "+slackTimeOf(t0.Add(time.Hour))+
		" · <https://github.com/org/repo/actions/runs/42|workflow run>",
		s.lastSent(MsgAnnounce).Text, "the announcement is marked")

	// The scheduled ticks after it don't ask for a dry run and run 24h, which doesn't change the dry run.
	s.cfg = testConfig()
	st, actions, _ = s.settle(st, inputs(t0.Add(30*time.Minute), healthy(v1, t0)))
	require.Empty(t, actions, "running")
	stop := t0.Add(time.Hour + 10*time.Minute)
	st, actions, _ = s.settle(st, inputs(stop, healthy(v1, stop)))
	require.Equal(t, []string{"uninstall"}, actions, "stops after its own length")
	st, actions, _ = s.settle(st, inputs(stop.Add(HandOffMax), healthy(v1, stop.Add(HandOffMax))))
	require.Equal(t, []string{"post:" + MsgBrief}, actions, "the brief, without the tag")
	require.Equal(t, "🧪 Dry run: <@U0ASSIST> assess the soak from `2026-10-15T12:00:00Z` to `2026-10-15T13:00:00Z`.",
		s.lastSent(MsgBrief).Text, "the brief is marked")
	require.Nil(t, st.Run, "done")

	_, actions, _ = s.settle(st, inputs(stop.Add(time.Hour), healthy(v1, stop.Add(time.Hour))))
	require.Empty(t, actions, "without the dry-run setting nothing starts")
}

func TestDryRunOnTheFirstTick(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = true
	s := &settler{t: t, cfg: cfg}
	st, actions, _ := s.settle(State{Schema: StateSchema}, inputs(t0, healthy(v1, t0)))
	require.Equal(t, []string{"post:announce", "install"}, actions, "baseline and dry run")
	require.Equal(t, v1, st.Version, "the baseline")
	require.True(t, st.Run.DryRun, "a dry run")
}

func TestDryRunWhileARunIsActive(t *testing.T) {
	s, st := running(t, testConfig())
	s.cfg.DryRun = true
	at := tStart.Add(time.Hour)
	next, actions, _ := s.settle(st, inputs(at, healthy(v2, at)))
	require.Empty(t, actions, "no dry run while a run is active")
	require.Equal(t, st.Run.ID, next.Run.ID, "the same run")
}

func TestDryRunSupersededByARealVersion(t *testing.T) {
	cfg := testConfig()
	cfg.DryRun = true
	s := &settler{t: t, cfg: cfg}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v1, t0)))
	s.cfg = testConfig()
	st, _, _ = s.settle(st, inputs(t0.Add(15*time.Minute), healthy(v2, t0)))
	at := t0.Add(15*time.Minute + StableFor)
	st, actions, _ := s.settle(st, inputs(at, healthy(v2, at)))
	require.Equal(t, []string{"uninstall", "post:announce", "post:" + MsgSuperseded + "@closing", "install"}, actions,
		"a real version supersedes the dry run")
	require.False(t, st.Run.DryRun, "a real run")
	require.Equal(t, v1, st.Run.FromVersion, "from the version before the dry run")
	require.Contains(t, s.lastSent(MsgSuperseded).Text, "🧪 Dry run: superseded by", "the dry run's reply is marked")
}

func TestRepliesWithoutAThread(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
	in := inputs(tStart, healthy(v2, tStart))
	in.Obs.OtherLoad.Deployments = []string{"x"}
	d, err := Decide(s.cfg, st, in)
	require.NoError(t, err, "deciding")
	require.Equal(t, "post:announce", describe(d.Action), "the announcement")

	// The tick lost Slack's answer: the announcement may be out, but its ts is unknown.
	st = d.State
	st.Run.Posts[MsgAnnounce] = Post{Status: PostUnknown, At: tStart}
	st, actions, _ := s.settle(st, in)
	require.Equal(t, []string{"post:" + MsgBlockedLoad}, actions, "the announcement isn't posted again")
	msg := s.lastSent(MsgBlockedLoad)
	require.Empty(t, msg.ThreadTS, "the reply goes to the channel")
	require.Equal(t, "Pulsar soak test `"+st.Run.ID+"`: blocked: another load test is running in soak-ns", msg.Text,
		"and names the run")
}

func TestPendingPostIsNeverRepeated(t *testing.T) {
	s := &settler{t: t, cfg: testConfig()}
	st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
	d, err := Decide(s.cfg, st, inputs(tStart, healthy(v2, tStart)))
	require.NoError(t, err, "deciding")

	// A tick recorded the announcement as about to be posted, then stopped.
	st = d.State
	st.Run.Posts[MsgAnnounce] = Post{Status: PostPending, At: tStart}
	_, actions, _ := s.settle(st, inputs(tStart.Add(15*time.Minute), healthy(v2, tStart)))
	require.Equal(t, []string{"install"}, actions, "the run goes on without posting the announcement again")
}

func TestBrokenBriefBlocksStartsAndHandOffs(t *testing.T) {
	broken := errors.New("the brief is 4100 bytes")

	t.Run("pending", func(t *testing.T) {
		s := &settler{t: t, cfg: testConfig()}
		st, _, _ := s.settle(baseline(), inputs(t0, healthy(v2, t0)))
		d, err := Decide(s.cfg, st, inputs(tStart, healthy(v2, tStart)))
		require.NoError(t, err, "deciding")
		st = d.State
		st.Run.Posts[MsgAnnounce] = Post{Status: PostPosted, TS: fakeTS(1)}

		in := inputs(tStart, healthy(v2, tStart))
		in.BriefErr = broken
		st, actions, wait := s.settle(st, in)
		require.Empty(t, actions, "no install with a broken brief")
		require.Contains(t, wait, "brief template", "the wait reason")
		require.Equal(t, PhasePending, st.Run.Phase, "still pending")
	})

	t.Run("hand-off", func(t *testing.T) {
		s, st, stop := stopped(t, testConfig())
		in := inputs(stop.Add(HandOffMax), healthy(v2, stop.Add(HandOffMax)))
		in.BriefErr = broken
		st, actions, wait := s.settle(st, in)
		require.Empty(t, actions, "no brief")
		require.Contains(t, wait, "brief template", "the wait reason")
		require.Equal(t, PhaseEnded, st.Run.Phase, "the run is over but not handed off")

		in.BriefErr = nil
		_, actions, _ = s.settle(st, in)
		require.Equal(t, []string{"post:" + MsgBrief, "post:" + MsgTag}, actions, "posted once it's fixed")
	})
}

func TestDecideIsPure(t *testing.T) {
	_, st := running(t, testConfig())
	before, err := st.Encode()
	require.NoError(t, err, "encoding")
	in := inputs(st.Run.Deadline, healthy(v2, st.Run.Deadline))

	d1, err := Decide(testConfig(), st, in)
	require.NoError(t, err, "deciding")
	d2, err := Decide(testConfig(), st, in)
	require.NoError(t, err, "deciding again")
	require.Equal(t, d1, d2, "the same inputs give the same decision")
	after, err := st.Encode()
	require.NoError(t, err, "encoding")
	require.Equal(t, string(before), string(after), "the input state isn't changed")
	require.Equal(t, ActUninstall, d1.Action.Kind, "the deadline uninstalls")
}

func TestSupersededWithoutAWorkspaceURL(t *testing.T) {
	cfg := testConfig()
	cfg.SlackWorkspaceURL = ""
	require.Equal(t, "superseded by `4.0.13.1`, announced in a new thread", supersededText(cfg, v3, fakeTS(2)),
		"no link without a workspace URL")
}

func TestEscape(t *testing.T) {
	require.Equal(t, "&lt;!channel&gt; &amp; &lt;@U1&gt;", esc("<!channel> & <@U1>"), "escaped")
}
