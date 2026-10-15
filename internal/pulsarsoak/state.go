package pulsarsoak

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"time"
)

// StateSchema is the version of the state's JSON layout.
const StateSchema = 1

// State is what the Pulsar soak test remembers between ticks.
type State struct {
	Schema int `json:"schema"`
	// Version is the broker version of the latest run, or the baseline the first tick recorded.
	Version string `json:"version,omitempty"`
	// Candidate is a new broker version every broker reports, waiting to be stable for long enough.
	Candidate *Candidate `json:"candidate,omitempty"`
	// Run is the current run.
	Run *Run `json:"run,omitempty"`
	// Closing is a superseded run whose last reply, pointing at the new run, is still due.
	Closing *Run `json:"closing,omitempty"`
	// Last is the latest finished run, kept for the record.
	Last *Run `json:"last,omitempty"`
}

// Candidate is a broker version first seen on every broker at Since.
type Candidate struct {
	Version string    `json:"version"`
	Since   time.Time `json:"since"`
}

// Phase is where a run is.
type Phase string

// The phases of a run, in order.
const (
	// PhasePending runs are announced and wait for the namespace to be free and src-router to discard.
	PhasePending Phase = "pending"
	// PhaseRunning runs have rudder-load installed.
	PhaseRunning Phase = "running"
	// PhaseStopped runs have rudder-load uninstalled after the deadline and wait for the metrics to catch up.
	PhaseStopped Phase = "stopped"
	// PhaseEnded runs are over; their last replies may still be due.
	PhaseEnded Phase = "ended"
)

// EndReason says why a run ended.
type EndReason string

// Why runs end.
const (
	EndCompleted  EndReason = "completed"
	EndAborted    EndReason = "aborted"    // src-router stopped discarding
	EndUnverified EndReason = "unverified" // src-router's discard couldn't be verified for too long
	EndSuperseded EndReason = "superseded" // a newer broker version arrived
)

// PostStatus is what is known about a Slack message.
type PostStatus string

// A message is recorded as pending before it's posted, and as posted or unknown after. A message with any record
// is never posted again: a pending one means the tick stopped while posting, so it may have been posted.
const (
	PostPending PostStatus = "pending"
	PostPosted  PostStatus = "posted"
	PostUnknown PostStatus = "unknown"
)

// Post records a Slack message.
type Post struct {
	Status PostStatus `json:"status"`
	TS     string     `json:"ts,omitempty"`
	At     time.Time  `json:"at"`
}

// The messages of a run.
const (
	MsgAnnounce       = "announce"
	MsgBlockedLoad    = "blocked-load-test"
	MsgBlockedDiscard = "blocked-discard"
	MsgStarted        = "started"
	MsgAborted        = "aborted"
	MsgSuperseded     = "superseded"
	MsgBrief          = "brief"
	MsgTag            = "tag"
)

// Run is one run of the Pulsar soak test.
type Run struct {
	// ID is rudder-load's LOAD_RUN_ID.
	ID          string    `json:"id"`
	Version     string    `json:"version"`
	FromVersion string    `json:"fromVersion"`
	DryRun      bool      `json:"dryRun,omitempty"`
	Length      Duration  `json:"length"`
	Phase       Phase     `json:"phase"`
	DetectedAt  time.Time `json:"detectedAt"`
	// StartedAt is when rudder-load was installed, Deadline when it stops sending.
	StartedAt time.Time `json:"startedAt,omitzero"`
	Deadline  time.Time `json:"deadline,omitzero"`
	// StoppedAt is when rudder-load was uninstalled.
	StoppedAt    time.Time `json:"stoppedAt,omitzero"`
	EndReason    EndReason `json:"endReason,omitempty"`
	SupersededBy string    `json:"supersededBy,omitempty"`
	// UnverifiedSince is when the checks of src-router's discard started failing to give an answer.
	UnverifiedSince time.Time       `json:"unverifiedSince,omitzero"`
	Posts           map[string]Post `json:"posts,omitempty"`
}

// Attempted reports whether the message key was posted, or may have been.
func (r *Run) Attempted(key string) bool {
	_, ok := r.Posts[key]
	return ok
}

// ThreadTS returns the timestamp of the run's announcement, or "" if it isn't known.
func (r *Run) ThreadTS() string {
	if p, ok := r.Posts[MsgAnnounce]; ok && p.Status == PostPosted {
		return p.TS
	}
	return ""
}

// WindowEnd is when the load stopped: the deadline, or the uninstall if that came first.
func (r *Run) WindowEnd() time.Time {
	if !r.StoppedAt.IsZero() && r.StoppedAt.Before(r.Deadline) {
		return r.StoppedAt
	}
	return r.Deadline
}

func (r *Run) clone() *Run {
	if r == nil {
		return nil
	}
	c := *r
	c.Posts = maps.Clone(r.Posts)
	return &c
}

// Clone returns a deep copy of the state.
func (s State) Clone() State {
	c := s
	if s.Candidate != nil {
		cand := *s.Candidate
		c.Candidate = &cand
	}
	c.Run = s.Run.clone()
	c.Closing = s.Closing.clone()
	c.Last = s.Last.clone()
	return c
}

// Encode returns the state as JSON.
func (s State) Encode() ([]byte, error) {
	s.Schema = StateSchema
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the Pulsar soak test state: %w", err)
	}
	return data, nil
}

// DecodeState parses a state written by Encode. No data is the empty state of a Pulsar soak test that never ticked.
func DecodeState(data []byte) (State, error) {
	if len(data) == 0 {
		return State{Schema: StateSchema}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s State
	if err := dec.Decode(&s); err != nil {
		return State{}, fmt.Errorf("decoding the Pulsar soak test state: %w", err)
	}
	if s.Schema != StateSchema {
		return State{}, fmt.Errorf("the Pulsar soak test state has schema %d, this version reads %d",
			s.Schema, StateSchema)
	}
	return s, nil
}

// Duration is a time.Duration that reads and writes JSON as a string such as "24h0m0s".
type Duration time.Duration

// MarshalJSON writes the duration as a string.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// UnmarshalJSON reads a duration string.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("a duration must be a string: %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parsing a duration: %w", err)
	}
	*d = Duration(v)
	return nil
}
