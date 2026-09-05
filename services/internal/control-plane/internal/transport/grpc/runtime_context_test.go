package grpc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeContextProjectionPreservesPins(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	context := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: "org_fixture", ProjectRef: "prj_fixture", AgentRef: "agt_fixture",
		Skills: []runtimecontract.RuntimeSkillBundle{{BindingRef: "binding_skill", BindingVersion: 2, BundleRef: "skill_fixture", RevisionRef: "skill_revision", Revision: 3,
			Digest: "aggregate", ScanEngine: "scanner", ScanDigest: "scan", ScannedAt: now, Name: "Skill", Description: "Description",
			Files: []runtimecontract.RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_fixture", ArtifactRevision: 4, Digest: "sha256:file", SizeBytes: 8}}}},
		Memories: []runtimecontract.RuntimeMemoryRecord{{BindingRef: "binding_memory", BindingVersion: 5, RecordRef: "memory_fixture", RevisionRef: "memory_revision", Revision: 6,
			Digest: "memory", Summary: "Summary", Title: "Title", RetentionUntil: now.Add(time.Hour)}}}
	context.Digest, _ = context.ComputeDigest()
	values := map[string]any{"organizationRef": context.OrganizationRef, "projectRef": context.ProjectRef, "agentRef": context.AgentRef, "contextSnapshot": context}
	first := castRuntimeRevision(values)
	if first == nil || len(first.SkillBundles) != 1 || len(first.MemoryRecords) != 1 || first.SkillBundles[0].Files[0].Digest != "sha256:file" ||
		first.SkillBundles[0].BindingVersion != 2 || first.MemoryRecords[0].RevisionRef != "memory_revision" || first.MemoryRecords[0].Summary != "Summary" {
		t.Fatal("runtime context caster lost exact pins")
	}
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) != nil || !proto.Equal(first, castRuntimeRevision(decoded)) {
		t.Fatal("persisted runtime context changed wire projection")
	}
	values["projectRef"] = "prj_other"
	if castRuntimeRevision(values) != nil {
		t.Fatal("context from another project accepted")
	}
	values["projectRef"] = context.ProjectRef
	context.Memories[0].Summary = "Changed"
	values["contextSnapshot"] = context
	if castRuntimeRevision(values) != nil {
		t.Fatal("changed context digest accepted")
	}
}
