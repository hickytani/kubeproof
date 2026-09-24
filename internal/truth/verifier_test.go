package truth

import (
	"encoding/json"
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

func TestEvidenceAndFindingsAreDeterministicAcrossAPIOrdering(t *testing.T) {
	dep := deployment([]corev1.Container{{Name: "api", Image: "api@" + appDigest}, {Name: "sidecar", Image: "sidecar@" + proxyDigest}}, nil)
	a := readyPod("a", "api-new", []corev1.ContainerStatus{status("sidecar", proxyDigest), status("api", appDigest)}, nil)
	b := readyPod("b", "api-old", []corev1.ContainerStatus{status("api", appDigest)}, nil)
	sets := []appsv1.ReplicaSet{rs("api-old", "1"), rs("api-new", "2")}
	first := BuildDeploymentResultWithReplicaSets(dep, sets, []corev1.Pod{b, a})
	second := BuildDeploymentResultWithReplicaSets(dep, []appsv1.ReplicaSet{sets[1], sets[0]}, []corev1.Pod{a, b})
	first.Timestamp, second.Timestamp = first.Timestamp.UTC(), first.Timestamp.UTC()
	for i := range first.Observations {
		first.Observations[i].Timestamp = first.Timestamp
	}
	for i := range second.Observations {
		second.Observations[i].Timestamp = second.Timestamp
	}
	for i := range first.EvidenceChain {
		for j := range first.EvidenceChain[i].Evidence {
			first.EvidenceChain[i].Evidence[j].Timestamp = first.Timestamp
		}
	}
	for i := range second.EvidenceChain {
		for j := range second.EvidenceChain[i].Evidence {
			second.EvidenceChain[i].Evidence[j].Timestamp = second.Timestamp
		}
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("ordering changed normalized result\n%s\n%s", firstJSON, secondJSON)
	}
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

func statefulSet(containers, init []corev1.Container) *appsv1.StatefulSet {
	replicas := int32(1)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: containers, InitContainers: init}}},
		Status:     appsv1.StatefulSetStatus{CurrentRevision: "db-rev-1", UpdateRevision: "db-rev-1", Replicas: 1, ReadyReplicas: 1},
	}
}

func readyStsPod(name, rev string, statuses, init []corev1.ContainerStatus) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}},
			Labels:          map[string]string{"controller-revision-hash": rev},
		},
		Status: corev1.PodStatus{
			Conditions:             []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses:     statuses,
			InitContainerStatuses: init,
		},
	}
}

func TestStatefulSetCurrentRevisionMatches(t *testing.T) {
	sts := statefulSet([]corev1.Container{{Name: "db", Image: "db@" + appDigest}}, nil)
	pod := readyStsPod("db-0", "db-rev-1", []corev1.ContainerStatus{status("db", appDigest)}, nil)
	crs := []appsv1.ControllerRevision{{ObjectMeta: metav1.ObjectMeta{Name: "db-rev-1", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}, Revision: 1}}

	res := BuildStatefulSetResultWithControllerRevisions(sts, crs, []corev1.Pod{pod})
	if res.Status != StatusMatch {
		t.Fatalf("want MATCH, got %s: %+v", res.Status, res.Findings)
	}
}

func TestStatefulSetOldRevisionExcluded(t *testing.T) {
	sts := statefulSet([]corev1.Container{{Name: "db", Image: "db@" + appDigest}}, nil)
	sts.Status.UpdateRevision = "db-rev-2"

	podNew := readyStsPod("db-1", "db-rev-2", []corev1.ContainerStatus{status("db", appDigest)}, nil)
	podOld := readyStsPod("db-0", "db-rev-1", []corev1.ContainerStatus{status("db", appDigest)}, nil)
	crs := []appsv1.ControllerRevision{
		{ObjectMeta: metav1.ObjectMeta{Name: "db-rev-1", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}, Revision: 1},
		{ObjectMeta: metav1.ObjectMeta{Name: "db-rev-2", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}, Revision: 2},
	}

	res := BuildStatefulSetResultWithControllerRevisions(sts, crs, []corev1.Pod{podNew, podOld})
	if res.Status != StatusMatch || res.Summary.Observed != 1 {
		t.Fatalf("want MATCH with 1 observed current pod, got %s (observed=%d): %+v", res.Status, res.Summary.Observed, res.Findings)
	}
	foundOldFinding := false
	for _, f := range res.Findings {
		if f.Code == "old-revision-pod" {
			foundOldFinding = true
		}
	}
	if !foundOldFinding {
		t.Fatalf("expected old-revision-pod finding")
	}
}

func TestStatefulSetDigestMismatch(t *testing.T) {
	sts := statefulSet([]corev1.Container{{Name: "db", Image: "db@" + appDigest}}, nil)
	pod := readyStsPod("db-0", "db-rev-1", []corev1.ContainerStatus{status("db", proxyDigest)}, nil)
	crs := []appsv1.ControllerRevision{{ObjectMeta: metav1.ObjectMeta{Name: "db-rev-1", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}, Revision: 1}}

	res := BuildStatefulSetResultWithControllerRevisions(sts, crs, []corev1.Pod{pod})
	if res.Status != StatusPartial {
		t.Fatalf("want PARTIAL for digest mismatch, got %s", res.Status)
	}
}

func TestStatefulSetUnknownRevision(t *testing.T) {
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: func() *int32 { i := int32(1); return &i }()},
	}
	pod := readyStsPod("db-0", "", []corev1.ContainerStatus{status("db", appDigest)}, nil)

	res := BuildStatefulSetResultWithControllerRevisions(sts, nil, []corev1.Pod{pod})
	if res.Status != StatusUnknown {
		t.Fatalf("want UNKNOWN for missing revision metadata, got %s", res.Status)
	}
}

func daemonSet(containers, init []corev1.Container) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "kube-system"},
		Spec:       appsv1.DaemonSetSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: containers, InitContainers: init}}},
		Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, CurrentNumberScheduled: 2, NumberReady: 2, UpdatedNumberScheduled: 2},
	}
}

func readyDsPod(name, rev string, statuses, init []corev1.ContainerStatus) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       "kube-system",
			OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}},
			Labels:          map[string]string{"controller-revision-hash": rev},
		},
		Status: corev1.PodStatus{
			Conditions:             []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses:     statuses,
			InitContainerStatuses: init,
		},
	}
}

func TestDaemonSetCurrentRevisionMatches(t *testing.T) {
	ds := daemonSet([]corev1.Container{{Name: "agent", Image: "agent@" + appDigest}}, nil)
	pod1 := readyDsPod("node-1", "ds-rev-1", []corev1.ContainerStatus{status("agent", appDigest)}, nil)
	pod2 := readyDsPod("node-2", "ds-rev-1", []corev1.ContainerStatus{status("agent", appDigest)}, nil)
	crs := []appsv1.ControllerRevision{{ObjectMeta: metav1.ObjectMeta{Name: "ds-rev-1", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}}}, Revision: 1}}

	res := BuildDaemonSetResultWithControllerRevisions(ds, crs, []corev1.Pod{pod1, pod2})
	if res.Status != StatusMatch {
		t.Fatalf("want MATCH, got %s: %+v", res.Status, res.Findings)
	}
}

func TestDaemonSetOldRevisionExcluded(t *testing.T) {
	ds := daemonSet([]corev1.Container{{Name: "agent", Image: "agent@" + appDigest}}, nil)
	podNew := readyDsPod("node-1", "ds-rev-2", []corev1.ContainerStatus{status("agent", appDigest)}, nil)
	podOld := readyDsPod("node-2", "ds-rev-1", []corev1.ContainerStatus{status("agent", appDigest)}, nil)
	crs := []appsv1.ControllerRevision{
		{ObjectMeta: metav1.ObjectMeta{Name: "ds-rev-1", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}}}, Revision: 1},
		{ObjectMeta: metav1.ObjectMeta{Name: "ds-rev-2", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}}}, Revision: 2},
	}

	res := BuildDaemonSetResultWithControllerRevisions(ds, crs, []corev1.Pod{podNew, podOld})
	if res.Status != StatusPartial {
		t.Fatalf("want PARTIAL due to 1/2 ready current pods, got %s", res.Status)
	}
}

func TestDaemonSetDoesNotImposeDeploymentReplicas(t *testing.T) {
	ds := daemonSet([]corev1.Container{{Name: "agent", Image: "agent@" + appDigest}}, nil)
	ds.Status.DesiredNumberScheduled = 5
	ds.Status.NumberReady = 5

	pods := make([]corev1.Pod, 5)
	for i := 0; i < 5; i++ {
		pods[i] = readyDsPod("node-"+string(rune('a'+i)), "ds-rev-1", []corev1.ContainerStatus{status("agent", appDigest)}, nil)
	}
	crs := []appsv1.ControllerRevision{{ObjectMeta: metav1.ObjectMeta{Name: "ds-rev-1", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}}}, Revision: 1}}

	res := BuildDaemonSetResultWithControllerRevisions(ds, crs, pods)
	if res.Status != StatusMatch || res.Summary.Observed != 5 {
		t.Fatalf("want MATCH with 5 ready pods, got %s (observed=%d)", res.Status, res.Summary.Observed)
	}
}
