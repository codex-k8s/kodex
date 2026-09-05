package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestRuntimeContextSessionID(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, test := range []struct {
		name, previous, current, expected string
	}{
		{"unchanged", digest, digest, "session"},
		{"changed", digest, strings.Repeat("b", 64), ""},
		{"legacy", "", digest, ""},
		{"missing_current", digest, "", ""},
		{"missing_both", "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := runtimeContextSessionID("session", test.previous, test.current); got != test.expected {
				t.Fatalf("session continuation = %q, want %q", got, test.expected)
			}
		})
	}
}

func TestRuntimeRevisionIncludesExplicitContext(t *testing.T) {
	values := map[string]any{"runtimeRevisionRef": "revision", "runtimeRevisionVersion": int64(1),
		"providerSecretName": "secret", "providerSecretUID": "uid", "providerSecretResourceVersion": "1"}
	before, err := runtimeRevisionDigestFromSnapshot(values)
	if err != nil {
		t.Fatal(err)
	}
	context := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: "org_fixture", AgentRef: "agt_fixture",
		Skills: []runtimecontract.RuntimeSkillBundle{}, Memories: []runtimecontract.RuntimeMemoryRecord{}}
	context.Digest, _ = context.ComputeDigest()
	values["contextSnapshot"] = context
	after, err := runtimeRevisionDigestFromSnapshot(values)
	if err != nil || before == after {
		t.Fatalf("empty explicit context omitted from RuntimeRevision digest: %v", err)
	}
	context.AgentRef = "agt_other"
	context.Digest, _ = context.ComputeDigest()
	values["contextSnapshot"] = context
	changed, err := runtimeRevisionDigestFromSnapshot(values)
	if err != nil || changed == after {
		t.Fatalf("context lineage omitted from RuntimeRevision digest: %v", err)
	}
}
