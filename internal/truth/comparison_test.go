package truth

import "testing"

func TestCompareSnapshots(t *testing.T) {
	b := VerificationResult{SchemaVersion: 1, Subject: "Deployment/default/api", Evidence: EvidenceSnapshot{Deployment: DeploymentEvidence{CurrentRevision: "1"}, Pods: []PodEvidence{{Namespace: "default", Name: "old", Ready: true}}}, Observations: []Observation{{Kind: "container", Subject: "Container/default/old/api", Expected: "api@sha256:a", Value: "api@sha256:a"}}}
	a := b
	a.Evidence.Deployment.CurrentRevision = "2"
	a.Evidence.Pods = []PodEvidence{{Namespace: "default", Name: "new", Ready: true}}
	a.Observations = []Observation{{Kind: "container", Subject: "Container/default/new/api", Expected: "api@sha256:b", Value: "api@sha256:b"}}
	r, e := CompareSnapshots(b, a)
	if e != nil || r.Status != ComparisonChanged || len(r.Changes) < 3 {
		t.Fatalf("unexpected comparison %+v %v", r, e)
	}
	same, e := CompareSnapshots(b, b)
	if e != nil || same.Status != ComparisonNoChange {
		t.Fatal("same snapshots must not change")
	}
}
func TestCompareSnapshotValidation(t *testing.T) {
	_, e := CompareSnapshots(VerificationResult{SchemaVersion: 2, Subject: "a"}, VerificationResult{SchemaVersion: 2, Subject: "a"})
	if e == nil {
		t.Fatal("unsupported schema accepted")
	}
}
