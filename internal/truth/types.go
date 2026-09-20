package truth

import (
	"time"
)

type Status string

const (
	StatusMatch       Status = "MATCH"
	StatusMismatch    Status = "MISMATCH"
	StatusStale       Status = "STALE"
	StatusPartial     Status = "PARTIAL"
	StatusUnknown     Status = "UNKNOWN"
	StatusUnobservable Status = "UNOBSERVABLE"
	StatusError       Status = "ERROR"
)

type Observation struct {
	Kind       string    `json:"kind"`
	Subject    string    `json:"subject"`
	Value      string    `json:"value"`
	Source     string    `json:"source"`
	Timestamp  time.Time `json:"timestamp"`
	Method     string    `json:"method"`
	Status     Status    `json:"status"`
	Limitations []string `json:"limitations,omitempty"`
}

type EvidenceNode struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type EvidenceEdge struct {
	From     string       `json:"from"`
	To       string       `json:"to"`
	Type     string       `json:"type"`
	Evidence []Observation `json:"evidence,omitempty"`
}

type VerificationResult struct {
	Claim         string         `json:"claim"`
	Subject       string         `json:"subject"`
	Desired       string         `json:"desired"`
	Observed      string         `json:"observed,omitempty"`
	Status        Status         `json:"status"`
	Source        string         `json:"source"`
	Timestamp     time.Time      `json:"timestamp"`
	Method        string         `json:"method"`
	Limitations   []string       `json:"limitations,omitempty"`
	Observations  []Observation  `json:"observations,omitempty"`
	EvidenceChain []EvidenceEdge `json:"evidence_chain,omitempty"`
}
