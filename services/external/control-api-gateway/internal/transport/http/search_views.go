package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func validSearchQuery(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(strings.TrimSpace(value)) >= 2 && utf8.RuneCountInString(value) <= 200 && !strings.ContainsRune(value, '\x00')
}

func writeSearchPage(w http.ResponseWriter, response *cp.SearchPlatformResponse, project string, limit int) {
	if response == nil || len(response.Results) > limit || response.Total < int64(len(response.Results)) || response.Total > maximumSafeJSONInteger ||
		len(response.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.SearchResultPage{Items: make([]generated.SearchResult, 0, len(response.Results)), Total: response.Total}
	if next := response.GetPage().GetNextPageToken(); next != "" {
		result.NextPageToken = &next
	}
	seen := map[string]bool{}
	for _, item := range response.Results {
		if item == nil || !opaqueHTTPReference.MatchString(item.Ref) || !opaqueHTTPReference.MatchString(item.ProjectRef) ||
			project != "" && item.ProjectRef != project || !validSearchText(item.Title, 1, 300) || !validSearchText(item.Subtitle, 0, 1000) ||
			!validSearchText(item.State, 1, 80) || item.UpdatedAt == nil || item.UpdatedAt.CheckValid() != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		switch item.Kind {
		case cp.SearchResultKind_SEARCH_RESULT_KIND_PROJECT:
			if item.Ref != item.ProjectRef {
				writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
		case cp.SearchResultKind_SEARCH_RESULT_KIND_AGENT, cp.SearchResultKind_SEARCH_RESULT_KIND_WORKFLOW, cp.SearchResultKind_SEARCH_RESULT_KIND_RUN:
		default:
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		identity := item.Kind.String() + ":" + item.Ref
		if seen[identity] {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[identity] = true
		result.Items = append(result.Items, generated.SearchResult{Kind: generated.SearchResultKind(strings.TrimPrefix(item.Kind.String(), "SEARCH_RESULT_KIND_")),
			Ref: item.Ref, ProjectRef: item.ProjectRef, Title: item.Title, Subtitle: item.Subtitle, State: item.State, UpdatedAt: item.UpdatedAt.AsTime()})
	}
	writeJSON(w, http.StatusOK, result)
}

func validSearchText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum && !strings.ContainsRune(value, '\x00')
}
