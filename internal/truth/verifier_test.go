package truth

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const appDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const proxyDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func deployment(containers, init []corev1.Container) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", Annotations: map[string]string{"deployment.kubernetes.io/revision": "2"}}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: containers, InitContainers: init}}}}
}
func readyPod(name, rs string, statuses, init []corev1.ContainerStatus) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs}}}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: statuses, InitContainerStatuses: init}}
}
func rs(name, revision string) appsv1.ReplicaSet {
	return appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Annotations: map[string]string{"deployment.kubernetes.io/revision": revision}}}
}
func status(name, digest string) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, ImageID: "docker-pullable://registry.example/team/" + name + "@" + digest, Ready: true}
}

func TestDigestPinnedContainersMatchByName(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "registry.example:5000/team/api@" + appDigest}, {Name: "sidecar", Image: "registry.example:5000/team/proxy@" + proxyDigest}}, nil)
	pod := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("api", appDigest), status("sidecar", proxyDigest)}, nil)
	result := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod})
	if result.Status != StatusMatch {
		t.Fatalf("want MATCH, got %s: %+v", result.Status, result.Findings)
	}
}
func TestSwappedContainerImagesDoNotMatch(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "api@" + appDigest}, {Name: "sidecar", Image: "proxy@" + proxyDigest}}, nil)
	pod := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("api", proxyDigest), status("sidecar", appDigest)}, nil)
	if got := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod}).Status; got != StatusPartial {
		t.Fatalf("want PARTIAL, got %s", got)
	}
}
func TestMissingAndUnexpectedContainerStatusAreEvidence(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "api@" + appDigest}}, nil)
	pod := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("extra", appDigest)}, nil)
	result := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod})
	if result.Status != StatusUnknown {
		t.Fatalf("want UNKNOWN, got %s", result.Status)
	}
	if len(result.Findings) != 2 {
		t.Fatalf("want missing and unexpected findings, got %+v", result.Findings)
	}
}
func TestInitContainersUseInitStatus(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "api@" + appDigest}}, []corev1.Container{{Name: "migrate", Image: "migrate@" + proxyDigest}})
	pod := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("api", appDigest)}, []corev1.ContainerStatus{status("migrate", proxyDigest)})
	if got := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod}).Status; got != StatusMatch {
		t.Fatalf("want MATCH, got %s", got)
	}
	pod.Status.InitContainerStatuses[0] = status("migrate", appDigest)
	if got := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod}).Status; got != StatusPartial {
		t.Fatalf("want PARTIAL, got %s", got)
	}
}
func TestTagsAreInsufficientImmutableEvidence(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "registry.example:5000/team/api:1.0"}}, nil)
	pod := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("api", appDigest)}, nil)
	if got := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2")}, []corev1.Pod{pod}).Status; got != StatusUnknown {
		t.Fatalf("want UNKNOWN for tag, got %s", got)
	}
}
func TestOldTerminatingPodDoesNotCountAgainstCurrentReady(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "api@" + appDigest}}, nil)
	current := readyPod("api-new", "api-new", []corev1.ContainerStatus{status("api", appDigest)}, nil)
	old := readyPod("api-old", "api-old", []corev1.ContainerStatus{status("api", appDigest)}, nil)
	now := metav1.Now()
	old.DeletionTimestamp = &now
	result := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs("api-new", "2"), rs("api-old", "1")}, []corev1.Pod{current, old})
	if result.Status != StatusMatch || result.Summary.Observed != 1 {
		t.Fatalf("old terminating pod must be excluded: %+v", result)
	}
}
func TestParseImageReference(t *testing.T) {
	for _, value := range []string{"nginx:1.25", "registry.example:5000/team/api:1.0", "registry.example/team/api@" + appDigest, "team/api@" + appDigest, "api"} {
		if parseImageReference(value).Repository == "" {
			t.Fatalf("failed to parse %q", value)
		}
	}
}
