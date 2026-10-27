package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeCatalogReadConfiguration reads provider/model/account candidates for the selected
// agent configuration and the agent current overlay by exact organization/agent and optional
// internal config ID. It does not check actor permissions or account eligibility; the caller owns
// those boundaries.
// queryRuntimeCatalogReadConfiguration читает provider, model и account candidates выбранной
// конфигурации агента и текущий overlay агента по точным организации, агенту и необязательному ID
// конфигурации. Полномочия субъекта и доступность accounts проверяет вызывающий.
//
//go:embed sql/runtime_catalog__read_configuration.sql
var queryRuntimeCatalogReadConfiguration string

// queryRuntimeCatalogLockAccounts requires an enabled authorized exact
// organization/account/provider with current credential and enabled provider definition, locking
// account and definition FOR SHARE. It serializes credential/revoke decisions within the caller
// transaction without selecting secret bytes.
// queryRuntimeCatalogLockAccounts требует включённый AUTHORIZED account точных организации, ссылки
// и provider с текущими credential и включённым определением, блокируя account и definition FOR
// SHARE. Упорядочивает credential/revoke решения в транзакции без чтения значений секретов.
//
//go:embed sql/runtime_catalog__lock_accounts.sql
var queryRuntimeCatalogLockAccounts string

// queryRuntimeCatalogReadAccounts checks the same enabled/authorized/current-credential
// organization/account/provider eligibility without a row lock. It is a snapshot predicate, not a
// reservation against concurrent revocation or permission grant.
// queryRuntimeCatalogReadAccounts проверяет те же включение, авторизацию и текущие credential
// точных организации, account и provider без блокировки. Это условие снимка, не резервирование от
// конкурентного отзыва и не выдача прав.
//
//go:embed sql/runtime_catalog__read_accounts.sql
var queryRuntimeCatalogReadAccounts string

// queryRuntimeCatalogBootstrapAccounts returns at most 32 enabled authorized organization/provider
// accounts with current credentials in ref order. If any latest applied authorized method is
// DEVICE_CODE, only such preferred accounts remain; provider-definition and model/catalog checks
// still follow.
// queryRuntimeCatalogBootstrapAccounts возвращает до 32 включённых AUTHORIZED accounts организации
// и provider с текущими credential по ссылке. Если последний применённый AUTHORIZED метод хотя бы
// одного account — DEVICE_CODE, остаются только такие предпочтительные accounts; definition и
// model/catalog проверки выполняются далее.
//
//go:embed sql/runtime_catalog__bootstrap_accounts.sql
var queryRuntimeCatalogBootstrapAccounts string

// queryRuntimeCatalogLockAgent takes FOR SHARE on the exact organization/agent ref and returns its
// internal ID without lifecycle, actor or runtime-configuration checks. The surrounding owner path
// establishes those conditions.
// queryRuntimeCatalogLockAgent берёт FOR SHARE точного агента организации, возвращая внутренний ID
// без проверки жизненного цикла, субъекта и runtime configuration. Эти условия устанавливает
// внешний путь владельца.
//
//go:embed sql/runtime_catalog__lock_agent.sql
var queryRuntimeCatalogLockAgent string

// bootstrapUnpinnedCatalogCandidates reads the ordered bootstrap account pool as an allocated
// slice of weight-one candidates with no catalog pins. Query/scan/iteration failure discards the
// entire result as Unavailable; later capture/validation is required before execution.
// bootstrapUnpinnedCatalogCandidates читает упорядоченный bootstrap пул в выделенный срез
// кандидатов веса один без catalog pins. Ошибки запроса, Scan и iteration отбрасывают результат
// как Unavailable; до исполнения обязательны последующие capture и validation.
func bootstrapUnpinnedCatalogCandidates(ctx context.Context, tx pgx.Tx, organizationID, provider string) ([]entity.ProviderAccountCandidate, error) {
	rows, err := tx.Query(ctx, queryRuntimeCatalogBootstrapAccounts, organizationID, provider)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	result := []entity.ProviderAccountCandidate{}
	for rows.Next() {
		candidate := entity.ProviderAccountCandidate{Weight: 1}
		if rows.Scan(&candidate.AccountRef) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, candidate)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// validRuntimeCatalogPin checks catalog digest grammar, exact mcat_ revision derivation, stable
// provider key and an optional model capability digest. It checks shape only and does not verify
// account identity, weight, current model availability or authority.
// validRuntimeCatalogPin проверяет форму хеша каталога, точную производную ревизию mcat_, stable
// provider key и необязательный хеш capability модели. Проверяет только форму, не account
// identity, вес, доступность модели и полномочия.
func validRuntimeCatalogPin(candidate entity.ProviderAccountCandidate) bool {
	if !validModelCatalogDigest(candidate.CatalogDigest) || candidate.CatalogRevision != "mcat_"+candidate.CatalogDigest || !validStableKey(candidate.ProviderDefinitionKey) || candidate.ModelCapabilityDigest != "" && !validModelCatalogDigest(candidate.ModelCapabilityDigest) {
		return false
	}
	return true
}

// legacyUnpinnedRuntimeCatalogCandidate recognizes an existing candidate with account/positive
// weight and empty catalog/provider/default-effort pins. It intentionally does not test
// ModelCapabilityDigest; only callers in non-input compatibility paths may use this predicate.
// legacyUnpinnedRuntimeCatalogCandidate распознаёт сохранённого кандидата с account, положительным
// весом и пустыми catalog, provider и default-effort привязками. ModelCapabilityDigest намеренно
// не проверяет; условие применяют только совместимые пути чтения прежнего состояния, не новый
// input.
func legacyUnpinnedRuntimeCatalogCandidate(candidate entity.ProviderAccountCandidate) bool {
	return candidate.AccountRef != "" && candidate.Weight > 0 && candidate.CatalogDigest == "" &&
		candidate.CatalogRevision == "" && candidate.ProviderDefinitionKey == "" && candidate.DefaultReasoningEffort == ""
}

// validateRuntimeCatalogCandidates delegates to the snapshot validator with account/definition
// share locks enabled, preserving input mode and overlay. The caller owns the transaction, actor
// authorization and publication; validated pins do not themselves reserve provider capacity.
// validateRuntimeCatalogCandidates вызывает проверку снимка с включёнными share locks account и
// definition, сохраняя input mode и overlay. Транзакция, авторизация и публикация принадлежат
// вызывающему; проверенные pins не резервируют ёмкость provider.
func validateRuntimeCatalogCandidates(ctx context.Context, tx pgx.Tx, current scope, provider, model, overlay string, candidates []entity.ProviderAccountCandidate, input bool) ([]entity.ProviderAccountCandidate, []string, error) {
	return validateRuntimeCatalogCandidatesSnapshot(ctx, tx, current, provider, model, overlay, candidates, input, true)
}

// validateRuntimeCatalogCandidatesSnapshot clones and sorts nonempty candidates by account ref,
// verifies account eligibility and exact input catalog pins or persisted model-specific
// digest/default, then validates overlay against every model and intersects reasoning efforts.
// Non-input legacy candidates acquire current pins; input rejects caller-assigned derived
// effort/digest. Account locks are optional for read snapshots; unknown account/model, stale pins,
// unavailable scans and invalid shape retain distinct errors. All partial results are discarded on
// failure; no credential material or capacity reservation is created.
// validateRuntimeCatalogCandidatesSnapshot копирует и сортирует непустых кандидатов по account
// ref, проверяет доступность account, точные входные catalog pins либо сохранённые model
// digest/default, затем overlay каждой модели и пересечение reasoning efforts. Прежние non-input
// кандидаты получают актуальные pins; input отклоняет назначенные клиентом производные
// effort/digest. Для снимков чтения блокировка необязательна; неизвестный account/model,
// устаревшие pins, сбой чтения и неверная форма сохраняют разные ошибки. Частичные результаты
// отбрасываются; значения credential и резерв ёмкости не создаются.
func validateRuntimeCatalogCandidatesSnapshot(ctx context.Context, tx pgx.Tx, current scope, provider, model, overlay string, candidates []entity.ProviderAccountCandidate, input, lock bool) ([]entity.ProviderAccountCandidate, []string, error) {
	if len(candidates) == 0 {
		return nil, nil, errs.ErrConflict
	}
	result := slices.Clone(candidates)
	slices.SortFunc(result, func(a, b entity.ProviderAccountCandidate) int { return strings.Compare(a.AccountRef, b.AccountRef) })
	efforts := []string{}
	for index := range result {
		candidate := &result[index]
		legacyUnpinned := !input && legacyUnpinnedRuntimeCatalogCandidate(*candidate)
		if !legacyUnpinned && (!validRuntimeCatalogPin(*candidate) || candidate.ProviderDefinitionKey != provider) || input && (candidate.DefaultReasoningEffort != "" || candidate.ModelCapabilityDigest != "") {
			return nil, nil, errs.ErrInvalid
		}
		var ref string
		accountQuery := queryRuntimeCatalogReadAccounts
		accountArgs := []any{pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_ref": candidate.AccountRef, "provider": provider}}
		if lock {
			accountQuery = queryRuntimeCatalogLockAccounts
			accountArgs = []any{current.organizationID, candidate.AccountRef, provider}
		}
		if err := tx.QueryRow(ctx, accountQuery, accountArgs...).Scan(&ref); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil, errs.ErrConflict
			}
			return nil, nil, errs.ErrUnavailable
		}
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidate.AccountRef)
		if err != nil {
			return nil, nil, err
		}
		if legacyUnpinned {
			candidate.ProviderDefinitionKey = provider
			candidate.CatalogRevision, candidate.CatalogDigest = catalog.Revision, catalog.Digest
		}
		// При публикации проверяется точный снимок каталога. Уже сохранённая
		// конфигурация с новым pin зависит только от выбранной модели: удаление
		// или изменение чужой модели не отзывает разрешённый запуск.
		if (input || candidate.ModelCapabilityDigest == "") && (catalog.Revision != candidate.CatalogRevision || catalog.Digest != candidate.CatalogDigest) {
			return nil, nil, errs.ErrVersionMismatch
		}
		found := false
		for _, capability := range catalog.Models {
			if capability.ID != model {
				continue
			}
			validDefault := len(capability.ReasoningEfforts) == 0 && capability.DefaultReasoningEffort == "" || slices.Contains(capability.ReasoningEfforts, capability.DefaultReasoningEffort)
			if !capability.Available || !slices.Contains(capability.EligibleProviderAccountRefs, candidate.AccountRef) || !validDefault {
				return nil, nil, errs.ErrConflict
			}
			modelDigest := modelcatalog.CapabilityDigest(provider, candidate.AccountRef, capability.ID, capability.ReasoningEfforts, capability.DefaultReasoningEffort, capability.IsDefault)
			if !input && candidate.ModelCapabilityDigest != "" && candidate.ModelCapabilityDigest != modelDigest {
				return nil, nil, errs.ErrVersionMismatch
			}
			if legacyUnpinned {
				candidate.DefaultReasoningEffort = capability.DefaultReasoningEffort
			}
			if !input && candidate.DefaultReasoningEffort != capability.DefaultReasoningEffort {
				return nil, nil, errs.ErrConflict
			}
			candidate.DefaultReasoningEffort = capability.DefaultReasoningEffort
			if input || legacyUnpinned {
				candidate.ModelCapabilityDigest = modelDigest
			}
			modelEfforts := append([]string{}, capability.ReasoningEfforts...)
			if len(runtimecontract.DiagnoseConfigOverlay(overlay, modelEfforts)) != 0 {
				return nil, nil, errs.ErrConflict
			}
			if index == 0 {
				efforts = modelEfforts
			} else {
				efforts = slices.DeleteFunc(efforts, func(effort string) bool { return !slices.Contains(capability.ReasoningEfforts, effort) })
			}
			found = true
			break
		}
		if !found {
			return nil, nil, errs.ErrConflict
		}
	}
	return result, efforts, nil
}

// readRuntimeCatalogConfiguration scans provider/model/account policy and overlay for the exact
// organization/agent/configuration, then strictly decodes candidates. Query or decode failure is
// Unavailable and may accompany partially scanned config; caller must discard it and separately
// authorize and validate pins.
// readRuntimeCatalogConfiguration читает provider, model, account policy и overlay точных
// организации, агента и конфигурации, затем строго декодирует кандидатов. Ошибка — Unavailable с
// возможной частичной конфигурацией, которую вызывающий отбрасывает; полномочия и pins проверяются
// отдельно.
func readRuntimeCatalogConfiguration(ctx context.Context, tx pgx.Tx, organizationID, agentRef, configID string) (entity.AgentRuntimeConfiguration, string, error) {
	var config entity.AgentRuntimeConfiguration
	var raw []byte
	var overlay string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogReadConfiguration, organizationID, agentRef, configID).Scan(&config.Provider, &config.Model, &raw, &overlay); err != nil {
		return config, "", errs.ErrUnavailable
	}
	if decodeStrict(raw, &config.ProviderPolicy.AccountCandidates) != nil {
		return config, "", errs.ErrUnavailable
	}
	return config, overlay, nil
}

// captureRuntimeCatalogPins serves server-owned bootstrap/reconcile only: nil candidates load the
// preferred pool, while an empty or over-128 supplied list conflicts. It reads catalogs, removes
// unavailable model candidates, assigns current provider/catalog pins, then runs locked input
// validation to derive default effort/model digest. User publication must supply its own expected
// snapshot; capture is not a silent replacement of an owner selection.
// captureRuntimeCatalogPins служит только серверным bootstrap и reconcile: nil загружает
// предпочтительный пул, пустой либо превышающий 128 список вызывает Conflict. Читает каталоги,
// исключает кандидатов без доступной модели, назначает provider/catalog pins, затем проверкой с
// блокировками выводит default effort/model digest. Пользовательская публикация передаёт
// собственный ожидаемый снимок; capture не заменяет незаметно выбор владельца.
func captureRuntimeCatalogPins(ctx context.Context, tx pgx.Tx, current scope, provider, model string, candidates []entity.ProviderAccountCandidate) ([]entity.ProviderAccountCandidate, error) {
	if candidates == nil {
		accounts, err := bootstrapUnpinnedCatalogCandidates(ctx, tx, current.organizationID, provider)
		if err != nil {
			return nil, err
		}
		candidates = accounts
	}
	if len(candidates) == 0 || len(candidates) > 128 {
		return nil, errs.ErrConflict
	}
	eligible := make([]entity.ProviderAccountCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidate.AccountRef)
		if err != nil {
			return nil, err
		}
		available := false
		for _, capability := range catalog.Models {
			if capability.ID == model && capability.Available {
				available = true
				break
			}
		}
		if !available {
			continue
		}
		candidate.ProviderDefinitionKey = provider
		candidate.CatalogRevision, candidate.CatalogDigest = catalog.Revision, catalog.Digest
		candidate.DefaultReasoningEffort = ""
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return nil, errs.ErrConflict
	}
	result, _, err := validateRuntimeCatalogCandidates(ctx, tx, current, provider, model, "", eligible, true)
	return result, err
}

// firstRunProviderDefaultModel selects one common available account-eligible default model across
// all candidate catalogs, each requiring READY status. Read/status failures, missing or
// contradictory defaults and an empty list become Conflict. Only the untouched provider-free
// bootstrap caller may apply this choice; an existing owner-selected model is not replaced here.
// firstRunProviderDefaultModel выбирает одну общую доступную default модель, допустимую для
// accounts всех кандидатов, требуя READY каждого каталога. Ошибки чтения и статуса, отсутствие
// либо противоречие defaults и пустой список дают Conflict. Применять выбор может только
// неизменённый bootstrap без provider; прежняя модель владельца здесь не заменяется.
func firstRunProviderDefaultModel(ctx context.Context, tx pgx.Tx, current scope, provider string, candidates []entity.ProviderAccountCandidate) (string, error) {
	selected := ""
	for _, candidate := range candidates {
		catalog, err := readModelCatalogTx(ctx, tx, current, provider, candidate.AccountRef)
		if err != nil || catalog.Status == nil || catalog.Status.State != "READY" {
			return "", errs.ErrConflict
		}
		accountDefault := ""
		for _, capability := range catalog.Models {
			if !capability.IsDefault || !capability.Available || !slices.Contains(capability.EligibleProviderAccountRefs, candidate.AccountRef) {
				continue
			}
			if accountDefault != "" && accountDefault != capability.ID {
				return "", errs.ErrConflict
			}
			accountDefault = capability.ID
		}
		if accountDefault == "" || selected != "" && selected != accountDefault {
			return "", errs.ErrConflict
		}
		selected = accountDefault
	}
	if selected == "" {
		return "", errs.ErrConflict
	}
	return selected, nil
}
