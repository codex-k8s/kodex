package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

type nativeToolRecorderStub struct {
	calls []runtimecontract.NativeToolCall
	err   error
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
			{Ref: "artifact_abcdefgh", FileName: "new.txt", MediaType: "text/plain", Scope: "INPUT", Position: 1, Source: "CONTROL_CENTER", Version: 2},
			{Ref: "artifact_ijklmnop", FileName: "prior.txt", MediaType: "text/plain", Scope: "SESSION", Position: 1, Source: "CONTROL_CENTER", Version: 1},
			{Ref: "artifact_qrstuvwx", FileName: "policy.md", MediaType: "text/markdown", Scope: "KNOWLEDGE", Position: 1, Source: "KNOWLEDGE_SOURCE", Version: 3},
		},
	}
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
			{Ref: "artifact_abcdefgh", FileName: "workflow.txt", MediaType: "text/plain", Scope: "INPUT", Position: 1},
			{Ref: "artifact_ijklmnop", FileName: "knowledge.md", MediaType: "text/markdown", Scope: "KNOWLEDGE", Position: 1},
		},
	}
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
