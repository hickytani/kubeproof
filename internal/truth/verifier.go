package truth

import (
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentCheck struct {
	Deployment    *appsv1.Deployment
	Pods          []corev1.Pod
	ReplicaSets   []appsv1.ReplicaSet
	Status        Status
	Evidence      []Observation
	Findings      []Finding
	Summary       ReplicaSummary
	Configuration ConfigurationEvidence
	Limitations   []string
	Graph         []EvidenceEdge
}

func VerifyDeployment(deployment *appsv1.Deployment, pods []corev1.Pod) DeploymentCheck {
	return verifyDeployment(deployment, nil, pods)
}
func VerifyDeploymentWithReplicaSets(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) DeploymentCheck {
	return verifyDeployment(deployment, replicaSets, pods)
}

func verifyDeployment(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) DeploymentCheck {
	status := StatusMatch
	observations, findings, graph := []Observation{}, []Finding{}, []EvidenceEdge{}
	limits := []string{"Kubernetes object state does not prove application behavior, process memory, or Secret consumption."}
	configuration := ConfigurationEvidence{Declared: configurationReferences(&deployment.Spec.Template.Spec, deployment.Namespace, ""), Limitations: []string{"Configuration references do not prove runtime materialization or application consumption."}}
	current := currentReplicaSets(deployment, replicaSets)
	ready := []corev1.Pod{}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if len(replicaSets) > 0 && !current[ownerReplicaSetName(&pod)] {
			findings = append(findings, Finding{Code: "old-revision-pod", Status: StatusStale, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: "Pod belongs to an older Deployment revision and is excluded from current-revision verification"})
			continue
		}
		if !podReady(&pod) {
			findings = append(findings, Finding{Code: "current-pod-not-ready", Status: StatusPartial, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: "Current-revision Pod is not Ready"})
			status = StatusPartial
			continue
		}
		ready = append(ready, pod)
	}
	if len(replicaSets) > 0 && len(current) == 0 {
		status = StatusUnknown
		limits = append(limits, "current ReplicaSet could not be identified from Deployment revision metadata")
	}
	desired := 1
	if deployment.Spec.Replicas != nil {
		desired = int(*deployment.Spec.Replicas)
	}
	if len(ready) < desired && status != StatusUnknown {
		status = StatusPartial
		findings = append(findings, Finding{Code: "current-ready-replicas-incomplete", Status: StatusPartial, Subject: objectID("Deployment", deployment.Namespace, deployment.Name), Message: fmt.Sprintf("expected %d ready current-revision Pods, found %d", desired, len(ready))})
	}
	for _, pod := range ready {
		graph = append(graph, EvidenceEdge{From: objectID("ReplicaSet", pod.Namespace, ownerReplicaSetName(&pod)), To: objectID("Pod", pod.Namespace, pod.Name), Type: "owns"})
		configuration.Observed = append(configuration.Observed, configurationReferences(&pod.Spec, deployment.Namespace, pod.Name)...)
		status = combineStatus(status, verifyContainerSet(&pod, deployment.Spec.Template.Spec.Containers, pod.Status.ContainerStatuses, false, &observations, &findings, &graph))
		status = combineStatus(status, verifyContainerSet(&pod, deployment.Spec.Template.Spec.InitContainers, pod.Status.InitContainerStatuses, true, &observations, &findings, &graph))
	}
	sort.Slice(configuration.Observed, func(i, j int) bool {
		return configurationReferenceKey(configuration.Observed[i]) < configurationReferenceKey(configuration.Observed[j])
	})
	if len(ready) == 0 {
		status = StatusUnknown
	}
	summary := ReplicaSummary{Desired: desired, Observed: len(ready)}
	for _, o := range observations {
		if o.Status == StatusMatch {
			summary.Matching++
		}
		if o.Status == StatusMismatch {
			summary.Divergent++
		}
		if o.Status == StatusUnknown {
			summary.Unknown++
		}
	}
	return DeploymentCheck{deployment, pods, replicaSets, status, observations, findings, summary, configuration, limits, graph}
}

func verifyContainerSet(pod *corev1.Pod, declared []corev1.Container, actual []corev1.ContainerStatus, init bool, observations *[]Observation, findings *[]Finding, graph *[]EvidenceEdge) Status {
	if len(declared) == 0 {
		return StatusMatch
	}
	byName := map[string]corev1.ContainerStatus{}
	for _, c := range actual {
		byName[c.Name] = c
	}
	result := StatusMatch
	kind, source := "container", "PodStatus.containerStatuses[].imageID"
	if init {
		kind, source = "init-container", "PodStatus.initContainerStatuses[].imageID"
	}
	for _, want := range declared {
		got, ok := byName[want.Name]
		if !ok {
			*findings = append(*findings, Finding{Code: "declared-container-missing", Status: StatusUnknown, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: fmt.Sprintf("Declared %s %q has no matching status", kind, want.Name)})
			result = combineStatus(result, StatusUnknown)
			continue
		}
		obs := Observation{Kind: kind, Subject: objectID("Container", pod.Namespace, pod.Name+"/"+want.Name), Value: got.ImageID, Expected: want.Image, ObservedImage: got.Image, Ready: got.Ready, RestartCount: got.RestartCount, State: containerState(&got), Source: source, Timestamp: time.Now(), Method: "kubernetes-api"}
		if got.ImageID == "" {
			obs.Status = StatusUnknown
			obs.Limitations = []string{"runtime image identity not reported"}
			result = combineStatus(result, StatusUnknown)
		} else if matched, reason := imageIdentityMatch(want.Image, got.ImageID); matched {
			obs.Status = StatusMatch
		} else if parseImageReference(want.Image).Digest == "" {
			obs.Status = StatusUnknown
			obs.Limitations = []string{reason}
			result = combineStatus(result, StatusUnknown)
		} else {
			obs.Status = StatusMismatch
			obs.Limitations = []string{reason}
			result = combineStatus(result, StatusPartial)
			*findings = append(*findings, Finding{Code: "runtime-image-divergence", Status: StatusMismatch, Subject: obs.Subject, Message: reason, Evidence: []Observation{obs}})
		}
		*observations = append(*observations, obs)
		*graph = append(*graph, EvidenceEdge{From: objectID("Pod", pod.Namespace, pod.Name), To: obs.Subject, Type: "contains", Evidence: []Observation{obs}})
	}
	for name := range byName {
		declaredName := false
		for _, c := range declared {
			if c.Name == name {
				declaredName = true
				break
			}
		}
		if !declaredName {
			*findings = append(*findings, Finding{Code: "unexpected-container-status", Status: StatusUnknown, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: fmt.Sprintf("Observed %s %q is not declared", kind, name)})
		}
	}
	return result
}

func currentReplicaSets(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet) map[string]bool {
	current := map[string]bool{}
	revision := deployment.Annotations["deployment.kubernetes.io/revision"]
	for _, rs := range replicaSets {
		if revision != "" && rs.Annotations["deployment.kubernetes.io/revision"] == revision {
			current[rs.Name] = true
		}
	}
	return current
}
func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
func combineStatus(left, right Status) Status {
	if left == StatusPartial || right == StatusPartial {
		return StatusPartial
	}
	if left == StatusUnknown || right == StatusUnknown {
		return StatusUnknown
	}
	return StatusMatch
}
func objectID(kind, namespace, name string) string {
	return fmt.Sprintf("%s/%s/%s", kind, namespace, name)
}
func containerState(status *corev1.ContainerStatus) string {
	if status.State.Running != nil {
		return "running"
	}
	if status.State.Waiting != nil {
		return "waiting"
	}
	if status.State.Terminated != nil {
		return "terminated"
	}
	return "unknown"
}
func ownerReplicaSetName(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			return owner.Name
		}
	}
	return "unknown"
}

func BuildDeploymentResult(deployment *appsv1.Deployment, pods []corev1.Pod) VerificationResult {
	return buildDeploymentResult(VerifyDeployment(deployment, pods), deployment)
}
func BuildDeploymentResultWithReplicaSets(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) VerificationResult {
	return buildDeploymentResult(VerifyDeploymentWithReplicaSets(deployment, replicaSets, pods), deployment)
}
func buildDeploymentResult(check DeploymentCheck, deployment *appsv1.Deployment) VerificationResult {
	res := VerificationResult{Claim: "current ready Deployment Pods run declared digest-pinned runtime identities", Subject: objectID("Deployment", deployment.Namespace, deployment.Name), Status: check.Status, Source: "Kubernetes API", Timestamp: time.Now(), Method: "read-only", Limitations: check.Limitations, Observations: check.Evidence, Findings: check.Findings, Summary: check.Summary, Configuration: check.Configuration, EvidenceChain: check.Graph}
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		res.Desired = deployment.Spec.Template.Spec.Containers[0].Image
	} else {
		res.Desired = "<no containers>"
	}
	switch res.Status {
	case StatusMatch:
		res.Observed = "all current ready Pod container identities matched digest-pinned declarations"
	case StatusPartial:
		res.Observed = "current revision has drift or is incomplete"
	case StatusUnknown:
		res.Observed = "insufficient immutable runtime identity evidence"
	}
	return res
}
func BuildScanSummary(deployments []appsv1.Deployment, results map[string]VerificationResult) string {
	if len(deployments) == 0 {
		return "No workloads found."
	}
	lines, counts := []string{}, map[Status]int{}
	for _, dep := range deployments {
		result, ok := results[dep.Name]
		if !ok {
			result.Status = StatusUnknown
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", dep.Name, result.Status))
		counts[result.Status]++
	}
	sort.Strings(lines)
	return fmt.Sprintf("%d workloads inspected\n\nMATCH\t%d\nPARTIAL\t%d\nUNKNOWN\t%d\n\n%s", len(deployments), counts[StatusMatch], counts[StatusPartial], counts[StatusUnknown], strings.Join(lines, "\n"))
}
func ResourceVersion(obj metav1.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetResourceVersion()
}
