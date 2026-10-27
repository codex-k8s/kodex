package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryProviderUsageAccounts Embeds the organization-scoped 4097-row account usage projection with
// current credential identity, active execution count, latest observation state and latest
// successful model content. Empty refs mean all accounts; stale credential/version observations
// become pending, and models are optional. No remote probe is performed.
// queryProviderUsageAccounts Встраивает выборку до 4097 аккаунтов организации с текущей
// идентичностью credential, числом активных выполнений, состоянием последнего наблюдения и
// последним успешным содержимым моделей. Пустые ссылки означают все аккаунты; устаревшая версия
// или credential дают PENDING, модели включаются отдельно. Внешняя проверка не выполняется.
//
//go:embed sql/provider_usage__accounts.sql
var queryProviderUsageAccounts string

// queryProviderUsageAgents Embeds exact organization/agent-ref usage inputs: current runtime
// config/policy/overlay and the policy-selected account, with a 4097-row bound. It projects
// dependencies without reserving account capacity or authorizing launch.
// queryProviderUsageAgents Встраивает данные использования точных агентов организации: текущие
// конфигурацию, policy, overlay и выбранный policy аккаунт с пределом 4097 строк. Проецирует
// зависимости без резервирования ёмкости аккаунта и авторизации запуска.
//
//go:embed sql/provider_usage__agents.sql
var queryProviderUsageAgents string

// queryProviderUsageProfile Reads the global runtime profile by stable key, including
// provider/model/runtime revision and enabled flag. No organization predicate is needed for this
// global catalog; callers decide whether the profile is usable.
// queryProviderUsageProfile Читает глобальный runtime-профиль по стабильному ключу с провайдером,
// моделью, ревизией runtime и признаком включения. Глобальный каталог не ограничен организацией;
// пригодность профиля проверяют вызывающие.
//
//go:embed sql/provider_usage__profile.sql
var queryProviderUsageProfile string

// providerUsageProfile Carries the selected global runtime profile identity/provider/model/runtime
// revision and enabled state for CONFIGURE compatibility checks. A populated profile alone is not
// a launch grant.
// providerUsageProfile Хранит идентичность, провайдера, модель, ревизию runtime и включённость
// глобального профиля для проверки CONFIGURE. Заполненный профиль сам не разрешает запуск.
type providerUsageProfile struct {
	Ref, Provider, Model, RuntimeRevision string
	Enabled                               bool
}

// providerUsageAccount Carries account lifecycle/capacity, current credential and observation
// identifiers, bounded catalog models/status/provenance and transaction observation time.
// Credential values are absent; this dependency snapshot does not reserve capacity or prove
// provider SLA.
// providerUsageAccount Хранит жизненный цикл и ёмкость аккаунта, идентификаторы текущих credential
// и наблюдения, ограниченный каталог моделей с состоянием и происхождением, время наблюдения
// транзакции. Значений credential нет; снимок не резервирует ёмкость и не доказывает SLA
// провайдера.
type providerUsageAccount struct {
	Ref, Provider, State, CredentialID, ObservationID, ContentDigest, CatalogSource string
	Version, Maximum, Active                                                        int64
	Enabled, ProviderEnabled                                                        bool
	CredentialObservedAt                                                            *time.Time
	Catalog                                                                         entity.ModelCatalogStatus
	Models                                                                          []platformrepo.ProviderModelCatalogRecord
	ObservedAt                                                                      time.Time
}

// providerUsageAgent Carries the current agent identity/lifecycle/version/project, runtime
// configuration and digest, provider/model/overlay, pinned candidates and selected account. These
// read-side dependencies require separate actor launch/configure authority.
// providerUsageAgent Хранит текущие идентичность, состояние, версию и проект агента, конфигурацию
// runtime с хешем, провайдера, модель, overlay, закреплённых кандидатов и выбранный аккаунт.
// Зависимости чтения требуют отдельного права субъекта на запуск или настройку.
type providerUsageAgent struct {
	Ref, State, ProjectRef, ConfigRef, ConfigDigest, Provider, Model, Overlay, SelectedAccount string
	Version                                                                                    int64
	Enabled                                                                                    bool
	Candidates                                                                                 []entity.ProviderAccountCandidate
}

// providerUsageContext Combines optional caller usage intent with the resolved agent/profile,
// separately evaluated actor eligibility and subject/binding digest. ActorAllowed is an internal
// projection; it must not be accepted as caller-provided authority.
// providerUsageContext Объединяет необязательное намерение использования с разрешёнными агентом и
// профилем, отдельно вычисленной пригодностью субъекта и хешем субъекта с привязками. ActorAllowed
// является внутренней проекцией и не принимается как полномочие из данных клиента.
type providerUsageContext struct {
	Input           *entity.ProviderAccountUsageContext
	Agent           providerUsageAgent
	ActorAllowed    bool
	AuthorityDigest string
	Profile         providerUsageProfile
}

// readProviderUsageAccounts Reads exact organization/ref accounts, normalizing nil refs to an
// empty SQL array, then enforces 4096 accounts, 128KiB model JSON, lifecycle/capacity and catalog
// invariants. Scan/iteration/decode failures discard partial results as unavailable; too many
// accounts has its own error. Closes rows without committing.
// readProviderUsageAccounts Читает аккаунты точных организации и ссылок, заменяя nil пустым
// SQL-массивом. Проверяет предел 4096 аккаунтов, 128KiB JSON моделей, жизненный цикл, ёмкость и
// каталог. Сбой чтения или декодирования отбрасывает частичный результат с Unavailable;
// переполнение имеет отдельную ошибку. Закрывает rows без commit.
func readProviderUsageAccounts(ctx context.Context, tx pgx.Tx, organization string, refs []string, models bool) ([]providerUsageAccount, error) {
	if refs == nil {
		refs = []string{}
	}
	rows, err := tx.Query(ctx, queryProviderUsageAccounts, pgx.StrictNamedArgs{"organization_id": organization, "account_refs": refs, "include_models": models})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []providerUsageAccount{}
	for rows.Next() {
		var item providerUsageAccount
		var raw []byte
		if rows.Scan(&item.Ref, &item.Version, &item.Provider, &item.State, &item.Enabled, &item.ProviderEnabled, &item.CredentialID,
			&item.CredentialObservedAt, &item.Maximum, &item.Active, &item.ObservationID, &item.Catalog.ObservedAt, &item.Catalog.ExpiresAt,
			&item.Catalog.State, &item.Catalog.Source, &item.Catalog.Failure, &item.ContentDigest, &raw, &item.ObservedAt, &item.CatalogSource) != nil {
			return nil, errs.ErrUnavailable
		}
		if len(result) >= 4096 {
			return nil, errs.ErrProviderCatalogTooLarge
		}
		if len(raw) > 131072 || item.Maximum < 1 || item.Active < 0 || !validProviderAccountLifecycle(item.State, item.Enabled) || decodeStrict(raw, &item.Models) != nil {
			return nil, errs.ErrUnavailable
		}
		if !validProviderUsageCatalog(item) {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// readProviderUsageAgents Requires 1..4096 requested refs and reads current organization agents
// into a ref-keyed map, enforcing bounded strict candidate JSON and rejecting scan/iteration
// failures as unavailable. Missing refs remain absent; caller authorization and transaction
// completion are separate.
// readProviderUsageAgents Требует от 1 до 4096 запрошенных ссылок, читает текущих агентов
// организации в карту по ссылке, ограничивает и строго декодирует JSON кандидатов. Ошибки чтения
// дают Unavailable; отсутствующие ссылки в карту не добавляются. Авторизация и завершение
// транзакции выполняются отдельно.
func readProviderUsageAgents(ctx context.Context, tx pgx.Tx, organization string, refs []string) (map[string]providerUsageAgent, error) {
	if len(refs) == 0 || len(refs) > 4096 {
		return nil, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryProviderUsageAgents, pgx.StrictNamedArgs{"organization_id": organization, "agent_refs": refs})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := map[string]providerUsageAgent{}
	for rows.Next() {
		var item providerUsageAgent
		var candidates []byte
		if rows.Scan(&item.Ref, &item.Version, &item.Enabled, &item.State, &item.ProjectRef, &item.ConfigRef, &item.ConfigDigest, &item.Provider, &item.Model, &candidates, &item.Overlay, &item.SelectedAccount) != nil || len(candidates) > 131072 || decodeStrict(candidates, &item.Candidates) != nil {
			return nil, errs.ErrUnavailable
		}
		result[item.Ref] = item
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// resolveProviderUsageContext Resolves current access subject/bindings and hashes them even
// without usage intent. With input, validates CONFIGURE/LAUNCH fields, exact target/project/view
// authority, separately projects manage/launch permission, reads the agent and optional global
// profile. Missing profile remains a blocked compatibility input; no capacity is claimed.
// resolveProviderUsageContext Разрешает текущего субъекта доступа с привязками и хеширует их даже
// без намерения использования. При наличии входа проверяет поля CONFIGURE/LAUNCH, точную цель,
// проект и просмотр, отдельно проецирует право настройки или запуска, читает агента и
// необязательный глобальный профиль. Отсутствующий профиль блокирует совместимость; ёмкость не
// захватывается.
func (repository *Repository) resolveProviderUsageContext(ctx context.Context, tx pgx.Tx, current scope, input *entity.ProviderAccountUsageContext) (providerUsageContext, error) {
	result := providerUsageContext{Input: input}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return result, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return result, err
	}
	result.AuthorityDigest = digestBytes(asJSON(subject), asJSON(bindings))
	if input == nil {
		return result, nil
	}
	if !validOverlayHistoryRef(input.AgentRef) || !slices.Contains([]string{"CONFIGURE", "LAUNCH"}, input.Purpose) {
		return result, errs.ErrInvalid
	}
	if input.Purpose == "LAUNCH" && (input.Model != "" || input.ProviderDefinitionKey != "" || input.ReasoningEffort != "" || input.RuntimeProfileRef != "") {
		return result, errs.ErrInvalid
	}
	if input.Purpose == "CONFIGURE" && (!validStableKey(input.RuntimeProfileRef) || !validStableKey(input.ProviderDefinitionKey) || input.Model != "" && !validModel(input.Model) || input.Model == "" && input.ReasoningEffort != "" || len(input.ReasoningEffort) > 80) {
		return result, errs.ErrInvalid
	}
	permission, target, err := repository.resolveRuntimeConfigurationTarget(ctx, tx, current, "agent.view", input.AgentRef)
	if err != nil {
		return result, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != target.projectID {
		return result, errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return result, errs.ErrNotFound
	}
	usePermission := "agent.manage"
	if input.Purpose == "LAUNCH" {
		usePermission = "agent.launch"
	}
	if permission == "organization.manage" {
		usePermission = permission
	}
	if err := repository.requireAccess(ctx, tx, current, usePermission, target); err == nil {
		result.ActorAllowed = true
	} else if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
		return result, err
	}
	agents, err := readProviderUsageAgents(ctx, tx, current.organizationID, []string{input.AgentRef})
	if err != nil {
		return result, err
	}
	var ok bool
	result.Agent, ok = agents[input.AgentRef]
	if !ok {
		return result, errs.ErrNotFound
	}
	if input.Purpose == "CONFIGURE" {
		err = tx.QueryRow(ctx, queryProviderUsageProfile, pgx.StrictNamedArgs{"profile_ref": input.RuntimeProfileRef}).Scan(&result.Profile.Ref, &result.Profile.Provider, &result.Profile.Model, &result.Profile.RuntimeRevision, &result.Profile.Enabled)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrUnavailable
		}
	}
	return result, nil
}

// providerDimension Builds a usage dimension from caller-selected bounded state/reason/remediation
// values. It does not perform checks, normalize unknown values or change runtime admission.
// providerDimension Строит измерение использования из выбранных вызывающим ограниченных состояния,
// причины и исправления. Не выполняет проверки, не нормализует неизвестные значения и не меняет
// допуск runtime.
func providerDimension(state, reason, remediation string) entity.ProviderUsageDimension {
	return entity.ProviderUsageDimension{State: state, Reason: reason, Remediation: remediation}
}

// providerAccountUsage Projects lifecycle, credential presence, observed catalog reachability,
// compatibility, actor permission and capacity into usage with a 10-second/catalog-expiry bound
// and deterministic context digest. Selection eligibility excludes capacity/health while
// operational state includes them; READY health is credentialed catalog reachability, not provider
// SLA or execution proof.
// providerAccountUsage Проецирует жизненный цикл, наличие credential, наблюдавшуюся доступность
// каталога, совместимость, право субъекта и ёмкость с пределом 10 секунд либо сроком каталога и
// детерминированным хешем контекста. Пригодность выбора не включает ёмкость и здоровье, рабочее
// состояние учитывает их. READY означает доступность каталога с credential, а не SLA или
// выполненный запуск.
func providerAccountUsage(item providerUsageAccount, context providerUsageContext) (*entity.ProviderAccountUsage, error) {
	ready := providerDimension("READY", "AVAILABLE", "NONE")
	notEvaluated := providerDimension("NOT_EVALUATED", "CONTEXT_REQUIRED", "SELECT_CONTEXT")
	u := &entity.ProviderAccountUsage{Context: context.Input, AccountVersion: item.Version, AgentVersion: context.Agent.Version,
		RuntimeConfigurationRef: context.Agent.ConfigRef, RuntimeConfigurationDigest: context.Agent.ConfigDigest,
		Lifecycle: ready, Credential: providerDimension("READY", "CREDENTIAL_READY", "NONE"),
		ProviderHealth: providerDimension("UNKNOWN", "PROVIDER_HEALTH_UNOBSERVED", "NONE"), ModelCompatibility: notEvaluated, ActorEligibility: notEvaluated,
		Capacity: providerDimension("READY", "CAPACITY_AVAILABLE", "NONE"), MaximumConcurrentExecutions: item.Maximum, ActiveExecutions: item.Active,
		CatalogStatus: &item.Catalog, ObservedAt: item.ObservedAt, ExpiresAt: item.ObservedAt.Add(10 * time.Second), OperationalState: "NOT_EVALUATED"}
	u.ProviderHealthScope = "CREDENTIALED_CATALOG_REACHABILITY"
	// READY относится только к подтверждённому пути текущей credential, не к SLA провайдера.
	if item.CredentialID != "" && item.Catalog.State == "READY" && item.Catalog.Failure == "NONE" &&
		(item.Catalog.Source == "REMOTE_API" || item.Catalog.Source == "REMOTE_CODEX") &&
		item.Catalog.ObservedAt != nil && !item.Catalog.ObservedAt.After(item.ObservedAt) && item.Catalog.ExpiresAt != nil && item.Catalog.ExpiresAt.After(item.ObservedAt) {
		u.ProviderHealth = providerDimension("READY", "CREDENTIALED_CATALOG_REACHABLE", "NONE")
		u.ProviderHealthObservedAt, u.ProviderHealthExpiresAt = item.Catalog.ObservedAt, item.Catalog.ExpiresAt
	}
	if !item.ProviderEnabled {
		u.Lifecycle = providerDimension("BLOCKED", "PROVIDER_DISABLED", "CONTACT_ADMINISTRATOR")
	} else if item.State == "DISABLED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_DISABLED", "ENABLE_ACCOUNT")
	} else if item.State == "REVOKED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_REVOKED", "AUTHORIZE_ACCOUNT")
	} else if item.State == "DELETING" || item.State == "DELETED" {
		u.Lifecycle = providerDimension("BLOCKED", "ACCOUNT_REVOKED", "NONE")
	} else if item.State != "AUTHORIZED" {
		u.Lifecycle = providerDimension("BLOCKED", "AUTHORIZATION_REQUIRED", "AUTHORIZE_ACCOUNT")
	}
	if item.CredentialID == "" {
		u.Credential = providerDimension("BLOCKED", "CREDENTIAL_MISSING", "AUTHORIZE_ACCOUNT")
	}
	if item.Active >= item.Maximum {
		u.Capacity = providerDimension("BLOCKED", "CAPACITY_EXHAUSTED", "WAIT_FOR_CAPACITY")
	}
	if item.Catalog.State == "READY" && item.Catalog.ExpiresAt != nil && item.Catalog.ExpiresAt.Before(u.ExpiresAt) {
		u.ExpiresAt = *item.Catalog.ExpiresAt
	}
	var err error
	u.CatalogDigest, err = modelcatalog.Digest(nil, item.CatalogSource)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	u.CatalogRevision = "mcat_" + u.CatalogDigest
	if context.Input != nil {
		u.ActorEligibility = ready
		if !context.ActorAllowed {
			u.ActorEligibility = providerDimension("BLOCKED", "PERMISSION_REQUIRED", "CONTACT_ADMINISTRATOR")
		}
		u.ModelCompatibility = providerModelCompatibility(item, context, u.CatalogRevision, u.CatalogDigest)
		u.EligibleForSelection = u.Lifecycle.State == "READY" && u.Credential.State == "READY" && u.ActorEligibility.State == "READY"
		if context.Input.Purpose == "CONFIGURE" && context.Input.ProviderDefinitionKey != item.Provider {
			u.EligibleForSelection = false
		}
		u.AllowedToSubmit = u.EligibleForSelection && u.ModelCompatibility.State == "READY"
		u.OperationalState = "UNKNOWN"
		if u.ProviderHealth.State == "READY" {
			u.OperationalState = "READY"
		}
		if !u.AllowedToSubmit || u.Capacity.State == "BLOCKED" {
			u.OperationalState = "BLOCKED"
		}
	}
	u.ContextDigest = digestBytes(asJSON(context), []byte(providerUsageAccountDigest(item)))
	return u, nil
}

// providerModelCompatibility Checks CONFIGURE profile/provider or LAUNCH agent/configuration and
// unique selected candidate, then current catalog provenance/pins, model support,
// capability/default-effort digests and overlay effort compatibility. Returns bounded guidance
// without reserving capacity or executing a provider; requires a non-nil context input.
// providerModelCompatibility Проверяет профиль и провайдера CONFIGURE либо агента, конфигурацию и
// единственного выбранного кандидата LAUNCH, затем текущий каталог, привязки, поддержку модели,
// хеш возможностей и усилие overlay. Возвращает ограниченные подсказки без резервирования ёмкости
// или вызова провайдера; требует непустой вход контекста.
func providerModelCompatibility(item providerUsageAccount, context providerUsageContext, revision, digest string) entity.ProviderUsageDimension {
	blocked := func(reason, remediation string) entity.ProviderUsageDimension {
		return providerDimension("BLOCKED", reason, remediation)
	}
	provider, model, effort := context.Input.ProviderDefinitionKey, context.Input.Model, context.Input.ReasoningEffort
	if context.Input.Purpose == "CONFIGURE" && (!context.Profile.Enabled || context.Profile.Ref != context.Input.RuntimeProfileRef || context.Profile.Provider != provider || context.Profile.Model == "" || context.Profile.RuntimeRevision == "") {
		return blocked("RUNTIME_PROFILE_UNAVAILABLE", "REPUBLISH_CONFIGURATION")
	}
	var selected *entity.ProviderAccountCandidate
	if context.Input.Purpose == "LAUNCH" {
		if !context.Agent.Enabled || context.Agent.State != "READY" {
			return blocked("AGENT_UNAVAILABLE", "CONTACT_ADMINISTRATOR")
		}
		if context.Agent.ConfigRef == "" {
			return blocked("RUNTIME_CONFIGURATION_MISSING", "REPUBLISH_CONFIGURATION")
		}
		provider, model = context.Agent.Provider, context.Agent.Model
		for _, candidate := range context.Agent.Candidates {
			if candidate.AccountRef == item.Ref {
				if selected != nil {
					return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
				}
				copy := candidate
				selected = &copy
			}
		}
		if selected == nil {
			return blocked("ACCOUNT_NOT_SELECTED", "REPUBLISH_CONFIGURATION")
		}
		overlay, err := runtimecontract.ParseConfigOverlay(context.Agent.Overlay)
		if err != nil {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		effort = overlay.ModelReasoningEffort
	}
	if provider != item.Provider {
		return blocked("PROVIDER_MISMATCH", "SELECT_MODEL")
	}
	if model == "" {
		return providerDimension("NOT_EVALUATED", "MODEL_REQUIRED", "SELECT_MODEL")
	}
	if item.Catalog.State != "READY" {
		reason := "CATALOG_" + item.Catalog.State
		if item.Catalog.State == "FAILED" {
			reason = "CATALOG_" + item.Catalog.Failure
		}
		remediation := "WAIT_FOR_OBSERVATION"
		if reason == "CATALOG_AUTHORIZATION_REJECTED" {
			remediation = "AUTHORIZE_ACCOUNT"
		}
		if reason == "CATALOG_UNVERIFIED_SOURCE" {
			remediation = "CONTACT_ADMINISTRATOR"
		}
		return blocked(reason, remediation)
	}
	if selected != nil && (!validRuntimeCatalogPin(*selected) || selected.ProviderDefinitionKey != provider || selected.ModelCapabilityDigest == "" && (selected.CatalogRevision != revision || selected.CatalogDigest != digest)) {
		return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
	}
	for _, capability := range item.Models {
		if capability.ID != model {
			continue
		}
		if selected != nil && selected.ModelCapabilityDigest != "" && selected.ModelCapabilityDigest != modelcatalog.CapabilityDigest(provider, item.Ref, capability.ID, capability.ReasoningEfforts, capability.DefaultReasoningEffort, capability.IsDefault) {
			return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
		}
		if selected != nil && selected.DefaultReasoningEffort != capability.DefaultReasoningEffort {
			return blocked("CATALOG_PIN_CHANGED", "REPUBLISH_CONFIGURATION")
		}
		if effort != "" && !slices.Contains(capability.ReasoningEfforts, effort) {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		if len(runtimecontract.DiagnoseConfigOverlay(context.Agent.Overlay, capability.ReasoningEfforts)) != 0 {
			return blocked("EFFORT_UNSUPPORTED", "CHANGE_EFFORT")
		}
		return providerDimension("READY", "AVAILABLE", "NONE")
	}
	return blocked("MODEL_UNSUPPORTED", "SELECT_MODEL")
}

// providerUsageAccountDigest Hashes a value copy of account usage metadata after removing model
// payload and zeroing transaction observation time. Other catalog/credential/capacity changes
// remain represented; the caller value is not mutated and this is not a signature.
// providerUsageAccountDigest Хеширует копию метаданных использования аккаунта после удаления
// моделей и обнуления времени наблюдения транзакции. Остальные изменения каталога, credential и
// ёмкости учитываются; исходное значение не меняется, подпись не создаётся.
func providerUsageAccountDigest(item providerUsageAccount) string {
	item.Models = nil
	item.ObservedAt = time.Time{}
	return digestBytes(asJSON(item))
}

// validProviderUsageCatalog Checks closed catalog state/failure/source vocabularies, READY
// timestamps, at most 128 unique valid model IDs and up to 16 unique supported reasoning efforts
// with a consistent default. It validates stored shape/expiry only, not remote availability or
// authorization.
// validProviderUsageCatalog Проверяет закрытые наборы состояний, ошибок и источников каталога,
// сроки READY, до 128 уникальных допустимых моделей и до 16 уникальных поддерживаемых усилий с
// согласованным значением по умолчанию. Проверяет сохранённую форму и срок, а не внешнюю
// доступность или полномочия.
func validProviderUsageCatalog(item providerUsageAccount) bool {
	if !slices.Contains([]string{"READY", "PENDING", "FAILED", "EXPIRED"}, item.Catalog.State) || len(item.Models) > 128 {
		return false
	}
	if !slices.Contains([]string{"", "NONE", "UNAVAILABLE", "UNVERIFIED_SOURCE", "AUTHORIZATION_REJECTED"}, item.Catalog.Failure) || !slices.Contains([]string{"", "REMOTE_API", "REMOTE_CODEX"}, item.Catalog.Source) {
		return false
	}
	if item.Catalog.State == "READY" && (item.Catalog.Failure != "NONE" || item.Catalog.Source == "" || item.Catalog.ObservedAt == nil || item.Catalog.ExpiresAt == nil || !item.Catalog.ExpiresAt.After(item.ObservedAt)) {
		return false
	}
	seen := map[string]bool{}
	for _, model := range item.Models {
		if !validModel(model.ID) || seen[model.ID] || len(model.ReasoningEfforts) > 16 {
			return false
		}
		seen[model.ID] = true
		efforts := map[string]bool{}
		for _, effort := range model.ReasoningEfforts {
			if efforts[effort] || runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil {
				return false
			}
			efforts[effort] = true
		}
		if len(efforts) == 0 && model.DefaultReasoningEffort != "" || len(efforts) > 0 && !efforts[model.DefaultReasoningEffort] {
			return false
		}
	}
	return true
}

// providerUsageRead Resolves scope, starts read-only RepeatableRead and requires
// provider.account.view on the caller-derived target. Access failures roll back with the request
// context and are hidden as not-found; successful callers own commit/rollback, and rollback errors
// here remain ignored.
// providerUsageRead Разрешает область, открывает read-only RepeatableRead и требует
// provider.account.view для цели, вычисленной вызывающим. Ошибка доступа вызывает rollback с
// контекстом запроса и скрывается как NotFound; при успехе транзакцией управляет вызывающий.
// Ошибка rollback здесь игнорируется.
func (repository *Repository) providerUsageRead(ctx context.Context, p value.Principal, target func(scope) entity.AccessScope) (scope, pgx.Tx, error) {
	current, tx, err := repository.beginScopedSnapshotRead(ctx, p)
	if err != nil {
		return scope{}, nil, err
	}
	if err := repository.requireAccess(ctx, tx, current, "provider.account.view", target(current)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, errs.ErrNotFound
	}
	return current, tx, nil
}

// providerAdmissionSnapshot Carries submit eligibility, operational state/context digest and
// optional full usage for the policy-selected account of an agent. It is a read dependency
// snapshot, not reserved capacity or proof of actor launch authority.
// providerAdmissionSnapshot Хранит пригодность подачи, рабочее состояние, хеш контекста и
// необязательную полную проекцию использования выбранного policy аккаунта агента. Это снимок
// зависимости чтения, а не резерв ёмкости или подтверждение права субъекта на запуск.
type providerAdmissionSnapshot struct {
	AllowedToSubmit                 bool
	OperationalState, ContextDigest string
	Usage                           *entity.ProviderAccountUsage
}

// providerAdmissionForAgents Reads current agents and distinct policy-selected accounts, then
// projects LAUNCH compatibility per requested ref. Missing accounts produce blocked snapshots;
// actor eligibility is assumed already checked by the workflow/agent caller. Reads neither reserve
// capacity nor select replacement accounts.
// providerAdmissionForAgents Читает текущих агентов и различные выбранные policy аккаунты, затем
// проецирует совместимость LAUNCH по каждой запрошенной ссылке. Отсутствующий аккаунт даёт
// BLOCKED; право субъекта предполагается уже проверенным вызывающим workflow или agent. Чтение не
// резервирует ёмкость и не подбирает замену аккаунту.
func (repository *Repository) providerAdmissionForAgents(ctx context.Context, tx pgx.Tx, current scope, refs []string) (map[string]providerAdmissionSnapshot, error) {
	agents, err := readProviderUsageAgents(ctx, tx, current.organizationID, refs)
	if err != nil {
		return nil, err
	}
	accountRefs := []string{}
	for _, agent := range agents {
		if agent.SelectedAccount != "" && !slices.Contains(accountRefs, agent.SelectedAccount) {
			accountRefs = append(accountRefs, agent.SelectedAccount)
		}
	}
	accounts := []providerUsageAccount{}
	if len(accountRefs) > 0 {
		accounts, err = readProviderUsageAccounts(ctx, tx, current.organizationID, accountRefs, true)
		if err != nil {
			return nil, err
		}
	}
	byRef := map[string]providerUsageAccount{}
	for _, account := range accounts {
		byRef[account.Ref] = account
	}
	result := map[string]providerAdmissionSnapshot{}
	for _, ref := range refs {
		agent := agents[ref]
		account, ok := byRef[agent.SelectedAccount]
		if !ok {
			result[ref] = providerAdmissionSnapshot{OperationalState: "BLOCKED", ContextDigest: digestBytes(asJSON(agent))}
			continue
		}
		// Actor admission выполняет общий Workflow/Agent caller; это внутренний dependency snapshot.
		usage, err := providerAccountUsage(account, providerUsageContext{Input: &entity.ProviderAccountUsageContext{Purpose: "LAUNCH", AgentRef: ref}, Agent: agent, ActorAllowed: true})
		if err != nil {
			return nil, err
		}
		result[ref] = providerAdmissionSnapshot{AllowedToSubmit: usage.AllowedToSubmit, OperationalState: usage.OperationalState, ContextDigest: usage.ContextDigest, Usage: usage}
	}
	return result, nil
}
