package truth

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildDeploymentResultMatchesDeclaredImage(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}},
				},
			},
		},
	}
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-abc"},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:    "app",
			ImageID: "docker-pullable://nginx@sha256:abc123",
		}}},
	}}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusMatch {
		t.Fatalf("expected match status, got %s", res.Status)
	}
	if res.Desired == "" {
		t.Fatal("expected desired image to be populated")
	}
}

func TestBuildDeploymentResultFlagsMismatch(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "nginx:1.25"}},
				},
			},
		},
	}
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "demo-abc"},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:    "app",
			ImageID: "docker-pullable://busybox@sha256:def456",
		}}},
	}}

	res := BuildDeploymentResult(dep, pods)
	if res.Status != StatusPartial {
		t.Fatalf("expected mismatch status, got %s", res.Status)
	}
	if len(res.Observations) == 0 {
		t.Fatal("expected mismatch observation")
	}
}
