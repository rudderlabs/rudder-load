// Package pulsarsoak reconciles the automated Pulsar soak test: when the brokers of the Pulsar test cluster move to a
// new version, it announces the run in Slack, installs rudder-load for a fixed time, uninstalls it, and hands the run
// to rudder-assist with a brief.
//
// Each tick reads the state from a Kubernetes Secret, observes the cluster and the metrics, and lets Decide, a pure
// function, pick the next action, until there is nothing left to do. The state is written back after every action.
package pulsarsoak

import (
	"encoding/json"
	"strings"
	"time"
)

// Config is how and where the Pulsar soak test runs. It holds no tokens; the write keys are kept in a type that never
// prints them.
type Config struct {
	// Namespace is where rudder-load, ingestion and src-router run, and where the state Secret lives.
	Namespace string
	// Release is the Pulsar soak test's Helm release, ChartPath the rudder-load chart and ProfilePath its values file.
	Release     string
	ChartPath   string
	ProfilePath string
	// DeploymentName overrides the profile's deployment.name when it isn't empty. HTTPEndpoint is the installed
	// release's HTTP_ENDPOINT.
	DeploymentName string
	HTTPEndpoint   string
	// WriteKeys are the sources rudder-load sends to.
	WriteKeys WriteKeys

	// SrcRouterDeployment is src-router's Deployment, which must set SrcRouterDiscardEnv to "true".
	SrcRouterDeployment string
	SrcRouterDiscardEnv string

	// SlackChannel is the channel the Pulsar soak test posts in. SlackWorkspaceURL, such as
	// https://example.slack.com, is used to link threads; without it, links are left out.
	SlackChannel      string
	SlackWorkspaceURL string
	// RudderAssistUserID is mentioned in the brief, ServerEngGroupID is tagged once the brief is posted.
	RudderAssistUserID string
	ServerEngGroupID   string

	// AppMetricsUID, KubeMetricsUID and LogsUID are the Grafana datasources for application metrics (Mimir),
	// Kubernetes and Pulsar metrics (Thanos), and logs (Loki).
	AppMetricsUID  string
	KubeMetricsUID string
	LogsUID        string
	// PulsarCluster is the cloud_streamnative_io_cluster label value of the Pulsar cluster, and PulsarKubeNamespace
	// the namespace label of its pods in kube-state-metrics, such as cluster:namespace.
	PulsarCluster       string
	PulsarKubeNamespace string

	// RunLength is how long rudder-load sends. DryRun starts a run without a version change.
	RunLength time.Duration
	DryRun    bool
	// RunURL links the workflow run that ticks, when known.
	RunURL string
}

// WriteKeys are source write keys. They print as [redacted], so logging a Config can't leak them.
type WriteKeys []string

// Joined returns the keys as the comma-separated list rudder-load's SOURCES expects.
func (k WriteKeys) Joined() string { return strings.Join(k, ",") }

// String hides the keys.
func (k WriteKeys) String() string { return "[redacted]" }

// GoString hides the keys from %#v.
func (k WriteKeys) GoString() string { return "[redacted]" }

// MarshalJSON hides the keys from JSON.
func (k WriteKeys) MarshalJSON() ([]byte, error) { return json.Marshal("[redacted]") }
