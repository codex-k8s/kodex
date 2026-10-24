package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

var (
	// querySecretDraftImpactGet reads an actor-bound organization plan by ref, idempotency key or
	// operation ID with item count and snapshot revision 2; an overdue PREPARED plan is projected as
	// EXPIRED without writing state. Draft ownership and current resource permissions are checked
	// separately.
	// querySecretDraftImpactGet читает план организации, привязанный к субъекту, по ссылке, ключу
	// идемпотентности либо ID операции, с числом элементов и версией снимка 2. Просроченный PREPARED
	// отображается как EXPIRED без записи; владение черновиком и актуальные права проверяются
	// отдельно.
	//go:embed sql/secret_draft_impact_get.sql
	querySecretDraftImpactGet string
	// querySecretDraftImpactInsert stores server-resolved actor/draft/version/source/credential pins,
	// digest and idempotency intent with owner snapshot revision 2, returning the internal ID and
	// database expiry. The surrounding transaction owns authorization and insertion of immutable
	// items.
	// querySecretDraftImpactInsert сохраняет назначенные сервером привязки субъекта, черновика,
	// версий, исходной ревизии и учётных данных, хеш и идемпотентное намерение с версией снимка 2;
	// возвращает ID и срок БД. Авторизация и атомарная вставка элементов принадлежат вызывающему.
	//go:embed sql/secret_draft_impact_insert.sql
	querySecretDraftImpactInsert string
	// querySecretDraftImpactItemsInsert inserts each supplied Ref/Consumer JSON snapshot under the
	// resolved plan ID. It does not choose consumers, grant authority or validate snapshot ownership;
	// preparation must supply the bounded trusted list.
	// querySecretDraftImpactItemsInsert вставляет JSON-снимки Ref/Consumer под разрешённым ID плана.
	// Не выбирает потребителей и не проверяет владение; подготовка передаёт ограниченный доверенный
	// список, а запись не выдаёт полномочий.
	//go:embed sql/secret_draft_impact_items_insert.sql
	querySecretDraftImpactItemsInsert string
	// querySecretDraftImpactItems reads snapshots, outcomes and result references in stable item-ref
	// order for an already resolved internal plan ID. It applies no actor or permission predicates, so
	// callers must authorize the plan and revalidate disclosure.
	// querySecretDraftImpactItems читает снимки, исходы и ссылки результатов в порядке ссылок
	// элементов по разрешённому ID плана. Условий по субъекту и правам нет; вызывающий авторизует план
	// и повторно проверяет допустимость раскрытия.
	//go:embed sql/secret_draft_impact_items.sql
	querySecretDraftImpactItems string
	// querySecretDraftImpactBind binds one unused, unexpired PREPARED plan to the exact operation with
	// matching organization, actor, draft and pinned draft/secret versions. Returning its ID confirms
	// only the owner-side binding; item selection and publication remain separate.
	// querySecretDraftImpactBind связывает неиспользованный, неистёкший PREPARED план с точной
	// операцией при совпадении организации, субъекта, черновика и его закреплённых версий.
	// Возвращённый ID подтверждает связь владельца; выбор элементов и публикация выполняются отдельно.
	//go:embed sql/secret_draft_impact_bind.sql
	querySecretDraftImpactBind string
	// querySecretDraftImpactSelect sets every item of a resolved plan to PENDING when selected or
	// NOT_SELECTED otherwise. Selection membership, lifecycle eligibility and authorization are caller
	// responsibilities; this update alone performs no consumer replacement.
	// querySecretDraftImpactSelect помечает выбранные элементы разрешённого плана PENDING, остальные
	// NOT_SELECTED. Принадлежность выбранных ссылок, жизненный цикл и полномочия проверяет вызывающий;
	// замена потребителей этим запросом не выполняется.
	//go:embed sql/secret_draft_impact_select.sql
	querySecretDraftImpactSelect string
	// querySecretDraftImpactOutcome writes one pending item outcome and resulting environment/binding
	// references by exact plan ID and item ref. The caller establishes valid outcome/result pairing
	// and transactionally couples successful consumer changes; row-count handling is external.
	// querySecretDraftImpactOutcome записывает исход и результирующие ссылки окружения и привязки для
	// PENDING элемента точного плана. Допустимость сочетания исхода и ссылок, атомарность успешной
	// замены и обработка числа строк принадлежат вызывающему.
	//go:embed sql/secret_draft_impact_outcome.sql
	querySecretDraftImpactOutcome string
	// querySecretDraftImpactFinish transitions PREPARED plans bound to a resolved operation ID to the
	// supplied state. It has no independent actor, expiry or outcome validation; the locked operation
	// lifecycle determines the terminal state.
	// querySecretDraftImpactFinish переводит PREPARED планы разрешённой операции в переданное
	// состояние. Самостоятельных проверок субъекта, срока и исходов нет; итог определяет жизненный
	// цикл операции под блокировкой.
	//go:embed sql/secret_draft_impact_finish.sql
	querySecretDraftImpactFinish string
	// querySecretDraftImpactCancel cancels every PREPARED plan of the resolved internal draft ID,
	// including unbound plans. It changes owner metadata only; authorization, draft terminal
	// transition and cancellation of operation grants remain in the enclosing owner path.
	// querySecretDraftImpactCancel отменяет все PREPARED планы разрешённого черновика, включая ещё не
	// связанные с операцией. Меняет только метаданные; авторизация, завершение черновика и отзыв
	// разрешений операции принадлежат внешнему пути владельца.
	//go:embed sql/secret_draft_impact_cancel.sql
	querySecretDraftImpactCancel string
)

// maximumSecretDraftImpactItems caps persisted impact snapshots and preparation results at 1000
// items. Preparation requests one extra row to detect overflow; this bound does not itself
// authorize any listed consumer.
// maximumSecretDraftImpactItems ограничивает сохранённые снимки и подготовку плана 1000
// элементами. Подготовка запрашивает дополнительную строку для обнаружения переполнения;
// ограничение не авторизует потребителей.
const maximumSecretDraftImpactItems = 1000

// secretDraftImpactRow combines the public metadata-only plan with its internal database ID,
// idempotency intent and optional bound operation ID. These private fields support owner-side
// lookup and replay, not caller-supplied authority.
// secretDraftImpactRow объединяет публичные метаданные плана с внутренним ID, идемпотентным
// намерением и необязательным ID связанной операции. Закрытые поля служат чтению и повтору
// результата владельца, а не полномочиям клиента.
type secretDraftImpactRow struct {
	public                  entity.RuntimeSecretDraftImpactPlan
	id, intent, operationID string
}

// secretDraftImpact scans the exact organization/actor plan projection through the caller
// transaction and locator alternatives. NoRows becomes NotFound, other scan errors Unavailable;
// the possibly partially scanned row accompanies errors and callers must discard it.
// secretDraftImpact сканирует проекцию плана точных организации и субъекта в транзакции
// вызывающего по альтернативным указателям. NoRows становится NotFound, остальные ошибки —
// Unavailable; частично заполненная строка при ошибке должна отбрасываться вызывающим.
func (r *Repository) secretDraftImpact(ctx context.Context, tx pgx.Tx, s scope, ref, key, operationID string) (secretDraftImpactRow, error) {
	var row secretDraftImpactRow
	err := tx.QueryRow(ctx, querySecretDraftImpactGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "plan_ref": ref, "idempotency_key": key, "operation_id": operationID}).Scan(&row.id, &row.public.Ref, &row.public.DraftRef, &row.public.DraftVersion, &row.public.SecretRef, &row.public.SecretVersion, &row.public.SourceRevision, &row.public.Digest, &row.public.State, &row.public.ExpiresAt, &row.intent, &row.operationID, &row.public.Total)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	return row, nil
}

// secretDraftImpactItems loads at most 1000 ordered items, decodes consumer snapshots and verifies
// matching outer/inner organization/project ownership shapes. Scan/JSON/overflow/iteration errors
// discard all items as Unavailable; invalid owner shape is NotFound. Current permissions are
// checked by the higher-level reader or executor.
// secretDraftImpactItems читает до 1000 упорядоченных элементов, декодирует снимки и проверяет
// согласованность внешней и вложенной области организации и проекта. Ошибки чтения, JSON,
// переполнение и iteration отбрасывают результат как Unavailable; неверный владелец — NotFound.
// Актуальные права проверяет внешний reader либо executor.
func (r *Repository) secretDraftImpactItems(ctx context.Context, tx pgx.Tx, id string) ([]entity.RuntimeSecretDraftImpactItem, error) {
	rows, err := tx.Query(ctx, querySecretDraftImpactItems, pgx.StrictNamedArgs{"plan_id": id})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RuntimeSecretDraftImpactItem
	for rows.Next() {
		var item entity.RuntimeSecretDraftImpactItem
		var raw []byte
		if rows.Scan(&item.Ref, &raw, &item.Outcome, &item.ResultEnvironmentVersionRef, &item.ResultBindingRef, &item.ResultBindingVersion) != nil || json.Unmarshal(raw, &item.Consumer) != nil || len(result) >= maximumSecretDraftImpactItems {
			return nil, errs.ErrUnavailable
		}
		consumer := item.Consumer
		if !validRuntimeOwnerSnapshot(consumer.ScopeKind, consumer.OrganizationRef, consumer.ProjectRef) ||
			consumer.Consumer.ScopeKind != consumer.ScopeKind || consumer.Consumer.OrganizationRef != consumer.OrganizationRef || consumer.Consumer.ProjectRef != consumer.ProjectRef {
			return nil, errs.ErrNotFound
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// PrepareRuntimeSecretDraftImpact creates an actor-bound immutable consumer plan in a serializable
// transaction after locking the draft, checking create/rotate authority, VALID state, expiry and
// expected version. Exact draft/intent idempotency replay returns its previous plan before fresh
// state/version checks. A bounded current-consumer snapshot, secret pins and actor/credential JSON
// digest are stored with audit; commit failure is Conflict. This prepares metadata and performs no
// secret or consumer materialization.
// PrepareRuntimeSecretDraftImpact создаёт привязанный к субъекту неизменяемый план в serializable
// транзакции: блокирует черновик, проверяет право create/rotate, VALID, срок и ожидаемую версию.
// Повтор точного черновика и намерения возвращает прежний план до свежих проверок состояния и
// версии. Ограниченный снимок потребителей, привязки секрета и JSON-хеш с субъектом и ревизией
// учётных данных сохраняются с аудитом; ошибка commit — Conflict. Материализация секрета и замена
// потребителей здесь не выполняются.
func (r *Repository) PrepareRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref string, mutation value.Mutation) (entity.RuntimeSecretDraftImpactPlan, error) {
	var empty entity.RuntimeSecretDraftImpactPlan
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := r.lockSecretDraft(ctx, tx, s.organizationID, ref)
	if err != nil {
		return empty, err
	}
	permission := "secret.rotate"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return empty, err
	}
	existing, err := r.secretDraftImpact(ctx, tx, s, "", mutation.IdempotencyKey, "")
	if err == nil {
		if existing.public.DraftRef != ref || existing.intent != mutation.IntentDigest {
			return empty, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return empty, errs.ErrConflict
		}
		return existing.public, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return empty, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion != d.public.Version {
		return empty, errs.ErrVersionMismatch
	}
	if d.public.State != "VALID" || !time.Now().Before(d.public.ExpiresAt) {
		return empty, errs.ErrConflict
	}
	secret, err := r.lockRuntimeSecret(ctx, tx, s.organizationID, d.public.SecretRef)
	if err != nil {
		return empty, err
	}
	rows, err := tx.Query(ctx, querySecretImpactConsumers, pgx.StrictNamedArgs{"organization_id": s.organizationID, "actor_id": s.actorID, "query": "", "authority_project": s.authorityProjectID, "secret_ref": secret.ref, "target_revision": int64(0), "evaluated_at": time.Now().UTC(), "cursor_ref": "", "page_size": maximumSecretDraftImpactItems + 1})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	var items []entity.RuntimeSecretDraftImpactItem
	var total int64
	for rows.Next() {
		var key string
		var item entity.RuntimeSecretDraftImpactItem
		c := &item.Consumer
		if rows.Scan(&key, &c.EnvironmentRef, &c.EnvironmentVersion, &c.EnvironmentVersionRef, &c.SecretRevisions, &c.Consumer.AgentRef, &c.Consumer.AgentVersion, &c.Consumer.BindingRef, &c.Consumer.BindingVersion, &c.ProjectRef, &c.ScopeKind, &c.OrganizationRef, &total) != nil {
			rows.Close()
			return empty, errs.ErrUnavailable
		}
		if key != "" {
			item.Ref, err = newRef("sdit")
			if err != nil {
				rows.Close()
				return empty, errs.ErrUnavailable
			}
			c.Consumer.VersionRef = c.EnvironmentVersionRef
			c.Consumer.ScopeKind, c.Consumer.OrganizationRef, c.Consumer.ProjectRef = c.ScopeKind, c.OrganizationRef, c.ProjectRef
			item.Outcome = "PENDING"
			items = append(items, item)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return empty, errs.ErrUnavailable
	}
	if total > maximumSecretDraftImpactItems {
		return empty, errs.ErrConflict
	}
	plan := entity.RuntimeSecretDraftImpactPlan{DraftRef: ref, DraftVersion: d.public.Version, SecretRef: secret.ref, SecretVersion: secret.version, SourceRevision: secret.currentRevision, Total: total, State: "PREPARED"}
	plan.Ref, err = newRef("sdip")
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	raw, err := json.Marshal(struct {
		Plan       entity.RuntimeSecretDraftImpactPlan
		Actor      string
		Credential uint64
		Items      []entity.RuntimeSecretDraftImpactItem
	}{plan, s.actorID, p.CredentialRevision, items})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	digest := sha256.Sum256(raw)
	plan.Digest = hex.EncodeToString(digest[:])
	var id string
	err = tx.QueryRow(ctx, querySecretDraftImpactInsert, pgx.StrictNamedArgs{"ref": plan.Ref, "organization_id": s.organizationID, "actor_id": s.actorID, "draft_id": d.id, "draft_version": plan.DraftVersion, "secret_version": plan.SecretVersion, "source_revision": plan.SourceRevision, "credential_revision": p.CredentialRevision, "digest": plan.Digest, "idempotency_key": mutation.IdempotencyKey, "intent_digest": mutation.IntentDigest}).Scan(&id, &plan.ExpiresAt)
	if err != nil {
		return empty, mapWriteError(err)
	}
	if items == nil {
		items = []entity.RuntimeSecretDraftImpactItem{}
	}
	if _, err = tx.Exec(ctx, querySecretDraftImpactItemsInsert, pgx.StrictNamedArgs{"plan_id": id, "items": string(asJSON(items))}); err != nil {
		return empty, errs.ErrUnavailable
	}
	if err = r.auditSecretDraft(ctx, tx, s, d, secretDraftOperationRow{kind: "IMPACT", actorID: s.actorID, correlation: p.CorrelationRef}, "SUCCEEDED"); err != nil {
		return empty, err
	}
	if tx.Commit(ctx) != nil {
		return empty, errs.ErrConflict
	}
	return plan, nil
}

// GetRuntimeSecretDraftImpact reads an actor-bound plan in a read-only repeatable-read
// transaction, rechecks draft create/rotate authority and filters each immutable item against
// current environment/agent ownership and access. Forbidden/NotFound consumers are hidden; other
// failures return the accumulated page with an error. The cursor binds scope, plan digest/state
// and trimmed query; Total counts eligible matches before cursor pagination. Commit failure is
// Unavailable; this read does not apply the plan.
// GetRuntimeSecretDraftImpact читает план субъекта в read-only repeatable-read транзакции,
// повторяет create/rotate проверку черновика и сверяет каждый снимок с актуальным владением и
// доступом к окружению и агенту. Forbidden/NotFound потребители скрываются; прочие ошибки
// сопровождаются накопленной страницей. Курсор связан с областью, хешем и состоянием плана,
// нормализованным поиском; Total учитывает допустимые совпадения до курсора. Ошибка commit —
// Unavailable; план не применяется.
func (r *Repository) GetRuntimeSecretDraftImpact(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RuntimeSecretDraftImpactPage, error) {
	var result entity.RuntimeSecretDraftImpactPage
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plan, err := r.secretDraftImpact(ctx, tx, s, ref, "", "")
	if err != nil {
		return result, err
	}
	d, err := scanSecretDraft(tx.QueryRow(ctx, querySecretDraftGet, pgx.StrictNamedArgs{"organization_id": s.organizationID, "draft_ref": plan.public.DraftRef}))
	if err != nil {
		return result, err
	}
	permission := "secret.rotate"
	if d.secretState == "PROVISIONING" {
		permission = "secret.create"
	}
	if err = r.secretDraftAccess(ctx, tx, s, d, permission); err != nil {
		return result, err
	}
	filter := query.Filter{ResourceRef: ref, Query: strings.TrimSpace(search), Category: plan.public.Digest + "/" + plan.public.State, Page: page}
	cursor, err := decodeCatalogCursor(s, "SECRET_DRAFT_IMPACT", filter)
	if err != nil {
		return result, err
	}
	items, err := r.secretDraftImpactItems(ctx, tx, plan.id)
	if err != nil {
		return result, err
	}
	result.Plan = plan.public
	limit := boundedPage(page)
	for _, item := range items {
		c := item.Consumer
		environment, err := r.getRuntimeEnvironmentTx(ctx, tx, s, c.EnvironmentRef)
		if err == nil {
			err = r.requireRuntimeEnvironmentOwnerAccess(ctx, tx, s, environment.ScopeKind, environment.ProjectRef)
		}
		if err == nil {
			err = matchRuntimeOwnerSnapshot(environment.ScopeKind, environment.OrganizationRef, environment.ProjectRef, c.ScopeKind, c.OrganizationRef, c.ProjectRef)
		}
		if err == nil && c.Consumer.AgentRef != "" {
			err = r.authorizeRuntimeEnvironmentConsumers(ctx, tx, s, environment, []entity.RuntimeEnvironmentConsumer{c.Consumer})
		}
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) {
				continue
			}
			return result, err
		}
		if search != "" && !strings.Contains(strings.ToLower(c.EnvironmentRef+" "+c.Consumer.AgentRef+" "+c.Consumer.ProjectRef), strings.ToLower(strings.TrimSpace(search))) {
			continue
		}
		result.Total++
		if item.Ref > cursor && len(result.Items) <= int(limit) {
			result.Items = append(result.Items, item)
		}
	}
	if len(result.Items) > int(limit) {
		result.Items = result.Items[:limit]
		result.NextPageToken = encodeCatalogCursor(s, "SECRET_DRAFT_IMPACT", filter, result.Items[limit-1].Ref)
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}
