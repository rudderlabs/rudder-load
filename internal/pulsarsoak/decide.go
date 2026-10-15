package pulsarsoak

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// HandOffMin is the least time between the uninstall and the hand-off, HandOffMax the most: after it the brief
	// goes out even if the datasources haven't caught up.
	HandOffMin = 2 * time.Minute
	HandOffMax = 15 * time.Minute
	// unverifiedMax is how long src-router's discard may go unverified during a run before the run is aborted.
	unverifiedMax = 30 * time.Minute

	maxDecideSteps = 16
)

// ActionKind is what a decision does.
type ActionKind string

// The actions.
const (
	ActNone      ActionKind = ""
	ActPost      ActionKind = "post"
	ActInstall   ActionKind = "install"
	ActUninstall ActionKind = "uninstall"
)

// Target says which run a message belongs to.
type Target string

// The runs in the state.
const (
	TargetRun     Target = "run"
	TargetClosing Target = "closing"
)

// Action is one side effect. ActInstall installs the state's run with its ID and deadline; ActUninstall uninstalls
// the soak's release.
type Action struct {
	Kind ActionKind
	// For ActPost: the message Key of the Target run, its Text, and the thread to reply in, if any.
	Target   Target
	Key      string
	Text     string
	ThreadTS string
}

// Decision is what to do next.
type Decision struct {
	Action Action
	// State is the state once the action has succeeded. For a post, the tick records the message in it.
	State State
	// Wait says why there is nothing to do, when Action is ActNone.
	Wait string
}

// Inputs are everything a decision depends on besides the state and the configuration.
type Inputs struct {
	Now time.Time
	Obs Observations
	// Brief is the brief template, BriefErr why it can't be used, if it can't.
	Brief    string
	BriefErr error
}

// Decide returns the next action and the state after it. It is a pure function: the same arguments always give the
// same decision. A decision without an action may still change the state, e.g. to record a new version.
func Decide(cfg Config, st State, in Inputs) (Decision, error) {
	st = st.Clone()
	trackVersion(&st, in)
	for range maxDecideSteps {
		d, done, err := step(cfg, &st, in)
		if err != nil {
			return Decision{}, err
		}
		if done {
			return d, nil
		}
	}
	return Decision{}, errors.New("the decision didn't settle")
}

// trackVersion records the first version as the baseline, and then a new version every broker agrees on as the
// candidate, from when it was first seen.
func trackVersion(st *State, in Inputs) {
	p := in.Obs.Pulsar
	if p.Err != nil {
		return
	}
	switch {
	case st.Version == "":
		st.Version = p.Version
	case p.Version == "" || p.Version == st.Version:
		st.Candidate = nil
	case st.Candidate == nil || st.Candidate.Version != p.Version:
		st.Candidate = &Candidate{Version: p.Version, Since: in.Now.UTC()}
	}
}

// newVersion returns the candidate version if a run should start for it: every broker has reported it, and only it,
// for StableFor, every Pulsar pod is ready, and the brief template works.
func newVersion(st *State, in Inputs) (string, bool) {
	c, p := st.Candidate, in.Obs.Pulsar
	if c == nil || p.Err != nil || p.Version != c.Version || !p.Stable || !p.Ready || in.BriefErr != nil {
		return "", false
	}
	if in.Now.Sub(c.Since) < StableFor {
		return "", false
	}
	return c.Version, true
}

// step either applies one change to st and returns done=false, so the next step sees it, or returns a decision.
func step(cfg Config, st *State, in Inputs) (Decision, bool, error) {
	// A superseded run's last reply links to the new run's thread, so it waits for the new announcement.
	if c := st.Closing; c != nil {
		if c.Attempted(MsgSuperseded) {
			st.Last, st.Closing = c, nil
			return Decision{}, false, nil
		}
		if st.Run == nil || st.Run.Attempted(MsgAnnounce) {
			newThread := ""
			if st.Run != nil {
				newThread = st.Run.ThreadTS()
			}
			text, thread := reply(c, supersededText(cfg, c.SupersededBy, newThread), true)
			return post(*st, TargetClosing, MsgSuperseded, text, thread), true, nil
		}
	}
	if st.Run == nil {
		return stepIdle(cfg, st, in)
	}
	switch st.Run.Phase {
	case PhasePending:
		return stepPending(cfg, st, in)
	case PhaseRunning:
		return stepRunning(cfg, st, in)
	case PhaseStopped:
		return stepStopped(st, in)
	case PhaseEnded:
		return stepEnded(cfg, st, in)
	}
	return Decision{}, true, fmt.Errorf("run %s has an unknown phase %q", st.Run.ID, st.Run.Phase)
}

func stepIdle(cfg Config, st *State, in Inputs) (Decision, bool, error) {
	if v, ok := newVersion(st, in); ok {
		startRun(cfg, st, v, false, in.Now)
		return Decision{}, false, nil
	}
	if cfg.DryRun {
		switch {
		case st.Version == "":
			return wait(*st, "the dry run waits for the broker version"), true, nil
		case in.BriefErr != nil:
			return wait(*st, "the dry run waits for a brief template that works"), true, nil
		}
		startRun(cfg, st, st.Version, true, in.Now)
		return Decision{}, false, nil
	}
	return wait(*st, idleReason(st, in)), true, nil
}

func idleReason(st *State, in Inputs) string {
	c := st.Candidate
	switch {
	case in.Obs.Pulsar.Err != nil:
		return "the broker version is unknown"
	case c == nil:
		return "no new broker version since " + st.Version
	case in.BriefErr != nil:
		return "a run for " + c.Version + " waits for a brief template that works"
	}
	return fmt.Sprintf("%s has been on every broker since %s; a run starts once it has been stable for %s "+
		"and every Pulsar pod is ready", c.Version, FormatWindowTime(c.Since), StableFor)
}

func stepPending(cfg Config, st *State, in Inputs) (Decision, bool, error) {
	r := st.Run
	if !r.Attempted(MsgAnnounce) {
		return post(*st, TargetRun, MsgAnnounce, announceText(cfg, r), ""), true, nil
	}
	if v, ok := newVersion(st, in); ok {
		supersede(cfg, st, v, in.Now)
		return Decision{}, false, nil
	}
	if load := in.Obs.OtherLoad; load.Err != nil {
		return wait(*st, "the run waits: can't tell whether another load test is running"), true, nil
	} else if len(load.Deployments) > 0 {
		if !r.Attempted(MsgBlockedLoad) {
			text, thread := reply(r, blockedLoadText(cfg), true)
			return post(*st, TargetRun, MsgBlockedLoad, text, thread), true, nil
		}
		return wait(*st, "the run waits for other load tests: "+strings.Join(load.Deployments, ", ")), true, nil
	}
	switch d := in.Obs.Discard; d.State {
	case DiscardUnknown:
		return wait(*st, "the run waits: can't tell whether src-router discards"), true, nil
	case DiscardFailed:
		if !r.Attempted(MsgBlockedDiscard) {
			text, thread := reply(r, blockedDiscardText(cfg), true)
			return post(*st, TargetRun, MsgBlockedDiscard, text, thread), true, nil
		}
		return wait(*st, "the run waits for src-router to discard: "+d.Reason), true, nil
	}
	if in.BriefErr != nil {
		return wait(*st, "the run waits for a brief template that works"), true, nil
	}
	next := st.Clone()
	start := in.Now.UTC().Truncate(time.Second)
	next.Run.Phase = PhaseRunning
	next.Run.StartedAt = start
	next.Run.Deadline = start.Add(time.Duration(r.Length))
	return Decision{Action: Action{Kind: ActInstall}, State: next}, true, nil
}

func stepRunning(cfg Config, st *State, in Inputs) (Decision, bool, error) {
	r, now := st.Run, in.Now.UTC()
	switch in.Obs.Discard.State {
	case DiscardFailed:
		return uninstall(*st, func(s *State) { end(s.Run, EndAborted, now) }), true, nil
	case DiscardUnknown:
		if r.UnverifiedSince.IsZero() {
			r.UnverifiedSince = now
			return Decision{}, false, nil
		}
		if now.Sub(r.UnverifiedSince) >= unverifiedMax {
			return uninstall(*st, func(s *State) { end(s.Run, EndUnverified, now) }), true, nil
		}
	case DiscardOK:
		if !r.UnverifiedSince.IsZero() {
			r.UnverifiedSince = time.Time{}
			return Decision{}, false, nil
		}
	}
	if (r.Attempted(MsgBlockedLoad) || r.Attempted(MsgBlockedDiscard)) && !r.Attempted(MsgStarted) {
		text, thread := reply(r, startedText(r), true)
		return post(*st, TargetRun, MsgStarted, text, thread), true, nil
	}
	if !now.Before(r.Deadline) {
		return uninstall(*st, func(s *State) {
			s.Run.Phase = PhaseStopped
			s.Run.StoppedAt = now
		}), true, nil
	}
	if v, ok := newVersion(st, in); ok {
		return uninstall(*st, func(s *State) { supersede(cfg, s, v, now) }), true, nil
	}
	return wait(*st, "the run goes on until "+FormatWindowTime(r.Deadline)), true, nil
}

func stepStopped(st *State, in Inputs) (Decision, bool, error) {
	r := st.Run
	since := in.Now.Sub(r.StoppedAt)
	caughtUp := since >= HandOffMin && in.Obs.Fresh.NewerThan(r.StoppedAt)
	if !caughtUp && since < HandOffMax {
		return wait(*st, "the hand-off waits for every datasource to have data from after the stop"), true, nil
	}
	r.Phase, r.EndReason = PhaseEnded, EndCompleted
	return Decision{}, false, nil
}

func stepEnded(cfg Config, st *State, in Inputs) (Decision, bool, error) {
	r := st.Run
	switch r.EndReason {
	case EndCompleted:
		if !r.Attempted(MsgBrief) {
			if in.BriefErr != nil {
				return wait(*st, "the hand-off waits for a brief template that works"), true, nil
			}
			brief, err := FillBrief(in.Brief, cfg.RudderAssistUserID, r.StartedAt, r.WindowEnd())
			if err != nil {
				return Decision{}, true, err
			}
			text, thread := reply(r, brief, false)
			if err := checkBriefSize(text); err != nil {
				return Decision{}, true, err
			}
			return post(*st, TargetRun, MsgBrief, text, thread), true, nil
		}
		if !r.DryRun && !r.Attempted(MsgTag) {
			text, thread := reply(r, tagText(cfg, r), false)
			return post(*st, TargetRun, MsgTag, text, thread), true, nil
		}
	case EndAborted, EndUnverified:
		if !r.Attempted(MsgAborted) {
			text, thread := reply(r, abortedText(r), true)
			return post(*st, TargetRun, MsgAborted, text, thread), true, nil
		}
	}
	st.Last, st.Run = r, nil
	return Decision{}, false, nil
}

// startRun starts a run for version. A dry run keeps the version as it is.
func startRun(cfg Config, st *State, version string, dryRun bool, now time.Time) {
	now = now.UTC().Truncate(time.Second)
	from := st.Version
	if from == "" {
		from = version
	}
	id := "pulsar-soak-test-" + version + "-" + now.Format("20060102T150405Z")
	if dryRun {
		id = "pulsar-soak-test-dry-run-" + version + "-" + now.Format("20060102T150405Z")
	}
	st.Run = &Run{
		ID:          id,
		Version:     version,
		FromVersion: from,
		DryRun:      dryRun,
		Length:      Duration(cfg.RunLength),
		Phase:       PhasePending,
		DetectedAt:  now,
		Posts:       map[string]Post{},
	}
	if !dryRun {
		st.Version, st.Candidate = version, nil
	}
}

// supersede ends the current run for a newer version and starts a run for it.
func supersede(cfg Config, st *State, version string, now time.Time) {
	old := st.Run
	end(old, EndSuperseded, now)
	old.SupersededBy = version
	if st.Closing != nil {
		st.Last = st.Closing
	}
	st.Closing = old
	startRun(cfg, st, version, false, now)
}

func end(r *Run, reason EndReason, now time.Time) {
	r.Phase, r.EndReason = PhaseEnded, reason
	if r.StoppedAt.IsZero() {
		r.StoppedAt = now
	}
}

func post(st State, target Target, key, text, threadTS string) Decision {
	return Decision{
		Action: Action{Kind: ActPost, Target: target, Key: key, Text: text, ThreadTS: threadTS},
		State:  st,
	}
}

func uninstall(st State, change func(*State)) Decision {
	next := st.Clone()
	change(&next)
	return Decision{Action: Action{Kind: ActUninstall}, State: next}
}

func wait(st State, reason string) Decision {
	return Decision{State: st, Wait: reason}
}
