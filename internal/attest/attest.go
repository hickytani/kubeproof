package attest

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"k8s-truth/internal/truth"
)

const (
	ToolName    = "stateproof"
	ToolVersion = "0.2.0"
)

// Attestation is a cryptographically signed runtime verification artifact.
type Attestation struct {
	SchemaVersion             int                       `json:"schemaVersion"`
	AttestationType           string                    `json:"attestationType"`
	CreatedAt                 string                    `json:"createdAt"`
	Tool                      string                    `json:"tool"`
	ToolVersion               string                    `json:"toolVersion"`
	Workload                  string                    `json:"workload"`
	Namespace                 string                    `json:"namespace"`
	Deployment                string                    `json:"deployment"`
	DeploymentRevision        string                    `json:"deploymentRevision"`
	ReplicaEvidence           ReplicaEvidence           `json:"replicaEvidence"`
	ExpectedDigests           []ExpectedDigestEntry     `json:"expectedDigests"`
	ObservedContainerEvidence []ObservedContainer       `json:"observedContainerEvidence"`
	VerificationResult        string                    `json:"verificationResult"`
	Findings                  []AttestationFinding      `json:"findings"`
	EvidenceGraph             []AttestationEvidenceEdge `json:"evidenceGraph"`
	Signer                    SignerInfo                `json:"signer"`
	Signature                 string                    `json:"signature"`
}

// ReplicaEvidence captures replica count evidence at observation time.
type ReplicaEvidence struct {
	Desired  int `json:"desired"`
	Observed int `json:"observed"`
	Matching int `json:"matching"`
}

// ExpectedDigestEntry maps a container name to the expected digest.
type ExpectedDigestEntry struct {
	Container string `json:"container"`
	Digest    string `json:"digest"`
}

// ObservedContainer captures per-container runtime evidence.
type ObservedContainer struct {
	ContainerName      string `json:"containerName"`
	PodName            string `json:"podName"`
	DeclaredImage      string `json:"declaredImage"`
	RuntimeImageID     string `json:"runtimeImageID"`
	CurrentRevision    bool   `json:"currentRevision"`
	Ready              bool   `json:"ready"`
	VerificationResult string `json:"verificationResult"`
}

// AttestationFinding captures a single finding from the verification.
type AttestationFinding struct {
	Code    string `json:"code"`
	Reason  string `json:"reason"`
	Status  string `json:"status"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// AttestationEvidenceEdge captures one edge in the evidence graph.
type AttestationEvidenceEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// SignerInfo identifies the signing key without including the key itself.
type SignerInfo struct {
	Algorithm           string `json:"algorithm"`
	PublicKeyFingerprint string `json:"publicKeyFingerprint"`
}

// NewAttestation creates an attestation from a successful verification result.
// It refuses to create an attestation if the verification result is not MATCH.
func NewAttestation(result truth.VerificationResult, expected map[string]string, publicKey ed25519.PublicKey) (*Attestation, error) {
	if result.Status != truth.StatusMatch {
		return nil, fmt.Errorf("attestation requires verification status MATCH, got %s", result.Status)
	}

	// Extract deployment name and namespace from the subject (e.g., "Deployment/production/payments").
	workload := result.Subject
	namespace := result.Evidence.Deployment.Namespace
	deploymentName := result.Evidence.Deployment.Name

	a := &Attestation{
		SchemaVersion:      1,
		AttestationType:    "kubeproof.runtime-verification/v1",
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
		Tool:               ToolName,
		ToolVersion:        ToolVersion,
		Workload:           workload,
		Namespace:          namespace,
		Deployment:         deploymentName,
		DeploymentRevision: result.Evidence.Deployment.CurrentRevision,
		ReplicaEvidence: ReplicaEvidence{
			Desired:  result.Summary.Desired,
			Observed: result.Summary.Observed,
			Matching: result.Summary.Matching,
		},
		VerificationResult: string(result.Status),
		Signer: SignerInfo{
			Algorithm:           "Ed25519",
			PublicKeyFingerprint: PublicKeyFingerprint(publicKey),
		},
	}

	// Populate expected digests.
	for container, digest := range expected {
		a.ExpectedDigests = append(a.ExpectedDigests, ExpectedDigestEntry{
			Container: container,
			Digest:    digest,
		})
	}

	// Populate observed container evidence from observations.
	for _, obs := range result.Observations {
		if obs.Kind != "container" && obs.Kind != "init-container" {
			continue
		}
		// Extract pod and container name from subject like "Container/default/podname/containername".
		parts := strings.Split(obs.Subject, "/")
		containerName := ""
		podName := ""
		if len(parts) >= 4 {
			podName = parts[2]
			containerName = parts[3]
		} else if len(parts) >= 2 {
			containerName = parts[len(parts)-1]
		}

		a.ObservedContainerEvidence = append(a.ObservedContainerEvidence, ObservedContainer{
			ContainerName:      containerName,
			PodName:            podName,
			DeclaredImage:      obs.Expected,
			RuntimeImageID:     obs.Value,
			CurrentRevision:    true, // only current-revision pods make it to observations
			Ready:              obs.Ready,
			VerificationResult: string(obs.Status),
		})
	}

	// Populate findings.
	for _, f := range result.Findings {
		a.Findings = append(a.Findings, AttestationFinding{
			Code:    f.Code,
			Reason:  string(f.Reason),
			Status:  string(f.Status),
			Subject: f.Subject,
			Message: f.Message,
		})
	}

	// Populate evidence graph edges.
	for _, edge := range result.EvidenceChain {
		a.EvidenceGraph = append(a.EvidenceGraph, AttestationEvidenceEdge{
			From: edge.From,
			To:   edge.To,
			Type: edge.Type,
		})
	}

	// Ensure nil slices become empty slices for deterministic JSON.
	if a.ExpectedDigests == nil {
		a.ExpectedDigests = []ExpectedDigestEntry{}
	}
	if a.ObservedContainerEvidence == nil {
		a.ObservedContainerEvidence = []ObservedContainer{}
	}
	if a.Findings == nil {
		a.Findings = []AttestationFinding{}
	}
	if a.EvidenceGraph == nil {
		a.EvidenceGraph = []AttestationEvidenceEdge{}
	}

	return a, nil
}

// SignAttestation signs an attestation with the given private key.
// It produces a canonical payload, signs it, and sets the hex-encoded signature.
func SignAttestation(a *Attestation, privateKey ed25519.PrivateKey) error {
	payload, err := CanonicalPayload(a)
	if err != nil {
		return fmt.Errorf("canonical payload: %w", err)
	}
	sig := Sign(payload, privateKey)
	a.Signature = hex.EncodeToString(sig)
	return nil
}
