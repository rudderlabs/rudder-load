package pulsarsoak

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
	obskit "github.com/rudderlabs/rudder-observability-kit/go/labels"

	"rudder-load/internal/pulsarsoak/helm"
	"rudder-load/internal/pulsarsoak/slack"
)

const (
	// maxSteps bounds the actions of one tick. A run needs about ten from detection to hand-off, over many ticks.
	maxSteps = 20
	// saveAfterTimeout bounds the save that records a side effect.
	saveAfterTimeout = 30 * time.Second
)

// Store keeps the state between ticks with optimistic updates.
type Store interface {
	// Load returns the state and its version, or no data and an empty version if there's no state yet.
	Load(ctx context.Context) ([]byte, string, error)
	// Save writes the state if it is still at version, creating it when version is empty, and returns the new
	// version.
	Save(ctx context.Context, data []byte, version string) (string, error)
}

// Helm installs and uninstalls rudder-load.
type Helm interface {
	UpgradeInstall(ctx context.Context, in helm.Install) error
	Uninstall(ctx context.Context, release, namespace string) error
}

// Poster posts Slack messages. An error wrapping slack.ErrNotPosted means the message certainly wasn't posted.
type Poster interface {
	PostMessage(ctx context.Context, text, threadTS string) (string, error)
}

// Observer gathers the observations of a tick.
type Observer interface {
	Observe(ctx context.Context, now time.Time) Observations
}

// Deps are the tick's adapters.
type Deps struct {
	Store    Store
	Helm     Helm
	Slack    Poster
	Observer Observer
	// ReadBrief returns the brief template.
	ReadBrief func() (string, error)
	// OnAnnounce is called with the ts of a run's announcement once it's posted, if it isn't nil.
	OnAnnounce func(ts string) error
	Now        func() time.Time
	Log        logger.Logger
}

// Ticker runs ticks.
type Ticker struct {
	cfg  Config
	deps Deps
	log  logger.Logger
}

// NewTicker returns a ticker for cfg.
func NewTicker(cfg Config, deps Deps) *Ticker {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Log == nil {
		deps.Log = logger.NOP
	}
	return &Ticker{cfg: cfg, deps: deps, log: deps.Log.Withn(obskit.Namespace(cfg.Namespace))}
}

// Tick reads the state, observes, and acts on the decisions until there is nothing left to do now. It returns the
// errors it met, including those of observations it could do without, so a broken credential fails the tick long
// before a run needs it.
func (t *Ticker) Tick(ctx context.Context) error {
	data, version, err := t.deps.Store.Load(ctx)
	if err != nil {
		return fmt.Errorf("loading the Pulsar soak test state: %w", err)
	}
	st, err := DecodeState(data)
	if err != nil {
		return err
	}

	template, briefErr := t.deps.ReadBrief()
	if briefErr == nil {
		briefErr = CheckBrief(template, t.cfg.RudderAssistUserID)
	}
	if briefErr != nil {
		briefErr = fmt.Errorf("checking the brief template: %w", briefErr)
		t.log.Errorn("The brief template can't be used", obskit.Error(briefErr))
	}

	obs := t.deps.Observer.Observe(ctx, t.deps.Now())
	t.logObservations(obs)
	errs := []error{briefErr, obs.Err()}

	for i := 0; ; i++ {
		if i == maxSteps {
			errs = append(errs, fmt.Errorf("the tick took more than %d steps", maxSteps))
			break
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("the tick was stopped: %w", err))
			break
		}
		in := Inputs{Now: t.deps.Now(), Obs: obs, Brief: template, BriefErr: briefErr}
		d, err := Decide(t.cfg, st, in)
		if err != nil {
			errs = append(errs, fmt.Errorf("deciding: %w", err))
			break
		}
		if d.Action.Kind == ActNone {
			if changed, err := differ(st, d.State); err != nil {
				errs = append(errs, err)
			} else if changed {
				if _, err := t.save(ctx, d.State, version); err != nil {
					errs = append(errs, err)
				}
			}
			t.log.Infon("Nothing more to do", logger.NewStringField("reason", d.Wait))
			break
		}
		var stop bool
		st, version, stop, err = t.execute(ctx, st, d, version)
		errs = append(errs, err)
		if stop {
			break
		}
	}
	return errors.Join(errs...)
}

// execute does the decision's action and saves the state. It returns the state to go on from, and stop when the
// tick must not take another step.
func (t *Ticker) execute(ctx context.Context, cur State, d Decision, version string) (
	State, string, bool, error,
) {
	log := t.log
	if r := d.State.Run; r != nil {
		log = log.Withn(logger.NewStringField("runId", r.ID), logger.NewStringField("version", r.Version))
	}
	switch d.Action.Kind {
	case ActPost:
		return t.post(ctx, d, version)
	case ActInstall:
		log.Infon("Installing rudder-load",
			logger.NewStringField("release", t.cfg.Release),
			logger.NewTimeField("deadline", d.State.Run.Deadline))
		if err := t.deps.Helm.UpgradeInstall(ctx, InstallRequest(t.cfg, d.State.Run)); err != nil {
			// Whatever helm did, nothing must send load that the state doesn't know about.
			uerr := t.deps.Helm.Uninstall(ctx, t.cfg.Release, t.cfg.Namespace)
			if uerr != nil {
				uerr = fmt.Errorf("uninstalling after the failed install: %w", uerr)
			}
			return cur, version, true, errors.Join(fmt.Errorf("installing rudder-load: %w", err), uerr)
		}
	case ActUninstall:
		log.Infon("Uninstalling rudder-load", logger.NewStringField("release", t.cfg.Release))
		if err := t.deps.Helm.Uninstall(ctx, t.cfg.Release, t.cfg.Namespace); err != nil {
			return cur, version, true, fmt.Errorf("uninstalling rudder-load: %w", err)
		}
	default:
		return cur, version, true, fmt.Errorf("unknown action %q", d.Action.Kind)
	}
	newVersion, err := t.saveAfter(ctx, d.State, version)
	if err != nil {
		return cur, version, true, err
	}
	return d.State, newVersion, false, nil
}

// saveAfter saves the state after a side effect. It goes ahead even if the tick has just been cancelled, e.g. by the
// workflow, so what was done is recorded.
func (t *Ticker) saveAfter(ctx context.Context, st State, version string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveAfterTimeout)
	defer cancel()
	return t.save(ctx, st, version)
}

// post posts a message at most once: it records the message as pending before posting it, and as posted, or
// unknown if Slack's answer was lost, after. A message that Slack certainly didn't post is forgotten, so a later
// tick posts it, and this tick stops so it doesn't try again right away.
func (t *Ticker) post(ctx context.Context, d Decision, version string) (State, string, bool, error) {
	st := d.State.Clone()
	run := st.Run
	if d.Action.Target == TargetClosing {
		run = st.Closing
	}
	if run == nil {
		return d.State, version, true, fmt.Errorf("no %s run to post %s for", d.Action.Target, d.Action.Key)
	}
	log := t.log.Withn(
		logger.NewStringField("runId", run.ID),
		logger.NewStringField("message", d.Action.Key),
		logger.NewBoolField("inThread", d.Action.ThreadTS != ""))

	if run.Posts == nil { // a run whose only record was dropped reads back without the map
		run.Posts = map[string]Post{}
	}
	run.Posts[d.Action.Key] = Post{Status: PostPending, At: t.deps.Now().UTC()}
	version, err := t.save(ctx, st, version)
	if err != nil {
		return d.State, version, true, fmt.Errorf("recording that %s is about to be posted: %w", d.Action.Key, err)
	}

	log.Infon("Posting to Slack")
	ts, postErr := t.deps.Slack.PostMessage(ctx, d.Action.Text, d.Action.ThreadTS)
	stop := false
	switch {
	case postErr == nil:
		run.Posts[d.Action.Key] = Post{Status: PostPosted, TS: ts, At: t.deps.Now().UTC()}
	case errors.Is(postErr, slack.ErrNotPosted):
		delete(run.Posts, d.Action.Key)
		stop = true
		postErr = fmt.Errorf("posting %s: %w", d.Action.Key, postErr)
	default:
		run.Posts[d.Action.Key] = Post{Status: PostUnknown, At: t.deps.Now().UTC()}
		postErr = fmt.Errorf("posting %s, which won't be tried again: %w", d.Action.Key, postErr)
	}
	version, err = t.saveAfter(ctx, st, version)
	if err != nil {
		return st, version, true, errors.Join(postErr, err)
	}
	if postErr == nil && d.Action.Key == MsgAnnounce && d.Action.Target == TargetRun && ts != "" &&
		t.deps.OnAnnounce != nil {
		if err := t.deps.OnAnnounce(ts); err != nil {
			postErr = fmt.Errorf("reporting the thread: %w", err)
		}
	}
	return st, version, stop, postErr
}

func (t *Ticker) save(ctx context.Context, st State, version string) (string, error) {
	data, err := st.Encode()
	if err != nil {
		return version, err
	}
	newVersion, err := t.deps.Store.Save(ctx, data, version)
	if err != nil {
		return version, fmt.Errorf("saving the Pulsar soak test state: %w", err)
	}
	return newVersion, nil
}

func differ(a, b State) (bool, error) {
	ea, err := a.Encode()
	if err != nil {
		return false, err
	}
	eb, err := b.Encode()
	if err != nil {
		return false, err
	}
	return !bytes.Equal(ea, eb), nil
}

// InstallRequest is the helm upgrade --install for run. The write keys only go in the secret values. Without a
// deployment name, the profile's stands.
func InstallRequest(cfg Config, run *Run) helm.Install {
	set := []helm.Value{{Key: "namespace", Value: cfg.Namespace}}
	if cfg.DeploymentName != "" {
		set = append(set, helm.Value{Key: "deployment.name", Value: cfg.DeploymentName})
	}
	return helm.Install{
		Release:     cfg.Release,
		Chart:       cfg.ChartPath,
		Namespace:   cfg.Namespace,
		ValuesFiles: []string{cfg.ProfilePath},
		Set:         set,
		SetString: []helm.Value{
			{Key: "deployment.env.HTTP_ENDPOINT", Value: cfg.HTTPEndpoint},
			{Key: "deployment.env.LOAD_RUN_ID", Value: run.ID},
			{Key: "deployment.env.RUN_DEADLINE", Value: run.Deadline.UTC().Format(time.RFC3339)},
		},
		SecretValues: map[string]any{
			"deployment": map[string]any{"env": map[string]any{"SOURCES": cfg.WriteKeys.Joined()}},
		},
	}
}

func (t *Ticker) logObservations(obs Observations) {
	p := obs.Pulsar
	t.log.Infon("Observed",
		logger.NewStringField("brokerVersion", p.Version),
		logger.NewBoolField("versionStable", p.Stable),
		logger.NewBoolField("pulsarReady", p.Ready),
		logger.NewStringField("pulsarProblems", strings.Join(p.Problems, "; ")),
		logger.NewStringField("otherLoadTests", strings.Join(obs.OtherLoad.Deployments, ", ")),
		logger.NewStringField("srcRouterDiscard", obs.Discard.State.String()),
		logger.NewStringField("srcRouterDiscardReason", obs.Discard.Reason),
		logger.NewTimeField("latestAppMetric", obs.Fresh.AppMetrics),
		logger.NewTimeField("latestKubeMetric", obs.Fresh.KubeMetrics),
		logger.NewTimeField("latestLog", obs.Fresh.Logs),
	)
	if err := obs.Err(); err != nil {
		t.log.Errorn("Some observations failed", obskit.Error(err))
	}
}
