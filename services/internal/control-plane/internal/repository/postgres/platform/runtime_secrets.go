package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const (
	// runtimeSecretOperationTTL bounds each newly issued or reissued operation grant to one minute; claiming has its own shorter lease.
	// runtimeSecretOperationTTL ограничивает каждый выданный или повторно выданный grant операции одной минутой; захват имеет отдельную более короткую аренду.
	runtimeSecretOperationTTL = time.Minute
	// runtimeSecretClaimLease bounds the broker claim lease used for completion and recovery; it is not a secret revision lifetime.
	// runtimeSecretClaimLease ограничивает аренду захвата брокера для завершения и восстановления; это не срок жизни ревизии секрета.
	runtimeSecretClaimLease = 30 * time.Second
	// runtimeSecretRevealMaxAge requires recent credential authentication when preparing REVEAL, independently of ordinary session validity.
	// runtimeSecretRevealMaxAge требует недавнюю аутентификацию учётными данными при подготовке REVEAL независимо от обычной действительности сессии.
	runtimeSecretRevealMaxAge = 5 * time.Minute
	// runtimeSecretKey fixes the single Kubernetes data key in operation and materialization descriptors; the value itself is not stored here.
	// runtimeSecretKey закрепляет единственный ключ данных Kubernetes в дескрипторах операции и материализации; само значение здесь не хранится.
	runtimeSecretKey = "value"
	// runtimeSecretRecoveryPage provides the recovery work page size when the trusted broker omits an explicit size.
	// runtimeSecretRecoveryPage задаёт размер страницы работ восстановления, когда доверенный брокер не указал его явно.
	runtimeSecretRecoveryPage = int32(50)
	// runtimeSecretRecoveryMax caps each recovery query page; the extra fetched row is used only to determine continuation.
	// runtimeSecretRecoveryMax ограничивает страницу запроса восстановления; дополнительная строка нужна лишь для определения продолжения.
	runtimeSecretRecoveryMax = int32(100)
)

// runtimeSecretRecoveryCursorVersion versions the deadline/ref cursor encoding so a different wire shape is rejected rather than guessed.
// runtimeSecretRecoveryCursorVersion версионирует кодирование курсора deadline/ref, чтобы другая форма отклонялась без догадок.
const runtimeSecretRecoveryCursorVersion = "v1"

// runtimeSecretListCursorVersion versions the filtered secret-list cursor independently of the recovery work cursor.
// runtimeSecretListCursorVersion версионирует курсор фильтрованного списка секретов отдельно от курсора работ восстановления.
const runtimeSecretListCursorVersion = "v1"

// runtimeSecretListCursor carries a format version, filter digest and last secret ref; it conveys pagination position, not authority.
// runtimeSecretListCursor содержит версию формата, digest фильтра и последнюю ссылку секрета; передаёт позицию страницы, а не полномочия.
type runtimeSecretListCursor struct {
	Version, FilterDigest, Ref string
}

// lockedRuntimeSecret holds the SQL-locked secret metadata and revision counters used by commands; it contains no plaintext secret material.
// lockedRuntimeSecret хранит заблокированные в SQL метаданные секрета и счётчики ревизий для команд; открытого значения секрета не содержит.
type lockedRuntimeSecret struct {
	scopeKind, organizationRef                                                     string
	id, ref, projectID, projectRef, name, description, valueType, state, namespace string
	version, currentRevision                                                       int64
	createdAt, updatedAt                                                           time.Time
}

// lockedRuntimeSecretOperation joins a locked operation with current secret metadata, expected versions, claimant generation, deadlines and terminal receipt for exact transition checks; raw secret content is absent.
// lockedRuntimeSecretOperation объединяет заблокированную операцию с текущими метаданными секрета, ожидаемыми версиями, поколением захвата, сроками и итоговой квитанцией для точных проверок перехода; исходного содержимого секрета нет.
type lockedRuntimeSecretOperation struct {
	scopeKind, organizationRef                                         string
	id, ref, kind, state, projectID, secretID, actorID, correlationRef string
	projectRef, secretRef, name, description, valueType, namespace     string
	secretState                                                        string
	expectedContentSHA256, claimantID, failureCode, intentDigest       string
	targetRevision, expectedSecretVersion, expectedCurrentRevision     int64
	claimGeneration, secretVersion, secretCurrentRevision              int64
	grantExpiresAt, secretCreatedAt, secretUpdatedAt                   time.Time
	leaseDeadline                                                      *time.Time
	terminalSnapshot                                                   []byte
}

// ListRuntimeSecrets requires secret.view, resolves the verified scope and lists either organization or authorized project metadata with bounded filters and action hints. Neither a list item nor its nextActions authorizes revealing a value.
// ListRuntimeSecrets требует secret.view, разрешает проверенную область и выдаёт метаданные организации либо разрешённых проектов с ограниченными фильтрами и подсказками действий. Элемент списка и его nextActions не разрешают раскрытие значения.
func (repository *Repository) ListRuntimeSecrets(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RuntimeSecret, string, error) {
	if principal.Permission != "secret.view" {
		return nil, "", errs.ErrForbidden
	}
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 200 {
		return nil, "", errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if filter.RuntimeResourceScopeKind == "ORGANIZATION" {
		return repository.listOrganizationRuntimeSecrets(ctx, current, filter)
	}
	if filter.RuntimeResourceScopeKind != "" && filter.RuntimeResourceScopeKind != "PROJECT" {
		return nil, "", errs.ErrInvalid
	}
	return authorizedCatalog(ctx, repository, current, "SECRET", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.RuntimeSecret, error) {
			rows, err := tx.Query(ctx, queryRuntimeSecretsList, pgx.StrictNamedArgs{
				"actor_id": current.actorID, "authority_project": current.authorityProjectID,
				"organization_id": current.organizationID, "project_ref": filter.ProjectRef,
				"query": filter.Query, "cursor_ref": cursor, "page_size": limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.RuntimeSecret
			for rows.Next() {
				item, err := scanRuntimeSecret(rows)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.RuntimeSecret) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.RuntimeSecret, allowed func(string) bool) error {
			item.NextActions = runtimeSecretActions(*item, allowed)
			return nil
		})
}

// runtimeSecretActions returns permitted rotate/reveal/revoke presentation hints only for ACTIVE metadata; mutation paths must recheck access and current state.
// runtimeSecretActions возвращает разрешённые подсказки rotate/reveal/revoke только для ACTIVE; пути изменения повторно проверяют доступ и текущее состояние.
func runtimeSecretActions(item entity.RuntimeSecret, allowed func(string) bool) []string {
	actions := make([]string, 0, 3)
	if item.State != "ACTIVE" {
		return actions
	}
	for _, operation := range []struct{ action, permission string }{
		{"ROTATE", "secret.rotate"},
		{"REVEAL", "secret.reveal"},
		{"REVOKE", "secret.revoke"},
	} {
		if operation.action == "REVEAL" && item.Purpose == "SSH_COMPOSE" {
			continue
		}
		if allowed(operation.permission) {
			actions = append(actions, operation.action)
		}
	}
	return actions
}

// runtimeSecretListFilterDigest hashes the project/query pair with an unambiguous separator to bind pagination to the same filter; this unkeyed digest is not a signature.
// runtimeSecretListFilterDigest хеширует пару project/query с однозначным разделителем для привязки страницы к прежнему фильтру; этот digest без ключа не является подписью.
func runtimeSecretListFilterDigest(projectRef, queryValue string) string {
	digest := sha256.Sum256([]byte(projectRef + "\x00" + queryValue))
	return hex.EncodeToString(digest[:])
}

// encodeRuntimeSecretListCursor encodes the bounded secret ref with its filter digest as URL-safe JSON; invalid ref shape fails before encoding and ownership remains a query concern.
// encodeRuntimeSecretListCursor кодирует ограниченную ссылку секрета с digest фильтра как URL-safe JSON; неверная форма отклоняется до кодирования, а владение проверяется запросом.
func encodeRuntimeSecretListCursor(projectRef, queryValue, ref string) (string, error) {
	if !strings.HasPrefix(ref, "sec_") || len(ref) > 96 {
		return "", errs.ErrInvalid
	}
	raw, err := json.Marshal(runtimeSecretListCursor{Version: runtimeSecretListCursorVersion,
		FilterDigest: runtimeSecretListFilterDigest(projectRef, queryValue), Ref: ref})
	if err != nil {
		return "", errs.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// decodeRuntimeSecretListCursor accepts an empty initial cursor or decodes a bounded versioned cursor whose filter/ref match the request. It validates pagination syntax and binding, not tenant access or a cryptographic proof.
// decodeRuntimeSecretListCursor принимает пустой начальный курсор либо разбирает ограниченный версионированный курсор с совпадающими filter/ref. Проверяет синтаксис и привязку страницы, а не доступ арендатора или криптографическое доказательство.
func decodeRuntimeSecretListCursor(token, projectRef, queryValue string) (runtimeSecretListCursor, error) {
	if token == "" {
		return runtimeSecretListCursor{}, nil
	}
	if len(token) > 512 {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	var cursor runtimeSecretListCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != runtimeSecretListCursorVersion ||
		cursor.FilterDigest != runtimeSecretListFilterDigest(projectRef, queryValue) ||
		!strings.HasPrefix(cursor.Ref, "sec_") || len(cursor.Ref) > 96 {
		return runtimeSecretListCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

// GetRuntimeSecret reads metadata in a repeatable-read transaction, hides foreign or unauthorized resources as NotFound and derives action hints from current organization/project access. It never returns the secret value.
// GetRuntimeSecret читает метаданные в repeatable-read транзакции, скрывает чужие или недоступные ресурсы как NotFound и выводит подсказки действий из текущего доступа организации/проекта. Значение секрета не возвращает.
func (repository *Repository) GetRuntimeSecret(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeSecret, error) {
	if principal.Permission != "secret.view" {
		return entity.RuntimeSecret{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanRuntimeSecret(tx.QueryRow(ctx, queryRuntimeSecretGet, pgx.StrictNamedArgs{"organization_id": current.organizationID, "secret_ref": ref}))
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if item.ScopeKind == "ORGANIZATION" {
		if item.OrganizationRef != current.organizationRef || item.ProjectRef != "" || repository.requireOrganizationRuntimeResourceAccess(ctx, tx, current, "secret.view") != nil {
			return entity.RuntimeSecret{}, errs.ErrNotFound
		}
		item.NextActions = runtimeSecretActions(item, func(permission string) bool {
			return repository.requireOrganizationRuntimeResourceAccess(ctx, tx, current, permission) == nil
		})
		if tx.Commit(ctx) != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		return item, nil
	}
	if item.ScopeKind != "PROJECT" || item.OrganizationRef != current.organizationRef || item.ProjectRef == "" {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "SECRET", ResourceRef: ref})
	if err != nil || principal.ProjectRef != "" && principal.ProjectRef != target.projectID {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.view", target); err != nil {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	at := time.Now().UTC()
	item.NextActions = runtimeSecretActions(item, func(permission string) bool {
		return accessservice.Evaluate(subject.AccessSubject, permission, target.scope, target.ownerSubjectRef, bindings, at).Allowed
	})
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	return item, nil
}

// PrepareRuntimeSecretOperation accepts only the gateway command with its matching permission, validates reveal freshness, locks an authorized target and checks actor-scoped idempotency before preparing a short-lived grant. Creation/rotation reserve the next revision; this transaction does not materialize secret bytes.
// PrepareRuntimeSecretOperation принимает только команду шлюза с соответствующим полномочием, проверяет свежесть reveal, блокирует разрешённую цель и проверяет идемпотентность в области actor перед подготовкой короткого grant. Создание/ротация резервируют следующую ревизию; транзакция не материализует байты секрета.
func (repository *Repository) PrepareRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretPrepareInput) (platformrepo.RuntimeSecretPrepareResult, error) {
	expectedPermission := map[string]string{"CREATE": "secret.create", "ROTATE": "secret.rotate", "REVEAL": "secret.reveal", "REVOKE": "secret.revoke"}[input.Kind]
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != expectedPermission || input.Mutation.Validate() != nil || !validRuntimeSecretPrepare(input) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrInvalid
	}
	now := time.Now().UTC()
	if input.Kind == "REVEAL" && (principal.CredentialAuthenticatedAt.IsZero() || principal.CredentialAuthenticatedAt.After(now.Add(30*time.Second)) || now.Sub(principal.CredentialAuthenticatedAt) > runtimeSecretRevealMaxAge) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnauthorized
	}
	current, tx, err := repository.runtimeSecretOperationTransaction(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	secret, err := repository.prepareRuntimeSecretTarget(ctx, tx, current, principal, input)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	existing, found, err := repository.lockRuntimeSecretOperationByIdempotency(ctx, tx, current.organizationID, current.actorID, input.Kind, input.Mutation.IdempotencyKey)
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	if found {
		if existing.secretID != secret.id || existing.intentDigest != input.Mutation.IntentDigest {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		result, retryErr := repository.retryRuntimeSecretOperation(ctx, tx, existing, now)
		if retryErr != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, retryErr
		}
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		return result, nil
	}

	if err := repository.validateNewRuntimeSecretOperation(ctx, tx, current.organizationID, secret, input, now); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	targetRevision := secret.currentRevision
	if input.Kind == "CREATE" || input.Kind == "ROTATE" {
		if err := tx.QueryRow(ctx, queryRuntimeSecretMaximumRevision, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&targetRevision); err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnavailable
		}
		targetRevision++
	}
	grant, tokenDigest, err := newRuntimeSecretGrant()
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	operationRef, err := newRef("secop")
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	expiresAt := now.Add(runtimeSecretOperationTTL)
	var expectedHash any
	if input.ExpectedContentSHA256 != "" {
		expectedHash = input.ExpectedContentSHA256
	}
	if _, err := tx.Exec(ctx, queryRuntimeSecretOperationInsert, pgx.StrictNamedArgs{
		"ref": operationRef, "organization_id": current.organizationID, "project_id": secret.projectID,
		"actor_id": current.actorID, "secret_id": secret.id, "kind": input.Kind, "target_revision": targetRevision,
		"expected_secret_version": secret.version, "expected_current_revision": secret.currentRevision,
		"expected_content_sha256": expectedHash, "token_digest": tokenDigest,
		"idempotency_key": input.Mutation.IdempotencyKey, "intent_digest": input.Mutation.IntentDigest,
		"correlation_ref": principal.CorrelationRef, "grant_expires_at": expiresAt,
	}); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, mapWriteError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	return platformrepo.RuntimeSecretPrepareResult{
		OperationRef: operationRef, OperationGrant: grant, State: "PREPARED",
		ExpiresAt: expiresAt, ValueType: secret.valueType,
	}, nil
}

// runtimeSecretOperationTransaction resolves the verified principal's scope before opening a serializable secret-operation transaction. It maps only BeginTx failure to Unavailable; callers retain permission checks, rollback, locks, owner revalidation, audit and commit.
// runtimeSecretOperationTransaction разрешает область проверенного principal перед открытием сериализуемой транзакции операции секрета. Только отказ BeginTx заменяется на Unavailable; проверки полномочий, rollback, блокировки, повторная проверка владельца, аудит и commit остаются у вызывающих методов.
func (repository *Repository) runtimeSecretOperationTransaction(ctx context.Context, principal value.Principal) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	return current, tx, nil
}

// prepareRuntimeSecretTarget locks or creates the metadata target within the resolved organization/project and checks action-specific access before returning it. CREATE may insert PROVISIONING metadata; existing secret refs and caller project scope never independently confer authority.
// prepareRuntimeSecretTarget блокирует или создаёт метаданные цели внутри разрешённой организации/проекта и проверяет доступ для конкретного действия. CREATE может вставить PROVISIONING; существующая ссылка секрета и область проекта вызывающего сами по себе не дают полномочий.
func (repository *Repository) prepareRuntimeSecretTarget(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, input platformrepo.RuntimeSecretPrepareInput) (lockedRuntimeSecret, error) {
	var secret lockedRuntimeSecret
	var err error
	if input.Kind == "CREATE" {
		if input.Purpose == "" {
			input.Purpose = "APPLICATION"
		}
		if input.Purpose != "APPLICATION" && input.Purpose != "SSH_COMPOSE" || input.Purpose == "SSH_COMPOSE" && input.ScopeKind != "PROJECT" {
			return secret, errs.ErrInvalid
		}
	} else if input.Purpose != "" {
		return secret, errs.ErrInvalid
	}
	if input.Kind == "CREATE" {
		if input.ScopeKind == "ORGANIZATION" {
			return repository.prepareOrganizationRuntimeSecretTarget(ctx, tx, current, input)
		}
		if input.ScopeKind != "PROJECT" {
			return lockedRuntimeSecret{}, errs.ErrInvalid
		}
		if err := tx.QueryRow(ctx, queryRuntimeSecretLockProject, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_ref": input.ProjectRef}).Scan(&secret.projectID, &secret.projectRef); errors.Is(err, pgx.ErrNoRows) {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		} else if err != nil {
			return lockedRuntimeSecret{}, errs.ErrUnavailable
		}
		if principal.ProjectRef != "" && principal.ProjectRef != secret.projectID {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		}
		project, targetErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "PROJECT", ResourceRef: secret.projectRef})
		if targetErr != nil || repository.requireAccess(ctx, tx, current, "secret.create", project) != nil {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		}
		projectID, projectRef := secret.projectID, secret.projectRef
		secret, err = repository.lockRuntimeSecretByName(ctx, tx, current.organizationID, projectID, input.Name)
		if errors.Is(err, errs.ErrNotFound) {
			secret = lockedRuntimeSecret{scopeKind: "PROJECT", organizationRef: current.organizationRef, projectID: projectID, projectRef: projectRef, name: input.Name, description: input.Description, valueType: input.ValueType, state: "PROVISIONING", namespace: repository.runtimeSecretNamespace}
			secret.ref, err = newRef("sec")
			if err != nil {
				return lockedRuntimeSecret{}, err
			}
			if err := tx.QueryRow(ctx, queryRuntimeSecretInsert, pgx.StrictNamedArgs{
				"ref": secret.ref, "organization_id": current.organizationID, "project_id": secret.projectID,
				"namespace": secret.namespace, "name": secret.name, "description": secret.description,
				"scope_kind": secret.scopeKind,
				"value_type": secret.valueType, "actor_id": current.actorID,
				"purpose": input.Purpose,
			}).Scan(&secret.id, &secret.version, &secret.currentRevision, &secret.createdAt, &secret.updatedAt); err != nil {
				return lockedRuntimeSecret{}, mapWriteError(err)
			}
			return secret, nil
		}
		if err != nil {
			return lockedRuntimeSecret{}, err
		}
		var existingPurpose string
		if tx.QueryRow(ctx, querySSHComposeSecretPurpose, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&existingPurpose) != nil {
			return secret, errs.ErrUnavailable
		}
		if existingPurpose != input.Purpose {
			return secret, errs.ErrConflict
		}
		return secret, nil
	}

	secret, err = repository.lockRuntimeSecret(ctx, tx, current.organizationID, input.SecretRef)
	if err != nil || principal.ProjectRef != "" && principal.ProjectRef != secret.projectID {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	if input.Kind == "REVEAL" {
		var purpose string
		if tx.QueryRow(ctx, querySSHComposeSecretPurpose, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&purpose) != nil {
			return lockedRuntimeSecret{}, errs.ErrUnavailable
		}
		if purpose == "SSH_COMPOSE" {
			return lockedRuntimeSecret{}, errs.ErrForbidden
		}
	}
	permission := map[string]string{"ROTATE": "secret.rotate", "REVEAL": "secret.reveal", "REVOKE": "secret.revoke"}[input.Kind]
	if secret.scopeKind == "ORGANIZATION" {
		if secret.projectRef != "" || secret.projectID != "" || secret.organizationRef != current.organizationRef || repository.requireOrganizationRuntimeResourceAccess(ctx, tx, current, permission) != nil {
			return lockedRuntimeSecret{}, errs.ErrNotFound
		}
		return secret, nil
	}
	if secret.scopeKind != "PROJECT" || secret.organizationRef != current.organizationRef || secret.projectID == "" {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	target, targetErr := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{ResourceKind: "SECRET", ResourceRef: secret.ref})
	if targetErr != nil || repository.requireAccess(ctx, tx, current, permission, target) != nil {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	return secret, nil
}

// validateNewRuntimeSecretOperation checks active/provisioning state, expected versions and value type, rejects concurrent publishing or mutation and fails an expired prepared predecessor in the same transaction. REVOKE additionally requires no retained references.
// validateNewRuntimeSecretOperation проверяет active/provisioning, ожидаемые версии и тип значения, отклоняет параллельную публикацию или изменение и завершает просроченного подготовленного предшественника в той же транзакции. REVOKE дополнительно требует отсутствия удерживающих ссылок.
func (repository *Repository) validateNewRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, secret lockedRuntimeSecret, input platformrepo.RuntimeSecretPrepareInput, now time.Time) error {
	if input.Kind == "CREATE" {
		if secret.state != "PROVISIONING" || secret.description != input.Description || secret.valueType != input.ValueType || secret.namespace != repository.runtimeSecretNamespace {
			return errs.ErrConflict
		}
	} else {
		if secret.state != "ACTIVE" {
			return errs.ErrNotFound
		}
		if input.Kind == "ROTATE" && input.ValueType != secret.valueType {
			return errs.ErrConflict
		}
		if input.Mutation.ExpectedVersion != nil && *input.Mutation.ExpectedVersion != secret.version {
			return errs.ErrVersionMismatch
		}
	}
	if input.Kind == "CREATE" || input.Kind == "ROTATE" || input.Kind == "REVOKE" {
		var publishing bool
		if tx.QueryRow(ctx, querySecretDraftPublishingActive, pgx.StrictNamedArgs{"secret_id": secret.id}).Scan(&publishing) != nil {
			return errs.ErrUnavailable
		}
		if publishing {
			return errs.ErrConflict
		}
		active, found, err := repository.lockActiveRuntimeSecretMutation(ctx, tx, secret.id)
		if err != nil {
			return err
		}
		if found {
			if active.state != "PREPARED" || active.grantExpiresAt.After(now) {
				return errs.ErrConflict
			}
			if err := repository.failLockedRuntimeSecretOperation(ctx, tx, organizationID, active, "GRANT_EXPIRED"); err != nil {
				return err
			}
		}
	}
	if input.Kind == "REVOKE" {
		return repository.ensureRuntimeSecretUnreferenced(ctx, tx, organizationID, secret.ref)
	}
	return nil
}

// retryRuntimeSecretOperation replays a completed non-reveal metadata receipt or a failed result, rejects claimed/unknown states and reissues only an unchanged PREPARED operation. A reissued grant replaces its digest and expiry instead of creating a second operation.
// retryRuntimeSecretOperation повторно выдаёт итоговую квитанцию метаданных кроме reveal либо результат ошибки, отклоняет claimed/неизвестные состояния и перевыдаёт лишь неизменённую PREPARED операцию. Новый grant заменяет digest и срок, не создавая вторую операцию.
func (repository *Repository) retryRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, operation lockedRuntimeSecretOperation, now time.Time) (platformrepo.RuntimeSecretPrepareResult, error) {
	result := platformrepo.RuntimeSecretPrepareResult{OperationRef: operation.ref, State: operation.state, ValueType: operation.valueType, FailureCode: operation.failureCode}
	switch operation.state {
	case "COMPLETED":
		if operation.kind == "REVEAL" {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
		}
		secret, err := decodeRuntimeSecretSnapshot(operation.terminalSnapshot)
		if err != nil {
			return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrUnavailable
		}
		result.TerminalSecret = &secret
		return result, nil
	case "FAILED":
		return result, nil
	case "CLAIMED":
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	case "PREPARED":
	default:
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	if !repository.runtimeSecretOperationMatchesCurrent(operation) {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	grant, tokenDigest, err := newRuntimeSecretGrant()
	if err != nil {
		return platformrepo.RuntimeSecretPrepareResult{}, err
	}
	expiresAt := now.Add(runtimeSecretOperationTTL)
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationReissue, pgx.StrictNamedArgs{
		"operation_id": operation.id, "token_digest": tokenDigest, "grant_expires_at": expiresAt,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return platformrepo.RuntimeSecretPrepareResult{}, errs.ErrConflict
	}
	result.State, result.OperationGrant, result.ExpiresAt = "PREPARED", grant, expiresAt
	return result, nil
}

// lockActiveRuntimeSecretMutation locks the active mutation selected for one secret, distinguishes absence from query failure and leaves expiry/state decisions to its caller.
// lockActiveRuntimeSecretMutation блокирует выбранное активное изменение одного секрета, различает отсутствие и ошибку запроса, оставляя решение по сроку/состоянию вызывающему.
func (repository *Repository) lockActiveRuntimeSecretMutation(ctx context.Context, tx pgx.Tx, secretID string) (lockedRuntimeSecretOperation, bool, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretHasActiveOperation, pgx.StrictNamedArgs{"secret_id": secretID}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, false, nil
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, false, errs.ErrUnavailable
	}
	return operation, true, nil
}

// ConsumeRuntimeSecretOperation requires the exact broker consume identity, resolves a grant by its digest, rechecks preparation expiry/current secret/owner access and atomically assigns claimant generation plus lease. Returned names/descriptors guide broker work without exposing secret bytes.
// ConsumeRuntimeSecretOperation требует точную consume identity брокера, разрешает grant по digest, повторно проверяет срок подготовки, текущий секрет и доступ владельца, атомарно назначая поколение захвата и аренду. Возвращённые имена/дескрипторы направляют работу брокера без раскрытия байтов секрета.
func (repository *Repository) ConsumeRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error) {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != "platform.runtime-secrets.operations.consume" ||
		len(input.OperationGrant) < 32 || len(input.OperationGrant) > 256 || strings.TrimSpace(input.OperationGrant) != input.OperationGrant || !validRuntimeSecretClaimant(input.ClaimantID) {
		return entity.RuntimeSecretOperation{}, errs.ErrForbidden
	}
	current, tx, err := repository.runtimeSecretOperationTransaction(ctx, principal)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digest := sha256.Sum256([]byte(input.OperationGrant))
	locked, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLockByDigest, pgx.StrictNamedArgs{
		"token_digest": hex.EncodeToString(digest[:]), "organization_id": current.organizationID,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretOperation{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrUnavailable
	}
	if locked.state != "PREPARED" || !time.Now().UTC().Before(locked.grantExpiresAt) || !repository.runtimeSecretOperationMatchesCurrent(locked) {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	}
	if err := repository.authorizeRuntimeSecretOperationOwner(ctx, tx, current, locked); err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	descriptors, err := repository.runtimeSecretOperationDescriptors(ctx, tx, locked)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	leaseDeadline := time.Now().UTC().Add(runtimeSecretClaimLease)
	if err := tx.QueryRow(ctx, queryRuntimeSecretOperationConsume, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": input.ClaimantID, "claim_lease_deadline": leaseDeadline,
	}).Scan(&locked.claimGeneration, &leaseDeadline); errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	} else if err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecretOperation{}, errs.ErrConflict
	}
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.SecretName)
	}
	return entity.RuntimeSecretOperation{
		ScopeKind: locked.scopeKind, OrganizationRef: locked.organizationRef,
		Ref: locked.ref, Kind: locked.kind, ProjectRef: locked.projectRef, SecretRef: locked.secretRef,
		Name: locked.name, Description: locked.description, ValueType: locked.valueType,
		Namespace: locked.namespace, SecretKey: runtimeSecretKey, TargetRevision: locked.targetRevision,
		ExpectedContentSHA256: locked.expectedContentSHA256, ClaimGeneration: locked.claimGeneration,
		VersionedSecretNames: names, RevisionDescriptors: descriptors,
		ExpiresAt: locked.grantExpiresAt, LeaseDeadline: leaseDeadline,
	}, nil
}

// ListRuntimeSecretRecoveryWork requires the broker recovery permission and returns an organization-scoped bounded keyset page of operation metadata after the deadline/ref cursor. The extra row detects a next page; returned work still requires recovery-time validation.
// ListRuntimeSecretRecoveryWork требует полномочие восстановления брокера и возвращает ограниченную keyset-страницу метаданных операций организации после курсора deadline/ref. Дополнительная строка определяет следующую страницу; сама работа ещё требует проверки при восстановлении.
func (repository *Repository) ListRuntimeSecretRecoveryWork(ctx context.Context, principal value.Principal, page platformrepo.RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.recover") {
		return nil, "", errs.ErrForbidden
	}
	limit, err := boundedRuntimeSecretRecoveryPage(page.Size)
	if err != nil {
		return nil, "", err
	}
	cursorDeadline, cursorRef, err := decodeRuntimeSecretRecoveryCursor(page.Token)
	if err != nil {
		return nil, "", err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryRuntimeSecretRecoveryWorkList, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
		"cursor_deadline": cursorDeadline,
		"cursor_ref":      cursorRef,
		"page_size":       limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RuntimeSecretRecoveryWork, 0, limit+1)
	for rows.Next() {
		var item entity.RuntimeSecretRecoveryWork
		if err := rows.Scan(
			&item.OperationRef, &item.Kind, &item.ClaimantID, &item.ClaimGeneration,
			&item.Namespace, &item.SecretRef, &item.TargetRevision, &item.SecretKey,
			&item.ExpectedContentSHA256, &item.LeaseDeadline,
		); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeRuntimeSecretRecoveryCursor(last.LeaseDeadline, last.OperationRef)
	}
	return items, next, nil
}

// CompleteRuntimeSecretOperation requires the broker completion identity, exact claimant generation and live lease, revalidates owner access and commits secret revision, operation receipt, audit and change event together. A completed replay is accepted only for the same claimant/generation.
// CompleteRuntimeSecretOperation требует completion identity брокера, точное поколение захвата и действующую аренду, повторно проверяет доступ владельца и совместно фиксирует ревизию, квитанцию операции, аудит и событие. Повтор завершения принимается лишь для прежних claimant/generation.
func (repository *Repository) CompleteRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretCompleteInput) (entity.RuntimeSecret, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.complete") || input.OperationRef == "" || !validRuntimeSecretClaimant(input.ClaimantID) || input.ClaimGeneration < 1 {
		return entity.RuntimeSecret{}, errs.ErrForbidden
	}
	current, tx, err := repository.runtimeSecretOperationTransaction(ctx, principal)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if err := repository.authorizeRuntimeSecretOperationOwner(ctx, tx, current, locked); err != nil {
		return entity.RuntimeSecret{}, err
	}
	if locked.state == "COMPLETED" {
		if locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration {
			return entity.RuntimeSecret{}, errs.ErrConflict
		}
		secret, decodeErr := decodeRuntimeSecretSnapshot(locked.terminalSnapshot)
		if decodeErr != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		if err := tx.Commit(ctx); err != nil {
			return entity.RuntimeSecret{}, errs.ErrConflict
		}
		return secret, nil
	}
	if locked.state != "CLAIMED" || locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.leaseDeadline == nil || !time.Now().UTC().Before(*locked.leaseDeadline) {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	secret, err := repository.completeLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, input.Materialization)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "RUNTIME_SECRET_CHANGED", locked.projectRef, secret.Ref, runtimeSecretAuditSummary(locked.kind), secret.Version, secret.State); err != nil {
		return entity.RuntimeSecret{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	return secret, nil
}

// FailRuntimeSecretOperation records a closed broker failure only for the matching claimed generation, with an exact already-failed replay and transactional audit. It deliberately does not require an unexpired lease as successful completion does.
// FailRuntimeSecretOperation фиксирует закрытую ошибку брокера только для совпадающего поколения захвата, с точным повтором уже завершённой ошибки и транзакционным аудитом. В отличие от успешного завершения намеренно не требует непросроченную аренду.
func (repository *Repository) FailRuntimeSecretOperation(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretFailInput) (platformrepo.RuntimeSecretFailureResult, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.fail") || input.OperationRef == "" ||
		!validRuntimeSecretClaimant(input.ClaimantID) || input.ClaimGeneration < 1 || !validRuntimeSecretFailureCode(input.FailureCode) {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrForbidden
	}
	current, tx, err := repository.runtimeSecretOperationTransaction(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	if locked.state == "FAILED" {
		if locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.failureCode != input.FailureCode {
			return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
		}
		return platformrepo.RuntimeSecretFailureResult{OperationRef: locked.ref, State: "FAILED", FailureCode: locked.failureCode}, nil
	}
	if locked.state != "CLAIMED" || locked.claimantID != input.ClaimantID || locked.claimGeneration != input.ClaimGeneration || locked.leaseDeadline == nil {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
	}
	if err := repository.failLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, input.FailureCode); err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretFailureResult{}, errs.ErrConflict
	}
	return platformrepo.RuntimeSecretFailureResult{OperationRef: locked.ref, State: "FAILED", FailureCode: input.FailureCode}, nil
}

// RecoverRuntimeSecretMaterialization reconciles a broker-observed versioned materialization under its locked operation: default DELETE remains unless a matching retained completion, live claim or recoverable expired claim proves KEEP. Owner revocation/stale versions fail the operation; recovered completion emits the same change event atomically.
// RecoverRuntimeSecretMaterialization согласует наблюдаемую брокером версионированную материализацию с заблокированной операцией: DELETE сохраняется, пока совпадающее удержанное завершение, живая аренда или восстанавливаемый просроченный захват не докажут KEEP. Отзыв доступа/устаревшие версии завершают операцию ошибкой; восстановленное завершение атомарно пишет то же событие изменения.
func (repository *Repository) RecoverRuntimeSecretMaterialization(ctx context.Context, principal value.Principal, input platformrepo.RuntimeSecretRecoveryInput) (platformrepo.RuntimeSecretRecoveryResult, error) {
	if !validRuntimeSecretWorkPrincipal(principal, "platform.runtime-secrets.operations.recover") || input.OperationRef == "" || !validRuntimeSecretMaterialization(&input.Materialization) {
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrForbidden
	}
	if result, handled, err := repository.recoverTerminalRuntimeSecret(ctx, principal, input); handled || err != nil {
		return result, err
	}
	current, tx, err := repository.runtimeSecretOperationTransaction(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	locked, err := repository.lockRuntimeSecretOperation(ctx, tx, current.organizationID, input.OperationRef)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	if terminalRuntimeSecretRecovery(locked) {
		if err := tx.Rollback(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrUnavailable
		}
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
	}
	result := platformrepo.RuntimeSecretRecoveryResult{Action: "DELETE", OperationState: locked.state}
	if locked.state == "CLAIMED" && repository.authorizeRuntimeSecretOperationOwner(ctx, tx, current, locked) != nil {
		if err := repository.failLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, "GRANT_EXPIRED"); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, err
		}
		result.OperationState = "FAILED"
		if tx.Commit(ctx) != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.kind != "CREATE" && locked.kind != "ROTATE" || !repository.materializationMatchesOperation(locked, &input.Materialization) {
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.state != "CLAIMED" {
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if locked.leaseDeadline == nil || time.Now().UTC().Before(*locked.leaseDeadline) {
		result.Action = "KEEP"
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	if !repository.runtimeSecretOperationMatchesCurrent(locked) {
		if err := repository.failLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, "STALE_SECRET_VERSION"); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, err
		}
		result.OperationState = "FAILED"
		if err := tx.Commit(ctx); err != nil {
			return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
		}
		return result, nil
	}
	secret, err := repository.completeLockedRuntimeSecretOperation(ctx, tx, current.organizationID, locked, &input.Materialization)
	if err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "RUNTIME_SECRET_CHANGED", locked.projectRef, secret.Ref, runtimeSecretAuditSummary(locked.kind), secret.Version, secret.State); err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, err
	}
	result.Action, result.OperationState, result.Secret = "KEEP", "COMPLETED", &secret
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeSecretRecoveryResult{}, errs.ErrConflict
	}
	return result, nil
}

// completeLockedRuntimeSecretOperation applies a still-current CLAIMED operation to metadata: create/rotate insert a bound immutable descriptor, reveal selects the current descriptor and revoke rechecks references. It writes terminal receipt and audit inside the caller transaction; the outer command owns event publication and commit.
// completeLockedRuntimeSecretOperation применяет актуальную CLAIMED операцию к метаданным: create/rotate вставляют привязанный неизменяемый дескриптор, reveal выбирает текущий, revoke повторно проверяет ссылки. Итоговая квитанция и аудит пишутся в транзакции вызывающего; внешняя команда владеет событием и commit.
func (repository *Repository) completeLockedRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, materialization *entity.RuntimeSecretMaterialization) (entity.RuntimeSecret, error) {
	if locked.state != "CLAIMED" || !repository.runtimeSecretOperationMatchesCurrent(locked) {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	secret := locked.runtimeSecret()
	if tx.QueryRow(ctx, querySSHComposeSecretPurpose, pgx.StrictNamedArgs{"secret_id": locked.secretID}).Scan(&secret.Purpose) != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	switch locked.kind {
	case "CREATE", "ROTATE":
		if !validRuntimeSecretMaterialization(materialization) || !repository.materializationMatchesOperation(locked, materialization) {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		revisionRef, err := newRef("secr")
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		if _, err := tx.Exec(ctx, queryRuntimeSecretRevisionInsert, pgx.StrictNamedArgs{
			"ref": revisionRef, "secret_id": locked.secretID, "revision": locked.targetRevision,
			"namespace": materialization.Namespace, "secret_name": materialization.SecretName,
			"secret_key": materialization.SecretKey, "secret_uid": materialization.SecretUID,
			"secret_resource_version": materialization.SecretResourceVersion, "content_sha256": materialization.ContentSHA256,
		}); err != nil {
			return entity.RuntimeSecret{}, mapWriteError(err)
		}
		hintPrefix, hintSuffix := "", ""
		if materialization.DisplayHint != nil && secret.Purpose != "SSH_COMPOSE" {
			hintPrefix, hintSuffix = materialization.DisplayHint.Prefix, materialization.DisplayHint.Suffix
			secret.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: hintPrefix, Suffix: hintSuffix}
		}
		expectedState := "ACTIVE"
		if locked.kind == "CREATE" {
			expectedState = "PROVISIONING"
		}
		if err := tx.QueryRow(ctx, queryRuntimeSecretActivate, pgx.StrictNamedArgs{
			"secret_id": locked.secretID, "revision": locked.targetRevision, "hint_prefix": hintPrefix, "hint_suffix": hintSuffix,
			"expected_version": locked.expectedSecretVersion, "expected_current_revision": locked.expectedCurrentRevision,
			"expected_state": expectedState,
		}).Scan(&secret.Version, &secret.State, &secret.CurrentRevision, &hintPrefix, &hintSuffix, &secret.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
			return entity.RuntimeSecret{}, errs.ErrConflict
		} else if err != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		secret.CurrentRevisionDescriptor = &entity.RuntimeSecretRevisionDescriptor{
			Revision: locked.targetRevision, Namespace: materialization.Namespace, SecretName: materialization.SecretName,
			SecretKey: materialization.SecretKey, SecretUID: materialization.SecretUID,
			SecretResourceVersion: materialization.SecretResourceVersion, ContentSHA256: materialization.ContentSHA256,
		}
	case "REVEAL":
		if materialization != nil || locked.secretState != "ACTIVE" {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.secretCurrentRevision)
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		secret.CurrentRevisionDescriptor = &descriptor
	case "REVOKE":
		if materialization != nil {
			return entity.RuntimeSecret{}, errs.ErrInvalid
		}
		if err := repository.ensureRuntimeSecretUnreferenced(ctx, tx, organizationID, locked.secretRef); err != nil {
			return entity.RuntimeSecret{}, err
		}
		if err := tx.QueryRow(ctx, queryRuntimeSecretRevoke, pgx.StrictNamedArgs{
			"secret_id": locked.secretID, "expected_version": locked.expectedSecretVersion,
			"expected_current_revision": locked.expectedCurrentRevision,
		}).Scan(&secret.Version, &secret.State, &secret.CurrentRevision, &secret.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
			return entity.RuntimeSecret{}, errs.ErrConflict
		} else if err != nil {
			return entity.RuntimeSecret{}, errs.ErrUnavailable
		}
		secret.DisplayHint = nil
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, locked.secretID, locked.secretCurrentRevision)
		if err != nil {
			return entity.RuntimeSecret{}, err
		}
		secret.CurrentRevisionDescriptor = &descriptor
	default:
		return entity.RuntimeSecret{}, errs.ErrInvalid
	}
	if secret.Purpose == "SSH_COMPOSE" && (locked.kind == "ROTATE" || locked.kind == "REVOKE") {
		if err := repository.invalidateSSHComposeDependencies(ctx, tx, organizationID, locked.actorID, locked.correlationRef, "", locked.secretRef, ""); err != nil {
			return entity.RuntimeSecret{}, err
		}
	}
	snapshot, err := json.Marshal(secret)
	if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationComplete, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": locked.claimantID, "claim_generation": locked.claimGeneration,
		"terminal_secret_snapshot": string(snapshot),
	})
	if err != nil || tag.RowsAffected() != 1 {
		return entity.RuntimeSecret{}, errs.ErrConflict
	}
	if err := repository.insertRuntimeSecretAudit(ctx, tx, organizationID, locked, "SUCCEEDED", runtimeSecretAuditSummary(locked.kind)); err != nil {
		return entity.RuntimeSecret{}, err
	}
	return secret, nil
}

// failLockedRuntimeSecretOperation conditionally marks the exact operation/claim generation failed and inserts a failure audit in the caller transaction; a missing update is a conflict and no external Kubernetes deletion occurs here.
// failLockedRuntimeSecretOperation условно завершает ошибкой точную операцию/поколение захвата и вставляет аудит в транзакции вызывающего; отсутствие обновления означает конфликт, внешнее удаление Kubernetes здесь не выполняется.
func (repository *Repository) failLockedRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, failureCode string) error {
	tag, err := tx.Exec(ctx, queryRuntimeSecretOperationFail, pgx.StrictNamedArgs{
		"operation_id": locked.id, "claimant_id": locked.claimantID,
		"claim_generation": locked.claimGeneration, "failure_code": failureCode,
	})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return repository.insertRuntimeSecretAudit(ctx, tx, organizationID, locked, "FAILED", "i18n:RUNTIME_SECRET_OPERATION_FAILED")
}

// insertRuntimeSecretAudit inserts operation/action/outcome/correlation metadata with a newly generated audit ref, without including secret content or the operation grant.
// insertRuntimeSecretAudit вставляет метаданные операции/действия/результата/корреляции с новой ссылкой аудита, не включая содержимое секрета и grant операции.
func (repository *Repository) insertRuntimeSecretAudit(ctx context.Context, tx pgx.Tx, organizationID string, locked lockedRuntimeSecretOperation, outcome, summary string) error {
	auditRef, err := newRef("aud")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryRuntimeSecretAudit, pgx.StrictNamedArgs{
		"ref": auditRef, "operation_id": locked.id, "organization_id": organizationID,
		"project_id": locked.projectID, "actor_id": locked.actorID,
		"action": "runtime-secret." + strings.ToLower(locked.kind), "secret_ref": locked.secretRef,
		"outcome": outcome, "summary": summary, "correlation_ref": locked.correlationRef,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// lockRuntimeSecret locks organization-scoped metadata by secret ref and scans the canonical projection, mapping absent rows to NotFound and other query/scan failures to Unavailable. It does not by itself authorize the requesting actor.
// lockRuntimeSecret блокирует метаданные организации по ссылке секрета и читает каноническую проекцию, заменяя отсутствие на NotFound, остальные ошибки запроса/чтения на Unavailable. Самостоятельно доступ actor не разрешает.
func (repository *Repository) lockRuntimeSecret(ctx context.Context, tx pgx.Tx, organizationID, ref string) (lockedRuntimeSecret, error) {
	var result lockedRuntimeSecret
	err := tx.QueryRow(ctx, queryRuntimeSecretLock, pgx.StrictNamedArgs{"organization_id": organizationID, "secret_ref": ref}).Scan(
		&result.id, &result.ref, &result.version, &result.projectID, &result.projectRef, &result.name,
		&result.description, &result.valueType, &result.state, &result.currentRevision, &result.namespace,
		&result.createdAt, &result.updatedAt, &result.scopeKind, &result.organizationRef,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecret{}, errs.ErrUnavailable
	}
	return result, nil
}

// lockRuntimeSecretByName locks secret metadata by the exact organization/project/name tuple and preserves the same absence/error mapping as ref lookup; the prepared command already owns project authorization.
// lockRuntimeSecretByName блокирует метаданные по точной тройке organization/project/name и сохраняет различение отсутствия/ошибки как при поиске по ref; авторизацией проекта владеет подготовленная команда.
func (repository *Repository) lockRuntimeSecretByName(ctx context.Context, tx pgx.Tx, organizationID, projectID, name string) (lockedRuntimeSecret, error) {
	var result lockedRuntimeSecret
	err := tx.QueryRow(ctx, queryRuntimeSecretLockByName, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "name": name,
	}).Scan(&result.id, &result.ref, &result.version, &result.projectID, &result.projectRef, &result.name,
		&result.description, &result.valueType, &result.state, &result.currentRevision, &result.namespace,
		&result.createdAt, &result.updatedAt, &result.scopeKind, &result.organizationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecret{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecret{}, errs.ErrUnavailable
	}
	return result, nil
}

// lockRuntimeSecretOperation locks one operation under the current organization, scanning its joined secret state and translating row absence separately from infrastructure failure.
// lockRuntimeSecretOperation блокирует одну операцию текущей организации, читает связанное состояние секрета и отдельно классифицирует отсутствие строки и инфраструктурную ошибку.
func (repository *Repository) lockRuntimeSecretOperation(ctx context.Context, tx pgx.Tx, organizationID, operationRef string) (lockedRuntimeSecretOperation, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLock, pgx.StrictNamedArgs{
		"organization_id": organizationID, "operation_ref": operationRef,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, errs.ErrUnavailable
	}
	return operation, nil
}

// lockRuntimeSecretOperationByIdempotency finds the locked operation in the organization/actor/kind/idempotency-key scope, returning found=false only for NoRows. The caller must still compare target and intent digest before replay.
// lockRuntimeSecretOperationByIdempotency находит заблокированную операцию в области organization/actor/kind/idempotency-key, возвращая found=false лишь при NoRows. Перед повтором вызывающий ещё сравнивает цель и digest намерения.
func (repository *Repository) lockRuntimeSecretOperationByIdempotency(ctx context.Context, tx pgx.Tx, organizationID, actorID, kind, key string) (lockedRuntimeSecretOperation, bool, error) {
	operation, err := repository.scanRuntimeSecretOperation(tx.QueryRow(ctx, queryRuntimeSecretOperationLockIdempotency, pgx.StrictNamedArgs{
		"organization_id": organizationID, "actor_id": actorID, "kind": kind, "idempotency_key": key,
	}))
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeSecretOperation{}, false, nil
	}
	if err != nil {
		return lockedRuntimeSecretOperation{}, false, errs.ErrUnavailable
	}
	return operation, true, nil
}

// scanRuntimeSecretOperation decodes the fixed joined SQL column order into operation and current-secret metadata while preserving the scanner error; it does not validate a lifecycle transition or read Kubernetes secret content.
// scanRuntimeSecretOperation читает фиксированный порядок SQL-столбцов операции и текущих метаданных секрета, сохраняя ошибку scanner; не проверяет переход состояния и не читает содержимое секрета Kubernetes.
func (repository *Repository) scanRuntimeSecretOperation(scanner rowScanner) (lockedRuntimeSecretOperation, error) {
	var operation lockedRuntimeSecretOperation
	err := scanner.Scan(
		&operation.id, &operation.ref, &operation.kind, &operation.state,
		&operation.projectID, &operation.secretID, &operation.actorID, &operation.correlationRef,
		&operation.targetRevision, &operation.expectedSecretVersion, &operation.expectedCurrentRevision,
		&operation.expectedContentSHA256, &operation.grantExpiresAt, &operation.claimantID,
		&operation.claimGeneration, &operation.leaseDeadline, &operation.failureCode,
		&operation.terminalSnapshot, &operation.intentDigest, &operation.projectRef, &operation.secretRef,
		&operation.secretVersion, &operation.secretState, &operation.secretCurrentRevision,
		&operation.name, &operation.description, &operation.valueType, &operation.namespace,
		&operation.secretCreatedAt, &operation.secretUpdatedAt, &operation.scopeKind, &operation.organizationRef,
	)
	return operation, err
}

// runtimeSecretOperationDescriptors selects the exact broker descriptors required by the operation kind: prospective version name for create/rotate, one current revision for reveal or all stored revisions for revoke. Unknown kinds fail closed.
// runtimeSecretOperationDescriptors выбирает точные дескрипторы брокера по виду операции: будущее имя версии для create/rotate, текущую ревизию для reveal либо все сохранённые для revoke. Неизвестный вид закрыто отклоняется.
func (repository *Repository) runtimeSecretOperationDescriptors(ctx context.Context, tx pgx.Tx, operation lockedRuntimeSecretOperation) ([]entity.RuntimeSecretRevisionDescriptor, error) {
	switch operation.kind {
	case "CREATE", "ROTATE":
		name, err := runtimesecret.VersionedKubernetesName(operation.secretRef, operation.targetRevision)
		if err != nil {
			return nil, errs.ErrInvalid
		}
		return []entity.RuntimeSecretRevisionDescriptor{{
			Revision: operation.targetRevision, Namespace: operation.namespace, SecretName: name,
			SecretKey: runtimeSecretKey, ContentSHA256: operation.expectedContentSHA256,
		}}, nil
	case "REVEAL":
		descriptor, err := repository.runtimeSecretRevision(ctx, tx, operation.secretID, operation.expectedCurrentRevision)
		if err != nil {
			return nil, err
		}
		return []entity.RuntimeSecretRevisionDescriptor{descriptor}, nil
	case "REVOKE":
		return repository.runtimeSecretRevisions(ctx, tx, operation.secretID)
	default:
		return nil, errs.ErrInvalid
	}
}

// runtimeSecretRevision reads one stored revision descriptor for an already resolved secret ID; row absence is NotFound and scan/query failures are Unavailable, with no secret bytes returned.
// runtimeSecretRevision читает один сохранённый дескриптор ревизии уже разрешённого ID секрета; отсутствие означает NotFound, ошибка чтения/запроса — Unavailable, байты секрета не возвращаются.
func (repository *Repository) runtimeSecretRevision(ctx context.Context, tx pgx.Tx, secretID string, revision int64) (entity.RuntimeSecretRevisionDescriptor, error) {
	var item entity.RuntimeSecretRevisionDescriptor
	err := tx.QueryRow(ctx, queryRuntimeSecretRevisionGet, pgx.StrictNamedArgs{"secret_id": secretID, "revision": revision}).Scan(
		&item.Revision, &item.Namespace, &item.SecretName, &item.SecretKey, &item.SecretUID,
		&item.SecretResourceVersion, &item.ContentSHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecretRevisionDescriptor{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeSecretRevisionDescriptor{}, errs.ErrUnavailable
	}
	return item, nil
}

// runtimeSecretRevisions lists stored revision descriptors in SQL order for broker revocation, closes rows and rejects scan/iteration failure without returning a partial successful list.
// runtimeSecretRevisions перечисляет сохранённые дескрипторы в порядке SQL для отзыва брокером, закрывает rows и отклоняет ошибку чтения/итерации без выдачи частично успешного списка.
func (repository *Repository) runtimeSecretRevisions(ctx context.Context, tx pgx.Tx, secretID string) ([]entity.RuntimeSecretRevisionDescriptor, error) {
	rows, err := tx.Query(ctx, queryRuntimeSecretRevisionList, pgx.StrictNamedArgs{"secret_id": secretID})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var items []entity.RuntimeSecretRevisionDescriptor
	for rows.Next() {
		var item entity.RuntimeSecretRevisionDescriptor
		if err := rows.Scan(&item.Revision, &item.Namespace, &item.SecretName, &item.SecretKey, &item.SecretUID, &item.SecretResourceVersion, &item.ContentSHA256); err != nil {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

// ensureRuntimeSecretUnreferenced asks the owner query whether any retained reference prevents revocation, returning ResourceInUse on a positive result and Unavailable on a query failure.
// ensureRuntimeSecretUnreferenced через запрос владельца проверяет удерживающие ссылки перед отзывом, возвращая ResourceInUse при наличии и Unavailable при ошибке запроса.
func (repository *Repository) ensureRuntimeSecretUnreferenced(ctx context.Context, tx pgx.Tx, organizationID, secretRef string) error {
	var referenced bool
	if err := tx.QueryRow(ctx, queryRuntimeSecretIsReferenced, pgx.StrictNamedArgs{
		"organization_id": organizationID, "secret_ref": secretRef,
	}).Scan(&referenced); err != nil {
		return errs.ErrUnavailable
	}
	if referenced {
		return errs.ErrResourceInUse
	}
	return nil
}

// runtimeSecretOperationMatchesCurrent compares namespace, current version/revision and kind-specific active/provisioning requirements against the prepared operation; it does not check actor access, grant expiry or claim lease.
// runtimeSecretOperationMatchesCurrent сверяет namespace, текущую версию/ревизию и зависящие от вида active/provisioning с подготовленной операцией; не проверяет доступ actor, срок grant и аренду захвата.
func (repository *Repository) runtimeSecretOperationMatchesCurrent(operation lockedRuntimeSecretOperation) bool {
	if operation.namespace != repository.runtimeSecretNamespace || operation.secretVersion != operation.expectedSecretVersion || operation.secretCurrentRevision != operation.expectedCurrentRevision {
		return false
	}
	if operation.kind == "CREATE" {
		return operation.secretState == "PROVISIONING" && operation.targetRevision > operation.expectedCurrentRevision
	}
	if operation.kind == "ROTATE" {
		return operation.secretState == "ACTIVE" && operation.targetRevision > operation.expectedCurrentRevision
	}
	return operation.secretState == "ACTIVE" && operation.targetRevision == operation.expectedCurrentRevision
}

// materializationMatchesOperation binds non-nil materialization to the configured namespace, fixed data key, expected digest and deterministic versioned name. Kubernetes UID/resourceVersion validation is handled by the materialization/descriptor checks, not inferred here.
// materializationMatchesOperation привязывает непустую материализацию к настроенному namespace, фиксированному ключу, ожидаемому digest и детерминированному имени версии. Проверка UID/resourceVersion Kubernetes относится к проверкам материализации/дескриптора и здесь не подразумевается.
func (repository *Repository) materializationMatchesOperation(operation lockedRuntimeSecretOperation, materialization *entity.RuntimeSecretMaterialization) bool {
	if materialization == nil || operation.expectedContentSHA256 == "" || materialization.Namespace != repository.runtimeSecretNamespace ||
		materialization.Namespace != operation.namespace || materialization.SecretKey != runtimeSecretKey ||
		!runtimeSecretDigestsEqual(materialization.ContentSHA256, operation.expectedContentSHA256) {
		return false
	}
	name, err := runtimesecret.VersionedKubernetesName(operation.secretRef, operation.targetRevision)
	return err == nil && materialization.SecretName == name
}

// runtimeSecret projects only the joined secret metadata from a locked operation into its public entity; display hints, revision descriptor and action hints are added by the appropriate completion/read path.
// runtimeSecret переносит лишь связанные метаданные секрета заблокированной операции в публичную сущность; display hints, дескриптор ревизии и действия добавляет соответствующий путь завершения/чтения.
func (operation lockedRuntimeSecretOperation) runtimeSecret() entity.RuntimeSecret {
	return entity.RuntimeSecret{
		ScopeKind: operation.scopeKind, OrganizationRef: operation.organizationRef,
		Ref: operation.secretRef, ProjectRef: operation.projectRef, Name: operation.name,
		Description: operation.description, ValueType: operation.valueType, State: operation.secretState,
		Namespace: operation.namespace, Version: operation.secretVersion,
		CurrentRevision: operation.secretCurrentRevision, CreatedAt: operation.secretCreatedAt, UpdatedAt: operation.secretUpdatedAt,
	}
}

// scanRuntimeSecret decodes metadata and the optional revision descriptor, omits empty display hints and distinguishes absence from scan failure. It returns no plaintext value and performs no access decision.
// scanRuntimeSecret читает метаданные и необязательный дескриптор ревизии, пропускает пустые display hints и различает отсутствие и ошибку чтения. Открытое значение не возвращает и решений о доступе не принимает.
func scanRuntimeSecret(scanner rowScanner) (entity.RuntimeSecret, error) {
	var item entity.RuntimeSecret
	var prefix, suffix string
	var descriptor entity.RuntimeSecretRevisionDescriptor
	if err := scanner.Scan(&item.Ref, &item.Version, &item.ProjectRef, &item.Name, &item.Description,
		&item.ValueType, &item.State, &item.CurrentRevision, &prefix, &suffix,
		&item.CreatedAt, &item.UpdatedAt, &item.Namespace, &descriptor.Revision, &descriptor.Namespace,
		&descriptor.SecretName, &descriptor.SecretKey, &descriptor.SecretUID,
		&descriptor.SecretResourceVersion, &descriptor.ContentSHA256, &item.ScopeKind, &item.OrganizationRef, &item.Purpose); errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSecret{}, errs.ErrNotFound
	} else if err != nil {
		return entity.RuntimeSecret{}, errs.ErrUnavailable
	}
	if item.Purpose != "SSH_COMPOSE" && (prefix != "" || suffix != "") {
		item.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: prefix, Suffix: suffix}
	}
	if descriptor.Revision > 0 {
		item.CurrentRevisionDescriptor = &descriptor
	}
	return item, nil
}

// validRuntimeSecretPrepare checks kind-specific field combinations, bounded metadata, value type and expected content digest shape. This is syntactic input validation; scope ownership and operation state are checked separately.
// validRuntimeSecretPrepare проверяет сочетания полей по виду, ограничения метаданных, тип значения и форму ожидаемого digest. Это синтаксическая проверка входа; владение областью и состояние операции проверяются отдельно.
func validRuntimeSecretPrepare(input platformrepo.RuntimeSecretPrepareInput) bool {
	switch input.Kind {
	case "CREATE":
		return strings.HasPrefix(input.ProjectRef, "prj_") && input.SecretRef == "" && strings.TrimSpace(input.Name) == input.Name &&
			utf8.RuneCountInString(input.Name) > 0 && utf8.RuneCountInString(input.Name) <= 120 && len(input.Description) <= 1000 &&
			validRuntimeSecretValueType(input.ValueType) && input.Mutation.ExpectedVersion == nil && validRuntimeSecretSHA256(input.ExpectedContentSHA256)
	case "ROTATE":
		return input.ProjectRef == "" && strings.HasPrefix(input.SecretRef, "sec_") && input.Name == "" && input.Description == "" && validRuntimeSecretValueType(input.ValueType) && validRuntimeSecretSHA256(input.ExpectedContentSHA256)
	case "REVEAL", "REVOKE":
		return input.ProjectRef == "" && strings.HasPrefix(input.SecretRef, "sec_") && input.Name == "" && input.Description == "" && input.ValueType == "" && input.ExpectedContentSHA256 == ""
	default:
		return false
	}
}

// validRuntimeSecretValueType accepts only the three supported metadata value types STRING, BINARY and JSON; it does not inspect or parse a secret value.
// validRuntimeSecretValueType принимает лишь поддержанные типы метаданных STRING, BINARY и JSON; значение секрета не просматривает и не разбирает.
func validRuntimeSecretValueType(value string) bool {
	return value == "STRING" || value == "BINARY" || value == "JSON"
}

// validRuntimeSecretMaterialization checks required names/key, bounded UID/resourceVersion, canonical digest and optional short hints. Expected operation ownership, namespace/name equality and actual Kubernetes readback are separate checks.
// validRuntimeSecretMaterialization проверяет обязательные имена/ключ, ограничения UID/resourceVersion, канонический digest и необязательные короткие подсказки. Владение операцией, равенство namespace/name и фактическое чтение Kubernetes проверяются отдельно.
func validRuntimeSecretMaterialization(value *entity.RuntimeSecretMaterialization) bool {
	if value == nil || value.Namespace == "" || value.SecretName == "" || value.SecretKey != runtimeSecretKey ||
		len(value.SecretUID) < 1 || len(value.SecretUID) > 128 || len(value.SecretResourceVersion) < 1 || len(value.SecretResourceVersion) > 128 ||
		!validRuntimeSecretSHA256(value.ContentSHA256) {
		return false
	}
	if value.DisplayHint != nil && (utf8.RuneCountInString(value.DisplayHint.Prefix) > 6 || utf8.RuneCountInString(value.DisplayHint.Suffix) > 6) {
		return false
	}
	return true
}

// validRuntimeSecretClaimant allows a nonempty bounded printable-ASCII claimant without whitespace/control characters; shape validation is not a workload identity proof.
// validRuntimeSecretClaimant разрешает непустой ограниченный claimant из печатного ASCII без пробелов/управляющих символов; проверка формы не доказывает identity workload.
func validRuntimeSecretClaimant(value string) bool {
	if len(value) < 1 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

// validRuntimeSecretWorkPrincipal matches the verified caller workload and exact operation permission required by the broker command; organization scope is resolved separately.
// validRuntimeSecretWorkPrincipal сверяет проверенный workload вызывающего и точное полномочие команды брокера; область организации разрешается отдельно.
func validRuntimeSecretWorkPrincipal(principal value.Principal, permission string) bool {
	return principal.CallerWorkload == "secret-broker" && principal.Permission == permission
}

// validRuntimeSecretFailureCode restricts broker-submitted failure reasons to the supported closed set; internal expiry failures are assigned by owner logic rather than accepted from this input.
// validRuntimeSecretFailureCode ограничивает переданные брокером причины закрытым набором; внутренние ошибки истечения назначает логика владельца, а не этот вход.
func validRuntimeSecretFailureCode(value string) bool {
	switch value {
	case "KUBERNETES_UNAVAILABLE", "MATERIALIZATION_CONFLICT", "MATERIALIZATION_INVALID", "STALE_SECRET_VERSION", "RECONCILIATION_FAILED":
		return true
	default:
		return false
	}
}

// boundedRuntimeSecretRecoveryPage rejects negative sizes, supplies the default for zero and caps oversized positive requests at the recovery maximum.
// boundedRuntimeSecretRecoveryPage отклоняет отрицательный размер, подставляет значение по умолчанию для нуля и ограничивает слишком большой положительный запрос максимумом восстановления.
func boundedRuntimeSecretRecoveryPage(size int32) (int32, error) {
	if size < 0 {
		return 0, errs.ErrInvalid
	}
	if size == 0 {
		return runtimeSecretRecoveryPage, nil
	}
	if size > runtimeSecretRecoveryMax {
		return runtimeSecretRecoveryMax, nil
	}
	return size, nil
}

// encodeRuntimeSecretRecoveryCursor encodes version, UTC microsecond deadline and operation ref as a URL-safe keyset position without including credentials or claiming authorization.
// encodeRuntimeSecretRecoveryCursor кодирует версию, срок UTC в микросекундах и ссылку операции как URL-safe позицию keyset без учётных данных и без назначения полномочий.
func encodeRuntimeSecretRecoveryCursor(deadline time.Time, operationRef string) string {
	payload := fmt.Sprintf("%s\n%d\n%s", runtimeSecretRecoveryCursorVersion, deadline.UTC().UnixMicro(), operationRef)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeRuntimeSecretRecoveryCursor accepts an empty initial position or validates a bounded versioned deadline/ref token with a positive timestamp. It reconstructs UTC position only; broker/organization eligibility is enforced by the query path.
// decodeRuntimeSecretRecoveryCursor принимает пустую начальную позицию либо проверяет ограниченный версионированный deadline/ref с положительным временем. Восстанавливает лишь позицию UTC; доступ брокера/организации проверяет путь запроса.
func decodeRuntimeSecretRecoveryCursor(token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	if len(token) > 256 {
		return nil, "", errs.ErrInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parts := strings.Split(string(payload), "\n")
	if len(parts) != 3 || parts[0] != runtimeSecretRecoveryCursorVersion || !strings.HasPrefix(parts[2], "secop_") || len(parts[2]) > 96 {
		return nil, "", errs.ErrInvalid
	}
	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || microseconds < 1 {
		return nil, "", errs.ErrInvalid
	}
	deadline := time.UnixMicro(microseconds).UTC()
	return &deadline, parts[2], nil
}

// validRuntimeSecretSHA256 requires exactly 64 lowercase hexadecimal characters, rejecting alternate encodings before materialization comparison.
// validRuntimeSecretSHA256 требует ровно 64 строчных шестнадцатеричных символа, отклоняя другие кодирования до сравнения материализации.
func validRuntimeSecretSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// runtimeSecretDigestsEqual decodes both hexadecimal digests, requires SHA-256 byte lengths and uses constant-time comparison for the equal-length decoded values.
// runtimeSecretDigestsEqual декодирует оба шестнадцатеричных digest, требует длину SHA-256 и сравнивает декодированные значения одинаковой длины за постоянное время.
func runtimeSecretDigestsEqual(left, right string) bool {
	leftBytes, leftErr := hex.DecodeString(left)
	rightBytes, rightErr := hex.DecodeString(right)
	return leftErr == nil && rightErr == nil && len(leftBytes) == sha256.Size && len(rightBytes) == sha256.Size && subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

// runtimeSecretMaterializationMatchesDescriptor compares the full namespace/name/key/UID/resourceVersion tuple and content digest with one stored descriptor, without performing a Kubernetes fetch or owner access check.
// runtimeSecretMaterializationMatchesDescriptor сравнивает полную комбинацию namespace/name/key/UID/resourceVersion и digest содержимого с сохранённым дескриптором без запроса Kubernetes и проверки доступа владельца.
func runtimeSecretMaterializationMatchesDescriptor(materialization entity.RuntimeSecretMaterialization, descriptor entity.RuntimeSecretRevisionDescriptor) bool {
	return materialization.Namespace == descriptor.Namespace && materialization.SecretName == descriptor.SecretName &&
		materialization.SecretKey == descriptor.SecretKey && materialization.SecretUID == descriptor.SecretUID &&
		materialization.SecretResourceVersion == descriptor.SecretResourceVersion && runtimeSecretDigestsEqual(materialization.ContentSHA256, descriptor.ContentSHA256)
}

// decodeRuntimeSecretSnapshot decodes a nonempty terminal metadata receipt and requires its ref, namespace and valid organization/project scope; this shape check does not replace the locked operation/claim replay checks.
// decodeRuntimeSecretSnapshot разбирает непустую итоговую квитанцию метаданных и требует ref, namespace и корректную область организации/проекта; проверка формы не заменяет проверку заблокированной операции/поколения при повторе.
func decodeRuntimeSecretSnapshot(raw []byte) (entity.RuntimeSecret, error) {
	var result entity.RuntimeSecret
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || result.Ref == "" || result.Namespace == "" || runtimesecret.ValidateScope(runtimesecret.ScopeKind(result.ScopeKind), result.OrganizationRef, result.ProjectRef) != nil {
		return entity.RuntimeSecret{}, errors.New("runtime secret terminal snapshot is invalid")
	}
	return result, nil
}

// newRuntimeSecretGrant generates 32 cryptographically random bytes, returns the URL-safe grant once alongside its SHA-256 digest and maps entropy failure to Unavailable. Only the digest is intended for persistent operation lookup.
// newRuntimeSecretGrant генерирует 32 криптографически случайных байта, возвращает URL-safe grant с digest SHA-256 и заменяет отказ энтропии на Unavailable. Для постоянного поиска операции предназначен лишь digest.
func newRuntimeSecretGrant() (string, string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", errs.ErrUnavailable
	}
	grant := base64.RawURLEncoding.EncodeToString(buffer)
	digest := sha256.Sum256([]byte(grant))
	return grant, hex.EncodeToString(digest[:]), nil
}

// runtimeSecretAuditSummary maps supported operation kinds to fixed localization keys; unknown kinds produce an empty key and must already have been rejected by the command path.
// runtimeSecretAuditSummary сопоставляет поддержанные виды операций фиксированным ключам локализации; неизвестный вид даёт пустой ключ и должен быть ранее отклонён командой.
func runtimeSecretAuditSummary(kind string) string {
	return map[string]string{
		"CREATE": "i18n:RUNTIME_SECRET_CREATED", "ROTATE": "i18n:RUNTIME_SECRET_ROTATED",
		"REVEAL": "i18n:RUNTIME_SECRET_REVEALED", "REVOKE": "i18n:RUNTIME_SECRET_REVOKED",
	}[kind]
}
