package k8s

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetDeploymentPodsFollowsReplicaSetOwnership(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-123", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api"}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-123-pod", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-123"}}}}
	unrelated := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "other"}}}}
	client := fake.NewSimpleClientset(dep, rs, pod, unrelated)

	pods, err := GetDeploymentPods(client, "default", dep)
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != pod.Name {
		t.Fatalf("expected only owned Pod, got %+v", pods)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "get" && action.GetResource().Resource == "replicasets" {
			t.Fatalf("Pod discovery must use the ReplicaSet list, not per-Pod ReplicaSet GETs: %+v", action)
		}
	}
}

func TestGetReplicaSetsForDeploymentFiltersOwners(t *testing.T) {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}}
	owned := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "api-123", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "api"}}}}
	other := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "other-123", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "other"}}}}
	client := fake.NewSimpleClientset(owned, other)

	sets, err := GetReplicaSetsForDeployment(client, "default", dep)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 || sets[0].Name != owned.Name {
		t.Fatalf("expected only owned ReplicaSet, got %+v", sets)
	}
}

func TestGetStatefulSetPodsFollowsDirectOwnership(t *testing.T) {
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "db-0", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}}
	unrelated := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-0", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "other"}}}}
	client := fake.NewSimpleClientset(sts, pod, unrelated)

	pods, err := GetStatefulSetPods(client, "default", sts)
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != pod.Name {
		t.Fatalf("expected only owned StatefulSet pod, got %+v", pods)
	}
}

func TestGetControllerRevisionsForStatefulSet(t *testing.T) {
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"}}
	cr := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "db-rev-1", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db"}}}, Revision: 1}
	otherCr := &appsv1.ControllerRevision{ObjectMeta: metav1.ObjectMeta{Name: "other-rev-1", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "other"}}}, Revision: 1}
	client := fake.NewSimpleClientset(sts, cr, otherCr)

	crs, err := GetControllerRevisionsForStatefulSet(client, "default", sts)
	if err != nil {
		t.Fatal(err)
	}
	if len(crs) != 1 || crs[0].Name != cr.Name {
		t.Fatalf("expected only owned ControllerRevision, got %+v", crs)
	}
}

func TestGetDaemonSetPodsFollowsDirectOwnership(t *testing.T) {
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "kube-system"}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "agent-node1", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "agent"}}}}
	unrelated := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other-pod", Namespace: "kube-system", OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "other"}}}}
	client := fake.NewSimpleClientset(ds, pod, unrelated)

	pods, err := GetDaemonSetPods(client, "kube-system", ds)
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != pod.Name {
		t.Fatalf("expected only owned DaemonSet pod, got %+v", pods)
	}
}
