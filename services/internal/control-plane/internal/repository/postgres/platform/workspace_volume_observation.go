package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryWorkspaceVolumeAttempt resolves only the exact durable fence, including a closed historical attempt, after root/session/custody/task locks.
// queryWorkspaceVolumeAttempt разрешает только exact durable fence, включая closed historical attempt, после root/session/custody/task locks.
//
//go:embed sql/workspace_volume__attempt.sql
var queryWorkspaceVolumeAttempt string

// workspaceVolumeAttempt retains the immutable effective input and an optional past physical fact without treating either as renewed authority.
// workspaceVolumeAttempt сохраняет immutable effective input и optional past physical fact, не объявляя их renewed authority.
type workspaceVolumeAttempt struct {
	work                                                runtimecontract.WorkspaceVolumeWork
	leaseRef, grantState, pvcUID, jobUID, podUID, ready string
	expiresAt                                           time.Time
	unexpired, reported                                 bool
}

// decodeWorkspaceVolumeAttemptSnapshot verifies both stored bytes and the canonical empty-digest frame before restoring its externally stored digest.
// It never constructs a model revision, reassigns a logical preparation or grants a physical action.
// decodeWorkspaceVolumeAttemptSnapshot проверяет stored bytes и canonical frame с пустым digest до восстановления отдельно сохранённого digest.
// Проверка не создаёт model revision, не меняет logical preparation и не разрешает physical action.
func decodeWorkspaceVolumeAttemptSnapshot(raw []byte, digest string) (runtimecontract.WorkspaceVolumeWork, error) {
	var work runtimecontract.WorkspaceVolumeWork
	if len(raw) == 0 || len(raw) > runtimecontract.MaximumWorkspaceVolumeReadyBytes ||
		runtimecontract.DecodeWorkspaceStorageTaskJSON(raw, &work) != nil || work.InputDigest != "" {
		return runtimecontract.WorkspaceVolumeWork{}, errs.ErrConflict
	}
	hash := sha256.Sum256(raw)
	canonical, err := json.Marshal(work)
	if err != nil || !bytes.Equal(raw, canonical) || hex.EncodeToString(hash[:]) != digest {
		return runtimecontract.WorkspaceVolumeWork{}, errs.ErrConflict
	}
	work.InputDigest = digest
	if work.Validate() != nil {
		return runtimecontract.WorkspaceVolumeWork{}, errs.ErrConflict
	}
	return work, nil
}

// loadWorkspaceVolumeAttemptTx authenticates the exact ledger tuple without requiring an old grant to remain active for a late positive ACK.
// Renew/failure callers must separately require current attempt, active grant, unexpired lease and fresh origin; this read cannot revive them.
// loadWorkspaceVolumeAttemptTx проверяет exact ledger tuple без требования active старого grant для late positive ACK.
// Renew/failure отдельно требуют current attempt/active grant/unexpired lease/fresh origin; это чтение не возобновляет их.
func loadWorkspaceVolumeAttemptTx(ctx context.Context, tx pgx.Tx, current scope, task workspaceVolumeTask, input command.WorkspaceVolumeTaskInput) (workspaceVolumeAttempt, error) {
	if input.TaskRef != task.ref || input.Generation < 1 || input.Generation > 5 ||
		len(input.LeaseRef) < 8 || len(input.LeaseRef) > 96 || len(input.Fence) < 8 || len(input.Fence) > 96 {
		return workspaceVolumeAttempt{}, errs.ErrForbidden
	}
	fenceDigest := sha256.Sum256([]byte(input.Fence))
	var attempt workspaceVolumeAttempt
	var snapshot, digest string
	err := tx.QueryRow(ctx, queryWorkspaceVolumeAttempt, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "task_id": task.id, "task_ref": task.ref,
		"lease_ref": input.LeaseRef, "generation": input.Generation, "fence_digest": hex.EncodeToString(fenceDigest[:]),
	}).Scan(&snapshot, &digest, &attempt.leaseRef, &attempt.grantState, &attempt.expiresAt,
		&attempt.unexpired, &attempt.reported, &attempt.pvcUID, &attempt.jobUID, &attempt.podUID, &attempt.ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return workspaceVolumeAttempt{}, errs.ErrForbidden
	} else if err != nil {
		return workspaceVolumeAttempt{}, errs.ErrUnavailable
	}
	attempt.work, err = decodeWorkspaceVolumeAttemptSnapshot([]byte(snapshot), digest)
	if err != nil || attempt.work.TaskRef != task.ref || attempt.work.Kind != task.kind ||
		attempt.work.Volume != task.input || int64(attempt.work.Attempt) != input.Generation ||
		attempt.grantState != "ACTIVE" && attempt.grantState != "CLOSED" {
		return workspaceVolumeAttempt{}, errs.ErrConflict
	}
	if attempt.reported {
		var ready *runtimecontract.WorkspaceVolumeReady
		if attempt.ready != "" {
			value, err := runtimecontract.DecodeWorkspaceVolumeReady([]byte(attempt.ready))
			if err != nil {
				return workspaceVolumeAttempt{}, errs.ErrConflict
			}
			ready = &value
		}
		if attempt.grantState != "CLOSED" || runtimecontract.ValidateWorkspaceVolumeObservation(attempt.work, attempt.pvcUID, attempt.jobUID, attempt.podUID, ready) != nil {
			return workspaceVolumeAttempt{}, errs.ErrConflict
		}
	} else if attempt.pvcUID != "" || attempt.jobUID != "" || attempt.podUID != "" || attempt.ready != "" {
		return workspaceVolumeAttempt{}, errs.ErrConflict
	}
	return attempt, nil
}
