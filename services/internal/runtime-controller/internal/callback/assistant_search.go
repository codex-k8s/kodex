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
	return map[string]any{
		"name":        "find_platform_resources",
		"description": "Find projects, AI employees, workflows, runs, role images, runtime environments, schedules, integration connections and secret metadata visible to the initiating user. Secret values are never returned. Use exact opaque refs from results. When a result belongs to another project, ask the user to open its route before proposing changes; this tool never changes browser context or grants access.",
		"inputSchema": objectSchema([]string{"query"}, map[string]any{"query": stringSchema(2, 160)}),
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

func (server *Server) findPlatformResources(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if !input.SystemAssistant || !onlyKeys(arguments, "query") || input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 {
		return nil, errors.New("assistant resource search is not available")
	}
	query, ok := arguments["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || len([]rune(query)) < 2 || len([]rune(query)) > 160 {
		return nil, errors.New("assistant resource search query is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.SearchAssistantResources(requestContext, &controlplanev1.SearchAssistantResourcesRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Query: query,
	})
	if err != nil {
		return nil, err
	}
	if len(response.GetResults()) > maximumAssistantSearchResults {
		return nil, errors.New("assistant resource search result is invalid")
	}
	items := make([]map[string]any, 0, len(response.GetResults()))
	for _, item := range response.GetResults() {
		if item == nil || !validAssistantResourceRef(item.GetRef()) ||
			(item.GetKind() != controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && !validAssistantResourceRef(item.GetProjectRef())) ||
			(item.GetKind() == controlplanev1.SearchResultKind_SEARCH_RESULT_KIND_INTEGRATION && item.GetProjectRef() != "") {
			return nil, errors.New("assistant resource search result is invalid")
		}
		kind, route := assistantResourceRoute(item)
		if kind == "" {
			return nil, errors.New("assistant resource search result is invalid")
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
