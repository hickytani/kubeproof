# Demonstration procedure

This is a demonstration procedure. It was not executed against a live Kubernetes cluster in the current development environment.

## Option A: existing kubeconfig-backed cluster

The procedure uses an existing kubeconfig-backed cluster and should inspect a safe test or demo workload. Do not mutate an existing production workload.

## 1. Prerequisites

Have the following available:

- Go
- `kubectl`
- a reachable Kubernetes API
- credentials with read access to Deployments, ReplicaSets, and Pods

Build StateProof from the repository root:

```powershell
go build -o stateproof.exe .
```

## 2. Configure kubeconfig

Use the normal kubeconfig and select a context without placing its contents in the repository:

```powershell
kubectl config get-contexts
kubectl config current-context
kubectl cluster-info
kubectl get nodes -o wide
```

StateProof can select a context explicitly:

```powershell
.\stateproof.exe --context <context> verify deployment <name> -n <namespace>
```

## 3. Select a namespace

Inspect namespaces and choose a safe namespace:

```powershell
kubectl get namespaces
kubectl get deployments -n <namespace>
```

## 4. Inspect a Deployment

Before running StateProof, inspect the source objects directly:

```powershell
kubectl get deployment <name> -n <namespace> -o yaml
kubectl get replicasets -n <namespace> -o wide
kubectl get pods -n <namespace> -o wide
kubectl get pods -n <namespace> -o json
```

## 5. Run verification

```powershell
.\stateproof.exe verify deployment <name> -n <namespace>
.\stateproof.exe explain deployment <name> -n <namespace>
```

For a definitive `MATCH`, use a digest-pinned image (`image@sha256:...`). The result compares each declared normal or init container by name with the matching runtime `imageID`. A tag-only declaration is `UNKNOWN` without registry evidence.

## 6. Inspect JSON evidence

```powershell
.\stateproof.exe evidence deployment <name> -n <namespace> --json
.\stateproof.exe scan <namespace>
```

Compare the `desired`, `observations`, `status`, and `evidence_chain` fields with the Kubernetes objects inspected in step 4.

## 7. Introduce a deliberate mismatch

Only do this for a temporary isolated workload created specifically for the demonstration. Do not change an existing production workload.

For an isolated Deployment, change its image and inspect the rollout while it is in progress:

```powershell
kubectl set image deployment/<demo-name> <container>=<different-image> -n <namespace>
kubectl get pods -n <namespace> -o wide
kubectl get replicasets -n <namespace> -o wide
.\stateproof.exe evidence deployment <demo-name> -n <namespace> --json
```

Do not claim a mismatch if the rollout has already converged or if the observed evidence is incomplete.

## 8. Verify again

Wait for the isolated rollout to finish, then compare the before and after evidence:

```powershell
kubectl rollout status deployment/<demo-name> -n <namespace> --timeout=180s
.\stateproof.exe verify deployment <demo-name> -n <namespace>
.\stateproof.exe evidence deployment <demo-name> -n <namespace> --json
```

## 9. Interpret the result

- `MATCH` means every current ready Pod matched each digest-pinned declaration.
- `PARTIAL` means digest divergence or an incomplete current revision was observed.
- `UNKNOWN` means immutable runtime evidence is insufficient, including tag-only declarations.

The result does not prove application memory, configuration consumption, Secret use, process behavior, or semantic application health.

## Option B: disposable real E2E suite

The repository contains a build-tagged real Kubernetes suite. It is intentionally excluded from ordinary Go tests and creates all fixtures in a temporary namespace:

```powershell
go test -tags=e2e -p 1 ./e2e -v
```

It requires `KUBECONFIG` to point to a cluster where the test identity can create and delete a namespace. The test captures each Pod's reported `imageID`, pins the paused Deployment to that observed digest, and invokes the production CLI. This demonstrates the actual `Deployment -> ReplicaSet -> Pod -> container -> imageID` path, plus tag-only insufficient evidence, digest drift, container-name swaps, and init-container evidence. The GitHub Actions `kubernetes-e2e` workflow provisions kind separately from ordinary CI.
