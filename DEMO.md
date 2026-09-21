# Demonstration procedure

This is a demonstration procedure. It was not executed against a live Kubernetes cluster in the current development environment.

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

The result compares declared image references with the runtime `imageID` values reported in Pod container status.

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

- `MATCH` means the observed runtime image IDs matched the declared repository-level identity.
- `PARTIAL` means observations disagree or contain mixed runtime evidence.
- `UNKNOWN` means the API did not provide enough usable runtime identity evidence.

The result does not prove application memory, configuration consumption, Secret use, process behavior, or semantic application health.
