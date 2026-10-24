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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// maximumAssistantSearchCandidates caps combined ordinary and extra SQL candidates at 500 before
// permission filtering. Exceeding the cap returns Invalid rather than silently dropping candidates
// or claiming a complete result.
// maximumAssistantSearchCandidates ограничивает общий список обычных и дополнительных
// SQL-кандидатов 500 до фильтра прав. Переполнение даёт Invalid вместо незаметного пропуска или
// заявления о полном результате.
const maximumAssistantSearchCandidates = 500

// maximumAssistantSearchResults limits the visible sorted search response to ten entries; later
// eligible entries set the truncated flag while permission checks still run.
// maximumAssistantSearchResults ограничивает видимый упорядоченный результат десятью элементами;
// следующие допустимые элементы ставят truncated, проверки доступа при этом продолжаются.
const maximumAssistantSearchResults = 10

// maximumAssistantIntegrationDefinitions limits each integration-definition page to ten entries,
// with one additional SQL row used only to detect a next offset.
// maximumAssistantIntegrationDefinitions ограничивает страницу integration definitions десятью
// элементами; дополнительная SQL-строка служит только обнаружению следующего смещения.
const maximumAssistantIntegrationDefinitions = 10

// queryAssistantSearchResolveLease resolves the live exact organization/ref/fence/generation lease
// to its persisted user and optional assistant project while holding FOR SHARE on the lease.
// Run/root/node/revision/session/conversation and SYSTEM/PROJECT assistant pins are checked in
// SQL; actor membership and resource-specific eligibility remain caller checks.
// queryAssistantSearchResolveLease разрешает живую lease по точным организации, ссылке, fence и
// generation в сохранённого пользователя и необязательный проект помощника с FOR SHARE lease. SQL
// проверяет run, root, node, revision, session, conversation и SYSTEM/PROJECT привязки; membership
// субъекта и доступ к ресурсу проверяются далее.
//
//go:embed sql/assistant_search_resolve_lease.sql
var queryAssistantSearchResolveLease string

// queryAssistantSearchExtra provides ranked role-image/environment/schedule/secret/integration
// metadata candidates constrained by organization and optional assistant project. It filters
// resource states but does not itself apply the final actor visibility rule; no credential value
// is selected.
// queryAssistantSearchExtra возвращает ранжированные метаданные кандидатов образов, окружений,
// расписаний, секретов и интеграций с ограничением организации и проекта помощника. Фильтрует
// состояния, но итоговую видимость для субъекта проверяет внешний reader; значения учётных данных
// не выбираются.
//
//go:embed sql/assistant_search_extra.sql
var queryAssistantSearchExtra string

// queryAssistantIntegrationDefinitions pages enabled global integration definitions by stable key
// and optional text match, returning schema, capabilities and a credential-key name. It selects no
// credential values; only the preceding exact lease/subject check permits this assistant read.
// queryAssistantIntegrationDefinitions выбирает включённые общие integration definitions по stable
// key и поиску, возвращая schema, capabilities и имя credential key. Значения не выбираются;
// доступ помощника устанавливается предшествующей проверкой точных lease и субъекта.
//
//go:embed sql/assistant_integration_definitions.sql
var queryAssistantIntegrationDefinitions string

// resolveAssistantReadLease hashes the supplied fence and scans the existing exact lease query
// into the current scope actor ref/ID, authority project and returned assistant project.
// NoRows/wrapped maps to NotFound; only serialization 40001 retains the locked-read retry marker,
// other failures become Unavailable without driver details. Scan may partially update the trusted
// scope on failure, which callers discard. It owns no transaction, subject membership or resource
// authorization.
// resolveAssistantReadLease хеширует переданный fence и сканирует прежний запрос точной lease в
// actor ref/ID и проект полномочий переданной области, возвращая проект помощника. NoRows и
// wrapped дают NotFound; только serialization 40001 сохраняет маркер повтора, прочие ошибки —
// Unavailable без деталей драйвера. Scan может частично изменить область при ошибке; вызывающий
// отбрасывает результат. Транзакция, membership и авторизация ресурса принадлежат вызывающему.
func (repository *Repository) resolveAssistantReadLease(ctx context.Context, tx pgx.Tx, current *scope, leaseRef, fence string, generation int64) (string, error) {
	fenceDigest := sha256.Sum256([]byte(fence))
	var assistantProjectRef string
	err := tx.QueryRow(ctx, queryAssistantSearchResolveLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "lease_ref": leaseRef,
		"fence_digest": hex.EncodeToString(fenceDigest[:]), "generation": generation,
	}).Scan(&current.actorRef, &current.actorID, &current.authorityProjectID, &assistantProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrNotFound
	}
	if err != nil {
		return "", assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return assistantProjectRef, nil
}

// ListAssistantIntegrationDefinitions wraps the whole exact-lease definition page in the existing
// bounded locked-read retry, preserving query/offset and the entries/next tuple. Failed attempts
// are discarded by that wrapper; the global definition page is not a grant to use an integration.
// ListAssistantIntegrationDefinitions оборачивает полную страницу определений с проверкой точной
// lease в прежний ограниченный повтор, сохраняя поиск, смещение и пару entries/next. Неуспешная
// попытка отбрасывается общей обёрткой; общий каталог не разрешает использование интеграции.
func (repository *Repository) ListAssistantIntegrationDefinitions(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string, offset int32) ([]entity.AssistantIntegrationDefinition, int32, error) {
	// page carries definition entries and next offset through the generic transaction retry without
	// changing their nil/empty or error semantics.
	// page переносит определения и следующее смещение через общий повтор транзакции, сохраняя форму
	// nil/empty и ошибки.
	type page struct {
		entries []entity.AssistantIntegrationDefinition
		next    int32
	}
	result, err := retryAssistantLockedRead(ctx, func(attemptCtx context.Context) (page, error) {
		entries, next, err := repository.listAssistantIntegrationDefinitionsOnce(attemptCtx, principal, leaseRef, fence, generation, search, offset)
		return page{entries: entries, next: next}, err
	})
	return result.entries, result.next, err
}

// listAssistantIntegrationDefinitionsOnce opens a lease-locked RepeatableRead snapshot, checks the
// persisted actor, then reads ten enabled global definitions plus a continuation row. It decodes
// bounded schemas/capabilities, returns only capability keys and credential-key names, and commits
// before success. Begin/query/scan/iteration/commit use the existing locked-read error policy;
// deferred bounded rollback may override failure.
// listAssistantIntegrationDefinitionsOnce открывает RepeatableRead снимок с блокировкой lease,
// проверяет сохранённого субъекта, затем читает десять общих определений и дополнительную строку.
// Декодирует ограниченные schema/capabilities, возвращает ключи capabilities и имена credential
// keys, фиксирует транзакцию до успеха. Ошибки Begin, query, Scan, iteration и commit сохраняют
// locked-read правило; ограниченный rollback может заменить ошибку.
func (repository *Repository) listAssistantIntegrationDefinitionsOnce(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string, offset int32) (_ []entity.AssistantIntegrationDefinition, _ int32, resultError error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	leaseScope := current
	_, err = repository.resolveAssistantReadLease(ctx, tx, &leaseScope, leaseRef, fence, generation)
	if err != nil {
		return nil, 0, err
	}
	if _, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, leaseScope.actorRef); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, queryAssistantIntegrationDefinitions, pgx.StrictNamedArgs{
		"query": search, "limit": maximumAssistantIntegrationDefinitions + 1, "offset": offset,
	})
	if err != nil {
		return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	definitions := make([]entity.AssistantIntegrationDefinition, 0, maximumAssistantIntegrationDefinitions)
	for rows.Next() {
		var item entity.AssistantIntegrationDefinition
		var schema, capabilities []byte
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Category,
			&item.Adapter, &item.CredentialSecretKey, &schema, &capabilities, &item.Origin); err != nil {
			rows.Close()
			return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		if len(definitions) == maximumAssistantIntegrationDefinitions {
			rows.Close()
			if err := tx.Commit(ctx); err != nil {
				return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
			}
			return definitions, offset + maximumAssistantIntegrationDefinitions, nil
		}
		var entries []entity.IntegrationCapability
		if json.Unmarshal(schema, &item.ConfigurationFields) != nil || json.Unmarshal(capabilities, &entries) != nil ||
			len(item.ConfigurationFields) > 100 || len(entries) > 100 {
			rows.Close()
			return nil, 0, errs.ErrUnavailable
		}
		for _, entry := range entries {
			item.CapabilityKeys = append(item.CapabilityKeys, entry.Key)
		}
		definitions = append(definitions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return definitions, 0, nil
}

// SearchAssistantResources retries the complete lease-locked search within the shared bounded
// policy and returns its visible entries/truncated tuple. It does not authorize commands or widen
// the project restriction selected from persisted assistant ownership.
// SearchAssistantResources повторяет полное чтение поиска с блокировкой lease по общей
// ограниченной политике и возвращает видимые entries/truncated. Не разрешает команды и не
// расширяет проектную область сохранённого владельца помощника.
func (repository *Repository) SearchAssistantResources(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string) ([]entity.SearchResult, bool, error) {
	// page packages visible search entries and truncation state for a single successful generic retry
	// result; no authority or SQL state is carried in this local wrapper.
	// page объединяет видимые результаты и признак усечения для успешного общего повтора; полномочия и
	// SQL-состояние локальная оболочка не переносит.
	type page struct {
		entries   []entity.SearchResult
		truncated bool
	}
	result, err := retryAssistantLockedRead(ctx, func(attemptCtx context.Context) (page, error) {
		entries, truncated, err := repository.searchAssistantResourcesOnce(attemptCtx, principal, leaseRef, fence, generation, search)
		return page{entries: entries, truncated: truncated}, err
	})
	return result.entries, result.truncated, err
}

// searchAssistantResourcesOnce resolves the exact locked assistant lease, fresh access subject and
// bindings in RepeatableRead, then gathers at most 500 ordinary/extra candidates for its persisted
// project. It ranks by relevance/time/kind/ref and applies the closed resource-kind and
// authoritative visibility rules at one evaluation time. At most ten visible entries are returned
// with trimmed text and a 160-rune subtitle; any later visible item marks truncation.
// Commit/rollback/retry ownership remains local and no hidden result is returned on failure.
// searchAssistantResourcesOnce разрешает точную lease помощника, актуального субъекта и bindings в
// RepeatableRead, собирает до 500 обычных и дополнительных кандидатов сохранённого проекта.
// Ранжирует по релевантности, времени, виду и ссылке, применяет закрытый перечень видов и
// авторитетную видимость на один момент оценки. Возвращает до десяти видимых элементов с
// нормализованным текстом и subtitle до 160 рун; следующий видимый ставит truncated. Commit,
// rollback и повтор остаются локальными; при ошибке результаты не выдаются.
func (repository *Repository) searchAssistantResourcesOnce(ctx context.Context, principal value.Principal, leaseRef, fence string, generation int64, search string) (_ []entity.SearchResult, _ bool, resultError error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, false, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	assistantProjectRef, err := repository.resolveAssistantReadLease(ctx, tx, &current, leaseRef, fence, generation)
	if err != nil {
		return nil, false, err
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, false, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, false, err
	}
	candidates := make([]rankedSearchResult, 0, maximumAssistantSearchCandidates)
	for _, statement := range []string{queryQueriesSearchSelectEligibleResources, queryAssistantSearchExtra} {
		rows, queryErr := tx.Query(ctx, statement, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "query": search, "project_ref": assistantProjectRef,
		})
		if queryErr != nil {
			return nil, false, assistantLockedReadError(queryErr, errs.ErrUnavailable)
		}
		for rows.Next() {
			if len(candidates) == maximumAssistantSearchCandidates {
				rows.Close()
				return nil, false, errs.ErrInvalid
			}
			var item rankedSearchResult
			if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle,
				&item.State, &item.UpdatedAt, &item.relevance, &item.orderTime); err != nil {
				rows.Close()
				return nil, false, assistantLockedReadError(err, errs.ErrUnavailable)
			}
			candidates = append(candidates, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, false, assistantLockedReadError(err, errs.ErrUnavailable)
		}
		rows.Close()
	}
	sortSearchResults(candidates)
	result := make([]entity.SearchResult, 0, maximumAssistantSearchResults)
	truncated := false
	evaluatedAt := time.Now().UTC()
	for _, item := range candidates {
		if item.Kind != "PROJECT" && item.Kind != "AGENT" && item.Kind != "WORKFLOW" && item.Kind != "RUN" &&
			item.Kind != "ROLE_IMAGE" && item.Kind != "RUNTIME_ENVIRONMENT" && item.Kind != "SCHEDULE" &&
			item.Kind != "INTEGRATION" && item.Kind != "SECRET" && item.Kind != "ARTIFACT" {
			continue
		}
		visible, err := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			item.Kind, item.Ref, item.ProjectRef, evaluatedAt)
		if err != nil {
			return nil, false, err
		}
		if !visible {
			continue
		}
		if len(result) == maximumAssistantSearchResults {
			truncated = true
			continue
		}
		item.SearchResult.Title = strings.TrimSpace(item.Title)
		item.SearchResult.Subtitle = strings.TrimSpace(item.Subtitle)
		if len([]rune(item.Subtitle)) > 160 {
			item.SearchResult.Subtitle = string([]rune(item.Subtitle)[:160])
		}
		result = append(result, item.SearchResult)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return result, truncated, nil
}
