package pulsarsoak

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

const (
	v1 = "4.0.12.5"
	v2 = "4.0.12.6"
	v3 = "4.0.13.1"

	testBrief = "@RUDDER_ASSIST@ assess the soak from `@WINDOW_START@` to `@WINDOW_END@`.\n"
)

func testConfig() Config {
	return Config{
		Namespace:           "soak-ns",
		Release:             "rudder-load-pulsar-soak-test",
		ChartPath:           "artifacts/helm",
		ProfilePath:         "artifacts/helm/pulsar_soak_test_values.yaml",
		DeploymentName:      "rudder-load-pulsar-soak-test-oz",
		HTTPEndpoint:        "https://ingest.example.com",
		WriteKeys:           WriteKeys{"wk-secret-1", "wk-secret-2"},
		SlackChannel:        "C0CHANNEL",
		SlackWorkspaceURL:   "https://example.slack.com",
		RudderAssistUserID:  "U0ASSIST",
		ServerEngGroupID:    "S0SERVERENG",
		AppMetricsUID:       "app",
		KubeMetricsUID:      "kube",
		LogsUID:             "logs",
		PulsarCluster:       "test-cluster",
		PulsarKubeNamespace: "test-cluster:o-abc",
		RunLength:           24 * time.Hour,
		RunURL:              "https://github.com/org/repo/actions/runs/42",
	}
}

// healthy is a cluster on version, stable and ready, with nothing in the way of a run and data up to now.
func healthy(version string, now time.Time) Observations {
	return Observations{
		Pulsar:  Pulsar{Version: version, Stable: true, Ready: true},
		Discard: Discard{State: DiscardOK},
		Fresh:   Freshness{AppMetrics: now, KubeMetrics: now, Logs: now},
	}
}

func inputs(now time.Time, obs Observations) Inputs {
	return Inputs{Now: now, Obs: obs, Brief: testBrief}
}

// fakeTS returns the ts of the n-th message.
func fakeTS(n int) string { return fmt.Sprintf("1792072800.%06d", n) }

// step describes an action for comparisons: post:<key>, post:<key>@closing, install or uninstall.
func describe(a Action) string {
	switch a.Kind {
	case ActPost:
		if a.Target == TargetClosing {
			return "post:" + a.Key + "@closing"
		}
		return "post:" + a.Key
	case ActNone:
		return "none"
	}
	return string(a.Kind)
}

// settler applies decisions as if every action succeeded, numbering the posts it makes.
type settler struct {
	t     *testing.T
	cfg   Config
	posts int
	// sent are the posts made, in order.
	sent []Action
}

// settle decides and applies until there's nothing to do, and returns the state, the actions and the last wait
// reason.
func (s *settler) settle(st State, in Inputs) (State, []string, string) {
	s.t.Helper()
	var actions []string
	for range 30 {
		d, err := Decide(s.cfg, st, in)
		require.NoError(s.t, err, "deciding")
		if d.Action.Kind == ActNone {
			return d.State, actions, d.Wait
		}
		actions = append(actions, describe(d.Action))
		st = d.State
		if d.Action.Kind == ActPost {
			s.posts++
			s.sent = append(s.sent, d.Action)
			r := st.Run
			if d.Action.Target == TargetClosing {
				r = st.Closing
			}
			r.Posts[d.Action.Key] = Post{Status: PostPosted, TS: fakeTS(s.posts), At: in.Now}
		}
	}
	s.t.Fatal("the decisions didn't settle")
	return State{}, nil, ""
}

// lastSent returns the last post made with key.
func (s *settler) lastSent(key string) Action {
	s.t.Helper()
	for i := len(s.sent) - 1; i >= 0; i-- {
		if s.sent[i].Key == key {
			return s.sent[i]
		}
	}
	s.t.Fatalf("nothing was posted as %s", key)
	return Action{}
}

// baseline is the state after the first tick saw v1.
func baseline() State { return State{Schema: StateSchema, Version: v1} }
