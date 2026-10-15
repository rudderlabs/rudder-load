package pulsarsoak

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"rudder-load/internal/pulsarsoak/grafana"
	"rudder-load/internal/pulsarsoak/kube"
)

const (
	// StableFor is how long every broker must have reported only the new version before a run starts.
	StableFor = 30 * time.Minute
	// discardWindow is how far back the metric check of src-router's discard looks, a little over a tick.
	discardWindow = "20m"
	// logsLookback is how far back the newest log line is looked for.
	logsLookback = 30 * time.Minute

	// The containers of the pods that must be ready, as kube_pod_container_info names them.
	containerBroker    = "pulsar-broker"
	containerBookie    = "bookie"
	containerZooKeeper = "zookeeper"

	maxProblems = 10
)

var versionRE = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)

// Observations are what a tick saw. A field with an error is unknown; Decide never acts on what it can't see.
type Observations struct {
	Pulsar    Pulsar
	OtherLoad OtherLoad
	Discard   Discard
	Fresh     Freshness
}

// Err joins the errors of every observation.
func (o Observations) Err() error {
	return errors.Join(o.Pulsar.Err, o.OtherLoad.Err, o.Discard.Err, o.Fresh.Err)
}

// Pulsar is the state of the Pulsar cluster's brokers, bookies and ZooKeeper.
type Pulsar struct {
	// Version is the version every broker reports, or "" if they don't agree or none reports one.
	Version string
	// Stable is set when the brokers reported no other version than Version in the last StableFor.
	Stable bool
	// Ready is set when every broker, bookie and ZooKeeper pod is ready and every broker's image tag carries Version.
	Ready bool
	// Problems say why Version is empty or Stable or Ready is unset, for the log.
	Problems []string
	Err      error
}

// OtherLoad lists the rudder-load Deployments in the namespace that aren't the soak's.
type OtherLoad struct {
	Deployments []string
	Err         error
}

// DiscardState is what is known about src-router's discard.
type DiscardState int

// An error leaves the discard unknown, unless another check failed.
const (
	DiscardUnknown DiscardState = iota
	DiscardOK
	DiscardFailed
)

// String names the state for the log.
func (s DiscardState) String() string {
	switch s {
	case DiscardOK:
		return "ok"
	case DiscardFailed:
		return "failed"
	}
	return "unknown"
}

// Discard says whether src-router discards what it consumes, as both the Kubernetes and the metric checks see it.
type Discard struct {
	State  DiscardState
	Reason string
	Err    error
}

// Freshness has the newest sample of each datasource the brief uses, zero when unknown.
type Freshness struct {
	AppMetrics  time.Time
	KubeMetrics time.Time
	Logs        time.Time
	Err         error
}

// NewerThan reports whether every datasource has a sample newer than t.
func (f Freshness) NewerThan(t time.Time) bool {
	return f.AppMetrics.After(t) && f.KubeMetrics.After(t) && f.Logs.After(t)
}

// Metrics queries Grafana's datasources.
type Metrics interface {
	Query(ctx context.Context, uid, query string, at time.Time) ([]grafana.Sample, error)
	LatestLog(ctx context.Context, uid, query string, start, end time.Time) (time.Time, bool, error)
}

// Cluster checks the namespace in Kubernetes.
type Cluster interface {
	OtherLoadTests(ctx context.Context) ([]string, error)
	SrcRouterDiscard(ctx context.Context) (kube.DiscardStatus, error)
}

// LiveObserver observes the live cluster and metrics.
type LiveObserver struct {
	cfg     Config
	metrics Metrics
	cluster Cluster
}

// NewLiveObserver returns an observer for cfg.
func NewLiveObserver(cfg Config, metrics Metrics, cluster Cluster) *LiveObserver {
	return &LiveObserver{cfg: cfg, metrics: metrics, cluster: cluster}
}

// Observe gathers everything Decide needs. Every check runs on every tick, so a broken credential shows before a run
// needs it.
func (o *LiveObserver) Observe(ctx context.Context, now time.Time) Observations {
	var obs Observations
	obs.Pulsar = o.pulsar(ctx, now)
	obs.OtherLoad.Deployments, obs.OtherLoad.Err = o.cluster.OtherLoadTests(ctx)
	obs.Discard = o.discard(ctx, now)
	obs.Fresh = o.freshness(ctx, now)
	return obs
}

// Queries is every query the soak runs, for the README and the tests.
type Queries struct {
	BrokerVersions, RecentVersions, PodContainers, PodReady string
	NonDiscarding, AppFreshness, KubeFreshness, Logs        string
}

// QueriesFor returns the queries for cfg. Its label values are validated settings, so %q quotes them safely.
func QueriesFor(cfg Config) Queries {
	brokers := fmt.Sprintf(
		`pulsar_version_info{cloud_streamnative_io_cluster=%q, cloud_streamnative_io_component="broker"}`,
		cfg.PulsarCluster)
	return Queries{
		// One series per broker pod, with its version in the version label.
		BrokerVersions: "max by (pod, version) (" + brokers + ")",
		// Every version a broker reported in the last StableFor.
		RecentVersions: fmt.Sprintf("count by (version) (max_over_time(%s[%dm]))",
			brokers, int(StableFor.Minutes())),
		// The broker, bookie and ZooKeeper containers with their images. kube_pod_status_ready has no container label
		// of its own (its container label is kube-state-metrics'), so the two are joined on pod in Go.
		PodContainers: fmt.Sprintf(
			`max by (pod, container, image) (kube_pod_container_info{namespace=%q, container=~"%s|%s|%s"})`,
			cfg.PulsarKubeNamespace, containerBroker, containerBookie, containerZooKeeper),
		// 1 for a ready pod, 0 otherwise. Each pod also has condition="false" and "unknown" series; only "true" says
		// it's ready. min keeps a pod unready while any of its series, e.g. a restarted pod's old one, is.
		PodReady: fmt.Sprintf(`min by (pod) (kube_pod_status_ready{namespace=%q, condition="true"})`,
			cfg.PulsarKubeNamespace),
		// != also matches series without the label.
		NonDiscarding: fmt.Sprintf(
			`count(last_over_time(src_router_http_requests{namespace=%q, discard!="true"}[%s]))`,
			cfg.Namespace, discardWindow),
		// Gauges ingestion and src-router report all the time, with or without load.
		AppFreshness: fmt.Sprintf(
			`max(timestamp({__name__=~"ingestion_svc_config|src_router_config", namespace=%q}))`,
			cfg.Namespace),
		KubeFreshness: "max(timestamp(" + brokers + "))",
		// Any log line of the namespace: ingestion can go hours without logging when it has no traffic.
		Logs: fmt.Sprintf(`{namespace=%q}`, cfg.Namespace),
	}
}

func (o *LiveObserver) pulsar(ctx context.Context, now time.Time) Pulsar {
	q := QueriesFor(o.cfg)
	var errs []error
	query := func(what, query string) []grafana.Sample {
		samples, err := o.metrics.Query(ctx, o.cfg.KubeMetricsUID, query, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("querying %s: %w", what, err))
		}
		return samples
	}
	versions := query("the broker versions", q.BrokerVersions)
	recent := query("the recent broker versions", q.RecentVersions)
	containers := query("the Pulsar pods' containers", q.PodContainers)
	ready := query("the Pulsar pods' readiness", q.PodReady)
	if err := errors.Join(errs...); err != nil {
		return Pulsar{Err: err}
	}
	return DigestPulsar(versions, recent, containers, ready)
}

// DigestPulsar judges the Pulsar cluster from the results of the BrokerVersions, RecentVersions, PodContainers and
// PodReady queries.
func DigestPulsar(versions, recent, containers, ready []grafana.Sample) Pulsar {
	var p Pulsar
	problem := func(format string, args ...any) {
		if len(p.Problems) < maxProblems {
			p.Problems = append(p.Problems, fmt.Sprintf(format, args...))
		}
	}

	brokerVersions := map[string]string{} // broker pod -> its version
	all := map[string]bool{}
	invalid := false
	for _, s := range versions {
		pod, v := s.Labels["pod"], s.Labels["version"]
		if pod == "" || !versionRE.MatchString(v) {
			invalid = true
			problem("a pulsar_version_info series has no pod or an unexpected version %q", v)
			continue
		}
		brokerVersions[pod] = v
		all[v] = true
	}
	switch {
	case invalid: // a series that can't be read leaves the version unknown
	case len(all) == 0:
		problem("no broker reports pulsar_version_info")
	case len(all) > 1:
		problem("the brokers report different versions: %s", strings.Join(slices.Sorted(maps.Keys(all)), ", "))
	default:
		for v := range all {
			p.Version = v
		}
	}

	recentVersions := map[string]bool{}
	for _, s := range recent {
		recentVersions[s.Labels["version"]] = true
	}
	p.Stable = p.Version != "" && len(recentVersions) == 1 && recentVersions[p.Version]
	if p.Version != "" && !p.Stable {
		problem("in the last %s the brokers reported %s", StableFor,
			strings.Join(slices.Sorted(maps.Keys(recentVersions)), ", "))
	}

	pods := map[string]map[string]bool{containerBroker: {}, containerBookie: {}, containerZooKeeper: {}}
	images := map[string][]string{} // broker pod -> its images
	for _, s := range containers {
		pod, ctr := s.Labels["pod"], s.Labels["container"]
		if _, ok := pods[ctr]; !ok || pod == "" {
			continue
		}
		pods[ctr][pod] = true
		if ctr == containerBroker {
			images[pod] = append(images[pod], s.Labels["image"])
		}
	}
	readiness := map[string]float64{}
	for _, s := range ready {
		readiness[s.Labels["pod"]] = s.Value
	}

	p.Ready = p.Version != ""
	for _, ctr := range []string{containerBroker, containerBookie, containerZooKeeper} {
		if len(pods[ctr]) == 0 {
			p.Ready = false
			problem("kube_pod_container_info has no pod with a %s container", ctr)
		}
		for _, pod := range slices.Sorted(maps.Keys(pods[ctr])) {
			if v, ok := readiness[pod]; !ok || v != 1 {
				p.Ready = false
				problem("pod %s isn't ready", pod)
			}
		}
	}
	for _, pod := range slices.Sorted(maps.Keys(pods[containerBroker])) {
		if _, ok := brokerVersions[pod]; !ok {
			p.Ready = false
			problem("broker %s reports no version", pod)
		}
		for _, img := range images[pod] {
			if p.Version != "" && !TagCarries(img, p.Version) {
				p.Ready = false
				problem("broker %s runs %s, whose tag doesn't carry %s", pod, img, p.Version)
			}
		}
	}
	for _, pod := range slices.Sorted(maps.Keys(brokerVersions)) {
		if !pods[containerBroker][pod] {
			p.Ready = false
			problem("broker %s has no kube_pod_container_info", pod)
		}
	}
	return p
}

// TagCarries reports whether the image's tag contains version as a whole: 4.0.12.5 is in 4.0.12.5 and
// v4.0.12.5-amd64, not in 4.0.12.50 or 14.0.12.5. An image without a tag carries nothing.
func TagCarries(image, version string) bool {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}
	slash := strings.LastIndexByte(image, '/')
	colon := strings.LastIndexByte(image, ':')
	if colon <= slash {
		return false
	}
	tag := image[colon+1:]
	partOfVersion := func(c byte) bool { return c == '.' || c >= '0' && c <= '9' }
	for from := 0; version != "" && from < len(tag); {
		i := strings.Index(tag[from:], version)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(version)
		if (start == 0 || !partOfVersion(tag[start-1])) && (end == len(tag) || !partOfVersion(tag[end])) {
			return true
		}
		from = start + 1
	}
	return false
}

func (o *LiveObserver) discard(ctx context.Context, now time.Time) Discard {
	status, kerr := o.cluster.SrcRouterDiscard(ctx)
	if kerr != nil {
		kerr = fmt.Errorf("checking src-router in Kubernetes: %w", kerr)
	}
	samples, merr := o.metrics.Query(ctx, o.cfg.AppMetricsUID, QueriesFor(o.cfg).NonDiscarding, now)
	if merr != nil {
		merr = fmt.Errorf("checking src-router's metrics: %w", merr)
	}
	d := Discard{Err: errors.Join(kerr, merr)}
	var nonDiscarding float64
	for _, s := range samples {
		nonDiscarding += s.Value
	}
	switch {
	case kerr == nil && !status.OK:
		d.State, d.Reason = DiscardFailed, status.Reason
	case merr == nil && nonDiscarding > 0:
		d.State = DiscardFailed
		d.Reason = fmt.Sprintf("%.0f src_router_http_requests series in %s lack discard=\"true\"",
			nonDiscarding, o.cfg.Namespace)
	case d.Err == nil:
		d.State = DiscardOK
	}
	return d
}

func (o *LiveObserver) freshness(ctx context.Context, now time.Time) Freshness {
	q := QueriesFor(o.cfg)
	var (
		f    Freshness
		errs []error
	)
	latest := func(what, uid, query string) time.Time {
		samples, err := o.metrics.Query(ctx, uid, query, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("querying the newest %s: %w", what, err))
			return time.Time{}
		}
		var t time.Time
		for _, s := range samples {
			if st := grafana.SecondsToTime(s.Value); st.After(t) {
				t = st
			}
		}
		return t
	}
	f.AppMetrics = latest("application metrics", o.cfg.AppMetricsUID, q.AppFreshness)
	f.KubeMetrics = latest("Pulsar metrics", o.cfg.KubeMetricsUID, q.KubeFreshness)
	logs, ok, err := o.metrics.LatestLog(ctx, o.cfg.LogsUID, q.Logs, now.Add(-logsLookback), now)
	if err != nil {
		errs = append(errs, fmt.Errorf("querying the newest log line: %w", err))
	} else if ok {
		f.Logs = logs
	}
	f.Err = errors.Join(errs...)
	return f
}
