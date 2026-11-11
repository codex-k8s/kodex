package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestProviderContextPreservesPromptAndExplicitEmptyMemoryOnResume checks resumed input keeps the exact prompt and explicit empty records, then rejects a changed snapshot digest rather than reusing prior memory.
// TestProviderContextPreservesPromptAndExplicitEmptyMemoryOnResume проверяет точный prompt и явные empty records resumed input, затем отклоняет изменённый snapshot digest вместо повторного использования прежней памяти.
func TestProviderContextPreservesPromptAndExplicitEmptyMemoryOnResume(t *testing.T) {
	input := model.Input{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh", NativeSession: &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_previous", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_previous.json", SHA256: strings.Repeat("a", 64), SizeBytes: 800}, RuntimeRevisionRef: "rrev_abcdefgh"}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	items, err := contextInputItems(input, snapshot, []byte("server-owned prompt"), time.Now())
	if err != nil || len(items) != 2 || items[0]["text"] != "server-owned prompt" || !strings.Contains(items[1]["text"].(string), `"records":[]`) {
		t.Fatal("context projection changed prompt or reused old memory")
	}
	snapshot.Digest = strings.Repeat("f", 64)
	if _, err := contextInputItems(input, snapshot, nil, time.Now()); err == nil {
		t.Fatal("corrupt snapshot reached provider mapping")
	}
}

// TestProviderContextUsesNativeSkillAndExactMemoryPins verifies initial/resumed mappings preserve native skill path/name and exact memory record, context digest and current revision pins without promoting memory to instructions.
// TestProviderContextUsesNativeSkillAndExactMemoryPins проверяет сохранение native skill path/name и точных memory record, context digest и current revision pins в initial/resumed mappings без превращения memory в instructions.
func TestProviderContextUsesNativeSkillAndExactMemoryPins(t *testing.T) {
	now := time.Now().UTC()
	input := model.Input{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh",
		RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionDigest: strings.Repeat("a", 64)}
	provenance := runtimecontract.RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("b", 64), CreatedAt: now}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema,
		OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
		Skills: []runtimecontract.RuntimeSkillBundle{{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 3,
			Digest: strings.Repeat("c", 64), Name: "fixture", Description: "Approved fixture", BindingRef: "ctxb_abcdefgh", BindingVersion: 2,
			Provenance: provenance, ScanEngine: "fixture", ScanDigest: strings.Repeat("d", 64), ScannedAt: now,
			Files: []runtimecontract.RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 4, Digest: "sha256:" + strings.Repeat("e", 64), SizeBytes: 32}}}},
		Memories: []runtimecontract.RuntimeMemoryRecord{{RecordRef: "memr_abcdefgh", RevisionRef: "memv_abcdefgh", Revision: 7,
			Digest: strings.Repeat("f", 64), Title: "Fixture", Summary: "Bounded memory", BindingRef: "ctxb_ijklmnop", BindingVersion: 5,
			Provenance: provenance, RetentionUntil: now.Add(time.Hour)}},
	}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	for _, resumed := range []bool{false, true} {
		if resumed {
			input.NativeSession = &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_previous", Version: runtimecontract.OpenCodeVersion, Generation: 1, RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_previous.json", SHA256: strings.Repeat("a", 64), SizeBytes: 800}
			input.RuntimeRevisionRef = "rrev_ijklmnop"
			input.RuntimeRevisionDigest = strings.Repeat("9", 64)
		}
		items, err := contextInputItems(input, snapshot, []byte("server prompt"), now)
		if err != nil || len(items) != 3 {
			t.Fatalf("valid context was not projected: %v", err)
		}
		if items[0]["text"] != "server prompt" || items[1]["type"] != "skill" || items[1]["name"] != "fixture" ||
			items[1]["path"] != "/workspace/context/skills/sklb_abcdefgh/SKILL.md" {
			t.Fatal("native skill input does not match pinned bundle")
		}
		_, raw, found := strings.Cut(items[2]["text"].(string), "\n")
		var projection struct {
			RuntimeRevisionRef    string `json:"runtime_revision_ref"`
			RuntimeRevisionDigest string `json:"runtime_revision_digest"`
			ContextDigest         string `json:"context_digest"`
			Records               []struct {
				Record runtimecontract.RuntimeMemoryRecord `json:"record"`
				Path   string                              `json:"path"`
			} `json:"records"`
		}
		if !found || json.Unmarshal([]byte(raw), &projection) != nil || len(projection.Records) != 1 ||
			projection.RuntimeRevisionRef != input.RuntimeRevisionRef || projection.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
			projection.ContextDigest != snapshot.Digest || projection.Records[0].Record != snapshot.Memories[0] ||
			projection.Records[0].Path != "/workspace/context/memory/memr_abcdefgh.md" {
			t.Fatal("memory projection lost immutable pins or used previous revision")
		}
	}
}
