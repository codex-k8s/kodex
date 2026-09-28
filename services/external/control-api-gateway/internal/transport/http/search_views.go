package httptransport

import (
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const searchUpstreamProblemCode = "INVALID_UPSTREAM_RESPONSE"

func rejectSearchPage(w http.ResponseWriter, reason string) {
	slog.Error("Invalid search response", "reason", reason)
	writeLocalProblem(w, http.StatusBadGateway, searchUpstreamProblemCode, false)
}

func validSearchQuery(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(strings.TrimSpace(value)) >= 2 && utf8.RuneCountInString(value) <= 200 && !strings.ContainsRune(value, '\x00')
}

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

func validSearchText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return utf8.ValidString(value) && length >= minimum && length <= maximum && !strings.ContainsRune(value, '\x00')
}
