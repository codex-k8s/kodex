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
	"slices"
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

func Run(baseContext, lifecycleContext context.Context, args []string, buildVersion string) (resultErr error) {
	if len(args) != 2 {
		return errors.New("agent-runner mode is required")
	}
	mode := args[1]
	if mode != "runtime-init-workspace" && mode != "runtime-session" && mode != "runtime-provider" {
		return errors.New("agent-runner mode is invalid")
	}
	if err := security.VerifyInvocation(args, mode); err != nil {
		return err
	}
	if mode == "runtime-provider" {
		return codex.ServeProviderBroker(lifecycleContext)
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
		if err := materialize.Run(lifecycleContext, input); err != nil {
			return err
		}
		return security.EnsureSharedWorkspaceDirectory(".matter-codex/state/codex-home")
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
	if err := security.EnsureWorkspaceDirectory(".matter-codex/recovery"); err != nil {
		return err
	}
	if err := materialize.Run(ctx, input); err != nil {
		return err
	}
	recovery, recovering, err := output.LoadRecovery(input)
	if err != nil {
		return errors.New("load runtime output recovery")
	}
	recoveryAuthorizationErr := output.AuthorizeRecovery(input, recovery, recovering)
	recoveryJournalUnavailable := errors.Is(recoveryAuthorizationErr, output.ErrRecoveryJournalUnavailable)
	if recoveryAuthorizationErr != nil && !recoveryJournalUnavailable {
		return recoveryAuthorizationErr
	}
	// Проверка выполняется до ClaimTurn, но terminal фиксируется только после
	// admission через signed handoff и owner transaction.
	providerAuthenticationErr := codex.ValidateProviderAuthentication(input)
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
	state.ready.Store(true)
	if err := client.Progress(ctx, lease,
		controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_STATUS, 1, "Выполнение хода начато."); err != nil {
		return errors.New("publish runtime start status")
	}
	if recovering {
		if err := client.Progress(ctx, lease,
			controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_PROGRESS, 1,
			"Восстанавливается доставка выходных артефактов без повторного запуска модели."); err != nil {
			return errors.New("publish runtime recovery progress")
		}
	} else if recoveryJournalUnavailable {
		if err := client.Progress(ctx, lease,
			controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_PROGRESS, 1,
			"Журнал повторной доставки недоступен; повторный запуск модели запрещён."); err != nil {
			return errors.New("publish runtime recovery unavailable progress")
		}
	} else if providerAuthenticationErr == nil {
		if err := client.Progress(ctx, lease,
			controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_PROGRESS, 1, "Codex выполняет ход."); err != nil {
			return errors.New("publish runtime progress")
		}
	}
	runContext, stopRun := context.WithCancel(ctx)
	defer stopRun()
	watchErrors := make(chan error, 1)
	watchDone := make(chan struct{})
	go watchExecution(runContext, stopRun, client, watchErrors, watchDone)
	defer func() {
		stopRun()
		<-watchDone
	}()
	result := codex.Result{Outcome: "FAILED", FailureCode: "authentication_required"}
	if recovering {
		result = codex.Result{Outcome: recovery.OriginalOutcome, FinalMessage: recovery.TerminalMarkdown,
			SessionID: recovery.CodexSessionID, ArchiveRelativePath: recovery.ArchiveRelativePath,
			ArchiveSHA256: recovery.ArchiveSHA256}
	} else if recoveryJournalUnavailable {
		result = codex.Result{Outcome: "FAILED",
			FinalMessage: "Доставка выходных артефактов не восстановлена: защищённый journal недоступен. Модель повторно не запускалась; восстановите retained workspace и повторите доставку.",
			SessionID:    input.CodexSessionID, ArchiveRelativePath: input.CodexArchiveRelativePath,
			ArchiveSHA256: input.CodexArchiveSHA256}
	} else if providerAuthenticationErr == nil {
		prompt, readErr := os.ReadFile(filepath.Join(input.WorkspaceRoot, input.PromptPath))
		if readErr != nil || len(prompt) == 0 || len(prompt) > 1<<20 || !utf8.Valid(prompt) {
			return errors.New("read immutable turn prompt")
		}
		result, err = executeWithCapacityRetry(runContext, input, prompt, mcpProxy.SocketPath(),
			mcpProxy.LocalBearerToken(), client, lease, state)
		if err != nil {
			return err
		}
	}
	select {
	case watchErr := <-watchErrors:
		return watchErr
	default:
	}
	execution, err = client.GetExecution(ctx)
	if err != nil {
		return err
	}
	terminalMarkdown := result.FinalMessage
	outcome := result.Outcome
	if recoveryJournalUnavailable {
		terminalMarkdown = result.FinalMessage
	} else if result.Outcome == "FAILED" {
		var nextAction string
		outcome, terminalMarkdown, nextAction = codex.TerminalPresentation(result.FailureCode)
		if nextAction == "REAUTH_DEVICE_CODE" {
			terminalMarkdown += "\n\nКоманда: `/agents openai auth " + input.ProviderAccountName + "`."
		}
	} else if terminalMarkdown == "" {
		terminalMarkdown = "Выполнение завершилось без итогового сообщения."
	}
	var built output.BuildResult
	if recovering {
		built, err = output.Resume(ctx, input, recovery)
	} else if recoveryJournalUnavailable {
		built, err = output.TerminalOnly(input, terminalMarkdown)
	} else {
		built, err = output.Build(ctx, input, terminalMarkdown, result.ArchivePath)
	}
	if err != nil || len(built.Outputs) == 0 {
		return errors.New("build terminal runtime outputs")
	}
	outputs := built.Outputs
	originalOutcome := outcome
	scheduledOutcome, err := output.ScheduledOutcome(input, outcome)
	if err != nil {
		return err
	}
	if recovering {
		originalOutcome = recovery.OriginalOutcome
		scheduledOutcome = recovery.ScheduledOutcome
	}
	if len(built.Failed) != 0 {
		archiveExecutionID := input.ExecutionID
		if recovering {
			archiveExecutionID = recovery.ArchiveExecutionID
		}
		journal := output.RecoveryJournal{TurnID: input.TurnID, SourceExecutionID: input.ExecutionID,
			ArchiveExecutionID: archiveExecutionID, SourceAttempt: input.Attempt,
			OriginalOutcome: originalOutcome, ScheduledOutcome: scheduledOutcome,
			TerminalMarkdown: string(outputs[0].Payload),
			CodexSessionID:   result.SessionID, ArchiveRelativePath: result.ArchiveRelativePath,
			ArchiveSHA256: result.ArchiveSHA256, Existing: slices.Clone(outputs[1:]), Failed: built.Failed}
		if err := output.SaveRecovery(input, journal); err != nil {
			return errors.New("save runtime output recovery")
		}
		outcome = "FAILED"
	}
	terminalDigest := sha256.Sum256([]byte(outcome + "\x00" + result.SessionID + "\x00" +
		result.ArchiveSHA256 + "\x00" + outputs[0].SHA256))
	terminalReference := "codex://sessions/" + result.SessionID + "/executions/" + input.ExecutionID
	if len(built.Failed) != 0 {
		terminalReference += "/delivery-recovery"
	} else if recoveryJournalUnavailable {
		terminalReference += "/delivery-recovery/source/" + input.CodexDeliveryRecoverySourceExecutionID
	}
	archiveExecutionID := input.ExecutionID
	if recovering {
		archiveExecutionID = recovery.ArchiveExecutionID
	}
	archiveProvenance := "codex-app-server-rollout-v1:" + archiveExecutionID + ":" + result.ArchiveRelativePath + ":" + result.ArchiveSHA256
	if recoveryJournalUnavailable {
		archiveProvenance = input.CodexArchiveProvenance
	}
	if result.SessionID == "" && outcome == "BLOCKED" {
		terminalReference = "preflight://provider-auth/" + input.ExecutionID
		archiveProvenance = ""
	}
	handoffValue := runtimecontract.HandoffV2{Schema: runtimecontract.HandoffSchemaV2,
		ExecutionID: input.ExecutionID, ExecutionVersion: execution.Version, Fence: execution.Fence,
		GrantGeneration: input.GrantGeneration, RuntimeRevisionSHA256: input.RuntimeRevisionSHA256,
		EffectiveRuntimeSHA256: input.EffectiveRuntimeSHA256, ImmutableInputSHA256: input.ImmutableInputSHA256,
		SessionID: input.SessionID, TurnID: input.TurnID,
		ScheduleOccurrenceID: input.ScheduleOccurrenceID, Attempt: input.Attempt,
		ProviderBindingID: input.ProviderBindingID, ProviderBindingVersion: input.ProviderBindingVersion,
		ProviderBindingSHA256: input.ProviderBindingSHA256, Outcome: outcome,
		ScheduledOutcome:  scheduledOutcome,
		TerminalReference: terminalReference,
		TerminalSHA256:    hex.EncodeToString(terminalDigest[:]), Outputs: outputs, CodexSessionID: result.SessionID,
		ArchiveRelativePath: result.ArchiveRelativePath,
		ArchiveSHA256:       result.ArchiveSHA256,
		ArchiveProvenance:   archiveProvenance,
		ObservedAt:          time.Now().UTC()}
	state.ready.Store(false)
	publishErr := handoff.Publish(ctx, input, handoffValue)
	if publishErr == nil && recovering && len(built.Failed) == 0 {
		if err := output.ClearRecovery(input); err != nil {
			return err
		}
	}
	return publishErr
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

func watchExecution(ctx context.Context, cancel context.CancelFunc, client *controlplane.Client,
	failures chan<- error, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			execution, err := client.GetExecution(ctx)
			if err != nil {
				continue
			}
			switch execution.State {
			case controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_CANCELLED,
				controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_EXPIRED,
				controlplanev1.RuntimeExecutionState_RUNTIME_EXECUTION_STATE_RETRIED:
				select {
				case failures <- errors.New("runtime execution was closed by owner"):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func executeWithCapacityRetry(ctx context.Context, input model.Input, prompt []byte, mcpSocket, mcpProxyToken string,
	client *controlplane.Client, lease controlplane.TurnLease, state *health,
) (codex.Result, error) {
	currentInput := input
	for retry := 0; ; retry++ {
		result, err := codex.ExecuteViaBroker(ctx, currentInput, prompt, mcpSocket, mcpProxyToken)
		if err != nil {
			if errors.Is(err, codex.ErrProviderAuthentication) {
				return codex.Result{Outcome: "FAILED", FailureCode: "authentication_required"}, nil
			}
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
		if err := client.Progress(ctx, lease,
			controlplanev1.RuntimeProgressKind_RUNTIME_PROGRESS_KIND_STATUS, uint32(retry+2),
			fmt.Sprintf("Временная нехватка мощности провайдера; повтор через %s.", delay)); err != nil {
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
		input.CredentialFiles.MaterializationToken,
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
