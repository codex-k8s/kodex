// Package app composes runner modes and executes immutable server-owned turns, including serial warm-assistant consumption, without owning execution grants or terminal domain state.
// Пакет app собирает режимы runner и исполняет immutable серверные ходы, включая последовательную warm-очередь помощника, не владея execution grants и terminal domain state.
package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/imageinventory"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/readiness"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
	"golang.org/x/sys/unix"
)

// mcpStartupFailureMessage fixes the startup warning text; only the separately classified stage is logged, never the MCP endpoint or raw error payload.
// mcpStartupFailureMessage закрепляет текст startup warning; журналируется лишь отдельно классифицированный этап, не MCP endpoint и не сырой пакет ошибки.
const mcpStartupFailureMessage = "Runtime MCP startup failed"

// inputPath is the fixed mounted immutable runtime input path used by runner modes, not a caller-selected file or an authority grant by itself.
// inputPath — фиксированный mount-путь immutable runtime-входа для режимов runner, а не выбранный клиентом файл или самостоятельный grant.
const inputPath = "/var/run/config/kodex/runtime/runtime.json"

// attachmentSetReadme fixes the exact non-secret directory guidance shared by manifest publication and readback, preventing their bytes from drifting independently.
// attachmentSetReadme закрепляет точную несекретную инструкцию каталога для публикации и сверки manifest, не позволяя их байтам расходиться независимо.
const attachmentSetReadme = "This directory contains a read-only, server-owned AttachmentSet. Read manifest.json before using files.\n"

// health holds atomic liveness/readiness flags and the latest workspace-probe snapshot alongside immutable input; a ready flag alone does not prove fresh filesystem readiness.
// health хранит atomic liveness/readiness и последний workspace-probe snapshot рядом с immutable-входом; один ready flag не доказывает свежую файловую готовность.
type health struct {
	live, ready atomic.Bool
	input       model.Input
	workspace   atomic.Pointer[workspaceHealth]
}

// Run dispatches fixed inventory, workspace, provider, relay and turn/warm modes, validating the relevant invocation, input and UID boundaries before their work. Session modes compose telemetry, health monitoring and callbacks; warm mode serially consumes server-owned turns. Owned clients/monitor/server and independent bounded telemetry shutdown use caller-provided lifecycle/base contexts, without creating root authority.
// Run выбирает фиксированные режимы inventory, workspace, provider, relay и turn/warm, проверяя соответствующие invocation, input и UID boundaries до работы. Session-режимы собирают telemetry, health monitoring и callbacks; warm последовательно получает серверные ходы. Свои clients/monitor/server и независимый ограниченный telemetry shutdown используют переданные lifecycle/base contexts без создания корневых полномочий.
func Run(baseContext, lifecycleContext context.Context, args []string, buildVersion string) (resultErr error) {
	if len(args) > 1 && args[1] == imageinventory.SandboxExecMode {
		return imageinventory.RunSandboxedTool(args[2:])
	}
	if len(args) > 1 && args[1] == imageinventory.Mode {
		return imageinventory.Run(lifecycleContext, args)
	}
	if len(args) != 2 {
		return errors.New("agent-runner mode is required")
	}
	mode := args[1]
	if mode != "runtime-prepare-workspace" && mode != "runtime-init-workspace" && mode != "runtime-session" && mode != "runtime-warm" && mode != "runtime-provider" && mode != "runtime-provider-credential-relay" && mode != workspaceCanaryMode {
		return errors.New("agent-runner mode is invalid")
	}
	if err := security.VerifyInvocation(args, mode); err != nil {
		return err
	}
	if mode == "runtime-prepare-workspace" {
		if os.Geteuid() != 10001 {
			return errors.New("agent-runner runtime UID is invalid")
		}
		return security.EnsureSharedWorkspaceDirectory(".kodex")
	}
	if mode == "runtime-provider" {
		return codex.ServeProviderBroker(lifecycleContext)
	}
	input, err := model.DecodeInput(inputPath)
	if err != nil {
		return err
	}
	if mode == workspaceCanaryMode {
		err := runWorkspaceCanary(lifecycleContext, input)
		result := "OK"
		if err != nil {
			result = workspacepolicy.DenialReason(err)
		}
		_, writeErr := io.WriteString(os.Stdout, result)
		return writeErr
	}
	if mode == "runtime-provider-credential-relay" {
		return credentialrelay.Serve(lifecycleContext, input)
	}
	if mode == "runtime-init-workspace" {
		if err := materializeRuntimeEgressTrust(); err != nil {
			return err
		}
		snapshot, err := input.RequiredContextSnapshot(time.Now())
		if err != nil {
			return err
		}
		if err := materializeWorkspace(lifecycleContext, input); err != nil {
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
	telemetry, err := sharedobservability.NewRuntimeFromEnv(startup, "agent-runner", buildVersion)
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
	logger := telemetry.Logger(os.Stderr)
	state := &health{input: input}
	state.live.Store(true)
	stopWorkspaceMonitor := startWorkspaceMonitor(lifecycleContext, state, checkWorkspaceProcess)
	defer stopWorkspaceMonitor()
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
	runtime := turnRuntime{prepare: func(ctx context.Context, input model.Input, client *callback.Client) (preparedTurn, string, error) {
		prepared, code, err := prepareRuntimeTurn(ctx, input, client)
		if code == "RUNTIME_MCP_UNAVAILABLE" && err != nil {
			logMCPStartupFailure(ctx, logger, err)
		}
		return prepared, code, err
	}, execute: codex.ExecuteViaBroker, checkWorkspace: checkWorkspaceProcess, logger: logger}
	if mode == "runtime-session" {
		resultErr = runTurn(lifecycleContext, input, client, func() { state.ready.Store(true) }, runtime)
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
			if err := runTurn(lifecycleContext, turn, client, nil, runtime); err != nil {
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

// turnMCPProxy exposes only the prepared turn's local MCP endpoint, local bearer and bounded close operation, allowing unit tests to replace process plumbing without bypassing callback authority.
// turnMCPProxy предоставляет лишь локальный MCP endpoint подготовленного хода, local bearer и ограниченное закрытие, позволяя unit-тестам заменить процессную обвязку без обхода callback authority.
type turnMCPProxy interface {
	// SocketPath returns the prepared local Unix endpoint used by the provider broker, not a remotely supplied tool destination.
	// SocketPath возвращает подготовленный локальный Unix endpoint provider broker, не удалённый tool destination клиента.
	SocketPath() string
	// LocalBearerToken returns the local proxy capability; it is distinct from the controller execution ticket and must not be logged.
	// LocalBearerToken возвращает локальную capability прокси; она отличается от execution ticket controller и не должна журналироваться.
	LocalBearerToken() string
	// Close releases the prepared proxy under the caller's shutdown context; the turn orchestrator supplies an independent bounded context even after cancellation.
	// Close освобождает подготовленный прокси в shutdown-контексте вызывающего; orchestrator передаёт независимый ограниченный контекст даже после отмены.
	Close(context.Context) error
}

// preparedTurn owns the effective snapshot/deadline context, MCP proxy and combined cancellation for one turn; runTurn releases the proxy before cancelling that prepared context.
// preparedTurn владеет действующим snapshot/deadline context, MCP proxy и объединённой отменой одного хода; runTurn освобождает прокси до отмены подготовленного контекста.
type preparedTurn struct {
	ctx    context.Context
	proxy  turnMCPProxy
	cancel context.CancelFunc
}

// turnRuntime injects preparation, broker execution, workspace probing and a runtime logger into turn orchestration; tests can substitute these seams without spawning an actual provider.
// turnRuntime передаёт preparation, broker execution, workspace probing и runtime logger в orchestration хода; тесты могут заменить эти зависимости без настоящего провайдера.
type turnRuntime struct {
	prepare        func(context.Context, model.Input, *callback.Client) (preparedTurn, string, error)
	execute        func(context.Context, model.Input, []byte, string, string, func(runtimecontract.RuntimeActivity) error) (codex.Result, error)
	checkWorkspace func(context.Context) error
	logger         *slog.Logger
}

// prepareRuntimeTurn validates a turn and its mounted context snapshot, binds snapshot expiry and the effective deadline, prepares workspace/input/profile, resets outbox and starts the exact MCP proxy. Failures cancel prepared contexts and return an existing safe failure code where applicable; the input remains server-owned authority.
// prepareRuntimeTurn проверяет ход и смонтированный context snapshot, связывает snapshot expiry и действующий deadline, готовит workspace/input/profile, очищает outbox и запускает точный MCP proxy. Ошибки отменяют подготовленные контексты и по возможности возвращают прежний безопасный код; authority входа остаётся серверной.
func prepareRuntimeTurn(ctx context.Context, input model.Input, client *callback.Client) (prepared preparedTurn, code string, resultErr error) {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil {
		return preparedTurn{}, "", errors.New("runtime turn input is invalid")
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil || contextfiles.Verify(input, snapshot) != nil {
		return preparedTurn{}, "RUNTIME_INPUT_INVALID", errors.New("runtime context is invalid")
	}
	ctx, cancelContext := snapshot.BoundExecutionContext(ctx)
	deadlineContext, cancelDeadline := input.BoundExecutionDeadline(ctx, 0)
	previousCancel := cancelContext
	ctx = deadlineContext
	cancelContext = func() { cancelDeadline(); previousCancel() }
	defer func() {
		if resultErr != nil {
			cancelContext()
		}
	}()
	if input.ExecutionDeadline != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return preparedTurn{}, "RUNTIME_TIMEOUT", ctx.Err()
	}
	if err := materializeWorkspace(ctx, input); err != nil {
		return preparedTurn{}, "RUNTIME_WORKSPACE_INVALID", err
	}
	if err := verifyInputArtifacts(input); err != nil {
		return preparedTurn{}, "RUNTIME_INPUT_INVALID", err
	}
	if err := codex.ValidateRuntimeProfile(input); err != nil {
		return preparedTurn{}, runtimeExecutionFailureCode(err), err
	}
	if err := resetWorkspaceDirectory(ctx, input.WorkspaceRoot, ".kodex/outbox"); err != nil {
		return preparedTurn{}, "RUNTIME_WORKSPACE_INVALID", err
	}
	mcpProxy, err := readiness.StartMCPProxy(ctx, input, client.Token(), codex.RequiredMCPToolNames(input))
	if err != nil {
		return preparedTurn{}, "RUNTIME_MCP_UNAVAILABLE", err
	}
	return preparedTurn{ctx: ctx, proxy: mcpProxy, cancel: cancelContext}, "", nil
}

// runTurn prepares once, reports readiness/progress, builds the prompt and executes the broker once while validating and delivering typed activity. It preserves measured usage on execution/deadline/activity failures and independently closes the proxy; a nil return means callback delivery succeeded, not necessarily that the model task succeeded.
// runTurn один раз готовит ход, сообщает readiness/progress, строит prompt и однократно выполняет broker с проверкой и доставкой typed activity. Сохраняет измеренный расход при execution/deadline/activity failures и независимо закрывает прокси; nil означает успешную доставку callback, не обязательно успех задачи модели.
func runTurn(ctx context.Context, input model.Input, client *callback.Client, workingPathReady func(), runtime turnRuntime) error {
	prepared, code, err := runtime.prepare(ctx, input, client)
	if err != nil {
		if code == "" {
			return err
		}
		return completeFailure(ctx, input, client, code)
	}
	ctx = prepared.ctx
	defer prepared.cancel()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = prepared.proxy.Close(shutdown)
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
	activityFailed := false
	result, executionErr := runtime.execute(ctx, input, prompt, prepared.proxy.SocketPath(), prepared.proxy.LocalBearerToken(), func(activity runtimecontract.RuntimeActivity) error {
		if err := activity.Validate(); err != nil {
			activityFailed = true
			return errors.New("runtime activity is invalid")
		}
		var err error
		if activity.ProviderProcess != nil {
			err = client.ProviderProcess(ctx, input, *activity.ProviderProcess)
		} else if activity.Financial != nil {
			err = client.FinancialObservation(ctx, input, *activity.Financial)
		} else if activity.Message != nil {
			err = client.PublishedMessage(ctx, input, *activity.Message)
		} else {
			err = client.RecordNativeToolCall(ctx, input, *activity.ToolCall)
		}
		activityFailed = activityFailed || err != nil
		return err
	})
	if executionErr != nil {
		logRuntimeExecutionFailure(ctx, runtime.logger, executionErr, activityFailed)
	}
	if input.ExecutionDeadline != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return completeMeasuredFailure(ctx, input, client, result, "RUNTIME_TIMEOUT")
	}
	if activityFailed {
		return completeMeasuredFailure(ctx, input, client, result, "RUNTIME_UNAVAILABLE")
	}
	if executionErr != nil {
		code := runtimeExecutionFailureCode(executionErr)
		if input.ExecutionDeadline != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "RUNTIME_TIMEOUT"
		}
		return completeMeasuredFailure(ctx, input, client, result, code)
	}
	return completeExecutedTurn(ctx, input, client, result, runtime.checkWorkspace, runtime.logger)
}

// completeMeasuredFailure sends a failed terminal payload with measured usage and safe provider diagnostics; archive pins are included only when the result's private capture proof verifies against this input. Delivery detaches from cancellation without rerunning the provider.
// completeMeasuredFailure отправляет failed terminal payload с измеренным usage и безопасной provider-диагностикой; archive pins включаются лишь при проверке private capture proof результата против данного входа. Доставка отделена от отмены без повторного провайдера.
func completeMeasuredFailure(ctx context.Context, input model.Input, client *callback.Client, result codex.Result, code string) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt,
		Success: false, SafeErrorCode: safeFailureCode(code), ResultSummary: "i18n:" + code, Usage: result.Usage}
	payload.ProviderDiagnostic = result.ProviderDiagnostic(input)
	if result.HasVerifiedRollout(input) {
		payload.CodexSessionID, payload.ArchiveRelativePath, payload.ArchiveSHA256, payload.ArchiveSizeBytes = result.SessionID, result.ArchiveRelativePath, result.ArchiveSHA256, result.ArchiveSizeBytes
	}
	return client.Complete(context.WithoutCancel(ctx), input, payload)
}

// completeExecutedTurn checks workspace after execution, handles terminal failures, validates a successful final message, optionally publishes workspace provenance and collects allowed artifacts before completion. Every later failure retains measured usage through the failure path; success payload validation precedes the callback and does not grant new artifact access.
// completeExecutedTurn проверяет workspace после execute, обрабатывает terminal failures, проверяет успешный final message, при необходимости публикует workspace provenance и собирает разрешённые артефакты до completion. Последующие отказы сохраняют измеренный usage через failure path; success payload проверяется до callback без новых прав на артефакты.
func completeExecutedTurn(ctx context.Context, input model.Input, client *callback.Client, result codex.Result, checkWorkspace func(context.Context) error, logger *slog.Logger) error {
	if err := checkWorkspace(ctx); err != nil {
		logWorkspaceCompletionDenial(ctx, logger, workspaceCompletionCheck, err)
		if result.ProviderDiagnostic(input) == nil {
			result = result.WithWorkspaceDiagnostic(input, workspaceCompletionCheck, workspaceCompletionDenialReason(err))
		}
		return completeExecutedFailure(ctx, input, client, result, "RUNTIME_WORKSPACE_INVALID", "i18n:RUNTIME_WORKSPACE_INVALID")
	}
	if result.Outcome != "SUCCEEDED" {
		_, message, _ := codex.TerminalPresentation(result.FailureCode)
		if result.FailureCode == "RUNTIME_ARTIFACT_INVALID" {
			message = "i18n:RUNTIME_ARTIFACT_INVALID"
		}
		return completeResultFailure(ctx, input, client, result, message)
	}
	if strings.TrimSpace(result.FinalMessage) == "" || len(result.FinalMessage) > 64<<10 || !utf8.ValidString(result.FinalMessage) {
		return completeExecutedFailure(ctx, input, client, result, "RUNTIME_RESULT_INVALID", "i18n:RUNTIME_RESULT_INVALID")
	}
	if input.CodexSandbox == "workspace-write" && hasCapability(input, runtimecontract.ArtifactCapability) {
		if err := workspacepolicy.PublishResult(ctx, input.WorkspaceRoot, input.WorkspacePolicy, workspacepolicy.ResultProvenance{
			Schema: "kodex.workspace-write-result.v1", RuntimeRevisionRef: input.RuntimeRevisionRef,
			RuntimeRevisionVersion: input.RuntimeRevisionVersion, RuntimeRevisionDigest: input.RuntimeRevisionDigest,
			Attempt: input.Attempt, ExecutionBindingDigest: input.ExecutionBindingDigest,
		}); err != nil {
			logWorkspaceCompletionDenial(ctx, logger, workspaceCompletionPublish, err)
			result = result.WithWorkspaceDiagnostic(input, workspaceCompletionPublish, workspaceCompletionDenialReason(err))
			return completeExecutedFailure(ctx, input, client, result, "RUNTIME_WORKSPACE_INVALID", "i18n:RUNTIME_WORKSPACE_INVALID")
		}
	}
	artifacts, err := completionArtifacts(input, result.FinalMessage)
	if err != nil {
		logWorkspaceCompletionDenial(ctx, logger, workspaceCompletionPublish, err)
		result = result.WithWorkspaceDiagnostic(input, workspaceCompletionPublish, workspaceCompletionDenialReason(err))
		return completeExecutedFailure(ctx, input, client, result, "RUNTIME_ARTIFACT_INVALID", "i18n:RUNTIME_ARTIFACT_INVALID")
	}
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: true, ResultSummary: result.FinalMessage, Usage: result.Usage, Artifacts: artifacts, CodexSessionID: result.SessionID, ArchiveRelativePath: result.ArchiveRelativePath, ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	if payload.Validate() != nil {
		return completeExecutedFailure(ctx, input, client, result, "RUNTIME_RESULT_INVALID", "i18n:RUNTIME_RESULT_INVALID")
	}
	return client.Complete(ctx, input, payload)
}

// completeExecutedFailure handles a completed execute result that later failed publication, mapping workspace failure to the existing artifact code and retaining a valid terminal archive tuple. Invalid outcome/payload falls back to a tuple-free failure with the same usage; this path is not the private-proof test used for arbitrary execution errors.
// completeExecutedFailure обрабатывает завершённый execute с последующим отказом публикации, переводя workspace failure в прежний artifact code и сохраняя корректный terminal archive tuple. Неверные outcome/payload дают отказ без tuple с тем же usage; это не private-proof проверка произвольных execution errors.
func completeExecutedFailure(ctx context.Context, input model.Input, client *callback.Client, result codex.Result, code, summary string) error {
	// Отказ сохранения после execute не означает ошибку пользовательского ввода.
	// Canary остаётся обязательным; наружу выдаётся существующий закрытый код.
	if code == "RUNTIME_WORKSPACE_INVALID" {
		code, summary = "RUNTIME_ARTIFACT_INVALID", "i18n:RUNTIME_ARTIFACT_INVALID"
	}
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt,
		Success: false, ResultSummary: summary, SafeErrorCode: safeFailureCode(code), Usage: result.Usage,
		CodexSessionID: result.SessionID, ArchiveRelativePath: result.ArchiveRelativePath,
		ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	payload.ProviderDiagnostic = result.ProviderDiagnostic(input)
	if (result.Outcome != "SUCCEEDED" && result.Outcome != "FAILED") || payload.Validate() != nil {
		return completeFailureWithSummaryAndUsage(ctx, input, client, code, summary, result.Usage)
	}
	return client.Complete(context.WithoutCancel(ctx), input, payload)
}

// completeResultFailure projects an executed failed result into the terminal callback with normalized failure, measured usage, archive metadata and safe diagnostic provenance; callback validation still rejects an invalid tuple or usage.
// completeResultFailure проецирует выполненный failed result в terminal callback с нормализованным отказом, измеренным usage, archive metadata и безопасной diagnostic provenance; callback validation отклоняет неверные tuple или usage.
func completeResultFailure(ctx context.Context, input model.Input, client *callback.Client, result codex.Result, summary string) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: false, ResultSummary: summary,
		SafeErrorCode: safeFailureCode(result.FailureCode), Usage: result.Usage, CodexSessionID: result.SessionID,
		ArchiveRelativePath: result.ArchiveRelativePath, ArchiveSHA256: result.ArchiveSHA256, ArchiveSizeBytes: result.ArchiveSizeBytes}
	payload.ProviderDiagnostic = result.ProviderDiagnostic(input)
	return client.Complete(context.WithoutCancel(ctx), input, payload)
}

// completionArtifacts returns no project artifacts for assistant scopes or missing artifact capability; otherwise it delegates outbox collection without enlarging capabilities.
// completionArtifacts не создаёт проектных артефактов для областей помощника или без artifact capability; иначе передаёт сбор outbox без расширения capabilities.
func completionArtifacts(input model.Input, finalMessage string) ([]runtimecontract.RunnerArtifact, error) {
	if input.IsAssistant() || !hasCapability(input, runtimecontract.ArtifactCapability) {
		return nil, nil
	}
	return collectArtifacts(input, finalMessage)
}

// hasCapability checks exact membership in the input's already materialized capability list; matching a string is not independent server authorization.
// hasCapability проверяет точное вхождение в уже материализованный список capabilities входа; совпадение строки не является самостоятельной серверной авторизацией.
func hasCapability(input model.Input, expected string) bool {
	for _, capability := range input.Capabilities {
		if capability == expected {
			return true
		}
	}
	return false
}

// completeFailure derives the existing i18n summary for a pre-execution failure and delegates terminal delivery with zero measured usage.
// completeFailure строит прежнюю i18n-сводку отказа до исполнения и передаёт terminal delivery с нулевым измеренным usage.
func completeFailure(ctx context.Context, input model.Input, client *callback.Client, code string) error {
	return completeFailureWithSummary(ctx, input, client, code, "i18n:"+code)
}

// completeFailureWithSummary retains a caller-supplied failure summary but explicitly supplies zero usage for the generic failure path.
// completeFailureWithSummary сохраняет переданную сводку отказа, но явно задаёт нулевой usage для generic failure path.
func completeFailureWithSummary(ctx context.Context, input model.Input, client *callback.Client, code, summary string) error {
	return completeFailureWithSummaryAndUsage(ctx, input, client, code, summary, runtimecontract.TokenUsage{})
}

// completeFailureWithSummaryAndUsage builds a tuple-free failed completion with normalized code and supplied usage, then delivers it independently of cancellation. Nil reports delivery success only; it never turns the failed payload into successful execution.
// completeFailureWithSummaryAndUsage строит failed completion без tuple с нормализованным кодом и переданным usage, доставляя независимо от отмены. Nil сообщает лишь успех доставки; failed payload не превращается в успешное исполнение.
func completeFailureWithSummaryAndUsage(ctx context.Context, input model.Input, client *callback.Client, code, summary string, usage runtimecontract.TokenUsage) error {
	payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: false, ResultSummary: summary, SafeErrorCode: safeFailureCode(code), Usage: usage}
	if err := client.Complete(context.WithoutCancel(ctx), input, payload); err != nil {
		return err
	}
	return nil
}

// safeFailureCode maps the closed provider/runtime failure vocabulary to existing public terminal codes and collapses every unknown input to RUNTIME_UNAVAILABLE without copying diagnostics.
// safeFailureCode переводит закрытый словарь provider/runtime failures в прежние публичные terminal codes, заменяя неизвестное на RUNTIME_UNAVAILABLE без копирования диагностики.
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
	case "RUNTIME_ARTIFACT_INVALID":
		return "RUNTIME_ARTIFACT_INVALID"
	case "provider_error_info_invalid", "provider_interrupted", "provider_other_error", "RUNTIME_RESULT_INVALID":
		return "PROVIDER_RESPONSE_INVALID"
	case "RUNTIME_INPUT_INVALID", "RUNTIME_WORKSPACE_INVALID":
		return "RUNTIME_INPUT_INVALID"
	case "RUNTIME_MCP_UNAVAILABLE":
		return "RUNTIME_MCP_UNAVAILABLE"
	case "RUNTIME_TIMEOUT":
		return "RUNTIME_TIMEOUT"
	default:
		return "RUNTIME_UNAVAILABLE"
	}
}

// runtimeExecutionFailureCode classifies wrapped context-file, authentication, unsupported-authority, required-MCP and profile sentinels in fixed order; unclassified execution errors become provider-unavailable rather than exposing their text.
// runtimeExecutionFailureCode классифицирует обёрнутые sentinels context-file, authentication, unsupported-authority, required-MCP и profile в фиксированном порядке; остальные execution errors становятся provider-unavailable без раскрытия текста.
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

// materializeWorkspace creates the fixed service layout, probes readiness, validates materialized instructions and holds the cooperative workspace lock while publishing AGENTS.md and inbox prompt. Warm mode writes only the inert wait prompt; caller-prepared mounts and policy remain the filesystem authority boundary.
// materializeWorkspace создаёт фиксированную служебную структуру, проверяет готовность и materialized instructions и под согласующим workspace lock публикует AGENTS.md и inbox prompt. Warm пишет лишь неактивное ожидание; подготовленные вызывающим mounts и policy остаются файловой authority boundary.
func materializeWorkspace(ctx context.Context, input model.Input) error {
	if err := materializeWorkspaceDirectories(); err != nil {
		return err
	}
	if err := checkWorkspaceProcess(ctx); err != nil {
		return err
	}
	if err := validateMaterializedInstructions(input); err != nil {
		return err
	}
	lock, err := workspacepolicy.Lock(ctx, input.WorkspaceRoot)
	if err != nil {
		return err
	}
	defer lock.Close()
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

// materializeWorkspaceDirectories creates the fixed shared runtime directory list through the security helper, stopping on the first error and never accepting arbitrary directory names.
// materializeWorkspaceDirectories создаёт фиксированный список общих runtime-каталогов через security helper, останавливаясь при первой ошибке и не принимая произвольных имён.
func materializeWorkspaceDirectories() error {
	for _, relative := range []string{".kodex", ".kodex/inbox", ".kodex/outbox", ".kodex/state", ".kodex/state/codex-home", "input", "session", "knowledge", "work"} {
		if err := security.EnsureSharedWorkspaceDirectory(relative); err != nil {
			return err
		}
	}
	return nil
}

// buildPrompt accepts only turn mode, validates versioned prompt provenance when present and appends validated session history plus the exact current task. A missing current notice triggers the existing continuation revision path; the final UTF-8 prompt is bounded to one MiB without issuing any execution grant.
// buildPrompt принимает лишь turn mode, проверяет присутствующую versioned prompt provenance и добавляет проверенную историю и точную текущую задачу. При отсутствии текущей notice использует прежний continuation revision path; итоговый UTF-8 prompt ограничен одним MiB без выдачи grant исполнения.
func buildPrompt(input model.Input) ([]byte, error) {
	if input.Mode != runtimecontract.RunnerModeTurn {
		return nil, errors.New("turn prompt is unavailable")
	}
	if input.PromptServiceTemplateRevision != "" || input.PromptServiceTemplateDigest != "" || input.PromptTargetKind != "" {
		if err := validateMaterializedInstructions(input); err != nil {
			return nil, err
		}
	}
	var builder strings.Builder
	continuationNotice, err := appendSessionContext(&builder, input)
	if err != nil {
		return nil, err
	}
	builder.WriteString(input.Task)
	if !continuationNotice && (input.CodexSessionID != "" || input.PromptTargetKind == "SESSION_CONTINUATION" ||
		(input.PromptServiceTemplateRevision == "" && strings.Contains(input.Instructions, `<session-continuation used="true">`))) {
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

// appendContinuationRevision validates reasoning configuration and appends exact revision/config/environment pins with sorted tool/grant/file descriptions. Versioned prompts include only PLATFORM sections; the existing unversioned path extracts its fixed service blocks. Textual context describes approved state, not newly granted capabilities.
// appendContinuationRevision проверяет reasoning configuration и добавляет точные revision/config/environment pins с сортированными tool/grant/file descriptions. Versioned prompts включают лишь PLATFORM sections; прежний unversioned path извлекает фиксированные service blocks. Текст описывает одобренное состояние, а не выдаёт capabilities.
func appendContinuationRevision(builder *strings.Builder, input model.Input) error {
	if runtimecontract.ValidateEffectiveReasoningEffort(input.ConfigOverlay, input.EffectiveReasoningEffort, input.ReasoningMode) != nil {
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
	builder.WriteString("model=" + input.Model + "\nreasoning=" + input.EffectiveReasoningEffort + "\n")
	builder.WriteString("reasoning-mode=" + input.ReasoningMode + "\n")
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
	if input.PromptServiceTemplateRevision != "" {
		envelope, err := runtimecontract.DecodePromptService(input)
		if err != nil {
			return err
		}
		sections := envelope.Sections[:0]
		for _, section := range envelope.Sections {
			if section.Source == "PLATFORM" {
				sections = append(sections, section)
			}
		}
		envelope.Sections = sections
		raw, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		builder.Write(raw)
		builder.WriteByte('\n')
		return nil
	}
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

// validateMaterializedInstructions delegates versioned envelopes to the strict contract decoder; otherwise it requires bounded UTF-8 resolved text, exactly one of each fixed block, at most one used launch kind and an exact capability block. Resumed sessions require the existing continuation block; these shape checks do not manufacture trusted provenance.
// validateMaterializedInstructions передаёт versioned envelopes строгому contract decoder; иначе требует ограниченный UTF-8 resolved text, по одному фиксированному блоку, не более одного used launch kind и точный capability block. Resumed sessions требуют прежний continuation block; проверки формы не создают доверенную provenance.
func validateMaterializedInstructions(input model.Input) error {
	if input.PromptServiceTemplateRevision != "" {
		_, err := runtimecontract.DecodePromptService(input)
		return err
	}
	if input.PromptServiceTemplateDigest != "" || input.PromptTargetKind != "" {
		return errors.New("runtime prompt service provenance is invalid")
	}
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

// materializedServiceBlock extracts one fixed-name used=true/false block with exactly one closing tag using literal counts and offsets, not an XML parser or general user-content sanitizer; callers supply the closed block-name set.
// materializedServiceBlock извлекает один fixed-name used=true/false block с ровно одним closing tag по literal counts и offsets; это не XML parser и не очиститель произвольного user-content, имена задаются закрытым набором вызывающего.
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

// scopedArtifacts creates a separate filtered slice for exact scope and sorts it by declared position without mutating the caller's slice order or deciding attachment eligibility.
// scopedArtifacts создаёт отдельный фильтрованный срез точной scope и сортирует по заявленной позиции без изменения порядка среза вызывающего и без решения attachment eligibility.
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

// materializeInputArtifacts verifies canonical catalogs before resetting input/knowledge/session
// trees, retrieves exact artifact bytes through the callback, writes manifests and protects read-only
// trees; a catalog failure precedes those effects and this function grants no additional access.
// materializeInputArtifacts проверяет канонические каталоги до очистки деревьев input/knowledge/session,
// получает точные байты через callback, записывает манифесты и защищает деревья только для чтения;
// ошибка каталога предшествует этим эффектам, дополнительных прав функция не выдаёт.
func materializeInputArtifacts(ctx context.Context, input model.Input, client *callback.Client) error {
	setManifests, workspaceManifest, err := buildInputManifests(input)
	if err != nil {
		return err
	}
	if err := resetWorkspaceDirectory(ctx, input.WorkspaceRoot, "input"); err != nil {
		return err
	}
	if err := resetWorkspaceDirectory(ctx, input.WorkspaceRoot, "knowledge"); err != nil {
		return err
	}
	if err := resetWorkspaceDirectory(ctx, input.WorkspaceRoot, "session"); err != nil {
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

// verifyInputArtifacts rebuilds the expected catalogs before checking exact on-disk manifests,
// README files and artifact digests inside the workspace; it performs readback only and never repairs
// mismatched files or accepts persisted bytes as a replacement for the runtime input authority.
// verifyInputArtifacts восстанавливает ожидаемые каталоги до сверки точных манифестов на диске,
// README и хешей файлов внутри workspace; выполняет только чтение, не исправляет несовпадения
// и не заменяет полномочия runtime-входа сохранёнными байтами.
func verifyInputArtifacts(input model.Input) error {
	sets, workspaceManifest, err := buildInputManifests(input)
	if err != nil {
		return err
	}
	if err := verifyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", "manifest.json"), workspaceManifest.Bytes, int64(len(workspaceManifest.Bytes))); err != nil {
		return err
	}
	for _, set := range input.AttachmentSets {
		manifest := sets[set.Ref]
		if err := verifyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", set.Ref, "manifest.json"), manifest.Bytes, int64(len(manifest.Bytes))); err != nil {
			return err
		}
		readme := []byte(attachmentSetReadme)
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

// verifyWorkspaceFile opens a guarded regular input, requires exact size and mode 0440 and compares bounded bytes in constant time; it never repairs mismatched files or accepts their contents as new authority.
// verifyWorkspaceFile открывает защищённый обычный input, требует точные размер и режим 0440 и сравнивает ограниченные байты constant-time; не исправляет несовпадения и не принимает содержимое за новые полномочия.
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

// verifyWorkspaceDigest verifies a guarded input's exact size/mode, hashes at most the configured artifact bound plus one and compares the expected SHA-256 pin without modifying the file.
// verifyWorkspaceDigest проверяет точные размер/режим защищённого input, хеширует не более artifact bound плюс один и сравнивает ожидаемый SHA-256 pin без изменения файла.
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

// openWorkspaceFile validates lexical root/relative paths, walks relative directories by no-follow descriptors and opens the final nonblocking regular file. The caller owns the returned file; ancestors preceding the supplied root and single-link ownership are not checked here.
// openWorkspaceFile проверяет lexical root/relative paths, обходит относительные каталоги no-follow дескрипторами и открывает конечный nonblocking обычный файл. Возвращённый файл принадлежит вызывающему; предки перед root и single-link ownership здесь не проверяются.
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

// writeInputManifests publishes each declared attachment set's canonical manifest and fixed read-only README before the full workspace manifest; missing set data fails rather than inventing a catalog or attachment authority.
// writeInputManifests публикует канонический manifest и фиксированный read-only README каждого объявленного attachment set до полного workspace manifest; отсутствие данных set даёт отказ без выдумывания каталога или attachment authority.
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
		readme := []byte(attachmentSetReadme)
		if err := writeReadOnlyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", set.Ref, "README.md"), readme); err != nil {
			return err
		}
	}
	return writeReadOnlyWorkspaceFile(input.WorkspaceRoot, filepath.Join("input", "manifest.json"), workspace.Bytes)
}

// resetWorkspaceDirectory takes the cooperative lock, requires a nonsymlink directory and removes its current entries under the supplied internal relative path with fixed diagnostics. It uses path-based operations on caller-prepared workspace parents, not an atomic descriptor-rooted snapshot or general authorization API.
// resetWorkspaceDirectory берёт согласующий lock, требует каталог без symlink и удаляет его текущие entries под переданным внутренним relative path с фиксированной диагностикой. Использует path-based операции на подготовленных workspace parents, не atomic descriptor-rooted snapshot и не общий authorization API.
func resetWorkspaceDirectory(ctx context.Context, root, relative string) error {
	lock, err := workspacepolicy.Lock(ctx, root)
	if err != nil {
		return err
	}
	defer lock.Close()
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

// protectReadOnlyWorkspaceTrees delegates each internal tree to the security helper and stops on the first failure; it does not change which trees the server-approved input may expose.
// protectReadOnlyWorkspaceTrees передаёт каждое внутреннее дерево security helper и прекращает при первой ошибке; не меняет список деревьев, доступных по одобренному сервером входу.
func protectReadOnlyWorkspaceTrees(root string, relatives ...string) error {
	for _, relative := range relatives {
		if err := security.ProtectWorkspaceInputTree(root, relative); err != nil {
			return err
		}
	}
	return nil
}

// writeWorkspaceArtifact enforces lexical containment, streams callback-verified bytes into an exclusive no-follow 0440 staging file, syncs/closes and renames only on success, cleaning failed staging. Parent-directory trust remains external and directory fsync is not performed.
// writeWorkspaceArtifact требует lexical containment, передаёт проверенные callback байты в исключительный no-follow staging 0440, синхронизирует/закрывает и переименовывает лишь при успехе, очищая отказавший staging. Доверие parent-каталогам внешнее; fsync каталога не выполняется.
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

// writeReadOnlyWorkspaceFile first publishes through the ordinary workspace writer and then changes the final file to mode 0440, returning a fixed protection error if chmod fails after publication.
// writeReadOnlyWorkspaceFile сначала публикует обычным workspace writer, затем меняет конечный файл на 0440, возвращая фиксированный protection error при отказе chmod уже после публикации.
func writeReadOnlyWorkspaceFile(root, relative string, payload []byte) error {
	if err := writeWorkspaceFile(root, relative, payload); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Join(root, relative), 0o440); err != nil {
		return errors.New("protect workspace input file")
	}
	return nil
}

// writeWorkspaceFile checks lexical containment and writes/syncs/closes an exclusive no-follow 0640 staging file before rename. It cleans write/close failures but leaves the existing staging file on rename failure; trusted parents and caller locking are assumed, and no directory sync is claimed.
// writeWorkspaceFile проверяет lexical containment и пишет/синхронизирует/закрывает исключительный no-follow staging 0640 до rename. Очищает write/close failures, но при rename failure оставляет staging; предполагает доверенные parents и lock вызывающего, не обещая directory sync.
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

// collectArtifacts begins with result.md, prioritizes workspace provenance and collects at most sixteen total artifacts from guarded immediate outbox files. Unsafe names/directories and unsupported sizes/types are skipped; open/read/link guards fail with redacted typed denials. Single-link regular files are read with a one-MiB bound without traversing subdirectories.
// collectArtifacts начинает с result.md, отдаёт приоритет workspace provenance и собирает не более шестнадцати артефактов из защищённых непосредственных outbox-файлов. Небезопасные имена/каталоги и недопустимые размеры/типы пропускаются; open/read/link guards дают очищенный typed denial. Single-link обычные файлы читаются с пределом один MiB без обхода подкаталогов.
func collectArtifacts(input model.Input, markdown string) ([]runtimecontract.RunnerArtifact, error) {
	artifacts := []runtimecontract.RunnerArtifact{artifact("result.md", "text/markdown", []byte(markdown))}
	directory, err := workspacepolicy.OpenOutbox(input.WorkspaceRoot)
	if err != nil {
		return nil, denyCompletionArtifact(err)
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, denyCompletionArtifact(err)
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
			return nil, denyCompletionArtifact(err)
		}
		file := os.NewFile(uintptr(descriptor), "runtime-artifact")
		if file == nil {
			_ = unix.Close(descriptor)
			return nil, denyCompletionArtifact(nil)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
			file.Close()
			continue
		}
		metadata, ok := info.Sys().(*syscall.Stat_t)
		if !ok || metadata.Nlink != 1 {
			file.Close()
			return nil, denyCompletionArtifact(nil)
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, 1<<20+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || int64(len(raw)) != info.Size() {
			return nil, denyCompletionArtifact(errors.Join(readErr, closeErr))
		}
		mediaType := mime.TypeByExtension(filepath.Ext(name))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		artifacts = append(artifacts, artifact(name, mediaType, raw))
	}
	return artifacts, nil
}

// artifact computes the content SHA-256 and pairs it with name/media type while retaining the supplied byte slice rather than copying it; callers own content immutability until serialization.
// artifact вычисляет SHA-256 содержимого и связывает его с name/media type, сохраняя исходный срез без копирования; вызывающий отвечает за immutable content до сериализации.
func artifact(name, mediaType string, content []byte) runtimecontract.RunnerArtifact {
	digest := sha256.Sum256(content)
	return runtimecontract.RunnerArtifact{FileName: name, MediaType: mediaType, SHA256: hex.EncodeToString(digest[:]), Content: content}
}

// safeFileName accepts only a nonempty single filename up to 255 bytes without separators, NUL or line breaks and rejects dot names; it returns the original spelling without normalization.
// safeFileName принимает лишь непустое единичное имя до 255 байтов без разделителей, NUL и переносов строк, отклоняя точечные имена; возвращает исходное написание без нормализации.
func safeFileName(value string) string {
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "/\\\x00\r\n") || value == "." || value == ".." {
		return ""
	}
	return value
}

// startHealthServer starts the fixed :9090 health listener with bounded HTTP headers/timeouts and a buffered terminal error channel, normalizing ErrServerClosed. The caller owns shutdown and observation of that channel; starting the goroutine alone is not a readiness barrier.
// startHealthServer запускает фиксированный health listener :9090 с ограниченными HTTP headers/timeouts и буферным terminal error channel, нормализуя ErrServerClosed. Shutdown и наблюдение канала принадлежат вызывающему; старт goroutine сам не readiness barrier.
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

// healthHandler serves identical healthz/livez aliases from the live flag and readyz from the ready flag plus a fresh workspace snapshot. It preserves existing status/body behavior without doing filesystem I/O or deriving execution authority from probes.
// healthHandler обслуживает одинаковые healthz/livez aliases по live flag и readyz по ready flag плюс свежему workspace snapshot. Сохраняет прежние status/body без файлового I/O и без вывода execution authority из probes.
func healthHandler(state *health) http.Handler {
	mux := http.NewServeMux()
	// liveness shares the unchanged live-flag response between the two route aliases.
	// liveness разделяет неизменный ответ live flag между двумя route aliases.
	liveness := func(writer http.ResponseWriter, _ *http.Request) {
		if !state.live.Load() {
			http.Error(writer, "process is stopping", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
	mux.HandleFunc("/healthz", liveness)
	mux.HandleFunc("/livez", liveness)
	mux.HandleFunc("/readyz", func(writer http.ResponseWriter, _ *http.Request) {
		if !state.ready.Load() {
			http.Error(writer, "runtime is not ready", http.StatusServiceUnavailable)
			return
		}
		if err := state.workspaceStatus(time.Now()); err != nil {
			http.Error(writer, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// logMCPStartupFailure emits the fixed startup warning and closed readiness stage through the supplied non-nil logger, leaving terminal codes unchanged and excluding raw errors or MCP payloads.
// logMCPStartupFailure пишет фиксированный startup warning и закрытый readiness stage через переданный non-nil logger, не меняя terminal codes и исключая сырые ошибки и MCP-пакеты.
func logMCPStartupFailure(ctx context.Context, logger *slog.Logger, err error) {
	logger.WarnContext(ctx, mcpStartupFailureMessage, "stage", readiness.FailureStage(err))
}
