package kube

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	ns         = "soak-ns"
	release    = "rudder-load-pulsar-soak-test"
	srcRouter  = "srcrouter"
	discardEnv = "SRC_ROUTER_GATEWAY_DISCARD_MESSAGES"
)

var secretsGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// withResourceVersions makes the fake clientset version Secrets the way the API server does: a create sets a
// resourceVersion, and an update must name the current one and gets a new one.
func withResourceVersions(cs *fake.Clientset) {
	cs.PrependReactor("create", "secrets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		s := a.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		s.ResourceVersion = "1"
		if err := cs.Tracker().Create(secretsGVR, s, s.Namespace); err != nil {
			return true, nil, err
		}
		return true, s, nil
	})
	cs.PrependReactor("update", "secrets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		s := a.(k8stesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
		cur, err := cs.Tracker().Get(secretsGVR, s.Namespace, s.Name)
		if err != nil {
			return true, nil, err
		}
		if cur.(*corev1.Secret).ResourceVersion != s.ResourceVersion {
			return true, nil, apierrors.NewConflict(secretsGVR.GroupResource(), s.Name, nil)
		}
		n, err := strconv.Atoi(s.ResourceVersion)
		if err != nil {
			return true, nil, err
		}
		s.ResourceVersion = strconv.Itoa(n + 1)
		if err := cs.Tracker().Update(secretsGVR, s, s.Namespace); err != nil {
			return true, nil, err
		}
		return true, s, nil
	})
}

func TestStateStore(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset()
	withResourceVersions(cs)
	store := NewStateStore(cs, ns, "rudder-load-pulsar-soak-test-state")

	data, version, err := store.Load(ctx)
	require.NoError(t, err, "loading before the first save")
	require.Nil(t, data, "no state yet")
	require.Empty(t, version, "no version yet")

	v1, err := store.Save(ctx, []byte(`{"a":1}`), "")
	require.NoError(t, err, "creating the Secret")

	data, version, err = store.Load(ctx)
	require.NoError(t, err, "loading")
	require.Equal(t, `{"a":1}`, string(data), "the saved state")
	require.Equal(t, v1, version, "the saved version")

	_, err = store.Save(ctx, []byte(`{"a":2}`), "")
	require.ErrorIs(t, err, ErrConflict, "creating it again conflicts")

	v2, err := store.Save(ctx, []byte(`{"a":3}`), v1)
	require.NoError(t, err, "updating at the current version")
	require.NotEqual(t, v1, v2, "an update changes the version")
	data, version, err = store.Load(ctx)
	require.NoError(t, err, "loading")
	require.Equal(t, `{"a":3}`, string(data), "the updated state")
	require.Equal(t, v2, version, "the updated version")

	_, err = store.Save(ctx, []byte(`{"a":4}`), v1)
	require.ErrorIs(t, err, ErrConflict, "a stale version conflicts")

	secret, err := cs.CoreV1().Secrets(ns).Get(ctx, "rudder-load-pulsar-soak-test-state", metav1.GetOptions{})
	require.NoError(t, err, "reading the Secret")
	require.Equal(t, corev1.SecretTypeOpaque, secret.Type, "an opaque Secret")
	require.Equal(t, `{"a":3}`, string(secret.Data[stateKey]), "the conflicting write changed nothing")
}

func TestStateStoreSecretWithoutState(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: ns}})
	_, _, err := NewStateStore(cs, ns, "s").Load(context.Background())
	require.Error(t, err, "a Secret without the state is an error, not an empty state")
}

func TestStateStoreRetriesReads(t *testing.T) {
	cs := fake.NewClientset()
	calls := 0
	cs.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewServiceUnavailable("try again")
		}
		return false, nil, nil
	})
	data, _, err := NewStateStore(cs, ns, "s").Load(context.Background())
	require.NoError(t, err, "the second read succeeds")
	require.Nil(t, data, "no state")
	require.Equal(t, 2, calls, "the read was retried once")
}

func deployment(name string, mutate ...func(*appsv1.Deployment)) *appsv1.Deployment {
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main", Image: "example.com/other:1.0"}},
		}}},
	}
	for _, m := range mutate {
		m(d)
	}
	return d
}

func helmRelease(name, namespace string) func(*appsv1.Deployment) {
	return func(d *appsv1.Deployment) {
		d.Annotations = map[string]string{helmReleaseNameAnnotation: name, helmReleaseNamespaceAnnotation: namespace}
	}
}

func image(img string, command ...string) func(*appsv1.Deployment) {
	return func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Image = img
		d.Spec.Template.Spec.Containers[0].Command = command
	}
}

func TestOtherLoadTests(t *testing.T) {
	cs := fake.NewClientset(
		deployment("rudder-load-pulsar-soak-test-0", helmRelease(release, ns)),
		deployment("rudder-load-other-0", helmRelease("rudder-load-other", ns)),
		deployment("rudder-load-kubectl-0"),
		deployment("custom-name", image("123.dkr.ecr.us-east-1.amazonaws.com/rudderstack/rudder-load:main")),
		deployment("by-digest", image("me/rudder-load@sha256:abc")),
		deployment("by-command", image("me/custom:1", "/rudder-load-producer")),
		deployment("ingestion", image("rudderstack/rudder-ingestion-svc:1.31.0")),
		deployment("same-release-other-namespace", image("rudder-load"), helmRelease(release, "elsewhere")),
	)
	names, err := NewCluster(cs, ns, release, srcRouter, discardEnv).OtherLoadTests(context.Background())
	require.NoError(t, err, "listing")
	require.Equal(t, []string{
		"by-command", "by-digest", "custom-name", "rudder-load-kubectl-0", "rudder-load-other-0",
		"same-release-other-namespace",
	}, names, "every rudder-load Deployment except the soak's own")
}

func srcRouterDeployment(mutate ...func(*appsv1.Deployment)) *appsv1.Deployment {
	replicas := int32(2)
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: srcRouter, Namespace: ns, Generation: 3},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "srcrouter"}},
			Template: corev1.PodTemplateSpec{Spec: discardingPodSpec("true")},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 3, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2,
		},
	}
	for _, m := range mutate {
		m(d)
	}
	return d
}

func discardingPodSpec(value string) corev1.PodSpec {
	return corev1.PodSpec{Containers: []corev1.Container{
		{Name: "sidecar"},
		{Name: "srcrouter", Env: []corev1.EnvVar{{Name: "OTHER", Value: "x"}, {Name: discardEnv, Value: value}}},
	}}
}

func pod(name, value string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: map[string]string{"app": "srcrouter"}},
		Spec:       discardingPodSpec(value),
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestSrcRouterDiscard(t *testing.T) {
	tests := []struct {
		name       string
		objects    []runtime.Object
		wantOK     bool
		wantReason string
	}{
		{
			name: "discarding",
			objects: []runtime.Object{
				srcRouterDeployment(),
				pod("a", "true", corev1.PodRunning), pod("b", "true", corev1.PodRunning),
				pod("old", "false", corev1.PodSucceeded),
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: ns}},
			},
			wantOK: true,
		},
		{
			name:       "no Deployment",
			wantReason: "doesn't exist",
		},
		{
			name: "template sets false",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Spec.Template.Spec = discardingPodSpec("false")
			})},
			wantReason: `sets SRC_ROUTER_GATEWAY_DISCARD_MESSAGES to "false"`,
		},
		{
			name: "template sets TRUE",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Spec.Template.Spec = discardingPodSpec("TRUE")
			})},
			wantReason: `to "TRUE"`,
		},
		{
			name: "template doesn't set it",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Spec.Template.Spec = corev1.PodSpec{Containers: []corev1.Container{{Name: "srcrouter"}}}
			})},
			wantReason: "no container sets",
		},
		{
			name: "template takes it from a ConfigMap",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Spec.Template.Spec.Containers[1].Env[1].ValueFrom = &corev1.EnvVarSource{}
			})},
			wantReason: "from a reference",
		},
		{
			name: "spec not observed yet",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Generation = 4
			})},
			wantReason: "hasn't been observed",
		},
		{
			name: "replicas not updated",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Status.UpdatedReplicas = 1
			})},
			wantReason: "not every replica has been updated",
		},
		{
			name: "old replicas terminating",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Status.Replicas = 3
			})},
			wantReason: "old replicas",
		},
		{
			name: "updated replicas not available",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Status.AvailableReplicas = 1
			})},
			wantReason: "not every updated replica is available",
		},
		{
			name: "progress deadline exceeded",
			objects: []runtime.Object{srcRouterDeployment(func(d *appsv1.Deployment) {
				d.Status.Conditions = []appsv1.DeploymentCondition{
					{Type: appsv1.DeploymentProgressing, Reason: "ProgressDeadlineExceeded"},
				}
			})},
			wantReason: "progress deadline",
		},
		{
			name: "a running pod without it",
			objects: []runtime.Object{
				srcRouterDeployment(), pod("a", "true", corev1.PodRunning), pod("b", "false", corev1.PodRunning),
			},
			wantReason: "pod b:",
		},
		{
			name: "a pending pod without it",
			objects: []runtime.Object{
				srcRouterDeployment(), pod("a", "true", corev1.PodRunning), pod("c", "", corev1.PodPending),
			},
			wantReason: "pod c:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := fake.NewClientset(tt.objects...)
			status, err := NewCluster(cs, ns, release, srcRouter, discardEnv).SrcRouterDiscard(context.Background())
			require.NoError(t, err, "checking")
			require.Equal(t, tt.wantOK, status.OK, "discarding: %s", status.Reason)
			if !tt.wantOK {
				require.Contains(t, status.Reason, tt.wantReason, "reason")
			}
		})
	}
}

func TestSrcRouterDiscardAPIError(t *testing.T) {
	cs := fake.NewClientset(srcRouterDeployment())
	cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
	})
	_, err := NewCluster(cs, ns, release, srcRouter, discardEnv).SrcRouterDiscard(context.Background())
	require.Error(t, err, "an API error is an error, not a verdict")
}
