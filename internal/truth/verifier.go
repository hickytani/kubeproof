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
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	sort.Slice(replicaSets, func(i, j int) bool { return replicaSets[i].Name < replicaSets[j].Name })
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if len(replicaSets) > 0 && !current[ownerReplicaSetName(&pod)] {
			findings = append(findings, Finding{Code: "old-revision-pod", Reason: ReasonOldRevisionIgnored, Status: StatusStale, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: "Pod belongs to an older Deployment revision and is excluded from current-revision verification"})
			continue
		}
		if !podReady(&pod) {
			findings = append(findings, Finding{Code: "current-pod-not-ready", Reason: ReasonPodNotReady, Status: StatusPartial, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: "Current-revision Pod is not Ready"})
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
		findings = append(findings, Finding{Code: "current-ready-replicas-incomplete", Reason: ReasonCurrentRevisionIncomplete, Status: StatusPartial, Subject: objectID("Deployment", deployment.Namespace, deployment.Name), Message: fmt.Sprintf("expected %d ready current-revision Pods, found %d", desired, len(ready))})
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
			reason := ReasonContainerMissing
			if init {
				reason = ReasonInitContainerMissing
			}
			*findings = append(*findings, Finding{Code: "declared-container-missing", Reason: reason, Status: StatusUnknown, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: fmt.Sprintf("Declared %s %q has no matching status", kind, want.Name)})
			result = combineStatus(result, StatusUnknown)
			continue
		}
		obs := Observation{Kind: kind, Subject: objectID("Container", pod.Namespace, pod.Name+"/"+want.Name), Value: got.ImageID, Expected: want.Image, ObservedImage: got.Image, Ready: got.Ready, RestartCount: got.RestartCount, State: containerState(&got), Source: source, Timestamp: time.Now(), Method: "kubernetes-api"}
		if got.ImageID == "" {
			obs.Status = StatusUnknown
			obs.Reason = ReasonImmutableIdentityUnavailable
			obs.Limitations = []string{"runtime image identity not reported"}
			result = combineStatus(result, StatusUnknown)
		} else if matched, reason := imageIdentityMatch(want.Image, got.ImageID); matched {
			obs.Status = StatusMatch
			obs.Reason = ReasonDigestMatch
		} else if parseImageReference(want.Image).Digest == "" {
			obs.Status = StatusUnknown
			obs.Reason = ReasonImmutableIdentityUnavailable
			obs.Limitations = []string{reason}
			result = combineStatus(result, StatusUnknown)
		} else {
			obs.Status = StatusMismatch
			obs.Reason = ReasonDigestMismatch
			obs.Limitations = []string{reason}
			result = combineStatus(result, StatusPartial)
			*findings = append(*findings, Finding{Code: "runtime-image-divergence", Reason: ReasonDigestMismatch, Status: StatusMismatch, Subject: obs.Subject, Message: reason, Evidence: []Observation{obs}})
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
			reason := ReasonUnexpectedContainer
			if init {
				reason = ReasonUnexpectedInitContainer
			}
			*findings = append(*findings, Finding{Code: "unexpected-container-status", Reason: reason, Status: StatusUnknown, Subject: objectID("Pod", pod.Namespace, pod.Name), Message: fmt.Sprintf("Observed %s %q is not declared", kind, name)})
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
	sort.Slice(check.Findings, func(i, j int) bool {
		if check.Findings[i].Subject == check.Findings[j].Subject {
			return check.Findings[i].Reason < check.Findings[j].Reason
		}
		return check.Findings[i].Subject < check.Findings[j].Subject
	})
	sort.Slice(check.Evidence, func(i, j int) bool { return check.Evidence[i].Subject < check.Evidence[j].Subject })
	res := VerificationResult{SchemaVersion: 1, Claim: "current ready Deployment Pods run declared digest-pinned runtime identities", Subject: objectID("Deployment", deployment.Namespace, deployment.Name), Status: check.Status, Source: "Kubernetes API", Timestamp: time.Now(), Method: "read-only", Limitations: check.Limitations, Observations: check.Evidence, Findings: check.Findings, Summary: check.Summary, Configuration: check.Configuration, Evidence: normalizedEvidence(deployment, check), EvidenceChain: check.Graph}
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
func normalizedEvidence(deployment *appsv1.Deployment, check DeploymentCheck) EvidenceSnapshot {
	e := EvidenceSnapshot{Deployment: DeploymentEvidence{Namespace: deployment.Namespace, Name: deployment.Name, CurrentRevision: deployment.Annotations["deployment.kubernetes.io/revision"], Generation: deployment.Generation, ObservedGeneration: deployment.Status.ObservedGeneration, AvailableReplicas: deployment.Status.AvailableReplicas, UpdatedReplicas: deployment.Status.UpdatedReplicas, ReadyReplicas: deployment.Status.ReadyReplicas}}
	if deployment.Spec.Replicas != nil {
		e.Deployment.DesiredReplicas = *deployment.Spec.Replicas
	}
	current := currentReplicaSets(deployment, check.ReplicaSets)
	for _, rs := range check.ReplicaSets {
		d := int32(0)
		if rs.Spec.Replicas != nil {
			d = *rs.Spec.Replicas
		}
		e.ReplicaSets = append(e.ReplicaSets, ReplicaSetEvidence{rs.Namespace, rs.Name, rs.Annotations["deployment.kubernetes.io/revision"], deployment.Name, d, current[rs.Name]})
	}
	for _, pod := range check.Pods {
		e.Pods = append(e.Pods, PodEvidence{
			Namespace:               pod.Namespace,
			Name:                    pod.Name,
			UID:                     string(pod.UID),
			Phase:                   string(pod.Status.Phase),
			OwnerReplicaSet:         ownerReplicaSetName(&pod),
			OwnerControllerRevision: "",
			Revision:                "",
			Ready:                   podReady(&pod),
			Terminating:             pod.DeletionTimestamp != nil,
		})
	}
	return e
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

type StatefulSetCheck struct {
	StatefulSet         *appsv1.StatefulSet
	Pods                []corev1.Pod
	ControllerRevisions []appsv1.ControllerRevision
	Status              Status
	Evidence            []Observation
	Findings            []Finding
	Summary             ReplicaSummary
	Configuration       ConfigurationEvidence
	Limitations         []string
	Graph               []EvidenceEdge
}

type DaemonSetCheck struct {
	DaemonSet           *appsv1.DaemonSet
	Pods                []corev1.Pod
	ControllerRevisions []appsv1.ControllerRevision
	Status              Status
	Evidence            []Observation
	Findings            []Finding
	Summary             ReplicaSummary
	Configuration       ConfigurationEvidence
	Limitations         []string
	Graph               []EvidenceEdge
}

func VerifyStatefulSet(sts *appsv1.StatefulSet, pods []corev1.Pod) StatefulSetCheck {
	return VerifyStatefulSetWithControllerRevisions(sts, nil, pods)
}

func VerifyStatefulSetWithControllerRevisions(sts *appsv1.StatefulSet, controllerRevisions []appsv1.ControllerRevision, pods []corev1.Pod) StatefulSetCheck {
	status := StatusMatch
	observations, findings, graph := []Observation{}, []Finding{}, []EvidenceEdge{}
	limits := []string{"Kubernetes object state does not prove application behavior, process memory, or Secret consumption."}
	configuration := ConfigurationEvidence{
		Declared:    configurationReferences(&sts.Spec.Template.Spec, sts.Namespace, ""),
		Limitations: []string{"Configuration references do not prove runtime materialization or application consumption."},
	}

	targetRevisionHash := sts.Status.UpdateRevision
	if targetRevisionHash == "" {
		targetRevisionHash = sts.Status.CurrentRevision
	}
	if targetRevisionHash == "" && len(controllerRevisions) > 0 {
		var maxRev *appsv1.ControllerRevision
		for i := range controllerRevisions {
			cr := &controllerRevisions[i]
			if isOwnedBy(cr.OwnerReferences, "StatefulSet", sts.Name) {
				if maxRev == nil || cr.Revision > maxRev.Revision {
					maxRev = cr
				}
			}
		}
		if maxRev != nil {
			targetRevisionHash = maxRev.Name
		}
	}

	if targetRevisionHash == "" {
		status = StatusUnknown
		limits = append(limits, "current ControllerRevision could not be identified from StatefulSet status or revision metadata")
	}

	ready := []corev1.Pod{}
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	sort.Slice(controllerRevisions, func(i, j int) bool { return controllerRevisions[i].Name < controllerRevisions[j].Name })

	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		podRev := podControllerRevisionHash(&pod)
		if targetRevisionHash != "" && podRev != "" && !isRevisionMatch(podRev, targetRevisionHash, controllerRevisions) {
			findings = append(findings, Finding{
				Code:    "old-revision-pod",
				Reason:  ReasonOldRevisionIgnored,
				Status:  StatusStale,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Pod belongs to an older StatefulSet revision and is excluded from current-revision verification",
			})
			continue
		}
		if targetRevisionHash == "" || podRev == "" {
			findings = append(findings, Finding{
				Code:    "unknown-revision-pod",
				Reason:  ReasonImmutableIdentityUnavailable,
				Status:  StatusUnknown,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Pod controller-revision-hash is missing or unresolvable",
			})
			status = StatusUnknown
			continue
		}
		if !podReady(&pod) {
			findings = append(findings, Finding{
				Code:    "current-pod-not-ready",
				Reason:  ReasonPodNotReady,
				Status:  StatusPartial,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Current-revision Pod is not Ready",
			})
			status = StatusPartial
			continue
		}
		ready = append(ready, pod)
	}

	desired := 1
	if sts.Spec.Replicas != nil {
		desired = int(*sts.Spec.Replicas)
	}
	if len(ready) < desired && status != StatusUnknown {
		status = StatusPartial
		findings = append(findings, Finding{
			Code:    "current-ready-replicas-incomplete",
			Reason:  ReasonCurrentRevisionIncomplete,
			Status:  StatusPartial,
			Subject: objectID("StatefulSet", sts.Namespace, sts.Name),
			Message: fmt.Sprintf("expected %d ready current-revision Pods, found %d", desired, len(ready)),
		})
	}
	if len(ready) == 0 {
		status = StatusUnknown
	}

	for _, pod := range ready {
		rev := podControllerRevisionHash(&pod)
		graph = append(graph, EvidenceEdge{
			From: objectID("ControllerRevision", pod.Namespace, rev),
			To:   objectID("Pod", pod.Namespace, pod.Name),
			Type: "owns",
		})
		configuration.Observed = append(configuration.Observed, configurationReferences(&pod.Spec, sts.Namespace, pod.Name)...)
		status = combineStatus(status, verifyContainerSet(&pod, sts.Spec.Template.Spec.Containers, pod.Status.ContainerStatuses, false, &observations, &findings, &graph))
		status = combineStatus(status, verifyContainerSet(&pod, sts.Spec.Template.Spec.InitContainers, pod.Status.InitContainerStatuses, true, &observations, &findings, &graph))
	}

	if targetRevisionHash != "" {
		graph = append(graph, EvidenceEdge{
			From: objectID("StatefulSet", sts.Namespace, sts.Name),
			To:   objectID("ControllerRevision", sts.Namespace, targetRevisionHash),
			Type: "owns",
		})
	}

	sort.Slice(configuration.Observed, func(i, j int) bool {
		return configurationReferenceKey(configuration.Observed[i]) < configurationReferenceKey(configuration.Observed[j])
	})

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

	return StatefulSetCheck{
		StatefulSet:         sts,
		Pods:                pods,
		ControllerRevisions: controllerRevisions,
		Status:              status,
		Evidence:            observations,
		Findings:            findings,
		Summary:             summary,
		Configuration:       configuration,
		Limitations:         limits,
		Graph:               graph,
	}
}

func VerifyDaemonSet(ds *appsv1.DaemonSet, pods []corev1.Pod) DaemonSetCheck {
	return VerifyDaemonSetWithControllerRevisions(ds, nil, pods)
}

func VerifyDaemonSetWithControllerRevisions(ds *appsv1.DaemonSet, controllerRevisions []appsv1.ControllerRevision, pods []corev1.Pod) DaemonSetCheck {
	status := StatusMatch
	observations, findings, graph := []Observation{}, []Finding{}, []EvidenceEdge{}
	limits := []string{"Kubernetes object state does not prove application behavior, process memory, or Secret consumption."}
	configuration := ConfigurationEvidence{
		Declared:    configurationReferences(&ds.Spec.Template.Spec, ds.Namespace, ""),
		Limitations: []string{"Configuration references do not prove runtime materialization or application consumption."},
	}

	targetRevisionHash := ""
	var maxRev *appsv1.ControllerRevision
	for i := range controllerRevisions {
		cr := &controllerRevisions[i]
		if isOwnedBy(cr.OwnerReferences, "DaemonSet", ds.Name) {
			if maxRev == nil || cr.Revision > maxRev.Revision {
				maxRev = cr
			}
		}
	}
	if maxRev != nil {
		targetRevisionHash = maxRev.Name
	}

	if targetRevisionHash == "" && len(pods) > 0 {
		firstRev := podControllerRevisionHash(&pods[0])
		allSame := true
		for _, p := range pods {
			if podControllerRevisionHash(&p) != firstRev || firstRev == "" {
				allSame = false
				break
			}
		}
		if allSame && firstRev != "" {
			targetRevisionHash = firstRev
		}
	}

	if targetRevisionHash == "" {
		status = StatusUnknown
		limits = append(limits, "current ControllerRevision could not be identified from DaemonSet revision metadata")
	}

	ready := []corev1.Pod{}
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	sort.Slice(controllerRevisions, func(i, j int) bool { return controllerRevisions[i].Name < controllerRevisions[j].Name })

	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		podRev := podControllerRevisionHash(&pod)
		if targetRevisionHash != "" && podRev != "" && !isRevisionMatch(podRev, targetRevisionHash, controllerRevisions) {
			findings = append(findings, Finding{
				Code:    "old-revision-pod",
				Reason:  ReasonOldRevisionIgnored,
				Status:  StatusStale,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Pod belongs to an older DaemonSet revision and is excluded from current-revision verification",
			})
			continue
		}
		if targetRevisionHash == "" || podRev == "" {
			findings = append(findings, Finding{
				Code:    "unknown-revision-pod",
				Reason:  ReasonImmutableIdentityUnavailable,
				Status:  StatusUnknown,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Pod controller-revision-hash is missing or unresolvable",
			})
			status = StatusUnknown
			continue
		}
		if !podReady(&pod) {
			findings = append(findings, Finding{
				Code:    "current-pod-not-ready",
				Reason:  ReasonPodNotReady,
				Status:  StatusPartial,
				Subject: objectID("Pod", pod.Namespace, pod.Name),
				Message: "Current-revision Pod is not Ready",
			})
			status = StatusPartial
			continue
		}
		ready = append(ready, pod)
	}

	desired := int(ds.Status.DesiredNumberScheduled)
	if desired == 0 && ds.Status.CurrentNumberScheduled > 0 {
		desired = int(ds.Status.CurrentNumberScheduled)
	}
	if len(ready) < desired && status != StatusUnknown {
		status = StatusPartial
		findings = append(findings, Finding{
			Code:    "current-ready-replicas-incomplete",
			Reason:  ReasonCurrentRevisionIncomplete,
			Status:  StatusPartial,
			Subject: objectID("DaemonSet", ds.Namespace, ds.Name),
			Message: fmt.Sprintf("expected %d ready current-revision Pods, found %d", desired, len(ready)),
		})
	}
	if len(ready) == 0 {
		status = StatusUnknown
	}

	for _, pod := range ready {
		rev := podControllerRevisionHash(&pod)
		graph = append(graph, EvidenceEdge{
			From: objectID("ControllerRevision", pod.Namespace, rev),
			To:   objectID("Pod", pod.Namespace, pod.Name),
			Type: "owns",
		})
		configuration.Observed = append(configuration.Observed, configurationReferences(&pod.Spec, ds.Namespace, pod.Name)...)
		status = combineStatus(status, verifyContainerSet(&pod, ds.Spec.Template.Spec.Containers, pod.Status.ContainerStatuses, false, &observations, &findings, &graph))
		status = combineStatus(status, verifyContainerSet(&pod, ds.Spec.Template.Spec.InitContainers, pod.Status.InitContainerStatuses, true, &observations, &findings, &graph))
	}

	if targetRevisionHash != "" {
		graph = append(graph, EvidenceEdge{
			From: objectID("DaemonSet", ds.Namespace, ds.Name),
			To:   objectID("ControllerRevision", ds.Namespace, targetRevisionHash),
			Type: "owns",
		})
	}

	sort.Slice(configuration.Observed, func(i, j int) bool {
		return configurationReferenceKey(configuration.Observed[i]) < configurationReferenceKey(configuration.Observed[j])
	})

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

	return DaemonSetCheck{
		DaemonSet:           ds,
		Pods:                pods,
		ControllerRevisions: controllerRevisions,
		Status:              status,
		Evidence:            observations,
		Findings:            findings,
		Summary:             summary,
		Configuration:       configuration,
		Limitations:         limits,
		Graph:               graph,
	}
}

func BuildStatefulSetResultWithControllerRevisions(sts *appsv1.StatefulSet, controllerRevisions []appsv1.ControllerRevision, pods []corev1.Pod) VerificationResult {
	return buildStatefulSetResult(VerifyStatefulSetWithControllerRevisions(sts, controllerRevisions, pods), sts)
}

func buildStatefulSetResult(check StatefulSetCheck, sts *appsv1.StatefulSet) VerificationResult {
	sort.Slice(check.Findings, func(i, j int) bool {
		if check.Findings[i].Subject == check.Findings[j].Subject {
			return check.Findings[i].Reason < check.Findings[j].Reason
		}
		return check.Findings[i].Subject < check.Findings[j].Subject
	})
	sort.Slice(check.Evidence, func(i, j int) bool { return check.Evidence[i].Subject < check.Evidence[j].Subject })
	res := VerificationResult{
		SchemaVersion: 1,
		Claim:         "current ready StatefulSet Pods run declared digest-pinned runtime identities",
		Subject:       objectID("StatefulSet", sts.Namespace, sts.Name),
		Status:        check.Status,
		Source:        "Kubernetes API",
		Timestamp:     time.Now(),
		Method:        "read-only",
		Limitations:   check.Limitations,
		Observations:  check.Evidence,
		Findings:      check.Findings,
		Summary:       check.Summary,
		Configuration: check.Configuration,
		Evidence:      normalizedStatefulSetEvidence(sts, check),
		EvidenceChain: check.Graph,
	}
	if len(sts.Spec.Template.Spec.Containers) > 0 {
		res.Desired = sts.Spec.Template.Spec.Containers[0].Image
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

func normalizedStatefulSetEvidence(sts *appsv1.StatefulSet, check StatefulSetCheck) EvidenceSnapshot {
	curRev := sts.Status.CurrentRevision
	if curRev == "" {
		curRev = sts.Status.UpdateRevision
	}
	e := EvidenceSnapshot{
		StatefulSet: StatefulSetEvidence{
			Namespace:          sts.Namespace,
			Name:               sts.Name,
			CurrentRevision:    curRev,
			UpdateRevision:     sts.Status.UpdateRevision,
			Generation:         sts.Generation,
			ObservedGeneration: sts.Status.ObservedGeneration,
			ReadyReplicas:      sts.Status.ReadyReplicas,
			UpdatedReplicas:    sts.Status.UpdatedReplicas,
		},
	}
	if sts.Spec.Replicas != nil {
		e.StatefulSet.DesiredReplicas = *sts.Spec.Replicas
	}
	for _, cr := range check.ControllerRevisions {
		e.ControllerRevisions = append(e.ControllerRevisions, ControllerRevisionEvidence{
			Namespace:     cr.Namespace,
			Name:          cr.Name,
			Revision:      cr.Revision,
			OwnerWorkload: sts.Name,
			Current:       cr.Name == curRev || cr.Name == sts.Status.UpdateRevision,
		})
	}
	for _, pod := range check.Pods {
		rev := podControllerRevisionHash(&pod)
		e.Pods = append(e.Pods, PodEvidence{
			Namespace:               pod.Namespace,
			Name:                    pod.Name,
			UID:                     string(pod.UID),
			Phase:                   string(pod.Status.Phase),
			OwnerControllerRevision: rev,
			Revision:                rev,
			Ready:                   podReady(&pod),
			Terminating:             pod.DeletionTimestamp != nil,
		})
	}
	return e
}

func BuildDaemonSetResultWithControllerRevisions(ds *appsv1.DaemonSet, controllerRevisions []appsv1.ControllerRevision, pods []corev1.Pod) VerificationResult {
	return buildDaemonSetResult(VerifyDaemonSetWithControllerRevisions(ds, controllerRevisions, pods), ds)
}

func buildDaemonSetResult(check DaemonSetCheck, ds *appsv1.DaemonSet) VerificationResult {
	sort.Slice(check.Findings, func(i, j int) bool {
		if check.Findings[i].Subject == check.Findings[j].Subject {
			return check.Findings[i].Reason < check.Findings[j].Reason
		}
		return check.Findings[i].Subject < check.Findings[j].Subject
	})
	sort.Slice(check.Evidence, func(i, j int) bool { return check.Evidence[i].Subject < check.Evidence[j].Subject })
	res := VerificationResult{
		SchemaVersion: 1,
		Claim:         "current ready DaemonSet Pods run declared digest-pinned runtime identities",
		Subject:       objectID("DaemonSet", ds.Namespace, ds.Name),
		Status:        check.Status,
		Source:        "Kubernetes API",
		Timestamp:     time.Now(),
		Method:        "read-only",
		Limitations:   check.Limitations,
		Observations:  check.Evidence,
		Findings:      check.Findings,
		Summary:       check.Summary,
		Configuration: check.Configuration,
		Evidence:      normalizedDaemonSetEvidence(ds, check),
		EvidenceChain: check.Graph,
	}
	if len(ds.Spec.Template.Spec.Containers) > 0 {
		res.Desired = ds.Spec.Template.Spec.Containers[0].Image
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

func normalizedDaemonSetEvidence(ds *appsv1.DaemonSet, check DaemonSetCheck) EvidenceSnapshot {
	curRev := ""
	for _, cr := range check.ControllerRevisions {
		if curRev == "" || cr.Name == curRev {
			curRev = cr.Name
		}
	}
	e := EvidenceSnapshot{
		DaemonSet: DaemonSetEvidence{
			Namespace:              ds.Namespace,
			Name:                   ds.Name,
			CurrentRevision:        curRev,
			Generation:             ds.Generation,
			ObservedGeneration:     ds.Status.ObservedGeneration,
			DesiredNumberScheduled: ds.Status.DesiredNumberScheduled,
			CurrentNumberScheduled: ds.Status.CurrentNumberScheduled,
			NumberReady:            ds.Status.NumberReady,
			UpdatedNumberScheduled: ds.Status.UpdatedNumberScheduled,
		},
	}
	for _, cr := range check.ControllerRevisions {
		e.ControllerRevisions = append(e.ControllerRevisions, ControllerRevisionEvidence{
			Namespace:     cr.Namespace,
			Name:          cr.Name,
			Revision:      cr.Revision,
			OwnerWorkload: ds.Name,
			Current:       cr.Name == curRev,
		})
	}
	for _, pod := range check.Pods {
		rev := podControllerRevisionHash(&pod)
		e.Pods = append(e.Pods, PodEvidence{
			Namespace:               pod.Namespace,
			Name:                    pod.Name,
			UID:                     string(pod.UID),
			Phase:                   string(pod.Status.Phase),
			OwnerControllerRevision: rev,
			Revision:                rev,
			Ready:                   podReady(&pod),
			Terminating:             pod.DeletionTimestamp != nil,
		})
	}
	return e
}

func isOwnedBy(owners []metav1.OwnerReference, kind, name string) bool {
	for _, owner := range owners {
		if owner.Kind == kind && owner.Name == name {
			return true
		}
	}
	return false
}

func podControllerRevisionHash(pod *corev1.Pod) string {
	if val, ok := pod.Labels["controller-revision-hash"]; ok && val != "" {
		return val
	}
	if val, ok := pod.Labels["controller.kubernetes.io/revision-hash"]; ok && val != "" {
		return val
	}
	if val, ok := pod.Annotations["controller-revision-hash"]; ok && val != "" {
		return val
	}
	return ""
}

func isRevisionMatch(podRev, targetRev string, controllerRevisions []appsv1.ControllerRevision) bool {
	if podRev == targetRev {
		return true
	}
	if strings.HasSuffix(targetRev, podRev) || strings.HasSuffix(podRev, targetRev) {
		return true
	}
	for _, cr := range controllerRevisions {
		if cr.Name == podRev && (cr.Name == targetRev || cr.Labels["controller-revision-hash"] == targetRev) {
			return true
		}
		if cr.Name == targetRev && cr.Labels["controller-revision-hash"] == podRev {
			return true
		}
	}
	return false
}

func ResourceVersion(obj metav1.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetResourceVersion()
}
