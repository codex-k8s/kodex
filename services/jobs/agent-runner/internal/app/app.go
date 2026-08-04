// Package app собирает lifecycle одного server-owned claimed turn.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedobservability "github.com/codex-k8s/matter-codex/libs/go/observability"
	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/clients/controlplane"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/handoff"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/materialize"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/output"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/readiness"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/security"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const inputPath = "/var/run/config/mattercodex/runtime/runtime.json"

var retryDelays = []time.Duration{time.Minute, 3 * time.Minute, 5 * time.Minute}

type health struct {
	live, ready atomic.Bool
	turns       *prometheus.CounterVec
	retries     *prometheus.CounterVec
}

type liveTurnLease struct {
	mu    sync.Mutex
	value controlplane.TurnLease
}

func (lease *liveTurnLease) progress(ctx context.Context, client *controlplane.Client,
	execution controlplane.Execution, kind controlplanev1.RuntimeProgressKind, sequence uint32, markdown string,
) error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return client.Progress(ctx, lease.value, execution, kind, sequence, markdown)
}

func (lease *liveTurnLease) renew(ctx context.Context, client *controlplane.Client) error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	renewed, err := client.Renew(ctx, lease.value)
	if err != nil {
		return err
	}
	lease.value = renewed
	return nil
}

func Run(baseContext, lifecycleContext context.Context, args []string, buildVersion string) (resultErr error) {
	if len(args) != 2 {
		return errors.New("agent-runner mode is required")
	}
	mode := args[1]
	if mode != "runtime-init-workspace" && mode != "runtime-session" {
		return errors.New("agent-runner mode is invalid")
	}
	if err := security.VerifyInvocation(args, mode); err != nil {
		return err
	}
	startupContext, startupCancel := context.WithTimeout(lifecycleContext, 10*time.Second)
	defer startupCancel()
	telemetryConfig, err := sharedobservability.RuntimeConfigFromEnv("agent-runner", buildVersion)
	if err != nil {
		return err
	}
	telemetry, err := sharedobservability.NewRuntime(startupContext, telemetryConfig)
	if err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			telemetry.CaptureException(lifecycleContext, resultErr)
		}
		tracingContext, tracingCancel := context.WithTimeout(baseContext, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.ShutdownTracing(tracingContext))
		tracingCancel()
		sentryContext, sentryCancel := context.WithTimeout(baseContext, 5*time.Second)
		resultErr = errors.Join(resultErr, telemetry.FlushSentry(sentryContext))
		sentryCancel()
	}()
	input, err := model.DecodeInput(inputPath)
	if err != nil {
		return err
	}
	if mode == "runtime-init-workspace" {
		return materialize.Run(lifecycleContext, input)
	}
	if os.Geteuid() == 0 {
		return errors.New("agent-runner runtime must not run as root")
	}
	state := newHealth()
	serverErrors := make(chan error, 1)
	server := startHealthServer(state, serverErrors)
	defer func() {
		state.ready.Store(false)
		shutdownContext, cancel := context.WithTimeout(baseContext, 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	state.live.Store(true)
	if err := runTurn(baseContext, lifecycleContext, input, state); err != nil {
		state.turns.WithLabelValues("incident").Inc()
		return err
	}
	state.turns.WithLabelValues("handoff").Inc()
	select {
	case err := <-serverErrors:
		return err
	default:
		return nil
	}
}

func runTurn(baseContext, ctx context.Context, input model.Input, state *health) error {
	if err := verifyRuntimeFiles(input); err != nil {
		return err
	}
	if err := security.EnsureWorkspaceDirectory(".matter-codex/outbox"); err != nil {
		return err
	}
	if err := materialize.Run(ctx, input); err != nil {
		return err
	}
	if err := security.EnsureWorkspaceDirectory(".matter-codex/state/codex-home"); err != nil {
		return err
	}
	mcpToken, err := codex.ReadCredential(input.CredentialFiles.MCPToken)
	if err != nil {
		return err
	}
	mcpProxy, err := readiness.StartMCPProxy(ctx, input, mcpToken)
	if err != nil {
		return err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(baseContext, 5*time.Second)
		defer cancel()
		_ = mcpProxy.Close(shutdownContext)
	}()
	if err := codex.PrepareHome(input, mcpProxy.URL()); err != nil {
		return err
	}
	client, err := controlplane.Dial(ctx, input)
	if err != nil {
		return err
	}
	defer client.Close()
	checkContext, checkCancel := context.WithTimeout(ctx, 5*time.Second)
	err = client.Check(checkContext)
	checkCancel()
	if err != nil {
		return err
	}
	lease, err := client.Claim(ctx)
	if err != nil {
		return err
	}
	execution, err := waitForAdmission(ctx, client)
	if err != nil {
		return err
	}
	liveLease := &liveTurnLease{value: lease}
	state.ready.Store(true)
	if err := liveLease.progress(ctx, client, execution,
		controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_STATUS, 1, "Выполнение хода начато."); err != nil {
		return errors.New("publish runtime start status")
	}
	if err := liveLease.progress(ctx, client, execution,
		controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_PROGRESS, 1, "Codex выполняет задачу."); err != nil {
		return errors.New("publish runtime progress")
	}
	runContext, stopRun := context.WithCancel(ctx)
	defer stopRun()
	heartbeatErrors := make(chan error, 1)
	go heartbeat(runContext, stopRun, client, liveLease, heartbeatErrors)
	prompt, err := os.ReadFile(filepath.Join(input.WorkspaceRoot, input.PromptPath))
	if err != nil || len(prompt) == 0 || len(prompt) > 1<<20 || !utf8.Valid(prompt) {
		return errors.New("read immutable turn prompt")
	}
	result, err := executeWithCapacityRetry(runContext, input, prompt, client, liveLease, execution, state)
	if err != nil {
		return err
	}
	select {
	case heartbeatErr := <-heartbeatErrors:
		return heartbeatErr
	default:
	}
	execution, err = client.GetExecution(ctx)
	if err != nil {
		return err
	}
	terminalMarkdown := result.FinalMessage
	if terminalMarkdown == "" {
		terminalMarkdown = "Выполнение завершилось без итогового сообщения; код результата: `" + result.FailureCode + "`."
	}
	outputs, err := output.Build(input, terminalMarkdown, result.ArchivePath)
	if err != nil || len(outputs) == 0 {
		return errors.New("build terminal runtime outputs")
	}
	outcome := result.Outcome
	if codex.BlockedFailure(result.FailureCode) {
		outcome = "BLOCKED"
	}
	terminalDigest := sha256.Sum256([]byte(outcome + "\x00" + result.SessionID + "\x00" +
		result.ArchiveSHA256 + "\x00" + outputs[0].SHA256))
	handoffValue := runtimecontract.HandoffV2{Schema: runtimecontract.HandoffSchemaV2,
		ExecutionID: input.ExecutionID, ExecutionVersion: execution.Version, Fence: execution.Fence,
		GrantGeneration: input.GrantGeneration, RuntimeRevisionSHA256: input.RuntimeRevisionSHA256,
		EffectiveRuntimeSHA256: input.EffectiveRuntimeSHA256, ImmutableInputSHA256: input.ImmutableInputSHA256,
		SessionID: input.SessionID, TurnID: input.TurnID, Attempt: input.Attempt,
		ProviderBindingID: input.ProviderBindingID, ProviderBindingVersion: input.ProviderBindingVersion,
		ProviderBindingSHA256: input.ProviderBindingSHA256, Outcome: outcome,
		TerminalReference: "codex://sessions/" + result.SessionID + "/executions/" + input.ExecutionID,
		TerminalSHA256:    hex.EncodeToString(terminalDigest[:]), Outputs: outputs, CodexSessionID: result.SessionID,
		ArchiveRelativePath: result.ArchiveRelativePath,
		ArchiveSHA256:       result.ArchiveSHA256,
		ArchiveProvenance:   "codex-app-server-rollout-v1:" + input.ExecutionID + ":" + result.ArchiveRelativePath + ":" + result.ArchiveSHA256,
		ObservedAt:          time.Now().UTC()}
	state.ready.Store(false)
	return handoff.Publish(ctx, input, handoffValue)
}

func waitForAdmission(ctx context.Context, client *controlplane.Client) (controlplane.Execution, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		execution, err := client.GetExecution(ctx)
		if err == nil {
			switch execution.State {
			case controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_ADMITTED,
				controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_RUNNING:
				return execution, nil
			case controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_CANCELLED,
				controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_EXPIRED,
				controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_RETRIED:
				return controlplane.Execution{}, errors.New("runtime execution closed before admission")
			}
		}
		select {
		case <-ctx.Done():
			return controlplane.Execution{}, context.Canceled
		case <-ticker.C:
		}
	}
}

func heartbeat(ctx context.Context, cancel context.CancelFunc, client *controlplane.Client, lease *liveTurnLease,
	failures chan<- error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := lease.renew(ctx, client); err != nil {
				failures <- errors.New("renew exact turn lease")
				cancel()
				return
			}
		}
	}
}

func executeWithCapacityRetry(ctx context.Context, input model.Input, prompt []byte,
	client *controlplane.Client, lease *liveTurnLease, execution controlplane.Execution, state *health,
) (codex.Result, error) {
	currentInput := input
	for retry := 0; ; retry++ {
		result, err := codex.Execute(ctx, currentInput, prompt)
		if err != nil {
			return codex.Result{}, err
		}
		if result.Outcome != "FAILED" || !codex.CapacityFailure(result.FailureCode) || retry == len(retryDelays) {
			return result, nil
		}
		currentInput.CodexSessionID = result.SessionID
		currentInput.CodexArchiveRelativePath = result.ArchiveRelativePath
		currentInput.CodexArchiveSHA256 = result.ArchiveSHA256
		currentInput.CodexArchiveProvenance = "codex-app-server-rollout-v1:" + input.ExecutionID + ":" +
			result.ArchiveRelativePath + ":" + result.ArchiveSHA256
		delay := retryDelays[retry]
		state.retries.WithLabelValues("capacity").Inc()
		if err := lease.progress(ctx, client, execution,
			controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_STATUS, uint32(retry+2),
			fmt.Sprintf("Провайдер временно перегружен; повтор через %s.", delay)); err != nil {
			return codex.Result{}, errors.New("publish capacity retry status")
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return codex.Result{}, context.Canceled
		case <-timer.C:
		}
	}
}

func verifyRuntimeFiles(input model.Input) error {
	paths := []string{inputPath, input.CredentialFiles.ControlPlaneGrant, input.CredentialFiles.MCPToken,
		input.CredentialFiles.MaterializationToken, input.CredentialFiles.CodexAuth,
		input.CredentialFiles.HandoffPrivateKey, input.ControlPlane.TLS.CAFile,
		input.ControlPlane.TLS.CertificateFile, input.ControlPlane.TLS.PrivateKeyFile,
		input.MCP.TLS.CAFile, input.MCP.TLS.CertificateFile, input.MCP.TLS.PrivateKeyFile,
		input.InteractionGateway.TLS.CAFile, input.InteractionGateway.TLS.CertificateFile,
		input.InteractionGateway.TLS.PrivateKeyFile}
	for _, path := range paths {
		if err := security.VerifyProtectedRegular(path, false); err != nil {
			return err
		}
	}
	return nil
}

func newHealth() *health {
	turns := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mattercodex_agent_runner_turns_total",
		Help: "Total agent-runner turn lifecycle outcomes."}, []string{"outcome"})
	retries := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "mattercodex_agent_runner_retries_total",
		Help: "Total bounded agent-runner retries."}, []string{"class"})
	prometheus.MustRegister(turns, retries)
	return &health{turns: turns, retries: retries}
}

func startHealthServer(state *health, failures chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.live.Load() {
			http.Error(writer, "not live", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.ready.Load() {
			http.Error(writer, "not ready", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: ":9090", Handler: mux, ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- errors.New("agent-runner health server failed")
		}
	}()
	return server
}
