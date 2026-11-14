// Package modeldiscovery searches a pinned informational catalog without acquiring credentials, changing routes or authorizing inference.
// Package modeldiscovery ищет в закреплённом справочном каталоге без получения credentials, изменения маршрутов или разрешения inference.
package modeldiscovery

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	querytype "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// assets contains the reviewed compressed projection, provenance and separately maintained custom records; refreshing upstream never rewrites the latter.
// assets содержит проверенную сжатую проекцию, provenance и отдельно сопровождаемые custom records; обновление upstream не переписывает последние.
//
//go:embed data/*
var assets embed.FS

// ErrCatalog closes malformed assets without exposing imported payloads or confusing readiness failure with invalid input.
// ErrCatalog закрыто отклоняет неверные assets без раскрытия импортированных данных и смешения readiness failure с неверным вводом.
var ErrCatalog = errors.New("model discovery catalog is invalid")

// ErrQuery rejects invalid filters or stale cursors independently of asset readiness.
// ErrQuery отклоняет неверные фильтры и устаревшие курсоры независимо от готовности assets.
var ErrQuery = errors.New("model discovery query is invalid")

var (
	// catalogOnce synchronizes the immutable asset loader across concurrent reads and readiness probes.
	// catalogOnce синхронизирует immutable asset loader между параллельными reads и readiness probes.
	catalogOnce sync.Once
	// catalogRecords retains reviewed metadata; callers receive detached record slices before annotation.
	// catalogRecords хранит проверенные metadata; callers получают отдельные record slices до добавления статусов.
	catalogRecords []entity.ModelDiscoveryRecord
	// catalogSource preserves artifact and schema provenance without claiming an account observation.
	// catalogSource сохраняет artifact и schema provenance без заявления о наблюдении аккаунта.
	catalogSource entity.ModelDiscoverySource
	// catalogError makes asset loading failure durable for this immutable process image.
	// catalogError сохраняет отказ загрузки assets на всё время работы immutable process image.
	catalogError error
)

// decodeAssets verifies exact decompressed bytes against provenance before any record is used; incomplete refreshes fail closed.
// decodeAssets проверяет точные распакованные байты по provenance до использования записей; неполное обновление закрыто отклоняется.
func decodeAssets() {
	records, source, err := loadCatalogAssets(assets)
	if err != nil {
		catalogError = err
		return
	}
	catalogRecords, catalogSource = records, source
}

// Ready verifies the shipped assets through the same immutable loader used by search; it performs no remote calls or credential reads.
// Ready проверяет поставляемые assets тем же immutable loader, что использует поиск; внешних вызовов или чтения credentials нет.
func Ready() error { catalogOnce.Do(decodeAssets); return catalogError }

// Search applies bounded deterministic search and cursor binding to immutable metadata, then annotates only exact owner-configured endpoint/protocol/model matches.
// Search выполняет ограниченный детерминированный поиск и связывает курсор с immutable metadata; статусы дополняются только по точным owner-configured endpoint/protocol/model.
func Search(query, provider, cursor string, size int, definitions []entity.ProviderDefinition) (entity.ModelDiscoveryPage, error) {
	return SearchFiltered(query, cursor, size, querytype.ModelDiscoveryFilter{ProviderID: provider}, definitions)
}

// SearchFiltered binds every typed filter and grouping choice into pagination before joining any configured-route evidence; unknown capability metadata never matches a supported-only filter.
// SearchFiltered связывает каждый typed filter и grouping choice с пагинацией до добавления route evidence; неизвестные capabilities не проходят supported-only filter.
func SearchFiltered(query, cursor string, size int, filter querytype.ModelDiscoveryFilter, definitions []entity.ProviderDefinition) (entity.ModelDiscoveryPage, error) {
	catalogOnce.Do(decodeAssets)
	if catalogError != nil {
		return entity.ModelDiscoveryPage{}, catalogError
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if filter.GroupBy == "" {
		filter.GroupBy = "PROVIDER"
	}
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 200 || size < 1 || size > 100 || len(cursor) > 512 || validateFilter(filter) != nil {
		return entity.ModelDiscoveryPage{}, ErrQuery
	}
	routePins := []string{}
	for _, definition := range definitions {
		if definition.ModelProvider != nil {
			digest, err := definition.ModelProvider.Digest()
			if err != nil {
				return entity.ModelDiscoveryPage{}, ErrCatalog
			}
			routePins = append(routePins, definition.Key+":"+digest)
		}
	}
	slices.Sort(routePins)
	filterRaw, _ := json.Marshal(filter)
	queryDigest := sha256.Sum256([]byte(catalogSource.CatalogSHA256 + "\x00" + strings.Join(routePins, ",") + "\x00" + query + "\x00" + string(filterRaw)))
	prefix := hex.EncodeToString(queryDigest[:]) + ":"
	offset := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || !bytes.HasPrefix(raw, []byte(prefix)) {
			return entity.ModelDiscoveryPage{}, ErrQuery
		}
		offset, err = strconv.Atoi(string(raw[len(prefix):]))
		if err != nil || offset < 0 || strconv.Itoa(offset) != string(raw[len(prefix):]) {
			return entity.ModelDiscoveryPage{}, ErrQuery
		}
	}
	records := slices.Clone(catalogRecords)
	indexedRoutes := map[string]bool{}
	for _, record := range records {
		for _, api := range record.APIs {
			if api.Path != "" && api.Path != defaultAPIPath(api.Protocol) {
				continue
			}
			indexedRoutes[record.ModelID+"\x00"+api.Protocol+"\x00"+api.BaseURL+"\x00"+api.SDK] = true
		}
	}
	// Сохранённый разрешённый descriptor остаётся видимым после удаления записи upstream; выбор не подменяется соседней моделью.
	for _, definition := range definitions {
		if definition.ModelProvider == nil {
			continue
		}
		for _, model := range definition.ModelProvider.Models {
			found := indexedRoutes[model.ID+"\x00"+protocol(*definition.ModelProvider)+"\x00"+definition.ModelProvider.BaseURL+"\x00"+definition.ModelProvider.AdapterPackage()]
			if !found {
				records = append(records, entity.ModelDiscoveryRecord{ProviderID: definition.Key, ProviderName: definition.Name, ModelID: model.ID, Name: model.ID, Family: "UNKNOWN", Type: "chat", ContextTokens: 0, OutputTokens: 0, ContextKnown: false, OutputKnown: false, Tools: "UNKNOWN", Reasoning: "UNKNOWN", Cache: "UNKNOWN", CacheImplicit: "UNKNOWN", CacheExplicit: "UNKNOWN", Known: "CUSTOM", UpstreamStatus: "custom", APIs: []entity.ModelDiscoveryAPI{{Protocol: protocol(*definition.ModelProvider), BaseURL: definition.ModelProvider.BaseURL, SDK: definition.ModelProvider.AdapterPackage()}}})
			}
		}
	}
	slices.SortFunc(records, func(a, b entity.ModelDiscoveryRecord) int {
		return strings.Compare(groupKey(a, filter.GroupBy), groupKey(b, filter.GroupBy))
	})
	result := entity.ModelDiscoveryPage{Items: []entity.ModelDiscoveryRecord{}, Source: catalogSource, GroupBy: filter.GroupBy}
	for _, record := range records {
		if !matchesFilters(record, filter) || query != "" && !strings.Contains(strings.ToLower(record.ProviderID+" "+record.ProviderName+" "+record.ModelID+" "+record.Name+" "+record.Family), query) {
			continue
		}
		index := result.Total
		result.Total++
		if index < int64(offset) || len(result.Items) >= size {
			continue
		}
		raw, _ := json.Marshal(record)
		var detached entity.ModelDiscoveryRecord
		_ = json.Unmarshal(raw, &detached)
		detached.Known = record.Known
		annotate(&detached, definitions)
		result.Items = append(result.Items, detached)
	}
	if int64(offset) > result.Total {
		return entity.ModelDiscoveryPage{}, ErrQuery
	}
	if int64(offset+len(result.Items)) < result.Total {
		result.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(prefix + strconv.Itoa(offset+len(result.Items))))
	}
	return result, nil
}

// protocol translates only the three already bundled adapter APIs, retaining the existing default chat protocol.
// protocol переводит только три уже поставляемых API адаптеров, сохраняя существующий default chat protocol.
func protocol(config runtimecontract.ModelProviderConfiguration) string {
	switch config.API {
	case runtimecontract.ModelAPIResponses:
		return "responses"
	case runtimecontract.ModelAPIMessages:
		return "messages"
	default:
		return "chat-completions"
	}
}

// matchesRoute compares inert discovery metadata with a validated owner descriptor; matching never creates or expands that descriptor.
// matchesRoute сравнивает пассивные discovery metadata с проверенным owner descriptor; совпадение не создаёт и не расширяет descriptor.
func matchesRoute(record entity.ModelDiscoveryRecord, config runtimecontract.ModelProviderConfiguration, model string) bool {
	if config.Validate() != nil || record.ModelID != model {
		return false
	}
	if _, found := config.Model(model); !found {
		return false
	}
	for _, api := range record.APIs {
		if api.BaseURL == config.BaseURL && api.Protocol == protocol(config) && api.SDK == config.AdapterPackage() && (api.Path == "" || api.Path == defaultAPIPath(api.Protocol)) {
			return true
		}
	}
	return false
}

// defaultAPIPath accepts only paths already implemented by the closed runtime adapter; arbitrary upstream path overrides remain informational.
// defaultAPIPath принимает только пути уже реализованного закрытого runtime adapter; произвольные path overrides upstream остаются справочной информацией.
func defaultAPIPath(protocol string) string {
	switch protocol {
	case "chat-completions":
		return "/chat/completions"
	case "responses":
		return "/responses"
	case "messages":
		return "/messages"
	default:
		return ""
	}
}

// annotate keeps account catalog eligibility, route configuration, runtime authority and actual execution evidence separate; a bundled SDK is not a tested model.
// annotate разделяет допуск каталога аккаунта, настройку маршрута, runtime authority и фактическое исполнение; поставляемый SDK не является проверенной моделью.
func annotate(record *entity.ModelDiscoveryRecord, definitions []entity.ProviderDefinition) {
	if record.Known == "" {
		record.Known = "KNOWN"
	}
	record.Adapter, record.Account, record.Allowed, record.Tested = "UNSUPPORTED", "UNKNOWN", "UNAVAILABLE", "NOT_RUN"
	for _, api := range record.APIs {
		if api.Protocol == "chat-completions" && api.SDK == "@ai-sdk/openai-compatible" || api.Protocol == "responses" && api.SDK == "@ai-sdk/openai" || api.Protocol == "messages" && api.SDK == "@ai-sdk/anthropic" {
			record.Adapter = "BUNDLED"
		}
	}
	for _, definition := range definitions {
		if definition.ModelProvider == nil || !matchesRoute(*record, *definition.ModelProvider, record.ModelID) {
			continue
		}
		record.RouteConfigured = true
		if record.Known == "REMOVED" {
			record.Allowed = "UNAVAILABLE"
			continue
		}
		if !definition.Available {
			if record.Account != "AVAILABLE" {
				record.Account = "UNAVAILABLE"
			}
			continue
		}
		record.Allowed = "UNKNOWN"
		for _, model := range definition.Models {
			if model.ID != record.ModelID {
				continue
			}
			if model.Available {
				record.Account = "AVAILABLE"
			} else if record.Account != "AVAILABLE" {
				record.Account = "UNAVAILABLE"
			}
		}
	}
}
