package attest

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// VerificationReport contains the results of offline attestation verification.
type VerificationReport struct {
	Valid              bool                    `json:"valid"`
	SignatureValid     bool                    `json:"signatureValid"`
	PayloadIntegrity   bool                    `json:"payloadIntegrity"`
	SchemaSupported    bool                    `json:"schemaSupported"`
	ResultVerified     bool                    `json:"resultVerified"`
	EvidencePresent    bool                    `json:"evidencePresent"`
	DigestsVerified    bool                    `json:"digestsVerified"`
	TimestampPresent   bool                    `json:"timestampPresent"`
	WorkloadIdentified bool                    `json:"workloadIdentified"`
	Attestation        *Attestation            `json:"attestation,omitempty"`
	Checks             []VerificationCheck     `json:"checks"`
	Errors             []string                `json:"errors,omitempty"`
}

// VerificationCheck represents a single verification check result.
type VerificationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // PASS, FAIL
	Detail string `json:"detail,omitempty"`
}

// VerifyAttestation performs offline verification of a signed attestation.
// It does NOT require Kubernetes access.
func VerifyAttestation(a *Attestation, publicKey ed25519.PublicKey) (*VerificationReport, error) {
	report := &VerificationReport{
		Attestation: a,
	}
	allPassed := true

	addCheck := func(name, status, detail string) {
		report.Checks = append(report.Checks, VerificationCheck{Name: name, Status: status, Detail: detail})
		if status == "FAIL" {
			allPassed = false
			report.Errors = append(report.Errors, name+": "+detail)
		}
	}

	// 1. Schema version
	if a.SchemaVersion == 1 {
		report.SchemaSupported = true
		addCheck("schema_version", "PASS", "schemaVersion 1 supported")
	} else {
		addCheck("schema_version", "FAIL", fmt.Sprintf("unsupported schemaVersion %d", a.SchemaVersion))
	}

	// 2. Signer information
	if a.Signer.Algorithm == "" || a.Signer.PublicKeyFingerprint == "" {
		addCheck("signer_present", "FAIL", "missing signer information")
	} else {
		addCheck("signer_present", "PASS", fmt.Sprintf("algorithm=%s fingerprint=%s", a.Signer.Algorithm, a.Signer.PublicKeyFingerprint))
	}

	// 3. Signer fingerprint matches provided public key
	expectedFingerprint := PublicKeyFingerprint(publicKey)
	if a.Signer.PublicKeyFingerprint == expectedFingerprint {
		addCheck("signer_fingerprint_match", "PASS", "public key fingerprint matches attestation signer")
	} else {
		addCheck("signer_fingerprint_match", "FAIL", fmt.Sprintf("attestation signer fingerprint %s does not match provided key %s", a.Signer.PublicKeyFingerprint, expectedFingerprint))
	}

	// 4. Signature present
	if a.Signature == "" {
		addCheck("signature_present", "FAIL", "missing signature")
		report.Valid = false
		return report, nil
	}
	addCheck("signature_present", "PASS", "signature present")

	// 5. Decode signature
	sigBytes, err := hex.DecodeString(a.Signature)
	if err != nil {
		addCheck("signature_decode", "FAIL", fmt.Sprintf("invalid signature encoding: %v", err))
		report.Valid = false
		return report, nil
	}
	addCheck("signature_decode", "PASS", "signature hex decoded")

	// 6. Reconstruct canonical payload and verify signature
	payload, err := CanonicalPayload(a)
	if err != nil {
		addCheck("canonical_payload", "FAIL", fmt.Sprintf("failed to reconstruct canonical payload: %v", err))
		report.Valid = false
		return report, nil
	}
	addCheck("canonical_payload", "PASS", "canonical payload reconstructed")

	if Verify(payload, sigBytes, publicKey) {
		report.SignatureValid = true
		report.PayloadIntegrity = true
		addCheck("signature_verify", "PASS", "Ed25519 signature valid")
	} else {
		addCheck("signature_verify", "FAIL", "Ed25519 signature verification failed")
		report.Valid = false
		return report, nil
	}

	// 7. Verification result
	if a.VerificationResult == string(StatusMatch) {
		report.ResultVerified = true
		addCheck("verification_result", "PASS", "verification result is MATCH")
	} else {
		addCheck("verification_result", "FAIL", fmt.Sprintf("verification result is %s, not MATCH", a.VerificationResult))
	}

	// 8. Workload identity
	if a.Workload != "" && a.Namespace != "" && a.Deployment != "" {
		report.WorkloadIdentified = true
		addCheck("workload_identity", "PASS", fmt.Sprintf("workload=%s namespace=%s deployment=%s", a.Workload, a.Namespace, a.Deployment))
	} else {
		addCheck("workload_identity", "FAIL", "incomplete workload identity")
	}

	// 9. Evidence present
	if len(a.ObservedContainerEvidence) > 0 {
		report.EvidencePresent = true
		addCheck("evidence_present", "PASS", fmt.Sprintf("%d container observations", len(a.ObservedContainerEvidence)))
	} else {
		addCheck("evidence_present", "FAIL", "no container evidence")
	}

	// 10. Verify expected digests against observed evidence
	if len(a.ExpectedDigests) > 0 {
		digestsOK := true
		for _, ed := range a.ExpectedDigests {
			found := false
			for _, oc := range a.ObservedContainerEvidence {
				if oc.ContainerName == ed.Container {
					found = true
					runtimeDigest := extractDigest(oc.RuntimeImageID)
					if strings.EqualFold(runtimeDigest, ed.Digest) {
						addCheck("digest_"+ed.Container, "PASS", fmt.Sprintf("expected %s matches observed runtime", ed.Digest))
					} else {
						digestsOK = false
						addCheck("digest_"+ed.Container, "FAIL", fmt.Sprintf("expected %s, observed %s", ed.Digest, runtimeDigest))
					}
					break
				}
			}
			if !found {
				digestsOK = false
				addCheck("digest_"+ed.Container, "FAIL", "container not found in observed evidence")
			}
		}
		report.DigestsVerified = digestsOK
	} else {
		report.DigestsVerified = true
		addCheck("expected_digests", "PASS", "no expected digests specified (runtime identity verified by declaration match)")
	}

	// 11. Timestamp
	if a.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339, a.CreatedAt); err == nil {
			report.TimestampPresent = true
			addCheck("timestamp", "PASS", a.CreatedAt)
		} else {
			addCheck("timestamp", "FAIL", fmt.Sprintf("invalid timestamp: %s", a.CreatedAt))
		}
	} else {
		addCheck("timestamp", "FAIL", "missing timestamp")
	}

	// 12. Observed container evidence verification results
	for _, oc := range a.ObservedContainerEvidence {
		if oc.VerificationResult != string(StatusMatch) {
			addCheck("container_result_"+oc.PodName+"_"+oc.ContainerName, "FAIL", fmt.Sprintf("container verification result is %s", oc.VerificationResult))
		}
	}

	report.Valid = allPassed && report.SignatureValid && report.ResultVerified && report.EvidencePresent && report.DigestsVerified
	return report, nil
}

// StatusMatch is duplicated here to avoid importing truth in the offline verifier path.
const StatusMatch = "MATCH"

// extractDigest extracts a digest from an image reference like "repo@sha256:abc123".
func extractDigest(imageRef string) string {
	if i := strings.LastIndex(imageRef, "@"); i >= 0 {
		return imageRef[i+1:]
	}
	return ""
}
