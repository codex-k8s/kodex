package callback

import (
	"context"
	"errors"
	"net/url"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func assistantResourceSearchTool() map[string]any {
	query := stringSchema(2, 160)
	query["description"] = assistantSearchQueryDescription
	return map[string]any{
		"name":        "find_platform_resources",
		"description": "Search resource metadata visible to the initiating user; never returns secret values. Use exact result refs. For another project, ask the user to open its route before proposing changes. Search never changes context or grants access.",
		"inputSchema": objectSchema([]string{"query"}, map[string]any{"query": query}),
		"outputSchema": objectSchema([]string{"current_project_ref", "results", "truncated"}, map[string]any{
			"current_project_ref": map[string]any{"type": "string"},
			"results": map[string]any{"type": "array", "maxItems": maximumAssistantSearchResults,
				"items": objectSchema([]string{"kind", "ref", "project_ref", "title", "subtitle", "state", "route", "requires_context_switch"}, map[string]any{
					"kind": enumSchema("PROJECT", "AGENT", "WORKFLOW", "RUN", "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", "SCHEDULE", "INTEGRATION", "SECRET"), "ref": opaqueRefSchema(),
					"project_ref": stringSchema(0, 96), "title": stringSchema(0, 160), "subtitle": stringSchema(0, 160),
					"state": map[string]any{"type": "string"}, "route": map[string]any{"type": "string"},
					"requires_context_switch": map[string]any{"type": "boolean"},
				}),
			},
			"truncated": map[string]any{"type": "boolean"},
		}),
	}
}

const maximumAssistantSearchResults = 10

const (
	assistantSearchQueryDescription      = "Specific name or opaque ref: 2..160 trimmed Unicode codepoints, not blank. Search is not full inventory."
	assistantSearchInputInvalidCode      = "SEARCH_INPUT_INVALID"
	assistantSearchInputInvalidGuidance  = "Retry at most once with only query: a specific resource name or exact opaque ref containing 2 to 160 Unicode codepoints after trimming surrounding whitespace. Do not send an empty query, task text, kind, ref or filters as separate fields. For supported configuration discovery, use get_configuration_catalog with the current schema; its agent list is turn-pinned and does not prove a complete fresh inventory. If authoritative discovery is unavailable, ask the owner for readback instead of guessing resources."
	assistantSearchFailureMessage        = "assistant resource search failed"
	assistantSearchContextInvalid        = "assistant_search_context_invalid"
	assistantSearchInputShapeInvalid     = "assistant_search_input_shape_invalid"
	assistantSearchQueryInvalid          = "assistant_search_query_invalid"
	assistantSearchOwnerFailed           = "assistant_search_owner_failed"
	assistantSearchResponseShapeInvalid  = "assistant_search_response_shape_invalid"
	assistantSearchResultIdentityInvalid = "assistant_search_result_identity_invalid"
	assistantSearchResultRouteInvalid    = "assistant_search_result_route_invalid"
)

type assistantSearchError struct {
	class string
	cause error
}

func (err *assistantSearchError) Error() string { return assistantSearchFailureMessage }
func (err *assistantSearchError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func assistantSearchFailureClass(err error) string {
	var failure *assistantSearchError
	if !errors.As(err, &failure) || failure == nil {
		return ""
	}
	switch failure.class {
	case assistantSearchContextInvalid, assistantSearchInputShapeInvalid, assistantSearchQueryInvalid,
		assistantSearchOwnerFailed, assistantSearchResponseShapeInvalid, assistantSearchResultIdentityInvalid,
		assistantSearchResultRouteInvalid:
		return failure.class
	default:
		return ""
	}
}

func (server *Server) findPlatformResources(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if !input.IsAssistant() || input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, &assistantSearchError{class: assistantSearchContextInvalid}
	}
	if !onlyKeys(arguments, "query") {
		return nil, &assistantSearchError{class: assistantSearchInputShapeInvalid}
	}
	query, ok := arguments["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || len([]rune(query)) < 2 || len([]rune(query)) > 160 {
		return nil, &assistantSearchError{class: assistantSearchQueryInvalid}
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Query: query,
	})
	if err != nil {
		return nil, &assistantSearchError{class: assistantSearchOwnerFailed, cause: err}
	}
	if response == nil || response.GetAssistantConfigurationCatalog() != nil || len(response.GetDefinitions()) != 0 || response.GetNextDefinitionOffset() != 0 || len(response.GetResults()) > maximumAssistantSearchResults {
		return nil, &assistantSearchError{class: assistantSearchResponseShapeInvalid}
	}
	items := make([]map[string]any, 0, len(response.GetResults()))
	for _, item := range response.GetResults() {
		if item == nil || !validAssistantResourceRef(item.GetRef()) ||
			(item.GetKind() != controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && !validAssistantResourceRef(item.GetProjectRef())) ||
			(item.GetKind() == controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && item.GetProjectRef() != "") {
			return nil, &assistantSearchError{class: assistantSearchResultIdentityInvalid}
		}
		kind, route := assistantResourceRoute(item)
		if kind == "" {
			return nil, &assistantSearchError{class: assistantSearchResultRouteInvalid}
		}
		items = append(items, map[string]any{
			"kind": kind, "ref": item.GetRef(), "project_ref": item.GetProjectRef(),
			"title": truncateRunes(item.GetTitle(), 160), "subtitle": truncateRunes(item.GetSubtitle(), 160),
			"state": item.GetState(), "route": route,
			"requires_context_switch": item.GetProjectRef() != "" && item.GetProjectRef() != input.ProjectRef ||
				(kind == "INTEGRATION" && (input.AssistantContext == nil || input.AssistantContext.EntityKind != "INTEGRATION_CONNECTION" || input.AssistantContext.EntityRef != item.GetRef())),
		})
	}
	return map[string]any{"current_project_ref": input.ProjectRef, "results": items, "truncated": response.GetTruncated()}, nil
}

func validAssistantResourceRef(ref string) bool {
	return len(ref) <= 96 && safeInvocationRef(ref)
}

func assistantResourceRoute(item *controlplanev1.SearchResult) (string, string) {
	project := "/projects/" + url.PathEscape(item.GetProjectRef())
	switch item.GetKind() {
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_PROJECT:
		if item.GetRef() != item.GetProjectRef() {
			return "", ""
		}
		return "PROJECT", project
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_AGENT:
		return "AGENT", project + "/agents/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_WORKFLOW:
		return "WORKFLOW", project + "/workflows/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_RUN:
		return "RUN", project + "/runs/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_ROLE_IMAGE:
		return "ROLE_IMAGE", project + "/role-images/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_RUNTIME_ENVIRONMENT:
		return "RUNTIME_ENVIRONMENT", project + "/environments/" + url.PathEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SCHEDULE:
		return "SCHEDULE", project + "/automations?scheduleRef=" + url.QueryEscape(item.GetRef())
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_SECRET:
		return "SECRET", project + "/secrets"
	case controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION:
		if item.GetProjectRef() != "" {
			return "", ""
		}
		return "INTEGRATION", "/integrations?connectionRef=" + url.QueryEscape(item.GetRef())
	default:
		return "", ""
	}
}
