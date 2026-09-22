package attest

import (
	"encoding/json"
	"sort"
)

// canonicalPayload is the subset of an Attestation that is signed.
// It uses only struct fields (no maps) to guarantee deterministic
// JSON serialization by encoding/json.
type canonicalPayload struct {
	SchemaVersion             int                        `json:"schemaVersion"`
	AttestationType           string                     `json:"attestationType"`
	CreatedAt                 string                     `json:"createdAt"`
	Tool                      string                     `json:"tool"`
	ToolVersion               string                     `json:"toolVersion"`
	Workload                  string                     `json:"workload"`
	Namespace                 string                     `json:"namespace"`
	Deployment                string                     `json:"deployment"`
	DeploymentRevision        string                     `json:"deploymentRevision"`
	ReplicaEvidence           ReplicaEvidence            `json:"replicaEvidence"`
	ExpectedDigests           []ExpectedDigestEntry      `json:"expectedDigests"`
	ObservedContainerEvidence []ObservedContainer        `json:"observedContainerEvidence"`
	VerificationResult        string                     `json:"verificationResult"`
	Findings                  []AttestationFinding       `json:"findings"`
	EvidenceGraph             []AttestationEvidenceEdge  `json:"evidenceGraph"`
	Signer                    SignerInfo                 `json:"signer"`
}

// CanonicalPayload serializes the signable fields of an Attestation into
// deterministic JSON. The same logical attestation always produces the
// same byte sequence.
func CanonicalPayload(a *Attestation) ([]byte, error) {
	// Sort slices to ensure deterministic ordering.
	expected := make([]ExpectedDigestEntry, len(a.ExpectedDigests))
	copy(expected, a.ExpectedDigests)
	sort.Slice(expected, func(i, j int) bool { return expected[i].Container < expected[j].Container })

	containers := make([]ObservedContainer, len(a.ObservedContainerEvidence))
	copy(containers, a.ObservedContainerEvidence)
	sort.Slice(containers, func(i, j int) bool {
		if containers[i].PodName == containers[j].PodName {
			return containers[i].ContainerName < containers[j].ContainerName
		}
		return containers[i].PodName < containers[j].PodName
	})

	findings := make([]AttestationFinding, len(a.Findings))
	copy(findings, a.Findings)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Subject == findings[j].Subject {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Subject < findings[j].Subject
	})

	edges := make([]AttestationEvidenceEdge, len(a.EvidenceGraph))
	copy(edges, a.EvidenceGraph)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})

	cp := canonicalPayload{
		SchemaVersion:             a.SchemaVersion,
		AttestationType:           a.AttestationType,
		CreatedAt:                 a.CreatedAt,
		Tool:                      a.Tool,
		ToolVersion:               a.ToolVersion,
		Workload:                  a.Workload,
		Namespace:                 a.Namespace,
		Deployment:                a.Deployment,
		DeploymentRevision:        a.DeploymentRevision,
		ReplicaEvidence:           a.ReplicaEvidence,
		ExpectedDigests:           expected,
		ObservedContainerEvidence: containers,
		VerificationResult:        a.VerificationResult,
		Findings:                  findings,
		EvidenceGraph:             edges,
		Signer:                    a.Signer,
	}

	return json.Marshal(cp)
}
