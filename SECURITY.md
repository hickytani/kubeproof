# Security and trust boundary

## Read-only behavior

StateProof is an inspection tool. Its production Kubernetes client performs read operations only:

- `get`
- `list`
- `watch` permissions are supplied by the recommended RBAC manifest, although the current client path uses `get` and `list`

The CLI does not call Kubernetes create, update, patch, delete, replace, or restart operations. It does not use `pods/exec`, attach, or port-forward endpoints.

There is no automatic remediation. StateProof reports the evidence it can obtain and leaves workloads unchanged.

## Least privilege

[`deploy/rbac-readonly.yaml`](deploy/rbac-readonly.yaml) grants:

- core API `get`, `list`, and `watch` on `pods`
- Apps API `get`, `list`, and `watch` on `deployments`, `replicasets`, `daemonsets`, and `statefulsets`

The manifest uses a ServiceAccount, ClusterRole, and ClusterRoleBinding for an example read-only identity. It does not grant cluster-admin and does not grant access to Secrets or ConfigMaps.

The actual caller may instead use a normal user kubeconfig identity or in-cluster configuration. The required permissions remain read-only access to the resources used by the selected command.

## Secret handling

StateProof does not request Secret objects, read Secret `data`, or expose Secret values. It cannot determine whether an application consumed a Secret or ConfigMap value.

## Authentication and kubeconfig

The CLI uses the normal Kubernetes client configuration. Outside a cluster it loads the standard kubeconfig or the path supplied by `KUBECONFIG`; `--context` selects a named context. Inside a cluster it uses in-cluster configuration when the Kubernetes service environment is present.

Kubeconfig files and credentials belong to the operator environment and must not be committed to this repository.

## Evidence trust boundary

StateProof can connect declared Deployment images, ReplicaSet ownership, Pod ownership, reported container `imageID` values, replica counts, and selected container status fields. That is evidence about Kubernetes object state and runtime identity as reported by the API.

It does not prove:

- application memory or arbitrary process memory
- effective in-memory configuration
- Secret or ConfigMap consumption
- semantic application behavior
- rollout revision consistency
- that a container is healthy beyond the evidence exposed by the objects read

Those questions require other evidence or instrumentation and remain UNKNOWN or UNOBSERVABLE in this model.
