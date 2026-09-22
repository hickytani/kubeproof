package truth

import (
	"fmt"
	"sort"
)

type ComparisonStatus string

const (
	ComparisonNoChange ComparisonStatus = "NO_CHANGE"
	ComparisonChanged  ComparisonStatus = "CHANGED"
)

type ComparisonCode string

const (
	ChangeRevision         ComparisonCode = "REVISION_CHANGED"
	ChangeReplicas         ComparisonCode = "REPLICAS_CHANGED"
	ChangePodAdded         ComparisonCode = "POD_ADDED"
	ChangePodRemoved       ComparisonCode = "POD_REMOVED"
	ChangePodReady         ComparisonCode = "POD_READINESS_CHANGED"
	ChangeContainerAdded   ComparisonCode = "CONTAINER_ADDED"
	ChangeContainerRemoved ComparisonCode = "CONTAINER_REMOVED"
	ChangeDeclaredImage    ComparisonCode = "DECLARED_IMAGE_CHANGED"
	ChangeRuntimeImage     ComparisonCode = "RUNTIME_IMAGE_CHANGED"
	ChangeRestartCount     ComparisonCode = "RESTART_COUNT_CHANGED"
)

type ComparisonChange struct {
	Code    ComparisonCode `json:"code"`
	Subject string         `json:"subject"`
	Before  string         `json:"before,omitempty"`
	After   string         `json:"after,omitempty"`
}
type ComparisonResult struct {
	SchemaVersion int                `json:"schemaVersion"`
	Workload      string             `json:"workload"`
	Status        ComparisonStatus   `json:"status"`
	Changes       []ComparisonChange `json:"changes"`
}

func CompareSnapshots(before, after VerificationResult) (ComparisonResult, error) {
	if before.SchemaVersion != 1 || after.SchemaVersion != 1 {
		return ComparisonResult{}, fmt.Errorf("SNAPSHOT_SCHEMA_MISMATCH: supported schemaVersion is 1")
	}
	if before.Subject == "" || before.Subject != after.Subject {
		return ComparisonResult{}, fmt.Errorf("WORKLOAD_IDENTITY_MISMATCH: snapshots refer to different workloads")
	}
	r := ComparisonResult{1, before.Subject, ComparisonNoChange, nil}
	add := func(c ComparisonCode, s, b, a string) { r.Changes = append(r.Changes, ComparisonChange{c, s, b, a}) }
	if before.Evidence.Deployment.CurrentRevision != after.Evidence.Deployment.CurrentRevision {
		add(ChangeRevision, before.Subject, before.Evidence.Deployment.CurrentRevision, after.Evidence.Deployment.CurrentRevision)
	}
	if before.Evidence.Deployment.ReadyReplicas != after.Evidence.Deployment.ReadyReplicas || before.Evidence.Deployment.DesiredReplicas != after.Evidence.Deployment.DesiredReplicas {
		add(ChangeReplicas, before.Subject, fmt.Sprintf("desired=%d ready=%d", before.Evidence.Deployment.DesiredReplicas, before.Evidence.Deployment.ReadyReplicas), fmt.Sprintf("desired=%d ready=%d", after.Evidence.Deployment.DesiredReplicas, after.Evidence.Deployment.ReadyReplicas))
	}
	comparePods(before.Evidence.Pods, after.Evidence.Pods, add)
	compareObs(before.Observations, after.Observations, add)
	sort.Slice(r.Changes, func(i, j int) bool {
		if r.Changes[i].Subject == r.Changes[j].Subject {
			return r.Changes[i].Code < r.Changes[j].Code
		}
		return r.Changes[i].Subject < r.Changes[j].Subject
	})
	if len(r.Changes) > 0 {
		r.Status = ComparisonChanged
	}
	return r, nil
}
func comparePods(b, a []PodEvidence, add func(ComparisonCode, string, string, string)) {
	bm, am := map[string]PodEvidence{}, map[string]PodEvidence{}
	for _, p := range b {
		bm[p.Namespace+"/"+p.Name] = p
	}
	for _, p := range a {
		am[p.Namespace+"/"+p.Name] = p
	}
	for k, p := range bm {
		q, ok := am[k]
		if !ok {
			add(ChangePodRemoved, "Pod/"+k, "present", "")
		} else if p.Ready != q.Ready {
			add(ChangePodReady, "Pod/"+k, fmt.Sprint(p.Ready), fmt.Sprint(q.Ready))
		}
	}
	for k := range am {
		if _, ok := bm[k]; !ok {
			add(ChangePodAdded, "Pod/"+k, "", "present")
		}
	}
}
func compareObs(b, a []Observation, add func(ComparisonCode, string, string, string)) {
	bm, am := map[string]Observation{}, map[string]Observation{}
	for _, o := range b {
		if o.Kind == "container" || o.Kind == "init-container" {
			bm[o.Kind+"/"+o.Subject] = o
		}
	}
	for _, o := range a {
		if o.Kind == "container" || o.Kind == "init-container" {
			am[o.Kind+"/"+o.Subject] = o
		}
	}
	for k, p := range bm {
		q, ok := am[k]
		if !ok {
			add(ChangeContainerRemoved, k, "present", "")
			continue
		}
		if p.Expected != q.Expected {
			add(ChangeDeclaredImage, k, p.Expected, q.Expected)
		}
		if p.Value != q.Value {
			add(ChangeRuntimeImage, k, p.Value, q.Value)
		}
		if p.RestartCount != q.RestartCount {
			add(ChangeRestartCount, k, fmt.Sprint(p.RestartCount), fmt.Sprint(q.RestartCount))
		}
	}
	for k := range am {
		if _, ok := bm[k]; !ok {
			add(ChangeContainerAdded, k, "", "present")
		}
	}
}
