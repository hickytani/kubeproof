package truth

import (
	"time"
)

type Status string

const (
	StatusMatch        Status = "MATCH"
	StatusMismatch     Status = "MISMATCH"
	StatusStale        Status = "STALE"
	StatusPartial      Status = "PARTIAL"
	StatusUnknown      Status = "UNKNOWN"
	StatusUnobservable Status = "UNOBSERVABLE"
	StatusError        Status = "ERROR"
)

// ReasonCode is stable machine-readable evidence. Messages may evolve; codes are
// the contract for automation.
type ReasonCode string

const (
	ReasonDigestMatch                  ReasonCode = "DIGEST_MATCH"
	ReasonDigestMismatch               ReasonCode = "DIGEST_MISMATCH"
	ReasonImmutableIdentityUnavailable ReasonCode = "IMMUTABLE_IDENTITY_UNAVAILABLE"
	ReasonCurrentRevisionIncomplete    ReasonCode = "CURRENT_REVISION_INCOMPLETE"
	ReasonContainerMissing             ReasonCode = "CONTAINER_MISSING"
	ReasonUnexpectedContainer          ReasonCode = "UNEXPECTED_CONTAINER"
	ReasonInitContainerMissing         ReasonCode = "INIT_CONTAINER_MISSING"
	ReasonUnexpectedInitContainer      ReasonCode = "UNEXPECTED_INIT_CONTAINER"
	ReasonPodNotReady                  ReasonCode = "POD_NOT_READY"
	ReasonOldRevisionIgnored           ReasonCode = "OLD_REVISION_IGNORED"
)

type Observation struct {
	Kind          string     `json:"kind"`
	Subject       string     `json:"subject"`
	Value         string     `json:"value"`
	Expected      string     `json:"expected,omitempty"`
	ObservedImage string     `json:"observed_image,omitempty"`
	State         string     `json:"state,omitempty"`
	Ready         bool       `json:"ready,omitempty"`
	RestartCount  int32      `json:"restart_count,omitempty"`
	Source        string     `json:"source"`
	Timestamp     time.Time  `json:"timestamp"`
	Method        string     `json:"method"`
	Status        Status     `json:"status"`
	Reason        ReasonCode `json:"reason,omitempty"`
	Limitations   []string   `json:"limitations,omitempty"`
}

type Finding struct {
	Code     string        `json:"code"`
	Reason   ReasonCode    `json:"reason"`
	Status   Status        `json:"status"`
	Subject  string        `json:"subject"`
	Message  string        `json:"message"`
	Evidence []Observation `json:"evidence,omitempty"`
}

// EvidenceSnapshot is a deliberately small normalized view of API objects; it
// avoids raw Kubernetes object dumps while retaining audit-relevant fields.
type EvidenceSnapshot struct {
	Deployment          DeploymentEvidence           `json:"deployment,omitempty"`
	StatefulSet         StatefulSetEvidence          `json:"statefulset,omitempty"`
	DaemonSet           DaemonSetEvidence            `json:"daemonset,omitempty"`
	ReplicaSets         []ReplicaSetEvidence         `json:"replica_sets,omitempty"`
	ControllerRevisions []ControllerRevisionEvidence `json:"controller_revisions,omitempty"`
	Pods                []PodEvidence                `json:"pods,omitempty"`
}
type DeploymentEvidence struct {
	Namespace, Name, CurrentRevision                                   string
	Generation, ObservedGeneration                                     int64
	DesiredReplicas, AvailableReplicas, UpdatedReplicas, ReadyReplicas int32
}
type StatefulSetEvidence struct {
	Namespace, Name, CurrentRevision, UpdateRevision string
	Generation, ObservedGeneration                 int64
	DesiredReplicas, ReadyReplicas, UpdatedReplicas int32
}
type DaemonSetEvidence struct {
	Namespace, Name, CurrentRevision               string
	Generation, ObservedGeneration                 int64
	DesiredNumberScheduled, CurrentNumberScheduled int32
	NumberReady, UpdatedNumberScheduled            int32
}
type ReplicaSetEvidence struct {
	Namespace, Name, Revision, OwnerDeployment string
	DesiredReplicas                            int32
	Current                                    bool
}
type ControllerRevisionEvidence struct {
	Namespace, Name, OwnerWorkload string
	Revision                       int64
	Current                        bool
}
type PodEvidence struct {
	Namespace, Name, UID, Phase, OwnerReplicaSet, OwnerControllerRevision, Revision string
	Ready, Terminating                                                               bool
}

type ReplicaSummary struct {
	Desired   int `json:"desired"`
	Observed  int `json:"observed"`
	Matching  int `json:"matching"`
	Divergent int `json:"divergent"`
	Unknown   int `json:"unknown"`
}

type ConfigurationReference struct {
	Pod           string `json:"pod,omitempty"`
	Container     string `json:"container,omitempty"`
	Location      string `json:"location"`
	ReferenceType string `json:"reference_type"`
	Kind          string `json:"kind"`
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
}

type ConfigurationEvidence struct {
	Declared    []ConfigurationReference `json:"declared,omitempty"`
	Observed    []ConfigurationReference `json:"observed,omitempty"`
	Limitations []string                 `json:"limitations,omitempty"`
}

type EvidenceNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type EvidenceEdge struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Type     string        `json:"type"`
	Evidence []Observation `json:"evidence,omitempty"`
}

type VerificationResult struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Claim         string                `json:"claim"`
	Subject       string                `json:"subject"`
	Desired       string                `json:"desired"`
	Observed      string                `json:"observed,omitempty"`
	Status        Status                `json:"status"`
	Source        string                `json:"source"`
	Timestamp     time.Time             `json:"timestamp"`
	Method        string                `json:"method"`
	Limitations   []string              `json:"limitations,omitempty"`
	Observations  []Observation         `json:"observations,omitempty"`
	Findings      []Finding             `json:"findings,omitempty"`
	Summary       ReplicaSummary        `json:"summary"`
	Configuration ConfigurationEvidence `json:"configuration"`
	Evidence      EvidenceSnapshot      `json:"evidence"`
	EvidenceChain []EvidenceEdge        `json:"evidence_chain,omitempty"`
}
