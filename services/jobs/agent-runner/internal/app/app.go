// Package app выполняет один immutable turn либо последовательную очередь always-hot помощника.
package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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
	"time"
	"unicode/utf8"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/contextfiles"
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
		snapshot, err := input.RequiredContextSnapshot(time.Now())
		if err != nil {
			return err
		}
		if err := materializeWorkspace(input); err != nil {
			return err
		}
		if input.Mode == runtimecontract.RunnerModeWarm {
			if err := materializeInputArtifacts(lifecycleContext, input, nil); err != nil {
				return err
			}
			return contextfiles.Materialize(lifecycleContext, input, snapshot, nil)
		}
		client, err := callback.New(input)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := materializeInputArtifacts(lifecycleContext, input, client); err != nil {
			return err
		}
		return contextfiles.Materialize(lifecycleContext, input, snapshot, client)
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
	if mode == "runtime-session" {
		resultErr = runTurn(lifecycleContext, input, client, func() { state.ready.Store(true) })
		return resultErr
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil {
		return err
	}
	if err := contextfiles.Verify(input, snapshot); err != nil {
		return err
	}
	state.ready.Store(true)
	for {
		turn, available, nextErr := client.NextWarm(lifecycleContext, input)
		if nextErr != nil {
			return nextErr
		}
		if available {
			if err := runTurn(lifecycleContext, turn, client, nil); err != nil {
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

func runTurn(ctx context.Context, input model.Input, client *callback.Client, workingPathReady func()) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil {
		return errors.New("runtime turn input is invalid")
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil || contextfiles.Verify(input, snapshot) != nil {
		return completeFailure(ctx, input, client, "RUNTIME_INPUT_INVALID")
	}
	ctx, cancelContext := snapshot.BoundExecutionContext(ctx)
	defer cancelContext()
	if err := materializeWorkspace(input); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_WORKSPACE_INVALID")
	}
	if err := verifyInputArtifacts(input); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_INPUT_INVALID")
	}
	if err := codex.ValidateRuntimeProfile(input); err != nil {
		return completeFailure(ctx, input, client, runtimeExecutionFailureCode(err))
	}
	if err := resetWorkspaceDirectory(input.WorkspaceRoot, ".kodex/outbox"); err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_WORKSPACE_INVALID")
	}
	mcpProxy, err := readiness.StartMCPProxy(ctx, input, client.Token(), codex.RequiredMCPToolNames(input))
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_MCP_UNAVAILABLE")
	}
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = mcpProxy.Close(shutdown)
	}()
	if workingPathReady != nil {
		workingPathReady()
	}
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
	if strings.TrimSpace(result.FinalMessage) == "" || len(result.FinalMessage) > 64<<10 || !utf8.ValidString(result.FinalMessage) {
		return completeFailure(ctx, input, client, "RUNTIME_RESULT_INVALID")
	}
	if input.CodexSandbox == "workspace-write" && hasCapability(input, runtimecontract.ArtifactCapability) {
		if err := workspacepolicy.PublishResult(input.WorkspaceRoot, input.WorkspacePolicy, workspacepolicy.ResultProvenance{
			Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: input.RuntimeRevisionRef,
			RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
			Attempt: input.Attempt, ExecutionBindingDigest: input.ExecutionBindingDigest,
		}); err != nil {
			return completeFailure(ctx, input, client, "RUNTIME_WORKSPACE_INVALID")
		}
	}
	artifacts, err := completionArtifacts(input, result.FinalMessage)
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_ARTIFACT_INVALID")
	}
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: true, ResultSummary: result.FinalMessage, Usage: result.Usage, Artifacts: artifacts, CodexSessionID: result.SessionID, ArchiveRelativePath: result.ArchiveRelativePath, ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	if payload.Validate() != nil {
		return completeFailure(ctx, input, client, "RUNTIME_RESULT_INVALID")
	}
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
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: false, ResultSummary: summary,
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
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: false, ResultSummary: summary, SafeErrorCode: safeFailureCode(code), Usage: usage}
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
	case errors.Is(err, runtimecontract.ErrRuntimeContext), errors.Is(err, contextfiles.ErrContextFiles):
		return "RUNTIME_INPUT_INVALID"
	case errors.Is(err, codex.ErrProviderAuthentication):
		return "PROVIDER_AUTH_REJECTED"
	case errors.Is(err, codex.ErrAuthorityRequestUnsupported):
		return "RUNTIME_PROFILE_UNSUPPORTED"
	case errors.Is(err, codex.ErrRequiredMCPUnavailable):
		return "RUNTIME_MCP_UNAVAILABLE"
	case errors.Is(err, codex.ErrRuntimeProfile):
		return "RUNTIME_PROFILE_UNSUPPORTED"
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
	if err := validateMaterializedInstructions(input); err != nil {
		return err
	}
	if err := writeWorkspaceFile(input.WorkspaceRoot, "AGENTS.md", []byte(input.Instructions)); err != nil {
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
	builder.WriteString(input.Task)
	if input.CodexSessionID != "" || strings.Contains(input.Instructions, `<session-continuation used="true">`) {
		if err := appendContinuationRevision(&builder, input); err != nil {
			return nil, err
		}
	}
	result := []byte(builder.String())
	if len(result) == 0 || len(result) > 1<<20 || !utf8.Valid(result) {
		return nil, errors.New("turn prompt is invalid")
	}
	return result, nil
}

func appendContinuationRevision(builder *strings.Builder, input model.Input) error {
	overlay, err := runtimecontract.ParseConfigOverlay(input.ConfigOverlay)
	if err != nil {
		return errors.New("continuation configuration is invalid")
	}
	toolNames := make([]string, 0, len(input.EnvironmentTools))
	for _, tool := range input.EnvironmentTools {
		toolNames = append(toolNames, tool.Name+":"+tool.Command)
	}
	sort.Strings(toolNames)
	mcpTools := codex.RequiredMCPToolNames(input)
	grants := make([]string, 0, len(input.IntegrationGrants))
	for _, grant := range input.IntegrationGrants {
		grants = append(grants, grant.Ref+":"+grant.ConnectionRef+":"+grant.CapabilityKey)
	}
	sort.Strings(grants)
	files := make([]string, 0, len(input.InputArtifacts))
	for _, artifact := range input.InputArtifacts {
		files = append(files, artifact.Ref+":"+artifact.Digest)
	}
	sort.Strings(files)
	builder.WriteString("\n<runtime-revision-delta>\n")
	builder.WriteString("revision=" + input.RuntimeRevisionRef + ":" + strconv.FormatInt(input.RuntimeRevisionVersion, 10) + ":" + input.RuntimeRevisionDigest + "\n")
	builder.WriteString("attempt=" + strconv.FormatInt(int64(input.Attempt), 10) + "\n")
	builder.WriteString("model=" + input.Model + "\nreasoning=" + overlay.ModelReasoningEffort + "\n")
	builder.WriteString("image=" + input.ImageReference + ":" + input.ImageManifestDigest + "\n")
	builder.WriteString("tools=" + strings.Join(toolNames, ",") + "\n")
	builder.WriteString("mcp=" + input.MCPBindingDigest + ":" + strings.Join(mcpTools, ",") + ":" + strings.Join(grants, ",") + "\n")
	builder.WriteString("files=" + input.AttachmentSetRef + ":" + input.AttachmentSetManifestDigest + ":" + strings.Join(files, ",") + "\n")
	builder.WriteString("config=" + input.RuntimeConfigRef + ":" + strconv.FormatInt(input.RuntimeConfigVersion, 10) + ":" + input.RuntimeConfigDigest +
		":" + input.ProviderPolicyRef + ":" + strconv.FormatInt(input.ProviderPolicyVersion, 10) + ":" + input.ProviderPolicyDigest +
		":" + input.ConfigOverlayRef + ":" + strconv.FormatInt(input.ConfigOverlayVersion, 10) + ":" + input.ConfigOverlayDigest + "\n")
	builder.WriteString("environment=" + input.RuntimeEnvironmentRef + ":" + strconv.FormatInt(input.RuntimeEnvironmentVersion, 10) + ":" +
		input.RuntimeEnvironmentDigest + ":" + input.EnvironmentBindingRef + ":" + strconv.FormatInt(input.EnvironmentBindingVersion, 10) +
		":" + input.EnvironmentBindingDigest + "\n")
	builder.WriteString("</runtime-revision-delta>\n")
	for _, name := range []string{"workflow-stage", "automation", "session-continuation", "effective-capabilities"} {
		block, blockErr := materializedServiceBlock(input.Instructions, name)
		if blockErr != nil {
			return blockErr
		}
		builder.WriteString(block)
		builder.WriteByte('\n')
	}
	return nil
}

func validateMaterializedInstructions(input model.Input) error {
	value := input.Instructions
	if strings.TrimSpace(value) == "" || len(value) > 1<<20 || !utf8.ValidString(value) ||
		strings.Contains(value, "{{") || strings.Contains(value, "}}") {
		return errors.New("server-materialized instructions are invalid")
	}
	usedKinds := 0
	for _, name := range []string{"workflow-stage", "automation", "session-continuation"} {
		block, err := materializedServiceBlock(value, name)
		if err != nil {
			return err
		}
		if strings.Contains(block, `used="true"`) {
			usedKinds++
		}
	}
	capabilities, err := materializedServiceBlock(value, "effective-capabilities")
	if err != nil || usedKinds > 1 {
		return errors.New("server-materialized service blocks are invalid")
	}
	expected := append([]string(nil), input.Capabilities...)
	sort.Strings(expected)
	want := `<effective-capabilities used="false">unused</effective-capabilities>`
	if len(expected) != 0 {
		want = `<effective-capabilities used="true">` + strings.Join(expected, ",") + `</effective-capabilities>`
	}
	if capabilities != want {
		return errors.New("server-materialized capability block is invalid")
	}
	if input.CodexSessionID != "" {
		continuation, _ := materializedServiceBlock(value, "session-continuation")
		if !strings.Contains(continuation, `used="true"`) {
			return errors.New("server-materialized continuation block is missing")
		}
	}
	return nil
}

func materializedServiceBlock(value, name string) (string, error) {
	trueOpen := "<" + name + ` used="true">`
	falseOpen := "<" + name + ` used="false">`
	closeTag := "</" + name + ">"
	if strings.Count(value, trueOpen)+strings.Count(value, falseOpen) != 1 || strings.Count(value, closeTag) != 1 {
		return "", errors.New("server-materialized service block is invalid")
	}
	start := strings.Index(value, trueOpen)
	open := trueOpen
	if start < 0 {
		start, open = strings.Index(value, falseOpen), falseOpen
	}
	end := strings.Index(value[start+len(open):], closeTag)
	if start < 0 || end < 0 {
		return "", errors.New("server-materialized service block is invalid")
	}
	end += start + len(open) + len(closeTag)
	return value[start:end], nil
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
	if err := writeInputManifests(input, setManifests, workspaceManifest); err != nil {
		return err
	}
	return protectReadOnlyWorkspaceTrees(input.WorkspaceRoot, "input", "knowledge")
}

func verifyInputArtifacts(input model.Input) error {
	sets := make(map[string]runtimecontract.CanonicalAttachmentManifest, len(input.AttachmentSets))
	for _, set := range input.AttachmentSets {
		artifacts := make([]runtimecontract.RunnerInputArtifact, 0)
		for _, artifact := range input.InputArtifacts {
			if artifact.AttachmentSetRef != set.Ref {
				continue
			}
			canonical := artifact
			canonical.Scope = runtimecontract.AttachmentScopeInput
			canonical.AttachmentSetRef = ""
			canonical.AttachmentPurpose = ""
			canonical.Provenance = ""
			artifacts = append(artifacts, canonical)
		}
		manifest, err := runtimecontract.BuildAttachmentManifest(set.Ref, set.Purpose, artifacts)
		if err != nil || manifest.Digest != set.ManifestDigest {
			return errors.New("runtime attachment manifest digest is invalid")
		}
		sets[set.Ref] = manifest
	}
	workspaceManifest, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return errors.New("build runtime workspace manifest")
	}
	if err := verifyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", "manifest.json"), workspaceManifest.Bytes, int64(len(workspaceManifest.Bytes))); err != nil {
		return err
	}
	for _, set := range input.AttachmentSets {
		manifest := sets[set.Ref]
		if err := verifyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", set.Ref, "manifest.json"), manifest.Bytes, int64(len(manifest.Bytes))); err != nil {
			return err
		}
		readme := []byte("This directory contains a read-only, server-owned AttachmentSet. Read manifest.json before using files.\n")
		if err := verifyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", set.Ref, "README.md"), readme, int64(len(readme))); err != nil {
			return err
		}
	}
	for _, artifact := range input.InputArtifacts {
		path, err := runtimecontract.ArtifactWorkspacePath(input.AttachmentSetRef, artifact)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(input.WorkspaceRoot, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return errors.New("workspace artifact path is invalid")
		}
		if err := verifyWorkspaceDigest(input.WorkspaceRoot, relative, artifact); err != nil {
			return err
		}
	}
	return nil
}

func verifyWorkspaceFile(root, relative string, expected []byte, expectedSize int64) error {
	file, info, err := openWorkspaceFile(root, relative)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Size() != expectedSize || info.Mode().Perm() != 0o440 {
		return errors.New("runtime input file metadata is invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(file, expectedSize+1))
	if err != nil || int64(len(raw)) != expectedSize || subtle.ConstantTimeCompare(raw, expected) != 1 {
		return errors.New("runtime input file content is invalid")
	}
	return nil
}

func verifyWorkspaceDigest(root, relative string, artifact runtimecontract.RunnerInputArtifact) error {
	file, info, err := openWorkspaceFile(root, relative)
	if err != nil {
		return err
	}
	defer file.Close()
	if info.Size() != artifact.SizeBytes || info.Mode().Perm() != 0o440 {
		return errors.New("runtime artifact metadata is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, runtimecontract.MaximumInputArtifactBytes+1))
	actual := "sha256:" + hex.EncodeToString(digest.Sum(nil))
	if err != nil || written != artifact.SizeBytes || subtle.ConstantTimeCompare([]byte(actual), []byte(artifact.Digest)) != 1 {
		return errors.New("runtime artifact content is invalid")
	}
	return nil
}

func openWorkspaceFile(root, relative string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.IsAbs(relative) || filepath.Clean(relative) != relative ||
		relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, nil, errors.New("workspace input path is invalid")
	}
	current, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("open workspace input root")
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			_ = unix.Close(current)
			return nil, nil, errors.New("workspace input path is invalid")
		}
		next, openErr := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(current)
		if openErr != nil {
			return nil, nil, errors.New("open workspace input directory")
		}
		current = next
	}
	descriptor, err := unix.Openat(current, parts[len(parts)-1], unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	_ = unix.Close(current)
	if err != nil {
		return nil, nil, errors.New("open workspace input file")
	}
	file := os.NewFile(uintptr(descriptor), "runtime-input")
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, nil, errors.New("open workspace input file")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		_ = file.Close()
		return nil, nil, errors.New("workspace input file is invalid")
	}
	return file, info, nil
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
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime artifact directory is unsafe")
	}
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

func protectReadOnlyWorkspaceTrees(root string, relatives ...string) error {
	for _, relative := range relatives {
		tree := filepath.Join(root, relative)
		if filepath.Clean(tree) != tree || !strings.HasPrefix(tree, root+string(os.PathSeparator)) {
			return errors.New("workspace input tree is invalid")
		}
		if err := filepath.WalkDir(tree, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(filepath.Clean(path), tree) {
				return errors.New("workspace input tree is unsafe")
			}
			mode := os.FileMode(0o440)
			if entry.IsDir() {
				mode = 0o750 | os.ModeSetgid
			}
			if err := os.Chmod(path, mode); err != nil {
				return errors.New("protect workspace input tree")
			}
			return nil
		}); err != nil {
			return err
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
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name() == "workspace-write-result.json" && entries[j].Name() != entries[i].Name() {
			return true
		}
		if entries[j].Name() == "workspace-write-result.json" {
			return false
		}
		return entries[i].Name() < entries[j].Name()
	})
	for _, entry := range entries {
		if len(artifacts) >= 16 {
			break
		}
		name := entry.Name()
		if entry.IsDir() || name == "result.md" || safeFileName(name) != name {
			continue
		}
		descriptor, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
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
