# StateProof

A read-only Kubernetes runtime truth verification engine that builds evidence chains from declared workload state to live runtime identity.

Kubernetes exposes desired state and runtime state through different objects, but operators often have to manually connect those objects to determine whether what was declared is actually what is running.

## What this tool checks

StateProof inspects Kubernetes objects and verifies whether the declared workload identity still matches the currently observed runtime image identity. It follows Deployment, ReplicaSet ownership, Pod, container status, and runtime image identity evidence from the Kubernetes API.

It does not claim to know whether application code is consuming a specific Secret or ConfigMap value. Application memory, effective in-memory values, process behavior, and application consumption remain UNKNOWN or UNOBSERVABLE.

## Status model

- MATCH
- MISMATCH
- STALE
- PARTIAL
- UNKNOWN
- UNOBSERVABLE
- ERROR

## Quickstart

Build:

```powershell
cd C:\Users\prasu\Downloads\cli-hunt\k8s-truth
C:\tools\go\bin\go.exe build -o stateproof.exe .
```

Display help:

```powershell
.\stateproof.exe --help
```

Verify a deployment:

```powershell
.\stateproof.exe verify deployment demo -n default
```

Explain a result:

```powershell
.\stateproof.exe explain deployment demo -n default
```

Emit structured evidence:

```powershell
.\stateproof.exe evidence deployment demo -n default --json
```

Scan a namespace:

```powershell
.\stateproof.exe scan default
```

## Real-cluster demo

Use an existing kubeconfig-backed Kubernetes cluster and choose a safe deployment to inspect. StateProof does not create infrastructure or mutate workloads.

```powershell
kubectl config current-context
kubectl get deployment -A
.\stateproof.exe verify deployment <name> -n <namespace>
```

## Security boundaries

This tool is read-only. It inspects:

- Deployment metadata
- ReplicaSet relationships
- Pod metadata and container status
- imageID values from Pod runtime status
- Secret and ConfigMap metadata only as references when needed

It does not read Secret data or expose secret bytes.

## Verified status in this environment

The repository was verified with:

```powershell
C:\tools\go\bin\go.exe test ./...
```

This passed in the current environment. Real-cluster E2E was blocked because no kubeconfig-backed Kubernetes API was available.
