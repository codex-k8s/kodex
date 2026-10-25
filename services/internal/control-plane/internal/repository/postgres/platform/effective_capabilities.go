package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryEffectiveCapabilitiesAgent Embeds exact organization/ref agent version/capability
// projection excluding archived agents and inactive projects, deriving readiness from enabled and
// READY state. Actor eligibility is checked by the surrounding read path.
// queryEffectiveCapabilitiesAgent Встраивает проекцию версии и возможностей агента точных
// организации и ссылки, исключая архивных агентов и неактивные проекты. Готовность выводится из
// enabled и READY; доступ субъекта проверяет окружающий путь чтения.
//
//go:embed sql/effective_capabilities_agent.sql
var queryEffectiveCapabilitiesAgent string

// queryEffectiveCapabilitiesWorkflow Embeds the published-version specification of a nonarchived
// workflow in an active project under exact organization/ref. Callers separately require
// workflow.view, same project and the matching step agent.
// queryEffectiveCapabilitiesWorkflow Встраивает спецификацию опубликованной версии неархивного
// workflow активного проекта по точным организации и ссылке. Вызывающий отдельно требует
// workflow.view, тот же проект и совпадение агента этапа.
//
//go:embed sql/effective_capabilities_workflow.sql
var queryEffectiveCapabilitiesWorkflow string

// queryEffectiveCapabilitiesGrants Embeds up to 4097 agent-target grants with current
// connection/definition pins, adapter route/readiness and approval policy, projecting an
// eligibility boolean. It returns metadata for explanation; the caller still validates
// package/actor/runtime boundaries.
// queryEffectiveCapabilitiesGrants Встраивает до 4097 разрешений цели-агента с текущими привязками
// соединения и пакета, маршрутом и готовностью адаптера, правилом одобрения и boolean пригодности.
// Возвращает метаданные объяснения; пакет, субъект и runtime проверяются вызывающим.
//
//go:embed sql/effective_capabilities_grants.sql
var queryEffectiveCapabilitiesGrants string

const (
	// capabilityAvailable Names the reason for an effective capability after all applicable
	// actor/request/workflow/runtime checks.
	// capabilityAvailable Задаёт причину пригодной возможности после всех применимых проверок
	// субъекта, запроса, workflow и runtime.
	capabilityAvailable = "AVAILABLE"
	// capabilityActorDenied Names the highest-priority actor-permission denial in effective-capability
	// explanation.
	// capabilityActorDenied Задаёт отказ прав субъекта с наивысшим приоритетом в объяснении
	// возможности.
	capabilityActorDenied = "ACTOR_PERMISSION_REQUIRED"
	// capabilityNotRequested Names a capability missing from the agent request, also used to expose
	// unknown workflow requirements rather than hiding them.
	// capabilityNotRequested Задаёт отсутствие возможности в запросе агента; также показывает
	// неизвестное требование workflow вместо его скрытия.
	capabilityNotRequested = "AGENT_CAPABILITY_REQUIRED"
	// capabilityRuntimeNotReady Names the runtime-readiness denial after actor/request/workflow checks
	// pass.
	// capabilityRuntimeNotReady Задаёт отказ готовности runtime после успешных проверок субъекта,
	// запроса и workflow.
	capabilityRuntimeNotReady = "RUNTIME_NOT_READY"
	// capabilityWorkflowExcluded Names a requested capability excluded because the selected workflow
	// step does not require it.
	// capabilityWorkflowExcluded Задаёт исключение запрошенной возможности, которая не требуется
	// выбранному этапу workflow.
	capabilityWorkflowExcluded = "WORKFLOW_CAPABILITY_NOT_REQUIRED"
	// capabilityGrantUnavailable Names an unavailable integration grant without converting it into an
	// executable capability.
	// capabilityGrantUnavailable Задаёт недоступность разрешения интеграции без превращения его в
	// исполняемую возможность.
	capabilityGrantUnavailable = "INTEGRATION_GRANT_UNAVAILABLE"
	// capabilityPackageUnavailable Names an unavailable integration revision for explanatory read
	// paths; it does not enable a historical decoder or execution fallback.
	// capabilityPackageUnavailable Задаёт недоступность ревизии интеграции для объясняющего чтения; не
	// включает исторический декодер или запасной путь выполнения.
	capabilityPackageUnavailable = "INTEGRATION_REVISION_UNAVAILABLE"
)

// GetAgentEffectiveCapabilities Within five seconds and read-only RepeatableRead, validates
// input/scope, reads current agent/runtime/actor/workflow dependencies and explains
// platform/integration capabilities, including unknown workflow requirements. Sorts and pages a
// snapshot-bound result before commit; readiness/Grantable are read projections, not command
// grants. Existing errors may return partial metadata with the error.
// GetAgentEffectiveCapabilities За пять секунд в read-only RepeatableRead проверяет вход и
// область, читает текущие зависимости агента, runtime, субъекта и workflow, объясняет
// платформенные и интеграционные возможности, включая неизвестные требования workflow. Сортирует и
// разбивает привязанный к снимку результат до commit. Готовность и Grantable являются проекциями
// чтения; прежние ошибки могут возвращаться с частичными метаданными.
func (repository *Repository) GetAgentEffectiveCapabilities(ctx context.Context, principal value.Principal, agentRef, workflowRef, stepKey string, filter query.Filter) (entity.AgentEffectiveCapabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(agentRef) || (workflowRef == "") != (stepKey == "") || workflowRef != "" && (!validOverlayHistoryRef(workflowRef) || len(stepKey) > 96 || !utf8.ValidString(stepKey) || strings.IndexFunc(stepKey, unicode.IsControl) >= 0) || !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, '\x00') || len(filter.Page.Token) > 2048 {
		return entity.AgentEffectiveCapabilities{}, errs.ErrInvalid
	}
	current, tx, err := repository.beginScopedSnapshotRead(ctx, principal)
	if err != nil {
		return entity.AgentEffectiveCapabilities{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := repository.overlayHistoryScope(ctx, tx, current, agentRef); err != nil {
		return entity.AgentEffectiveCapabilities{}, err
	}
	result := entity.AgentEffectiveCapabilities{AgentRef: agentRef, WorkflowRef: workflowRef, StepKey: stepKey, EvaluatedAt: time.Now().UTC(), Items: []entity.EffectiveCapability{}}
	var enabled bool
	var requested []string
	err = tx.QueryRow(ctx, queryEffectiveCapabilitiesAgent, current.organizationID, agentRef).Scan(&result.AgentVersion, &result.ProjectRef, &enabled, &requested)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	authority, err := repository.capabilityAuthority(ctx, tx, current, result.ProjectRef, agentRef)
	if err != nil {
		return result, err
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, current, agentRef)
	if err != nil {
		return result, err
	}
	result.RuntimeConfigurationRef, result.RuntimeConfigurationVersion = view.Configuration.Ref, view.Configuration.Version
	result.EnvironmentVersionRef = view.EnvironmentBinding.VersionRef
	// Scanner проверил полный image contract в этой транзакции до удаления
	// внутренних полей из public view; повторная проверка очищенного image неверна.
	result.RuntimeReady = enabled && view.Environment.Ready
	if result.RuntimeReady {
		result.RuntimeReady, err = capabilityCatalogReady(ctx, tx, current, view.Configuration)
		if err != nil {
			return result, err
		}
	}
	required, err := repository.effectiveWorkflowCapabilities(ctx, tx, current, &result)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, queryQueriesListcapabilitiesSelectPlatformCapabilitiesEnabled)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	for rows.Next() {
		item := entity.EffectiveCapability{Source: "PLATFORM"}
		var risk string
		if rows.Scan(&item.Key, &item.Name, &item.Description, &risk) != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		item.Requested = slices.Contains(requested, item.Key)
		item.Required = slices.Contains(required, item.Key)
		item.Grantable = authority.platformAllowed(item.Key)
		item.Reason = effectiveCapabilityReason(item.Requested, item.Grantable, result.RuntimeReady, workflowRef != "", item.Required)
		item.Effective = item.Reason == capabilityAvailable
		result.Items = append(result.Items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	integrations, err := repository.effectiveIntegrationCapabilities(ctx, tx, current, authority, agentRef, result.RuntimeReady, required, workflowRef != "")
	if err != nil {
		return result, err
	}
	result.Items = append(result.Items, integrations...)
	// Отсутствующее в registry требование этапа тоже объясняется, а не исчезает.
	for _, key := range required {
		if !slices.ContainsFunc(result.Items, func(item entity.EffectiveCapability) bool { return item.Key == key }) {
			result.Items = append(result.Items, entity.EffectiveCapability{Key: key, Name: key, Source: "WORKFLOW", Required: true, Reason: capabilityNotRequested})
		}
	}
	if err := pageEffectiveCapabilities(&result, current, filter); err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// capabilityCatalogReady Requires at least one pinned account and validates every candidate
// against current readable catalog revision/digest or model capability digest and available
// eligible model. Missing catalog is not-ready; other errors propagate. It does not probe
// providers, reserve capacity or authorize launch.
// capabilityCatalogReady Требует хотя бы один закреплённый аккаунт и сверяет каждого кандидата с
// текущими ревизией и хешем каталога либо хешем возможностей модели и доступной пригодной моделью.
// Отсутствующий каталог означает неготовность, другая ошибка возвращается. Провайдера не
// опрашивает, ёмкость не резервирует и запуск не авторизует.
func capabilityCatalogReady(ctx context.Context, tx pgx.Tx, current scope, configuration entity.AgentRuntimeConfiguration) (bool, error) {
	if len(configuration.ProviderPolicy.AccountCandidates) == 0 {
		return false, nil
	}
	for _, candidate := range configuration.ProviderPolicy.AccountCandidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, configuration.Provider, candidate.AccountRef)
		if errors.Is(err, errs.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !validRuntimeCatalogPin(candidate) || candidate.ModelCapabilityDigest == "" && (catalog.Digest != candidate.CatalogDigest || catalog.Revision != candidate.CatalogRevision) {
			return false, nil
		}
		if !slices.ContainsFunc(catalog.Models, func(model entity.ModelCapability) bool {
			return model.ID == configuration.Model && model.Available && slices.Contains(model.EligibleProviderAccountRefs, candidate.AccountRef) &&
				(candidate.ModelCapabilityDigest == "" || candidate.ModelCapabilityDigest == modelcatalog.CapabilityDigest(configuration.Provider, candidate.AccountRef, model.ID, model.ReasoningEfforts, model.DefaultReasoningEffort, model.IsDefault))
		}) {
			return false, nil
		}
	}
	return true, nil
}

// effectiveWorkflowCapabilities For an optional workflow context, requires same-project
// workflow.view, reads its published specification and resolves the exact step assigned to this
// agent. Returns a copied required-key list; missing/mismatched step is not-found and malformed
// stored JSON is unavailable.
// effectiveWorkflowCapabilities Для необязательного контекста workflow требует тот же проект и
// workflow.view, читает опубликованную спецификацию и разрешает точный этап данного агента.
// Возвращает копию требуемых ключей; отсутствующий или иной этап даёт NotFound, повреждённый JSON
// — Unavailable.
func (repository *Repository) effectiveWorkflowCapabilities(ctx context.Context, tx pgx.Tx, current scope, result *entity.AgentEffectiveCapabilities) ([]string, error) {
	if result.WorkflowRef == "" {
		return nil, nil
	}
	target, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "WORKFLOW", ResourceRef: result.WorkflowRef})
	if err != nil {
		return nil, err
	}
	if target.scope.ProjectRef != result.ProjectRef {
		return nil, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, "workflow.view", target); err != nil {
		return nil, err
	}
	var projectRef string
	var raw []byte
	err = tx.QueryRow(ctx, queryEffectiveCapabilitiesWorkflow, current.organizationID, result.WorkflowRef).Scan(&projectRef, &result.WorkflowVersionRef, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrNotFound
	}
	if err != nil || projectRef != result.ProjectRef {
		return nil, errs.ErrUnavailable
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(raw, &version) != nil {
		return nil, errs.ErrUnavailable
	}
	for _, step := range version.Steps {
		if step.Key == result.StepKey {
			if step.AgentRef != result.AgentRef {
				return nil, errs.ErrNotFound
			}
			return append([]string{}, step.RequiredCapabilityKeys...), nil
		}
	}
	return nil, errs.ErrNotFound
}

// effectiveCapabilityReason Chooses one reason in fixed order: actor denied, not requested,
// workflow excluded, runtime not ready, then available. This priority explains a snapshot without
// authorizing or mutating runtime state.
// effectiveCapabilityReason Выбирает одну причину в фиксированном порядке: отказ субъекта,
// отсутствие запроса, исключение workflow, неготовность runtime, затем доступность. Приоритет
// объясняет снимок без авторизации и изменения runtime.
func effectiveCapabilityReason(requested, allowed, ready, workflow, required bool) string {
	if !allowed {
		return capabilityActorDenied
	}
	if !requested {
		return capabilityNotRequested
	}
	if workflow && !required {
		return capabilityWorkflowExcluded
	}
	if !ready {
		return capabilityRuntimeNotReady
	}
	return capabilityAvailable
}

// effectiveCapabilityCursor Carries the full-result digest,
// organization/actor/project/agent/workflow/step/query scope digest and last composite row key in
// an unsigned page token. Fresh server-side comparison binds pagination; the cursor grants no
// authority.
// effectiveCapabilityCursor Хранит хеш полного результата, хеш области организации, субъекта,
// проекта, агента, workflow, этапа и запроса, последний составной ключ строки в неподписанном
// токене. Свежая серверная сверка связывает страницы; полномочий курсор не выдаёт.
type effectiveCapabilityCursor struct{ Digest, Scope, After string }

// pageEffectiveCapabilities Sorts all items by key/connection/grant, hashes the full snapshot
// excluding time/digest/page metadata, validates a bounded scope/snapshot cursor with decodeStrict
// (unknown fields rejected; duplicate keys/trailing input are not separately guarded), then
// filters text and returns a bounded page with filtered total. It mutates result in place; the
// token is unsigned and final cursor marshal errors remain ignored.
// pageEffectiveCapabilities Сортирует все элементы по ключу, соединению и разрешению, хеширует
// полный снимок без времени, хеша и метаданных страницы, проверяет ограниченный курсор области и
// снимка через decodeStrict (неизвестные поля отклоняются, повторные ключи и остаток входа
// отдельно не проверяются), затем фильтрует текст и возвращает страницу с total после фильтра.
// Меняет result на месте; токен неподписанный, ошибка финального marshal курсора по-прежнему
// игнорируется.
func pageEffectiveCapabilities(result *entity.AgentEffectiveCapabilities, current scope, filter query.Filter) error {
	slices.SortFunc(result.Items, func(a, b entity.EffectiveCapability) int {
		return strings.Compare(effectiveCapabilityRowKey(a), effectiveCapabilityRowKey(b))
	})
	canonical := *result
	canonical.EvaluatedAt = time.Time{}
	canonical.Digest = ""
	canonical.NextPageToken = ""
	canonical.Total = 0
	raw, err := json.Marshal(canonical)
	if err != nil {
		return errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	result.Digest = hex.EncodeToString(sum[:])
	bound := sha256.Sum256([]byte(strings.Join([]string{current.organizationID, current.actorID, current.authorityProjectID, result.AgentRef, result.WorkflowRef, result.StepKey, filter.Query}, "\x00")))
	scopeDigest := hex.EncodeToString(bound[:])
	after := ""
	result.Total, result.NextPageToken = 0, ""
	if filter.Page.Token != "" {
		if len(filter.Page.Token) > 2048 {
			return errs.ErrInvalid
		}
		encoded, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		var cursor effectiveCapabilityCursor
		if err != nil || len(encoded) > 1024 || decodeStrict(encoded, &cursor) != nil || cursor.Scope != scopeDigest || cursor.After == "" {
			return errs.ErrInvalid
		}
		if cursor.Digest != result.Digest {
			return errs.ErrVersionMismatch
		}
		after = cursor.After
	}
	items := []entity.EffectiveCapability{}
	limit := int(boundedPage(filter.Page))
	query := strings.ToLower(filter.Query)
	for _, item := range result.Items {
		if query != "" && !strings.Contains(strings.ToLower(item.Key+" "+item.Name+" "+item.Description), query) {
			continue
		}
		result.Total++
		if effectiveCapabilityRowKey(item) > after {
			items = append(items, item)
		}
	}
	if len(items) > limit {
		items = items[:limit]
		raw, _ := json.Marshal(effectiveCapabilityCursor{Digest: result.Digest, Scope: scopeDigest, After: effectiveCapabilityRowKey(items[len(items)-1])})
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(raw)
	}
	result.Items = items
	return nil
}

// effectiveCapabilityRowKey Builds the stable lexical capability/connection/grant key with colon
// separators for sorting and page position. It is a display/pagination key, not an authority
// identity or collision-resistant digest.
// effectiveCapabilityRowKey Строит стабильный лексический ключ возможности, соединения и
// разрешения с разделителями-двоеточиями для сортировки и позиции страницы. Это ключ представления
// и страниц, а не идентичность полномочий или устойчивый к коллизиям хеш.
func effectiveCapabilityRowKey(item entity.EffectiveCapability) string {
	return item.Key + ":" + item.ConnectionRef + ":" + item.GrantRef
}
