package truth

import "strings"

// imageReference separates the parts Kubernetes preserves in image fields. It
// deliberately does not resolve tags: that requires registry evidence.
type imageReference struct {
	Repository string
	Tag        string
	Digest     string
}

func parseImageReference(value string) imageReference {
	value = strings.TrimSpace(value)
	if scheme := strings.Index(value, "://"); scheme >= 0 {
		value = value[scheme+3:]
	}
	ref := imageReference{}
	if at := strings.Index(value, "@"); at >= 0 {
		ref.Digest = value[at+1:]
		value = value[:at]
	}
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon > lastSlash {
		ref.Tag = value[lastColon+1:]
		value = value[:lastColon]
	}
	ref.Repository = value
	return ref
}

// imageIdentityMatch only returns true for an immutable digest declaration.
// A tag may be associated with a runtime repository, but cannot be resolved to
// an immutable artifact without registry evidence.
func imageIdentityMatch(declared, runtime string) (bool, string) {
	want := parseImageReference(declared)
	got := parseImageReference(runtime)
	if want.Digest == "" {
		return false, "tag or unpinned image declaration cannot establish immutable runtime identity without registry evidence"
	}
	if want.Digest != got.Digest {
		return false, "runtime digest differs from digest-pinned declaration"
	}
	return true, ""
}
