// Package kube reads and writes what the Pulsar soak test needs in Kubernetes: its state Secret, rudder-load
// Deployments and the src-router Deployment.
package kube

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const (
	// DefaultTimeout bounds every API call.
	DefaultTimeout = 30 * time.Second

	stateKey = "state.json"

	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"

	// rudder-load's image is published as .../rudder-load and runs /rudder-load-producer, and the producer only
	// starts in pods whose hostname starts with rudder-load-.
	rudderLoadImage   = "rudder-load"
	rudderLoadCommand = "rudder-load-producer"
	rudderLoadPrefix  = "rudder-load-"
)

// ErrConflict means the state changed since it was read, e.g. because another tick ran at the same time.
var ErrConflict = errors.New("the Pulsar soak test state changed since it was read")

// readBackoff retries reads that fail for a reason that may go away, for about two seconds.
var readBackoff = wait.Backoff{Steps: 4, Duration: 200 * time.Millisecond, Factor: 2, Jitter: 0.1}

// NewClientset returns a clientset for the cluster the default kubeconfig loading rules point at: $KUBECONFIG,
// ~/.kube/config, or the in-cluster config.
func NewClientset(timeout time.Duration) (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).
		ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading the kubeconfig: %w", err)
	}
	cfg.Timeout = timeout
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating the Kubernetes client: %w", err)
	}
	return cs, nil
}

// StateStore keeps the Pulsar soak test state in a Secret, updated optimistically with its resourceVersion.
type StateStore struct {
	cs        kubernetes.Interface
	namespace string
	name      string
}

// NewStateStore returns a store for the Secret name in namespace.
func NewStateStore(cs kubernetes.Interface, namespace, name string) *StateStore {
	return &StateStore{cs: cs, namespace: namespace, name: name}
}

// Load returns the state and the Secret's resourceVersion, or no data and an empty version if the Secret doesn't
// exist yet.
func (s *StateStore) Load(ctx context.Context) ([]byte, string, error) {
	var secret *corev1.Secret
	err := withRetry(func() (err error) {
		secret, err = s.cs.CoreV1().Secrets(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
		return err
	})
	if apierrors.IsNotFound(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("reading the state Secret %s/%s: %w", s.namespace, s.name, err)
	}
	data, ok := secret.Data[stateKey]
	if !ok {
		return nil, "", fmt.Errorf("the state Secret %s/%s has no %s", s.namespace, s.name, stateKey)
	}
	return data, secret.ResourceVersion, nil
}

// Save writes the state if the Secret is still at version, creating it when version is empty, and returns the new
// version. It fails with ErrConflict if the Secret changed in between. Writes are not retried.
func (s *StateStore) Save(ctx context.Context, data []byte, version string) (string, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            s.name,
			Namespace:       s.namespace,
			ResourceVersion: version,
			Labels:          map[string]string{"app.kubernetes.io/managed-by": "rudder-load-pulsar-soak-test"},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{stateKey: data},
	}
	var (
		saved *corev1.Secret
		err   error
	)
	if version == "" {
		saved, err = s.cs.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
	} else {
		saved, err = s.cs.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	}
	if apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) {
		return "", fmt.Errorf("writing the state Secret %s/%s: %w", s.namespace, s.name, ErrConflict)
	}
	if err != nil {
		return "", fmt.Errorf("writing the state Secret %s/%s: %w", s.namespace, s.name, err)
	}
	return saved.ResourceVersion, nil
}

// DiscardStatus says whether src-router discards what it consumes.
type DiscardStatus struct {
	OK     bool
	Reason string // why not, when OK is false
}

// Cluster checks the namespace the Pulsar soak test runs in.
type Cluster struct {
	cs         kubernetes.Interface
	namespace  string
	release    string
	srcRouter  string
	discardEnv string
}

// NewCluster returns checks for namespace, where the Pulsar soak test's own Helm release is release and src-router's
// Deployment is srcRouter, which discards when its env var discardEnv is "true".
func NewCluster(cs kubernetes.Interface, namespace, release, srcRouter, discardEnv string) *Cluster {
	return &Cluster{cs: cs, namespace: namespace, release: release, srcRouter: srcRouter, discardEnv: discardEnv}
}

// OtherLoadTests returns the names of the rudder-load Deployments in the namespace that don't belong to the soak's
// own Helm release, sorted.
func (c *Cluster) OtherLoadTests(ctx context.Context) ([]string, error) {
	var list *appsv1.DeploymentList
	err := withRetry(func() (err error) {
		list, err = c.cs.AppsV1().Deployments(c.namespace).List(ctx, metav1.ListOptions{})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("listing the Deployments in %s: %w", c.namespace, err)
	}
	var names []string
	for i := range list.Items {
		d := &list.Items[i]
		if !IsRudderLoad(d) || c.ownedBySoak(d) {
			continue
		}
		names = append(names, d.Name)
	}
	slices.Sort(names)
	return names, nil
}

func (c *Cluster) ownedBySoak(d *appsv1.Deployment) bool {
	return d.Annotations[helmReleaseNameAnnotation] == c.release &&
		d.Annotations[helmReleaseNamespaceAnnotation] == c.namespace
}

// IsRudderLoad reports whether the Deployment runs rudder-load: its name has rudder-load's prefix, or a container
// runs rudder-load's image or its producer.
func IsRudderLoad(d *appsv1.Deployment) bool {
	if strings.HasPrefix(d.Name, rudderLoadPrefix) {
		return true
	}
	for _, ctr := range d.Spec.Template.Spec.Containers {
		if imageName(ctr.Image) == rudderLoadImage {
			return true
		}
		if len(ctr.Command) > 0 && path.Base(ctr.Command[0]) == rudderLoadCommand {
			return true
		}
	}
	return false
}

// imageName returns the last path element of an image reference, without its tag or digest.
func imageName(image string) string {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}
	name := path.Base(image)
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name
}

// SrcRouterDiscard checks that the live src-router Deployment sets the discard env var to "true", that its rollout
// is complete, and that every pod that hasn't terminated has the env var set to "true" too.
func (c *Cluster) SrcRouterDiscard(ctx context.Context) (DiscardStatus, error) {
	var d *appsv1.Deployment
	err := withRetry(func() (err error) {
		d, err = c.cs.AppsV1().Deployments(c.namespace).Get(ctx, c.srcRouter, metav1.GetOptions{})
		return err
	})
	if apierrors.IsNotFound(err) {
		return DiscardStatus{Reason: "the src-router Deployment " + c.srcRouter + " doesn't exist"}, nil
	}
	if err != nil {
		return DiscardStatus{}, fmt.Errorf("reading the src-router Deployment: %w", err)
	}
	if ok, why := envIsTrue(d.Spec.Template.Spec, c.discardEnv); !ok {
		return DiscardStatus{Reason: "the Deployment's pod template: " + why}, nil
	}
	if done, why := rolledOut(d); !done {
		return DiscardStatus{Reason: "the rollout isn't complete: " + why}, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return DiscardStatus{}, fmt.Errorf("reading the src-router Deployment's selector: %w", err)
	}
	var pods *corev1.PodList
	err = withRetry(func() (err error) {
		pods, err = c.cs.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		return err
	})
	if err != nil {
		return DiscardStatus{}, fmt.Errorf("listing the src-router pods: %w", err)
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		if ok, why := envIsTrue(p.Spec, c.discardEnv); !ok {
			return DiscardStatus{Reason: "pod " + p.Name + ": " + why}, nil
		}
	}
	return DiscardStatus{OK: true}, nil
}

// envIsTrue reports whether at least one container sets the env var name to the literal "true" and none sets it to
// anything else.
func envIsTrue(spec corev1.PodSpec, name string) (bool, string) {
	found := false
	for _, ctr := range spec.Containers {
		for _, e := range ctr.Env {
			if e.Name != name {
				continue
			}
			if e.ValueFrom != nil {
				return false, "container " + ctr.Name + " takes " + name + " from a reference, which can't be checked"
			}
			if e.Value != "true" {
				return false, "container " + ctr.Name + " sets " + name + " to " + quote(e.Value) + ", not \"true\""
			}
			found = true
		}
	}
	if !found {
		return false, "no container sets " + name
	}
	return true, ""
}

// quote quotes an env var's value for a message, shortened to 40 characters.
func quote(s string) string {
	const maxRunes = 40
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes]) + "..."
	}
	return strconv.Quote(s)
}

// rolledOut reports whether the Deployment's rollout is complete, as kubectl rollout status judges it.
func rolledOut(d *appsv1.Deployment) (bool, string) {
	if d.Generation > d.Status.ObservedGeneration {
		return false, "the latest spec hasn't been observed yet"
	}
	for _, cond := range d.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return false, "it exceeded its progress deadline"
		}
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	switch {
	case d.Status.UpdatedReplicas < desired:
		return false, "not every replica has been updated"
	case d.Status.Replicas > d.Status.UpdatedReplicas:
		return false, "old replicas are still terminating"
	case d.Status.AvailableReplicas < d.Status.UpdatedReplicas:
		return false, "not every updated replica is available"
	}
	return true, ""
}

func withRetry(fn func() error) error {
	return retry.OnError(readBackoff, transient, fn)
}

// transient reports whether a read may succeed if it's tried again.
func transient(err error) bool {
	switch {
	case apierrors.IsServerTimeout(err), apierrors.IsTimeout(err), apierrors.IsTooManyRequests(err),
		apierrors.IsInternalError(err), apierrors.IsServiceUnavailable(err), apierrors.IsUnexpectedServerError(err):
		return true
	}
	if utilnet.IsConnectionReset(err) || utilnet.IsConnectionRefused(err) || utilnet.IsProbableEOF(err) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
