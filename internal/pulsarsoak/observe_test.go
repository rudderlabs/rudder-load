package pulsarsoak

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"rudder-load/internal/pulsarsoak/grafana"
	"rudder-load/internal/pulsarsoak/kube"
)

func sample(value float64, labels ...string) grafana.Sample {
	l := map[string]string{}
	for i := 0; i+1 < len(labels); i += 2 {
		l[labels[i]] = labels[i+1]
	}
	return grafana.Sample{Labels: l, Value: value}
}

const snImage = "registry.example.com/pulsar:"

// pulsarResults are the results of the four Pulsar queries.
type pulsarResults struct {
	versions, recent, containers, ready []grafana.Sample
}

// pulsarSamples are the results of the four Pulsar queries for a healthy cluster on version, as a StreamNative cluster
// answers them: two brokers, three bookies and three ZooKeepers.
func pulsarSamples(version string) pulsarResults {
	versions := []grafana.Sample{
		sample(1, "pod", "pc-broker-0", "version", version),
		sample(1, "pod", "pc-broker-1", "version", version),
	}
	recent := []grafana.Sample{sample(2, "version", version)}
	containers := []grafana.Sample{
		sample(1, "pod", "pc-broker-0", "container", "pulsar-broker", "image", snImage+version),
		sample(1, "pod", "pc-broker-1", "container", "pulsar-broker", "image", snImage+version),
		sample(1, "pod", "pc-bk-0", "container", "bookie", "image", snImage+version),
		sample(1, "pod", "pc-bk-1", "container", "bookie", "image", snImage+version),
		sample(1, "pod", "pc-bk-2", "container", "bookie", "image", snImage+version),
		sample(1, "pod", "pc-zk-0", "container", "zookeeper", "image", snImage+version),
		sample(1, "pod", "pc-zk-1", "container", "zookeeper", "image", snImage+version),
		sample(1, "pod", "pc-zk-2", "container", "zookeeper", "image", snImage+version),
	}
	var ready []grafana.Sample
	for _, s := range containers {
		ready = append(ready, sample(1, "pod", s.Labels["pod"]))
	}
	ready = append(ready, sample(0, "pod", "prometheus-0"))
	return pulsarResults{versions: versions, recent: recent, containers: containers, ready: ready}
}

func TestDigestPulsar(t *testing.T) {
	const v = "4.0.12.5"
	tests := []struct {
		name       string
		change     func(*pulsarResults)
		wantV      string
		wantStable bool
		wantReady  bool
		wantIssue  string
	}{
		{
			name:       "healthy",
			wantV:      v,
			wantStable: true,
			wantReady:  true,
		},
		{
			name: "brokers disagree",
			change: func(p *pulsarResults) {
				p.versions[1] = sample(1, "pod", "pc-broker-1", "version", "4.0.12.4")
			},
			wantIssue: "different versions: 4.0.12.4, 4.0.12.5",
		},
		{
			name: "a broker reports two versions",
			change: func(p *pulsarResults) {
				p.versions = append(p.versions, sample(1, "pod", "pc-broker-1", "version", "4.0.12.4"))
			},
			wantIssue: "different versions",
		},
		{
			name:      "no broker reports",
			change:    func(p *pulsarResults) { p.versions = nil },
			wantIssue: "no broker reports",
		},
		{
			name: "an odd version label",
			change: func(p *pulsarResults) {
				p.versions[0] = sample(1, "pod", "pc-broker-0", "version", "<!channel>")
				p.versions[1] = sample(1, "pod", "pc-broker-1", "version", "<!channel>")
			},
			wantIssue: "unexpected version",
		},
		{
			name: "another version in the last 30 minutes",
			change: func(p *pulsarResults) {
				p.recent = []grafana.Sample{sample(1, "version", "4.0.12.4"), sample(2, "version", v)}
			},
			wantV:     v,
			wantReady: true,
			wantIssue: "reported 4.0.12.4, 4.0.12.5",
		},
		{
			name:       "a bookie isn't ready",
			change:     func(p *pulsarResults) { p.ready[3] = sample(0, "pod", "pc-bk-1") },
			wantV:      v,
			wantStable: true,
			wantIssue:  "pod pc-bk-1 isn't ready",
		},
		{
			name:       "a ZooKeeper pod has no readiness series",
			change:     func(p *pulsarResults) { p.ready = p.ready[:5] },
			wantV:      v,
			wantStable: true,
			wantIssue:  "pod pc-zk-0 isn't ready",
		},
		{
			name:       "no ZooKeeper pods",
			change:     func(p *pulsarResults) { p.containers = p.containers[:5] },
			wantV:      v,
			wantStable: true,
			wantIssue:  "no pod with a zookeeper container",
		},
		{
			name: "a broker's image is still the old one",
			change: func(p *pulsarResults) {
				p.containers[1] = sample(1, "pod", "pc-broker-1", "container", "pulsar-broker",
					"image", snImage+"4.0.12.4")
			},
			wantV:      v,
			wantStable: true,
			wantIssue:  "whose tag doesn't carry 4.0.12.5",
		},
		{
			name:       "a broker pod reports no version",
			change:     func(p *pulsarResults) { p.versions = p.versions[:1] },
			wantV:      v,
			wantStable: true,
			wantIssue:  "broker pc-broker-1 reports no version",
		},
		{
			name:       "a broker has no container info",
			change:     func(p *pulsarResults) { p.containers = p.containers[1:] },
			wantV:      v,
			wantStable: true,
			wantIssue:  "broker pc-broker-0 has no kube_pod_container_info",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := pulsarSamples(v)
			if tt.change != nil {
				tt.change(&res)
			}
			p := DigestPulsar(res.versions, res.recent, res.containers, res.ready)
			require.Equal(t, tt.wantV, p.Version, "version; problems: %v", p.Problems)
			require.Equal(t, tt.wantStable, p.Stable, "stable; problems: %v", p.Problems)
			require.Equal(t, tt.wantReady, p.Ready, "ready; problems: %v", p.Problems)
			if tt.wantIssue == "" {
				require.Empty(t, p.Problems, "no problems")
			} else {
				require.Contains(t, strings.Join(p.Problems, "\n"), tt.wantIssue, "problems")
			}
		})
	}
}

func TestTagCarries(t *testing.T) {
	tests := []struct {
		image string
		want  bool
	}{
		{snImage + "4.0.12.5", true},
		{"repo/img:v4.0.12.5-amd64", true},
		{"registry:5000/img:4.0.12.5", true},
		{"repo/img:4.0.12.5@sha256:abc", true},
		{"repo/img:4.0.12.50", false},
		{"repo/img:14.0.12.5", false},
		{"repo/img:4.0.12.4", false},
		{"registry:5000/img", false},
		{"repo/img@sha256:4.0.12.5", false},
		{"img", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, TagCarries(tt.image, "4.0.12.5"), "%s", tt.image)
	}
}

// fakeMetrics answers queries by their text.
type fakeMetrics struct {
	answers map[string][]grafana.Sample
	errs    map[string]error
	logs    time.Time
	logsErr error
	queries []string
}

func (f *fakeMetrics) Query(_ context.Context, uid, query string, _ time.Time) ([]grafana.Sample, error) {
	f.queries = append(f.queries, uid+" "+query)
	return f.answers[query], f.errs[query]
}

func (f *fakeMetrics) LatestLog(_ context.Context, uid, query string, _, _ time.Time) (time.Time, bool, error) {
	f.queries = append(f.queries, uid+" "+query)
	return f.logs, !f.logs.IsZero(), f.logsErr
}

type fakeCluster struct {
	others     []string
	othersErr  error
	discard    kube.DiscardStatus
	discardErr error
}

func (f *fakeCluster) OtherLoadTests(context.Context) ([]string, error) { return f.others, f.othersErr }

func (f *fakeCluster) SrcRouterDiscard(context.Context) (kube.DiscardStatus, error) {
	return f.discard, f.discardErr
}

func healthyMetrics(cfg Config, now time.Time) *fakeMetrics {
	q := QueriesFor(cfg)
	res := pulsarSamples("4.0.12.5")
	return &fakeMetrics{
		answers: map[string][]grafana.Sample{
			q.BrokerVersions: res.versions, q.RecentVersions: res.recent,
			q.PodContainers: res.containers, q.PodReady: res.ready,
			q.AppFreshness:  {sample(float64(now.Add(-10 * time.Second).Unix()))},
			q.KubeFreshness: {sample(float64(now.Add(-20 * time.Second).Unix()))},
		},
		errs: map[string]error{},
		logs: now.Add(-5 * time.Second),
	}
}

func TestLiveObserver(t *testing.T) {
	cfg := testConfig()
	now := t0
	m := healthyMetrics(cfg, now)
	c := &fakeCluster{others: []string{"rudder-load-other-0"}, discard: kube.DiscardStatus{OK: true}}

	obs := NewLiveObserver(cfg, m, c).Observe(context.Background(), now)
	require.NoError(t, obs.Err(), "every observation works")
	require.Equal(t, Pulsar{Version: "4.0.12.5", Stable: true, Ready: true}, obs.Pulsar, "Pulsar")
	require.Equal(t, []string{"rudder-load-other-0"}, obs.OtherLoad.Deployments, "other load tests")
	require.Equal(t, DiscardOK, obs.Discard.State, "discard")
	require.Equal(t, Freshness{
		AppMetrics:  now.Add(-10 * time.Second),
		KubeMetrics: now.Add(-20 * time.Second),
		Logs:        now.Add(-5 * time.Second),
	}, obs.Fresh, "freshness")

	q := QueriesFor(cfg)
	require.ElementsMatch(t, []string{
		"kube " + q.BrokerVersions, "kube " + q.RecentVersions, "kube " + q.PodContainers, "kube " + q.PodReady,
		"app " + q.NonDiscarding, "app " + q.AppFreshness, "kube " + q.KubeFreshness, "logs " + q.Logs,
	}, m.queries, "each query goes to its datasource")
}

func TestQueries(t *testing.T) {
	q := QueriesFor(testConfig())
	require.Equal(t, Queries{
		BrokerVersions: `max by (pod, version) (pulsar_version_info{cloud_streamnative_io_cluster="test-cluster", ` +
			`cloud_streamnative_io_component="broker"})`,
		RecentVersions: `count by (version) (max_over_time(pulsar_version_info{` +
			`cloud_streamnative_io_cluster="test-cluster", cloud_streamnative_io_component="broker"}[30m]))`,
		PodContainers: `max by (pod, container, image) (kube_pod_container_info{namespace="test-cluster:o-abc", ` +
			`container=~"pulsar-broker|bookie|zookeeper"})`,
		PodReady: `min by (pod) (kube_pod_status_ready{namespace="test-cluster:o-abc", condition="true"})`,
		NonDiscarding: `count(last_over_time(src_router_http_requests{namespace="soak-ns", ` +
			`discard!="true"}[20m]))`,
		AppFreshness: `max(timestamp({__name__=~"ingestion_svc_config|src_router_config", namespace="soak-ns"}))`,
		KubeFreshness: `max(timestamp(pulsar_version_info{cloud_streamnative_io_cluster="test-cluster", ` +
			`cloud_streamnative_io_component="broker"}))`,
		Logs: `{namespace="soak-ns"}`,
	}, q, "queries")
}

func TestLiveObserverDiscard(t *testing.T) {
	cfg := testConfig()
	q := QueriesFor(cfg)
	boom := errors.New("boom")
	tests := []struct {
		name       string
		k8s        kube.DiscardStatus
		k8sErr     error
		nonDiscard []grafana.Sample
		metricErr  error
		want       DiscardState
		wantReason string
		wantErr    bool
	}{
		{name: "both agree", k8s: kube.DiscardStatus{OK: true}, want: DiscardOK},
		{name: "Kubernetes says no", k8s: kube.DiscardStatus{Reason: "pod x: not true"}, want: DiscardFailed,
			wantReason: "pod x"},
		{name: "the metric says no", k8s: kube.DiscardStatus{OK: true}, nonDiscard: []grafana.Sample{sample(3)},
			want: DiscardFailed, wantReason: "3 src_router_http_requests series in soak-ns lack discard=\"true\""},
		{name: "Kubernetes fails", k8sErr: boom, want: DiscardUnknown, wantErr: true},
		{name: "the metric fails", k8s: kube.DiscardStatus{OK: true}, metricErr: boom, want: DiscardUnknown,
			wantErr: true},
		{name: "a failed check wins over an error", k8s: kube.DiscardStatus{Reason: "no"}, metricErr: boom,
			want: DiscardFailed, wantReason: "no", wantErr: true},
		{name: "a failed metric wins over an error", k8sErr: boom, nonDiscard: []grafana.Sample{sample(1)},
			want: DiscardFailed, wantReason: "lack discard", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := healthyMetrics(cfg, t0)
			m.answers[q.NonDiscarding] = tt.nonDiscard
			m.errs[q.NonDiscarding] = tt.metricErr
			c := &fakeCluster{discard: tt.k8s, discardErr: tt.k8sErr}

			d := NewLiveObserver(cfg, m, c).Observe(context.Background(), t0).Discard
			require.Equal(t, tt.want, d.State, "state")
			require.Contains(t, d.Reason, tt.wantReason, "reason")
			require.Equal(t, tt.wantErr, d.Err != nil, "error: %v", d.Err)
		})
	}
}

func TestLiveObserverErrors(t *testing.T) {
	cfg := testConfig()
	q := QueriesFor(cfg)
	m := healthyMetrics(cfg, t0)
	m.errs[q.PodReady] = errors.New("mimir is down")
	m.logsErr = errors.New("loki is down")
	c := &fakeCluster{othersErr: errors.New("forbidden"), discard: kube.DiscardStatus{OK: true}}

	obs := NewLiveObserver(cfg, m, c).Observe(context.Background(), t0)
	require.ErrorContains(t, obs.Pulsar.Err, "mimir is down", "a failed Pulsar query makes Pulsar unknown")
	require.Empty(t, obs.Pulsar.Version, "no version")
	require.ErrorContains(t, obs.OtherLoad.Err, "forbidden", "the load test check failed")
	require.ErrorContains(t, obs.Fresh.Err, "loki is down", "the log check failed")
	require.True(t, obs.Fresh.Logs.IsZero(), "no log time")
	require.False(t, obs.Fresh.AppMetrics.IsZero(), "the other datasources still answered")
	require.Error(t, obs.Err(), "the errors are joined")
}
