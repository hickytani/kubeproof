package truth

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deploymentForImage(image string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "nginx", Image: image}},
				},
			},
		},
	}
}

func podWithImageID(name string, imageID string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "ReplicaSet",
				Name:       "demo-123",
			}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "nginx",
				Image:        "nginx:1.25",
				ImageID:      imageID,
				RestartCount: 0,
			}},
		},
	}
}

func TestVerifyDeploymentMatch(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	pods := []corev1.Pod{podWithImageID("demo-abc", "docker-pullable://nginx@sha256:deadbeef")}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusMatch {
		t.Fatalf("expected match, got %s", res.Status)
	}
	if res.Desired != "nginx:1.25" {
		t.Fatalf("expected desired image nginx:1.25, got %s", res.Desired)
	}
}

func TestVerifyDeploymentMismatch(t *testing.T) {
	dep := deploymentForImage("busybox:1.36")
	pods := []corev1.Pod{podWithImageID("demo-abc", "docker-pullable://nginx@sha256:deadbeef")}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusPartial {
		t.Fatalf("expected mismatch/partial state, got %s", res.Status)
	}
	if len(res.Observations) == 0 {
		t.Fatal("expected observations for mismatch")
	}
}

func TestVerifyDeploymentMixedRuntimeIdentity(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	pods := []corev1.Pod{
		podWithImageID("demo-a", "docker-pullable://nginx@sha256:aaa"),
		podWithImageID("demo-b", "docker-pullable://busybox@sha256:bbb"),
	}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusPartial {
		t.Fatalf("expected mixed runtime identity to be partial, got %s", res.Status)
	}
}

func TestVerifyDeploymentUnknownWhenRuntimeIdentityMissing(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-abc", Namespace: "default"},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:         "nginx",
			Image:        "nginx:1.25",
			ImageID:      "",
			RestartCount: 3,
		}}},
	}}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusUnknown && res.Status != StatusPartial {
		t.Fatalf("expected unknown-ish status, got %s", res.Status)
	}
}

func TestVerifyDeploymentZeroPodsIsUnknown(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	res := BuildDeploymentResult(dep, nil)
	if res.Status != StatusUnknown {
		t.Fatalf("expected unknown when there are no pods, got %s", res.Status)
	}
}

func TestVerifyDeploymentMissingContainerStatusIsUnknown(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-abc", Namespace: "default"},
		Status:     corev1.PodStatus{},
	}}
	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusUnknown && res.Status != StatusPartial {
		t.Fatalf("expected unknown/partial status, got %s", res.Status)
	}
}

func TestVerifyDeploymentWithStaleReplicaSetState(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	pods := []corev1.Pod{
		podWithImageID("demo-old-1", "docker-pullable://nginx@sha256:old"),
		podWithImageID("demo-new-1", "docker-pullable://busybox@sha256:deadbeef"),
	}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusPartial {
		t.Fatalf("expected stale mixed state to be partial, got %s", res.Status)
	}
}
