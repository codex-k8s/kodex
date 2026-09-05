package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

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
