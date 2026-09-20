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
	Deployment *appsv1.Deployment
	Pods       []corev1.Pod
	Status     Status
	Evidence   []Observation
	Limitations []string
	Graph      []EvidenceEdge
}

func VerifyDeployment(deployment *appsv1.Deployment, pods []corev1.Pod) DeploymentCheck {
	status := StatusMatch
	observations := make([]Observation, 0)
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

	_ = strings.Join(desiredImages, ", ")
	graph = append(graph, EvidenceEdge{From: fmt.Sprintf("deployment/%s", deployment.Name), To: fmt.Sprintf("replicaset/%s", deployment.Name), Type: "declares"})

	observedByPod := map[string]map[string]string{}
	for _, pod := range pods {
		podKey := fmt.Sprintf("pod/%s", pod.Name)
		observedByPod[podKey] = map[string]string{}
		for _, cs := range pod.Status.ContainerStatuses {
			observedByPod[podKey][cs.Name] = cs.ImageID
		}
		if len(pod.Status.ContainerStatuses) == 0 {
			observedByPod[podKey]["container"] = "<no runtime image status>"
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
			obs := Observation{
				Kind:      "container",
				Subject:   podName + "/" + containerName,
				Value:     imageID,
				Source:    "PodStatus.containerStatuses[].imageID",
				Timestamp: time.Now(),
				Method:    "kubernetes-api",
				Status:    StatusMatch,
			}
			if imageID == "" {
				obs.Status = StatusUnknown
				obs.Limitations = []string{"runtime identity not reported by the Kubernetes API"}
				status = StatusUnknown
				divergent++
				continue
			}
			if !isDesiredMatch(desiredImages, imageID) {
				obs.Status = StatusMismatch
				status = StatusPartial
				divergent++
				obs.Limitations = []string{"Deployment image and runtime imageID differ"}
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
	if divergent > 0 && matching > 0 {
		status = StatusPartial
	}
	if divergent == 0 && total > 0 {
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
		Deployment: deployment,
		Pods:       pods,
		Status:     status,
		Evidence:   observations,
		Limitations: limit,
		Graph:      graph,
	}
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
	for _, dep := range deployments {
		res, ok := results[dep.Name]
		if !ok {
			out = append(out, fmt.Sprintf("%s\tUNKNOWN", dep.Name))
			continue
		}
		out = append(out, fmt.Sprintf("%s\t%s", dep.Name, res.Status))
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
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
