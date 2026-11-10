package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/callback"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/security"
	workspacepolicy "github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/workspace"
)

// runtimeBindWorkspaceMode is a distinct protected entrypoint; missing input cannot fall back to legacy preparation.
// runtimeBindWorkspaceMode — отдельная protected entrypoint: отсутствие input не включает legacy preparation.
const runtimeBindWorkspaceMode = "runtime-bind-workspace"

// workspaceBindingStage identifies a fixed local rejection boundary, never caller data.
// workspaceBindingStage обозначает фиксированную точку отказа, не данные вызывающего.
type workspaceBindingStage uint8

// The stages name only trusted call sites; they never authorize a writer or classify raw errors.
// Стадии обозначают только trusted call sites; не разрешают writer и не разбирают raw errors.
const (
	workspaceBindingUnknown workspaceBindingStage = iota
	workspaceBindingVolume
	workspaceBindingInput
	workspaceBindingContext
	workspaceBindingClient
	workspaceBindingOwner
	workspaceBindingDirectory
	workspaceBindingGeneration
)

// logWorkspaceBindingRejection emits only a closed stage without private input or error bytes.
// It changes neither the returned failure nor writer admission, filesystem state or retry policy.
// logWorkspaceBindingRejection выводит только закрытую стадию без private input либо error bytes.
// Возвращаемый отказ, допуск writer, filesystem state и retry policy не меняются.
func logWorkspaceBindingRejection(ctx context.Context, stage workspaceBindingStage) {
	labels := [...]string{"UNKNOWN", "VOLUME_VERIFY", "INPUT", "CONTEXT", "CALLBACK_CLIENT", "OWNER_EXCHANGE", "WORKSPACE_DIRECTORY", "GENERATION_PREPARE"}
	if int(stage) >= len(labels) {
		stage = workspaceBindingUnknown
	}
	slog.WarnContext(ctx, "runtime workspace binding rejected", "binding_stage", labels[stage])
}

// bindWorkspaceWriter obtains a fresh authenticated owner ACK without modifying the application filesystem.
// Invalid input, UID or context prevents even the observation; transport failure never permits preparation.
// bindWorkspaceWriter получает fresh authenticated owner ACK без изменения application filesystem.
// Invalid input, UID и context запрещают observation; transport failure не разрешает preparation.
func bindWorkspaceWriter(ctx context.Context, input model.Input) (runtimecontract.WorkspaceWriterAcknowledgement, error) {
	var empty runtimecontract.WorkspaceWriterAcknowledgement
	if os.Geteuid() != 10001 || input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingInput)
		return empty, errors.New("runtime workspace binding input is invalid")
	}
	if _, err := input.RequiredContextSnapshot(time.Now()); err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingContext)
		return empty, errors.New("runtime workspace binding context is invalid")
	}
	client, err := callback.New(input)
	if err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingClient)
		return empty, errors.New("create runtime workspace binding client")
	}
	defer client.Close()
	ack, err := client.ObserveWorkspace(ctx, input, runtimecontract.RunnerWorkspaceObservation{
		RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		Phase:                 runtimecontract.WorkspaceObserveBind, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef,
		Generation: input.WorkspaceStorage.Generation,
	})
	if err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingOwner)
	}
	return ack, err
}

// prepareBoundWorkspace is the first V2 initializer and writes only after the exact owner BIND succeeds.
// The PVC root is mounted privately here; subsequent containers receive only the exact work subPath/proofs.
// prepareBoundWorkspace — первый V2 initializer; запись начинается лишь после exact owner BIND.
// Здесь PVC root подключён privately; последующие containers получают exact work subPath/proofs.
func prepareBoundWorkspace(ctx context.Context, input model.Input) error {
	if err := workspacepolicy.VerifyPreparedVolume(ctx, input); err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingVolume)
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", initializationFilesystemReason(err), err)
	}
	ack, err := bindWorkspaceWriter(ctx, input)
	if err != nil {
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", "UNKNOWN", err)
	}
	if err := security.EnsureSharedWorkspaceDirectory(".kodex"); err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingDirectory)
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", initializationFilesystemReason(err), err)
	}
	if err := workspacepolicy.PrepareBoundGeneration(ctx, input, ack); err != nil {
		logWorkspaceBindingRejection(ctx, workspaceBindingGeneration)
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", initializationFilesystemReason(err), err)
	}
	return nil
}

// publishWorkspaceQuiescence persists a small exact positive join record before the completion callback.
// It never copies model text/rollout as source proof; a missing private provider witness remains blocked.
// publishWorkspaceQuiescence сохраняет small exact positive join record до completion callback.
// Model text/rollout не копируются как source proof; отсутствие private provider witness остаётся blocked.
func publishWorkspaceQuiescence(input model.Input, result codex.Result) error {
	if input.WorkspaceStorage == nil {
		return nil
	}
	ack, err := workspacepolicy.ReadBoundWriterAcknowledgement(input)
	if err != nil {
		logWorkspaceQuiescenceRejection("WRITER_ACK")
		return errors.New("runtime workspace writer acknowledgement is unavailable")
	}
	record, ok := result.WorkspaceQuiescence(input, ack)
	if !ok {
		logWorkspaceQuiescenceRejection("PROVIDER_JOIN")
		return errors.New("runtime workspace writer join is unconfirmed")
	}
	raw, err := runtimecontract.EncodeWorkspaceQuiescence(record)
	if err != nil {
		logWorkspaceQuiescenceRejection("RECORD_ENCODE")
		return errors.New("runtime workspace writer join record is invalid")
	}
	if writeInitializationTermination(initializationTerminationPath, raw) != nil {
		logWorkspaceQuiescenceRejection("RECORD_WRITE")
		return errors.New("runtime workspace writer join record is unavailable")
	}
	return nil
}

// logWorkspaceQuiescenceRejection reports one closed local stage without error bytes, paths or provider output; it cannot grant a checkpoint.
// logWorkspaceQuiescenceRejection выводит закрытую local stage без error bytes, paths и provider output; checkpoint этим не разрешается.
func logWorkspaceQuiescenceRejection(stage string) {
	switch stage {
	case "EXECUTOR_UNAVAILABLE", "EXECUTOR_JOIN", "WRITER_ACK", "PROVIDER_JOIN", "RECORD_ENCODE", "RECORD_WRITE":
	default:
		stage = "UNKNOWN"
	}
	slog.Warn("runtime workspace quiescence rejected", "quiescence_stage", stage)
}
