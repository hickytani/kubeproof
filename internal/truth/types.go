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

type Observation struct {
	Kind          string    `json:"kind"`
	Subject       string    `json:"subject"`
	Value         string    `json:"value"`
	Expected      string    `json:"expected,omitempty"`
	ObservedImage string    `json:"observed_image,omitempty"`
	State         string    `json:"state,omitempty"`
	Ready         bool      `json:"ready,omitempty"`
	RestartCount  int32     `json:"restart_count,omitempty"`
	Source        string    `json:"source"`
	Timestamp     time.Time `json:"timestamp"`
	Method        string    `json:"method"`
	Status        Status    `json:"status"`
	Limitations   []string  `json:"limitations,omitempty"`
}

type Finding struct {
	Code     string        `json:"code"`
	Status   Status        `json:"status"`
	Subject  string        `json:"subject"`
	Message  string        `json:"message"`
	Evidence []Observation `json:"evidence,omitempty"`
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
	EvidenceChain []EvidenceEdge        `json:"evidence_chain,omitempty"`
}
