package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultCodexExecutable        = "/usr/local/bin/codex"
	codexExecOutputFilename       = "codex-last-message.json"
	maxCodexExecutionResultBytes  = 64 * 1024
	codeCodexExecutionFailed      = "codex_execution_failed"
	codeCodexExecutionTimeout     = "codex_execution_timeout"
	codeCodexExecutionCancelled   = "codex_execution_cancelled"
	codeCodexExecutionResultError = "codex_execution_result_invalid"
)

type CodexExecutor interface {
	Execute(context.Context, CodexExecutionRequest) (CodexExecutionResult, Diagnostic)
}

type CodexExecutionRequest struct {
	Config  Config
	Context AgentRunContext
	Spec    CodexSessionExecutionSpec
	Input   CodexExecutionInput
}

type CodexExecutionResult struct {
	ResultDigest       string
	ResultSchemaRef    string
	ResultSchemaDigest string
	SafeSummary        string
}

type CodexCLIExecutor struct {
	executable string
}

type codexRunnerProfile struct {
	Sandbox string
}

func NewCodexCLIExecutor() CodexCLIExecutor {
	return CodexCLIExecutor{executable: defaultCodexExecutable}
}

func NewCodexCLIExecutorForTest(executable string) CodexCLIExecutor {
	return CodexCLIExecutor{executable: strings.TrimSpace(executable)}
}

func (e CodexCLIExecutor) Execute(ctx context.Context, request CodexExecutionRequest) (CodexExecutionResult, Diagnostic) {
	profile, diagnostic := codexProfile(request.Spec.RunnerProfileRef)
	if !diagnostic.OK() {
		return CodexExecutionResult{}, diagnostic
	}
	executable := strings.TrimSpace(e.executable)
	if executable == "" {
		executable = defaultCodexExecutable
	}
	outputDir, err := os.MkdirTemp("", "kodex-agent-runner-")
	if err != nil {
		return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionResultError, "codex execution output cannot be prepared", ExitFailure)
	}
	defer func() { _ = os.RemoveAll(outputDir) }()
	outputPath := filepath.Join(outputDir, codexExecOutputFilename)
	if err := prepareCodexSubprocessDirs(outputDir); err != nil {
		return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionResultError, "codex execution environment cannot be prepared", ExitFailure)
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Spec.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executable, codexExecArgs(request.Config, request.Input, profile, outputPath)...)
	cmd.Stdin = bytes.NewReader(request.Input.Instruction)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = codexSubprocessEnv(outputDir)
	err = cmd.Run()
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionTimeout, "codex execution timed out", ExitFailure)
	case errors.Is(ctx.Err(), context.Canceled):
		return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionCancelled, "codex execution was cancelled", ExitFailure)
	case err != nil:
		return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionFailed, "codex execution failed", ExitFailure)
	}
	resultDigest, diagnostic := checkedResultDigest(outputPath)
	if !diagnostic.OK() {
		return CodexExecutionResult{}, diagnostic
	}
	return CodexExecutionResult{
		ResultDigest:       resultDigest,
		ResultSchemaRef:    request.Spec.ResultSchemaRef,
		ResultSchemaDigest: request.Spec.ResultSchemaDigest,
		SafeSummary:        "codex execution completed",
	}, OKDiagnostic()
}

func prepareCodexSubprocessDirs(root string) error {
	for _, path := range []string{
		filepath.Join(root, "home"),
		filepath.Join(root, "config"),
		filepath.Join(root, "cache"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func codexSubprocessEnv(root string) []string {
	return []string{
		"HOME=" + filepath.Join(root, "home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"TMPDIR=" + root,
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"NO_COLOR=1",
	}
}

func codexExecArgs(cfg Config, input CodexExecutionInput, profile codexRunnerProfile, outputPath string) []string {
	return []string{
		"exec",
		"--json",
		"--output-last-message", outputPath,
		"--output-schema", input.ResultSchemaPath,
		"--cd", cfg.WorkspaceMountPath,
		"--sandbox", profile.Sandbox,
		"--ephemeral",
		"-",
	}
}

func codexProfile(ref string) (codexRunnerProfile, Diagnostic) {
	switch strings.TrimSpace(ref) {
	case "runner-profile/codex-agent@v1", "runner-profile://codex-agent/default":
		return codexRunnerProfile{Sandbox: "workspace-write"}, OKDiagnostic()
	default:
		return codexRunnerProfile{}, executionContractUnavailable("codex runner profile is not supported")
	}
}

func checkedResultDigest(path string) (string, Diagnostic) {
	info, err := os.Stat(path)
	if err != nil {
		return "", NewDiagnostic(codeCodexExecutionResultError, "codex execution result is unavailable", ExitFailure)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCodexExecutionResultBytes {
		return "", NewDiagnostic(codeCodexExecutionResultError, "codex execution result has invalid size", ExitFailure)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", NewDiagnostic(codeCodexExecutionResultError, "codex execution result is unavailable", ExitFailure)
	}
	return SHA256Digest(raw), OKDiagnostic()
}
