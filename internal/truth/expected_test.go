package truth

import "testing"

func TestExpectedDigestGate(t *testing.T) {
	d := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r := VerificationResult{Status: StatusMatch, Observations: []Observation{{Kind: "container", Subject: "Container/default/p/api", Value: "repo@" + d}}}
	e, _ := ParseExpectedDigests([]string{"api=" + d})
	ApplyExpectedDigests(&r, e)
	if r.Status != StatusMatch || r.Observations[0].Reason != ReasonExpectedDigestMatch {
		t.Fatal("expected match")
	}
	e, _ = ParseExpectedDigests([]string{"api=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	ApplyExpectedDigests(&r, e)
	if r.Status != StatusPartial {
		t.Fatal("expected mismatch")
	}
}
func TestExpectedDigestValidation(t *testing.T) {
	if _, e := ParseExpectedDigests([]string{"api=latest"}); e == nil {
		t.Fatal("tag accepted")
	}
}
