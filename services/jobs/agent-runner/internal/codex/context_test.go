package codex

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestProviderContextPreservesPromptAndExplicitEmptyMemoryOnResume(t *testing.T) {
	input := model.Input{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh", CodexSessionID: "previous-thread", RuntimeRevisionRef: "rrev_abcdefgh"}
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

func TestMissingContextCannotStartProviderProcess(t *testing.T) {
	input := model.Input{Provider: "openai", Model: "gpt-6-astra", ReasoningMode: runtimecontract.ReasoningSupported, EffectiveReasoningEffort: "medium"}
	if _, err := executeLocal(t.Context(), input, []byte("task"), ""); err != runtimecontract.ErrRuntimeContext {
		t.Fatalf("missing context reached credential/process: %v", err)
	}
}

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
			input.CodexSessionID = "previous-thread"
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

func TestContextDiscoveryProcessFixture(t *testing.T) {
	mode := os.Getenv("KODEX_SKILL_DISCOVERY_FIXTURE")
	if mode == "" {
		return
	}
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	enabled := true
	for {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := decoder.Decode(&request); err == io.EOF {
			return
		} else if err != nil {
			t.Fatal(err)
		}
		var result any
		switch request.Method {
		case "skills/extraRoots/set":
			var params struct {
				ExtraRoots []string `json:"extraRoots"`
			}
			if json.Unmarshal(request.Params, &params) != nil || len(params.ExtraRoots) != 1 || params.ExtraRoots[0] != runtimecontract.RuntimeContextRoot+"/skills" {
				t.Fatal("unexpected skill root")
			}
			result = map[string]any{}
		case "skills/list":
			var params struct {
				CWDs        []string `json:"cwds"`
				ForceReload bool     `json:"forceReload"`
			}
			if json.Unmarshal(request.Params, &params) != nil || len(params.CWDs) != 1 || params.CWDs[0] != "/workspace" || !params.ForceReload {
				t.Fatal("unexpected discovery request")
			}
			skills := []discoveredSkill{{Name: "local-unbound", Description: "Not authorized", Path: "/etc/codex/skills/local/SKILL.md", Enabled: enabled, Scope: "admin"}}
			if mode != "missing" {
				skills = append(skills, discoveredSkill{Name: "fixture", Description: "Approved fixture", Path: runtimecontract.RuntimeContextRoot + "/skills/sklb_abcdefgh/SKILL.md", Enabled: true, Scope: "user"})
			}
			if mode == "wrong metadata" {
				skills[len(skills)-1].Description = "Changed"
			}
			result = map[string]any{"data": []any{map[string]any{"cwd": "/workspace", "skills": skills, "errors": []any{}}}}
		case "skills/config/write":
			var params struct {
				Path    string `json:"path"`
				Enabled bool   `json:"enabled"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.Path != "/etc/codex/skills/local/SKILL.md" || params.Enabled {
				t.Fatal("unexpected skill state mutation")
			}
			if mode != "remains enabled" {
				enabled = false
			}
			result = map[string]any{"effectiveEnabled": enabled}
		default:
			t.Fatal("unexpected provider operation")
		}
		if err := encoder.Encode(map[string]any{"id": request.ID, "result": result}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProviderDiscoveryReconcilesExactSkillsInRealProcess(t *testing.T) {
	for _, mode := range []string{"success", "missing", "wrong metadata", "remains enabled"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestContextDiscoveryProcessFixture$")
			command.Env = []string{"KODEX_SKILL_DISCOVERY_FIXTURE=" + mode}
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			messages := make(chan streamEvent, 64)
			readDone := make(chan struct{})
			go func() { defer close(readDone); readAppServerMessages(stdout, messages) }()
			server := &appServer{stdin: stdin, messages: messages}
			input := model.Input{WorkspaceRoot: "/workspace"}
			snapshot := runtimecontract.RuntimeContextSnapshot{Skills: []runtimecontract.RuntimeSkillBundle{{BundleRef: "sklb_abcdefgh", Name: "fixture", Description: "Approved fixture"}}}
			err = server.configureContextSkills(ctx, newProtocolState(""), input, snapshot)
			_ = stdin.Close()
			waitErr := command.Wait()
			<-readDone
			if waitErr != nil {
				t.Fatal("provider fixture failed")
			}
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected discovery outcome: %v", err)
			}
		})
	}
}
