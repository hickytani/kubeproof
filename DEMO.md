# KubeProof Demonstration & Launch Guide

KubeProof (`stateproof`) is a read-only Kubernetes runtime truth verifier and cryptographic attestation engine.

---

## Prerequisites

- Go 1.26+
- `kubectl`
- Access to a Kubernetes cluster (Kind, Minikube, or live cluster)
- Read-only RBAC access (`deploy/rbac-readonly.yaml`)

Build the CLI binary:

```powershell
go build -o stateproof.exe .
```

---

## 1. Verify Workloads (Deployments, StatefulSets, DaemonSets)

### Deployment Verification
```powershell
.\stateproof.exe verify workload deployment/payments -n production
.\stateproof.exe explain deployment payments -n production
```

### StatefulSet Verification
```powershell
.\stateproof.exe verify workload statefulset/database -n production
.\stateproof.exe explain statefulset database -n production
```

### DaemonSet Verification
```powershell
.\stateproof.exe verify workload daemonset/node-agent -n kube-system
```

---

## 2. Post-Deployment CI/CD Gate (`--expected-digest`)

Enforce exact image digests in CI/CD release pipelines without registry lookups:

```powershell
.\stateproof.exe verify workload deployment/payments -n production --expected-digest api=sha256:<64-hex-digest> --json
```

* **Exit Code `0`**: Verified MATCH (Rollout successful and image digests match).
* **Exit Code `2`**: Verified Drift or Incomplete Rollout.
* **Exit Code `3`**: Insufficient immutable identity (e.g. tag-only declaration).
* **Exit Code `1`**: Operational or authorization error.

---

## 3. Cryptographic Ed25519 Attestation Lifecycle

Turn a successful verification into a signed, portable evidence artifact:

```powershell
# Step 1: Generate key pair
.\stateproof.exe keygen --output production.key

# Step 2: Create signed attestation (requires MATCH status)
.\stateproof.exe attest workload deployment/payments -n production --signing-key production.key --output payments.attestation.json --expected-digest api=sha256:<digest>

# Step 3: Verify attestation completely offline (zero cluster or network access)
.\stateproof.exe verify-attestation payments.attestation.json --public-key production.key.pub
```

---

## 4. Offline Snapshot Comparison (`compare`)

Capture snapshots before and after maintenance, then diff offline:

```powershell
.\stateproof.exe evidence deployment payments -n production --json > before.json
# ... perform update / maintenance ...
.\stateproof.exe evidence deployment payments -n production --json > after.json
.\stateproof.exe compare before.json after.json --json
```

---

## 5. Disposable Real E2E Test Suite

Run the real Kind-backed E2E integration test suite:

```powershell
go test -tags=e2e -p 1 ./e2e -v
```
