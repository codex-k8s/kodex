package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// sshSetupCancelOperation fixes generic receipt and audit scope; a supplied mutation operation cannot select another command.
// sshSetupCancelOperation фиксирует generic receipt и audit scope; mutation operation не выбирает другую command.
const sshSetupCancelOperation = "platform.integrations.ssh.setup.cancel"

// querySSHSetupLockOwned preserves Connection→setup lock order and addresses only the original current active USER's own setup.
// querySSHSetupLockOwned сохраняет Connection→setup lock order и адресует лишь own setup original current active USER.
//
//go:embed sql/ssh_setup__lock_owned.sql
var querySSHSetupLockOwned string

// querySSHSetupCancel closes current attempt authority and setup atomically without deleting receipts or claiming that a dispatched material write stopped.
// querySSHSetupCancel атомарно закрывает current attempt authority и setup без удаления receipts либо утверждения об остановке dispatched material write.
//
//go:embed sql/ssh_setup__cancel.sql
var querySSHSetupCancel string

// CancelSSHIdentitySetup authorizes current self visibility before receipt replay/OCC and retains exact historical cancellation metadata; late material evidence remains independently recordable.
// CancelSSHIdentitySetup проверяет current self visibility до receipt replay/OCC и сохраняет exact historical cancellation metadata; late material evidence может сохраняться отдельно.
func (repository *Repository) CancelSSHIdentitySetup(ctx context.Context, principal value.Principal, mutation value.Mutation, input command.SSHIdentitySetupCancel) (entity.SSHIdentitySetupView, error) {
	var result entity.SSHIdentitySetupView
	if err := sshSetupAPICaller(principal, sshSetupCancelOperation); err != nil {
		return result, err
	}
	if !managedssh.OwnerReference(input.ConnectionRef) || !managedssh.OwnerReference(input.SetupRef) || !strings.HasPrefix(input.SetupRef, "ssu_") || mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || !validSSHSetupKey(mutation.IdempotencyKey) {
		return result, errs.ErrInvalid
	}
	raw, err := json.Marshal(struct {
		Input           command.SSHIdentitySetupCancel
		ExpectedVersion int64
	}{input, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrInvalid
	}
	digest := sha256.Sum256(raw)
	intent := hex.EncodeToString(digest[:])
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.requireAccess(ctx, tx, current, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input.ConnectionRef}); err != nil {
		return result, sshSetupAccessError(err)
	}
	if _, err = repository.lockIntegrationConnection(ctx, tx, current.organizationID, input.ConnectionRef); err != nil {
		return result, err
	}
	var setupID string
	var version int64
	var state enum.SSHIdentitySetupState
	err = tx.QueryRow(ctx, querySSHSetupLockOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef}).Scan(&setupID, &version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil || !state.Valid() {
		return result, errs.ErrUnavailable
	}
	var storedIntent string
	var payload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, sshSetupCancelOperation, mutation.IdempotencyKey).Scan(&storedIntent, &payload)
	if err == nil {
		if storedIntent != intent {
			return result, errs.ErrIdempotencyReuse
		}
		if len(payload) > 8192 {
			return result, errs.ErrUnavailable
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.Ref != input.SetupRef || result.ConnectionRef != input.ConnectionRef || result.Version < 1 || result.IdentityRevision < 1 || result.ExpiresAt.IsZero() || !sshSetupReason(result.ReasonCode) ||
			(result.State != enum.SSHIdentitySetupCancelled && result.State != enum.SSHIdentitySetupExpired && result.State != enum.SSHIdentitySetupUnreported) {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
		result, err = canonicalSSHSetupView(result)
		if err != nil {
			return entity.SSHIdentitySetupView{}, err
		}
		currentView, readErr := readSSHSetupView(tx.QueryRow(ctx, querySSHSetupReadOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef, "begin_key": ""}))
		if readErr != nil {
			return entity.SSHIdentitySetupView{}, readErr
		}
		if result.IdentityRef != currentView.IdentityRef || result.TargetRef != currentView.TargetRef || result.ConnectionVersion != currentView.ConnectionVersion || result.BindingVersion != currentView.BindingVersion || result.BindingGeneration != currentView.BindingGeneration || result.IdentityRevision != currentView.IdentityRevision || result.Policy != currentView.Policy || !result.ExpiresAt.Equal(currentView.ExpiresAt) || result.Version > currentView.Version {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
		if tx.Commit(ctx) != nil {
			return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if version != *mutation.ExpectedVersion {
		return result, errs.ErrVersionMismatch
	}
	if state == enum.SSHIdentitySetupCompleted || state == enum.SSHIdentitySetupAcknowledgementRejected || state == enum.SSHIdentitySetupQuarantined {
		return result, errs.ErrConflict
	}
	if !state.Terminal() {
		tag, err := tx.Exec(ctx, querySSHSetupCancel, pgx.StrictNamedArgs{"organization_id": current.organizationID, "setup_id": setupID, "expected_version": version})
		if err != nil || tag.RowsAffected() != 1 {
			return result, errs.ErrUnavailable
		}
	}
	result, err = readSSHSetupView(tx.QueryRow(ctx, querySSHSetupReadOwned, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "connection_ref": input.ConnectionRef, "setup_ref": input.SetupRef, "begin_key": ""}))
	if err != nil {
		return entity.SSHIdentitySetupView{}, err
	}
	payload, err = json.Marshal(result)
	if err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, sshSetupCancelOperation, mutation.IdempotencyKey, intent, "SSH_IDENTITY_SETUP", payload); err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID, sshSetupCancelOperation, "INTEGRATION_CONNECTION", input.ConnectionRef, "i18n:SSH_IDENTITY_SETUP_CANCELLED", current.correlationRef); err != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return entity.SSHIdentitySetupView{}, errs.ErrUnavailable
	}
	return result, nil
}
