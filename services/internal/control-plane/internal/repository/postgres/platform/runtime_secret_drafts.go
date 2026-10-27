package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repoport "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// secretDraftRow holds public draft metadata plus internal owner/secret/project IDs, expected
// content hash, namespaces and an optional encrypted descriptor. It contains no plaintext value;
// the private descriptor and IDs must stay within authorized broker/owner paths.
// secretDraftRow хранит публичные метаданные черновика, внутренние ID владельца, секрета и
// проекта, ожидаемый хеш, namespace и зашифрованный дескриптор. Открытого значения нет; закрытые
// дескриптор и ID используются только авторизованными путями владельца и брокера.
type secretDraftRow struct {
	id, ownerID, contentDigest, secretID, projectID, namespace, stagingNamespace, secretState string
	public                                                                                    entity.RuntimeSecretDraft
	encrypted                                                                                 *entity.RuntimeSecretDraftEncryptedDescriptor
}

// secretDraftOperationRow carries a locked operation identity, expected owner versions/revisions,
// intent, actor, grant deadline and claimant/generation/lease alongside terminal and cleanup
// snapshots. Cleanup descriptors are decoded separately for recovery; stored fields are evidence
// requiring lifecycle checks, not fresh grants.
// secretDraftOperationRow содержит заблокированную операцию, ожидаемые версии и ревизии владельца,
// намерение, субъекта, срок разрешения, заявителя, поколение и аренду, итоговые и очистные снимки.
// Дескрипторы декодируются для восстановления; сохранённые сведения требуют проверок жизненного
// цикла и не выдают новое разрешение.
type secretDraftOperationRow struct {
	recoveryEncrypted                                                             *entity.RuntimeSecretDraftEncryptedDescriptor
	recoveryMaterialization                                                       *entity.RuntimeSecretMaterialization
	id, ref, kind, state, actorID, intentDigest, claimantID, failure, correlation string
	draftVersion, secretVersion, currentRevision, targetRevision, claimGeneration int64
	expires                                                                       time.Time
	lease                                                                         *time.Time
	snapshot, encryptedCleanup, materializationCleanup                            []byte
	cleanupCompleted                                                              bool
}

// scanSecretDraft scans the fixed draft/secret/owner projection and decodes a nonempty encrypted
// descriptor. NoRows maps to NotFound and other scan/JSON errors to Unavailable; partially
// populated data remains in the error result and must not be used as success. It does not
// authorize or validate a state transition.
// scanSecretDraft сканирует фиксированную проекцию черновика, секрета и владельца, декодируя
// непустой зашифрованный дескриптор. NoRows даёт NotFound, другие ошибки чтения и JSON —
// Unavailable; частичные поля при ошибке не являются успешным результатом. Полномочия и переход
// состояния не проверяет.
func scanSecretDraft(row pgx.Row) (secretDraftRow, error) {
	var d secretDraftRow
	var raw []byte
	err := row.Scan(&d.id, &d.public.Ref, &d.public.Version, &d.public.Generation, &d.public.ProjectRef, &d.public.SecretRef, &d.public.Name, &d.public.Description, &d.public.ValueType,
		&d.public.State, &d.public.PublishedRevision, &d.public.CreatedAt, &d.public.UpdatedAt, &d.public.ExpiresAt, &d.ownerID, &d.contentDigest, &raw, &d.secretID, &d.projectID, &d.namespace, &d.stagingNamespace, &d.secretState, &d.public.SecretVersion, &d.public.ScopeKind, &d.public.OrganizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, errs.ErrNotFound
	}
	if err != nil {
		return d, errs.ErrUnavailable
	}
	if len(raw) > 0 && json.Unmarshal(raw, &d.encrypted) != nil {
		return d, errs.ErrUnavailable
	}
	return d, nil
}

// lockSecretDraft locks the exact organization/ref secret and draft rows using the caller
// transaction and delegates projection decoding/error mapping to scanSecretDraft. The lock does
// not replace subsequent actor, permission, version or expiry checks.
// lockSecretDraft блокирует строки секрета и черновика точных организации и ссылки в переданной
// транзакции, передавая декодирование и ошибки scanSecretDraft. Блокировка не заменяет проверки
// субъекта, прав, версии и срока.
func (r *Repository) lockSecretDraft(ctx context.Context, tx pgx.Tx, org, ref string) (secretDraftRow, error) {
	return scanSecretDraft(tx.QueryRow(ctx, querySecretDraftLock, pgx.StrictNamedArgs{"organization_id": org, "draft_ref": ref}))
}

// lockSecretDraftOperation locks and scans the exact organization/operation row, then decodes any
// pending encrypted/materialization cleanup descriptors. NoRows is NotFound and scan/decode
// failures Unavailable with a possibly partial private row; the caller owns fence, actor and state
// checks.
// lockSecretDraftOperation блокирует и читает точную операцию организации, затем декодирует
// имеющиеся дескрипторы очистки зашифрованных и материализованных данных. NoRows — NotFound,
// ошибки чтения и декодирования — Unavailable с возможной частичной строкой; субъект, поколение и
// состояние проверяются вызывающим.
func (r *Repository) lockSecretDraftOperation(ctx context.Context, tx pgx.Tx, org, ref string) (secretDraftOperationRow, error) {
	var o secretDraftOperationRow
	err := tx.QueryRow(ctx, querySecretDraftOperationLock, pgx.StrictNamedArgs{"organization_id": org, "operation_ref": ref}).Scan(&o.id, &o.ref, &o.kind, &o.state, &o.actorID, &o.draftVersion, &o.secretVersion, &o.currentRevision, &o.targetRevision, &o.intentDigest, &o.claimantID, &o.claimGeneration, &o.expires, &o.lease, &o.failure, &o.snapshot, &o.encryptedCleanup, &o.materializationCleanup, &o.cleanupCompleted, &o.correlation)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, errs.ErrNotFound
	}
	if err != nil {
		return o, errs.ErrUnavailable
	}
	if len(o.encryptedCleanup) > 0 && json.Unmarshal(o.encryptedCleanup, &o.recoveryEncrypted) != nil ||
		len(o.materializationCleanup) > 0 && json.Unmarshal(o.materializationCleanup, &o.recoveryMaterialization) != nil {
		return o, errs.ErrUnavailable
	}
	return o, nil
}

// lookupSecretDraftOperation resolves operation/draft refs within an organization by explicit
// operation ref, token digest or actor/kind/idempotency key. An empty actor becomes the zero UUID
// for typed SQL arguments; only the key branch checks actor/kind. NoRows is NotFound and other
// errors Unavailable; lookup success does not authorize consumption.
// lookupSecretDraftOperation разрешает ссылки операции и черновика в организации по ссылке
// операции, хешу токена либо субъекту, виду и ключу идемпотентности. Пустой субъект заменяется
// нулевым UUID для SQL; только ветвь ключа ограничена субъектом и видом. NoRows — NotFound, прочие
// ошибки — Unavailable; найденная операция ещё не разрешает исполнение.
func (r *Repository) lookupSecretDraftOperation(ctx context.Context, tx pgx.Tx, org, actor, kind, key, ref, token string) (string, string, error) {
	var operationRef, draftRef string
	if actor == "" {
		actor = "00000000-0000-0000-0000-000000000000"
	}
	err := tx.QueryRow(ctx, querySecretDraftOperationLookup, pgx.StrictNamedArgs{"organization_id": org, "actor_id": actor, "kind": kind, "idempotency_key": key, "operation_ref": ref, "token_digest": token}).Scan(&operationRef, &draftRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrNotFound
	}
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	return operationRef, draftRef, nil
}

// secretDraftAccess requires the exact draft owner and any authority-project pin, then validates
// organization/project scope consistency. Organization resources use the dedicated runtime
// permission; project creation targets the project and other permissions target the secret. Access
// denial is hidden as NotFound, while target-resolution errors propagate. It does not check
// version, freshness or draft expiry.
// secretDraftAccess требует точного владельца черновика и совпадения ограничивающего проекта,
// проверяет согласованность области организации и проекта. Для организации используется отдельное
// runtime право; создание в проекте адресует проект, остальные права — секрет. Отказ доступа
// скрывается как NotFound, ошибки разрешения цели передаются. Версия, свежесть и срок черновика
// проверяются отдельно.
func (r *Repository) secretDraftAccess(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, permission string) error {
	if s.actorID != d.ownerID || s.authorityProjectID != "" && s.authorityProjectID != d.projectID {
		return errs.ErrNotFound
	}
	if d.public.ScopeKind == "ORGANIZATION" {
		if d.public.OrganizationRef != s.organizationRef || d.projectID != "" || d.public.ProjectRef != "" {
			return errs.ErrNotFound
		}
		return r.requireOrganizationRuntimeResourceAccess(ctx, tx, s, permission)
	}
	if d.public.ScopeKind != "PROJECT" || d.public.OrganizationRef != s.organizationRef || d.projectID == "" || d.public.ProjectRef == "" {
		return errs.ErrNotFound
	}
	kind, ref := "SECRET", d.public.SecretRef
	if permission == "secret.create" {
		kind, ref = "PROJECT", d.public.ProjectRef
	}
	target, err := r.resolveAccessTarget(ctx, tx, s.organizationID, entity.AccessScope{ResourceKind: kind, ResourceRef: ref})
	if err != nil {
		return err
	}
	if err = r.requireAccess(ctx, tx, s, permission, target); err != nil {
		return errs.ErrNotFound
	}
	return nil
}

// secretDraftOwnerScope re-resolves an active persisted operation actor within the same
// organization and reconstructs its current membership role. Any query/scan failure becomes
// Forbidden and an empty scope; the returned scope still needs resource-specific authorization and
// does not inherit broker permissions.
// secretDraftOwnerScope повторно разрешает активного сохранённого субъекта операции внутри той же
// организации и его актуальную роль. Любая ошибка запроса или Scan даёт Forbidden и пустую
// область; права конкретного ресурса проверяются отдельно, права брокера не наследуются.
func (r *Repository) secretDraftOwnerScope(ctx context.Context, tx pgx.Tx, s scope, actorID string) (scope, error) {
	var ref, org string
	if tx.QueryRow(ctx, querySecretDraftOwnerScope, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": actorID}).Scan(&ref, &org) != nil {
		return scope{}, errs.ErrForbidden
	}
	var owner scope
	if tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, ref, org).Scan(&owner.organizationID, &owner.organizationRef, &owner.actorID, &owner.actorRef, &owner.actorName, &owner.role) != nil {
		return scope{}, errs.ErrForbidden
	}
	return owner, nil
}

// GetRuntimeSecretDraft reads the exact organization/ref draft in a read-only repeatable-read
// transaction and requires its owner plus secret.view, or secret.create for a provisioning secret.
// Only public metadata is returned after commit; read/authorization failures return an empty draft
// and commit failure is Unavailable. Deferred request-context rollback errors are ignored.
// GetRuntimeSecretDraft читает точный черновик организации в read-only repeatable-read транзакции,
// проверяя владельца и secret.view либо secret.create для PROVISIONING секрета. После commit
// возвращаются только публичные метаданные; при ошибках результат пуст, ошибка commit —
// Unavailable. Ошибка отложенного rollback с request context игнорируется.
func (r *Repository) GetRuntimeSecretDraft(ctx context.Context, p value.Principal, ref string) (entity.RuntimeSecretDraft, error) {
	s, tx, err := r.beginScopedSnapshotRead(ctx, p)
	if err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanSecretDraft(tx.QueryRow(ctx, querySecretDraftGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "draft_ref": ref}))
	if err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	permission := "secret.view"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return entity.RuntimeSecretDraft{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RuntimeSecretDraft{}, errs.ErrUnavailable
	}
	return d.public, nil
}

// updateSecretDraft updates a locked draft at its exact current version and synchronizes returned
// version/time, state and published revision in the private row. A nil encrypted descriptor
// preserves the stored descriptor; encoding failure is Invalid and any update Scan failure
// Conflict. DISCARDED/EXPIRED cancels prepared impact plans; cancellation failure requires caller
// rollback, since the local row may already have changed. No commit or external cleanup is
// performed.
// updateSecretDraft обновляет заблокированный черновик при точной версии, синхронизируя в закрытой
// строке версию, время, состояние и опубликованную ревизию. Nil дескриптор сохраняет прежний;
// ошибка кодирования — Invalid, любой сбой Scan обновления — Conflict. DISCARDED/EXPIRED отменяет
// PREPARED планы; при ошибке отмены вызывающий откатывает транзакцию, локальная строка уже могла
// измениться. Commit и внешняя очистка не выполняются.
func (r *Repository) updateSecretDraft(ctx context.Context, tx pgx.Tx, d *secretDraftRow, state string, encrypted *entity.RuntimeSecretDraftEncryptedDescriptor, published int64) error {
	var raw any
	if encrypted != nil {
		encoded, err := json.Marshal(encrypted)
		if err != nil {
			return errs.ErrInvalid
		}
		raw = string(encoded)
	}
	err := tx.QueryRow(ctx, querySecretDraftUpdate, pgx.StrictNamedArgs{"draft_id": d.id, "version": d.public.Version, "state": state, "encrypted": raw, "published_revision": published}).Scan(&d.public.Version, &d.public.UpdatedAt)
	if err != nil {
		return errs.ErrConflict
	}
	d.public.State = state
	if state == "DISCARDED" || state == "EXPIRED" {
		if _, err := tx.Exec(ctx, querySecretDraftImpactCancel, pgx.StrictNamedArgs{"draft_id": d.id}); err != nil {
			return errs.ErrUnavailable
		}
	}
	d.public.PublishedRevision = published
	if encrypted != nil {
		d.encrypted = encrypted
	}
	return nil
}

// auditSecretDraft inserts metadata-only audit with a generated ref, operation actor/correlation
// and a lowercased runtime-secret-draft action inside the caller transaction. Reference or insert
// failure is Unavailable; the caller must pair audit atomically with the owner change.
// auditSecretDraft вставляет аудит метаданных с новой ссылкой, субъектом и корреляцией операции,
// действием runtime-secret-draft с видом в нижнем регистре, в транзакции вызывающего. Ошибки
// ссылки и вставки — Unavailable; атомарную связь с изменением обеспечивает вызывающий.
func (r *Repository) auditSecretDraft(ctx context.Context, tx pgx.Tx, s scope, d secretDraftRow, o secretDraftOperationRow, outcome string) error {
	ref, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, querySecretDraftAudit, pgx.StrictNamedArgs{"ref": ref, "organization_id": s.organizationID, "project_id": d.projectID, "actor_id": o.actorID, "action": "runtime-secret-draft." + strings.ToLower(o.kind), "secret_ref": d.public.SecretRef, "outcome": outcome, "correlation_ref": o.correlation})
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// secretDraftGrantExpiry returns the earlier of the runtime operation TTL after now and five
// minutes after the principal authentication time. It may return a past deadline; validation of
// authentication freshness and eligibility belongs to the caller.
// secretDraftGrantExpiry возвращает более ранний срок: TTL runtime операции от now либо пять минут
// после аутентификации субъекта. Срок может быть в прошлом; свежесть аутентификации и допустимость
// операции проверяет вызывающий.
func secretDraftGrantExpiry(p value.Principal, now time.Time) time.Time {
	expires := now.Add(runtimeSecretOperationTTL)
	freshUntil := p.CredentialAuthenticatedAt.Add(5 * time.Minute)
	if freshUntil.Before(expires) {
		expires = freshUntil
	}
	return expires
}

// PrepareRuntimeSecretDraft prepares SAVE, VALIDATE, PUBLISH or DISCARD in a serializable owner
// transaction with database time, exact target locks and actor-bound idempotency intent. Completed
// replay validates its saved scope; eligible PREPARED replay rotates the grant hash without
// changing owner pins. Fresh paths check versions, expiry and concurrent work; PUBLISH reserves a
// monotonic target and binds impact, DISCARD changes terminal metadata. Grant hash, operation,
// audit and state commit together; the raw grant is returned only in the authorized receipt, never
// persisted. Non-SAVE callers must supply a non-nil ExpectedVersion and a validated kind; upstream
// freshness/input validation is not repeated here. Commit failure is Conflict.
// PrepareRuntimeSecretDraft подготавливает SAVE, VALIDATE, PUBLISH или DISCARD в serializable
// транзакции владельца со временем БД, точными блокировками и намерением идемпотентности субъекта.
// Завершённый повтор проверяет сохранённую область; допустимый PREPARED повтор меняет хеш
// разрешения, сохраняя привязки. Новые пути проверяют версии, срок и конкурирующую работу; PUBLISH
// закрепляет монотонный номер и impact, DISCARD меняет итоговые метаданные. Хеш разрешения,
// операция, аудит и состояние фиксируются вместе; исходное разрешение возвращается только
// авторизованной квитанцией, не сохраняется. Для non-SAVE требуются ненулевой ExpectedVersion и
// проверенный вид; внешние проверки свежести и входа здесь не повторяются. Ошибка commit —
// Conflict.
func (r *Repository) PrepareRuntimeSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftPrepareInput) (entity.RuntimeSecretDraftOperationReceipt, error) {
	var empty entity.RuntimeSecretDraftOperationReceipt
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	var d secretDraftRow
	var secret lockedRuntimeSecret
	if input.Kind == "SAVE" {
		decoded, decodeErr := hex.DecodeString(input.ExpectedContentSHA256)
		if decodeErr != nil || len(decoded) != 32 || strings.ToLower(input.ExpectedContentSHA256) != input.ExpectedContentSHA256 {
			return empty, errs.ErrInvalid
		}
		kind := "ROTATE"
		if input.SecretRef == "" {
			kind = "CREATE"
		}
		secret, err = r.prepareRuntimeSecretTarget(ctx, tx, s, p, repoport.RuntimeSecretPrepareInput{ScopeKind: input.ScopeKind, Kind: kind, ProjectRef: input.ProjectRef, SecretRef: input.SecretRef, Name: input.Name, Description: input.Description, ValueType: input.ValueType, Mutation: input.Mutation})
		if err != nil {
			return empty, err
		}
		if kind == "CREATE" {
			var creatorMatches bool
			if tx.QueryRow(ctx, querySecretDraftCreatorMatches, pgx.StrictNamedArgs{"secret_id": secret.id, "actor_id": s.actorID}).Scan(&creatorMatches) != nil {
				return empty, errs.ErrUnavailable
			}
			if !creatorMatches {
				return empty, errs.ErrNotFound
			}
		}
	} else {
		d, err = r.lockSecretDraft(ctx, tx, s.organizationID, input.DraftRef)
		if err != nil {
			return empty, err
		}
		secret, err = r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
		if err != nil {
			return empty, err
		}
		permission := "secret.rotate"
		if secret.state == "PROVISIONING" {
			permission = "secret.create"
		}
		if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
			return empty, err
		}
	}
	opRef, existingDraftRef, lookupErr := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, s.actorID, input.Kind, input.Mutation.IdempotencyKey, "", "")
	if lookupErr == nil {
		if input.Kind == "SAVE" {
			d, err = r.lockSecretDraft(ctx, tx, s.organizationID, existingDraftRef)
			if err != nil {
				return empty, err
			}
		}
		if d.public.Ref != existingDraftRef || d.secretID != secret.id || d.ownerID != s.actorID {
			return empty, errs.ErrConflict
		}
		o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, opRef)
		if err != nil {
			return empty, err
		}
		if o.intentDigest != input.Mutation.IntentDigest {
			return empty, errs.ErrConflict
		}
		result := entity.RuntimeSecretDraftOperationReceipt{OperationRef: o.ref, State: o.state, FailureCode: o.failure, Draft: d.public}
		if o.state == "COMPLETED" {
			var saved entity.RuntimeSecretDraftResult
			if json.Unmarshal(o.snapshot, &saved) != nil || !validSecretDraftResultScope(d.public, saved) {
				return empty, errs.ErrUnavailable
			}
			result.Draft = saved.Draft
			result.TerminalSecret = saved.Secret
		} else if o.state == "PREPARED" && o.draftVersion == d.public.Version && now.Before(d.public.ExpiresAt) {
			grant, token, err := newRuntimeSecretGrant()
			if err != nil {
				return empty, errs.ErrUnavailable
			}
			expires := secretDraftGrantExpiry(p, now)
			if tx.QueryRow(ctx, querySecretDraftOperationReissue, pgx.StrictNamedArgs{"operation_id": o.id, "token_digest": token, "grant_expires_at": expires}).Scan(&result.ExpiresAt) != nil {
				return empty, errs.ErrConflict
			}
			result.OperationGrant = grant
		} else if o.state != "FAILED" {
			return empty, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(lookupErr, errs.ErrNotFound) {
		return empty, lookupErr
	}
	if input.Kind == "SAVE" {
		if secret.valueType != input.ValueType || secret.state != "ACTIVE" && secret.state != "PROVISIONING" || input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != secret.version {
			return empty, errs.ErrVersionMismatch
		}
		if input.SecretRef == "" && secret.state != "PROVISIONING" {
			return empty, errs.ErrConflict
		}
		ref, err := newRef("sdft")
		if err != nil {
			return empty, errs.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, querySecretDraftInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": s.organizationID, "secret_id": secret.id, "actor_id": s.actorID, "content_sha256": input.ExpectedContentSHA256, "staged_namespace": r.runtimeSecretStagingNamespace}); err != nil {
			return empty, mapWriteError(err)
		}
		d, err = r.lockSecretDraft(ctx, tx, s.organizationID, ref)
		if err != nil {
			return empty, err
		}
	} else {
		if *input.Mutation.ExpectedVersion != d.public.Version {
			return empty, errs.ErrVersionMismatch
		}
		if !now.Before(d.public.ExpiresAt) {
			return empty, errs.ErrConflict
		}
		var active bool
		if tx.QueryRow(ctx, querySecretDraftOperationActive, pgx.StrictNamedArgs{"draft_id": d.id}).Scan(&active) != nil {
			return empty, errs.ErrUnavailable
		}
		if active {
			return empty, errs.ErrConflict
		}
		switch input.Kind {
		case "VALIDATE":
			if d.public.State != "DRAFT" && d.public.State != "VALID" {
				return empty, errs.ErrConflict
			}
		case "PUBLISH":
			var publishing bool
			if tx.QueryRow(ctx, querySecretDraftPublishingActive, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&publishing) != nil {
				return empty, errs.ErrUnavailable
			}
			if publishing {
				return empty, errs.ErrConflict
			}
			if d.public.State != "VALID" || secret.version != input.ExpectedSecretVersion || secret.state != "ACTIVE" && secret.state != "PROVISIONING" {
				return empty, errs.ErrVersionMismatch
			}
			if _, active, err := r.lockActiveRuntimeSecretMutation(ctx, tx, secret.id); err != nil {
				return empty, err
			} else if active {
				return empty, errs.ErrConflict
			}
			if err = r.updateSecretDraft(ctx, tx, &d, "PUBLISHING", nil, 0); err != nil {
				return empty, err
			}
		case "DISCARD":
			if d.public.State != "DRAFT" && d.public.State != "VALID" && d.public.State != "FAILED" {
				return empty, errs.ErrConflict
			}
			if err = r.updateSecretDraft(ctx, tx, &d, "DISCARDED", nil, 0); err != nil {
				return empty, err
			}
		}
	}
	targetRevision := int64(0)
	if input.Kind == "PUBLISH" {
		if tx.QueryRow(ctx, querySecretDraftTargetRevision, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&targetRevision) != nil {
			return empty, errs.ErrUnavailable
		}
	}
	grant, token, err := newRuntimeSecretGrant()
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	opRef, err = newRef("sdop")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	expires := secretDraftGrantExpiry(p, now)
	_, err = tx.Exec(ctx, querySecretDraftOperationInsert, pgx.StrictNamedArgs{"ref": opRef, "organization_id": s.organizationID, "draft_id": d.id, "actor_id": s.actorID, "kind": input.Kind, "draft_version": d.public.Version, "secret_version": secret.version, "current_revision": secret.currentRevision, "target_revision": targetRevision, "token_digest": token, "idempotency_key": input.Mutation.IdempotencyKey, "intent_digest": input.Mutation.IntentDigest, "grant_expires_at": expires, "correlation_ref": p.CorrelationRef})
	if err != nil {
		return empty, mapWriteError(err)
	}
	if input.Kind == "PUBLISH" {
		if err = r.bindSecretDraftImpact(ctx, tx, s, d, input, opRef); err != nil {
			return empty, err
		}
	}
	o := secretDraftOperationRow{ref: opRef, kind: input.Kind, actorID: s.actorID, correlation: p.CorrelationRef}
	if err = r.auditSecretDraft(ctx, tx, s, d, o, "SUCCEEDED"); err != nil {
		return empty, err
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return entity.RuntimeSecretDraftOperationReceipt{OperationRef: opRef, OperationGrant: grant, State: "PREPARED", ExpiresAt: expires, Draft: d.public}, nil
}

// ConsumeRuntimeSecretDraft requires the exact worker operation, a valid claimant and bounded
// grant, then resolves its SHA-256 lookup in a serializable transaction. It locks
// draft/operation/secret, re-resolves the persisted owner and current create/rotate authority, and
// checks database-time draft eligibility plus PUBLISH revision pins. Atomic PREPARED-to-CLAIMED
// consumption advances generation and assigns a grant-capped lease; work is returned only after
// commit. It performs no encryption, Kubernetes apply or publication.
// ConsumeRuntimeSecretDraft требует точной worker операции, допустимого заявителя и ограниченного
// разрешения, затем разрешает его SHA-256 в serializable транзакции. Блокирует черновик, операцию
// и секрет, повторно разрешает сохранённого владельца и create/rotate право, проверяет срок
// черновика по БД и PUBLISH ревизии. Атомарное потребление PREPARED в CLAIMED увеличивает
// поколение и назначает аренду не дольше разрешения; работа возвращается после commit. Шифрование,
// Kubernetes apply и публикация здесь не выполняются.
func (r *Repository) ConsumeRuntimeSecretDraft(ctx context.Context, p value.Principal, input repoport.RuntimeSecretDraftWorkInput) (entity.RuntimeSecretDraftWork, error) {
	var empty entity.RuntimeSecretDraftWork
	if !validRuntimeSecretWorkPrincipal(p, "platform.runtime-secret-drafts.operations.consume") || !validRuntimeSecretClaimant(input.ClaimantID) || len(input.OperationGrant) < 32 || len(input.OperationGrant) > 256 {
		return empty, errs.ErrForbidden
	}
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := sha256.Sum256([]byte(input.OperationGrant))
	opRef, draftRef, err := r.lookupSecretDraftOperation(ctx, tx, s.organizationID, "", "", "", "", hex.EncodeToString(hash[:]))
	if err != nil {
		return empty, err
	}
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, draftRef)
	if err != nil {
		return empty, err
	}
	o, err := r.lockSecretDraftOperation(ctx, tx, s.organizationID, opRef)
	if err != nil {
		return empty, err
	}
	owner, err := r.secretDraftOwnerScope(ctx, tx, s, o.actorID)
	if err != nil {
		return empty, err
	}
	permission := "secret.rotate"
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
	if err != nil {
		return empty, err
	}
	if secret.state == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, owner, d, permission); err != nil {
		return empty, err
	}
	var now time.Time
	if tx.QueryRow(ctx, querySecretDraftClock).Scan(&now) != nil {
		return empty, errs.ErrUnavailable
	}
	if o.state != "PREPARED" || o.draftVersion != d.public.Version || !now.Before(d.public.ExpiresAt) || o.kind != "DISCARD" && (d.public.State == "DISCARDED" || d.public.State == "EXPIRED" || d.public.State == "PUBLISHED") {
		return empty, errs.ErrConflict
	}
	if o.kind == "PUBLISH" && (secret.version != o.secretVersion || secret.currentRevision != o.currentRevision) {
		return empty, errs.ErrVersionMismatch
	}
	o.claimantID = input.ClaimantID
	var deadline time.Time
	if tx.QueryRow(ctx, querySecretDraftOperationConsume, pgx.StrictNamedArgs{"operation_id": o.id, "claimant_id": input.ClaimantID}).Scan(&o.claimGeneration, &deadline) != nil {
		return empty, errs.ErrConflict
	}
	o.lease = &deadline
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return secretDraftWork(d, o), nil
}

// secretDraftWork builds the broker work projection from validated private rows, deriving a stable
// staged object name from the first 16 SHA-256 bytes of the draft ref and the fixed ciphertext
// key. It includes lease when present and unfinished recovery descriptors only; it does not read
// secret bytes or independently authorize external effects.
// secretDraftWork формирует работу брокера из проверенных закрытых строк, выводя стабильное имя
// подготовленного объекта из первых 16 байт SHA-256 ссылки черновика и фиксированный ключ
// ciphertext. Добавляет имеющуюся аренду и только незавершённые дескрипторы восстановления;
// значения секретов не читает и внешние эффекты самостоятельно не разрешает.
func secretDraftWork(d secretDraftRow, o secretDraftOperationRow) entity.RuntimeSecretDraftWork {
	digest := sha256.Sum256([]byte(d.public.Ref))
	work := entity.RuntimeSecretDraftWork{OperationRef: o.ref, Kind: o.kind, Draft: d.public, ExpectedContentSHA256: d.contentDigest, Namespace: d.namespace, StagedNamespace: d.stagingNamespace, StagedSecretName: "runtime-secret-draft-" + hex.EncodeToString(digest[:16]), StagedSecretKey: "ciphertext", TargetRevision: o.targetRevision, Encrypted: d.encrypted, ClaimantID: o.claimantID, ClaimGeneration: o.claimGeneration, ExpiresAt: o.expires}
	if o.lease != nil {
		work.LeaseDeadline = *o.lease
	}
	if !o.cleanupCompleted {
		work.RecoveryEncrypted = o.recoveryEncrypted
		work.RecoveryMaterialization = o.recoveryMaterialization
	}
	return work
}
