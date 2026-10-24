package platform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	accessservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/access"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/modelcatalog"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// ListProviderDefinitions opens an authorized organization read, pages definitions by stable key
// and derives authorization methods from capability JSON. It attaches current model catalogs,
// selects an unambiguous eligible default and computes readiness blockers before commit. Ready
// describes catalog eligibility, not a provider probe; the shared read helper leaves isolation at
// the server default.
// ListProviderDefinitions открывает авторизованное чтение организации, получает страницу
// definitions по stable key и выводит методы authorization из capability JSON. Добавляет текущие
// model catalogs, выбирает однозначную доступную default model, вычисляет blockers до commit.
// Ready отражает catalog eligibility, не проверку провайдера; общий read helper оставляет изоляцию
// по умолчанию сервера.
func (repository *Repository) ListProviderDefinitions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ProviderDefinition, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "organization.view", func(current scope) entity.AccessScope {
		return organizationTarget(current.organizationRef)
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!validStableKey(cursor) || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListProviderDefinitions, pgx.StrictNamedArgs{
		"query": strings.TrimSpace(filter.Query), "cursor_key": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ProviderDefinition, 0, limit+1)
	for rows.Next() {
		var item entity.ProviderDefinition
		var capabilities []byte
		if err := rows.Scan(&item.Key, &item.Name, &capabilities, &item.Available); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		var capabilityMap map[string]any
		if json.Unmarshal(capabilities, &capabilityMap) != nil {
			return nil, "", errs.ErrUnavailable
		}
		item.Description = item.Name
		item.AuthorizationMethods = []string{"API_KEY"}
		if enabled, _ := capabilityMap["deviceAuthorization"].(bool); enabled {
			item.AuthorizationMethods = append([]string{"DEVICE_CODE"}, item.AuthorizationMethods...)
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	rows.Close()
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = items[len(items)-1].Key
	}
	for index := range items {
		item := &items[index]
		catalog, err := readModelCatalogTx(ctx, tx, current, item.Key, "")
		if err != nil {
			return nil, "", err
		}
		item.Models = catalog.Models
		observedDefault, defaultAmbiguous := "", false
		for _, model := range catalog.Models {
			item.ModelIDs = append(item.ModelIDs, model.ID)
			if model.Available {
				item.Ready = true
				if model.ID == repository.defaultRuntimeModel {
					item.DefaultModelID = model.ID
				}
				if model.IsDefault {
					if observedDefault != "" && observedDefault != model.ID {
						defaultAmbiguous = true
					}
					observedDefault = model.ID
				}
			}
		}
		if item.DefaultModelID == "" && observedDefault != "" && !defaultAmbiguous {
			item.DefaultModelID = observedDefault
		}
		if !item.Available {
			item.Ready = false
			item.ReadinessBlockers = []string{"PROVIDER_DISABLED"}
		} else if !item.Ready {
			item.ReadinessBlockers = []string{"ELIGIBLE_PROVIDER_MODEL_MISSING"}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrConflict
	}
	return items, next, nil
}

// modelCapabilityCursor stores the version/filter digest, catalog revision/digest and
// provider/model position for model pages. This unsigned cursor binds a read position and content
// pins without granting access to an account or model.
// modelCapabilityCursor хранит версию/дайджест фильтра, catalog revision/digest и provider/model
// position страницы. Этот неподписанный курсор связывает позицию чтения и content pins без
// предоставления доступа к аккаунту или модели.
type modelCapabilityCursor struct {
	CatalogRevision       string `json:"r"`
	CatalogDigest         string `json:"d"`
	Version               int    `json:"v"`
	Filter                string `json:"f"`
	ProviderDefinitionKey string `json:"p"`
	Model                 string `json:"m"`
}

// ListModelCapabilities requires organization view, validates definition/account/query and an
// actor-bound cursor, then reads the catalog and enforces optional expected content pins. It
// filters names, counts all matches and pages after an exact existing model position;
// stale/missing positions are Invalid and commit failure is Conflict. It does not execute provider
// discovery.
// ListModelCapabilities требует organization view, проверяет definition/account/query и связанный
// с субъектом курсор, читает каталог и проверяет необязательные ожидаемые content pins. Фильтрует
// имена, считает все совпадения, строит страницу после существующей точной модели;
// устаревшая/отсутствующая позиция даёт Invalid, отказ commit — Conflict. Provider discovery не
// выполняет.
func (repository *Repository) ListModelCapabilities(ctx context.Context, principal value.Principal, definitionKey, accountRef string, filter query.Filter) (entity.ModelCatalog, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "organization.view", func(current scope) entity.AccessScope {
		return organizationTarget(current.organizationRef)
	})
	if err != nil {
		return entity.ModelCatalog{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if definitionKey != "" && !validStableKey(definitionKey) || accountRef != "" && (!strings.HasPrefix(accountRef, "pacc_") || len(accountRef) > 96) || len([]rune(filter.Query)) > 200 {
		return entity.ModelCatalog{}, errs.ErrInvalid
	}
	cursor, err := decodeModelCapabilityCursor(filter.Page.Token, definitionKey, accountRef, modelCatalogActorFilter(current, filter.Query))
	if err != nil {
		return entity.ModelCatalog{}, err
	}
	catalog, err := readModelCatalogTx(ctx, tx, current, definitionKey, accountRef)
	if err != nil {
		return entity.ModelCatalog{}, err
	}
	result, revision, digest := catalog.Models, catalog.Revision, catalog.Digest
	if (filter.ExpectedCatalogRevision != "" || filter.ExpectedCatalogDigest != "") && (filter.ExpectedCatalogRevision != revision || filter.ExpectedCatalogDigest != digest) {
		return entity.ModelCatalog{}, errs.ErrInvalid
	}
	if cursor.Model != "" && (cursor.CatalogDigest != digest || cursor.CatalogRevision != revision) {
		return entity.ModelCatalog{}, errs.ErrInvalid
	}
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	filtered := make([]entity.ModelCapability, 0, len(result))
	for _, item := range result {
		if needle == "" || strings.Contains(strings.ToLower(item.ProviderDefinitionKey+" "+item.ID), needle) {
			filtered = append(filtered, item)
		}
	}
	total := int64(len(filtered))
	start := 0
	if cursor.Model != "" {
		for index, item := range filtered {
			if item.ProviderDefinitionKey == cursor.ProviderDefinitionKey && item.ID == cursor.Model {
				start = index + 1
				break
			}
		}
		if start == 0 {
			return entity.ModelCatalog{}, errs.ErrInvalid
		}
	}
	limit := int(boundedPage(filter.Page))
	end := min(start+limit, len(filtered))
	items := filtered[start:end]
	next := ""
	if end < len(filtered) {
		last := items[len(items)-1]
		next = encodeModelCapabilityCursor(last.ProviderDefinitionKey, last.ID, definitionKey, accountRef, modelCatalogActorFilter(current, filter.Query), revision, digest)
		if next == filter.Page.Token {
			return entity.ModelCatalog{}, errs.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ModelCatalog{}, errs.ErrConflict
	}
	return entity.ModelCatalog{Models: items, Total: total, NextPageToken: next, Revision: revision, Digest: digest, Status: catalog.Status}, nil
}

// readModelCatalogTx reads bounded strict catalog records in the caller transaction and merges
// models by provider/model key. Conflicting reasoning capabilities remove all eligible accounts
// for that model; otherwise any eligible account clears blockers. It sorts models/blockers and
// computes content-only revision/digest from source parts, excluding observed eligibility/time.
// Missing requested-account status is NotFound and corrupt/read failures are Unavailable.
// readModelCatalogTx читает ограниченные строгие записи каталога в транзакции вызывающего и
// объединяет модели по provider/model key. Противоречивые reasoning capabilities убирают все
// eligible accounts модели; иначе любой доступный аккаунт очищает blockers. Сортирует
// модели/blockers, вычисляет content-only revision/digest из source parts без observed
// eligibility/time. Отсутствие статуса запрошенного аккаунта даёт NotFound, повреждение/отказ
// чтения — Unavailable.
func readModelCatalogTx(ctx context.Context, tx pgx.Tx, current scope, definitionKey, accountRef string) (entity.ModelCatalog, error) {
	rows, err := tx.Query(ctx, queryMVPListModelCapabilities, pgx.StrictNamedArgs{"organization_id": current.organizationID, "provider_definition_key": definitionKey, "provider_account_ref": accountRef})
	if err != nil {
		return entity.ModelCatalog{}, errs.ErrUnavailable
	}
	defer rows.Close()
	result := entity.ModelCatalog{Models: []entity.ModelCapability{}}
	sources := []string{}
	positions := map[string]int{}
	conflicts := map[string]bool{}
	for rows.Next() {
		var key, ref, blocker, source string
		var raw []byte
		var status entity.ModelCatalogStatus
		if rows.Scan(&key, &ref, &raw, &blocker, &status.State, &status.ObservedAt, &status.ExpiresAt, &status.Source, &status.Failure, &source) != nil {
			return entity.ModelCatalog{}, errs.ErrUnavailable
		}
		sources = append(sources, source)
		if len(sources) > 4096 || len(raw) > 131072 || len(source) > 1048576 {
			return entity.ModelCatalog{}, errs.ErrUnavailable
		}
		if accountRef != "" {
			result.Status = &status
		}
		var records []platformrepo.ProviderModelCatalogRecord
		if decodeStrict(raw, &records) != nil || len(records) > 128 {
			return entity.ModelCatalog{}, errs.ErrUnavailable
		}
		for _, record := range records {
			modelKey := key + "\x00" + record.ID
			position, exists := positions[modelKey]
			if !exists {
				position = len(result.Models)
				positions[modelKey] = position
				result.Models = append(result.Models, entity.ModelCapability{ID: record.ID, ProviderDefinitionKey: key, DefaultReasoningEffort: record.DefaultReasoningEffort, ReasoningEfforts: append([]string{}, record.ReasoningEfforts...), IsDefault: record.IsDefault})
			}
			item := &result.Models[position]
			if item.DefaultReasoningEffort != record.DefaultReasoningEffort || !slices.Equal(item.ReasoningEfforts, record.ReasoningEfforts) {
				conflicts[modelKey] = true
			}
			item.IsDefault = item.IsDefault || record.IsDefault
			if blocker == "" {
				item.EligibleProviderAccountRefs = append(item.EligibleProviderAccountRefs, ref)
			} else if !slices.Contains(item.ReadinessBlockers, blocker) {
				item.ReadinessBlockers = append(item.ReadinessBlockers, blocker)
			}
		}
	}
	if rows.Err() != nil {
		return entity.ModelCatalog{}, errs.ErrUnavailable
	}
	if accountRef != "" && result.Status == nil {
		return entity.ModelCatalog{}, errs.ErrNotFound
	}
	for index := range result.Models {
		item := &result.Models[index]
		if conflicts[item.ProviderDefinitionKey+"\x00"+item.ID] {
			item.EligibleProviderAccountRefs = nil
			item.ReadinessBlockers = []string{"MODEL_CATALOG_CAPABILITIES_CONFLICT"}
		}
		item.Available = len(item.EligibleProviderAccountRefs) > 0
		if item.Available {
			item.ReadinessBlockers = nil
		}
		sort.Strings(item.ReadinessBlockers)
	}
	slices.SortFunc(result.Models, func(a, b entity.ModelCapability) int {
		if a.ProviderDefinitionKey != b.ProviderDefinitionKey {
			return strings.Compare(a.ProviderDefinitionKey, b.ProviderDefinitionKey)
		}
		return strings.Compare(a.ID, b.ID)
	})
	// Content pin не включает наблюдаемое время, expiry и результат текущей eligibility.
	digest, err := modelcatalog.Digest(nil, sources...)
	if err != nil {
		return entity.ModelCatalog{}, errs.ErrUnavailable
	}
	result.Digest = digest
	result.Revision = "mcat_" + digest
	result.Total = int64(len(result.Models))
	return result, nil
}

// modelCapabilityFilterDigest hashes definition/account/query separated by NUL and returns twelve
// SHA-256 bytes as raw URL base64. The query component can already contain actor scope; the digest
// binds filters without being a signature or permission.
// modelCapabilityFilterDigest хеширует definition/account/query, разделённые NUL, возвращает
// двенадцать байт SHA-256 в raw URL base64. Компонент query уже может содержать область субъекта;
// дайджест связывает фильтры без подписи и полномочий.
func modelCapabilityFilterDigest(definitionKey, accountRef, queryValue string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{definitionKey, accountRef, strings.TrimSpace(queryValue)}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// encodeModelCapabilityCursor encodes a version-two model position together with filter and
// catalog content pins as JSON/raw URL base64, preserving the ignored marshal error for this fixed
// serializable shape.
// encodeModelCapabilityCursor кодирует позицию модели версии два вместе с фильтром и catalog
// content pins в JSON/raw URL base64, сохраняя игнорирование ошибки marshal для фиксированной
// сериализуемой формы.
func encodeModelCapabilityCursor(definitionKey, model, filterDefinitionKey, accountRef, queryValue, revision, digest string) string {
	raw, _ := json.Marshal(modelCapabilityCursor{Version: 2, CatalogRevision: revision, CatalogDigest: digest,
		Filter: modelCapabilityFilterDigest(filterDefinitionKey, accountRef, queryValue), ProviderDefinitionKey: definitionKey, Model: model})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeModelCapabilityCursor accepts a blank first page or validates base64/JSON within512
// encoded and decoded bytes, version/filter match and nonempty provider/model. Catalog pin
// freshness and position existence are checked later against the current authorized catalog rather
// than trusted from this token.
// decodeModelCapabilityCursor принимает пустую первую страницу либо проверяет base64/JSON в
// пределах512 кодированных и декодированных байт, версию/фильтр и непустые provider/model.
// Свежесть catalog pins и существование позиции проверяются позже по текущему авторизованному
// каталогу, не принимаются на веру из токена.
func decodeModelCapabilityCursor(token, definitionKey, accountRef, queryValue string) (modelCapabilityCursor, error) {
	if strings.TrimSpace(token) == "" {
		return modelCapabilityCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(token) > 512 || len(raw) > 512 {
		return modelCapabilityCursor{}, errs.ErrInvalid
	}
	var cursor modelCapabilityCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 2 || cursor.Filter != modelCapabilityFilterDigest(definitionKey, accountRef, queryValue) ||
		cursor.ProviderDefinitionKey == "" || cursor.Model == "" {
		return modelCapabilityCursor{}, errs.ErrInvalid
	}
	return cursor, nil
}

// modelCatalogActorFilter joins organization, actor, authority-project and trimmed query with NUL
// to bind model pagination to the resolved read scope. It does not itself evaluate resource
// access.
// modelCatalogActorFilter объединяет organization, actor, authority-project и очищенный query
// через NUL для связи model pagination с разрешённой областью чтения. Сам не проверяет доступ к
// ресурсу.
func modelCatalogActorFilter(current scope, queryValue string) string {
	return strings.Join([]string{current.organizationID, current.actorID, current.authorityProjectID, strings.TrimSpace(queryValue)}, "\x00")
}

// ListProviderAccounts uses the provider-usage read path, resolves the requested usage context and
// binds pagination to scope plus current usage-account digests. It pages account rows, loads
// models only for selected refs, checks versions, computes usage and authorizes/redacts
// actions/financial data before commit. It preserves the caller read isolation and returns no
// partial page on failure.
// ListProviderAccounts использует provider-usage read path, разрешает usage context и связывает
// пагинацию с областью и текущими дайджестами usage accounts. Получает страницу аккаунтов,
// загружает модели лишь выбранных refs, проверяет версии, вычисляет usage, авторизует действия и
// скрывает финансовые данные до commit. Сохраняет изоляцию чтения вызывающего, при ошибке не
// возвращает частичную страницу.
func (repository *Repository) ListProviderAccounts(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ProviderAccount, string, []string, error) {
	current, tx, err := repository.providerUsageRead(ctx, principal, func(current scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"}
	})
	if err != nil {
		return nil, "", nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	usageContext, err := repository.resolveProviderUsageContext(ctx, tx, current, filter.ProviderUsage)
	if err != nil {
		return nil, "", nil, err
	}
	usageAccounts, err := readProviderUsageAccounts(ctx, tx, current.organizationID, nil, false)
	if err != nil {
		return nil, "", nil, err
	}
	sourceParts := []string{catalogScope(current, "provider-usage", filter), digestBytes(asJSON(usageContext))}
	for _, account := range usageAccounts {
		sourceParts = append(sourceParts, providerUsageAccountDigest(account))
	}
	usageSource := digestBytes(asJSON(sourceParts))
	cursorTime, cursorRef, err := decodeProviderUsageCursor(usageSource, filter.Page.Token)
	if err != nil {
		return nil, "", nil, err
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListProviderAccounts, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "query": strings.TrimSpace(filter.Query),
		"state": strings.TrimSpace(filter.State), "definition_key": strings.TrimSpace(filter.DefinitionKey),
		"cursor_time": cursorTime, "cursor_ref": cursorRef,
		"page_size": limit + 1,
	})
	if err != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ProviderAccount, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanProviderAccount(rows)
		if scanErr != nil {
			return nil, "", nil, scanErr
		}
		items = append(items, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", nil, errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeProviderUsageCursor(usageSource, last.UpdatedAt, last.Ref)
	}
	refs := []string{}
	for _, item := range items {
		refs = append(refs, item.Ref)
	}
	if len(refs) > 0 {
		withModels, readErr := readProviderUsageAccounts(ctx, tx, current.organizationID, refs, true)
		if readErr != nil {
			return nil, "", nil, readErr
		}
		modelsByRef := map[string]providerUsageAccount{}
		for _, account := range withModels {
			modelsByRef[account.Ref] = account
		}
		byRef := map[string]providerUsageAccount{}
		for _, account := range usageAccounts {
			account.Models = modelsByRef[account.Ref].Models
			byRef[account.Ref] = account
		}
		for index := range items {
			account, ok := byRef[items[index].Ref]
			if !ok || account.Version != items[index].Version {
				return nil, "", nil, errs.ErrUnavailable
			}
			items[index].Usage, err = providerAccountUsage(account, usageContext)
			if err != nil {
				return nil, "", nil, err
			}
		}
	}
	items, collectionActions, err := repository.authorizeProviderAccountActions(ctx, tx, current, items)
	if err != nil {
		return nil, "", nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", nil, errs.ErrConflict
	}
	return items, next, collectionActions, nil
}

// GetProviderAccount delegates a single-account read with no explicit usage context to
// GetProviderAccountWithUsage; exact resource access and all projections remain in that method.
// GetProviderAccount делегирует чтение аккаунта без явного usage context методу
// GetProviderAccountWithUsage; точный доступ и все проекции остаются там.
func (repository *Repository) GetProviderAccount(ctx context.Context, principal value.Principal, ref string) (entity.ProviderAccount, error) {
	return repository.GetProviderAccountWithUsage(ctx, principal, ref, nil)
}

// GetProviderAccountWithUsage opens the exact account provider-usage read, scans public/lifecycle
// data and requires one matching-version usage account with models. It projects usage and current
// authorized/redacted actions before commit; inconsistent counts/versions fail Unavailable and
// commit failure maps to Conflict.
// GetProviderAccountWithUsage открывает provider-usage чтение точного аккаунта, читает
// публичные/lifecycle данные и требует один usage account с моделями и совпадающей версией. До
// commit проецирует usage и текущие авторизованные/скрытые действия; несовпадение числа/версии
// даёт Unavailable, отказ commit — Conflict.
func (repository *Repository) GetProviderAccountWithUsage(ctx context.Context, principal value.Principal, ref string, usage *entity.ProviderAccountUsageContext) (entity.ProviderAccount, error) {
	current, tx, err := repository.providerUsageRead(ctx, principal, func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: ref}
	})
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := scanProviderAccount(tx.QueryRow(ctx, queryMVPGetProviderAccount, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "account_ref": ref,
	}))
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	usageContext, err := repository.resolveProviderUsageContext(ctx, tx, current, usage)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	accounts, err := readProviderUsageAccounts(ctx, tx, current.organizationID, []string{ref}, true)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if len(accounts) != 1 || accounts[0].Version != item.Version {
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	item.Usage, err = providerAccountUsage(accounts[0], usageContext)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	items, _, err := repository.authorizeProviderAccountActions(ctx, tx, current, []entity.ProviderAccount{item})
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProviderAccount{}, errs.ErrConflict
	}
	return items[0], nil
}

// scanProviderAccount decodes account/authorization metadata and optional financial observation
// with a required receipt time. It validates state/enabled consistency and concurrency1–256,
// derives Ready/status and removes authorization metadata for deleting/deleted accounts. NoRows
// becomes NotFound; corrupt projections and scan errors become Unavailable, with financial
// visibility filtered separately.
// scanProviderAccount декодирует account/authorization metadata и необязательное финансовое
// наблюдение с обязательным временем получения. Проверяет согласованность state/enabled и
// concurrency1–256, вычисляет Ready/status, убирает authorization metadata удаляемых/удалённых
// аккаунтов. NoRows даёт NotFound, повреждение и scan errors — Unavailable; финансовая видимость
// фильтруется отдельно.
func scanProviderAccount(row rowScanner) (entity.ProviderAccount, error) {
	var item entity.ProviderAccount
	var authorization entity.ProviderAuthorization
	var financial []byte
	var receivedAt *time.Time
	var expiresAt *time.Time
	if err := row.Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.ExternalAccountMasked,
		&item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt,
		&authorization.Ref, &authorization.Method, &authorization.State, &authorization.VerificationURI,
		&authorization.UserCode, &expiresAt, &authorization.SafeFailureCode, &authorization.MaterializerAttemptRef, &item.MaximumConcurrentExecutions, &financial, &receivedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.ProviderAccount{}, errs.ErrNotFound
		}
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	if len(financial) > 0 {
		if receivedAt == nil {
			return entity.ProviderAccount{}, errs.ErrUnavailable
		}
		var summary entity.ProviderFinancialSummary
		observation, err := runtimecontract.ParseProviderFinancialObservation(financial)
		if err != nil {
			return entity.ProviderAccount{}, errs.ErrUnavailable
		}
		summary.Observation = observation
		summary.ReceivedAt = *receivedAt
		summary.Version = item.Version
		item.FinancialSummary = &summary
	}
	if authorization.Ref != "" {
		authorization.ExpiresAt = expiresAt
		item.Authorization = &authorization
	}
	if !validProviderAccountLifecycle(item.State, item.Enabled) || item.MaximumConcurrentExecutions < 1 || item.MaximumConcurrentExecutions > 256 {
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	item.Ready = item.Enabled && item.State == "AUTHORIZED"
	if item.State == "DELETING" || item.State == "DELETED" {
		item.Authorization = nil
	}
	item.SafeStatusReason = providerAccountStatusReason(item)
	return item, nil
}

// providerAccountStatusReason maps the closed account lifecycle to public status reasons, allowing
// only safe known authorization failures during reauthorization and distinguishing pending device
// authorization from missing credential configuration. Unknown states receive
// ACCOUNT_STATE_UNKNOWN.
// providerAccountStatusReason сопоставляет закрытый lifecycle аккаунта публичным причинам,
// допуская лишь известные безопасные authorization failures при reauthorization, различает
// ожидающую device authorization и отсутствие credential configuration. Неизвестное состояние даёт
// ACCOUNT_STATE_UNKNOWN.
func providerAccountStatusReason(item entity.ProviderAccount) string {
	switch item.State {
	case "AUTHORIZED":
		return "AUTHORIZED"
	case "DISABLED":
		return "ACCOUNT_DISABLED"
	case "REVOKED":
		return "ACCOUNT_REVOKED"
	case "DELETING":
		return "ACCOUNT_DELETING"
	case "DELETED":
		return "ACCOUNT_DELETED"
	case "REAUTHORIZATION_REQUIRED":
		if item.Authorization != nil && safeProviderAuthorizationFailure(item.Authorization.SafeFailureCode) {
			return item.Authorization.SafeFailureCode
		}
		return "REAUTHORIZATION_REQUIRED"
	case "PENDING_AUTHORIZATION":
		if item.Authorization != nil && item.Authorization.State == "PENDING" {
			return "DEVICE_AUTHORIZATION_PENDING"
		}
		return "CREDENTIAL_CONFIGURATION_REQUIRED"
	default:
		return "ACCOUNT_STATE_UNKNOWN"
	}
}

// safeProviderAuthorizationFailure accepts only device authorization expiry/failure and credential
// materialization failure codes for public status projection; arbitrary provider/driver text is
// rejected.
// safeProviderAuthorizationFailure принимает лишь коды истечения/отказа device authorization и
// отказа credential materialization для публичного статуса; произвольный текст провайдера/драйвера
// отклоняется.
func safeProviderAuthorizationFailure(value string) bool {
	switch value {
	case "DEVICE_AUTHORIZATION_EXPIRED", "DEVICE_AUTHORIZATION_FAILED", "CREDENTIAL_MATERIALIZATION_FAILED":
		return true
	default:
		return false
	}
}

// validProviderAccountLifecycle requires AUTHORIZED accounts to be enabled,
// pending/revoked/disabled/deleting/deleted accounts disabled, and permits either enabled flag
// only for REAUTHORIZATION_REQUIRED. Unknown states fail the consistency check.
// validProviderAccountLifecycle требует enabled у AUTHORIZED, disabled у
// pending/revoked/disabled/deleting/deleted, допускает оба enabled лишь для
// REAUTHORIZATION_REQUIRED. Неизвестное состояние не проходит проверку согласованности.
func validProviderAccountLifecycle(state string, enabled bool) bool {
	switch state {
	case "AUTHORIZED":
		return enabled
	case "PENDING_AUTHORIZATION", "REVOKED", "DISABLED", "DELETING", "DELETED":
		return !enabled
	case "REAUTHORIZATION_REQUIRED":
		return true
	default:
		return false
	}
}

// providerAccountActions derives ordered UI hints from supplied
// manage/authorize/revoke/concurrency permissions and hydrated lifecycle metadata.
// Deleting/deleted accounts stop at OPEN plus eligible failed-deletion retry; credential refresh,
// testing and enable/disable/revoke remain state-specific. It does not perform those actions or
// establish the permissions itself.
// providerAccountActions вычисляет упорядоченные UI подсказки по
// manage/authorize/revoke/concurrency permissions и загруженным lifecycle metadata. У
// deleting/deleted остаются OPEN и возможный повтор неудачного удаления; credential refresh, test
// и enable/disable/revoke зависят от состояния. Не исполняет действия и не устанавливает
// полномочия самостоятельно.
func providerAccountActions(item entity.ProviderAccount, canManage, canAuthorize, canRevoke, canEditConcurrency bool) []string {
	actions := []string{"OPEN"}
	if item.State == "DELETING" || item.State == "DELETED" {
		if item.State == "DELETING" && item.Deletion != nil && item.Deletion.State == "FAILED" && canRevoke {
			actions = append(actions, "DELETE")
		}
		return actions
	}
	if canEditConcurrency {
		actions = append(actions, "EDIT")
	}
	if canRevoke {
		actions = append(actions, "DELETE")
	}
	if item.State == "PENDING_AUTHORIZATION" {
		if canAuthorize && item.Authorization == nil {
			actions = append(actions, "CONFIGURE_CREDENTIAL")
		}
		if canAuthorize && item.Authorization != nil && item.Authorization.State == "PENDING" {
			actions = append(actions, "REFRESH_AUTHORIZATION")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		return actions
	}
	switch item.State {
	case "AUTHORIZED":
		if canAuthorize && item.Authorization != nil && item.Authorization.Method == "DEVICE_CODE" {
			actions = append(actions, "CONFIGURE_CREDENTIAL")
			if item.Verification == nil || item.Verification.State != "PENDING" {
				actions = append(actions, "REFRESH_AUTHORIZATION")
			}
		}
		if canManage {
			actions = append(actions, "TEST")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		if canManage {
			actions = append(actions, "DISABLE")
		}
	case "DISABLED":
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
		if canManage {
			actions = append(actions, "ENABLE")
		}
	case "REAUTHORIZATION_REQUIRED":
		if canAuthorize {
			actions = append(actions, "CONFIGURE_CREDENTIAL")
		}
		if canRevoke {
			actions = append(actions, "REVOKE")
		}
	}
	return actions
}

// authorizeProviderAccountActions hydrates lifecycle projections, resolves the current access
// subject/bindings and financial eligibility, then evaluates collection and exact-account
// permissions at one application timestamp. It strips ineligible financial summaries and
// recomputes action hints; concurrency editing additionally requires owner/admin role. The passed
// slice is mutated and callers discard partial changes on error.
// authorizeProviderAccountActions добавляет lifecycle проекции, разрешает текущего access
// subject/bindings и financial eligibility, затем проверяет collection и exact-account permissions
// на одном application timestamp. Убирает недоступные financial summaries, пересчитывает действия;
// редактирование concurrency дополнительно требует owner/admin. Меняет переданный срез, вызывающий
// отбрасывает частичные изменения при ошибке.
func (repository *Repository) authorizeProviderAccountActions(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	items []entity.ProviderAccount,
) ([]entity.ProviderAccount, []string, error) {
	if err := repository.hydrateProviderAccountLifecycle(ctx, tx, current, items); err != nil {
		return nil, nil, err
	}
	subject, err := repository.resolveAccessSubject(ctx, tx, current.organizationID, current.actorRef)
	if err != nil {
		return nil, nil, err
	}
	bindings, err := repository.loadAccessBindings(ctx, tx, current.organizationID, subject)
	if err != nil {
		return nil, nil, err
	}
	financialRefs, allFinancial, err := repository.financialEligibility(ctx, tx, current)
	if err != nil {
		return nil, nil, err
	}
	at := time.Now().UTC()
	collectionTarget := providerAccountCollectionTarget()
	collectionActions := []string{}
	if accessservice.Evaluate(subject.AccessSubject, "provider.account.manage", collectionTarget, "", bindings, at).Allowed {
		collectionActions = append(collectionActions, "CREATE_CONNECTION")
	}
	for index := range items {
		target := entity.AccessScope{
			Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: items[index].Ref,
		}
		items[index].FinancialEligible = (allFinancial || financialRefs[items[index].Ref]) && accessservice.Evaluate(subject.AccessSubject, "provider.account.view", target, "", bindings, at).Allowed
		if !items[index].FinancialEligible {
			items[index].FinancialSummary = nil
		}
		canManage := accessservice.Evaluate(subject.AccessSubject, "provider.account.manage", target, "", bindings, at).Allowed
		canAuthorize := accessservice.Evaluate(subject.AccessSubject, "provider.account.authorize", target, "", bindings, at).Allowed
		canRevoke := accessservice.Evaluate(subject.AccessSubject, "provider.account.revoke", target, "", bindings, at).Allowed
		canEditConcurrency := canManage && (current.role == "OWNER" || current.role == "ADMINISTRATOR")
		items[index].NextActions = providerAccountActions(items[index], canManage, canAuthorize, canRevoke, canEditConcurrency)
	}
	return items, collectionActions, nil
}

// providerAccountCollectionTarget returns the provider-account RESOURCE_KIND access target for
// collection-level management checks without embedding a caller-controlled account reference.
// providerAccountCollectionTarget возвращает RESOURCE_KIND цель аккаунтов провайдера для проверки
// управления коллекцией без передачи управляемой вызывающим ссылки аккаунта.
func providerAccountCollectionTarget() entity.AccessScope {
	return entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"}
}

// ListRoleImageRecipeRevisions opens an authorized project-view read for the recipe and
// additionally resolves exact PROJECT role-image scope before delegating revision paging. The
// delegated reader owns successful commit; the wrapper retains deferred rollback on all exits.
// ListRoleImageRecipeRevisions открывает авторизованное project-view чтение recipe, дополнительно
// разрешает точную PROJECT область role-image до делегирования страницы ревизий. Успешный commit
// принадлежит делегированному reader; wrapper сохраняет deferred rollback при любом выходе.
func (repository *Repository) ListRoleImageRecipeRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RoleImageRecipeRevision, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "project.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ROLE_IMAGE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, filter.ResourceRef, "", "PROJECT"); err != nil {
		return nil, "", err
	}
	return repository.listRoleImageRevisions(ctx, tx, current, filter)
}

// listRoleImageRevisions validates a version cursor/nonempty recipe ref and pages immutable
// recipe/build/promotion metadata in the supplied transaction with one lookahead row. It trims to
// the requested size and uses the last revision as continuation; iteration or commit failure
// discards results as Unavailable. Exact access is owned by callers.
// listRoleImageRevisions проверяет version cursor/непустой recipe ref, читает страницу immutable
// recipe/build/promotion metadata в переданной транзакции с дополнительной строкой. Обрезает до
// размера и использует последнюю revision как продолжение; отказ итерации/commit отбрасывает
// результат с Unavailable. Точный доступ принадлежит вызывающим.
func (repository *Repository) listRoleImageRevisions(ctx context.Context, tx pgx.Tx, current scope, filter query.Filter) ([]entity.RoleImageRecipeRevision, string, error) {
	before, err := versionCursor(filter.Page.Token)
	if err != nil || strings.TrimSpace(filter.ResourceRef) == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListRoleImageRevisions, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "recipe_ref": filter.ResourceRef,
		"before_revision": before, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RoleImageRecipeRevision, 0, limit+1)
	for rows.Next() {
		var item entity.RoleImageRecipeRevision
		if err := rows.Scan(&item.Ref, &item.RecipeRef, &item.Revision, &item.RecipeVersion,
			&item.RecipeGeneration, &item.SpecSHA256, &item.ImageArtifactRef, &item.ProvenanceSHA256,
			&item.SourceSHA256, &item.ImmutableBuildSHA256, &item.ManifestDigest,
			&item.PromotedReference, &item.PromotionReceiptSHA256, &item.CreatedAt); err != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatUint(items[len(items)-1].Revision, 10)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

// ListScheduleRevisions requires exact schedule.view through the authorized read helper, validates
// revision position and scans immutable schedule policy/target/input/prompt snapshots. Malformed
// JSON, scan/iteration or commit failure yields Unavailable without a partial page; continuation
// is the final selected revision.
// ListScheduleRevisions требует точное schedule.view через authorized read helper, проверяет
// позицию ревизии и читает immutable policy/target/input/prompt снимки расписания. Неверный JSON,
// scan/iteration либо commit дают Unavailable без частичной страницы; продолжением служит
// последняя выбранная revision.
func (repository *Repository) ListScheduleRevisions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ScheduleRevision, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "schedule.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := versionCursor(filter.Page.Token)
	if err != nil || strings.TrimSpace(filter.ResourceRef) == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListScheduleRevisions, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "schedule_ref": filter.ResourceRef,
		"before_revision": before, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ScheduleRevision, 0, limit+1)
	for rows.Next() {
		var item entity.ScheduleRevision
		var input, promptInputs []byte
		if err := rows.Scan(&item.Ref, &item.Revision, &item.Digest, &item.Name, &item.Target.Type,
			&item.Target.Ref, &item.Preset, &item.CronExpression, &item.Timezone, &input,
			&item.SessionPolicy, &item.NotificationPolicy, &item.DSTGapPolicy, &item.DSTFoldPolicy,
			&item.MisfirePolicy, &item.OverlapPolicy, &item.TargetVersion, &item.TargetDigest,
			&item.AutomationText, &promptInputs, &item.CreatedAt); err != nil ||
			json.Unmarshal(input, &item.Input) != nil || json.Unmarshal(promptInputs, &item.PromptInputs) != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatInt(items[len(items)-1].Revision, 10)
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

// ListScheduleRuns requires schedule.view and validates a bounded run-ref cursor, then scans
// schedule revision prefixes followed by internal run projections. It pages with one lookahead and
// commits before return; internal scanner action defaults preserve historical behavior and do not
// prove per-run execution authority.
// ListScheduleRuns требует schedule.view и проверяет ограниченный run-ref курсор, читает префиксы
// schedule revision перед внутренней проекцией run. Использует дополнительную строку и commit до
// возврата; defaults действий внутреннего scanner сохраняют историческое поведение и не доказывают
// per-run полномочия исполнения.
func (repository *Repository) ListScheduleRuns(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.ScheduleRunOccurrence, string, error) {
	current, tx, err := repository.authorizedRead(ctx, principal, "schedule.view", func(scope scope) entity.AccessScope {
		return entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SCHEDULE", ResourceRef: filter.ResourceRef}
	})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!strings.HasPrefix(cursor, "run_") || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPListScheduleRuns, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "schedule_ref": filter.ResourceRef,
		"cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.ScheduleRunOccurrence, 0, limit+1)
	for rows.Next() {
		var item entity.ScheduleRunOccurrence
		run, scanErr := scanRunWithPrefix(rows, false, &item.ScheduleRef, &item.ScheduleRevisionRef, &item.ScheduleRevision)
		if scanErr != nil {
			return nil, "", scanErr
		}
		item.Run = run
		items = append(items, item)
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = items[len(items)-1].Run.Ref
	}
	if rows.Err() != nil || tx.Commit(ctx) != nil {
		return nil, "", errs.ErrUnavailable
	}
	return items, next, nil
}

// GetRuntimeEnvironmentReadiness uses the authoritative environment read path and copies its
// projected ready/blocker/environment/published-version pins into a timestamped readiness response. It
// commits that read before return and maps commit failure to Conflict without running an external
// runtime probe.
// GetRuntimeEnvironmentReadiness использует авторитетный environment read path и копирует его
// ready/blocker/environment/published-version pins в readiness ответ с временем. Фиксирует чтение до
// возврата, отказ commit отображает в Conflict без внешней проверки runtime.
func (repository *Repository) GetRuntimeEnvironmentReadiness(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentReadiness, error) {
	_, tx, item, err := repository.runtimeEnvironmentRead(ctx, principal, ref)
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := entity.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.Ref, EnvironmentVersion: item.Version,
		PublishedVersionRef: item.CurrentVersion.Ref, PublishedVersionDigest: item.CurrentVersion.Digest,
		Ready: item.Ready, Blockers: append([]string(nil), item.ReadinessBlockers...), ObservedAt: time.Now().UTC(),
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RuntimeEnvironmentReadiness{}, errs.ErrConflict
	}
	return result, nil
}

// runtimeEnvironmentReadiness derives static blockers from active state, published version/image
// presence and exact role-runtime/default-bootstrap-image pins. Ready means this local projection
// has no blockers; it does not test secret delivery, network, a running Pod or provider
// capability.
// runtimeEnvironmentReadiness вычисляет статические blockers по active state, наличию published
// version/image и точным role-runtime/default-bootstrap-image pins. Ready означает отсутствие
// blockers этой локальной проекции; не проверяет доставку секретов, сеть, работающий Pod и
// возможности провайдера.
func (repository *Repository) runtimeEnvironmentReadiness(item entity.RuntimeEnvironmentSet) entity.RuntimeEnvironmentReadiness {
	result := entity.RuntimeEnvironmentReadiness{
		EnvironmentRef: item.Ref, EnvironmentVersion: item.Version,
		PublishedVersionRef: item.CurrentVersion.Ref, PublishedVersionDigest: item.CurrentVersion.Digest,
		ObservedAt: time.Now().UTC(),
	}
	if item.State != "ACTIVE" {
		result.Blockers = append(result.Blockers, "ENVIRONMENT_NOT_ACTIVE")
	}
	if item.CurrentVersion.Ref == "" || item.CurrentVersion.Digest == "" {
		result.Blockers = append(result.Blockers, "PUBLISHED_VERSION_MISSING")
	}
	if item.CurrentVersion.Image.Reference == "" || item.CurrentVersion.Image.Digest == "" {
		result.Blockers = append(result.Blockers, "PROMOTED_IMAGE_MISSING")
	} else if item.CurrentVersion.Image.RoleRuntimeContractRevision != int64(repository.roleImages.RoleRuntimeContractRevision) ||
		item.CurrentVersion.Image.RoleRuntimeContractSHA256 != repository.roleImages.RoleRuntimeContractSHA256 {
		result.Blockers = append(result.Blockers, "ROLE_RUNTIME_CONTRACT_STALE")
	} else if item.CurrentVersion.Image.PlatformOwnedBootstrap &&
		item.CurrentVersion.Image.Digest != repository.roleImages.DefaultImageDigest {
		result.Blockers = append(result.Blockers, "DEFAULT_ROLE_IMAGE_STALE")
	}
	result.Ready = len(result.Blockers) == 0
	return result
}

// ListRuntimeEnvironmentAgents authorizes the environment read, pages bound agent references and
// closes that result before reading actor-scoped agent rows. It projects avatars/system/action
// hints and cards, preserving this narrower view without attaching instructions/grants;
// read/commit failures discard the page.
// ListRuntimeEnvironmentAgents авторизует environment read, получает страницу привязанных agent
// refs и закрывает результат до чтения строк агентов с областью субъекта. Проецирует
// avatar/system/actions и карточки, сохраняя этот узкий вид без instructions/grants; отказы
// чтения/commit отбрасывают страницу.
func (repository *Repository) ListRuntimeEnvironmentAgents(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	current, tx, _, err := repository.runtimeEnvironmentRead(ctx, principal, filter.ResourceRef)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	cursor := strings.TrimSpace(filter.Page.Token)
	if cursor != "" && (!strings.HasPrefix(cursor, "agt_") || len(cursor) > 96) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := tx.Query(ctx, queryMVPRuntimeEnvironmentAgents, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "environment_ref": filter.ResourceRef,
		"query": strings.TrimSpace(filter.Query), "cursor_ref": cursor, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	refs := make([]string, 0, limit+1)
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, "", errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(refs) > int(limit) {
		refs = refs[:limit]
		next = refs[len(refs)-1]
	}
	items := make([]entity.Agent, 0, len(refs))
	for _, ref := range refs {
		var item entity.Agent
		var canManage, canLaunch bool
		scanErr := tx.QueryRow(ctx, queryQueriesGetagentSelectAgentsOrganizationIdRefSystemKey,
			current.organizationID, ref, current.role, current.actorID).Scan(
			&item.Ref, &item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.SystemKey,
			&item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL,
			&item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision, &item.State, &item.Enabled,
			&item.Version, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model,
			&item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.CreatedAt,
			&item.UpdatedAt, &canManage, &canLaunch)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		setAgentAvatarReadback(&item)
		item.System = item.SystemKey != ""
		item.NextActions = agentActions(item, canManage, canLaunch)
		items = append(items, item)
	}
	selectedAgents := make([]*entity.Agent, len(items))
	for index := range items {
		selectedAgents[index] = &items[index]
	}
	if err := projectAgentCards(ctx, tx, current, selectedAgents); err != nil {
		return nil, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", errs.ErrConflict
	}
	return items, next, nil
}

// authorizedRead resolves scope and starts a read-only transaction with no explicit isolation
// override, then evaluates the supplied exact target/permission. Every access-check error is
// historically hidden as NotFound after rollback using the request context; successful callers own
// commit/rollback. This helper does not promise RepeatableRead or independent bounded cleanup.
// authorizedRead разрешает область, открывает read-only транзакцию без явного изменения изоляции,
// затем проверяет переданные точную цель/право. Любая ошибка access check исторически скрывается
// как NotFound после rollback с request context; при успехе commit/rollback принадлежат
// вызывающему. Helper не обещает RepeatableRead и независимый bounded cleanup.
func (repository *Repository) authorizedRead(
	ctx context.Context,
	principal value.Principal,
	permission string,
	target func(scope) entity.AccessScope,
) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target(current)); err != nil {
		_ = tx.Rollback(ctx)
		return scope{}, nil, errs.ErrNotFound
	}
	return current, tx, nil
}

// encodeMVPCursor formats a kind namespace, UTC RFC3339Nano timestamp and reference separated by
// newlines under a v1 raw-URL-base64 prefix. It provides positional encoding without a signature
// or actor/filter binding.
// encodeMVPCursor форматирует kind namespace, время UTC RFC3339Nano и ссылку, разделённые
// переводами строки, с префиксом v1/raw-URL-base64. Кодирует позицию без подписи и связи с
// субъектом/фильтром.
func encodeMVPCursor(kind string, timestamp time.Time, ref string) string {
	payload := kind + "\n" + timestamp.UTC().Format(time.RFC3339Nano) + "\n" + ref
	return "v1." + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeMVPCursor accepts an empty first page or validates v1, a384-character encoded payload
// bound, exactly three newline-separated parts, matching kind and a nonempty reference up to96
// bytes. It parses/normalizes time to UTC and returns Invalid for malformed positions without
// authorizing the reference.
// decodeMVPCursor принимает пустую первую страницу либо проверяет v1, ограничение384 символов
// кодированного payload, ровно три части с переводами строки, совпадение kind и непустую ссылку
// до96 байт. Разбирает/нормализует время в UTC, неверная позиция даёт Invalid без авторизации
// ссылки.
func decodeMVPCursor(kind, token string) (*time.Time, string, error) {
	if token == "" {
		return nil, "", nil
	}
	version, payload, ok := strings.Cut(token, ".")
	if !ok || version != "v1" || len(payload) > 384 {
		return nil, "", errs.ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parts := strings.Split(string(decoded), "\n")
	if len(parts) != 3 || parts[0] != kind || parts[2] == "" || len(parts[2]) > 96 {
		return nil, "", errs.ErrInvalid
	}
	parsed, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return nil, "", errs.ErrInvalid
	}
	parsed = parsed.UTC()
	return &parsed, parts[2], nil
}

// validStableKey requires at least two bytes, a lowercase ASCII initial letter and only lowercase
// letters, digits, underscore or hyphen thereafter. It imposes no maximum length; callers apply
// operation-specific limits.
// validStableKey требует не менее двух байт, начальную lowercase ASCII букву и далее лишь строчные
// буквы, цифры, underscore либо hyphen. Не ограничивает максимальную длину; отдельные пределы
// применяют вызывающие.
func validStableKey(value string) bool {
	if len(value) < 2 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		lowercaseLetter := character >= 'a' && character <= 'z'
		digit := character >= '0' && character <= '9'
		if !lowercaseLetter && !digit && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
