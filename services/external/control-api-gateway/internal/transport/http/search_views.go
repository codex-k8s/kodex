package httptransport

import (
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// searchUpstreamProblemCode is the stable public problem code for malformed owner search pages,
// shared across all search projection rejection paths.
// searchUpstreamProblemCode задаёт устойчивый публичный код ошибки неверной страницы поиска
// владельца для всех путей отказа проекции поиска.
const searchUpstreamProblemCode = "INVALID_UPSTREAM_RESPONSE"

// rejectSearchPage logs the supplied fixed search-validation reason and emits a nonretryable HTTP
// 502 problem. Callers provide safe categories without item contents or upstream errors.
// rejectSearchPage записывает переданную фиксированную причину проверки поиска и выдаёт
// неповторяемую ошибку HTTP 502. Caller передаёт безопасную категорию без содержимого элементов и
// upstream ошибок.
func rejectSearchPage(w http.ResponseWriter, reason string) {
	slog.Error("Invalid search response", "reason", reason)
	writeLocalProblem(w, http.StatusBadGateway, searchUpstreamProblemCode, false)
}

// validSearchQuery requires valid UTF-8 with at least two nonpadding runes, at most 200 original
// runes and no NUL. Validation does not trim or alter the query forwarded to the owner.
// validSearchQuery требует корректный UTF-8, не менее двух рун без внешних пробелов, не более 200
// исходных рун и отсутствие NUL. Проверка не обрезает и не меняет запрос владельцу.
func validSearchQuery(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(strings.TrimSpace(value)) >= 2 && utf8.RuneCountInString(value) <= 200 && !strings.ContainsRune(value, '\x00')
}

// writeSearchPage validates page size/count/cursor and every result identity, requested project,
// text, time and supported kind before writing any JSON. Duplicate identities or one invalid item
// reject the complete page, preserving owner eligibility and total.
// writeSearchPage проверяет размер/count/cursor страницы и identity, запрошенный проект, текст,
// время и поддерживаемый вид каждого результата до выдачи JSON. Дубликат или один неверный элемент
// отклоняет всю страницу, сохраняя допуск владельца и total.
func writeSearchPage(w http.ResponseWriter, response *cp.SearchPlatformResponse, project string, limit int) {
	if response == nil {
		rejectSearchPage(w, "missing page")
		return
	}
	if len(response.Results) > limit {
		rejectSearchPage(w, "page exceeds limit")
		return
	}
	if response.Total < int64(len(response.Results)) || response.Total > maximumSafeJSONInteger {
		rejectSearchPage(w, "invalid total")
		return
	}
	if len(response.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(response.GetPage().GetNextPageToken()) {
		rejectSearchPage(w, "invalid cursor")
		return
	}
	result := generated.SearchResultPage{Items: make([]generated.SearchResult, 0, len(response.Results)), Total: response.Total}
	if next := response.GetPage().GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	seen := map[string]bool{}
	for _, item := range response.Results {
		if item == nil {
			rejectSearchPage(w, "missing result")
			return
		}
		if !opaqueHTTPReference.MatchString(item.Ref) || !opaqueHTTPReference.MatchString(item.ProjectRef) {
			rejectSearchPage(w, "invalid result reference")
			return
		}
		if project != "" && item.ProjectRef != project {
			rejectSearchPage(w, "result outside requested project")
			return
		}
		if !validSearchText(item.Title, 1, 300) || !validSearchText(item.Subtitle, 0, 1000) {
			rejectSearchPage(w, "invalid result text")
			return
		}
		if !validSearchText(item.State, 1, 80) {
			rejectSearchPage(w, "invalid result state")
			return
		}
		if item.UpdatedAt == nil || item.UpdatedAt.CheckValid() != nil {
			rejectSearchPage(w, "invalid result timestamp")
			return
		}
		switch item.Kind {
		case cp.SearchResultKind_SEARCH_RESULT_KIND_PROJECT:
			if item.Ref != item.ProjectRef {
				rejectSearchPage(w, "project reference mismatch")
				return
			}
		case cp.SearchResultKind_SEARCH_RESULT_KIND_AGENT, cp.SearchResultKind_SEARCH_RESULT_KIND_WORKFLOW, cp.SearchResultKind_SEARCH_RESULT_KIND_RUN, cp.SearchResultKind_SEARCH_RESULT_KIND_ARTIFACT:
		default:
			rejectSearchPage(w, "unsupported result kind")
			return
		}
		identity := item.Kind.String() + ":" + item.Ref
		if seen[identity] {
			rejectSearchPage(w, "duplicate result")
			return
		}
		seen[identity] = true
		result.Items = append(result.Items, generated.SearchResult{Kind: generated.SearchResultKind(strings.TrimPrefix(item.Kind.String(), "SEARCH_RESULT_KIND_")),
			Ref: item.Ref, ProjectRef: item.ProjectRef, Title: item.Title, Subtitle: item.Subtitle, State: item.State, UpdatedAt: item.UpdatedAt.AsTime()})
	}
	writeJSON(w, http.StatusOK, result)
}

// validSearchText checks inclusive rune-count bounds, UTF-8 validity and absence of NUL, allowing
// empty text only when the minimum is zero. Other controls are not removed or normalized here.
// validSearchText проверяет включённые границы числа рун, UTF-8 и отсутствие NUL, разрешая пустой
// текст только при нулевом минимуме. Другие управляющие символы здесь не удаляются и не
// нормализуются.
func validSearchText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum && !strings.ContainsRune(value, '\x00')
}
