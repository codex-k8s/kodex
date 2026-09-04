package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestRuntimeExecutionFailureCodePreservesAuthorityBoundary(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "provider auth", err: codex.ErrProviderAuthentication, want: "PROVIDER_AUTH_REJECTED"},
		{name: "authority request", err: codex.ErrAuthorityRequestUnsupported, want: "RUNTIME_PROFILE_UNSUPPORTED"},
		{name: "required MCP", err: codex.ErrRequiredMCPUnavailable, want: "RUNTIME_MCP_UNAVAILABLE"},
		{name: "provider transport", err: errors.New("provider transport failed"), want: "RUNTIME_PROVIDER_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runtimeExecutionFailureCode(test.err); got != test.want {
				t.Fatalf("runtimeExecutionFailureCode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRuntimeMCPFailureCodeIsPreservedForCompletion(t *testing.T) {
	t.Parallel()

	code := runtimeExecutionFailureCode(codex.ErrRequiredMCPUnavailable)
	if got := safeFailureCode(code); got != "RUNTIME_MCP_UNAVAILABLE" {
		t.Fatalf("safeFailureCode(runtimeExecutionFailureCode()) = %q, want %q", got, "RUNTIME_MCP_UNAVAILABLE")
	}
}

type nativeToolRecorderStub struct {
	calls []runtimecontract.NativeToolCall
	err   error
}

func runnerInputArtifact(ref, fileName, mediaType, scope string, position, version int64, source string) runtimecontract.RunnerInputArtifact {
	artifact := runtimecontract.RunnerInputArtifact{
		Ref: ref, FileName: fileName, MediaType: mediaType,
		Digest: "sha256:" + strings.Repeat("a", 64), SizeBytes: 1,
		Revision: 1, Version: version, Scope: scope, Position: position, Source: source,
	}
	if scope == runtimecontract.AttachmentScopeKnowledge {
		artifact.AttachmentPurpose = "PROJECT_KNOWLEDGE"
		artifact.Provenance = "PROJECT_BINDING"
	}
	return artifact
}

func bindAttachmentCatalog(input model.Input) model.Input {
	for index := range input.InputArtifacts {
		artifact := &input.InputArtifacts[index]
		switch artifact.Scope {
		case runtimecontract.AttachmentScopeInput:
			artifact.AttachmentSetRef = input.AttachmentSetRef
			artifact.AttachmentPurpose = input.AttachmentContext
			artifact.Provenance = "CURRENT_TURN"
		case runtimecontract.AttachmentScopeSession:
			artifact.AttachmentSetRef = "aset_history1"
			artifact.AttachmentPurpose = "SESSION_TURN"
			artifact.Provenance = "SESSION_HISTORY"
		}
	}
	if input.AttachmentSetRef != "" {
		input.AttachmentSets = append(input.AttachmentSets, runtimecontract.RunnerAttachmentSet{
			Ref: input.AttachmentSetRef, ManifestDigest: input.AttachmentSetManifestDigest,
			Purpose: input.AttachmentContext, Scope: runtimecontract.AttachmentScopeInput, Provenance: "CURRENT_TURN",
		})
	}
	for _, artifact := range input.InputArtifacts {
		if artifact.Scope == runtimecontract.AttachmentScopeSession {
			input.AttachmentSets = append(input.AttachmentSets, runtimecontract.RunnerAttachmentSet{
				Ref: artifact.AttachmentSetRef, ManifestDigest: strings.Repeat("b", 64), Purpose: artifact.AttachmentPurpose,
				Scope: runtimecontract.AttachmentScopeSession, Provenance: artifact.Provenance,
			})
			break
		}
	}
	return input
}

func (stub *nativeToolRecorderStub) RecordNativeToolCall(_ context.Context, _ model.Input, call runtimecontract.NativeToolCall) error {
	stub.calls = append(stub.calls, call)
	return stub.err
}

func materializedInstructions(kind string, capabilities []string) string {
	blocks := map[string]string{
		"workflow-stage":       `<workflow-stage used="false">unused</workflow-stage>`,
		"automation":           `<automation used="false">unused</automation>`,
		"session-continuation": `<session-continuation used="false">unused</session-continuation>`,
	}
	if kind != "" {
		blocks[kind] = "<" + kind + ` used="true">configured</` + kind + ">"
	}
	sorted := append([]string(nil), capabilities...)
	sort.Strings(sorted)
	capabilityBlock := `<effective-capabilities used="false">unused</effective-capabilities>`
	if len(sorted) != 0 {
		capabilityBlock = `<effective-capabilities used="true">` + strings.Join(sorted, ",") + `</effective-capabilities>`
	}
	return "Server materialized prompt.\n" + blocks["workflow-stage"] + "\n" + blocks["automation"] + "\n" + blocks["session-continuation"] + "\n" + capabilityBlock
}

func TestBuildInitialPromptUsesExactServerTask(t *testing.T) {
	input := model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "  Analyze the attached customer brief.\n"}
	prompt, err := buildPrompt(input)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	if string(prompt) != input.Task {
		t.Fatalf("initial prompt was rebuilt: %q", prompt)
	}
}

func TestBuildContinuationPromptIncludesExactRevisionDeltaAndServiceBlocks(t *testing.T) {
	input := model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "Continue with the changes.", CodexSessionID: "00000000-0000-4000-8000-000000000001",
		Attempt: 2, RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionVersion: 3, RuntimeRevisionDigest: strings.Repeat("a", 64),
		Model: "gpt-5-codex", ImageReference: "registry.example/role@sha256:" + strings.Repeat("b", 64), ImageManifestDigest: "sha256:" + strings.Repeat("b", 64),
		RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigVersion: 4, RuntimeConfigDigest: strings.Repeat("c", 64),
		ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyVersion: 5, ProviderPolicyDigest: strings.Repeat("f", 64),
		ConfigOverlayRef: "cover_abcdefgh", ConfigOverlayVersion: 6,
		ConfigOverlayDigest: strings.Repeat("d", 64), ConfigOverlay: "model_reasoning_effort = \"high\"\n",
		RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentVersion: 7, RuntimeEnvironmentDigest: strings.Repeat("1", 64),
		EnvironmentBindingRef: "ebind_abcdefgh", EnvironmentBindingVersion: 8, EnvironmentBindingDigest: strings.Repeat("2", 64),
		MCPBindingDigest: strings.Repeat("e", 64), Capabilities: []string{"platform.artifact.manage"},
		EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{Name: "Shell", Command: "sh", Description: "Shell"}},
		Instructions:     materializedInstructions("session-continuation", []string{"platform.artifact.manage"}),
	}
	prompt, err := buildPrompt(input)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, expected := range []string{
		"<runtime-revision-delta>", "attempt=2", "model=gpt-5-codex", "reasoning=high",
		"tools=Shell:sh", "mcp=" + input.MCPBindingDigest, "files=::",
		"config=rconf_abcdefgh:4:", "environment=renv_abcdefgh:7:",
		`<session-continuation used="true">configured</session-continuation>`,
		`<effective-capabilities used="true">platform.artifact.manage</effective-capabilities>`,
	} {
		if !strings.Contains(string(prompt), expected) {
			t.Fatalf("continuation prompt does not contain %q: %s", expected, prompt)
		}
	}
}

func TestMaterializedPromptCoversAllRunnerLaunchKinds(t *testing.T) {
	tests := []struct {
		name, kind   string
		attempt      int32
		continued    bool
		resumeThread bool
	}{
		{name: "initial Agent", attempt: 1},
		{name: "Process stage", kind: "workflow-stage", attempt: 1},
		{name: "Automation", kind: "automation", attempt: 1},
		{name: "Continuation", kind: "session-continuation", attempt: 1, continued: true, resumeThread: true},
		{name: "Retry", kind: "session-continuation", attempt: 2, continued: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := model.Input{
				Mode: runtimecontract.RunnerModeTurn, Task: "Exact server task.", Attempt: test.attempt,
				Instructions: materializedInstructions(test.kind, []string{"project.read"}), Capabilities: []string{"project.read"},
				RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionVersion: int64(test.attempt), RuntimeRevisionDigest: strings.Repeat("a", 64),
				Model: "gpt-5.4", ImageReference: "registry.example/role@sha256:" + strings.Repeat("b", 64),
				ImageManifestDigest: "sha256:" + strings.Repeat("b", 64), ConfigOverlay: "model_reasoning_effort = \"high\"\n",
				MCPBindingDigest: strings.Repeat("c", 64), RuntimeConfigRef: "rconf_abcdefgh", RuntimeConfigDigest: strings.Repeat("d", 64),
				ProviderPolicyRef: "ppol_abcdefgh", ProviderPolicyDigest: strings.Repeat("e", 64), ConfigOverlayRef: "cover_abcdefgh",
				ConfigOverlayDigest: strings.Repeat("f", 64), RuntimeEnvironmentRef: "renv_abcdefgh", RuntimeEnvironmentDigest: strings.Repeat("1", 64),
				EnvironmentBindingRef: "ebind_abcdefgh", EnvironmentBindingDigest: strings.Repeat("2", 64),
			}
			if test.resumeThread {
				input.CodexSessionID = "00000000-0000-4000-8000-000000000001"
			}
			if err := validateMaterializedInstructions(input); err != nil {
				t.Fatalf("launch prompt rejected: %v", err)
			}
			prompt, err := buildPrompt(input)
			if err != nil {
				t.Fatalf("buildPrompt() error = %v", err)
			}
			hasDelta := strings.Contains(string(prompt), "<runtime-revision-delta>")
			if hasDelta != test.continued {
				t.Fatalf("continuation delta presence = %v, want %v: %s", hasDelta, test.continued, prompt)
			}
		})
	}
}

func TestValidateInstructionsAcceptsOnlyServerMaterializedPrompt(t *testing.T) {
	input := model.Input{Instructions: materializedInstructions("workflow-stage", []string{"project.read"}), Capabilities: []string{"project.read"}}
	if err := validateMaterializedInstructions(input); err != nil {
		t.Fatalf("validateMaterializedInstructions() error = %v", err)
	}
	for _, invalid := range []string{
		"{{.task}}", "plain text",
		strings.Replace(input.Instructions, `<automation used="false">unused</automation>`, `<automation used="true">configured</automation>`, 1),
		strings.Replace(input.Instructions, "project.read", "foreign.write", 1),
	} {
		if err := validateMaterializedInstructions(model.Input{Instructions: invalid, Capabilities: input.Capabilities}); err == nil {
			t.Fatalf("invalid materialized instructions were accepted: %q", invalid)
		}
	}
}

func TestWriteInputManifestsUsesCanonicalFullCatalog(t *testing.T) {
	root := t.TempDir()
	input := model.Input{
		WorkspaceRoot: "/workspace", AttachmentSetRef: "aset_abcdefgh", AttachmentContext: "RUN_INPUT",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			runnerInputArtifact("artifact_qrstuvwx", "policy.md", "text/markdown", runtimecontract.AttachmentScopeKnowledge, 1, 1, "KNOWLEDGE_SOURCE"),
			runnerInputArtifact("artifact_abcdefgh", "brief.txt", "text/plain", runtimecontract.AttachmentScopeInput, 1, 2, "CONTROL_CENTER"),
			runnerInputArtifact("artifact_ijklmnop", "prior.txt", "text/plain", runtimecontract.AttachmentScopeSession, 1, 1, "INTERACTION_ATTACHMENT"),
		},
	}
	input.AttachmentSetManifestDigest = strings.Repeat("a", 64)
	input = bindAttachmentCatalog(input)
	direct, err := runtimecontract.BuildAttachmentManifest(input.AttachmentSetRef, input.AttachmentContext,
		scopedArtifacts(input.InputArtifacts, runtimecontract.AttachmentScopeInput))
	if err != nil {
		t.Fatalf("BuildAttachmentManifest(direct) error = %v", err)
	}
	workspace, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		t.Fatalf("BuildAttachmentManifest(workspace) error = %v", err)
	}
	historyArtifacts := scopedArtifacts(input.InputArtifacts, runtimecontract.AttachmentScopeSession)
	for index := range historyArtifacts {
		historyArtifacts[index].Scope = runtimecontract.AttachmentScopeInput
		historyArtifacts[index].AttachmentSetRef = ""
	}
	history, err := runtimecontract.BuildAttachmentManifest("aset_history1", "SESSION_TURN", historyArtifacts)
	if err != nil {
		t.Fatalf("BuildAttachmentManifest(history) error = %v", err)
	}
	input.WorkspaceRoot = root
	if err := os.MkdirAll(filepath.Join(root, "knowledge"), 0o770); err != nil {
		t.Fatalf("MkdirAll(knowledge) error = %v", err)
	}
	for _, set := range input.AttachmentSets {
		if err := os.MkdirAll(filepath.Join(root, "input", set.Ref), 0o750); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
	}
	if err := writeInputManifests(input, map[string]runtimecontract.CanonicalAttachmentManifest{
		input.AttachmentSetRef: direct,
		"aset_history1":        history,
	}, workspace); err != nil {
		t.Fatalf("writeInputManifests() error = %v", err)
	}
	if err := protectReadOnlyWorkspaceTrees(root, "input", "knowledge"); err != nil {
		t.Fatalf("protectReadOnlyWorkspaceTrees() error = %v", err)
	}
	for path, expected := range map[string][]byte{
		filepath.Join(root, "input", input.AttachmentSetRef, "manifest.json"): direct.Bytes,
		filepath.Join(root, "input", "manifest.json"):                         workspace.Bytes,
	} {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("ReadFile(%s) error = %v", path, readErr)
		}
		if string(raw) != string(expected) {
			t.Fatalf("manifest %s differs from canonical bytes", path)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("Stat(%s) error = %v", path, statErr)
		}
		if info.Mode().Perm() != 0o440 {
			t.Fatalf("manifest %s mode = %v", path, info.Mode().Perm())
		}
	}
	for _, path := range []string{
		filepath.Join(root, "input"), filepath.Join(root, "input", input.AttachmentSetRef),
		filepath.Join(root, "input", "aset_history1"), filepath.Join(root, "knowledge"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", path, err)
		}
		if info.Mode().Perm() != 0o750 || info.Mode()&os.ModeSetgid == 0 {
			t.Fatalf("read-only directory mode for %s = %v", path, info.Mode())
		}
	}
}

func TestMaterializeInputArtifactsRejectsManifestMismatchBeforeWorkspaceMutation(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"input", "session", "knowledge"} {
		path := filepath.Join(root, directory)
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "sentinel"), []byte("preserve"), 0o640); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
	}
	input := model.Input{
		WorkspaceRoot: root, AttachmentSetRef: "aset_abcdefgh", AttachmentContext: "RUN_INPUT",
		AttachmentSetManifestDigest: strings.Repeat("f", 64),
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			runnerInputArtifact("artifact_abcdefgh", "brief.txt", "text/plain", runtimecontract.AttachmentScopeInput, 1, 1, "CONTROL_CENTER"),
		},
	}
	input = bindAttachmentCatalog(input)
	if err := materializeInputArtifacts(context.Background(), input, nil); err == nil || !strings.Contains(err.Error(), "manifest digest") {
		t.Fatalf("materializeInputArtifacts() error = %v", err)
	}
	for _, directory := range []string{"input", "session", "knowledge"} {
		if raw, err := os.ReadFile(filepath.Join(root, directory, "sentinel")); err != nil || string(raw) != "preserve" {
			t.Fatalf("workspace %s changed before digest validation: raw=%q err=%v", directory, raw, err)
		}
	}
}

func TestVerifyInputArtifactsRejectsSymlinkWithoutTargetLeakage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "input"), 0o770); err != nil {
		t.Fatal(err)
	}
	manifest, err := runtimecontract.BuildWorkspaceAttachmentManifest(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private-manifest.json")
	if err := os.WriteFile(target, manifest.Bytes, 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "input", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	err = verifyInputArtifacts(model.Input{WorkspaceRoot: root})
	if err == nil || strings.Contains(err.Error(), target) {
		t.Fatalf("verifyInputArtifacts() error = %v", err)
	}
}

func TestVerifyInputArtifactsRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "input"), 0o770); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "input", "manifest.json"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := verifyInputArtifacts(model.Input{WorkspaceRoot: root}); err == nil {
		t.Fatal("FIFO input was accepted")
	}
}

func TestMaterializedInstructionsRejectUnpromisedNames(t *testing.T) {
	for _, instructions := range []string{"{{ .agent.name }}", "{{ .project.name }}"} {
		if err := validateMaterializedInstructions(model.Input{Instructions: instructions}); err == nil {
			t.Fatalf("unmaterialized template variable %q was accepted", instructions)
		}
	}
}

func TestSystemAssistantCompletionDoesNotCreateProjectArtifact(t *testing.T) {
	artifacts, err := completionArtifacts(model.Input{SystemAssistant: true}, "Configuration plan proposed.")
	if err != nil {
		t.Fatalf("completionArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("system assistant artifacts = %#v", artifacts)
	}
}

func TestCompletionWithoutArtifactCapabilityDoesNotCreateProjectArtifact(t *testing.T) {
	artifacts, err := completionArtifacts(model.Input{}, "Bounded result.")
	if err != nil {
		t.Fatalf("completionArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("completion without artifact capability = %#v", artifacts)
	}
}

func TestCollectArtifactsRejectsSymlinkWithoutReadingTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".kodex/outbox"), 0o770); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(target, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".kodex/outbox/result.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := collectArtifacts(model.Input{WorkspaceRoot: root}, "done"); err == nil || strings.Contains(err.Error(), target) {
		t.Fatalf("collectArtifacts() error = %v", err)
	}
}

func TestReadinessCanaryReturnsOnlySafeSymlinkDenial(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{".kodex/outbox", "input", "knowledge"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	state := &health{input: model.Input{WorkspaceRoot: root, WorkspacePolicy: runtimecontract.RuntimeWorkspacePolicyV1()}}
	state.live.Store(true)
	state.ready.Store(true)
	response := httptest.NewRecorder()
	healthHandler(state).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("healthy readiness status = %d body=%q", response.Code, response.Body.String())
	}
	if err := os.Remove(filepath.Join(root, ".kodex/outbox")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".kodex/outbox")); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	healthHandler(state).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable ||
		strings.TrimSpace(response.Body.String()) != "workspace readiness denied: PATH_OUTSIDE_WORKSPACE" ||
		strings.Contains(response.Body.String(), root) {
		t.Fatalf("unsafe readiness response: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestRecordNativeToolTimelinePreservesOrderAndStopsOnCallbackFailure(t *testing.T) {
	calls := []runtimecontract.NativeToolCall{
		{CallID: "call-1", Kind: runtimecontract.NativeToolKindWebSearch, State: runtimecontract.NativeToolStateSucceeded,
			SafeResult: runtimecontract.NativeToolResultCompleted, SafeParameters: map[string]any{"action": "SEARCH", "query_count": 1}},
		{CallID: "call-2", Kind: runtimecontract.NativeToolKindSleep, State: runtimecontract.NativeToolStateSucceeded,
			DurationMS: 25, SafeResult: runtimecontract.NativeToolResultCompleted, SafeParameters: map[string]any{"requested_duration_ms": int64(25)}},
	}
	recorder := &nativeToolRecorderStub{}
	if err := recordNativeToolTimeline(context.Background(), model.Input{}, recorder, calls); err != nil {
		t.Fatalf("recordNativeToolTimeline() error = %v", err)
	}
	if len(recorder.calls) != 2 || recorder.calls[0].CallID != "call-1" || recorder.calls[1].CallID != "call-2" {
		t.Fatalf("recorded calls = %#v", recorder.calls)
	}
	recorder = &nativeToolRecorderStub{err: errors.New("unavailable")}
	if err := recordNativeToolTimeline(context.Background(), model.Input{}, recorder, calls); err == nil || len(recorder.calls) != 1 {
		t.Fatalf("callback failure was not propagated: calls=%#v err=%v", recorder.calls, err)
	}
}
