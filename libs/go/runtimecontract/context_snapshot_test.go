package runtimecontract

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestContextSnapshotIsBoundToExecutionAndWarmCompatibility(t *testing.T) {
	input := validRunnerInputFixture()
	_, snapshot, _ := contextFixture()
	snapshot.OrganizationRef, snapshot.ProjectRef, snapshot.AgentRef = input.OrganizationRef, input.ProjectRef, input.AgentRef
	snapshot.Memories[0].RetentionUntil = time.Now().Add(time.Hour)
	snapshot.Digest, _ = snapshot.ComputeDigest()
	input.ContextSnapshot = &snapshot
	if input.Validate() == nil {
		t.Fatal("new context reused previous execution binding")
	}
	refreshRunnerInputBindings(&input)
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	warm := input
	warm.SystemAssistant = true
	warmBefore, err := WarmCompatibilityDigest(warm)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Memories = nil
	snapshot.Digest, _ = snapshot.ComputeDigest()
	warmAfter, err := WarmCompatibilityDigest(warm)
	if err != nil || warmAfter == warmBefore {
		t.Fatal("removed memory retained warm compatibility")
	}
	if input.Validate() == nil {
		t.Fatal("removed context reused execution grant")
	}
	refreshRunnerInputBindings(&input)
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	input.ContextSnapshot = nil
	if _, err := input.RequiredContextSnapshot(time.Now()); err == nil {
		t.Fatal("missing snapshot became an implicit empty context")
	}
}

func TestMemoryRetentionBoundsActiveExecution(t *testing.T) {
	_, snapshot, _ := contextFixture()
	snapshot.Memories[0].RetentionUntil = time.Now().Add(20 * time.Millisecond)
	ctx, cancel := snapshot.BoundExecutionContext(t.Context())
	defer cancel()
	select {
	case <-ctx.Done():
		if ctx.Err() != context.DeadlineExceeded {
			t.Fatal("wrong retention cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("active execution survived memory retention")
	}
}

func contextFixture() (RunnerInput, RuntimeContextSnapshot, time.Time) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	input := RunnerInput{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh"}
	provenance := RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("a", 64), CreatedAt: now}
	snapshot := RuntimeContextSnapshot{Schema: RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
		Skills: []RuntimeSkillBundle{{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 1,
			Digest: strings.Repeat("b", 64), Name: "fixture", Description: "Fixture skill", BindingRef: "ctxb_abcdefgh", BindingVersion: 1,
			Provenance: provenance, ScanEngine: "fixture", ScanDigest: strings.Repeat("c", 64), ScannedAt: now,
			Files: []RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 1, Digest: "sha256:" + strings.Repeat("d", 64), SizeBytes: 50}}}},
		Memories: []RuntimeMemoryRecord{{RecordRef: "memr_abcdefgh", RevisionRef: "memv_abcdefgh", Revision: 1,
			Digest: strings.Repeat("e", 64), Title: "Fixture", Summary: "Synthetic memory", BindingRef: "ctxb_ijklmnop", BindingVersion: 1, Provenance: provenance, RetentionUntil: now.Add(time.Hour)}},
	}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	return input, snapshot, now
}

func TestRuntimeContextSnapshotExactPins(t *testing.T) {
	input, snapshot, now := contextFixture()
	if err := snapshot.ValidateFor(input, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*RuntimeContextSnapshot){
		"foreign organization": func(s *RuntimeContextSnapshot) { s.OrganizationRef = "org_foreign1" },
		"foreign project":      func(s *RuntimeContextSnapshot) { s.ProjectRef = "proj_foreign1" },
		"foreign agent memory": func(s *RuntimeContextSnapshot) { s.AgentRef = "agt_foreign1" },
		"expired memory":       func(s *RuntimeContextSnapshot) { s.Memories[0].RetentionUntil = now },
		"missing provenance":   func(s *RuntimeContextSnapshot) { s.Skills[0].Provenance.Digest = "" },
		"missing binding":      func(s *RuntimeContextSnapshot) { s.Skills[0].BindingVersion = 0 },
		"latest":               func(s *RuntimeContextSnapshot) { s.Skills[0].RevisionRef = "latest" },
		"missing manifest":     func(s *RuntimeContextSnapshot) { s.Skills[0].Files[0].Path = "references/readme.md" },
		"traversal":            func(s *RuntimeContextSnapshot) { s.Skills[0].Files[0].Path = "../SKILL.md" },
		"absolute":             func(s *RuntimeContextSnapshot) { s.Skills[0].Files[0].Path = "/SKILL.md" },
		"duplicate":            func(s *RuntimeContextSnapshot) { s.Skills = append(s.Skills, s.Skills[0]) },
		"oversized file":       func(s *RuntimeContextSnapshot) { s.Skills[0].Files[0].SizeBytes = MaximumSkillFileBytes + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			input, current, _ := contextFixture()
			change(&current)
			current.Digest, _ = current.ComputeDigest()
			if current.ValidateFor(input, now) == nil {
				t.Fatal("invalid context accepted")
			}
		})
	}
	snapshot.Memories[0].Summary = "changed after pin"
	if snapshot.ValidateFor(input, now) == nil {
		t.Fatal("digest mismatch accepted")
	}
}

func TestContextDoesNotInferSkillsOrMemoryFromOtherResources(t *testing.T) {
	input, snapshot, now := contextFixture()
	snapshot.Skills, snapshot.Memories = nil, nil
	snapshot.Digest, _ = snapshot.ComputeDigest()
	input.EnvironmentTools = []RuntimeEnvironmentTool{{}}
	input.InputArtifacts = []RunnerInputArtifact{{Scope: "KNOWLEDGE"}}
	if snapshot.ValidateFor(input, now) != nil || len(snapshot.Skills) != 0 || len(snapshot.Memories) != 0 {
		t.Fatal("ordinary resources became context")
	}
}

func TestSkillPathAllowsApprovedSupportingFiles(t *testing.T) {
	for _, value := range []string{"SKILL.md", "README.md", "scripts/check.sh", "references/context.txt", "assets/icon.png", "agents/openai.yaml"} {
		if !ValidSkillPath(value) {
			t.Fatalf("approved supporting path rejected: %s", value)
		}
	}
	for _, value := range []string{"", ".", "..", "a/../b", "/tmp/file", "a//b", "a\\b", "a/.hidden", "a/ b", "skill.md", "file:stream"} {
		if ValidSkillPath(value) {
			t.Fatalf("unsafe supporting path accepted: %s", value)
		}
	}
}
