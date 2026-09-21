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
	Deployment  *appsv1.Deployment
	Pods        []corev1.Pod
	ReplicaSets []appsv1.ReplicaSet
	Status      Status
	Evidence    []Observation
	Findings    []Finding
	Summary     ReplicaSummary
	Limitations []string
	Graph       []EvidenceEdge
}

func VerifyDeployment(deployment *appsv1.Deployment, pods []corev1.Pod) DeploymentCheck {
	return verifyDeployment(deployment, nil, pods)
}

func VerifyDeploymentWithReplicaSets(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) DeploymentCheck {
	return verifyDeployment(deployment, replicaSets, pods)
}

func verifyDeployment(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) DeploymentCheck {
	status := StatusMatch
	observations := make([]Observation, 0)
	findings := make([]Finding, 0)
	limit := make([]string, 0)
	graph := make([]EvidenceEdge, 0)

	var desiredImages []string
	for _, c := range deployment.Spec.Template.Spec.Containers {
		desiredImages = append(desiredImages, c.Image)
	}
	for _, c := range deployment.Spec.Template.Spec.InitContainers {
		desiredImages = append(desiredImages, c.Image)
	}
	if len(desiredImages) == 0 {
		desiredImages = append(desiredImages, "<none>")
	}

	desiredText := strings.Join(desiredImages, ", ")
	graph = append(graph, EvidenceEdge{From: fmt.Sprintf("deployment/%s", deployment.Name), To: fmt.Sprintf("replicaset/%s", deployment.Name), Type: "declares"})
	for _, rs := range replicaSets {
		graph = append(graph, EvidenceEdge{From: fmt.Sprintf("deployment/%s", deployment.Name), To: fmt.Sprintf("replicaset/%s", rs.Name), Type: "owns", Evidence: []Observation{{Kind: "replicaset", Subject: rs.Name, Value: rs.Annotations["deployment.kubernetes.io/revision"], Expected: desiredText, Source: "ReplicaSet.metadata.annotations", Method: "kubernetes-api", Status: StatusMatch}}})
	}

	observedByPod := map[string]map[string]string{}
	for _, pod := range pods {
		podKey := fmt.Sprintf("pod/%s", pod.Name)
		observedByPod[podKey] = map[string]string{}
		for _, cs := range pod.Status.ContainerStatuses {
			observedByPod[podKey][cs.Name] = cs.ImageID
		}
		if len(pod.Status.ContainerStatuses) == 0 {
			observedByPod[podKey]["container"] = ""
		}
	}

	matching := 0
	divergent := 0
	total := len(pods)
	for _, pod := range pods {
		podName := pod.Name
		podMap := observedByPod[fmt.Sprintf("pod/%s", podName)]
		if len(podMap) == 0 {
			divergent++
			status = StatusPartial
			continue
		}
		for containerName, imageID := range podMap {
			containerStatus := findContainerStatus(&pod, containerName)
			obs := Observation{
				Kind: "container", Subject: podName + "/" + containerName, Value: imageID,
				Expected: desiredText, Source: "PodStatus.containerStatuses[].imageID", Timestamp: time.Now(), Method: "kubernetes-api", Status: StatusMatch,
			}
			if containerStatus != nil {
				obs.ObservedImage = containerStatus.Image
				obs.Ready = containerStatus.Ready
				obs.RestartCount = containerStatus.RestartCount
				obs.State = containerState(containerStatus)
			}
			if imageID == "" {
				obs.Status = StatusUnknown
				obs.Limitations = []string{"runtime identity not reported by the Kubernetes API"}
				status = StatusUnknown
				divergent++
				findings = append(findings, Finding{Code: "runtime-identity-unavailable", Status: StatusUnknown, Subject: obs.Subject, Message: "Kubernetes did not report a runtime image identity", Evidence: []Observation{obs}})
				observations = append(observations, obs)
				continue
			}
			if !isDesiredMatch(desiredImages, imageID) {
				obs.Status = StatusMismatch
				status = StatusPartial
				divergent++
				obs.Limitations = []string{"Deployment image and runtime imageID differ"}
				findings = append(findings, Finding{Code: "runtime-image-divergence", Status: StatusMismatch, Subject: obs.Subject, Message: "runtime image identity differs from the declared image repository", Evidence: []Observation{obs}})
			} else {
				matching++
			}
			observations = append(observations, obs)
		}
	}

	if total == 0 {
		status = StatusUnknown
		limit = append(limit, "no pods were found for this deployment")
	}
	desiredReplicas := 1
	if deployment.Spec.Replicas != nil {
		desiredReplicas = int(*deployment.Spec.Replicas)
	}
	summary := ReplicaSummary{Desired: desiredReplicas, Observed: total, Matching: matching, Divergent: divergent}
	replicaObservation := Observation{Kind: "deployment", Subject: deployment.Name, Expected: fmt.Sprintf("%d replicas", desiredReplicas), Value: fmt.Sprintf("%d Pods", total), Source: "Deployment.spec.replicas and namespace Pod list", Timestamp: time.Now(), Method: "kubernetes-api", Status: StatusMatch}
	if total < desiredReplicas {
		findings = append(findings, Finding{Code: "replica-count-drift", Status: StatusPartial, Subject: deployment.Name, Message: fmt.Sprintf("expected %d replicas but observed %d Pods", desiredReplicas, total), Evidence: []Observation{replicaObservation}})
		status = StatusPartial
	}
	if total > desiredReplicas {
		findings = append(findings, Finding{Code: "extra-replicas", Status: StatusPartial, Subject: deployment.Name, Message: fmt.Sprintf("expected %d replicas but observed %d Pods", desiredReplicas, total), Evidence: []Observation{replicaObservation}})
		status = StatusPartial
	}
	for _, observation := range observations {
		if observation.Status == StatusUnknown {
			summary.Unknown++
		}
	}
	for _, pod := range pods {
		graph = append(graph, EvidenceEdge{From: fmt.Sprintf("replicaset/%s", ownerReplicaSetName(&pod)), To: fmt.Sprintf("pod/%s", pod.Name), Type: "owns"})
	}
	for _, rs := range replicaSets {
		for _, pod := range pods {
			if ownerReplicaSetName(&pod) == rs.Name {
				graph = append(graph, EvidenceEdge{From: fmt.Sprintf("pod/%s", pod.Name), To: fmt.Sprintf("container/%s", pod.Name), Type: "contains"})
			}
		}
	}
	deploymentRevision := deployment.Annotations["deployment.kubernetes.io/revision"]
	if deploymentRevision != "" {
		for _, rs := range replicaSets {
			if rs.Annotations["deployment.kubernetes.io/revision"] != "" && rs.Annotations["deployment.kubernetes.io/revision"] != deploymentRevision {
				revisionObservation := Observation{Kind: "replicaset", Subject: rs.Name, Expected: deploymentRevision, Value: rs.Annotations["deployment.kubernetes.io/revision"], Source: "Deployment and ReplicaSet revision annotations", Timestamp: time.Now(), Method: "kubernetes-api", Status: StatusMismatch}
				findings = append(findings, Finding{Code: "stale-replicaset", Status: StatusStale, Subject: rs.Name, Message: "ReplicaSet revision differs from the Deployment revision annotation", Evidence: []Observation{revisionObservation}})
				status = StatusPartial
			}
		}
	}
	if divergent > 0 && matching > 0 {
		status = StatusPartial
	}
	if divergent == 0 && total > 0 && total == desiredReplicas && len(findings) == 0 {
		status = StatusMatch
	}
	if status == StatusMatch {
		limit = append(limit, "application-level secret and config consumption are not observable in the API-only model")
	}
	if status != StatusMatch {
		limit = append(limit, "application consumption semantics are UNOBSERVABLE without instrumentation")
		limit = append(limit, "runtime process internals are not proven by Kubernetes object state alone")
	}

	for _, obs := range observations {
		switch obs.Status {
		case StatusMismatch:
			graph = append(graph, EvidenceEdge{From: fmt.Sprintf("pod/%s", strings.Split(obs.Subject, "/")[0]), To: fmt.Sprintf("container/%s", obs.Subject), Type: "runtime-diff", Evidence: []Observation{obs}})
		}
	}
	if total > 0 {
		graph = append(graph, EvidenceEdge{From: "deployment", To: "runtime-identity", Type: "observed", Evidence: observations})
	}
	return DeploymentCheck{
		Deployment:  deployment,
		Pods:        pods,
		ReplicaSets: replicaSets,
		Status:      status,
		Evidence:    observations,
		Findings:    findings,
		Summary:     summary,
		Limitations: limit,
		Graph:       graph,
	}
}

func findContainerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == name {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
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

func isDesiredMatch(desired []string, imageID string) bool {
	if len(desired) == 0 {
		return false
	}
	for _, d := range desired {
		if d == "" {
			continue
		}
		if strings.Contains(imageID, d) || strings.Contains(d, imageID) {
			return true
		}
		if strings.Contains(d, "@") {
			if strings.Contains(imageID, strings.SplitN(d, "@", 2)[1]) {
				return true
			}
		}
		if strings.Contains(d, ":") {
			base := strings.SplitN(d, ":", 2)[0]
			if strings.Contains(imageID, base) && strings.Contains(imageID, "@sha256:") {
				return true
			}
		}
	}
	return false
}

func BuildDeploymentResult(deployment *appsv1.Deployment, pods []corev1.Pod) VerificationResult {
	check := VerifyDeployment(deployment, pods)
	return buildDeploymentResult(check, deployment)
}

func BuildDeploymentResultWithReplicaSets(deployment *appsv1.Deployment, replicaSets []appsv1.ReplicaSet, pods []corev1.Pod) VerificationResult {
	check := VerifyDeploymentWithReplicaSets(deployment, replicaSets, pods)
	return buildDeploymentResult(check, deployment)
}

func buildDeploymentResult(check DeploymentCheck, deployment *appsv1.Deployment) VerificationResult {
	res := VerificationResult{
		Claim:         "deployment runtime identity matches declared workload",
		Subject:       fmt.Sprintf("deployment/%s/%s", deployment.Namespace, deployment.Name),
		Desired:       "",
		Status:        check.Status,
		Source:        "Kubernetes API",
		Timestamp:     time.Now(),
		Method:        "read-only",
		Limitations:   check.Limitations,
		Observations:  check.Evidence,
		Findings:      check.Findings,
		Summary:       check.Summary,
		EvidenceChain: check.Graph,
	}
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		res.Desired = deployment.Spec.Template.Spec.Containers[0].Image
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		res.Desired = "<no containers>"
	}
	if len(check.Evidence) == 0 {
		res.Status = StatusUnknown
	}
	if check.Status == StatusMatch {
		res.Observed = "all observed pod imageIDs matched declared image identity"
	} else if check.Status == StatusPartial {
		res.Observed = fmt.Sprintf("%d pod(s) diverged from declared image identity", countMismatch(check.Evidence))
	} else if check.Status == StatusUnknown {
		res.Observed = "insufficient runtime identity evidence"
	}
	return res
}

func countMismatch(obs []Observation) int {
	count := 0
	for _, o := range obs {
		if o.Status == StatusMismatch {
			count++
		}
	}
	return count
}

func BuildScanSummary(deployments []appsv1.Deployment, results map[string]VerificationResult) string {
	if len(deployments) == 0 {
		return "No workloads found."
	}
	out := make([]string, 0, len(deployments))
	counts := map[Status]int{}
	for _, dep := range deployments {
		res, ok := results[dep.Name]
		if !ok {
			out = append(out, fmt.Sprintf("%s\tUNKNOWN", dep.Name))
			counts[StatusUnknown]++
			continue
		}
		out = append(out, fmt.Sprintf("%s\t%s", dep.Name, res.Status))
		counts[res.Status]++
	}
	sort.Strings(out)
	return fmt.Sprintf("%d workloads inspected\n\nMATCH\t%d\nPARTIAL\t%d\nUNKNOWN\t%d\n\n%s", len(deployments), counts[StatusMatch], counts[StatusPartial], counts[StatusUnknown], strings.Join(out, "\n"))
}

func DescribeWorkloadReference(secretName string, workloadName string) string {
	return fmt.Sprintf("Workload %s references Secret %s; application consumption remains UNOBSERVABLE without runtime or application instrumentation.", workloadName, secretName)
}

func DescribeConfigReference(configName string, workloadName string) string {
	return fmt.Sprintf("Workload %s references ConfigMap %s; effective application config is UNOBSERVABLE without runtime or application instrumentation.", workloadName, configName)
}

func DeploymentStatusText(status Status) string {
	return string(status)
}

func FindContainerImageID(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	if len(pod.Status.ContainerStatuses) == 0 {
		return ""
	}
	return pod.Status.ContainerStatuses[0].ImageID
}

func ListPodImageIDs(pods []corev1.Pod) []string {
	ids := make([]string, 0, len(pods))
	for _, pod := range pods {
		ids = append(ids, FindContainerImageID(&pod))
	}
	return ids
}

func UniqueImageIDs(ids []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0)
	for _, id := range ids {
		if id == "" {
			continue
		}
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result
}

func DeploymentReplicasFromPods(pods []corev1.Pod) int { return len(pods) }

func ResourceVersion(obj metav1.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetResourceVersion()
}
