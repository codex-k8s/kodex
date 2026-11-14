package modeldiscovery

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// validateFilter rejects unknown selectors, unsupported grouping and bounds before pagination; the closed status vocabulary retains UNKNOWN as a distinct filter value.
// validateFilter отклоняет неизвестные selectors, grouping и превышение bounds до пагинации; закрытый словарь статусов сохраняет UNKNOWN как отдельное значение фильтра.
func validateFilter(filter query.ModelDiscoveryFilter) error {
	for _, value := range []string{filter.ProviderID, filter.Family, filter.InputModality, filter.OutputModality} {
		if len(value) > 128 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\t") {
			return ErrQuery
		}
	}
	if filter.MinimumContextTokens < 0 || filter.MinimumContextTokens > 1000000000000 || !slices.Contains([]string{"PROVIDER", "FAMILY"}, filter.GroupBy) || !slices.Contains([]string{"", "SUPPORTED", "UNSUPPORTED", "UNKNOWN"}, filter.Tools) || !slices.Contains([]string{"", "SUPPORTED", "UNSUPPORTED", "UNKNOWN"}, filter.Reasoning) || !slices.Contains([]string{"", "NONE", "IMPLICIT", "EXPLICIT", "BOTH", "UNKNOWN"}, filter.Cache) {
		return ErrQuery
	}
	return nil
}

// matchesModality accepts exact MIME values and broad media kinds without treating an absent list as lack of support.
// matchesModality принимает точные MIME values и широкие media kinds, не выдавая отсутствующий список за отсутствие поддержки.
func matchesModality(values []string, wanted string) bool {
	if wanted == "" {
		return true
	}
	for _, value := range values {
		if value == wanted || !strings.Contains(wanted, "/") && strings.HasPrefix(value, wanted+"/") {
			return true
		}
	}
	return false
}

// matchesFilters combines informational predicates before totals and cursor offsets, including declared cache kinds rather than a runtime cache guarantee.
// matchesFilters объединяет справочные predicates до total и cursor offsets, включая declared cache kinds без обещания runtime cache guarantees.
func matchesFilters(record entity.ModelDiscoveryRecord, filter query.ModelDiscoveryFilter) bool {
	if filter.ProviderID != "" && record.ProviderID != filter.ProviderID || filter.Family != "" && record.Family != filter.Family || filter.MinimumContextTokens > 0 && (!record.ContextKnown || record.ContextTokens < filter.MinimumContextTokens) || filter.Tools != "" && record.Tools != filter.Tools || filter.Reasoning != "" && record.Reasoning != filter.Reasoning || !matchesModality(record.InputModalities, filter.InputModality) || !matchesModality(record.OutputModalities, filter.OutputModality) {
		return false
	}
	switch filter.Cache {
	case "IMPLICIT":
		return record.CacheImplicit == "SUPPORTED"
	case "EXPLICIT":
		return record.CacheExplicit == "SUPPORTED"
	case "BOTH", "NONE", "UNKNOWN":
		return record.Cache == filter.Cache
	}
	return true
}

// groupKey orders every family page consistently across providers while retaining exact provider/model identities as deterministic tie breakers.
// groupKey одинаково упорядочивает страницы family между providers, сохраняя точные provider/model identities как детерминированный порядок внутри группы.
func groupKey(record entity.ModelDiscoveryRecord, group string) string {
	key := record.ProviderID + "\x00" + record.ModelID
	if group == "FAMILY" {
		key = record.Family + "\x00" + key
	}
	return key
}
