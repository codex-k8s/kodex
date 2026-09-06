package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/provider_cleanup_lock_account.sql
var queryProviderCleanupLockAccount string

//go:embed sql/provider_cleanup_authorization_successor.sql
var queryProviderCleanupAuthorizationSuccessor string

//go:embed sql/provider_cleanup_cas_successor.sql
var queryProviderCleanupCASSuccessor string

//go:embed sql/provider_cleanup_find_produced_revision.sql
var queryProviderCleanupFindProducedRevision string

//go:embed sql/provider_cleanup_produced_descriptor_conflict.sql
var queryProviderCleanupProducedDescriptorConflict string

func providerCleanupObservationReceipt(ref string, generation int64, descriptor []byte) string {
	digest := sha256.Sum256(descriptor)
	return fmt.Sprintf("provider-metadata:%s:g%d:%s", ref, generation, hex.EncodeToString(digest[:]))
}

func sameCanonicalJSON(left, right []byte) bool {
	var a, b entity.ProviderAuthorizationCleanupResult
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}

func (repository *Repository) applyProviderCleanupCompletion(ctx context.Context, tx pgx.Tx, ref string, task lockedProviderCredentialCleanupTask, completion entity.ProviderAuthorizationCleanupResult) error {
	if task.targetKind == "AUTHORIZATION_METADATA" {
		observation := completion.Observation
		if observation == nil || completion.TerminalReceipt != "" || completion.ProducedCredential != nil {
			return errs.ErrInvalid
		}
		target := observation.Target
		if target.TaskRef != ref || target.AccountRef != task.accountRef || target.Generation != task.generation ||
			target.AuthorizationAttemptRef != task.authorizationRef || target.MaterializerAttemptRef != task.materializerRef {
			return errs.ErrConflict
		}
		switch observation.State {
		case "PRESENT":
			uid, err := uuid.Parse(target.UID)
			if err != nil || uid.String() != target.UID || target.ResourceVersion == "" || len(target.ResourceVersion) > 128 || target.Kind != "AUTHORIZATION_ATTEMPT" {
				return errs.ErrInvalid
			}
		case "ABSENT_UNFENCED", "CONFIRMED_ABSENT":
			if target.Kind != "AUTHORIZATION_ABSENCE" || target.UID != "" || target.ResourceVersion != "" {
				return errs.ErrInvalid
			}
		default:
			return errs.ErrInvalid
		}
		nextRef, err := newRef("pcct")
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, queryProviderCleanupAuthorizationSuccessor, pgx.StrictNamedArgs{
			"next_ref": nextRef, "target_kind": target.Kind, "object_uid": target.UID,
			"object_version": target.ResourceVersion, "parent_ref": ref,
		})
		if err != nil {
			return errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		// Наблюдение не разрешает удаление credential до fencing/join попытки.
		return nil
	}
	if completion.Observation != nil || (task.targetKind != "CREDENTIAL" && task.targetKind != "AUTHORIZATION_ATTEMPT" && task.targetKind != "AUTHORIZATION_ABSENCE") {
		return errs.ErrInvalid
	}
	produced := completion.ProducedCredential
	if produced == nil {
		return nil
	}
	if !validProviderCredential(*produced) {
		return errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{
		"organization_id": task.organizationID, "account_id": task.accountID,
		"secret_name": produced.SecretName, "secret_uid": produced.SecretUID,
		"secret_resource_version": produced.SecretResourceVersion, "content_sha256": produced.ContentSHA256,
	}
	var credentialID string
	var descriptorConflict bool
	if err := tx.QueryRow(ctx, queryProviderCleanupProducedDescriptorConflict, args).Scan(&descriptorConflict); err != nil {
		return errs.ErrUnavailable
	}
	if descriptorConflict {
		return errs.ErrConflict
	}
	var cleanupRef, cleanupState string
	var terminalConfirmed bool
	var current *bool
	err := tx.QueryRow(ctx, queryProviderCleanupFindProducedRevision, args).Scan(&credentialID, &current, &cleanupRef, &cleanupState, &terminalConfirmed)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrUnavailable
	}
	if current != nil && *current {
		return errs.ErrConflict
	}
	if cleanupRef == ref {
		return errs.ErrConflict
	}
	if cleanupState == "COMPLETED" {
		// Устойчивый broker receipt сохраняет produced descriptor и после его
		// отдельной очистки. Принимаем только доказанное завершение exact revision.
		if !terminalConfirmed {
			return errs.ErrConflict
		}
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		credentialRef, err := newRef("pcred")
		if err != nil {
			return err
		}
		args["credential_ref"] = credentialRef
		if err := tx.QueryRow(ctx, queryProviderAccountsInsertCredentialRevision, args).Scan(&credentialID); err != nil {
			return errs.ErrUnavailable
		}
	}
	return repository.scheduleProviderCredentialCleanup(ctx, tx, task.organizationID, task.accountID, credentialID, time.Now().UTC())
}
