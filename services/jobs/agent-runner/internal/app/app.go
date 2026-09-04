// Package app выполняет один immutable turn либо последовательную очередь always-hot помощника.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"text/template"
	"time"
	"unicode/utf8"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/credentialrelay"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/readiness"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

const inputPath = "/var/run/config/kodex/runtime/runtime.json"

type health struct {
	live, ready atomic.Bool
	input       model.Input
}

func Run(baseContext, lifecycleContext context.Context, args []string, buildVersion string) (resultErr error) {
	if len(args) != 2 {
		return errors.New("agent-runner mode is required")
	}
	mode := args[1]
	if mode != "runtime-init-workspace" && mode != "runtime-session" && mode != "runtime-warm" && mode != "runtime-provider" && mode != "runtime-provider-credential-relay" {
		return errors.New("agent-runner mode is invalid")
	}
	if err := security.VerifyInvocation(args, mode); err != nil {
		return err
	}
	if mode == "runtime-provider" {
		return codex.ServeProviderBroker(lifecycleContext)
	}
	input, err := model.DecodeInput(inputPath)
	if err != nil {
		return err
	}
	if mode == "runtime-provider-credential-relay" {
		return credentialrelay.Serve(lifecycleContext, input)
	}
	if mode == "runtime-init-workspace" {
		return materializeWorkspace(input)
	}
	if os.Geteuid() != 10001 {
		return errors.New("agent-runner runtime UID is invalid")
	}
	startup, cancelStartup := context.WithTimeout(lifecycleContext, 10*time.Second)
	defer cancelStartup()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv("agent-runner", buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startup, telemetryConfig)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			telemetry.CaptureException(lifecycleContext, resultErr)
		}
		trace, cancelTrace := context.WithTimeout(baseContext, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(trace))
		cancelTrace()
		sentry, cancelSentry := context.WithTimeout(baseContext, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentry))
		cancelSentry()
	}()
	state := &health{input: input}
	state.live.Store(true)
	server, serverErrors := startHealthServer(lifecycleContext, state)
	defer func() {
		state.ready.Store(false)
		shutdown, cancel := context.WithTimeout(baseContext, 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	client, err := callback.New(input)
	if err != nil {
		return err
	}
	defer client.Close()
	state.ready.Store(true)
	if mode == "runtime-session" {
		resultErr = runTurn(lifecycleContext, input, client)
		return resultErr
	}
	for {
		turn, available, nextErr := client.NextWarm(lifecycleContext, input)
		if nextErr != nil {
			return nextErr
		}
		if available {
			if err := runTurn(lifecycleContext, turn, client); err != nil {
				return err
			}
		}
		select {
		case err := <-serverErrors:
			return err
		default:
		}
		if lifecycleContext.Err() != nil {
			return lifecycleContext.Err()
		}
	}
}

func runTurn(ctx context.Context, input model.Input, client *callback.Client) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil {
		return errors.New("runtime turn input is invalid")
	}
	if err := materializeWorkspace(input); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_WORKSPACE_INVALID")
	}
	if err := materializeInputArtifacts(ctx, input, client); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_INPUT_INVALID")
	}
	mcpProxy, err := readiness.StartMCPProxy(ctx, input, client.Token())
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_MCP_UNAVAILABLE")
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = mcpProxy.Close(shutdown)
	}()
	if err := client.Progress(ctx, input, "MODEL_REQUEST_RUNNING"); err != nil {
		return err
	}
	prompt, err := buildPrompt(input)
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_INPUT_INVALID")
	}
	result, err := codex.ExecuteViaBroker(ctx, input, prompt, mcpProxy.SocketPath(), mcpProxy.LocalBearerToken())
	if err != nil {
		return completeFailure(ctx, input, client, runtimeExecutionFailureCode(err))
	}
	if err := workspacepolicy.RunCanary(input.WorkspaceRoot, input.WorkspacePolicy); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_WORKSPACE_INVALID")
	}
	if err := recordNativeToolTimeline(ctx, input, client, result.ToolCalls); err != nil {
		return err
	}
	if result.Outcome != "SUCCEEDED" {
		_, message, _ := codex.TerminalPresentation(result.FailureCode)
		return completeResultFailure(ctx, input, client, result, message)
	}
	if strings.TrimSpace(result.FinalMessage) == "" || len(result.FinalMessage) > 1<<20 || !utf8.ValidString(result.FinalMessage) {
		return completeFailure(ctx, input, client, "RUNTIME_RESULT_INVALID")
	}
	artifacts, err := completionArtifacts(input, result.FinalMessage)
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_ARTIFACT_INVALID")
	}
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: true, ResultSummary: result.FinalMessage, Usage: result.Usage, Artifacts: artifacts, CodexSessionID: result.SessionID, ArchiveRelativePath: result.ArchiveRelativePath, ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	return client.Complete(ctx, input, payload)
}

type nativeToolCallRecorder interface {
	RecordNativeToolCall(context.Context, model.Input, runtimecontract.NativeToolCall) error
}

func recordNativeToolTimeline(ctx context.Context, input model.Input, recorder nativeToolCallRecorder, calls []runtimecontract.NativeToolCall) error {
	if len(calls) > runtimecontract.MaximumNativeToolCalls {
		return errors.New("native tool timeline is invalid")
	}
	for _, call := range calls {
		if call.Validate() != nil {
			return errors.New("native tool timeline is invalid")
		}
		if err := recorder.RecordNativeToolCall(ctx, input, call); err != nil {
			return errors.New("record native tool timeline")
		}
	}
	return nil
}

func completeResultFailure(ctx context.Context, input model.Input, client *callback.Client, result codex.Result, summary string) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: false, ResultSummary: summary,
		SafeErrorCode: safeFailureCode(result.FailureCode), Usage: result.Usage, CodexSessionID: result.SessionID,
		ArchiveRelativePath: result.ArchiveRelativePath, ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	return client.Complete(context.WithoutCancel(ctx), input, payload)
}

func completionArtifacts(input model.Input, finalMessage string) ([]runtimecontract.RunnerArtifact, error) {
	if input.SystemAssistant || !hasCapability(input, runtimecontract.ArtifactCapability) {
		return nil, nil
	}
	return collectArtifacts(input, finalMessage)
}

func hasCapability(input model.Input, expected string) bool {
	for _, capability := range input.Capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

func completeFailure(ctx context.Context, input model.Input, client *callback.Client, code string) error {
	return completeFailureWithSummary(ctx, input, client, code, "i18n:"+code)
}
func completeFailureWithSummary(ctx context.Context, input model.Input, client *callback.Client, code, summary string) error {
	return completeFailureWithSummaryAndUsage(ctx, input, client, code, summary, runtimecontract.TokenUsage{})
}
func completeFailureWithSummaryAndUsage(ctx context.Context, input model.Input, client *callback.Client, code, summary string, usage runtimecontract.TokenUsage) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: false, ResultSummary: summary, SafeErrorCode: safeFailureCode(code), Usage: usage}
	if err := client.Complete(context.WithoutCancel(ctx), input, payload); err != nil {
		return err
	}
	return nil
}

func safeFailureCode(code string) string {
	switch code {
	case "unauthorized", "authentication_required", "authentication_expired", "PROVIDER_AUTH_REJECTED":
		return "PROVIDER_AUTH_REJECTED"
	case "usage_limit_exceeded":
		return "PROVIDER_RATE_LIMITED"
	case "server_overloaded", "RUNTIME_PROVIDER_UNAVAILABLE":
		return "PROVIDER_UNAVAILABLE"
	case "provider_internal_error", "provider_transport_failure":
		return "PROVIDER_UNAVAILABLE"
	case "cyber_policy", "policy_denied":
		return "PROVIDER_REQUEST_REJECTED"
	case "provider_bad_request", "provider_sandbox_error":
		return "PROVIDER_REQUEST_REJECTED"
	case "invalid_configuration", "stale_grant", "RUNTIME_CONFIGURATION_STALE", "RUNTIME_PROFILE_UNSUPPORTED":
		return "RUNTIME_PROFILE_UNSUPPORTED"
	case "context_window_exceeded", "session_budget_exceeded", "thread_rollback_failed", "active_turn_not_steerable":
		return "RUNTIME_PROFILE_UNSUPPORTED"
	case "provider_error_info_invalid", "provider_interrupted", "provider_other_error", "RUNTIME_RESULT_INVALID", "RUNTIME_ARTIFACT_INVALID":
		return "PROVIDER_RESPONSE_INVALID"
	case "RUNTIME_INPUT_INVALID", "RUNTIME_WORKSPACE_INVALID":
		return "RUNTIME_INPUT_INVALID"
	case "RUNTIME_MCP_UNAVAILABLE":
		return "RUNTIME_MCP_UNAVAILABLE"
	default:
		return "RUNTIME_UNAVAILABLE"
	}
}

func runtimeExecutionFailureCode(err error) string {
	switch {
	case errors.Is(err, codex.ErrProviderAuthentication):
		return "PROVIDER_AUTH_REJECTED"
	case errors.Is(err, codex.ErrAuthorityRequestUnsupported):
		return "RUNTIME_PROFILE_UNSUPPORTED"
	case errors.Is(err, codex.ErrRequiredMCPUnavailable):
		return "RUNTIME_MCP_UNAVAILABLE"
	default:
		return "RUNTIME_PROVIDER_UNAVAILABLE"
	}
}

func materializeWorkspace(input model.Input) error {
	for _, relative := range []string{".kodex", ".kodex/inbox", ".kodex/outbox", ".kodex/state", ".kodex/state/codex-home", "input", "session", "knowledge"} {
		if err := security.EnsureSharedWorkspaceDirectory(relative); err != nil {
			return err
		}
	}
	if err := workspacepolicy.RunCanary(input.WorkspaceRoot, input.WorkspacePolicy); err != nil {
		return err
	}
	renderedInstructions, err := renderInstructions(input)
	if err != nil {
		return err
	}
	if err := writeWorkspaceFile(input.WorkspaceRoot, "AGENTS.md", []byte(renderedInstructions+"\n")); err != nil {
		return err
	}
	prompt, err := buildPrompt(input)
	if input.Mode == runtimecontract.RunnerModeWarm {
		prompt = []byte("Warm runtime is ready. Wait for a server-owned turn.\n")
		err = nil
	}
	if err != nil {
		return err
	}
	return writeWorkspaceFile(input.WorkspaceRoot, ".kodex/inbox/prompt.md", prompt)
}

func buildPrompt(input model.Input) ([]byte, error) {
	if input.Mode != runtimecontract.RunnerModeTurn {
		return nil, errors.New("turn prompt is unavailable")
	}
	var builder strings.Builder
	builder.WriteString("# Task\n\n")
	builder.WriteString(strings.TrimSpace(input.Task))
	builder.WriteString("\n")
	if err := appendAttachmentNotice(&builder, input); err != nil {
		return nil, err
	}
	if len(input.SessionContext) != 0 {
		builder.WriteString("\n# Session context\n")
		for _, message := range input.SessionContext {
			builder.WriteString("\n## ")
			builder.WriteString(message.Role)
			builder.WriteString("\n")
			builder.WriteString(message.Content)
			builder.WriteString("\n")
		}
	}
	if len(input.BoundedInput) != 0 {
		raw, err := json.MarshalIndent(input.BoundedInput, "", "  ")
		if err != nil {
			return nil, errors.New("encode bounded turn input")
		}
		builder.WriteString("\n# Bounded input\n\n```json\n")
		builder.Write(raw)
		builder.WriteString("\n```\n")
	}
	if hasCapability(input, runtimecontract.ArtifactCapability) {
		builder.WriteString("\n# File access\n")
		if len(input.InputArtifacts) != 0 {
			builder.WriteString("\nAll materialized files are read-only. The complete catalog is `/workspace/input/manifest.json`.\n")
			for _, artifact := range input.InputArtifacts {
				path, pathErr := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
				if pathErr != nil {
					return nil, errors.New("resolve prompt artifact path")
				}
				builder.WriteString("\n- `")
				builder.WriteString(path)
				builder.WriteString("` — ")
				builder.WriteString(fmt.Sprintf("%q", artifact.FileName))
				builder.WriteString(" (")
				builder.WriteString(artifact.MediaType)
				builder.WriteString(")")
			}
			builder.WriteString("\n")
		}
		builder.WriteString("\nWrite every output file directly to `/workspace/.kodex/outbox/<safe-name>`. ")
		builder.WriteString("The workspace root and all other paths are read-only; do not write output files to `/workspace` itself.\n")
	}
	result := []byte(builder.String())
	if len(result) == 0 || len(result) > 1<<20 || !utf8.Valid(result) {
		return nil, errors.New("turn prompt is invalid")
	}
	return result, nil
}

func appendAttachmentNotice(builder *strings.Builder, input model.Input) error {
	inputFiles := scopedArtifacts(input.InputArtifacts, "INPUT")
	if len(inputFiles) == 0 {
		return nil
	}
	builder.WriteString("\n# Platform attachment notice\n\n")
	builder.WriteString("The user attached ")
	builder.WriteString(strconv.Itoa(len(inputFiles)))
	builder.WriteString(" read-only file(s) to this turn. The authoritative manifest is `/workspace/input/")
	builder.WriteString(input.AttachmentSetRef)
	builder.WriteString("/manifest.json` and the files directory is `/workspace/input/")
	builder.WriteString(input.AttachmentSetRef)
	builder.WriteString("/files`.\n")
	if len(inputFiles) <= 20 {
		for _, artifact := range inputFiles {
			path, err := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
			if err != nil {
				return errors.New("resolve attachment notice path")
			}
			builder.WriteString("\n- `")
			builder.WriteString(path)
			builder.WriteString("`")
		}
		builder.WriteString("\n")
	}
	if input.CodexSessionID != "" || input.AttachmentContext == "SESSION_TURN" || input.AttachmentContext == "OWNER_GATE_MESSAGE" {
		builder.WriteString("These files were added with a continuation. Treat them as new input for the current turn even when earlier session context does not mention them.\n")
	}
	return nil
}

func scopedArtifacts(artifacts []runtimecontract.RunnerInputArtifact, scope string) []runtimecontract.RunnerInputArtifact {
	result := make([]runtimecontract.RunnerInputArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Scope == scope {
			result = append(result, artifact)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Position < result[right].Position })
	return result
}

func renderInstructions(input model.Input) (string, error) {
	parsed, err := template.New("instructions").Option("missingkey=error").Parse(input.Instructions)
	if err != nil {
		return "", errors.New("parse instruction template")
	}
	variables, err := promptTemplateVariables(input)
	if err != nil {
		return "", err
	}
	var rendered strings.Builder
	if err := parsed.Execute(&rendered, variables); err != nil {
		return "", errors.New("render instruction template")
	}
	if len(input.EnvironmentTools) > 0 {
		rendered.WriteString("\n\n# Verified runtime tools\n")
		rendered.WriteString("Only the following environment-selected executables are declared available for this runtime revision:\n")
		for _, tool := range input.EnvironmentTools {
			rendered.WriteString("\n- `")
			rendered.WriteString(tool.Command)
			rendered.WriteString("` — ")
			rendered.WriteString(tool.Description)
			if tool.UsageHint != "" {
				rendered.WriteString(" (")
				rendered.WriteString(tool.UsageHint)
				rendered.WriteString(")")
			}
		}
		rendered.WriteString("\n")
	}
	if rendered.Len() == 0 || rendered.Len() > 1<<20 || !utf8.ValidString(rendered.String()) {
		return "", errors.New("rendered instructions are invalid")
	}
	return rendered.String(), nil
}

func promptTemplateVariables(input model.Input) (map[string]any, error) {
	manifest, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return nil, errors.New("build instruction attachment manifest")
	}
	fileScope := func(files []runtimecontract.AttachmentManifestFile, directory, manifestPath string) map[string]any {
		items := make([]map[string]any, 0, len(files))
		for _, file := range files {
			items = append(items, map[string]any{
				"ref": file.ArtifactRef, "name": file.FileName, "media_type": file.MediaType,
				"size": file.SizeBytes, "sha256": file.SHA256,
				"path": file.Path, "source": file.Source, "version": file.Version,
				"revision": file.Revision, "purpose": file.Purpose,
			})
		}
		return map[string]any{"files": items, "files_count": len(items), "files_dir": directory, "manifest_path": manifestPath}
	}
	inputs := scopedManifestFiles(manifest.Manifest.Files, runtimecontract.AttachmentScopeInput)
	sessionInputs := append(scopedManifestFiles(manifest.Manifest.Files, runtimecontract.AttachmentScopeSession), inputs...)
	knowledge := scopedManifestFiles(manifest.Manifest.Files, runtimecontract.AttachmentScopeKnowledge)
	inputDirectory, inputManifest := "", "/workspace/input/manifest.json"
	if input.AttachmentSetRef != "" {
		inputDirectory = "/workspace/input/" + input.AttachmentSetRef + "/files"
		inputManifest = "/workspace/input/" + input.AttachmentSetRef + "/manifest.json"
	}
	inputScope := fileScope(inputs, inputDirectory, inputManifest)
	emptyScope := fileScope(nil, "", "")
	sessionScope := fileScope(sessionInputs, "/workspace", "/workspace/input/manifest.json")
	projectScope := fileScope(knowledge, "/workspace/knowledge", "/workspace/input/manifest.json")
	tools := make([]map[string]any, 0, len(input.EnvironmentTools))
	for _, tool := range input.EnvironmentTools {
		tools = append(tools, map[string]any{
			"name": tool.Name, "command": tool.Command, "description": tool.Description, "usage_hint": tool.UsageHint,
		})
	}
	image := map[string]any{"reference": input.EnvironmentImage.Reference, "digest": input.EnvironmentImage.Digest}
	variables := map[string]any{
		"agent":   map[string]any{"ref": input.AgentRef},
		"project": mergeFileScope(map[string]any{"ref": input.ProjectRef}, projectScope),
		"run":     mergeFileScope(map[string]any{"ref": input.RunRef}, inputScope),
		"session": mergeFileScope(map[string]any{"ref": input.SessionRef}, sessionScope),
		"turn":    map[string]any{"ref": input.TurnRef},
		"runtime": map[string]any{"environment": map[string]any{
			"ref": input.RuntimeEnvironmentRef, "image": image, "tools": tools,
		}},
		"tools":    tools,
		"input":    inputScope,
		"files":    inputScope["files"],
		"inputs":   input.BoundedInput,
		"workflow": emptyScope,
		"gate":     emptyScope,
	}
	if input.AttachmentContext == "WORKFLOW_INPUT" {
		variables["workflow"] = inputScope
	}
	if input.AttachmentContext == "OWNER_GATE_MESSAGE" {
		variables["gate"] = inputScope
	}
	return variables, nil
}

func mergeFileScope(base, scope map[string]any) map[string]any {
	for key, value := range scope {
		base[key] = value
	}
	return base
}

func scopedManifestFiles(files []runtimecontract.AttachmentManifestFile, scope string) []runtimecontract.AttachmentManifestFile {
	result := make([]runtimecontract.AttachmentManifestFile, 0, len(files))
	for _, file := range files {
		if file.Scope == scope {
			result = append(result, file)
		}
	}
	return result
}

func materializeInputArtifacts(ctx context.Context, input model.Input, client *callback.Client) error {
	setManifests := make(map[string]runtimecontract.CanonicalAttachmentManifest, len(input.AttachmentSets))
	for _, set := range input.AttachmentSets {
		setArtifacts := make([]runtimecontract.RunnerInputArtifact, 0)
		for _, artifact := range input.InputArtifacts {
			if artifact.AttachmentSetRef != set.Ref {
				continue
			}
			canonicalArtifact := artifact
			canonicalArtifact.Scope = runtimecontract.AttachmentScopeInput
			canonicalArtifact.AttachmentSetRef = ""
			canonicalArtifact.AttachmentPurpose = ""
			canonicalArtifact.Provenance = ""
			setArtifacts = append(setArtifacts, canonicalArtifact)
		}
		manifest, err := runtimecontract.BuildAttachmentManifest(set.Ref, set.Purpose, setArtifacts)
		if err != nil || manifest.Digest != set.ManifestDigest {
			return errors.New("runtime attachment manifest digest is invalid")
		}
		setManifests[set.Ref] = manifest
	}
	workspaceManifest, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return errors.New("build runtime workspace manifest")
	}
	if err := resetWorkspaceDirectory(input.WorkspaceRoot, "input"); err != nil {
		return err
	}
	if err := resetWorkspaceDirectory(input.WorkspaceRoot, "knowledge"); err != nil {
		return err
	}
	if err := resetWorkspaceDirectory(input.WorkspaceRoot, "session"); err != nil {
		return err
	}
	for _, set := range input.AttachmentSets {
		for _, relative := range []string{filepath.Join("input", set.Ref), filepath.Join("input", set.Ref, "files")} {
			if err := security.EnsureSharedWorkspaceDirectory(relative); err != nil {
				return err
			}
		}
	}
	for _, artifact := range input.InputArtifacts {
		path, pathErr := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
		if pathErr != nil {
			return pathErr
		}
		relative, relativeErr := filepath.Rel(input.WorkspaceRoot, path)
		if relativeErr != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("workspace artifact path is invalid")
		}
		if err := writeWorkspaceArtifact(ctx, input, artifact, relative, client); err != nil {
			return err
		}
	}
	return writeInputManifests(input, setManifests, workspaceManifest)
}

func writeInputManifests(input model.Input, sets map[string]runtimecontract.CanonicalAttachmentManifest, workspace runtimecontract.CanonicalWorkspaceAttachmentManifest) error {
	for _, set := range input.AttachmentSets {
		manifest, exists := sets[set.Ref]
		if !exists {
			return errors.New("runtime attachment manifest is missing")
		}
		manifestPath := filepath.Join("input", set.Ref, "manifest.json")
		if err := writeReadOnlyWorkspaceFile(input.WorkspaceRoot, manifestPath, manifest.Bytes); err != nil {
			return err
		}
		readme := []byte("This directory contains a read-only, server-owned AttachmentSet. Read manifest.json before using files.\n")
		if err := writeReadOnlyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", set.Ref, "README.md"), readme); err != nil {
			return err
		}
	}
	return writeReadOnlyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", "manifest.json"), workspace.Bytes)
}

func resetWorkspaceDirectory(root, relative string) error {
	directory := filepath.Join(root, relative)
	entries, err := os.ReadDir(directory)
	if err != nil {
		return errors.New("read runtime artifact directory")
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if !strings.HasPrefix(filepath.Clean(path), directory+string(os.PathSeparator)) {
			return errors.New("runtime artifact path is invalid")
		}
		if err := os.RemoveAll(path); err != nil {
			return errors.New("clear runtime artifact directory")
		}
	}
	return nil
}

func writeWorkspaceArtifact(ctx context.Context, input model.Input, artifact runtimecontract.RunnerInputArtifact, relative string, client *callback.Client) error {
	path := filepath.Join(input.WorkspaceRoot, relative)
	if filepath.Clean(path) != path || !strings.HasPrefix(path, input.WorkspaceRoot+string(os.PathSeparator)) {
		return errors.New("workspace artifact path is invalid")
	}
	temporary := path + ".next"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o440)
	if err != nil {
		return errors.New("create workspace artifact")
	}
	writeErr := client.WriteArtifact(ctx, input, artifact, file)
	if syncErr := file.Sync(); writeErr == nil {
		writeErr = syncErr
	}
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(temporary)
		return writeErr
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return errors.New("commit workspace artifact")
	}
	return nil
}

func writeReadOnlyWorkspaceFile(root, relative string, payload []byte) error {
	if err := writeWorkspaceFile(root, relative, payload); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(root, relative), 0o440); err != nil {
		return errors.New("protect workspace input file")
	}
	return nil
}

func writeWorkspaceFile(root, relative string, payload []byte) error {
	path := filepath.Join(root, relative)
	if filepath.Clean(path) != path || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return errors.New("workspace file path is invalid")
	}
	temporary := path + ".next"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o640)
	if err != nil {
		return errors.New("create workspace file")
	}
	if _, err = file.Write(payload); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.New("write workspace file")
	}
	if err = os.Rename(temporary, path); err != nil {
		return errors.New("commit workspace file")
	}
	return nil
}

func collectArtifacts(input model.Input, markdown string) ([]runtimecontract.RunnerArtifact, error) {
	artifacts := []runtimecontract.RunnerArtifact{artifact("result.md", "text/markdown", []byte(markdown))}
	directory, err := workspacepolicy.OpenOutbox(input.WorkspaceRoot)
	if err != nil {
		return nil, errors.New("read runtime outbox")
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, errors.New("read runtime outbox")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if len(artifacts) >= 16 {
			break
		}
		name := entry.Name()
		if entry.IsDir() || name == "result.md" || safeFileName(name) != name {
			continue
		}
		descriptor, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, errors.New("open runtime artifact")
		}
		file := os.NewFile(uintptr(descriptor), "runtime-artifact")
		if file == nil {
			_ = unix.Close(descriptor)
			return nil, errors.New("open runtime artifact")
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
			file.Close()
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, 1<<20+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || int64(len(raw)) != info.Size() {
			return nil, errors.New("read runtime artifact")
		}
		mediaType := mime.TypeByExtension(filepath.Ext(name))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		artifacts = append(artifacts, artifact(name, mediaType, raw))
	}
	return artifacts, nil
}

func artifact(name, mediaType string, content []byte) runtimecontract.RunnerArtifact {
	digest := sha256.Sum256(content)
	return runtimecontract.RunnerArtifact{FileName: name, MediaType: mediaType, SHA256: hex.EncodeToString(digest[:]), Content: content}
}
func safeFileName(value string) string {
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "/\\\x00\r\n") || value == "." || value == ".." {
		return ""
	}
	return value
}

func startHealthServer(ctx context.Context, state *health) (*http.Server, <-chan error) {
	server := &http.Server{Addr: ":9090", Handler: healthHandler(state), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	return server, done
}

func healthHandler(state *health) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.live.Load() {
			http.Error(writer, "process is stopping", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.live.Load() {
			http.Error(writer, "process is stopping", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.ready.Load() {
			http.Error(writer, "runtime is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := workspacepolicy.RunCanary(state.input.WorkspaceRoot, state.input.WorkspacePolicy); err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	return mux
}
