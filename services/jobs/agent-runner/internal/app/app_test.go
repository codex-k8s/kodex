package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestBuildPromptExplainsBoundedFileAccessWithArtifactCapability(t *testing.T) {
	input := model.Input{
		Mode:                        runtimecontract.RunnerModeTurn,
		Task:                        "Analyze the attached customer brief.",
		Capabilities:                []string{runtimecontract.ArtifactCapability},
		AttachmentSetRef:            "aset_abcdefgh",
		AttachmentSetManifestDigest: strings.Repeat("a", 64),
		AttachmentContext:           "RUN_INPUT",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			{Ref: "artifact_abcdefgh", FileName: "customer brief.txt", MediaType: "text/plain", Scope: "INPUT", Position: 1, Source: "CONTROL_CENTER"},
			{Ref: "artifact_ijklmnop", FileName: "terms.pdf", MediaType: "application/pdf", Scope: "INPUT", Position: 2, Source: "CONTROL_CENTER"},
		},
	}
	input = bindAttachmentCatalog(input)
	prompt, err := buildPrompt(input)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	text := string(prompt)
	for _, expected := range []string{
		"# File access",
		"The user attached 2 read-only file(s) to this turn.",
		"`/workspace/input/aset_abcdefgh/files/0001-customer_brief.txt`",
		"`/workspace/input/aset_abcdefgh/files/0002-terms.pdf`",
		"Write every output file directly to `/workspace/.kodex/outbox/<safe-name>`.",
		"The workspace root and all other paths are read-only",
		"do not write output files to `/workspace` itself.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("prompt does not contain %q: %s", expected, text)
		}
	}
	if strings.Contains(text, input.InputArtifacts[0].Ref) {
		t.Fatal("prompt exposed an opaque artifact ref instead of the materialized path")
	}
}

func TestBuildPromptDoesNotPromiseFileAccessWithoutArtifactCapability(t *testing.T) {
	prompt, err := buildPrompt(model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "Summarize the request."})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	text := string(prompt)
	for _, forbidden := range []string{"# File access", "/workspace/.kodex/outbox", "output file", "read-only"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("prompt without artifact capability contains %q: %s", forbidden, text)
		}
	}
}

func TestRenderInstructionsExposesTypedFileScopes(t *testing.T) {
	input := model.Input{
		Instructions: `{{range .input.files}}{{.name}}|{{.media_type}}|{{.path}}|{{.source}}|{{.purpose}}|{{.version}}{{end}}
{{range .session.files}}{{.name}};{{end}}
{{range .project.files}}{{.name}};{{end}}
{{.input.files_count}}|{{.input.files_dir}}|{{.input.manifest_path}}`,
		ProjectRef: "prj_abcdefgh", SessionRef: "ses_abcdefgh", RunRef: "run_abcdefgh",
		AttachmentSetRef: "aset_abcdefgh", AttachmentContext: "SESSION_TURN",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			runnerInputArtifact("artifact_abcdefgh", "new.txt", "text/plain", runtimecontract.AttachmentScopeInput, 1, 2, "CONTROL_CENTER"),
			runnerInputArtifact("artifact_ijklmnop", "prior.txt", "text/plain", runtimecontract.AttachmentScopeSession, 1, 1, "CONTROL_CENTER"),
			runnerInputArtifact("artifact_qrstuvwx", "policy.md", "text/markdown", runtimecontract.AttachmentScopeKnowledge, 1, 3, "KNOWLEDGE_SOURCE"),
		},
	}
	input.AttachmentSetManifestDigest = strings.Repeat("a", 64)
	input = bindAttachmentCatalog(input)
	rendered, err := renderInstructions(input)
	if err != nil {
		t.Fatalf("renderInstructions() error = %v", err)
	}
	for _, expected := range []string{
		"new.txt|text/plain|/workspace/input/aset_abcdefgh/files/0001-new.txt|CONTROL_CENTER|SESSION_TURN|2",
		"prior.txt;new.txt;",
		"policy.md;",
		"1|/workspace/input/aset_abcdefgh/files|/workspace/input/aset_abcdefgh/manifest.json",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered instructions do not contain %q: %s", expected, rendered)
		}
	}
}

func TestRenderInstructionsExposesOnlyEnvironmentSelectedTools(t *testing.T) {
	input := model.Input{
		Instructions: `{{range .runtime.environment.tools}}{{.command}}|{{.description}}|{{.usage_hint}}{{end}}
{{.runtime.environment.image.reference}}|{{.runtime.environment.image.digest}}`,
		EnvironmentImage: runtimecontract.RuntimeEnvironmentImage{
			Reference: "registry.example/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Digest:    "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		EnvironmentTools: []runtimecontract.RuntimeEnvironmentTool{{
			Name: "GitHub CLI", Command: "gh", Description: "Работа с GitHub", UsageHint: "Используй gh api",
		}},
	}
	rendered, err := renderInstructions(input)
	if err != nil {
		t.Fatalf("renderInstructions() error = %v", err)
	}
	for _, expected := range []string{
		"gh|Работа с GitHub|Используй gh api", "# Verified runtime tools", "`gh`",
		input.EnvironmentImage.Reference + "|" + input.EnvironmentImage.Digest,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered instructions do not contain %q: %s", expected, rendered)
		}
	}
}

func TestRenderInstructionsMaterializesRunWorkflowAndProjectFileScopes(t *testing.T) {
	input := model.Input{
		Instructions: `{{range .run.files}}{{.name}};{{end}}
{{range .workflow.files}}{{.name}};{{end}}
{{range .project.files}}{{.name}};{{end}}`,
		AttachmentSetRef: "aset_abcdefgh", AttachmentContext: "WORKFLOW_INPUT",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			runnerInputArtifact("artifact_abcdefgh", "workflow.txt", "text/plain", runtimecontract.AttachmentScopeInput, 1, 1, "CONTROL_CENTER"),
			runnerInputArtifact("artifact_ijklmnop", "knowledge.md", "text/markdown", runtimecontract.AttachmentScopeKnowledge, 1, 1, "KNOWLEDGE_SOURCE"),
		},
	}
	input.AttachmentSetManifestDigest = strings.Repeat("a", 64)
	input = bindAttachmentCatalog(input)
	rendered, err := renderInstructions(input)
	if err != nil {
		t.Fatalf("renderInstructions() error = %v", err)
	}
	for _, expected := range []string{"workflow.txt;", "knowledge.md;"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered scopes do not contain %q: %s", expected, rendered)
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

func TestRenderInstructionsDoesNotMaterializeUnpromisedNames(t *testing.T) {
	for _, instructions := range []string{"{{ .agent.name }}", "{{ .project.name }}"} {
		if _, err := renderInstructions(model.Input{Instructions: instructions}); err == nil {
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
