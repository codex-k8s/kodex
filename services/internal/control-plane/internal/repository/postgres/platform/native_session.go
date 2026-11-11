package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryNativeSessionSource locks the exact owner's row, including initializing or uncertain states.
// Filtering those states out would incorrectly turn a missing checkpoint into permission to cold-start.
// queryNativeSessionSource блокирует exact owner row, включая initializing и uncertain states.
// Их исключение ошибочно превратило бы missing checkpoint в разрешение cold start.
//
//go:embed sql/native_session__source.sql
var queryNativeSessionSource string

// queryNativeSessionClaimWriter creates the initial intent or fences the next turn atomically with claim.
// queryNativeSessionClaimWriter создаёт initial intent или ограждает следующий turn атомарно с claim.
//
//go:embed sql/native_session__claim_writer.sql
var queryNativeSessionClaimWriter string

// queryNativeSessionCompletionSource selects immutable input and current source under exact writer CAS.
// queryNativeSessionCompletionSource выбирает immutable input и current source под exact writer CAS.
//
//go:embed sql/native_session__completion_source.sql
var queryNativeSessionCompletionSource string

// queryNativeSessionComplete commits only the matched writer's next native checkpoint.
// queryNativeSessionComplete фиксирует следующий native checkpoint лишь совпавшего writer.
//
//go:embed sql/native_session__complete.sql
var queryNativeSessionComplete string

// queryNativeSessionUnresolvedEffects reads authoritative receipts across the native session's root graphs.
// It is not inferred from a summary or from an apparently completed native tool part.
// queryNativeSessionUnresolvedEffects читает authoritative receipts в root graphs native session.
// Состояние не выводится из summary либо завершённого native tool part.
//
//go:embed sql/native_session__unresolved_effects.sql
var queryNativeSessionUnresolvedEffects string

// queryNativeSessionRequiresCheckpoint derives successful-completion requirements from the immutable owner input.
// Caller omission of metadata cannot turn a native execution into a stateless success.
// queryNativeSessionRequiresCheckpoint выводит требования успешного completion из immutable owner input.
// Отсутствие caller metadata не превращает native execution в stateless success.
//
//go:embed sql/native_session__requires_checkpoint.sql
var queryNativeSessionRequiresCheckpoint string

// queryNativeSessionClosedWriter selects a closed lease's exact writer in a terminal graph or an expired active attempt; it never revives authority.
// queryNativeSessionClosedWriter выбирает точного writer закрытой lease завершённого графа либо истёкшей активной attempt, не возвращая полномочия.
//
//go:embed sql/native_session__closed_writer.sql
var queryNativeSessionClosedWriter string

// recordClosedNativeCompletion accepts a stopped-writer fact after cancellation/timeout, never a new run outcome.
// Exact owner scope, original fence/generation and native writer CAS are required. Receipt replay belongs
// to the enclosing command; no graph event is emitted and session_storage is the authoritative read path.
// recordClosedNativeCompletion принимает stopped-writer fact после cancel/timeout, не новый run outcome.
// Требуются exact owner scope, прежние fence/generation и native writer CAS. Replay обслуживает command;
// graph event не создаётся, authoritative read path — session_storage.
func (repository *Repository) recordClosedNativeCompletion(ctx context.Context, tx pgx.Tx, current scope, payload command.CompleteExecutionInput) (commandOutcome, bool, error) {
	if payload.NativeSession == nil {
		return commandOutcome{}, false, nil
	}
	if validateRuntimeCompletion(payload) != nil {
		return commandOutcome{}, true, errs.ErrInvalid
	}
	var leaseID, runID, nodeID, rootID, projectID, projectRef, runRef, revisionID, turnRef, sessionRef, storedFence string
	var generation int64
	err := tx.QueryRow(ctx, queryNativeSessionClosedWriter, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": payload.LeaseRef}).Scan(&leaseID, &runID, &nodeID, &rootID, &projectID, &projectRef, &runRef, &revisionID, &turnRef, &sessionRef, &storedFence, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, false, nil
	}
	if err != nil {
		return commandOutcome{}, true, errs.ErrUnavailable
	}
	fence := sha256.Sum256([]byte(payload.Fence))
	if storedFence != hex.EncodeToString(fence[:]) || generation != payload.Generation {
		return commandOutcome{}, true, errs.ErrForbidden
	}
	lease := map[string]any{"leaseID": leaseID, "runID": runID, "nodeID": nodeID, "rootRunID": rootID, "projectID": projectID, "projectRef": projectRef, "runRef": runRef, "runtimeRevisionID": revisionID, "turnRef": turnRef, "generation": generation}
	if err := recordExpiredRuntimeCompletion(ctx, tx, current, lease, payload); err != nil {
		return commandOutcome{}, true, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, runRef)
	if err != nil {
		return commandOutcome{}, true, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Runtime: map[string]any{"sessionRef": sessionRef, "nativeCheckpointRecorded": true}}, projectID: projectID, projectRef: projectRef, resourceKind: "SESSION", resourceRef: sessionRef, summary: "NATIVE_CHECKPOINT_RECORDED"}, true, nil
}

// requireNativeCompletion rejects a successful native outcome without its stopped-writer checkpoint.
// Failure without a checkpoint leaves the writer intent blocked for explicit recovery.
// requireNativeCompletion отклоняет успешный native outcome без stopped-writer checkpoint.
// Failure без checkpoint оставляет writer intent закрытым до явного recovery.
func requireNativeCompletion(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, leaseRef string) error {
	var required bool
	if err := tx.QueryRow(ctx, queryNativeSessionRequiresCheckpoint, pgx.StrictNamedArgs{"organization_id": current.organizationID, "runtime_revision_id": lease["runtimeRevisionID"], "lease_ref": leaseRef}).Scan(&required); err != nil {
		return errs.ErrUnavailable
	}
	if required {
		return errs.ErrInvalid
	}
	return nil
}

// nativeSessionSource returns only a compatible confirmed owner checkpoint; any prior unresolved writer blocks.
// The absence of a row means this new-format session has never been assigned a native execution.
// nativeSessionSource возвращает лишь совместимый подтверждённый owner checkpoint; unresolved writer блокирует.
// Отсутствие row означает, что этой new-format session ещё не назначалось native execution.
func nativeSessionSource(ctx context.Context, tx pgx.Tx, current scope, snapshot map[string]any) (*runtimecontract.NativeSessionCheckpoint, error) {
	var state, accountRef string
	var raw, previousRaw []byte
	var writer bool
	err := tx.QueryRow(ctx, queryNativeSessionSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": stringMap(snapshot, "sessionRef")}).Scan(&state, &raw, &writer, &accountRef, &previousRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	if state != "LIVE" || writer || accountRef != stringMap(snapshot, "providerAccountRef") {
		return nil, errs.ErrConflict
	}
	var unresolved bool
	if err := tx.QueryRow(ctx, queryNativeSessionUnresolvedEffects, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_ref": stringMap(snapshot, "sessionRef")}).Scan(&unresolved); err != nil {
		return nil, errs.ErrUnavailable
	}
	if unresolved {
		return nil, errs.ErrConflict
	}
	checkpoint, err := runtimecontract.DecodeNativeSessionCheckpoint(raw)
	if err != nil {
		return nil, errs.ErrConflict
	}
	var previous map[string]any
	if json.Unmarshal(previousRaw, &previous) != nil || !nativeSessionResumeCompatible(previous, snapshot) {
		return nil, errs.ErrConflict
	}
	workspace, err := runtimeWorkspaceSnapshotBinding(snapshot)
	if err != nil || checkpoint.ValidateWorkspace(workspace) != nil {
		return nil, errs.ErrConflict
	}
	return &checkpoint, nil
}

// nativeSessionResumeCompatible compares persistent trust identity and qualified provider protocol.
// Credentials, instructions, grants and model ceilings are freshly resolved rather than reused from history.
// nativeSessionResumeCompatible сравнивает persistent trust identity и qualified provider protocol.
// Credentials, инструкции, grants и model ceilings разрешаются заново, а не берутся из истории.
func nativeSessionResumeCompatible(previous, current map[string]any) bool {
	for _, snapshot := range []map[string]any{previous, current} {
		if stringMap(snapshot, "runtimeProvider") != "opencode" || runtimeRevisionMapInt64(snapshot, "roleRuntimeContractRevision") < 1 {
			return false
		}
		for _, key := range []string{"organizationRef", "sessionRef", "agentRef", "providerAccountRef", "runtimeModel", "runtimeKey"} {
			if stringMap(snapshot, key) == "" {
				return false
			}
		}
		switch stringMap(snapshot, "assistantScope") {
		case "SYSTEM":
			if stringMap(snapshot, "assistantProfileRef") != "" {
				return false
			}
		case "PROJECT":
			if stringMap(snapshot, "assistantProfileRef") == "" || stringMap(snapshot, "projectRef") == "" {
				return false
			}
		case "NONE":
			if stringMap(snapshot, "projectRef") == "" || stringMap(snapshot, "assistantProfileRef") != "" {
				return false
			}
		default:
			return false
		}
	}
	for _, key := range []string{"organizationRef", "projectRef", "sessionRef", "agentRef", "providerAccountRef", "runtimeModel", "runtimeKey", "assistantScope", "assistantProfileRef"} {
		if stringMap(previous, key) != stringMap(current, key) {
			return false
		}
	}
	if runtimeRevisionMapInt64(previous, "roleRuntimeContractRevision") != runtimeRevisionMapInt64(current, "roleRuntimeContractRevision") {
		return false
	}
	initiator := func(snapshot map[string]any) string {
		// Current snapshots contain typed values; persisted snapshots contain decoded maps.
		// Both must resolve the same immutable actor rather than silently cold-starting on representation.
		// Current snapshot содержит typed values, persisted — decoded maps.
		// Оба должны разрешать одинакового immutable actor без silent cold start из-за representation.
		raw, err := json.Marshal(snapshot["promptSnapshot"])
		var prompt struct {
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err != nil || json.Unmarshal(raw, &prompt) != nil {
			return ""
		}
		var actor string
		if json.Unmarshal(prompt.Variables["user.ref"], &actor) != nil {
			return ""
		}
		return actor
	}
	if initiator(previous) == "" || initiator(previous) != initiator(current) {
		return false
	}
	var profiles []runtimecontract.ModelProviderConfiguration
	for _, snapshot := range []map[string]any{previous, current} {
		raw, err := json.Marshal(snapshot["modelProvider"])
		if err != nil {
			return false
		}
		profile, err := runtimecontract.DecodeModelProviderConfiguration(raw)
		if err != nil {
			return false
		}
		if _, found := profile.Model(stringMap(snapshot, "runtimeModel")); !found {
			return false
		}
		profiles = append(profiles, profile)
	}
	return profiles[0].API == profiles[1].API && profiles[0].RouteKind == profiles[1].RouteKind && profiles[0].RouteVersion == profiles[1].RouteVersion && profiles[0].ProviderID == profiles[1].ProviderID && profiles[0].BaseURL == profiles[1].BaseURL && profiles[0].EngineVersion == profiles[1].EngineVersion && profiles[0].ToolMode == profiles[1].ToolMode
}

// claimNativeSessionWriter fences native state under the exact newly persisted lease/revision.
// A lost response or stale tail cannot create a second initial native session on the next attempt.
// claimNativeSessionWriter ограждает native state точной новой persisted lease/revision.
// Потерянный ответ или stale tail не создают вторую initial native session в следующей attempt.
func claimNativeSessionWriter(ctx context.Context, tx pgx.Tx, current scope, sessionID, revisionID, leaseRef string) error {
	stored, err := tx.Exec(ctx, queryNativeSessionClaimWriter, pgx.StrictNamedArgs{"organization_id": current.organizationID, "session_id": sessionID, "runtime_revision_id": revisionID, "lease_ref": leaseRef})
	if err != nil {
		return errs.ErrUnavailable
	}
	if stored.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// completeNativeSessionCheckpoint records the stopped writer's native metadata without changing a run verdict.
// A newer writer, foreign tuple, old generation or mismatched workspace is closed, including late callbacks.
// completeNativeSessionCheckpoint фиксирует native metadata остановленного writer без смены run verdict.
// Более новый writer, foreign tuple, старая generation и другое workspace закрыты, включая late callbacks.
func completeNativeSessionCheckpoint(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, leaseRef string, checkpoint runtimecontract.NativeSessionCheckpoint) error {
	var rawSnapshot, storedRaw []byte
	err := tx.QueryRow(ctx, queryNativeSessionCompletionSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "runtime_revision_id": lease["runtimeRevisionID"], "lease_ref": leaseRef}).Scan(&rawSnapshot, &storedRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	var snapshot map[string]any
	if json.Unmarshal(rawSnapshot, &snapshot) != nil {
		return errs.ErrConflict
	}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, Provider: stringMap(snapshot, "runtimeProvider")}
	if value, present := snapshot["nativeSession"]; present {
		raw, err := json.Marshal(value)
		if err != nil {
			return errs.ErrConflict
		}
		previous, err := runtimecontract.DecodeNativeSessionCheckpoint(raw)
		if err != nil {
			return errs.ErrConflict
		}
		input.NativeSession = &previous
	}
	if len(storedRaw) > 0 {
		stored, err := runtimecontract.DecodeNativeSessionCheckpoint(storedRaw)
		if err != nil || input.NativeSession == nil || stored != *input.NativeSession {
			return errs.ErrConflict
		}
	} else if input.NativeSession != nil {
		return errs.ErrConflict
	}
	workspace, err := runtimeWorkspaceSnapshotBinding(snapshot)
	if err != nil {
		return errs.ErrConflict
	}
	input.WorkspaceStorage = workspace
	if checkpoint.ValidateCompletion(input) != nil {
		return errs.ErrInvalid
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return errs.ErrInvalid
	}
	stored, err := tx.Exec(ctx, queryNativeSessionComplete, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "runtime_revision_id": lease["runtimeRevisionID"], "lease_ref": leaseRef,
		"native_session_id": checkpoint.SessionID, "native_checkpoint": raw, "content_generation": checkpoint.Generation,
		"source_relative_path": checkpoint.RelativePath, "source_sha256": checkpoint.SHA256, "source_size_bytes": checkpoint.SizeBytes,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if stored.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
