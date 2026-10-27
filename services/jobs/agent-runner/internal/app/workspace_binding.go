package app

import (
	"context"
	"errors"
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

// bindWorkspaceWriter obtains a fresh authenticated owner ACK without modifying the application filesystem.
// Invalid input, UID or context prevents even the observation; transport failure never permits preparation.
// bindWorkspaceWriter получает fresh authenticated owner ACK без изменения application filesystem.
// Invalid input, UID и context запрещают observation; transport failure не разрешает preparation.
func bindWorkspaceWriter(ctx context.Context, input model.Input) (runtimecontract.WorkspaceWriterAcknowledgement, error) {
	var empty runtimecontract.WorkspaceWriterAcknowledgement
	if os.Geteuid() != 10001 || input.WorkspaceStorage == nil || input.WorkspaceStorage.ValidateForInput(input) != nil {
		return empty, errors.New("runtime workspace binding input is invalid")
	}
	if _, err := input.RequiredContextSnapshot(time.Now()); err != nil {
		return empty, errors.New("runtime workspace binding context is invalid")
	}
	client, err := callback.New(input)
	if err != nil {
		return empty, errors.New("create runtime workspace binding client")
	}
	defer client.Close()
	return client.ObserveWorkspace(ctx, input, runtimecontract.RunnerWorkspaceObservation{
		RuntimeRevisionDigest: input.RuntimeRevisionDigest,
		Phase:                 runtimecontract.WorkspaceObserveBind, WorkspaceRef: input.WorkspaceStorage.WorkspaceRef,
		Generation: input.WorkspaceStorage.Generation,
	})
}

// prepareBoundWorkspace is the first V2 initializer and writes only after the exact owner BIND succeeds.
// The PVC root is mounted privately here; subsequent containers receive only the exact work subPath/proofs.
// prepareBoundWorkspace — первый V2 initializer; запись начинается лишь после exact owner BIND.
// Здесь PVC root подключён privately; последующие containers получают exact work subPath/proofs.
func prepareBoundWorkspace(ctx context.Context, input model.Input) error {
	ack, err := bindWorkspaceWriter(ctx, input)
	if err != nil {
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", "UNKNOWN", err)
	}
	if err := security.EnsureSharedWorkspaceDirectory(".kodex"); err != nil {
		return runtimecontract.InitializationError("WORKSPACE_LAYOUT", initializationFilesystemReason(err), err)
	}
	if err := workspacepolicy.PrepareBoundGeneration(ctx, input, ack); err != nil {
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
		return errors.New("runtime workspace writer acknowledgement is unavailable")
	}
	record, ok := result.WorkspaceQuiescence(input, ack)
	if !ok {
		return errors.New("runtime workspace writer join is unconfirmed")
	}
	raw, err := runtimecontract.EncodeWorkspaceQuiescence(record)
	if err != nil {
		return errors.New("runtime workspace writer join record is invalid")
	}
	if writeInitializationTermination(initializationTerminationPath, raw) != nil {
		return errors.New("runtime workspace writer join record is unavailable")
	}
	return nil
}
