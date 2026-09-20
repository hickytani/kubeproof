# Security

## Read-only model

The CLI is read-only. It reads Deployments, ReplicaSets, Pods, and workload-type metadata through the Kubernetes API. It uses the current kubeconfig context by default and supports an explicit `--context`.

## Secret handling

This project does not request Secret objects or read Secret values. It does not expose secret bytes or data entries.

## Required RBAC

The recommended policy in `deploy/rbac-readonly.yaml` grants only `get`, `list`, and `watch` on Pods and the Apps API's Deployments, ReplicaSets, DaemonSets, and StatefulSets. It does not require cluster-admin.

## Prohibited actions

The project and deployment manifests intentionally avoid permissions for:

- create
- update
- patch
- delete
- pods/exec
- pods/attach
- replace
- restart
- port-forward

StateProof performs no automatic remediation. Kubernetes API evidence cannot prove application memory state, effective in-memory configuration, Secret consumption, arbitrary process memory, or semantic application behavior; those boundaries are reported as UNKNOWN or UNOBSERVABLE.
