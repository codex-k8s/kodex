package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// GetBootstrapState resolves the actor and system assistant, reads bootstrap/onboarding timestamps
// and organization project count, then derives onboarding actions. STT configuration errors become
// safe availability reasons rather than failing bootstrap; Ready is configuration readback, not a
// live transcription probe. Reads are separate rather than one snapshot.
// GetBootstrapState разрешает субъекта и системного помощника, читает отметки bootstrap/onboarding
// и число проектов организации, затем вычисляет действия onboarding. Ошибки конфигурации STT
// превращаются в безопасные причины недоступности без отказа всего bootstrap; Ready отражает
// конфигурацию, а не реальную транскрипцию. Чтения выполняются отдельно, не в одном снимке.
func (repository *Repository) GetBootstrapState(ctx context.Context, principal value.Principal) (platformrepo.BootstrapState, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.BootstrapState{}, err
	}
	assistant, err := repository.getAssistant(ctx, scope)
	if err != nil {
		return platformrepo.BootstrapState{}, err
	}
	var state platformrepo.BootstrapState
	var bootstrappedAt, onboardingAt *time.Time
	err = repository.pool.QueryRow(ctx, queryQueriesGetbootstrapstateSelectProjectsOrganizationIdLifecycleSingleton, scope.organizationID).Scan(&bootstrappedAt, &onboardingAt, &state.ProjectCount)
	if err != nil {
		return platformrepo.BootstrapState{}, errs.ErrUnavailable
	}
	state.Bootstrapped = bootstrappedAt != nil
	state.OnboardingCompleted = onboardingAt != nil
	state.OrganizationRef = scope.organizationRef
	state.Assistant = assistant
	state.Actor = entity.User{Ref: scope.actorRef, DisplayName: scope.actorName, Active: true}
	state.PlatformRole = scope.role
	state.SpeechTranscription.Reason = "STT_NOT_CONFIGURED"
	configuration, configurationErr := repository.GetSystemSTTConfiguration(ctx, principal)
	if configurationErr == nil {
		state.SpeechTranscription.Eligible = configuration.Ready
		state.SpeechTranscription.Reason = "STT_RUNTIME_UNVERIFIED"
		if len(configuration.ReadinessBlockers) != 0 {
			state.SpeechTranscription.Reason = configuration.ReadinessBlockers[0]
		}
	} else if !errors.Is(configurationErr, errs.ErrNotFound) {
		state.SpeechTranscription.Reason = "STT_CONFIGURATION_UNAVAILABLE"
	}
	if !state.OnboardingCompleted && (scope.role == "OWNER" || scope.role == "ADMINISTRATOR") {
		state.NextActions = []string{"COMPLETE_ONBOARDING"}
	}
	return state, nil
}

// GetPlatformEventCursor returns the resolved organization reference together with the
// installation-wide event sequence; query failures map to Unavailable and the cursor itself grants
// no subscription authority.
// GetPlatformEventCursor возвращает ссылку разрешённой организации и общую последовательность
// событий установки; ошибки запроса отображаются в Unavailable, сам курсор не предоставляет право
// подписки.
func (repository *Repository) GetPlatformEventCursor(ctx context.Context, principal value.Principal) (string, int64, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return "", 0, err
	}
	var sequence int64
	if err := repository.pool.QueryRow(ctx, queryQueriesGetplatformeventcursorSelectInstallationPlatformSequence).Scan(&sequence); err != nil {
		return "", 0, errs.ErrUnavailable
	}
	return scope.organizationRef, sequence, nil
}

// GetOverview combines three independently authorized first pages of runs, open gates and
// artifacts with organization-wide counters. ActiveRuns filters only the fetched twenty-run page;
// counters are not scoped to the optional project filter and this is not an atomic snapshot.
// GetOverview объединяет три независимо авторизованные первые страницы запусков, открытых шлюзов и
// артефактов со счётчиками всей организации. ActiveRuns фильтрует лишь полученную страницу из
// двадцати запусков; счётчики не ограничены необязательным фильтром проекта, общего атомарного
// снимка нет.
func (repository *Repository) GetOverview(ctx context.Context, principal value.Principal, projectRef string) (platformrepo.Overview, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	filter := query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}}
	runs, _, _, err := repository.ListRuns(ctx, principal, filter)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	gates, _, _, err := repository.ListOwnerGates(ctx, principal, query.Filter{ProjectRef: projectRef, State: "OPEN", Page: query.Page{Size: 20}})
	if err != nil {
		return platformrepo.Overview{}, err
	}
	artifacts, _, _, err := repository.ListArtifacts(ctx, principal, filter)
	if err != nil {
		return platformrepo.Overview{}, err
	}
	var result platformrepo.Overview
	err = repository.pool.QueryRow(ctx, queryQueriesGetoverviewSelectProjectsOrganizationIdLifecycleState, scope.organizationID).Scan(
		&result.ProjectCount, &result.AgentCount, &result.ActiveRunCount, &result.PendingGateCount)
	if err != nil {
		return platformrepo.Overview{}, errs.ErrUnavailable
	}
	for _, run := range runs {
		if run.State == "QUEUED" || run.State == "RUNNING" || run.State == "WAITING_HUMAN" || run.State == "CANCELLING" {
			result.ActiveRuns = append(result.ActiveRuns, run)
		}
	}
	result.PendingGates = gates
	result.RecentArtifacts = artifacts
	return result, nil
}

// ListCapabilities requires a resolved actor scope and lists enabled capability descriptions and
// risk. Query/scan errors map to Unavailable; a final iteration error is returned unchanged with
// any accumulated rows.
// ListCapabilities требует разрешённую область субъекта и перечисляет включённые возможности с
// описанием и риском. Ошибки запроса/сканирования отображаются в Unavailable; конечная ошибка
// итерации возвращается без преобразования вместе с накопленными строками.
func (repository *Repository) ListCapabilities(ctx context.Context, principal value.Principal) ([]entity.IntegrationCapability, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationCapability
	for rows.Next() {
		var item entity.IntegrationCapability
		if err := rows.Scan(&item.Key, &item.Name, &item.Description, &item.Risk); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// ListRuntimes lists enabled runtime profiles after scope resolution and marks each returned
// selection Ready without probing a provider. Query/scan failures discard results as Unavailable;
// final iteration errors retain the accumulated result and raw error.
// ListRuntimes перечисляет включённые профили после разрешения области и помечает каждый Ready без
// проверки провайдера. Ошибки запроса/сканирования отбрасывают результат с Unavailable; конечная
// ошибка итерации сохраняет накопленный результат и исходную ошибку.
func (repository *Repository) ListRuntimes(ctx context.Context, principal value.Principal) ([]entity.RuntimeSelection, error) {
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return nil, err
	}
	rows, err := repository.pool.Query(ctx, queryQueriesListruntimesSelectRuntimeProfilesEnabled)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RuntimeSelection
	for rows.Next() {
		var item entity.RuntimeSelection
		if err := rows.Scan(&item.Ref, &item.Name, &item.Provider, &item.Model, &item.RuntimeRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		item.Ready = true
		result = append(result, item)
	}
	return result, rows.Err()
}

// Search collects text candidates in a read-only RepeatableRead transaction, evaluates current
// resource visibility before ranking/counting/pagination, and caps pages at fifty. Its versioned
// cursor binds normalized query/project, not identity; access is evaluated afresh. No partial
// results escape errors, and commit failure maps to Conflict.
// Search собирает текстовых кандидатов в read-only RepeatableRead транзакции, проверяет текущую
// видимость до ранжирования, подсчёта и пагинации, ограничивает страницу пятьюдесятью строками.
// Версионный курсор связан с нормализованными запросом/проектом, не с личностью; доступ
// проверяется заново. Ошибки исключают частичный результат, отказ commit отображается в Conflict.
func (repository *Repository) Search(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.SearchResult, int64, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	limit := boundedPage(filter.Page)
	if limit > 50 {
		limit = 50
	}
	cursor, err := decodeSearchCursor(filter.Page.Token, filter.Query, filter.ProjectRef)
	if err != nil {
		return nil, 0, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryQueriesSearchSelectEligibleResources, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "query": filter.Query,
		"project_ref": strings.TrimSpace(filter.ProjectRef),
	})
	if err != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	defer rows.Close()
	candidates := make([]rankedSearchResult, 0)
	for rows.Next() {
		var item rankedSearchResult
		if err := rows.Scan(&item.Kind, &item.Ref, &item.ProjectRef, &item.Title, &item.Subtitle,
			&item.State, &item.UpdatedAt, &item.relevance, &item.orderTime); err != nil {
			return nil, 0, "", errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	if rows.Err() != nil {
		return nil, 0, "", errs.ErrUnavailable
	}
	rows.Close()
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, 0, "", err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, 0, "", err
	}
	evaluatedAt := time.Now().UTC()
	ranked := make([]rankedSearchResult, 0, len(candidates))
	for _, item := range candidates {
		visible, visibilityErr := repository.resourceVisible(ctx, tx, current, subject.AccessSubject, bindings,
			item.Kind, item.Ref, item.ProjectRef, evaluatedAt)
		if visibilityErr != nil {
			return nil, 0, "", visibilityErr
		}
		if visible {
			ranked = append(ranked, item)
		}
	}
	sortSearchResults(ranked)
	total := int64(len(ranked))
	if cursor.Time != nil {
		remaining := ranked[:0]
		for _, item := range ranked {
			if searchResultAfterCursor(item.relevance, item.orderTime, item.Kind, item.Ref, cursor) {
				remaining = append(remaining, item)
			}
		}
		ranked = remaining
	}
	next := ""
	if len(ranked) > int(limit) {
		ranked = ranked[:limit]
		last := ranked[len(ranked)-1]
		next = encodeSearchCursor(searchCursor{Relevance: last.relevance, Time: &last.orderTime, Kind: last.Kind, Ref: last.Ref}, filter.Query, filter.ProjectRef)
		if next == filter.Page.Token {
			return nil, 0, "", errs.ErrConflict
		}
	}
	result := make([]entity.SearchResult, 0, len(ranked))
	for _, item := range ranked {
		result = append(result, item.SearchResult)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, "", errs.ErrConflict
	}
	return result, total, next, nil
}

// searchResultAfterCursor compares ascending relevance, descending time and ascending kind/NUL/ref
// to select rows strictly after a decoded search position; the caller must supply a nonnil cursor
// time.
// searchResultAfterCursor сравнивает релевантность по возрастанию, время по убыванию и
// kind/NUL/ref по возрастанию для выбора строк строго после позиции; вызывающий обязан передать
// ненулевое время курсора.
func searchResultAfterCursor(relevance int, createdAt time.Time, kind, ref string, cursor searchCursor) bool {
	return relevance > cursor.Relevance ||
		relevance == cursor.Relevance && createdAt.Before(*cursor.Time) ||
		relevance == cursor.Relevance && createdAt.Equal(*cursor.Time) && kind+"\x00"+ref > cursor.Kind+"\x00"+cursor.Ref
}

// rankedSearchResult pairs a public candidate with the relevance bucket and timestamp used by both
// ordinary and assistant search. These private ordering fields never establish visibility or
// authority; each reader applies its own permission, count and pagination rules.
// rankedSearchResult связывает публичного кандидата с релевантностью и временем для обычного поиска
// и поиска помощника. Закрытые поля сортировки не задают видимость и полномочия; права, подсчёт и
// пагинацию каждый reader проверяет отдельно.
type rankedSearchResult struct {
	entity.SearchResult
	relevance int
	orderTime time.Time
}

// sortSearchResults sorts the supplied slice in place by ascending relevance, descending timestamp
// and ascending kind/NUL/ref, using the same unstable sort as both original readers. Equal keys
// remain equivalent without an added tie-breaker; nil and empty slices are accepted. It performs
// no filtering, truncation or mutation of candidate fields.
// sortSearchResults сортирует переданный срез на месте по возрастанию релевантности, убыванию времени
// и возрастанию kind/NUL/ref тем же нестабильным алгоритмом, что оба исходных reader. Равные ключи
// остаются равными без нового условия порядка; nil и пустой срез допустимы. Фильтрация, усечение и
// изменение полей кандидатов не выполняются.
func sortSearchResults(results []rankedSearchResult) {
	sort.Slice(results, func(left, right int) bool {
		if results[left].relevance != results[right].relevance {
			return results[left].relevance < results[right].relevance
		}
		if !results[left].orderTime.Equal(results[right].orderTime) {
			return results[left].orderTime.After(results[right].orderTime)
		}
		return results[left].Kind+"\x00"+results[left].Ref < results[right].Kind+"\x00"+results[right].Ref
	})
}

// resourceVisible maps a known kind to its view permission and resolves an exact
// organization/resource target, hiding absent targets. Environments additionally resolve their own
// project; project-membership presentation bindings are excluded for other resource kinds. The
// shared evaluator decides using the supplied subject, bindings and evaluation time.
// resourceVisible сопоставляет известный вид с правом просмотра и разрешает точный ресурс
// организации, скрывая отсутствующие цели. Окружение дополнительно разрешает собственный проект;
// presentation bindings проектного membership исключаются для остальных видов ресурсов. Общий
// evaluator принимает решение по переданным субъекту, bindings и времени.
func (repository *Repository) resourceVisible(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	subject entity.AccessSubject,
	bindings []entity.AccessBinding,
	resourceKind, resourceRef, projectRef string,
	evaluatedAt time.Time,
) (bool, error) {
	permission := visibilityPermission(resourceKind)
	if permission == "" {
		return false, nil
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: resourceKind, ResourceRef: resourceRef,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Окружение читается через право просмотра его проекта, как и в обычном
	// каталоге. Сначала разрешаем exact окружение и его project binding, чтобы
	// locator другого проекта не мог превратиться в разрешённый project read.
	if resourceKind == "RUNTIME_ENVIRONMENT" {
		target, err = repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
			Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: "PROJECT", ResourceRef: projectRef,
		})
		if errors.Is(err, errs.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	resourceBindings := bindings
	if resourceKind != "PROJECT" && resourceKind != "RUNTIME_ENVIRONMENT" {
		resourceBindings = make([]entity.AccessBinding, 0, len(bindings))
		for _, binding := range bindings {
			if binding.PresentationKind != "PROJECT_MEMBERSHIP" {
				resourceBindings = append(resourceBindings, binding)
			}
		}
	}
	return accessservice.Evaluate(subject, permission, target.scope, target.ownerSubjectRef, resourceBindings, evaluatedAt).Allowed, nil
}

// searchCursor stores version/filter digest and the relevance/time/kind/ref keyset position in
// unsigned search tokens; decoding validates shape and filter binding without treating this
// payload as authority.
// searchCursor хранит версию, дайджест фильтра и позицию relevance/time/kind/ref в неподписанном
// токене поиска; декодирование проверяет форму и связь с фильтром, не признавая payload источником
// полномочий.
type searchCursor struct {
	Version   int        `json:"v"`
	Filter    string     `json:"f"`
	Relevance int        `json:"r"`
	Time      *time.Time `json:"t"`
	Kind      string     `json:"k"`
	Ref       string     `json:"i"`
}

// searchFilterDigest hashes trimmed query and project separated by NUL and returns the first
// twelve SHA-256 bytes as raw URL base64; this compact filter binding is not a MAC.
// searchFilterDigest хеширует очищенные от крайних пробелов запрос и проект, разделённые NUL,
// возвращая первые двенадцать байт SHA-256 в raw URL base64; эта компактная связь с фильтром не
// является MAC.
func searchFilterDigest(queryValue, projectRef string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(queryValue) + "\x00" + strings.TrimSpace(projectRef)))
	return base64.RawURLEncoding.EncodeToString(digest[:12])
}

// encodeSearchCursor copies the position, stamps version two and the filter digest, then
// JSON/raw-URL-base64 encodes it; it preserves the existing ignored marshal error for this fixed
// serializable shape.
// encodeSearchCursor копирует позицию, назначает версию два и дайджест фильтра, затем кодирует
// JSON/raw-URL-base64; сохраняет прежнее игнорирование ошибки marshal для этой фиксированной
// сериализуемой структуры.
func encodeSearchCursor(cursor searchCursor, queryValue, projectRef string) string {
	cursor.Version, cursor.Filter = 2, searchFilterDigest(queryValue, projectRef)
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeSearchCursor accepts a blank token as the first page; otherwise enforces raw URL base64, a
// 512-byte decoded limit, version/filter match, a timestamp, relevance zero through two and
// nonempty kind/ref. Malformed positions return Invalid; the token is not authenticated.
// decodeSearchCursor принимает пустой токен как первую страницу; иначе требует raw URL base64, не
// более512 декодированных байт, совпадение версии/фильтра, время, релевантность от нуля до двух и
// непустые kind/ref. Некорректная позиция даёт Invalid; подлинность токена не проверяется.
func decodeSearchCursor(token, queryValue, projectRef string) (searchCursor, error) {
	if strings.TrimSpace(token) == "" {
		return searchCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 512 {
		return searchCursor{}, errs.ErrInvalid
	}
	var cursor searchCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 2 || cursor.Filter != searchFilterDigest(queryValue, projectRef) ||
		cursor.Time == nil || cursor.Relevance < 0 || cursor.Relevance > 2 || cursor.Kind == "" || cursor.Ref == "" {
		return searchCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

// ListProjects validates the filter-bound updated-at/project-ref cursor, reads eligible projects
// and presentation permissions in one read-only RepeatableRead snapshot, trims a lookahead row and
// projects current cards before commit. Owner/admin presentation actions do not replace SQL
// eligibility; failures discard the page.
// ListProjects проверяет связанный с фильтром курсор updated-at/project-ref, читает доступные
// проекты и presentation permissions в одном read-only RepeatableRead снимке, удаляет
// дополнительную строку и проецирует текущие карточки до commit. Действия owner/admin не заменяют
// SQL eligibility; ошибки отбрасывают страницу.
func (repository *Repository) ListProjects(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Project, string, []string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", nil, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	cursor, err := decodeCatalogCursor(scope, "PROJECT", filter)
	if err != nil {
		return nil, "", nil, err
	}
	cursorAt, cursorRef := "", ""
	if cursor != "" {
		var found bool
		cursorAt, cursorRef, found = strings.Cut(cursor, "|")
		if !found || !strings.HasPrefix(cursorRef, "prj_") {
			return nil, "", nil, errs.ErrInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, cursorAt); err != nil {
			return nil, "", nil, errs.ErrInvalid
		}
	}
	limit := boundedPage(filter.Page)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryQueriesListprojectsSelectProjectsOrganizationIdProjectIdSubjectId, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "actor_id": scope.actorID, "query": filter.Query,
		"authority_project": scope.authorityProjectID, "cursor_at": cursorAt, "cursor_ref": cursorRef,
		"page_size": limit + 1,
	})
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Project
	for rows.Next() {
		var item entity.Project
		var projectID string
		var permissions []string
		if err := rows.Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt, &permissions, &item.AgentCount, &item.WorkflowCount, &item.ActiveRunCount, &item.PendingGateCount); err != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
			permissions = allPermissions()
		}
		item.NextActions = projectActions(permissions)
		if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
			item.NextActions = append(item.NextActions, "DELETE")
		}
		result = append(result, item)
	}
	actions := collectionCreateActions(scope.role, "CREATE_PROJECT")
	if rows.Err() != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	rows.Close()
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeCatalogCursor(scope, "PROJECT", filter, last.UpdatedAt.UTC().Format(time.RFC3339Nano)+"|"+last.Ref)
	}
	selected := make([]*entity.Project, len(result))
	for index := range result {
		selected[index] = &result[index]
	}
	if err := projectProjectCards(ctx, tx, scope, selected); err != nil {
		return nil, "", nil, err
	}
	if tx.Commit(ctx) != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	return result, next, actions, nil
}

// GetProject reads one eligible organization project in a read-only RepeatableRead transaction,
// loads membership presentation permissions for nonprivileged actors and projects the current
// card. Missing rows become NotFound; query/commit failures become Unavailable.
// GetProject читает доступный проект организации в read-only RepeatableRead транзакции, загружает
// presentation permissions membership для обычного субъекта и проецирует текущую карточку.
// Отсутствие строки даёт NotFound; ошибки запроса/commit дают Unavailable.
func (repository *Repository) GetProject(ctx context.Context, principal value.Principal, ref string) (entity.Project, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Project{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item entity.Project
	var projectID string
	err = tx.QueryRow(ctx, queryQueriesGetprojectSelectProjectsOrganizationIdRefProjectId,
		scope.organizationID, ref, scope.actorID, scope.authorityProjectID).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.AgentCount, &item.WorkflowCount, &item.ActiveRunCount, &item.PendingGateCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Project{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Project{}, errs.ErrUnavailable
	}
	permissions := allPermissions()
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		if err := tx.QueryRow(ctx, queryListProjectPermissions, scope.organizationID, projectID, scope.actorID).Scan(&permissions); err != nil {
			return entity.Project{}, errs.ErrUnavailable
		}
	}
	item.NextActions = projectActions(permissions)
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		item.NextActions = append(item.NextActions, "DELETE")
	}
	if err := projectProjectCards(ctx, tx, scope, []*entity.Project{&item}); err != nil {
		return entity.Project{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.Project{}, errs.ErrUnavailable
	}
	return item, nil
}

// projectActions starts with OPEN and maps the supplied presentation permissions to
// edit/create/manage/upload hints in stable order; it neither checks resource eligibility nor adds
// the privileged DELETE hint owned by callers.
// projectActions начинает с OPEN и в стабильном порядке преобразует переданные presentation
// permissions в подсказки edit/create/manage/upload; не проверяет доступ к ресурсу и не добавляет
// привилегированную DELETE, принадлежащую вызывающим.
func projectActions(permissions []string) []string {
	actions := []string{"OPEN"}
	mappings := []struct{ permission, action string }{
		{"MANAGE", "EDIT"},
		{"MANAGE_AGENTS", "CREATE_AGENT"},
		{"MANAGE_WORKFLOWS", "CREATE_WORKFLOW"},
		{"LAUNCH_RUNS", "CREATE_RUN"},
		{"MANAGE_SCHEDULES", "CREATE_SCHEDULE"},
		{"MANAGE_INTEGRATIONS", "MANAGE_INTEGRATIONS"},
		{"MANAGE_MEMBERS", "MANAGE_MEMBERS"},
		{"MANAGE_ARTIFACTS", "UPLOAD_ARTIFACT"},
	}
	for _, mapping := range mappings {
		if contains(permissions, mapping.permission) {
			actions = append(actions, mapping.action)
		}
	}
	return actions
}

// ListPlatformMemberships restricts the platform membership page to owner/admin, preserving
// distinct subject/binding active flags and deriving edit/revoke hints. It returns an empty
// continuation token even when the SQL page limit truncates rows; scan/iteration failures discard
// results.
// ListPlatformMemberships ограничивает страницу участников платформы owner/admin, сохраняет разные
// признаки активности субъекта и membership, вычисляет edit/revoke. Возвращает пустой токен
// продолжения даже при обрезке SQL лимитом; ошибки сканирования/итерации отбрасывают результат.
func (repository *Repository) ListPlatformMemberships(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return nil, "", errs.ErrForbidden
	}
	rows, err := repository.pool.Query(ctx, queryPlatformMembershipList, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"page_size":       boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.Membership
	for rows.Next() {
		var item entity.Membership
		if err := rows.Scan(
			&item.Ref, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Active, &item.Version,
		); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.Permissions = []string{}
		item.NextActions = platformMembershipActions(scope, item)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, "", nil
}

// ListPlatformMembershipCandidates requires owner/admin and queries active verified-OIDC
// organization subjects without any platform membership, matching a trimmed query. It delegates
// only four-column row decoding and returns no continuation token; candidate status grants no
// membership authority.
// ListPlatformMembershipCandidates требует owner/admin и выбирает активных verified-OIDC субъектов
// организации без платформенного membership по очищенному запросу. Делегирует лишь чтение четырёх
// колонок и не возвращает токен продолжения; статус кандидата не предоставляет право участия.
func (repository *Repository) ListPlatformMembershipCandidates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.User, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return nil, "", errs.ErrForbidden
	}
	rows, err := repository.pool.Query(ctx, queryPlatformMembershipListCandidates, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"query":           strings.TrimSpace(filter.Query),
		"page_size":       boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result, err := readMembershipCandidates(rows)
	return result, "", err
}

// ListMemberships uses the authorized MEMBERSHIP catalog with exact actor/authority-project SQL
// arguments, scanned subject/binding state and per-item edit/revoke hints. Catalog authorization
// and pagination remain separate from those presentation hints; row iteration errors are passed to
// the catalog.
// ListMemberships использует авторизованный каталог MEMBERSHIP с точными SQL аргументами
// субъекта/authority-project, состояниями субъекта/membership и подсказками edit/revoke.
// Авторизация и пагинация каталога отделены от подсказок; ошибки итерации передаются каталогу.
func (repository *Repository) ListMemberships(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	return authorizedCatalog(ctx, repository, scope, "MEMBERSHIP", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Membership, error) {
			rows, err := tx.Query(ctx, queryProjectMembershipList, pgx.StrictNamedArgs{
				"actor_id": scope.actorID, "authority_project": scope.authorityProjectID,
				"organization_id": scope.organizationID, "project_ref": filter.ProjectRef,
				"query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Membership
			for rows.Next() {
				var item entity.Membership
				if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions, &item.Active, &item.Version); err != nil {
					return nil, errs.ErrUnavailable
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Membership) entity.AccessScope {
			return entity.AccessScope{ResourceKind: "MEMBERSHIP", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Membership, _ func(string) bool) error {
			item.NextActions = projectMembershipActions(scope, *item)
			return nil
		})
}

// ListMembershipCandidates queries active platform members lacking any membership in the requested
// active project, preserving SQL owner/admin-or-MANAGE_MEMBERS selection and actor/project
// arguments. It shares row decoding with platform candidates and intentionally returns no
// continuation token.
// ListMembershipCandidates выбирает активных участников платформы без membership в запрошенном
// активном проекте, сохраняя SQL отбор owner/admin либо MANAGE_MEMBERS и аргументы
// субъекта/проекта. Разделяет чтение строк с платформенными кандидатами и намеренно не возвращает
// токен продолжения.
func (repository *Repository) ListMembershipCandidates(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.User, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	rows, err := repository.pool.Query(ctx, queryProjectMembershipListCandidates, pgx.StrictNamedArgs{
		"organization_id":     scope.organizationID,
		"project_ref":         filter.ProjectRef,
		"actor_platform_role": scope.role,
		"actor_id":            scope.actorID,
		"query":               strings.TrimSpace(filter.Query),
		"page_size":           boundedPage(filter.Page),
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result, err := readMembershipCandidates(rows)
	return result, "", err
}

// readMembershipCandidates collects the common four-column user projection from already authorized
// candidate rows without issuing SQL or closing caller-owned rows. Empty success preserves a nil
// slice; scan or final iteration failure discards all collected users and maps to Unavailable
// without exposing driver diagnostics.
// readMembershipCandidates собирает общую четырёхколоночную проекцию пользователя из уже
// авторизованных строк кандидатов без SQL и закрытия строк вызывающего. Пустой успешный результат
// сохраняет nil срез; ошибка scan либо конечной итерации отбрасывает всех собранных пользователей
// и даёт Unavailable без раскрытия диагностики драйвера.
func readMembershipCandidates(rows pgx.Rows) ([]entity.User, error) {
	var result []entity.User
	for rows.Next() {
		var item entity.User
		if err := rows.Scan(&item.Ref, &item.DisplayName, &item.EmailMasked, &item.Active); err != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// platformMembershipActions returns no hints for nonprivileged actors, self changes or an
// administrator acting on an owner. Otherwise it permits EDIT and adds REVOKE only for an active
// binding; last-owner checks remain command-time policy.
// platformMembershipActions не возвращает подсказок обычному субъекту, при изменении себя и для
// administrator над owner. Иначе предлагает EDIT и добавляет REVOKE лишь для активного membership;
// проверка последнего owner остаётся политикой команды.
func platformMembershipActions(scope scope, item entity.Membership) []string {
	if scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
		return []string{}
	}
	if item.User.Ref == scope.actorRef {
		return []string{}
	}
	if scope.role != "OWNER" && item.Role == "OWNER" {
		return []string{}
	}
	actions := []string{"EDIT"}
	if item.Active && item.User.Ref != scope.actorRef {
		actions = append(actions, "REVOKE")
	}
	return actions
}

// projectMembershipActions suppresses self-edit hints and otherwise returns EDIT plus REVOKE for
// active bindings; callers must already have established project management eligibility.
// projectMembershipActions подавляет подсказки изменения себя, иначе возвращает EDIT и REVOKE для
// активного membership; вызывающий уже должен проверить право управления проектом.
func projectMembershipActions(scope scope, item entity.Membership) []string {
	if item.User.Ref == scope.actorRef {
		return []string{}
	}
	actions := []string{"EDIT"}
	if item.Active && item.User.Ref != scope.actorRef {
		actions = append(actions, "REVOKE")
	}
	return actions
}

// ListAgents loads candidate agents through the authorized catalog, applies avatar/card readback
// before per-item authorization projection, then attaches instructions/grants and recomputes
// actions from exact manage/launch permissions. It keeps the transaction and cursor ownership in
// the catalog rather than exposing raw SQL flags as authority.
// ListAgents загружает кандидатов агентов через авторизованный каталог, проецирует avatar/карточки
// до поэлементной авторизации, затем добавляет инструкции/grants и пересчитывает действия по
// точным manage/launch permissions. Транзакцией и курсором владеет каталог; исходные SQL flags не
// выдаются за полномочия.
func (repository *Repository) ListAgents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	result, next, err := authorizedCatalog(ctx, repository, scope, "AGENT", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Agent, error) {
			rows, err := tx.Query(ctx, queryQueriesListagentsSelectAgentsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, limit, cursor, scope.authorityProjectID)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var result []entity.Agent
			for rows.Next() {
				var item entity.Agent
				var canManage, canLaunch bool
				if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch); err != nil {
					return nil, errs.ErrUnavailable
				}
				setAgentAvatarReadback(&item)
				result = append(result, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			rows.Close()
			selected := make([]*entity.Agent, len(result))
			for index := range result {
				selected[index] = &result[index]
			}
			if err := projectAgentCards(ctx, tx, scope, selected); err != nil {
				return nil, err
			}
			return result, nil
		}, func(item entity.Agent) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(tx pgx.Tx, item *entity.Agent, allowed func(string) bool) error {
			if err := repository.attachInstructionsFrom(ctx, tx, scope, item); err != nil {
				return errs.ErrUnavailable
			}
			if err := repository.attachAgentGrantsFrom(ctx, tx, scope, item); err != nil {
				return errs.ErrUnavailable
			}
			item.NextActions = agentActions(*item, allowed("agent.manage"), allowed("agent.launch"))
			return nil
		})
	if err != nil {
		return nil, "", err
	}
	return result, next, nil
}

// GetAgent reads an actor-scoped agent in a read-only snapshot and additionally resolves
// ordinary-agent assistant context. It projects avatar/system state, instructions, integration
// grants and cards before commit; missing rows are NotFound and failed attachments/commit are
// Unavailable.
// GetAgent читает агента с областью субъекта в read-only снимке и дополнительно разрешает
// assistant context обычного агента. До commit проецирует avatar/system state, инструкции,
// integration grants и карточки; отсутствие строки даёт NotFound, отказ добавления данных/commit
// даёт Unavailable.
func (repository *Repository) GetAgent(ctx context.Context, principal value.Principal, ref string) (entity.Agent, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Agent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item entity.Agent
	var canManage, canLaunch bool
	err = tx.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey, scope.organizationID, ref, scope.role, scope.actorID).Scan(
		&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled, &item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt, &item.UpdatedAt, &canManage, &canLaunch)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Agent{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if item.SystemKey == "" {
		if _, err := repository.resolveAssistantContext(ctx, tx, scope, entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: ref}, item.ProjectRef); err != nil {
			return entity.Agent{}, err
		}
	}
	setAgentAvatarReadback(&item)
	item.System = item.SystemKey != ""
	item.NextActions = agentActions(item, canManage, canLaunch)
	if err := repository.attachInstructionsFrom(ctx, tx, scope, &item); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	item.NextActions = agentActions(item, canManage, canLaunch)
	if err := repository.attachAgentGrantsFrom(ctx, tx, scope, &item); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if err := projectAgentCards(ctx, tx, scope, []*entity.Agent{&item}); err != nil {
		return entity.Agent{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	return item, nil
}

// agentActions derives ordered UI hints from supplied manage/launch flags, system/archive/enabled
// state and instruction versions. System agents expose only OPEN and optional RECOVER; ordinary
// agents may validate/publish/rollback instructions, and assistant-backed agents omit ARCHIVE.
// These hints do not execute or authorize mutations.
// agentActions вычисляет упорядоченные подсказки по manage/launch flags, system/archive/enabled и
// версиям инструкций. Системный агент предлагает только OPEN и возможный RECOVER; обычный может
// validate/publish/rollback инструкции, assistant-backed агент не предлагает ARCHIVE. Подсказки не
// исполняют и не авторизуют изменения.
func agentActions(agent entity.Agent, canManage, canLaunch bool) []string {
	if agent.System {
		actions := []string{"OPEN"}
		if canManage {
			actions = append(actions, "RECOVER")
		}
		return actions
	}
	actions := []string{"OPEN"}
	if agent.State == "ARCHIVED" {
		return actions
	}
	if canManage {
		actions = append(actions, "EDIT", "MANAGE_CAPABILITIES")
	}
	if canLaunch && agent.Enabled && agent.State == "READY" {
		actions = append(actions, "LAUNCH")
	}
	if canManage && agent.Enabled && agent.State == "READY" {
		actions = append(actions, "DISABLE")
	}
	if canManage && !agent.Enabled {
		actions = append(actions, "ENABLE")
	}
	if canManage {
		if !agent.AssistantBacked {
			actions = append(actions, "ARCHIVE")
		}
		switch {
		case agent.DraftInstructions != nil && agent.DraftInstructions.State == "VALID":
			actions = append(actions, "PUBLISH")
		case agent.DraftInstructions != nil:
			actions = append(actions, "VALIDATE")
		}
		if len(agent.PublishedInstructionVersions) > 1 {
			actions = append(actions, "ROLLBACK")
		}
	}
	return actions
}

// canManageAgent recognizes only the EDIT hint in an existing action list; it is a presentation
// helper rather than an independent permission evaluation.
// canManageAgent распознаёт только подсказку EDIT в готовом списке действий; служит представлению,
// не независимой проверке полномочий.
func canManageAgent(actions []string) bool {
	return slices.Contains(actions, "EDIT")
}

// canLaunchAgent recognizes only the LAUNCH hint in an existing action list without checking
// actor, target readiness or authorization itself.
// canLaunchAgent распознаёт лишь LAUNCH в готовом списке действий без самостоятельной проверки
// субъекта, готовности цели и полномочий.
func canLaunchAgent(actions []string) bool {
	return slices.Contains(actions, "LAUNCH")
}

// attachInstructions delegates instruction attachment to the pool-backed reader; callers needing
// one consistent snapshot must use attachInstructionsFrom with their transaction.
// attachInstructions делегирует добавление инструкций чтению через pool; для единого
// согласованного снимка вызывающий должен использовать attachInstructionsFrom со своей
// транзакцией.
func (repository *Repository) attachInstructions(ctx context.Context, scope scope, agent *entity.Agent) error {
	return repository.attachInstructionsFrom(ctx, repository.pool, scope, agent)
}

// attachInstructionsFrom appends ordered published instruction versions, selects the first
// published and first nonpublished version and updates the binding on the supplied agent.
// Validation-problem JSON decode errors are historically ignored; a successful empty binding read
// fails Unavailable. Partial in-memory changes can precede an error.
// attachInstructionsFrom добавляет упорядоченные опубликованные версии инструкций, выбирает первую
// published и первую непубликуемую версию, обновляет binding переданного агента. Ошибки JSON
// проблем валидации исторически игнорируются; успешное чтение без binding даёт Unavailable. До
// ошибки возможны частичные изменения памяти.
func (repository *Repository) attachInstructionsFrom(ctx context.Context, runner queryRunner, scope scope, agent *entity.Agent) error {
	rows, err := runner.Query(ctx, queryQueriesAttachinstructionsSelectInstructionVersionsOrganizationIdAgentIdRef, scope.organizationID, agent.Ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item entity.InstructionVersion
		var binding entity.AgentInstructionsBinding
		var problems []byte
		if err := rows.Scan(&item.Ref, &item.VersionNumber, &item.State, &item.Content, &item.Digest, &item.Core, &item.ParentRef, &problems, &item.CreatedAt, &item.PublishedAt, &binding.Ref, &binding.Version, &binding.RevisionRef, &binding.Effective); err != nil {
			return err
		}
		agent.InstructionBinding = &binding
		_ = json.Unmarshal(problems, &item.ValidationProblems)
		if item.State == "PUBLISHED" && agent.PublishedInstructions == nil {
			copy := item
			agent.PublishedInstructions = &copy
		}
		if item.State == "PUBLISHED" {
			agent.PublishedInstructionVersions = append(agent.PublishedInstructionVersions, item)
		} else if agent.DraftInstructions == nil {
			copy := item
			agent.DraftInstructions = &copy
		}
	}
	if rows.Err() == nil && agent.InstructionBinding == nil {
		return errs.ErrUnavailable
	}
	return rows.Err()
}

// attachAgentGrants uses the pool-backed grant reader to append integration grant references; it
// does not open a snapshot or independently authorize the agent.
// attachAgentGrants использует чтение grants через pool для добавления ссылок integration grants;
// не открывает снимок и не авторизует агента самостоятельно.
func (repository *Repository) attachAgentGrants(ctx context.Context, scope scope, agent *entity.Agent) error {
	return repository.attachAgentGrantsFrom(ctx, repository.pool, scope, agent)
}

// attachAgentGrantsFrom appends grant references selected by organization and agent using the
// supplied runner, closes rows and returns query/scan/iteration errors unchanged. Existing
// references are not cleared and partial appends may remain on failure.
// attachAgentGrantsFrom добавляет ссылки grants выбранные по организации и агенту через переданный
// runner, закрывает строки и возвращает исходные ошибки запроса/сканирования/итерации. Прежние
// ссылки не очищает; при отказе могут остаться частичные добавления.
func (repository *Repository) attachAgentGrantsFrom(ctx context.Context, runner queryRunner, scope scope, agent *entity.Agent) error {
	rows, err := runner.Query(ctx, queryQueriesAttachagentgrantsSelectIntegrationGrantsOrganizationIdTargetKindTargetRef, scope.organizationID, agent.Ref)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return err
		}
		agent.IntegrationGrantRefs = append(agent.IntegrationGrantRefs, ref)
	}
	return rows.Err()
}

// ListWorkflows reads actor/project-filtered workflow candidates through the authorized catalog,
// validates version JSON and projects launch readiness/cards in the same transaction. Exact
// per-item manage/launch evaluation then determines action hints; catalog ownership governs
// pagination and commit.
// ListWorkflows читает отфильтрованных по субъекту/проекту кандидатов workflow через
// авторизованный каталог, проверяет JSON версий и проецирует launch readiness/карточки в той же
// транзакции. Точная поэлементная manage/launch проверка затем задаёт подсказки; каталог управляет
// пагинацией и commit.
func (repository *Repository) ListWorkflows(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Workflow, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	return authorizedCatalog(ctx, repository, scope, "WORKFLOW", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Workflow, error) {
			rows, err := tx.Query(ctx, queryQueriesListworkflowsSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, strings.TrimSpace(filter.Query), filter.State, limit, cursor, scope.authorityProjectID)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Workflow
			for rows.Next() {
				item, err := scanWorkflow(rows, true)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			rows.Close()
			selected := make([]*entity.Workflow, len(items))
			for index := range items {
				selected[index] = &items[index]
			}
			if err := repository.projectWorkflowLaunchReadiness(ctx, tx, scope, selected); err != nil {
				return nil, err
			}
			if err := projectWorkflowCards(ctx, tx, scope, selected); err != nil {
				return nil, err
			}
			return items, nil
		}, func(item entity.Workflow) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "WORKFLOW", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Workflow, allowed func(string) bool) error {
			item.NextActions = workflowActions(*item, allowed("workflow.manage"), allowed("workflow.launch"))
			return nil
		})
}

// rowScanner is the minimal positional scan contract shared by single rows and iterated rows;
// readers choose their own projection and error mapping.
// rowScanner задаёт минимальный контракт позиционного сканирования одиночной и итерируемой строки;
// читатели сами выбирают проекцию и отображение ошибок.
type rowScanner interface {
	// Scan fills caller-owned destinations in query column order and reports a row/decode failure;
	// implementations own driver behavior and must be called at a valid row position.
	// Scan заполняет принадлежащие вызывающему destinations в порядке колонок и сообщает ошибку
	// строки/декодирования; поведение драйвера принадлежит реализации, вызов требует допустимой
	// текущей строки.
	Scan(...any) error
}

// queryRunner allows pool or transaction reads without transferring transaction ownership; callers
// remain responsible for authorization, result-set closure and snapshot consistency.
// queryRunner позволяет чтение через pool либо транзакцию без передачи владения транзакцией;
// вызывающий отвечает за авторизацию, закрытие строк и согласованность снимка.
type queryRunner interface {
	// Query opens a multirow result for the given context, SQL and arguments; the caller closes and
	// checks iteration errors before reusing the connection.
	// Query открывает многострочный результат по контексту, SQL и аргументам; вызывающий закрывает
	// строки и проверяет ошибки итерации до повторного использования соединения.
	Query(context.Context, string, ...any) (pgx.Rows, error)
	// QueryRow returns one deferred-scan row for the supplied context/query/arguments; absence and
	// query failures surface through Scan.
	// QueryRow возвращает одну строку с отложенным сканированием по контексту/запросу/аргументам;
	// отсутствие и ошибки запроса обнаруживаются через Scan.
	QueryRow(context.Context, string, ...any) pgx.Row
}

// scanWorkflow decodes the fixed workflow projection plus optional actor action flags, validates
// draft/published structure and publication reference, and normalizes draft identity. Internal
// reads default both flags true; that convenience does not grant authority. NoRows becomes
// NotFound and corrupt/other failures become Unavailable.
// scanWorkflow декодирует фиксированную проекцию workflow и необязательные flags действий
// субъекта, проверяет структуру draft/published и publication reference, нормализует draft
// identity. Внутреннее чтение по умолчанию ставит оба flags true; это не предоставляет полномочий.
// NoRows даёт NotFound, повреждение/остальные ошибки дают Unavailable.
func scanWorkflow(row rowScanner, actorScoped bool) (entity.Workflow, error) {
	var item entity.Workflow
	var draft, published []byte
	var publishedVersion int32
	var publishedRef string
	var canManage, canLaunch bool
	destinations := []any{&item.Ref, &item.ProjectRef, &item.Name, &item.Purpose, &item.CoordinatorAgentRef, &item.State, &item.Version, &draft, &published, &publishedVersion, &item.CreatedAt, &item.UpdatedAt, &publishedRef}
	if actorScoped {
		destinations = append(destinations, &canManage, &canLaunch)
	} else {
		canManage, canLaunch = true, true
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Workflow{}, errs.ErrNotFound
		}
		return entity.Workflow{}, errs.ErrUnavailable
	}
	item.Draft = &entity.WorkflowVersion{}
	if err := json.Unmarshal(draft, item.Draft); err != nil || workflowVersionStructureInvalidField(*item.Draft) != "" {
		return entity.Workflow{}, errs.ErrUnavailable
	}
	normalizeWorkflowDraftIdentity(item.Ref, item.Draft)
	if len(published) > 0 {
		item.Published = &entity.WorkflowVersion{}
		if err := json.Unmarshal(published, item.Published); err != nil || workflowVersionStructureInvalidField(*item.Published) != "" || publishedRef == "" {
			return entity.Workflow{}, errs.ErrUnavailable
		}
		item.Published.VersionNumber = publishedVersion
		item.Published.Ref = publishedRef
	}
	item.NextActions = workflowActions(item, canManage, canLaunch)
	return item, nil
}

// workflowActions maps manageable DRAFT/VALID/PUBLISHED states to edit/validate/publish/archive
// hints and offers LAUNCH for published launchable workflows when readiness is absent or permits
// submission. The absent-readiness compatibility behavior is retained; this function performs no
// live readiness check.
// workflowActions преобразует управляемые DRAFT/VALID/PUBLISHED в edit/validate/publish/archive и
// предлагает LAUNCH опубликованному workflow при праве запуска и отсутствии readiness либо
// разрешении подачи. Сохраняет совместимость при отсутствии readiness; реальную готовность не
// проверяет.
func workflowActions(item entity.Workflow, canManage, canLaunch bool) []string {
	actions := []string{"OPEN"}
	if canManage {
		switch item.State {
		case "DRAFT":
			actions = append(actions, "EDIT", "VALIDATE", "ARCHIVE")
		case "VALID":
			actions = append(actions, "EDIT", "PUBLISH", "ARCHIVE")
		case "PUBLISHED":
			actions = append(actions, "EDIT", "ARCHIVE")
		}
	}
	if canLaunch && item.State == "PUBLISHED" && (item.LaunchReadiness == nil || item.LaunchReadiness.AllowedToSubmit) {
		actions = append(actions, "LAUNCH")
	}
	return actions
}

// GetWorkflow resolves actor and exact workflow assistant context in a read-only RepeatableRead
// snapshot, decodes actor-scoped versions, projects launch readiness/cards and commits. Failed
// reads or projections return no workflow; failed commit maps to Unavailable.
// GetWorkflow разрешает субъекта и точный assistant context workflow в read-only RepeatableRead
// снимке, декодирует версии с областью субъекта, проецирует launch readiness/карточки и фиксирует
// транзакцию. Отказ чтения/проекции исключает workflow в ответе; отказ commit даёт Unavailable.
func (repository *Repository) GetWorkflow(ctx context.Context, principal value.Principal, ref string) (entity.Workflow, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Workflow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveAssistantContext(ctx, tx, scope, entity.AssistantContextDescriptor{EntityKind: "WORKFLOW", EntityRef: ref}, ""); err != nil {
		return entity.Workflow{}, err
	}
	item, err := scanWorkflow(tx.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, ref, scope.role, scope.actorID), true)
	if err != nil {
		return entity.Workflow{}, err
	}
	if err := repository.projectWorkflowLaunchReadiness(ctx, tx, scope, []*entity.Workflow{&item}); err != nil {
		return entity.Workflow{}, err
	}
	if err := projectWorkflowCards(ctx, tx, scope, []*entity.Workflow{&item}); err != nil {
		return entity.Workflow{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.Workflow{}, errs.ErrUnavailable
	}
	return item, nil
}

// ListRuns normalizes query/project and a copied unique closed set of states, routes
// resumable-session requests separately and rejects ordinary target filters. The authorized
// catalog reads/scans candidates, attaches assistant/continuation/artifact projections and a total
// count using timestamp/ref pagination within its transaction.
// ListRuns нормализует запрос/проект и копию уникальных состояний закрытого набора, отдельно
// направляет resumable-session запросы и отклоняет фильтры цели обычного списка. Авторизованный
// каталог читает кандидатов, добавляет assistant/continuation/artifact проекции и полный счётчик с
// пагинацией по времени/ref в своей транзакции.
func (repository *Repository) ListRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Run, int64, string, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.States = append([]string{}, filter.States...)
	sort.Strings(filter.States)
	for i, state := range filter.States {
		if !slices.Contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING", "SUCCEEDED", "FAILED", "CANCELLED"}, state) || (i > 0 && state == filter.States[i-1]) {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	if filter.ResumableSessionsOnly {
		return repository.listResumableSessions(ctx, scope, filter)
	}
	if filter.TargetType != "" || filter.TargetRef != "" {
		return nil, 0, "", errs.ErrInvalid
	}
	return authorizedCatalogWithTotal(ctx, repository, scope, "RUN", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Run, error) {
			cursorAt, cursorRef, err := parseRunCatalogPosition(cursor)
			if err != nil {
				return nil, err
			}
			rows, err := tx.Query(ctx, queryQueriesListrunsSelectRunsOrganizationIdRefProjectId, pgx.StrictNamedArgs{
				"organization_id":      scope.organizationID,
				"project_ref":          filter.ProjectRef,
				"role":                 scope.role,
				"actor_id":             scope.actorID,
				"query":                filter.Query,
				"limit":                limit,
				"cursor_at":            cursorAt,
				"cursor_ref":           cursorRef,
				"states":               filter.States,
				"authority_project_id": scope.authorityProjectID,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Run
			for rows.Next() {
				item, err := scanRun(rows, true)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Run) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(tx pgx.Tx, item *entity.Run, allowed func(string) bool) error {
			if err := attachRunAssistantPin(ctx, tx, scope, item); err != nil {
				return err
			}
			item.NextActions = runActions(item.State, allowed("run.cancel") || allowed("run.cancel.own"), false)
			if err := repository.applyContinuationAction(ctx, tx, scope, item); err != nil {
				return err
			}
			return projectArtifactResults(ctx, tx, scope, &command.Result{Run: item})
		}, func(ctx context.Context, tx pgx.Tx) (int64, error) {
			var total int64
			err := tx.QueryRow(ctx, queryCatalogRunsCount, scope.organizationID, filter.ProjectRef, scope.role, scope.actorID, filter.Query, filter.States, scope.authorityProjectID).Scan(&total)
			if err != nil {
				return 0, errs.ErrUnavailable
			}
			return total, nil
		}, runCatalogPosition)
}

// runCatalogPosition formats a run creation time in UTC RFC3339Nano followed by a vertical bar and
// reference; outer catalog encoding binds this position to scope and filters.
// runCatalogPosition форматирует время создания запуска в UTC RFC3339Nano, затем вертикальную
// черту и ссылку; внешнее кодирование каталога связывает позицию с областью и фильтрами.
func runCatalogPosition(item entity.Run) string {
	return item.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + item.Ref
}

// parseRunCatalogPosition accepts an empty first-page position or splits timestamp/ref, rejecting
// invalid timestamps and empty or separator-bearing references as Invalid. It parses position
// shape, not resource eligibility or a run-reference prefix.
// parseRunCatalogPosition принимает пустую позицию первой страницы либо разделяет время/ref,
// отклоняя неверное время и пустые либо содержащие разделитель ссылки с Invalid. Проверяет форму
// позиции, не доступ к ресурсу и не префикс ссылки запуска.
func parseRunCatalogPosition(cursor string) (*time.Time, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	stamp, ref, found := strings.Cut(cursor, "|")
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if !found || err != nil || ref == "" || strings.Contains(ref, "|") {
		return nil, "", errs.ErrInvalid
	}
	return &at, ref, nil
}

// scanRun delegates the ordinary run projection to scanRunWithPrefix without leading destinations;
// actorScoped selects whether the SQL row supplies two presentation action flags.
// scanRun делегирует обычную проекцию запуска scanRunWithPrefix без начальных destinations;
// actorScoped определяет наличие двух presentation flags действий в SQL строке.
func scanRun(row rowScanner, actorScoped bool) (entity.Run, error) {
	return scanRunWithPrefix(row, actorScoped)
}

// scanRunWithPrefix prepends caller destinations to the complete run projection, optionally reads
// actor cancel/launch flags, decodes input/usage and derives initial actions. Internal mode
// defaults flags true without authorizing a command; scan/JSON failures discard the run, though
// caller prefix destinations may already be written.
// scanRunWithPrefix добавляет destinations вызывающего перед полной проекцией запуска, при
// необходимости читает cancel/launch flags, декодирует input/usage и вычисляет исходные действия.
// Внутренний режим ставит flags true без авторизации команды; ошибки scan/JSON отбрасывают запуск,
// но начальные destinations уже могут быть заполнены.
func scanRunWithPrefix(row rowScanner, actorScoped bool, prefix ...any) (entity.Run, error) {
	var item entity.Run
	var input, usage []byte
	var canCancel, canLaunch bool
	destinations := append(prefix, &item.Ref, &item.ProjectRef, &item.SessionRef, &item.RootRunRef, &item.ParentRunRef, &item.RetryOfRunRef, &item.Title, &item.TitleSource, &item.ActivitySummary, &item.Task, &item.State, &item.Source, &item.ResultSummary, &item.SafeErrorCode, &item.SafeErrorMessage, &item.InitiatorName, &item.Target.Type, &item.Target.Ref, &item.Target.Name, &item.Target.Version, &item.Attempt, &item.GraphRevision, &item.EventSequence, &item.Version, &input, &item.InputAttachmentSetRef, &item.ArtifactRefs, &item.GateRefs, &usage, &item.CreatedAt, &item.StartedAt, &item.FinishedAt)
	if actorScoped {
		destinations = append(destinations, &canCancel, &canLaunch)
	} else {
		canCancel, canLaunch = true, true
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Run{}, errs.ErrNotFound
		}
		return entity.Run{}, errs.ErrUnavailable
	}
	if json.Unmarshal(input, &item.Input) != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	decodedUsage, err := decodeRunUsage(usage)
	if err != nil {
		return entity.Run{}, err
	}
	item.Usage = decodedUsage
	item.NextActions = runActions(item.State, canCancel, canLaunch)
	return item, nil
}

// runActions returns OPEN with CANCEL for cancellable live states, RETRY for failed/cancelled
// states using the same supplied cancel flag, or ADD_TURN for successful launchable runs. All
// other cases retain OPEN; this is an action hint, not continuation eligibility.
// runActions возвращает OPEN с CANCEL для отменяемых живых состояний, RETRY для failed/cancelled
// по тому же cancel flag либо ADD_TURN для successful при launch flag. Остальные случаи сохраняют
// OPEN; это подсказка, не проверка допустимости продолжения.
func runActions(state string, canCancel, canLaunch bool) []string {
	switch state {
	case "QUEUED", "RUNNING", "WAITING_HUMAN":
		if canCancel {
			return []string{"OPEN", "CANCEL"}
		}
	case "FAILED", "CANCELLED":
		if canCancel {
			return []string{"OPEN", "RETRY"}
		}
	case "SUCCEEDED":
		if canLaunch {
			return []string{"OPEN", "ADD_TURN"}
		}
	}
	return []string{"OPEN"}
}

// contains performs exact case-sensitive membership testing over string values without
// normalization, mutation or permission interpretation.
// contains проверяет точное регистрозависимое вхождение строки без нормализации, изменения данных
// и интерпретации полномочий.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// actorActionPermissions groups project membership-derived presentation flags for
// agent/workflow/run/gate/artifact/schedule actions; exact resource eligibility must be
// established by the query or command path.
// actorActionPermissions объединяет presentation flags проектного membership для действий агентов,
// workflow, запусков, шлюзов, артефактов и расписаний; точный доступ к ресурсу обязан установить
// путь запроса либо команды.
type actorActionPermissions struct {
	canManageAgents    bool
	canManageWorkflows bool
	canCancelRuns      bool
	canLaunchRuns      bool
	canResolveGates    bool
	canManageArtifacts bool
	canManageSchedules bool
}

// projectActionPermissions returns all presentation flags for owner/admin, none for an empty
// project or missing membership, and otherwise maps the stored permission strings. It maps other
// query errors to Unavailable and does not independently authorize an exact resource.
// projectActionPermissions возвращает все presentation flags для owner/admin, ни одного для
// пустого проекта либо отсутствующего membership, иначе сопоставляет сохранённые строки
// permissions. Остальные ошибки запроса отображает в Unavailable, точный ресурс самостоятельно не
// авторизует.
func (repository *Repository) projectActionPermissions(
	ctx context.Context,
	runner queryRunner,
	scope scope,
	projectRef string,
) (actorActionPermissions, error) {
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		return actorActionPermissions{
			canManageAgents:    true,
			canManageWorkflows: true,
			canCancelRuns:      true,
			canLaunchRuns:      true,
			canResolveGates:    true,
			canManageArtifacts: true,
			canManageSchedules: true,
		}, nil
	}
	if projectRef == "" {
		return actorActionPermissions{}, nil
	}
	var permissions []string
	if err := runner.QueryRow(
		ctx,
		queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef,
		scope.organizationID,
		projectRef,
		scope.actorID,
	).Scan(&permissions); errors.Is(err, pgx.ErrNoRows) {
		// Membership — только presentation: exact eligibility проверяется caller.
		return actorActionPermissions{}, nil
	} else if err != nil {
		return actorActionPermissions{}, errs.ErrUnavailable
	}
	return actorActionPermissions{
		canManageAgents:    contains(permissions, "MANAGE_AGENTS"),
		canManageWorkflows: contains(permissions, "MANAGE_WORKFLOWS"),
		canCancelRuns:      contains(permissions, "CANCEL_RUNS"),
		canLaunchRuns:      contains(permissions, "LAUNCH_RUNS"),
		canResolveGates:    contains(permissions, "RESOLVE_GATES"),
		canManageArtifacts: contains(permissions, "MANAGE_ARTIFACTS"),
		canManageSchedules: contains(permissions, "MANAGE_SCHEDULES"),
	}, nil
}

// applyResultActionPermissions refreshes command/receipt presentation in the caller transaction:
// current cards, queued-work access, conversation context, provider account, configuration and
// artifact eligibility. It re-reads workflow publication and assistant-run pins, projects gate
// intent and continuation actions, then filters graph/event hints. It can partially mutate the
// supplied result before failure and never commits; historical receipt outcome does not supply
// current authority.
// applyResultActionPermissions обновляет представление command/receipt в транзакции вызывающего:
// текущие карточки, доступ к queued work, контекст беседы, provider account, конфигурацию и
// artifact eligibility. Перечитывает публикацию workflow и pins assistant-run, проецирует gate
// intent/continuation, фильтрует graph/event actions. Может частично изменить результат до ошибки,
// не выполняет commit; исторический outcome receipt не даёт текущих полномочий.
func (repository *Repository) applyResultActionPermissions(
	ctx context.Context,
	runner pgx.Tx,
	scope scope,
	result *command.Result,
	projectRef string,
) error {
	if result == nil {
		return nil
	}
	if result.Project != nil {
		if err := projectProjectCards(ctx, runner, scope, []*entity.Project{result.Project}); err != nil {
			return err
		}
	}
	if result.Agent != nil {
		if err := projectAgentCards(ctx, runner, scope, []*entity.Agent{result.Agent}); err != nil {
			return err
		}
	}
	for _, item := range result.ProviderQueuedWorkResults {
		// Receipt сохраняет исторический outcome, но не обходит отзыв текущего доступа.
		if item.Outcome == "NOT_FOUND" {
			continue
		}
		target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: item.RunRef}
		if err := repository.requireAccess(ctx, runner, scope, "run.view", target); err != nil {
			return err
		}
		if item.Outcome != "PERMISSION_REQUIRED" {
			if err := repository.requireAccess(ctx, runner, scope, "run.cancel", target); err != nil {
				return err
			}
		}
	}
	if result.Conversation != nil {
		descriptor, project, err := repository.assistantConversationContext(ctx, runner, scope, result.Conversation.Ref, "")
		if err != nil {
			return err
		}
		result.Conversation.Context, result.Conversation.ProjectRef = descriptor, project
		assistant, err := repository.conversationAssistantTx(ctx, runner, scope, result.Conversation.Ref)
		if err != nil {
			return err
		}
		assistant.projectConversation(result.Conversation)
	}
	if result.ProviderAccount != nil {
		target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: result.ProviderAccount.Ref}
		if err := repository.requireAccess(ctx, runner, scope, "provider.account.view", target); err != nil {
			return err
		}
		fresh, err := repository.providerAccountByRef(ctx, runner, scope, result.ProviderAccount.Ref)
		if err != nil {
			return err
		}
		items, _, err := repository.authorizeProviderAccountActions(ctx, runner, scope, []entity.ProviderAccount{fresh})
		if err != nil {
			return err
		}
		result.ProviderAccount = &items[0]
	}
	if err := repository.projectManagedRoleImageSource(ctx, runner, scope, result); err != nil {
		return err
	}
	if err := repository.projectCFGActions(ctx, runner, scope, result.ManagedConfiguration); err != nil {
		return err
	}
	if projectRef == "" {
		switch {
		case result.Agent != nil:
			projectRef = result.Agent.ProjectRef
		case result.Workflow != nil:
			projectRef = result.Workflow.ProjectRef
		case result.Run != nil:
			projectRef = result.Run.ProjectRef
		case result.Gate != nil:
			projectRef = result.Gate.ProjectRef
		case result.Artifact != nil:
			projectRef = result.Artifact.ProjectRef
		case result.Schedule != nil:
			projectRef = result.Schedule.ProjectRef
		}
	}
	if err := projectArtifactResults(ctx, runner, scope, result); err != nil {
		return err
	}
	if projectRef == "" {
		for _, gate := range []*entity.OwnerGate{result.Gate} {
			if gate != nil {
				if err := repository.projectGateIntent(ctx, runner, scope, gate, true); err != nil {
					return err
				}
				gate.NextActions = gateActions(gate.State, true)
			}
		}
		if result.Event != nil && result.Event.Delta.Gate != nil {
			if err := repository.projectGateIntent(ctx, runner, scope, result.Event.Delta.Gate, true); err != nil {
				return err
			}
			result.Event.Delta.Gate.NextActions = gateActions(result.Event.Delta.Gate.State, true)
		}
		return nil
	}
	permissions, err := repository.projectActionPermissions(ctx, runner, scope, projectRef)
	if err != nil {
		return err
	}
	if result.Agent != nil {
		result.Agent.NextActions = agentActions(*result.Agent, permissions.canManageAgents, permissions.canLaunchRuns)
	}
	if result.Workflow != nil {
		// Receipt хранит исход перехода, а исполняемый readback использует текущую
		// защищённую версию: старые publication pins не смешиваются с readiness.
		currentWorkflow, err := scanWorkflow(runner.QueryRow(ctx, queryQueriesGetworkflowSelectWorkflowsOrganizationIdRefProjectId, scope.organizationID, result.Workflow.Ref, scope.role, scope.actorID), true)
		if err != nil {
			return err
		}
		result.Workflow = &currentWorkflow
		result.Workflow.NextActions = workflowActions(*result.Workflow, permissions.canManageWorkflows, permissions.canLaunchRuns)
		if err := repository.projectWorkflowLaunchReadiness(ctx, runner, scope, []*entity.Workflow{result.Workflow}); err != nil {
			return err
		}
		if err := projectWorkflowCards(ctx, runner, scope, []*entity.Workflow{result.Workflow}); err != nil {
			return err
		}
	}
	if result.Run != nil {
		if result.Run.Target.Type == "SYSTEM_ASSISTANT" {
			// Receipt помощника не выдаёт прежнюю owner identity: pin и target
			// version читаются по защищённому актуальному пути. Обычный launch
			// уже авторизован командой и не требует отдельного права run.view.
			currentRun, err := repository.readRunWithIncidents(ctx, runner, scope, result.Run.Ref)
			if err != nil {
				return err
			}
			result.Run.AssistantPin = currentRun.AssistantPin
			result.Run.Target = currentRun.Target
		}
		result.Run.NextActions = runActions(result.Run.State, permissions.canCancelRuns, permissions.canLaunchRuns)
		if err := repository.applyContinuationAction(ctx, runner, scope, result.Run); err != nil {
			return err
		}
	}
	if result.Graph != nil {
		for index := range result.Graph.Nodes {
			result.Graph.Nodes[index].NextActions = filterNodeActions(result.Graph.Nodes[index].NextActions, permissions)
		}
	}
	if result.Gate != nil {
		if err := repository.projectGateIntent(ctx, runner, scope, result.Gate, true); err != nil {
			return err
		}
		result.Gate.NextActions = gateActions(result.Gate.State, permissions.canResolveGates)
	}
	if result.Schedule != nil {
		result.Schedule.NextActions = scheduleActions(*result.Schedule, permissions.canManageSchedules)
	}
	if result.Event != nil {
		if err := repository.projectGateIntent(ctx, runner, scope, result.Event.Delta.Gate, true); err != nil {
			return err
		}
		applyEventActionPermissions(result.Event, permissions)
		if err := repository.applyContinuationEventAction(ctx, runner, scope, result.Event); err != nil {
			return err
		}
	}
	return nil
}

// filterNodeActions copies only OPEN and permission-allowed CANCEL/RETRY/RESOLVE_GATE hints in
// original order, dropping all unrecognized actions without mutating the source slice.
// filterNodeActions копирует только OPEN и разрешённые CANCEL/RETRY/RESOLVE_GATE в исходном
// порядке, отбрасывая неизвестные действия без изменения исходного среза.
func filterNodeActions(actions []string, permissions actorActionPermissions) []string {
	result := make([]string, 0, len(actions))
	for _, action := range actions {
		switch action {
		case "OPEN":
			result = append(result, action)
		case "CANCEL", "RETRY":
			if permissions.canCancelRuns {
				result = append(result, action)
			}
		case "RESOLVE_GATE":
			if permissions.canResolveGates {
				result = append(result, action)
			}
		}
	}
	return result
}

// applyEventActionPermissions recomputes action hints on nonnil run/node/gate deltas using
// supplied project presentation flags; the event itself must be nonnil and callers separately
// project current gate/continuation eligibility.
// applyEventActionPermissions пересчитывает подсказки непустых run/node/gate delta по переданным
// проектным presentation flags; само событие должно быть непустым, вызывающий отдельно проецирует
// текущую gate/continuation eligibility.
func applyEventActionPermissions(event *entity.RunEvent, permissions actorActionPermissions) {
	if event.Delta.Run != nil {
		event.Delta.Run.NextActions = runActions(
			event.Delta.Run.State,
			permissions.canCancelRuns,
			permissions.canLaunchRuns,
		)
	}
	if event.Delta.Node != nil {
		event.Delta.Node.NextActions = filterNodeActions(event.Delta.Node.NextActions, permissions)
	}
	if event.Delta.Gate != nil {
		event.Delta.Gate.NextActions = gateActions(event.Delta.Gate.State, permissions.canResolveGates)
	}
}

// GetRun resolves exact assistant-context eligibility for the requested run in a read-only
// RepeatableRead transaction, reads incidents/continuation/artifact projections, attaches session
// readiness and commits. Any failure discards the run; commit failure maps to Unavailable.
// GetRun разрешает точную assistant-context eligibility запуска в read-only RepeatableRead
// транзакции, читает incidents/continuation/artifact проекции, добавляет session readiness и
// фиксирует транзакцию. Любая ошибка отбрасывает запуск; отказ commit даёт Unavailable.
func (repository *Repository) GetRun(ctx context.Context, principal value.Principal, ref string) (entity.Run, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Run{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveAssistantContext(ctx, tx, scope, entity.AssistantContextDescriptor{EntityKind: "RUN", EntityRef: ref}, ""); err != nil {
		return entity.Run{}, err
	}
	item, err := repository.readRunWithIncidents(ctx, tx, scope, ref)
	if err != nil {
		return entity.Run{}, err
	}
	if err := attachRunSessionReadiness(ctx, tx, scope, &item); err != nil {
		return entity.Run{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	return item, nil
}

// readRunWithIncidents reads an actor/authority-project-scoped run through the supplied runner,
// attaches assistant pins and eligible artifacts, projects interaction delivery incidents and
// current continuation actions. It does not open or commit a transaction; callers own the
// authoritative access path and snapshot.
// readRunWithIncidents читает запуск с областью субъекта/authority-project через переданный
// runner, добавляет assistant pins и доступные артефакты, проецирует interaction delivery
// incidents и текущие continuation actions. Не открывает и не фиксирует транзакцию; авторитетным
// доступом и снимком владеет вызывающий.
func (repository *Repository) readRunWithIncidents(ctx context.Context, runner queryRunner, scope scope, ref string) (entity.Run, error) {
	item, err := scanRun(runner.QueryRow(ctx, queryQueriesGetrunSelectRunsOrganizationIdRefProjectId, scope.organizationID, ref, scope.actorID, scope.authorityProjectID), true)
	if err != nil {
		return entity.Run{}, err
	}
	if err := attachRunAssistantPin(ctx, runner, scope, &item); err != nil {
		return entity.Run{}, err
	}
	if err := projectArtifactResults(ctx, runner, scope, &command.Result{Run: &item}); err != nil {
		return entity.Run{}, err
	}
	rows, err := runner.Query(ctx, queryInteractionListRunIncidents, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"run_ref":         ref,
		"platform_role":   scope.role,
		"actor_id":        scope.actorID,
	})
	if err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var incident entity.Incident
		var deliveryState string
		var attempt, maximumAttempts int
		if err := rows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &maximumAttempts, &incident.CreatedAt); err != nil {
			return entity.Run{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt, maximumAttempts)
		item.Incidents = append(item.Incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return entity.Run{}, errs.ErrUnavailable
	}
	if err := repository.applyContinuationAction(ctx, runner, scope, &item); err != nil {
		return entity.Run{}, err
	}
	return item, nil
}

// GetRunGraph reads the selected run and its root graph through getRunGraph without embedding the
// root run family; access and snapshot/error handling remain in that shared implementation.
// GetRunGraph читает выбранный запуск и его корневой граф через getRunGraph без вложенного
// семейства запусков; доступом, снимком и ошибками управляет общий метод.
func (repository *Repository) GetRunGraph(ctx context.Context, principal value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	return repository.getRunGraph(ctx, principal, ref, false)
}

// GetRunGraphSnapshot requests the root graph with embedded run-family snapshots; getRunGraph
// rejects a nonroot selected run in this mode instead of attaching a mismatched family.
// GetRunGraphSnapshot запрашивает корневой граф со снимками семейства запусков; getRunGraph в этом
// режиме отклоняет выбранный некорневой запуск вместо добавления несовпадающего семейства.
func (repository *Repository) GetRunGraphSnapshot(ctx context.Context, principal value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	return repository.getRunGraph(ctx, principal, ref, true)
}

// getRunGraph reads a run, root cursor, nodes and edges in one read-only RepeatableRead
// transaction, filters node hints, masks callback continuation summaries and projects
// retry/artifact views. Snapshot mode requires the selected root and attaches its run family; all
// rows are closed before dependent reads and commit. Failures return neither partial run nor
// graph.
// getRunGraph читает запуск, корневой курсор, узлы и рёбра в одной read-only RepeatableRead
// транзакции, фильтрует подсказки узлов, скрывает callback continuation summaries и проецирует
// retry/артефакты. Режим snapshot требует выбранный root и добавляет его семейство; строки
// закрываются до зависимых чтений и commit. Ошибки исключают частичный запуск и граф.
func (repository *Repository) getRunGraph(ctx context.Context, principal value.Principal, ref string, includeRuns bool) (entity.Run, entity.RunGraph, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := repository.readRunWithIncidents(ctx, tx, scope, ref)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	if err := attachRunSessionReadiness(ctx, tx, scope, &run); err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	permissions, err := repository.projectActionPermissions(ctx, tx, scope, run.ProjectRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	graph, err := readRootRunGraphCursor(ctx, tx, scope, run)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	rows, err := tx.Query(ctx, queryQueriesGetrungraphSelectArtifactsNodeIdRef, scope.organizationID, run.RootRunRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	for rows.Next() {
		n, scanErr := scanRunNode(rows)
		if scanErr != nil {
			rows.Close()
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		n.NextActions = filterNodeActions(n.NextActions, permissions)
		graph.Nodes = append(graph.Nodes, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	rows.Close()
	edgeRows, err := tx.Query(ctx, queryQueriesGetrungraphSelectRunEdgesOrganizationIdRef, scope.organizationID, run.RootRunRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	for edgeRows.Next() {
		var e entity.RunEdge
		if err := edgeRows.Scan(&e.Ref, &e.RunRef, &e.SourceNodeRef, &e.TargetNodeRef, &e.Type, &e.Label); err != nil {
			edgeRows.Close()
			return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
		}
		graph.Edges = append(graph.Edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		edgeRows.Close()
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	edgeRows.Close()
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.Type != "AGENT_EXECUTION" || node.TurnRef == "" || (node.ParentNodeRef == "" && node.Attempt <= 1) {
			continue
		}
		source, err := readRuntimeMessageSource(ctx, tx, scope.organizationID, node.RunRef, node.TurnRef, node.Ref)
		if err != nil {
			return entity.Run{}, entity.RunGraph{}, err
		}
		if source.Origin == "CALLBACK_CONTINUATION" {
			node.InputSummary = callbackContinuationPublicText
		}
	}
	if run.RetryOfRunRef != "" {
		previous, readErr := repository.readRunWithIncidents(ctx, tx, scope, run.RetryOfRunRef)
		if readErr != nil {
			return entity.Run{}, entity.RunGraph{}, readErr
		}
		if err := projectChildRetryGraph(&graph, run, previous); err != nil {
			return entity.Run{}, entity.RunGraph{}, err
		}
	}
	if err := projectArtifactResults(ctx, tx, scope, &command.Result{Graph: &graph}); err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	if includeRuns {
		if run.Ref != graph.RunRef {
			return entity.Run{}, entity.RunGraph{}, errs.ErrInvalid
		}
		if err := repository.attachGraphRuns(ctx, tx, scope, &graph); err != nil {
			return entity.Run{}, entity.RunGraph{}, err
		}
		for _, item := range graph.Runs {
			if item.Ref == graph.RunRef {
				run = item
				break
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.Run{}, entity.RunGraph{}, errs.ErrUnavailable
	}
	return run, graph, nil
}

// scanRunNode scans the complete node projection including stored action hints, materialization
// state and artifact/child-run references. NoRows maps to NotFound and any other scan error to
// Unavailable; caller policy must filter the hints.
// scanRunNode сканирует полную проекцию узла, включая сохранённые подсказки, materialization state
// и ссылки артефактов/дочерних запусков. NoRows даёт NotFound, прочие ошибки scan дают
// Unavailable; вызывающий обязан отфильтровать подсказки политикой.
func scanRunNode(row rowScanner) (entity.RunNode, error) {
	var node entity.RunNode
	if err := row.Scan(&node.Ref, &node.RunRef, &node.ParentNodeRef, &node.Type, &node.State, &node.DisplayName, &node.Role, &node.AgentRef, &node.TurnRef, &node.Attempt, &node.InputSummary, &node.ProgressSummary, &node.IntegrationNames, &node.CallbackSummary, &node.SafeErrorCode, &node.SafeErrorMessage, &node.NextActions, &node.MaterializationState, &node.CreatedAt, &node.StartedAt, &node.FinishedAt, &node.ArtifactRefs, &node.ChildRunRefs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.RunNode{}, errs.ErrNotFound
		}
		return entity.RunNode{}, errs.ErrUnavailable
	}
	return node, nil
}

// ListRunEvents reads a bounded 200-default/500-maximum root-event page and root high-watermark in
// one read-only snapshot after reading the selected run. It validates run deltas, integration
// bindings and tool JSON, projects current artifacts/gates/actions and masks callback provenance
// text. Completion compares the page to the graph sequence; any failure discards the page and
// commit maps to Unavailable.
// ListRunEvents читает страницу корневых событий с лимитом200 по умолчанию и максимумом500 вместе
// с root high-watermark в одном read-only снимке после чтения выбранного запуска. Проверяет run
// delta, integration bindings и tool JSON, проецирует текущие артефакты/шлюзы/действия, скрывает
// callback provenance text. Полноту определяет относительно graph sequence; ошибки отбрасывают
// страницу, отказ commit даёт Unavailable.
func (repository *Repository) ListRunEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return nil, 0, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := repository.readRunWithIncidents(ctx, tx, scope, filter.ResourceRef)
	if err != nil {
		return nil, 0, false, err
	}
	graph, err := readRootRunGraphCursor(ctx, tx, scope, run)
	if err != nil {
		return nil, 0, false, err
	}
	permissions, err := repository.projectActionPermissions(ctx, tx, scope, run.ProjectRef)
	if err != nil {
		return nil, 0, false, err
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := tx.Query(ctx, queryQueriesListruneventsSelectRunEventsOrganizationIdRef, scope.organizationID, run.RootRunRef, filter.AfterSequence, limit)
	if err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.RunEvent
	for rows.Next() {
		var e entity.RunEvent
		var delta, toolCall []byte
		if err := rows.Scan(&e.Ref, &e.RunRef, &e.Sequence, &e.Type, &e.NodeRef, &e.EdgeRef, &e.GateRef, &e.ArtifactRef, &e.Summary, &e.Progress, &e.RunState, &e.NodeState, &delta, &e.Actor.Kind, &e.Actor.Ref, &e.Actor.Name, &e.MessageKind, &toolCall, &e.OccurredAt); err != nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if err := json.Unmarshal(delta, &e.Delta); err != nil || e.Delta.Run == nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		if e.Delta.IntegrationInvocationRef != "" && !validIntegrationActionBinding(e.Type, e.Summary, e.Delta.IntegrationInvocationRef) {
			return nil, 0, false, errs.ErrUnavailable
		}
		if e.Delta.Incident != nil {
			e.IncidentRef = e.Delta.Incident.Ref
		}
		if len(toolCall) != 0 && json.Unmarshal(toolCall, &e.ToolCall) != nil {
			return nil, 0, false, errs.ErrUnavailable
		}
		applyEventActionPermissions(&e, permissions)
		e.GraphRevision = e.Delta.Run.GraphRevision
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	rows.Close()
	complete := len(result) < int(limit) || len(result) > 0 && result[len(result)-1].Sequence == graph.Sequence
	projections := make([]*command.Result, len(result))
	for index := range result {
		projections[index] = &command.Result{Event: &result[index]}
	}
	if err := projectArtifactResults(ctx, tx, scope, projections...); err != nil {
		return nil, 0, false, err
	}
	for index := range result {
		if node := result[index].Delta.Node; node != nil && node.Type == "AGENT_EXECUTION" && node.TurnRef != "" && (node.ParentNodeRef != "" || node.Attempt > 1) {
			source, err := readRuntimeMessageSource(ctx, tx, scope.organizationID, node.RunRef, node.TurnRef, node.Ref)
			if err != nil {
				return nil, 0, false, err
			}
			if source.Origin == "CALLBACK_CONTINUATION" {
				node.InputSummary = callbackContinuationPublicText
			}
		}
		if message := result[index].Delta.Message; message != nil {
			message.Source = entity.MessageSource{Origin: "ORDINARY"}
			if message.Phase == "USER" {
				execution := result[index].Delta.Execution
				if execution == nil || message.Ref != execution.TurnRef {
					return nil, 0, false, errs.ErrUnavailable
				}
				message.Source, err = readRuntimeMessageSource(ctx, tx, scope.organizationID, execution.RunRef, execution.TurnRef, execution.NodeRef)
				if err != nil {
					return nil, 0, false, err
				}
				if message.Source.Origin == "CALLBACK_CONTINUATION" {
					message.Text = callbackContinuationPublicText
				}
			}
		}
		if err := repository.projectGateIntent(ctx, tx, scope, result[index].Delta.Gate, true); err != nil {
			return nil, 0, false, err
		}
		result[index].Delta.Run.NextActions = runActions(result[index].Delta.Run.State, permissions.canCancelRuns, slices.Contains(run.NextActions, "ADD_TURN"))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, false, errs.ErrUnavailable
	}
	return result, graph.Sequence, complete, nil
}

// scanGate reads the fixed gate projection and optional actor resolve flag, deriving actions from
// state and that flag. Internal mode defaults resolve true without granting authority; NoRows is
// NotFound and any other scan failure is Unavailable.
// scanGate читает фиксированную проекцию шлюза и необязательный resolve flag субъекта, вычисляет
// действия по состоянию/flag. Внутренний режим ставит resolve true без предоставления полномочий;
// NoRows даёт NotFound, остальные ошибки scan дают Unavailable.
func scanGate(row rowScanner, actorScoped bool) (entity.OwnerGate, error) {
	var item entity.OwnerGate
	canResolve := true
	destinations := []any{&item.Ref, &item.ProjectRef, &item.RunRef, &item.NodeRef, &item.Title, &item.Prompt, &item.ContextSummary, &item.RequestedByRef, &item.RequestedByName, &item.AllowedDecisions, &item.State, &item.Decision, &item.DecisionComment, &item.ResolvedByName, &item.Version, &item.CreatedAt, &item.ResolvedAt, &item.ResolutionAttachmentSetRef, &item.ScopeKind, &item.OrganizationRef}
	if actorScoped {
		canResolve = false
		destinations = append(destinations, &canResolve)
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.OwnerGate{}, errs.ErrNotFound
		}
		return entity.OwnerGate{}, errs.ErrUnavailable
	}
	item.NextActions = gateActions(item.State, canResolve)
	return item, nil
}

// gateActions offers RESOLVE_GATE only for an OPEN gate with the supplied resolve flag; all other
// cases return an empty nonnil action slice.
// gateActions предлагает RESOLVE_GATE только открытому шлюзу при переданном resolve flag; во всех
// других случаях возвращает пустой ненулевой срез действий.
func gateActions(state string, canResolve bool) []string {
	if state == "OPEN" && canResolve {
		return []string{"RESOLVE_GATE"}
	}
	return []string{}
}

// GetOwnerGate reads an actor-scoped gate in a read-only RepeatableRead transaction, additionally
// checks organization role-image access for organization gates and projects current intent before
// commit. It does not resolve or mutate the gate.
// GetOwnerGate читает шлюз с областью субъекта в read-only RepeatableRead транзакции,
// дополнительно проверяет organization role-image access для шлюза организации и проецирует
// текущий intent до commit. Не принимает решение и не изменяет шлюз.
func (repository *Repository) GetOwnerGate(ctx context.Context, principal value.Principal, ref string) (entity.OwnerGate, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, scope.organizationID, ref, scope.role, scope.actorID), true)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	if gate.ScopeKind == "ORGANIZATION" {
		if err := repository.requireOrganizationRoleImageAccess(ctx, tx, scope); err != nil {
			return entity.OwnerGate{}, err
		}
	}
	if err := repository.projectGateIntent(ctx, tx, scope, &gate, true); err != nil {
		return entity.OwnerGate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.OwnerGate{}, errs.ErrUnavailable
	}
	return gate, nil
}

// ListArtifacts normalizes and validates closed lifecycle/type/scan/source filters, including a
// copied sorted unique source-kind set. The authorized catalog owns snapshot/cursor/total
// handling; the loader scans eligible candidates and per-item projection applies current action
// permissions and visible bindings. Invalid filters or failed reads yield no page.
// ListArtifacts нормализует и проверяет закрытые lifecycle/type/scan/source фильтры, включая
// скопированный отсортированный уникальный набор source kinds. Авторизованный каталог владеет
// снимком, курсором и total; загрузчик читает доступных кандидатов, поэлементная проекция
// применяет текущие действия и видимые bindings. Неверные фильтры и ошибки чтения исключают
// страницу.
func (repository *Repository) ListArtifacts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Artifact, int64, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	lifecycleState := strings.TrimSpace(filter.State)
	if lifecycleState == "" {
		lifecycleState = "ACTIVE"
	}
	if !contains([]string{"ACTIVE", "DELETED", "PURGE_PENDING", "PURGED"}, lifecycleState) {
		return nil, 0, "", errs.ErrInvalid
	}
	artifactType := strings.TrimSpace(filter.ArtifactType)
	if artifactType != "" && !contains([]string{"TEXT", "DOCUMENT", "IMAGE"}, artifactType) {
		return nil, 0, "", errs.ErrInvalid
	}
	scanState := strings.TrimSpace(filter.ScanState)
	if scanState != "" && !contains([]string{"PENDING", "SCANNING", "CLEAN", "QUARANTINED", "FAILED"}, scanState) {
		return nil, 0, "", errs.ErrInvalid
	}
	sourceKind := strings.TrimSpace(filter.SourceKind)
	if sourceKind != "" && !contains([]string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, sourceKind) {
		return nil, 0, "", errs.ErrInvalid
	}
	if len(filter.SourceKinds) > 5 || (sourceKind != "" && len(filter.SourceKinds) > 0) {
		return nil, 0, "", errs.ErrInvalid
	}
	sourceKinds := append([]string{}, filter.SourceKinds...)
	for i, source := range sourceKinds {
		if !contains([]string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, source) || contains(sourceKinds[:i], source) {
			return nil, 0, "", errs.ErrInvalid
		}
	}
	sort.Strings(sourceKinds)
	filter.SourceKinds = sourceKinds
	filter.ProjectRef = strings.TrimSpace(filter.ProjectRef)
	filter.ResourceRef = strings.TrimSpace(filter.ResourceRef)
	filter.Query = strings.TrimSpace(filter.Query)
	filter.State, filter.ArtifactType, filter.ScanState, filter.SourceKind = lifecycleState, artifactType, scanState, sourceKind
	return authorizedCatalogWithTotal(ctx, repository, scope, "ARTIFACT", filter,
		func(ctx context.Context, tx pgx.Tx, cursorPosition string, limit int32) ([]entity.Artifact, error) {
			cursorAt, cursorRef, err := decodeArtifactCursor(cursorPosition)
			if err != nil {
				return nil, err
			}
			rows, err := tx.Query(ctx, queryQueriesListartifactsSelectArtifactBindingsArtifactIdIdOrganizationId, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID,
				"organization_id":   scope.organizationID,
				"project_ref":       strings.TrimSpace(filter.ProjectRef),
				"run_ref":           strings.TrimSpace(filter.ResourceRef),
				"role":              scope.role,
				"actor_id":          scope.actorID,
				"query":             strings.TrimSpace(filter.Query),
				"lifecycle_state":   lifecycleState,
				"artifact_type":     artifactType,
				"scan_state":        scanState,
				"source_kind":       sourceKind,
				"source_kinds":      sourceKinds,
				"cursor_created_at": cursorAt,
				"cursor_ref":        cursorRef,
				"limit":             limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var result []entity.Artifact
			for rows.Next() {
				item, scanErr := scanArtifact(rows)
				if scanErr != nil {
					return nil, scanErr
				}
				result = append(result, item)
			}
			if rows.Err() != nil {
				return nil, errs.ErrUnavailable
			}
			return result, nil
		}, func(item entity.Artifact) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(tx pgx.Tx, item *entity.Artifact, allowed func(string) bool) error {
			item.NextActions = permittedArtifactActions(item.ScanState, item.LifecycleState, allowed)
			return projectArtifactBindingRefs(ctx, tx, scope, item)
		}, func(ctx context.Context, tx pgx.Tx) (int64, error) {
			var total int64
			err := tx.QueryRow(ctx, queryCatalogArtifactsCount, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID, "organization_id": scope.organizationID,
				"project_ref": filter.ProjectRef, "run_ref": filter.ResourceRef, "actor_id": scope.actorID,
				"role":  scope.role,
				"query": filter.Query, "lifecycle_state": lifecycleState, "artifact_type": artifactType,
				"scan_state": scanState, "source_kind": sourceKind, "source_kinds": sourceKinds,
			}).Scan(&total)
			if err != nil {
				return 0, errs.ErrUnavailable
			}
			return total, nil
		}, func(item entity.Artifact) string {
			return encodeArtifactCursor(item.CreatedAt, item.Ref)
		})
}

// artifactCursorVersion pins the positional artifact cursor wire prefix to v1; outer catalog
// wrapping owns scope/filter binding and resource authorization.
// artifactCursorVersion фиксирует wire prefix позиционного курсора артефактов как v1; внешняя
// обёртка каталога владеет связью с областью/фильтром и авторизацией ресурсов.
const artifactCursorVersion = "v1"

// encodeArtifactCursor encodes UTC creation time and artifact reference separated by newline under
// a v1 raw-URL-base64 prefix; it does not authenticate the reference.
// encodeArtifactCursor кодирует время создания UTC и ссылку артефакта, разделённые переводом
// строки, с префиксом v1/raw-URL-base64; не удостоверяет подлинность ссылки.
func encodeArtifactCursor(createdAt time.Time, ref string) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "\n" + ref
	return artifactCursorVersion + "." + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeArtifactCursor accepts an empty first-page position; otherwise requires v1, at most256
// encoded payload characters, a valid timestamp and an art_ reference up to96 bytes without
// newlines. It normalizes time to UTC and returns Invalid for malformed positions.
// decodeArtifactCursor принимает пустую позицию первой страницы; иначе требует v1, не более256
// символов кодированного payload, корректное время и ссылку art_ до96 байт без переводов строки.
// Нормализует время в UTC, некорректная позиция даёт Invalid.
func decodeArtifactCursor(token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	version, payload, found := strings.Cut(token, ".")
	if !found || version != artifactCursorVersion || len(payload) > 256 {
		return nil, "", errs.ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	createdAtText, ref, found := strings.Cut(string(decoded), "\n")
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtText)
	if !found || err != nil || !strings.HasPrefix(ref, "art_") || len(ref) > 96 || strings.ContainsAny(ref, "\r\n") {
		return nil, "", errs.ErrInvalid
	}
	createdAt = createdAt.UTC()
	return &createdAt, ref, nil
}

// scanArtifact scans artifact revision/lifecycle/binding metadata and one presentation manage
// flag, then derives initial actions. It maps NoRows to NotFound and other scan errors to
// Unavailable; current binding eligibility remains a separate caller projection.
// scanArtifact читает метаданные revision/lifecycle/binding артефакта и один presentation manage
// flag, затем вычисляет исходные действия. NoRows отображает в NotFound, прочие ошибки scan в
// Unavailable; текущая доступность bindings остаётся отдельной проекцией вызывающего.
func scanArtifact(row rowScanner) (entity.Artifact, error) {
	var item entity.Artifact
	var canManage bool
	if err := row.Scan(&item.Ref, &item.ProjectRef, &item.RunRef, &item.SessionRef, &item.NodeRef, &item.FileName, &item.MediaType, &item.Digest, &item.ScanState, &item.PreviewState, &item.Source, &item.CurrentRevisionRef, &item.SizeBytes, &item.Revision, &item.Version, &item.LifecycleState, &item.CreatedAt, &item.DeletedAt, &item.PurgeAfter, &item.Bindings, &canManage); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Artifact{}, errs.ErrNotFound
		}
		return entity.Artifact{}, errs.ErrUnavailable
	}
	item.NextActions = artifactActions(item.ScanState, item.LifecycleState, canManage)
	return item, nil
}

// artifactActions treats empty lifecycle as ACTIVE, offers restore/purge only for managed deleted
// items and no actions after purge begins. Nonclean items expose only managed DELETE; clean items
// expose DOWNLOAD plus managed BIND/DELETE. It consumes supplied state/flags without checking
// current access.
// artifactActions считает пустой lifecycle равным ACTIVE, предлагает restore/purge лишь
// управляемым удалённым объектам и не предлагает действий после начала purge. Нечистый объект
// предлагает лишь DELETE при управлении; чистый — DOWNLOAD и возможные BIND/DELETE. Использует
// переданные состояния/flags без проверки текущего доступа.
func artifactActions(scanState, lifecycleState string, canManage bool) []string {
	if lifecycleState == "" {
		lifecycleState = "ACTIVE"
	}
	switch lifecycleState {
	case "DELETED":
		if canManage {
			return []string{"RESTORE", "PURGE"}
		}
		return []string{}
	case "PURGE_PENDING", "PURGED":
		return []string{}
	}
	if scanState != "CLEAN" {
		if canManage {
			return []string{"DELETE"}
		}
		return []string{}
	}
	actions := []string{"DOWNLOAD"}
	if canManage {
		actions = append(actions, "BIND", "DELETE")
	}
	return actions
}

// GetArtifact bounds the request to five seconds, resolves exact file assistant context in a
// read-only RepeatableRead snapshot, scans the artifact and projects current eligibility before
// commit. Any read/projection/commit failure discards the artifact.
// GetArtifact ограничивает запрос пятью секундами, разрешает точный assistant context файла в
// read-only RepeatableRead снимке, читает артефакт и проецирует текущую eligibility до commit.
// Любой отказ чтения, проекции или commit отбрасывает артефакт.
func (repository *Repository) GetArtifact(ctx context.Context, principal value.Principal, ref string) (entity.Artifact, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.Artifact{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveAssistantContext(ctx, tx, scope, entity.AssistantContextDescriptor{EntityKind: "FILE", EntityRef: ref}, ""); err != nil {
		return entity.Artifact{}, err
	}
	item, err := scanArtifact(tx.QueryRow(ctx, queryQueriesGetartifactSelectArtifactBindingsArtifactIdIdOrganizationId, scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return entity.Artifact{}, err
	}
	if err := projectArtifactEligibility(ctx, tx, scope, &item); err != nil {
		return entity.Artifact{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	return item, nil
}

// ListSchedules loads actor/authority-project-scoped schedule candidates through the authorized
// catalog and strict schedule scanner. The catalog applies exact per-item view/manage evaluation
// and cursor handling; the callback only derives management action hints.
// ListSchedules загружает кандидатов расписаний с областью субъекта/authority-project через
// авторизованный каталог и строгий scanner. Каталог применяет точную поэлементную view/manage
// проверку и курсор; callback лишь вычисляет подсказки управления.
func (repository *Repository) ListSchedules(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	return authorizedCatalog(ctx, repository, scope, "SCHEDULE", filter,
		func(ctx context.Context, tx pgx.Tx, cursor string, limit int32) ([]entity.Schedule, error) {
			rows, err := tx.Query(ctx, queryQueriesListschedulesSelectSchedulesOrganizationIdRefProjectId, pgx.StrictNamedArgs{
				"authority_project": scope.authorityProjectID,
				"organization_id":   scope.organizationID, "project_ref": filter.ProjectRef, "role": scope.role, "actor_id": scope.actorID,
				"search_query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit,
			})
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			var items []entity.Schedule
			for rows.Next() {
				item, err := scanSchedule(rows)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		}, func(item entity.Schedule) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: item.Ref, ProjectRef: item.ProjectRef}
		}, func(_ pgx.Tx, item *entity.Schedule, allowed func(string) bool) error {
			item.NextActions = scheduleActions(*item, allowed("schedule.manage"))
			return nil
		})
}

// GetSchedule resolves actor scope and directly scans the actor-scoped schedule query through the
// pool. It preserves this single-query read path rather than opening the catalog snapshot or
// adding a separate context resolver.
// GetSchedule разрешает область субъекта и непосредственно читает расписание через pool запросом с
// областью субъекта. Сохраняет путь одиночного запроса, не открывая снимок каталога и не добавляя
// отдельный context resolver.
func (repository *Repository) GetSchedule(ctx context.Context, principal value.Principal, ref string) (entity.Schedule, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.Schedule{}, err
	}
	return scanSchedule(repository.pool.QueryRow(ctx, queryQueriesGetscheduleSelectSchedulesOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
		"schedule_ref":    ref,
		"role":            scope.role,
		"actor_id":        scope.actorID,
	}))
}

// scanSchedule decodes mutable schedule fields and the complete immutable current revision,
// validates display/input/prompt JSON and projects revision policies into the public item.
// Continue-session expectation and nullable reference must agree; any mismatch/corruption fails
// Unavailable, while NoRows is NotFound.
// scanSchedule декодирует изменяемые поля расписания и полную immutable current revision,
// проверяет display/input/prompt JSON, переносит политики ревизии в публичный объект. Ожидание
// продолжения сессии и nullable reference обязаны совпасть; несовпадение/повреждение даёт
// Unavailable, NoRows — NotFound.
func scanSchedule(row rowScanner) (entity.Schedule, error) {
	var item entity.Schedule
	var input, currentRevisionInput, currentRevisionPromptInputs []byte
	var continueSessionExpected bool
	var continueSessionRef *string
	var canManage bool
	if err := row.Scan(
		&item.Ref, &item.ProjectRef, &item.Name, &item.Target.Type, &item.Target.Ref, &item.Target.Name,
		&item.Preset, &item.CronExpression, &item.Timezone, &input, &item.SessionPolicy,
		&item.NotificationPolicy, &item.State, &item.Enabled, &item.Version, &item.NextRunAt,
		&item.LastRunAt, &item.CreatedAt, &item.UpdatedAt,
		&item.CurrentRevision.Ref, &item.CurrentRevision.Revision, &item.CurrentRevision.Digest,
		&item.CurrentRevision.Name, &item.CurrentRevision.Target.Type, &item.CurrentRevision.Target.Ref,
		&item.CurrentRevision.Preset, &item.CurrentRevision.CronExpression, &item.CurrentRevision.Timezone,
		&currentRevisionInput, &item.CurrentRevision.SessionPolicy,
		&item.CurrentRevision.NotificationPolicy, &item.CurrentRevision.DSTGapPolicy,
		&item.CurrentRevision.DSTFoldPolicy, &item.CurrentRevision.MisfirePolicy,
		&item.CurrentRevision.OverlapPolicy, &item.CurrentRevision.TargetVersion,
		&item.CurrentRevision.TargetDigest, &item.CurrentRevision.AutomationText,
		&currentRevisionPromptInputs, &item.CurrentRevision.CreatedAt,
		&continueSessionExpected, &continueSessionRef, &item.LastOutcome, &canManage,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Schedule{}, errs.ErrNotFound
		}
		return entity.Schedule{}, errs.ErrUnavailable
	}
	if err := attachScheduleDisplay(&item); err != nil ||
		json.Unmarshal(input, &item.Input) != nil ||
		json.Unmarshal(currentRevisionInput, &item.CurrentRevision.Input) != nil ||
		json.Unmarshal(currentRevisionPromptInputs, &item.CurrentRevision.PromptInputs) != nil {
		return entity.Schedule{}, errs.ErrUnavailable
	}
	item.DSTGapPolicy, item.DSTFoldPolicy = item.CurrentRevision.DSTGapPolicy, item.CurrentRevision.DSTFoldPolicy
	item.MisfirePolicy, item.OverlapPolicy = item.CurrentRevision.MisfirePolicy, item.CurrentRevision.OverlapPolicy
	item.TargetVersion, item.TargetDigest = item.CurrentRevision.TargetVersion, item.CurrentRevision.TargetDigest
	item.AutomationText, item.PromptInputs = item.CurrentRevision.AutomationText, item.CurrentRevision.PromptInputs
	if continueSessionExpected {
		if continueSessionRef == nil || strings.TrimSpace(*continueSessionRef) == "" {
			return entity.Schedule{}, errs.ErrUnavailable
		}
		item.ContinueSessionRef = *continueSessionRef
	} else if continueSessionRef != nil {
		return entity.Schedule{}, errs.ErrUnavailable
	}
	item.NextActions = scheduleActions(item, canManage)
	return item, nil
}

// scheduleActions always offers OPEN, adds only DELETE for a managed archived schedule, and
// otherwise adds EDIT/ARCHIVE plus DISABLE or ENABLE according to the current enabled flag. It
// performs no lifecycle transition.
// scheduleActions всегда предлагает OPEN, добавляет лишь DELETE для управляемого архивного
// расписания, иначе EDIT/ARCHIVE и DISABLE либо ENABLE по текущему enabled. Не выполняет переходов
// жизненного цикла.
func scheduleActions(item entity.Schedule, canManage bool) []string {
	actions := []string{"OPEN"}
	if !canManage {
		return actions
	}
	if item.State == "ARCHIVED" {
		return append(actions, "DELETE")
	}
	actions = append(actions, "EDIT", "ARCHIVE")
	if item.Enabled {
		return append(actions, "DISABLE")
	}
	return append(actions, "ENABLE")
}

// ListIntegrationDefinitions validates a stable-key cursor, reads definitions with a lookahead row
// in a read-only snapshot and strictly decodes capabilities/schema. Current organization.manage
// allows COPY only for enabled shipped definitions; collection create hints still use platform
// role. Commit failure discards the page.
// ListIntegrationDefinitions проверяет курсор stable-key, читает определения с дополнительной
// строкой в read-only снимке, строго декодирует capabilities/schema. Текущее organization.manage
// даёт COPY лишь включённым shipped definitions; подсказка создания коллекции использует
// платформенную роль. Отказ commit отбрасывает страницу.
func (repository *Repository) ListIntegrationDefinitions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.IntegrationDefinition, string, []string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", nil, err
	}
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!validStableKey(cursor) || len(cursor) > 96) {
		return nil, "", nil, errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	authorization, err := repository.loadRoleImageAccessContext(ctx, tx, scope)
	if err != nil {
		return nil, "", nil, err
	}
	canCopy := authorization.allowed("organization.manage", resolvedAccessTarget{scope: organizationTarget(scope.organizationRef)})
	rows, err := tx.Query(ctx, queryQueriesListintegrationdefinitionsSelectIntegrationDefinitionsCategory,
		scope.organizationID, strings.TrimSpace(filter.Category), strings.TrimSpace(filter.Query), cursor, limit+1)
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.IntegrationDefinition
	for rows.Next() {
		var item entity.IntegrationDefinition
		var capabilities, schema []byte
		if err := rows.Scan(
			&item.Key, &item.Name, &item.Description, &item.Category, &item.Optional, &item.Enabled,
			&capabilities, &schema, &item.SchemaVersion, &item.DefinitionVersion, &item.Origin,
			&item.Digest, &item.Adapter, &item.CredentialSecretKey,
			&item.AdapterOwner, &item.ExecutionRoute, &item.AdapterReadiness, &item.Version,
			&item.ConnectionCount, &item.HealthyConnectionCount,
		); err != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		if json.Unmarshal(capabilities, &item.Capabilities) != nil || json.Unmarshal(schema, &item.ConfigurationFields) != nil {
			return nil, "", nil, errs.ErrUnavailable
		}
		if canCopy && item.Enabled && item.Origin == "SHIPPED" {
			item.NextActions = []string{"COPY"}
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		next = result[len(result)-1].Key
	}
	actions := collectionCreateActions(scope.role, "CREATE_CONNECTION")
	return result, next, actions, nil
}

// collectionCreateActions returns the supplied single collection action only for exact OWNER or
// ADMINISTRATOR roles; other roles receive an empty slice without an independent access check.
// collectionCreateActions возвращает единственное переданное действие коллекции лишь для точных
// ролей OWNER либо ADMINISTRATOR; остальные получают пустой срез без независимой проверки доступа.
func collectionCreateActions(role, action string) []string {
	if role == "OWNER" || role == "ADMINISTRATOR" {
		return []string{action}
	}
	return []string{}
}

// assistantActions offers OPEN and ready-dependent conversation/turn hints, adding EDIT for
// owner/admin and RECOVER when such an actor sees an unready assistant. Readiness is supplied by
// the caller rather than probed here.
// assistantActions предлагает OPEN и зависящие от ready подсказки беседы/turn, добавляет EDIT для
// owner/admin и RECOVER при неготовом помощнике для этих ролей. Готовность передаёт вызывающий,
// здесь она не проверяется.
func assistantActions(role string, ready bool) []string {
	actions := []string{"OPEN"}
	if ready {
		actions = append(actions, "CREATE_CONVERSATION", "ADD_TURN")
	}
	if role == "OWNER" || role == "ADMINISTRATOR" {
		actions = append(actions, "EDIT")
		if !ready {
			actions = append(actions, "RECOVER")
		}
	}
	return actions
}

// ListIntegrationConnections normalizes the definition-key alias into the category filter,
// rejecting disagreement, then uses the authorized integration catalog. Rows are decoded and
// package/grant readback attached within its transaction; exact integration.manage controls both
// connection and grant hints.
// ListIntegrationConnections переносит alias definition-key в фильтр category, отклоняя
// несовпадение, затем использует авторизованный каталог integrations. Строки декодируются,
// package/grant readback добавляется в его транзакции; точное integration.manage управляет
// подсказками connection и grant.
func (repository *Repository) ListIntegrationConnections(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if filter.DefinitionKey != "" {
		if filter.Category != "" && filter.Category != filter.DefinitionKey {
			return nil, "", errs.ErrInvalid
		}
		filter.Category = filter.DefinitionKey
		filter.DefinitionKey = ""
	}
	return authorizedCatalog(ctx, repository, current, "INTEGRATION", filter,
		func(ctx context.Context, tx pgx.Tx, after string, limit int32) ([]entity.IntegrationConnection, error) {
			rows, err := tx.Query(ctx, queryQueriesListintegrationconnectionsSelectIntegrationConnectionsOrganizationIdDefinitionKey,
				current.organizationID, filter.Category, limit, filter.Query, after)
			if err != nil {
				return nil, errs.ErrUnavailable
			}
			defer rows.Close()
			items := []entity.IntegrationConnection{}
			for rows.Next() {
				item, err := scanConnection(rows)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
			return items, rows.Err()
		},
		func(item entity.IntegrationConnection) entity.AccessScope {
			return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: item.Ref}
		},
		func(tx pgx.Tx, item *entity.IntegrationConnection, allowed func(string) bool) error {
			if err := attachConnection(ctx, tx, current, item); err != nil {
				return err
			}
			item.NextActions = connectionActions(*item, allowed("integration.manage"), allowed("integration.manage"))
			return nil
		})
}

// scanConnection decodes public configuration/capability JSON and credential-revision metadata,
// attaching the revision only when both its reference and creation time exist. It exposes OPEN
// initially; NoRows becomes NotFound and scan/JSON corruption becomes Unavailable without reading
// secret values.
// scanConnection декодирует public configuration/capability JSON и метаданные credential revision,
// добавляя ревизию лишь при наличии ссылки и времени создания. Изначально предлагает OPEN; NoRows
// даёт NotFound, ошибки scan/JSON дают Unavailable, значения секретов не читает.
func scanConnection(row rowScanner) (entity.IntegrationConnection, error) {
	var item entity.IntegrationConnection
	var configuration, capabilities []byte
	var credential entity.IntegrationCredentialRevision
	var credentialCreatedAt *time.Time
	if err := row.Scan(
		&item.Ref, &item.DefinitionKey, &item.DefinitionName, &item.Name, &item.State,
		&item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version,
		&configuration, &capabilities, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt,
		&item.DefinitionVersion, &item.DefinitionDigest, &item.CredentialSecretKey,
		&credential.Ref, &credential.Revision,
		&credential.SecretRef, &credential.SecretUID, &credential.SecretResourceVersion,
		&credential.ContentSHA256, &credentialCreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.IntegrationConnection{}, errs.ErrNotFound
		}
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	if json.Unmarshal(configuration, &item.PublicConfiguration) != nil || json.Unmarshal(capabilities, &item.Capabilities) != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	if credential.Ref != "" && credentialCreatedAt != nil {
		credential.CreatedAt = *credentialCreatedAt
		item.CredentialRevision = &credential
	}
	item.NextActions = []string{"OPEN"}
	return item, nil
}

// connectionQuerier accepts pool or transactional reads needed by connection package/grant
// projections without owning authorization, transaction boundaries or returned row closure.
// connectionQuerier принимает чтения через pool либо транзакцию для package/grant проекций
// connection без владения авторизацией, границами транзакции и закрытием возвращённых строк.
type connectionQuerier interface {
	// Query returns an iterated connection-related result set whose closure and final error checks
	// belong to the caller.
	// Query возвращает итерируемый набор строк connection, закрытие и проверка конечной ошибки
	// принадлежат вызывающему.
	Query(context.Context, string, ...any) (pgx.Rows, error)
	// QueryRow returns a deferred-scan connection-related row; caller scanning interprets absence and
	// query failures.
	// QueryRow возвращает строку connection с отложенным сканированием; вызывающий интерпретирует
	// отсутствие и ошибки запроса при scan.
	QueryRow(context.Context, string, ...any) pgx.Row
}

// connectionActions builds ordered connection/grant hints from supplied management flags,
// enabled/testing/credential state and approval requirements. Disabled connections stop at
// edit/enable/delete; grant management requires CONNECTED, while test/configure hints preserve
// their existing guards. No provider call or access evaluation occurs.
// connectionActions строит упорядоченные подсказки connection/grant по management flags,
// enabled/testing/credential и требованиям одобрения. Отключённый connection ограничен
// edit/enable/delete; управление grants требует CONNECTED, test/configure сохраняют прежние
// условия. Не вызывает провайдера и не проверяет доступ.
func connectionActions(item entity.IntegrationConnection, manageConnection, manageGrants bool) []string {
	actions := []string{"OPEN"}
	if !manageConnection && !manageGrants {
		return actions
	}
	if !item.Enabled {
		if manageConnection {
			return append(actions, "EDIT", "ENABLE", "DELETE")
		}
		return actions
	}
	if manageConnection {
		actions = append(actions, "EDIT")
	}
	if manageConnection && (item.CredentialSecretKey != "" || item.DefinitionKey == "email") && item.State != "TESTING" {
		actions = append(actions, "CONFIGURE_CREDENTIAL")
	}
	if manageConnection && !item.TestRequiresApproval && item.State != "TESTING" && item.MaskedCredentialsState == "CONFIGURED" {
		actions = append(actions, "TEST")
	}
	if manageConnection {
		actions = append(actions, "DISABLE")
	}
	if manageGrants && item.State == "CONNECTED" {
		actions = append(actions, "MANAGE_GRANTS")
	}
	return actions
}

// connectionAuthority queries the organization/actor/connection management predicate and returns
// that same boolean for connection and grant management. Any query/scan error returns two false
// flags and Unavailable; the caller owns surrounding resource eligibility.
// connectionAuthority запрашивает management predicate организации/субъекта/connection и
// возвращает одно значение для управления connection и grants. Любая ошибка запроса/scan даёт два
// false и Unavailable; окружающей eligibility ресурса владеет вызывающий.
func connectionAuthority(ctx context.Context, querier connectionQuerier, scope scope, ref string) (bool, bool, error) {
	var manage bool
	if err := querier.QueryRow(ctx, queryIntegrationConnectionAuthority, scope.organizationID, scope.actorID, ref).Scan(&manage); err != nil {
		return false, false, errs.ErrUnavailable
	}
	return manage, manage, nil
}

// attachConnection projects connection package metadata, then appends
// actor/authority-project-filtered grants with decoded resource scopes and the connection current
// version. Query/scan/iteration errors remain raw; malformed scope JSON returns a fixed error, and
// partial item mutations can precede failure.
// attachConnection проецирует package metadata connection, затем добавляет отфильтрованные по
// субъекту/authority-project grants с декодированными resource scopes и текущей версией
// connection. Ошибки query/scan/итерации остаются исходными; некорректный scope JSON даёт
// фиксированную ошибку, до отказа возможны частичные изменения объекта.
func attachConnection(ctx context.Context, querier connectionQuerier, scope scope, item *entity.IntegrationConnection) error {
	if err := projectConnectionPackage(ctx, querier, scope, item); err != nil {
		return err
	}
	rows, err := querier.Query(ctx, queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef, scope.organizationID, item.Ref, scope.actorID, scope.authorityProjectID, scope.role)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var grant entity.IntegrationGrant
		var resourceScope []byte
		if err := rows.Scan(
			&grant.Ref, &grant.CapabilityKey, &grant.TargetType, &grant.TargetRef, &grant.TargetName,
			&grant.Enabled, &grant.ApprovalPolicy, &grant.Version, &grant.Risk, &grant.ResourceKind,
			&resourceScope, &grant.ResourceScopeDigest, &grant.ApprovalScopePaths,
		); err != nil {
			return err
		}
		if json.Unmarshal(resourceScope, &grant.ResourceScope) != nil {
			return errors.New("decode integration grant resource scope")
		}
		grant.ConnectionVersion = item.Version
		item.Grants = append(item.Grants, grant)
	}
	return rows.Err()
}

// readConnection reads one organization connection through the supplied runner, attaches
// package/grants and derives actions from the management predicate. Attachment failures map to
// Unavailable; the caller must establish exact access and transaction consistency.
// readConnection читает connection организации через переданный runner, добавляет package/grants и
// вычисляет действия по management predicate. Отказ добавления данных даёт Unavailable; точный
// доступ и согласованность транзакции устанавливает вызывающий.
func readConnection(ctx context.Context, querier connectionQuerier, scope scope, ref string) (entity.IntegrationConnection, error) {
	item, err := scanConnection(querier.QueryRow(ctx, queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef, scope.organizationID, ref))
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if err := attachConnection(ctx, querier, scope, &item); err != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	manageConnection, manageGrants, err := connectionAuthority(ctx, querier, scope, ref)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	item.NextActions = connectionActions(item, manageConnection, manageGrants)
	return item, nil
}

// GetIntegrationConnection resolves exact integration-connection assistant context in a read-only
// RepeatableRead transaction, reads the connection/grants and attaches its definition binding
// before commit. Every failure returns an empty connection; it neither tests the provider nor
// reads secret content.
// GetIntegrationConnection разрешает точный assistant context connection в read-only
// RepeatableRead транзакции, читает connection/grants и добавляет definition binding до commit.
// Любая ошибка возвращает пустой connection; провайдера не тестирует, содержимое секретов не
// читает.
func (repository *Repository) GetIntegrationConnection(ctx context.Context, principal value.Principal, ref string) (entity.IntegrationConnection, error) {
	scope, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveAssistantContext(ctx, tx, scope, entity.AssistantContextDescriptor{EntityKind: "INTEGRATION_CONNECTION", EntityRef: ref}, ""); err != nil {
		return entity.IntegrationConnection{}, err
	}
	item, err := readConnection(ctx, tx, scope, ref)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	if err := repository.attachIntegrationDefinitionBinding(ctx, tx, scope, &item); err != nil {
		return entity.IntegrationConnection{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.IntegrationConnection{}, errs.ErrUnavailable
	}
	return item, nil
}

// getAssistant reads the organization system assistant, historically ignores resource-limit JSON
// errors and derives Ready from READY/BUSY state plus a heartbeat younger than45 seconds. It sets
// system/nondeletable flags and role-based actions; this readback does not prove provider
// execution readiness.
// getAssistant читает системного помощника организации, исторически игнорирует ошибки JSON
// resource limits и вычисляет Ready по READY/BUSY и heartbeat моложе45 секунд. Назначает
// system/nondeletable и действия роли; это чтение не доказывает готовность исполнения у
// провайдера.
func (repository *Repository) getAssistant(ctx context.Context, scope scope) (entity.SystemAssistant, error) {
	var item entity.SystemAssistant
	var limits []byte
	err := repository.pool.QueryRow(ctx, queryQueriesGetassistantSelectAssistantRuntimeOrganizationId, scope.organizationID).Scan(&item.Ref, &item.StableKey, &item.Name, &item.Purpose, &item.CorePromptRevision, &item.OwnerInstructions, &item.RuntimeState, &item.RuntimeRevision, &item.DesiredRuntimeRevision, &item.WarmSessionRef, &limits, &item.LastHeartbeatAt, &item.Version, &item.UpdatedAt)
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &item.ResourceLimits)
	item.Ready = contains([]string{"READY", "BUSY"}, item.RuntimeState) && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) < 45*time.Second
	item.System = true
	item.Deletable = false
	item.NextActions = assistantActions(scope.role, item.Ready)
	return item, nil
}

// GetSystemAssistant resolves the actor organization and delegates system-assistant
// readback/action projection to getAssistant without opening a transaction or running a
// recovery/readiness probe.
// GetSystemAssistant разрешает организацию субъекта и делегирует getAssistant чтение/проекцию
// действий системного помощника без открытия транзакции и recovery/readiness проверки.
func (repository *Repository) GetSystemAssistant(ctx context.Context, principal value.Principal) (entity.SystemAssistant, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	return repository.getAssistant(ctx, scope)
}

// ListAssistantConversations bounds the request to five seconds, validates normalized conversation
// filters and a scope/filter-bound timestamp/ref cursor, then reads a lookahead page in a
// read-only snapshot. It attaches turns/plans only for the selected page before commit; failures
// discard all conversations.
// ListAssistantConversations ограничивает запрос пятью секундами, проверяет нормализованные
// фильтры бесед и связанный с областью/фильтром курсор время/ref, читает страницу с дополнительной
// строкой в read-only снимке. Добавляет turns/plans только выбранной странице до commit; ошибки
// отбрасывают все беседы.
func (repository *Repository) ListAssistantConversations(ctx context.Context, principal value.Principal, filter query.AssistantConversationFilter) ([]entity.AssistantConversation, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	if len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, 0) ||
		!contains([]string{"", "SYSTEM", "PROJECT"}, filter.AssistantScope) || len(filter.AssistantRef) > 96 || strings.ContainsRune(filter.AssistantRef, 0) ||
		(filter.MatchAssistantLocalizedDefaultTitle && filter.Query == "") ||
		(filter.State != "ACTIVE" && filter.State != "CLOSED" && filter.State != "ARCHIVED") {
		return nil, "", errs.ErrInvalid
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	cursorKind := "ASSISTANT_CONVERSATIONS:" + filter.AssistantScope + ":" + filter.AssistantRef
	cursor, err := decodeCatalogCursor(scope, cursorKind, filter.Filter)
	if err != nil {
		return nil, "", err
	}
	var cursorAt time.Time
	var cursorRef string
	if cursor != "" {
		at, ref, ok := strings.Cut(cursor, "|")
		if !ok {
			return nil, "", errs.ErrInvalid
		}
		cursorAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil || ref == "" {
			return nil, "", errs.ErrInvalid
		}
		cursorRef = ref
	}
	limit := boundedPage(filter.Page)
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, queryQueriesListassistantconversationsSelectAssistantConversationsOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "actor_id": scope.actorID, "project_ref": filter.ProjectRef, "authority_project": scope.authorityProjectID,
		"assistant_scope": filter.AssistantScope, "assistant_ref": filter.AssistantRef,
		"query": filter.Query, "match_localized_default_title": filter.MatchAssistantLocalizedDefaultTitle,
		"state": filter.State, "evaluated_at": time.Now().UTC(), "cursor_at": cursorAt, "cursor_ref": cursorRef, "page_size": limit + 1})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	var result []entity.AssistantConversation
	for rows.Next() {
		var item entity.AssistantConversation
		if err := rows.Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.ProjectRef,
			&item.SessionRef, &item.State, &item.Version, &item.Context.Route, &item.Context.EntityKind,
			&item.Context.EntityRef, &item.Context.EntityName, &item.Context.EntityVersion,
			&item.Context.AllowedOperations, &item.CreatedAt, &item.UpdatedAt,
			&item.AssistantScope, &item.AssistantRef, &item.AssistantProfileRef); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeCatalogCursor(scope, cursorKind, filter.Filter, last.CreatedAt.UTC().Format(time.RFC3339Nano)+"|"+last.Ref)
	}
	for index := range result {
		if err := repository.attachConversation(ctx, tx, scope, &result[index]); err != nil {
			return nil, "", err
		}
	}
	if tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return result, next, nil
}

// attachConversation appends ordered turns and plans in the supplied transaction, projecting
// runtime message provenance and masking callback text. It decodes/redacts plan operations and
// attaches receipts only for matching terminal revision/state/outcome with a creation time;
// invalid matching receipts fail closed. LatestPlan copies the final accumulated plan; partial
// additions can precede errors.
// attachConversation добавляет упорядоченные turns/plans в переданной транзакции, проецирует
// runtime message provenance и скрывает callback text. Декодирует и редактирует plan operations,
// добавляет receipts лишь при совпадении terminal revision/state/outcome и наличии времени
// создания; некорректная совпадающая receipt закрыто отклоняется. LatestPlan копирует последний
// накопленный план; до ошибки возможны частичные добавления.
func (repository *Repository) attachConversation(ctx context.Context, tx pgx.Tx, scope scope, item *entity.AssistantConversation) error {
	rows, err := tx.Query(ctx, queryQueriesAttachconversationSelectSessionTurnsOrganizationIdSessionIdRef, scope.organizationID, item.Ref)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var turn entity.AssistantTurn
		if err := rows.Scan(&turn.Ref, &turn.Sequence, &turn.Actor, &turn.ActorName, &turn.Content, &turn.State, &turn.AttachmentSetRef, &turn.CreatedAt, &turn.CompletedAt, &turn.RunRef, &turn.RunVersion); err != nil {
			return errs.ErrUnavailable
		}
		item.Turns = append(item.Turns, turn)
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	rows.Close()
	for index := range item.Turns {
		turn := &item.Turns[index]
		turn.Source = entity.MessageSource{Origin: "ORDINARY"}
		if turn.Actor != "AGENT" || turn.RunRef == "" {
			continue
		}
		turn.Source, err = readRuntimeMessageSource(ctx, tx, scope.organizationID, turn.RunRef, turn.Ref, "")
		if err != nil {
			return err
		}
		if turn.Source.Origin == "CALLBACK_CONTINUATION" {
			turn.Content = callbackContinuationPublicText
		}
	}
	planRows, err := tx.Query(ctx, queryQueriesAttachconversationSelectAssistantPlansOrganizationIdRef, scope.organizationID, item.Ref)
	if err != nil {
		return errs.ErrUnavailable
	}
	defer planRows.Close()
	for planRows.Next() {
		var raw, rawReceiptOperations, rawReceiptConflicts []byte
		var plan entity.AssistantPlan
		var receiptRef, receiptOutcome string
		var receiptRevision int64
		var receiptAuditRefs, receiptCreatedRefs []string
		var receiptCreatedAt *time.Time
		if err := planRows.Scan(
			&plan.Ref, &plan.Summary, &plan.State, &plan.Version, &plan.Revision, &plan.ValidatedRevision,
			&plan.ContentDigest, &plan.ValidationProblems, &raw, &plan.CreatedAt, &plan.ValidatedAt, &plan.AppliedAt,
			&receiptRef, &receiptRevision, &receiptOutcome, &rawReceiptOperations, &rawReceiptConflicts,
			&receiptAuditRefs, &receiptCreatedRefs, &receiptCreatedAt,
		); err != nil {
			return errs.ErrUnavailable
		}
		if json.Unmarshal(raw, &plan.Operations) != nil {
			return errs.ErrUnavailable
		}
		redactAssistantFilePlanOperations(plan.Operations)
		if receiptRef != "" && receiptRevision == plan.Revision && assistantPlanStateHasReceipt(plan.State) {
			if !assistantPlanReceiptMatchesState(plan.State, receiptOutcome) || receiptCreatedAt == nil {
				return errs.ErrUnavailable
			}
			receipt := entity.AssistantPlanReceipt{Ref: receiptRef, PlanRef: plan.Ref,
				PlanRevision: receiptRevision, Outcome: receiptOutcome, AuditRefs: receiptAuditRefs,
				CreatedResourceRefs: receiptCreatedRefs, CreatedAt: *receiptCreatedAt}
			if json.Unmarshal(rawReceiptOperations, &receipt.Operations) != nil ||
				json.Unmarshal(rawReceiptConflicts, &receipt.Conflicts) != nil {
				return errs.ErrUnavailable
			}
			plan.Receipt = &receipt
		}
		plan.ConversationRef = item.Ref
		plan.ProjectRef = item.ProjectRef
		item.Plans = append(item.Plans, plan)
	}
	if planRows.Err() != nil {
		return errs.ErrUnavailable
	}
	if len(item.Plans) > 0 {
		latest := item.Plans[len(item.Plans)-1]
		item.LatestPlan = &latest
	}
	return nil
}

// assistantPlanStateHasReceipt recognizes only APPLIED, REJECTED and STALE plan states as
// receipt-bearing; unknown or nonterminal states return false.
// assistantPlanStateHasReceipt считает имеющими receipt только состояния плана APPLIED, REJECTED и
// STALE; неизвестное либо нетерминальное состояние даёт false.
func assistantPlanStateHasReceipt(state string) bool {
	return state == "APPLIED" || state == "REJECTED" || state == "STALE"
}

// assistantPlanReceiptMatchesState matches APPLIED to APPLIED, REJECTED to REJECTED and STALE to
// CONFLICT, rejecting all other pairs without inferring success from receipt existence.
// assistantPlanReceiptMatchesState сопоставляет APPLIED с APPLIED, REJECTED с REJECTED и STALE с
// CONFLICT, отклоняя прочие пары без вывода об успехе из наличия receipt.
func assistantPlanReceiptMatchesState(state, outcome string) bool {
	switch state {
	case "APPLIED":
		return outcome == "APPLIED"
	case "REJECTED":
		return outcome == "REJECTED"
	case "STALE":
		return outcome == "CONFLICT"
	default:
		return false
	}
}

// GetAdministration combines separate assistant/definition reads with adapter counts and failed
// interaction incidents. It reports the existing fixed CoreReady/summary and selects the web
// profile from active adapters rather than performing a live health probe or a single consistent
// snapshot.
// GetAdministration объединяет отдельные чтения помощника/определений со счётчиком adapters и
// неудачными interaction incidents. Возвращает прежние фиксированные CoreReady/summary, выбирает
// web profile по активным adapters без реальной health проверки и единого согласованного снимка.
func (repository *Repository) GetAdministration(ctx context.Context, principal value.Principal) (platformrepo.Administration, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.Administration{}, err
	}
	assistant, err := repository.getAssistant(ctx, scope)
	if err != nil {
		return platformrepo.Administration{}, err
	}
	definitions, _, _, err := repository.ListIntegrationDefinitions(ctx, principal, query.Filter{})
	if err != nil {
		return platformrepo.Administration{}, err
	}
	profile := "WEB_ONLY"
	var activeAdapters int
	if err := repository.pool.QueryRow(ctx, queryInteractionCountActiveAdapters, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	}).Scan(&activeAdapters); err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	if activeAdapters > 0 {
		profile = "WEB_WITH_OPTIONAL_ADAPTERS"
	}
	result := platformrepo.Administration{Profile: profile, CoreReady: true, CoreSummary: "i18n:WEB_ONLY_CORE_SUMMARY", Assistant: assistant, OptionalAdapters: definitions, ObservedAt: time.Now().UTC()}
	incidentRows, err := repository.pool.Query(ctx, queryInteractionListFailedIncidents, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID,
	})
	if err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	defer incidentRows.Close()
	for incidentRows.Next() {
		var incident entity.Incident
		var deliveryState string
		var attempt, maximumAttempts int
		if err := incidentRows.Scan(&incident.Ref, &incident.ProjectRef, &incident.RunRef, &deliveryState, &attempt, &maximumAttempts, &incident.CreatedAt); err != nil {
			return platformrepo.Administration{}, errs.ErrUnavailable
		}
		incident = projectInteractionIncident(incident, deliveryState, attempt, maximumAttempts)
		result.Incidents = append(result.Incidents, incident)
	}
	if err := incidentRows.Err(); err != nil {
		return platformrepo.Administration{}, errs.ErrUnavailable
	}
	return result, nil
}

// ListAuditEvents resolves actor scope, validates an audit position and queries
// actor/project/resource-filtered events with one lookahead row. It preserves SQL order, returns a
// timestamp/ref continuation when trimmed and discards results on query/scan/iteration errors.
// ListAuditEvents разрешает область субъекта, проверяет audit position и читает события с фильтром
// субъекта/проекта/ресурса и дополнительной строкой. Сохраняет SQL порядок, возвращает продолжение
// время/ref при обрезке, отбрасывает результат при ошибках запроса/scan/итерации.
func (repository *Repository) ListAuditEvents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	cursorOccurredAt, cursorRef, err := decodeAuditCursor(filter.Page.Token)
	if err != nil {
		return nil, "", err
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryQueriesListauditeventsSelectAuditEventsOrganizationIdRefAction,
		scope.organizationID, filter.ProjectRef, filter.ResourceRef, filter.Action, filter.Outcome, filter.Query,
		scope.role, scope.actorID, filter.ExcludeTechnical, cursorOccurredAt, cursorRef, limit+1,
	)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	result := make([]entity.AuditEvent, 0, limit+1)
	for rows.Next() {
		var item entity.AuditEvent
		if err := rows.Scan(&item.Ref, &item.ProjectRef, &item.ActorRef, &item.ActorName, &item.Executor, &item.Source, &item.Action, &item.ResourceKind, &item.ResourceRef, &item.ResourceName, &item.Outcome, &item.Summary, &item.CorrelationRef, &item.OccurredAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(result) > int(limit) {
		result = result[:limit]
		last := result[len(result)-1]
		next = encodeAuditCursor(last.OccurredAt, last.Ref)
	}
	return result, next, nil
}

// encodeAuditCursor delegates timestamp/ref encoding to the shared MVP cursor under the audit
// namespace without signing or authorizing the referenced event.
// encodeAuditCursor делегирует кодирование время/ref общему MVP курсору в пространстве audit без
// подписи и авторизации события по ссылке.
func encodeAuditCursor(occurredAt time.Time, ref string) string {
	return encodeMVPCursor("audit", occurredAt, ref)
}

// decodeAuditCursor uses the shared audit cursor decoder and additionally requires nonempty
// references to have aud_ prefix and no line breaks; empty first-page positions remain valid.
// decodeAuditCursor использует общий decoder audit cursor и дополнительно требует префикс aud_ и
// отсутствие переводов строки у непустой ссылки; пустая позиция первой страницы остаётся
// допустимой.
func decodeAuditCursor(token string) (*time.Time, string, error) {
	occurredAt, ref, err := decodeMVPCursor("audit", token)
	if err != nil {
		return nil, "", err
	}
	if ref != "" && (!strings.HasPrefix(ref, "aud_") || strings.ContainsAny(ref, "\r\n")) {
		return nil, "", errs.ErrInvalid
	}
	return occurredAt, ref, nil
}
