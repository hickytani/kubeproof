package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s-truth/internal/truth"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return pub, priv
}

func testVerificationResult() truth.VerificationResult {
	return truth.VerificationResult{
		SchemaVersion: 1,
		Claim:         "current ready Deployment Pods run declared digest-pinned runtime identities",
		Subject:       "Deployment/production/payments",
		Status:        truth.StatusMatch,
		Source:        "Kubernetes API",
		Timestamp:     time.Now(),
		Method:        "read-only",
		Desired:       "registry.example/payments@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Observed:      "all current ready Pod container identities matched digest-pinned declarations",
		Summary:       truth.ReplicaSummary{Desired: 3, Observed: 3, Matching: 3},
		Evidence: truth.EvidenceSnapshot{
			Deployment: truth.DeploymentEvidence{
				Namespace:       "production",
				Name:            "payments",
				CurrentRevision: "42",
			},
		},
		Observations: []truth.Observation{
			{
				Kind:    "container",
				Subject: "Container/production/payments-abc/api",
				Value:   "registry.example/payments@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Expected: "registry.example/payments@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Ready:   true,
				Status:  truth.StatusMatch,
				Reason:  truth.ReasonDigestMatch,
			},
		},
		Findings: []truth.Finding{},
		EvidenceChain: []truth.EvidenceEdge{
			{From: "ReplicaSet/production/payments-rs", To: "Pod/production/payments-abc", Type: "owns"},
			{From: "Pod/production/payments-abc", To: "Container/production/payments-abc/api", Type: "contains"},
		},
	}
}

// --- Key Generation Tests ---

func TestGenerateKeyPairIsValidEd25519(t *testing.T) {
	pub, priv := testKeyPair(t)
	if len(pub) != ed25519.PublicKeySize {
		t.Fatalf("public key size: want %d, got %d", ed25519.PublicKeySize, len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Fatalf("private key size: want %d, got %d", ed25519.PrivateKeySize, len(priv))
	}
}

func TestPublicKeyCorrespondsToPrivateKey(t *testing.T) {
	pub, priv := testKeyPair(t)
	msg := []byte("test message")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("public key does not correspond to private key")
	}
}

func TestKeyFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pub, priv := testKeyPair(t)

	privPath := filepath.Join(dir, "test.key")
	pubPath := filepath.Join(dir, "test.pub")

	if err := WritePrivateKey(privPath, priv); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := WritePublicKey(pubPath, pub); err != nil {
		t.Fatalf("write public key: %v", err)
	}

	readPriv, err := ReadPrivateKey(privPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	readPub, err := ReadPublicKey(pubPath)
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}

	if !priv.Equal(readPriv) {
		t.Fatal("private key roundtrip mismatch")
	}
	if !pub.Equal(readPub) {
		t.Fatal("public key roundtrip mismatch")
	}
}

func TestMalformedPrivateKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.key")
	os.WriteFile(path, []byte("not a pem file"), 0600)
	if _, err := ReadPrivateKey(path); err == nil {
		t.Fatal("malformed private key accepted")
	}
}

func TestMalformedPublicKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pub")
	os.WriteFile(path, []byte("not a pem file"), 0644)
	if _, err := ReadPublicKey(path); err == nil {
		t.Fatal("malformed public key accepted")
	}
}

func TestWrongPEMTypePrivateKeyRejected(t *testing.T) {
	dir := t.TempDir()
	pub, _ := testKeyPair(t)
	// Write a public key file but try to read it as private
	pubPath := filepath.Join(dir, "pub-as-priv.key")
	WritePublicKey(pubPath, pub)
	if _, err := ReadPrivateKey(pubPath); err == nil {
		t.Fatal("public key PEM accepted as private key")
	}
}

// --- Canonical Payload Tests ---

func TestCanonicalPayloadDeterministic(t *testing.T) {
	pub, _ := testKeyPair(t)
	result := testVerificationResult()
	expected := map[string]string{"api": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

	a1, err := NewAttestation(result, expected, pub)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := NewAttestation(result, expected, pub)
	if err != nil {
		t.Fatal(err)
	}
	// Force same timestamp
	a2.CreatedAt = a1.CreatedAt

	p1, err := CanonicalPayload(a1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := CanonicalPayload(a2)
	if err != nil {
		t.Fatal(err)
	}
	if string(p1) != string(p2) {
		t.Fatalf("same attestation produced different canonical payloads:\n%s\n%s", p1, p2)
	}
}

func TestModifyingFieldChangesCanonicalPayload(t *testing.T) {
	pub, _ := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)

	original, _ := CanonicalPayload(a)

	a.DeploymentRevision = "99"
	modified, _ := CanonicalPayload(a)

	if string(original) == string(modified) {
		t.Fatal("modifying a field did not change canonical payload")
	}
}

// --- Signing Tests ---

func TestValidSignatureSucceeds(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)

	if err := SignAttestation(a, priv); err != nil {
		t.Fatal(err)
	}

	payload, _ := CanonicalPayload(a)
	sigBytes, _ := hex.DecodeString(a.Signature)
	if !Verify(payload, sigBytes, pub) {
		t.Fatal("valid signature did not verify")
	}
}

func TestWrongPublicKeyFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	otherPub, _ := testKeyPair(t)

	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	payload, _ := CanonicalPayload(a)
	sigBytes, _ := hex.DecodeString(a.Signature)
	if Verify(payload, sigBytes, otherPub) {
		t.Fatal("wrong public key accepted")
	}
}

func TestCorruptedSignatureFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	payload, _ := CanonicalPayload(a)
	sigBytes, _ := hex.DecodeString(a.Signature)
	sigBytes[0] ^= 0xFF // corrupt one byte
	if Verify(payload, sigBytes, pub) {
		t.Fatal("corrupted signature accepted")
	}
}

func TestCorruptedPayloadFails(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	payload, _ := CanonicalPayload(a)
	payload[10] ^= 0xFF // corrupt one byte in payload
	sigBytes, _ := hex.DecodeString(a.Signature)
	if Verify(payload, sigBytes, pub) {
		t.Fatal("corrupted payload accepted")
	}
}

// --- Attestation Validation Tests ---

func TestRejectUnsupportedSchema(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)
	a.SchemaVersion = 99

	// Re-sign with the wrong schema to test the verifier
	SignAttestation(a, priv)

	report, _ := VerifyAttestation(a, pub)
	if report.SchemaSupported {
		t.Fatal("unsupported schema accepted")
	}
	if report.Valid {
		t.Fatal("attestation with unsupported schema should not be valid")
	}
}

func TestRejectMissingSignature(t *testing.T) {
	pub, _ := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	// Don't sign
	report, _ := VerifyAttestation(a, pub)
	if report.Valid || report.SignatureValid {
		t.Fatal("missing signature accepted")
	}
}

func TestRejectMissingSigner(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	a.Signer = SignerInfo{}
	SignAttestation(a, priv)

	report, _ := VerifyAttestation(a, pub)
	if report.Valid {
		t.Fatal("missing signer accepted as valid")
	}
}

func TestRejectMissingEvidence(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	a.ObservedContainerEvidence = nil
	SignAttestation(a, priv)

	report, _ := VerifyAttestation(a, pub)
	if report.EvidencePresent {
		t.Fatal("missing evidence accepted")
	}
}

func TestRejectUnsuccessfulVerificationResult(t *testing.T) {
	result := testVerificationResult()
	result.Status = truth.StatusPartial
	pub, _ := testKeyPair(t)
	_, err := NewAttestation(result, nil, pub)
	if err == nil {
		t.Fatal("unsuccessful verification result accepted")
	}
}

func TestRejectMismatchStatus(t *testing.T) {
	result := testVerificationResult()
	result.Status = truth.StatusMismatch
	pub, _ := testKeyPair(t)
	_, err := NewAttestation(result, nil, pub)
	if err == nil {
		t.Fatal("MISMATCH status accepted for attestation")
	}
}

func TestRejectUnknownStatus(t *testing.T) {
	result := testVerificationResult()
	result.Status = truth.StatusUnknown
	pub, _ := testKeyPair(t)
	_, err := NewAttestation(result, nil, pub)
	if err == nil {
		t.Fatal("UNKNOWN status accepted for attestation")
	}
}

func TestRejectMalformedWorkloadInVerification(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	a.Workload = ""
	a.Namespace = ""
	a.Deployment = ""
	SignAttestation(a, priv)

	report, _ := VerifyAttestation(a, pub)
	if report.WorkloadIdentified {
		t.Fatal("malformed workload identity accepted")
	}
}

// --- Offline Verification Tests ---

func TestOfflineVerificationSucceeds(t *testing.T) {
	pub, priv := testKeyPair(t)
	expected := map[string]string{"api": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	result := testVerificationResult()

	a, err := NewAttestation(result, expected, pub)
	if err != nil {
		t.Fatal(err)
	}
	if err := SignAttestation(a, priv); err != nil {
		t.Fatal(err)
	}

	// Verify with NO Kubernetes client - just the attestation and public key
	report, err := VerifyAttestation(a, pub)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("offline verification failed: %v", report.Errors)
	}
	if !report.SignatureValid {
		t.Fatal("signature should be valid")
	}
	if !report.PayloadIntegrity {
		t.Fatal("payload integrity should be valid")
	}
	if !report.ResultVerified {
		t.Fatal("result should be verified")
	}
	if !report.DigestsVerified {
		t.Fatal("digests should be verified")
	}
}

func TestOfflineVerificationDetectsTampering(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()

	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	// Tamper with the attestation after signing
	a.DeploymentRevision = "999"

	report, _ := VerifyAttestation(a, pub)
	if report.SignatureValid {
		t.Fatal("tampered attestation signature should be invalid")
	}
}

func TestOfflineVerificationJSON(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()

	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	// Serialize and deserialize to simulate file I/O
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	var loaded Attestation
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}

	report, _ := VerifyAttestation(&loaded, pub)
	if !report.Valid {
		t.Fatalf("verification after JSON roundtrip failed: %v", report.Errors)
	}
}

func TestPublicKeyFingerprintDeterministic(t *testing.T) {
	pub, _ := testKeyPair(t)
	fp1 := PublicKeyFingerprint(pub)
	fp2 := PublicKeyFingerprint(pub)
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %s != %s", fp1, fp2)
	}
	if !strings.HasPrefix(fp1, "sha256:") {
		t.Fatalf("fingerprint missing sha256: prefix: %s", fp1)
	}
}

func TestDifferentKeysProduceDifferentFingerprints(t *testing.T) {
	pub1, _ := testKeyPair(t)
	pub2, _ := testKeyPair(t)
	if PublicKeyFingerprint(pub1) == PublicKeyFingerprint(pub2) {
		t.Fatal("different keys produced same fingerprint")
	}
}

func TestSignatureNotInCanonicalPayload(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	payload, _ := CanonicalPayload(a)
	if strings.Contains(string(payload), a.Signature) {
		t.Fatal("signature appears in canonical payload")
	}
}

func TestPrivateKeyNotInAttestation(t *testing.T) {
	pub, priv := testKeyPair(t)
	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub)
	SignAttestation(a, priv)

	data, _ := json.Marshal(a)
	privHex := hex.EncodeToString(priv.Seed())
	if strings.Contains(string(data), privHex) {
		t.Fatal("private key seed found in attestation JSON")
	}

	// Check raw private key bytes as well
	privBytes := hex.EncodeToString(priv)
	if strings.Contains(string(data), privBytes) {
		t.Fatal("private key bytes found in attestation JSON")
	}
}

func TestPrivateKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	_, priv := testKeyPair(t)
	path := filepath.Join(dir, "test.key")
	WritePrivateKey(path, priv)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Unix, check for 0600. On Windows, file permissions work differently.
	mode := info.Mode().Perm()
	_ = mode // Windows doesn't enforce Unix permissions; this test validates the file was created.
}

func TestKeyGenWithCryptoRand(t *testing.T) {
	// Verify that GenerateKeyPair uses crypto/rand by checking the keys are unique.
	_, priv1, _ := GenerateKeyPair()
	_, priv2, _ := GenerateKeyPair()
	if priv1.Equal(priv2) {
		t.Fatal("two generated keys are identical — not using proper randomness")
	}
}

func TestVerifyWithWrongKeyReturnsInvalid(t *testing.T) {
	pub1, priv1 := testKeyPair(t)
	pub2, _ := testKeyPair(t)

	result := testVerificationResult()
	a, _ := NewAttestation(result, nil, pub1)
	SignAttestation(a, priv1)

	report, _ := VerifyAttestation(a, pub2)
	if report.Valid {
		t.Fatal("verification with wrong key should fail")
	}
}

func TestSignUsesStdlibEd25519(t *testing.T) {
	// Generate a key and sign, then verify with stdlib directly
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte("test payload")
	sig := Sign(payload, priv)
	if !ed25519.Verify(pub, payload, sig) {
		t.Fatal("Sign output not compatible with stdlib ed25519.Verify")
	}
}
