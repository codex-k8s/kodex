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

// queryProviderCleanupLockAccount embeds account locking through an exact cleanup-task ref and
// tenant-consistent join. It returns organization/account identifiers without checking claimant,
// generation or permission by itself.
// queryProviderCleanupLockAccount встраивает account lock через exact cleanup-task ref и
// tenant-consistent join. Возвращает organization/account identifiers без самостоятельной проверки
// claimant, generation либо permission.
//
//go:embed sql/provider_cleanup_lock_account.sql
var queryProviderCleanupLockAccount string

// queryProviderCleanupAuthorizationSuccessor embeds a successor for a claimed metadata task with
// observed materializer identity and inherited lineage/generation/attempt budget. This stages a
// fenced follow-up; it does not delete a credential or complete its predecessor.
// queryProviderCleanupAuthorizationSuccessor встраивает successor для claimed metadata task с
// observed materializer identity и унаследованными lineage/generation/attempt budget.
// Подготавливает fenced follow-up; не удаляет credential и не завершает predecessor.
//
//go:embed sql/provider_cleanup_authorization_successor.sql
var queryProviderCleanupAuthorizationSuccessor string

// queryProviderCleanupCASSuccessor embeds a metadata successor only for a claimed
// authorization-attempt/absence task, retaining predecessor, attempts and lease generation. It
// schedules reobservation instead of declaring terminal absence after a CAS mismatch.
// queryProviderCleanupCASSuccessor встраивает metadata successor только для claimed
// authorization-attempt/absence task, сохраняя predecessor, attempts и lease generation. Назначает
// повторное наблюдение вместо объявления terminal absence после CAS mismatch.
//
//go:embed sql/provider_cleanup_cas_successor.sql
var queryProviderCleanupCASSuccessor string

// queryProviderCleanupFindProducedRevision embeds exact descriptor lookup with current-revision
// status and latest cleanup. Terminal confirmation requires an unsuperseded matching credential
// receipt/descriptor and completion metadata; COMPLETED alone is insufficient.
// queryProviderCleanupFindProducedRevision встраивает exact descriptor lookup с current-revision
// status и latest cleanup. Terminal confirmation требует unsuperseded matching credential
// receipt/descriptor и completion metadata; одного COMPLETED недостаточно.
//
//go:embed sql/provider_cleanup_find_produced_revision.sql
var queryProviderCleanupFindProducedRevision string

// queryProviderCleanupProducedDescriptorConflict embeds detection of a reused Secret UID across
// owner/name boundaries or inconsistent digest at the same resource version. It checks descriptor
// metadata, not secret contents or external object existence.
// queryProviderCleanupProducedDescriptorConflict встраивает обнаружение повторного Secret UID в
// других owner/name boundaries либо противоречивого digest при том же resource version. Проверяет
// descriptor metadata, а не secret contents или внешнее существование объекта.
//
//go:embed sql/provider_cleanup_produced_descriptor_conflict.sql
var queryProviderCleanupProducedDescriptorConflict string

// providerCleanupObservationReceipt formats a deterministic metadata observation receipt from task
// ref, generation and SHA-256 of exact descriptor bytes. It is an identifier/digest binding, not a
// signature or proof of external absence.
// providerCleanupObservationReceipt формирует детерминированный metadata observation receipt из
// task ref, generation и SHA-256 точных descriptor bytes. Это identifier/digest binding, а не
// signature либо доказательство внешнего отсутствия.
func providerCleanupObservationReceipt(ref string, generation int64, descriptor []byte) string {
	digest := sha256.Sum256(descriptor)
	return fmt.Sprintf("provider-metadata:%s:g%d:%s", ref, generation, hex.EncodeToString(digest[:]))
}

// sameCanonicalJSON decodes both inputs into the cleanup-result struct and compares their
// re-encoded forms. Invalid JSON fails; unknown fields are ignored by ordinary Unmarshal, so this
// is typed semantic equality rather than strict schema validation or exact-byte identity.
// sameCanonicalJSON декодирует оба input в cleanup-result struct и сравнивает повторно
// закодированные формы. Некорректный JSON отклоняется; unknown fields игнорируются обычным
// Unmarshal, поэтому это typed semantic equality, а не strict schema validation либо exact-byte
// identity.
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

// applyProviderCleanupCompletion validates metadata observation target/task pins before creating
// one exact follow-up; observation alone never permits credential deletion. For produced
// credentials it rejects owner/descriptor/current-revision/self-cleanup conflicts, accepts only
// proven exact terminal cleanup, or records/schedules an unused revision. The caller must already
// lock and authorize the task and atomically commit; this helper does no external I/O.
// applyProviderCleanupCompletion проверяет metadata observation target/task pins до создания
// одного exact follow-up; одного observation недостаточно для удаления credential. Для produced
// credentials отклоняет owner/descriptor/current-revision/self-cleanup conflicts, принимает только
// доказанный exact terminal cleanup либо записывает/назначает unused revision. Caller заранее
// блокирует и авторизует task и атомарно выполняет commit; helper не делает external I/O.
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
