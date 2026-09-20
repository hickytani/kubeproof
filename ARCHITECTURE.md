# Architecture

## Scope

The project focuses on a narrow and defensible truth layer: compatibility between the declared Kubernetes workload identity and the currently observed runtime identity.

It does not attempt to infer application-level secret consumption or config usage from arbitrary process state because Kubernetes does not expose that reliably at the API level.

## Components

### CLI layer

The Cobra commands in `cmd/` expose `verify`, `explain`, `evidence`, and `scan`.

### Kubernetes access layer

The client package in `internal/k8s` wraps Kubernetes interfaces and performs read-only lookups for:

- Deployments
- ReplicaSets
- Pods

### Truth engine

The verifier in `internal/truth` compares:

- desired image(s) from the Deployment template
- observed imageIDs from Pod runtime container status
- rollout ownership used to locate Pods (Deployment -> ReplicaSet -> Pod), then Pod container runtime identity

It assigns statuses based on evidence and marks claims as UNKNOWN or UNOBSERVABLE where the API cannot prove more.

## Evidence-first model

The CLI emits the evidence chain rather than only a boolean. This makes it possible to explain where a mismatch came from without claiming to know deeper application behavior.

## Read-only behavior

No write operations are performed by the CLI. The project intentionally avoids:

- create
- update
- patch
- delete
- exec
- attach
- port-forward

Application memory, effective Secret or ConfigMap values, application consumption, and semantic behavior remain outside the Kubernetes API evidence boundary.
