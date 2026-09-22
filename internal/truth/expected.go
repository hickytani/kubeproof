package truth

import (
	"fmt"
	"regexp"
	"strings"
)

var digestPattern = regexp.MustCompile(`^sha256:[a-fA-F0-9]{64}$`)

const ReasonExpectedDigestMatch ReasonCode = "EXPECTED_DIGEST_MATCH"
const ReasonExpectedDigestMismatch ReasonCode = "EXPECTED_DIGEST_MISMATCH"
const ReasonExpectedContainerMissing ReasonCode = "EXPECTED_CONTAINER_MISSING"

func ParseExpectedDigests(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range values {
		p := strings.SplitN(v, "=", 2)
		if len(p) != 2 || p[0] == "" || !digestPattern.MatchString(p[1]) {
			return nil, fmt.Errorf("invalid --expected-digest %q: use container=sha256:<64 hexadecimal characters>", v)
		}
		out[p[0]] = strings.ToLower(p[1])
	}
	return out, nil
}
func ApplyExpectedDigests(res *VerificationResult, expected map[string]string) {
	if len(expected) == 0 {
		return
	}
	failed, unknown := false, false
	seen := map[string]bool{}
	for i := range res.Observations {
		o := &res.Observations[i]
		if o.Kind != "container" {
			continue
		}
		name := o.Subject[strings.LastIndex(o.Subject, "/")+1:]
		want, ok := expected[name]
		if !ok {
			continue
		}
		seen[name] = true
		got := parseImageReference(o.Value).Digest
		if got == "" {
			unknown = true
			continue
		}
		if strings.EqualFold(got, want) {
			o.Reason = ReasonExpectedDigestMatch
		} else {
			failed = true
			o.Reason = ReasonExpectedDigestMismatch
			res.Findings = append(res.Findings, Finding{Code: "expected-digest-mismatch", Reason: ReasonExpectedDigestMismatch, Status: StatusMismatch, Subject: o.Subject, Message: "expected runtime digest differs", Evidence: []Observation{*o}})
		}
	}
	for name, want := range expected {
		if !seen[name] {
			failed = true
			res.Findings = append(res.Findings, Finding{Code: "expected-container-missing", Reason: ReasonExpectedContainerMissing, Status: StatusMismatch, Subject: res.Subject + "/" + name, Message: "expected container was not observed", Evidence: []Observation{{Kind: "container", Subject: name, Expected: want, Status: StatusUnknown}}})
		}
	}
	if failed {
		res.Status = StatusPartial
		res.Observed = "expected runtime digest was not observed on all current ready replicas"
	} else if unknown {
		res.Status = StatusUnknown
		res.Observed = "expected digest could not be established from runtime identity"
	}
}
