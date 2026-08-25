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
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/readiness"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
)

const inputPath = "/var/run/config/kodex/runtime/runtime.json"

type health struct{ live, ready atomic.Bool }

func Run(baseContext, lifecycleContext context.Context, args []string, buildVersion string) (resultErr error) {
	if len(args) != 2 {
		return errors.New("agent-runner mode is required")
	}
	mode := args[1]
	if mode != "runtime-init-workspace" && mode != "runtime-session" && mode != "runtime-warm" && mode != "runtime-provider" {
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
	state := &health{}
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
		return completeFailure(ctx, input, client, "RUNTIME_PROVIDER_UNAVAILABLE")
	}
	if result.Outcome != "SUCCEEDED" {
		_, message, _ := codex.TerminalPresentation(result.FailureCode)
		return completeFailureWithSummary(ctx, input, client, result.FailureCode, message)
	}
	if strings.TrimSpace(result.FinalMessage) == "" || len(result.FinalMessage) > 1<<20 || !utf8.ValidString(result.FinalMessage) {
		return completeFailure(ctx, input, client, "RUNTIME_RESULT_INVALID")
	}
	artifacts, err := collectArtifacts(input, result.FinalMessage)
	if err != nil {
		return completeFailure(ctx, input, client, "RUNTIME_ARTIFACT_INVALID")
	}
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: true, ResultSummary: result.FinalMessage, Artifacts: artifacts}
	return client.Complete(ctx, input, payload)
}

func completeFailure(ctx context.Context, input model.Input, client *callback.Client, code string) error {
	return completeFailureWithSummary(ctx, input, client, code, "i18n:"+code)
}
func completeFailureWithSummary(ctx context.Context, input model.Input, client *callback.Client, code, summary string) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Success: false, ResultSummary: summary, SafeErrorCode: safeFailureCode(code)}
	if err := client.Complete(context.WithoutCancel(ctx), input, payload); err != nil {
		return err
	}
	return nil
}

func safeFailureCode(code string) string {
	switch code {
	case "unauthorized", "authentication_required", "authentication_expired":
		return "PROVIDER_AUTH_REJECTED"
	case "usage_limit_exceeded":
		return "PROVIDER_RATE_LIMITED"
	case "server_overloaded", "RUNTIME_PROVIDER_UNAVAILABLE":
		return "PROVIDER_UNAVAILABLE"
	case "cyber_policy", "policy_denied":
		return "PROVIDER_REQUEST_REJECTED"
	case "invalid_configuration", "stale_grant", "RUNTIME_CONFIGURATION_STALE":
		return "RUNTIME_PROFILE_UNSUPPORTED"
	case "provider_error_info_invalid", "provider_interrupted", "RUNTIME_RESULT_INVALID", "RUNTIME_ARTIFACT_INVALID":
		return "PROVIDER_RESPONSE_INVALID"
	case "RUNTIME_INPUT_INVALID", "RUNTIME_WORKSPACE_INVALID":
		return "RUNTIME_INPUT_INVALID"
	default:
		return "RUNTIME_UNAVAILABLE"
	}
}

func materializeWorkspace(input model.Input) error {
	for _, relative := range []string{".kodex", ".kodex/inbox", ".kodex/outbox", ".kodex/state", "input"} {
		if err := security.EnsureSharedWorkspaceDirectory(relative); err != nil {
			return err
		}
	}
	if err := writeWorkspaceFile(input.WorkspaceRoot, "AGENTS.md", []byte(input.Instructions+"\n")); err != nil {
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
	if len(input.InputArtifacts) != 0 {
		builder.WriteString("\n# Input files\n")
		for index, artifact := range input.InputArtifacts {
			builder.WriteString("\n- `")
			builder.WriteString(artifactWorkspacePath(index, artifact))
			builder.WriteString("` — ")
			builder.WriteString(fmt.Sprintf("%q", artifact.FileName))
			builder.WriteString(" (")
			builder.WriteString(artifact.MediaType)
			builder.WriteString(")")
		}
		builder.WriteString("\n")
	}
	result := []byte(builder.String())
	if len(result) == 0 || len(result) > 1<<20 || !utf8.Valid(result) {
		return nil, errors.New("turn prompt is invalid")
	}
	return result, nil
}

func materializeInputArtifacts(ctx context.Context, input model.Input, client *callback.Client) error {
	directory := filepath.Join(input.WorkspaceRoot, "input")
	entries, err := os.ReadDir(directory)
	if err != nil {
		return errors.New("read runtime input directory")
	}
	for _, entry := range entries {
		if entry.IsDir() || (entry.Type()&os.ModeType != 0 && entry.Type()&os.ModeSymlink == 0) {
			return errors.New("runtime input directory contains an unsupported entry")
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
			return errors.New("clear runtime input directory")
		}
	}
	for index, artifact := range input.InputArtifacts {
		raw, err := client.ReadArtifact(ctx, input, artifact)
		if err != nil {
			return err
		}
		if err := writeWorkspaceFile(input.WorkspaceRoot, artifactWorkspacePath(index, artifact), raw); err != nil {
			return err
		}
	}
	return nil
}

func artifactWorkspacePath(index int, artifact runtimecontract.RunnerInputArtifact) string {
	extension := strings.ToLower(filepath.Ext(artifact.FileName))
	if len(extension) > 16 || !safeArtifactExtension(extension) {
		extension = ".bin"
	}
	return filepath.Join("input", fmt.Sprintf("%03d%s", index+1, extension))
}

func safeArtifactExtension(extension string) bool {
	if len(extension) < 2 || extension[0] != '.' {
		return false
	}
	for _, character := range extension[1:] {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		return false
	}
	return true
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
	entries, err := os.ReadDir(input.OutboxRoot)
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
		path := filepath.Join(input.OutboxRoot, name)
		file, err := os.Open(path)
		if err != nil {
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
		writer.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Addr: ":9090", Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
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
