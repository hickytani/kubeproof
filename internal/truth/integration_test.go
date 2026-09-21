package truth

import (
	"encoding/json"
	"strings"
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

func podWithContainers(name string, containers ...corev1.ContainerStatus) corev1.Pod {
	pod := podWithImageID(name, "")
	pod.Status.ContainerStatuses = containers
	return pod
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

func TestVerifyDeploymentReplicaSummaryAndFindings(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	replicas := int32(3)
	dep.Spec.Replicas = &replicas
	pods := []corev1.Pod{
		podWithImageID("demo-a", "docker-pullable://nginx@sha256:aaa"),
		podWithImageID("demo-b", "docker-pullable://nginx@sha256:bbb"),
		podWithImageID("demo-c", "docker-pullable://busybox@sha256:ccc"),
	}

	res := BuildDeploymentResultWithReplicaSets(dep, nil, pods)
	if res.Status != StatusPartial {
		t.Fatalf("expected partial result, got %s", res.Status)
	}
	if res.Summary.Desired != 3 || res.Summary.Observed != 3 || res.Summary.Matching != 2 || res.Summary.Divergent != 1 {
		t.Fatalf("unexpected replica summary: %+v", res.Summary)
	}
	if len(res.Findings) != 1 || res.Findings[0].Code != "runtime-image-divergence" {
		t.Fatalf("expected one runtime divergence finding, got %+v", res.Findings)
	}
}

func TestVerifyDeploymentMissingReplicaIsPartial(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	replicas := int32(3)
	dep.Spec.Replicas = &replicas
	res := BuildDeploymentResultWithReplicaSets(dep, nil, []corev1.Pod{podWithImageID("demo-a", "docker-pullable://nginx@sha256:aaa"), podWithImageID("demo-b", "docker-pullable://nginx@sha256:bbb")})
	if res.Status != StatusPartial {
		t.Fatalf("expected missing replica to be partial, got %s", res.Status)
	}
	if res.Summary.Observed != 2 || res.Summary.Desired != 3 {
		t.Fatalf("unexpected replica summary: %+v", res.Summary)
	}
	if res.Findings[len(res.Findings)-1].Code != "replica-count-drift" {
		t.Fatalf("expected replica-count-drift finding, got %+v", res.Findings)
	}
}

func TestVerifyDeploymentStaleReplicaSetFinding(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	dep.Annotations = map[string]string{"deployment.kubernetes.io/revision": "2"}
	rs := appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "demo-old", Annotations: map[string]string{"deployment.kubernetes.io/revision": "1"}}}
	res := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{rs}, []corev1.Pod{podWithImageID("demo-a", "docker-pullable://nginx@sha256:aaa")})
	if res.Status != StatusPartial {
		t.Fatalf("expected stale ReplicaSet to produce partial, got %s", res.Status)
	}
	found := false
	for _, finding := range res.Findings {
		if finding.Code == "stale-replicaset" && finding.Status == StatusStale {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stale ReplicaSet finding, got %+v", res.Findings)
	}
}

func TestVerifyDeploymentMultipleContainersAndJSONEvidence(t *testing.T) {
	dep := deploymentForImage("nginx:1.25")
	dep.Spec.Template.Spec.Containers = []corev1.Container{{Name: "nginx", Image: "nginx:1.25"}, {Name: "sidecar", Image: "busybox:1.36"}}
	pod := podWithContainers("demo-a",
		corev1.ContainerStatus{Name: "nginx", Image: "nginx:1.25", ImageID: "docker-pullable://nginx@sha256:aaa", Ready: true},
		corev1.ContainerStatus{Name: "sidecar", Image: "busybox:1.36", ImageID: "docker-pullable://busybox@sha256:bbb", Ready: true},
	)
	res := BuildDeploymentResult(dep, []corev1.Pod{pod})
	if len(res.Observations) != 2 || res.Summary.Matching != 2 {
		t.Fatalf("expected two matching container observations, got %+v", res)
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 {
		t.Fatal("expected JSON evidence")
	}
}

func TestBuildScanSummaryAggregatesStatuses(t *testing.T) {
	deployments := []appsv1.Deployment{
		{ObjectMeta: metav1.ObjectMeta{Name: "api"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "worker"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "unknown"}},
	}
	results := map[string]VerificationResult{
		"api":    {Status: StatusMatch},
		"worker": {Status: StatusPartial},
	}

	summary := BuildScanSummary(deployments, results)
	for _, expected := range []string{"3 workloads inspected", "MATCH\t1", "PARTIAL\t1", "UNKNOWN\t1", "api\tMATCH", "worker\tPARTIAL", "unknown\tUNKNOWN"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("expected %q in summary %q", expected, summary)
		}
	}
}
