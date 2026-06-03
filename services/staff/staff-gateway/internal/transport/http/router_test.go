package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRouterListOwnerInboxItems(t *testing.T) {
	client := &fakeInteractionHubClient{listResponse: &interactionsv1.ListOwnerInboxItemsResponse{
		Items: []*interactionsv1.OwnerInboxItem{sampleOwnerInboxItem(interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING)},
		Page:  &interactionsv1.PageResponse{},
	}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1&request_kind=human_gate&status=waiting&page_size=10", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if client.listRequest.GetScope().GetRef() != "project-1" || client.listRequest.GetPage().GetPageSize() != 10 {
		t.Fatalf("list request = %+v, want scope and page", client.listRequest)
	}
	if got := client.listRequest.GetMeta().GetActor().GetId(); got != "owner-1" {
		t.Fatalf("actor id = %q", got)
	}
	var body generated.OwnerInboxListResponse
	decodeJSON(t, rec, &body)
	if len(body.Items) != 1 || body.Items[0].RequestKind != generated.HumanGate {
		t.Fatalf("items = %+v, want human gate item", body.Items)
	}
}

func TestRouterListOwnerInboxItemsFiltersAndPagination(t *testing.T) {
	client := &fakeInteractionHubClient{listResponse: &interactionsv1.ListOwnerInboxItemsResponse{
		Items: []*interactionsv1.OwnerInboxItem{sampleOwnerInboxItem(interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING)},
		Page:  &interactionsv1.PageResponse{NextPageToken: stringPtr("cursor-2")},
	}}
	router := newTestRouter(t, client)
	target := "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1" +
		"&request_kind=feedback,human_gate&status=created,waiting" +
		"&source_owner_kind=agent_manager&source_owner_ref=run-1" +
		"&assignee_kind=user&assignee_ref=owner-1&actor_ref=user/owner-1" +
		"&correlation_kind=agent_run&correlation_ref=run-1&correlation_id=corr-1" +
		"&include_diagnostics=true&page_size=50&page_token=cursor-1"
	req := authenticatedRequest(http.MethodGet, target, "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.listRequest
	if len(recorded.GetRequestKinds()) != 2 || len(recorded.GetStatuses()) != 2 {
		t.Fatalf("filters = %+v/%+v, want request kinds and statuses", recorded.GetRequestKinds(), recorded.GetStatuses())
	}
	if recorded.GetAssigneeRef().GetRefKind() != "user" || recorded.GetCorrelationRef().GetRef() != "run-1" {
		t.Fatalf("refs = %+v/%+v, want assignee and correlation refs", recorded.GetAssigneeRef(), recorded.GetCorrelationRef())
	}
	if !recorded.GetIncludeDiagnostics() || recorded.GetPage().GetPageSize() != 50 || recorded.GetPage().GetPageToken() != "cursor-1" {
		t.Fatalf("page/diagnostics = %+v/%t, want page and diagnostics", recorded.GetPage(), recorded.GetIncludeDiagnostics())
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" || recorded.GetMeta().GetRequestContext().GetSessionId() != "session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.OwnerInboxListResponse
	decodeJSON(t, rec, &body)
	if body.Page.NextPageToken == nil || *body.Page.NextPageToken != "cursor-2" {
		t.Fatalf("page response = %+v, want next token", body.Page)
	}
}

func TestRouterGetOwnerInboxItem(t *testing.T) {
	item := sampleOwnerInboxItem(interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_WAITING)
	client := &fakeInteractionHubClient{getResponse: &interactionsv1.OwnerInboxItemResponse{Item: item}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items/"+item.GetRequestId()+"?scope_type=project&scope_ref=project-1&assignee_kind=user&assignee_ref=owner-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if client.getRequest.GetRequestId() != item.GetRequestId() || client.getRequest.GetAssigneeRef().GetRef() != "owner-1" {
		t.Fatalf("get request = %+v, want request id and assignee", client.getRequest)
	}
	var body generated.OwnerInboxItemResponse
	decodeJSON(t, rec, &body)
	if len(body.Item.AllowedActions) != 3 {
		t.Fatalf("allowed actions = %+v, want 3", body.Item.AllowedActions)
	}
}

func TestRouterGetOwnerInboxItemReturnsSafeDetailDTO(t *testing.T) {
	item := sampleOwnerInboxItemWithDiagnostics(interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED)
	client := &fakeInteractionHubClient{getResponse: &interactionsv1.OwnerInboxItemResponse{Item: item}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items/"+item.GetRequestId()+"?scope_type=project&scope_ref=project-1&include_diagnostics=true", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.OwnerInboxItemResponse
	decodeJSON(t, rec, &body)
	if body.Item.LatestCallback == nil || body.Item.LatestResponse == nil {
		t.Fatalf("item = %+v, want callback and response summaries", body.Item)
	}
	if body.Item.LatestResponse.ResponseAction != generated.RequestChanges {
		t.Fatalf("latest response action = %s, want request_changes", body.Item.LatestResponse.ResponseAction)
	}
	for _, forbidden := range []string{"raw_payload", "secret-token", "provider_payload"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterRespondOwnerInboxItem(t *testing.T) {
	requestID := uuid.NewString()
	client := &fakeInteractionHubClient{recordResponse: sampleInteractionResponseResponse(requestID, interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES)}
	router := newTestRouter(t, client)
	payload := `{"action":"request_changes","expected_version":3,"idempotency_key":"owner-response-1","response_summary":"Нужна доработка проверки"}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+requestID+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.recordRequest
	if recorded.GetResponseAction() != interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES {
		t.Fatalf("response action = %s", recorded.GetResponseAction())
	}
	if recorded.GetMeta().GetExpectedVersion() != 3 || recorded.GetRespondedByActorRef() != "user/owner-1" {
		t.Fatalf("record request = %+v, want expected version and actor ref", recorded)
	}
	var body generated.OwnerInboxRespondResponse
	decodeJSON(t, rec, &body)
	if body.Response.ResponseAction != generated.RequestChanges || body.Response.ResponseSummary == nil {
		t.Fatalf("response = %+v, want request_changes summary", body.Response)
	}
	if strings.Contains(rec.Body.String(), "raw_payload") {
		t.Fatalf("response leaked raw payload marker: %s", rec.Body.String())
	}
}

func TestRouterRespondOwnerInboxItemMapsSafeActions(t *testing.T) {
	cases := map[string]interactionsv1.InteractionResponseAction{
		"approve":         interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_APPROVE,
		"reject":          interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REJECT,
		"request_changes": interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES,
		"answer":          interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_ANSWER,
		"defer":           interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_DEFER,
	}
	for action, want := range cases {
		t.Run(action, func(t *testing.T) {
			requestID := uuid.NewString()
			client := &fakeInteractionHubClient{recordResponse: sampleInteractionResponseResponse(requestID, want)}
			router := newTestRouter(t, client)
			payload := `{"action":"` + action + `","expected_version":3,"command_id":"cmd-1","response_summary":"Безопасная сводка"}`
			req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+requestID+"/response", payload)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
			if got := client.recordRequest.GetResponseAction(); got != want {
				t.Fatalf("response action = %s, want %s", got, want)
			}
		})
	}
}

func TestRouterRejectsUnknownAction(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	payload := `{"action":"ship_it","expected_version":3,"idempotency_key":"owner-response-1"}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if client.recordRequest != nil {
		t.Fatalf("downstream was called for unknown action")
	}
}

func TestRouterRejectsIncompleteAssigneeRef(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1&assignee_ref=owner-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.listRequest != nil {
		t.Fatalf("downstream was called for incomplete assignee ref")
	}
}

func TestRouterOpenAPIValidationRejectsInvalidQueryBoolean(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1&include_diagnostics=not-bool", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.listRequest != nil {
		t.Fatalf("downstream was called after OpenAPI validation failure")
	}
}

func TestRouterRejectsMissingCommandIdAndIdempotencyKey(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	payload := `{"action":"approve","expected_version":3}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.recordRequest != nil {
		t.Fatalf("downstream was called after OpenAPI validation failure")
	}
}

func TestRouterOpenAPIValidationRejectsWrongContentType(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	payload := `{"action":"approve","expected_version":3,"idempotency_key":"owner-response-1"}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.recordRequest != nil {
		t.Fatalf("downstream was called after OpenAPI validation failure")
	}
}

func TestRouterRejectsTrailingJSONBody(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	payload := `{"action":"approve","expected_version":3,"idempotency_key":"owner-response-1"}{}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.recordRequest != nil {
		t.Fatalf("downstream was called for trailing JSON body")
	}
}

func TestRouterRejectsRawPayloadField(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	payload := `{"action":"answer","expected_version":3,"idempotency_key":"owner-response-1","response_summary":"ok","raw_payload":{"secret":"value"}}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if client.recordRequest != nil {
		t.Fatalf("downstream was called for raw payload field")
	}
}

func TestRouterMapsStaleVersion(t *testing.T) {
	client := &fakeInteractionHubClient{recordErr: status.Error(codes.Aborted, "stale")}
	router := newTestRouter(t, client)
	payload := `{"action":"approve","expected_version":2,"idempotency_key":"owner-response-1"}`
	req := authenticatedRequest(http.MethodPost, "/v1/owner-inbox/items/"+uuid.NewString()+"/response", payload)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusConflict, generated.SafeErrorCodeStaleVersion)
}

func TestRouterMapsPermissionDenied(t *testing.T) {
	client := &fakeInteractionHubClient{listErr: status.Error(codes.PermissionDenied, "denied")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusForbidden, generated.SafeErrorCodePermissionDenied)
}

func TestRouterMapsNotFound(t *testing.T) {
	client := &fakeInteractionHubClient{getErr: status.Error(codes.NotFound, "missing")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items/"+uuid.NewString()+"?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusNotFound, generated.SafeErrorCodeNotFound)
}

func TestRouterMapsRateLimit(t *testing.T) {
	client := &fakeInteractionHubClient{listErr: status.Error(codes.ResourceExhausted, "limited")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusTooManyRequests, generated.SafeErrorCodeRateLimited)
}

func TestRouterMapsDownstreamUnavailable(t *testing.T) {
	client := &fakeInteractionHubClient{getErr: status.Error(codes.Unavailable, "unavailable")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items/"+uuid.NewString()+"?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, generated.SafeErrorCodeDownstreamUnavailable)
}

func TestRouterGetAgentRunRuntimeStatus(t *testing.T) {
	runID := uuid.NewString()
	client := &fakeInteractionHubClient{runtimeStatusResponse: sampleAgentRunRuntimeStatusResponse(runID)}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+runID+"/runtime-status", "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.runtimeStatusRequest
	if recorded.GetRunId() != runID || recorded.GetMeta().GetActor().GetId() != "owner-1" {
		t.Fatalf("runtime status request = %+v, want run id and actor", recorded)
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" || recorded.GetMeta().GetRequestContext().GetSessionId() != "session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.AgentRunRuntimeStatusResponse
	decodeJSON(t, rec, &body)
	if body.RuntimeStatus.RunId != runID || body.RuntimeStatus.RuntimeJobRef == nil || *body.RuntimeStatus.RuntimeJobRef != "runtime-job-1" {
		t.Fatalf("runtime status = %+v, want run and job refs", body.RuntimeStatus)
	}
	if body.RuntimeStatus.RuntimeJobStatus != generated.AgentRuntimeJobStatusRunning || !body.RuntimeStatus.HumanGateWaiting {
		t.Fatalf("runtime status = %+v, want running job and human gate waiting", body.RuntimeStatus)
	}
	for _, forbidden := range []string{"prompt body", "secret-token", "kubeconfig", "workspace/path", "provider_payload", "raw log tail"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterListAgentSessions(t *testing.T) {
	sessionID := uuid.NewString()
	runID := uuid.NewString()
	client := &fakeInteractionHubClient{sessionListResponse: sampleAgentSessionListResponse(sessionID, runID)}
	router := newTestRouter(t, client)
	target := "/v1/agent-sessions?scope_type=project&scope_ref=project-1&status=waiting" +
		"&provider_work_item_ref=issue-1&created_by_actor_ref=user/owner-1" +
		"&created_after=2026-05-28T11:00:00Z&created_before=2026-05-28T13:00:00Z" +
		"&page_size=10&page_token=cursor-1"
	req := authenticatedRequest(http.MethodGet, target, "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "browser-session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.sessionListRequest
	if recorded.GetScope().GetRef() != "project-1" || recorded.GetStatus() != agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_WAITING {
		t.Fatalf("session list request = %+v, want scope and waiting status", recorded)
	}
	if recorded.GetProviderWorkItemRef() != "issue-1" || recorded.GetCreatedByActorRef() != "user/owner-1" {
		t.Fatalf("session filters = %+v, want provider and actor filters", recorded)
	}
	if recorded.GetPage().GetPageSize() != 10 || recorded.GetPage().GetPageToken() != "cursor-1" {
		t.Fatalf("page = %+v, want pagination", recorded.GetPage())
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" || recorded.GetMeta().GetRequestContext().GetSessionId() != "browser-session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.AgentSessionListResponse
	decodeJSON(t, rec, &body)
	if len(body.Sessions) != 1 || body.Sessions[0].SessionId != sessionID || body.Sessions[0].LatestRunId == nil || *body.Sessions[0].LatestRunId != runID {
		t.Fatalf("sessions = %+v, want one session with latest run", body.Sessions)
	}
	if body.Sessions[0].LatestActivity == nil || body.Sessions[0].LatestActivity.PayloadDigest == nil {
		t.Fatalf("session = %+v, want latest safe activity summary", body.Sessions[0])
	}
	if body.Page.NextPageToken == nil || *body.Page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %+v, want next cursor", body.Page)
	}
	for _, forbidden := range []string{"raw prompt", "secret-token", "provider_payload", "workspace/path", "stdout", "stderr"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterListAgentRunSummaries(t *testing.T) {
	sessionID := uuid.NewString()
	runID := uuid.NewString()
	roleID := uuid.NewString()
	client := &fakeInteractionHubClient{runSummaryListResponse: sampleAgentRunSummaryListResponse(sessionID, runID, roleID)}
	router := newTestRouter(t, client)
	target := "/v1/agent-runs?scope_type=project&scope_ref=project-1&session_id=" + sessionID +
		"&role_profile_id=" + roleID + "&status=running&provider_work_item_ref=issue-1" +
		"&provider_pull_request_ref=pr-1&created_after=2026-05-28T11:00:00Z" +
		"&created_before=2026-05-28T13:00:00Z&page_size=20&page_token=cursor-1"
	req := authenticatedRequest(http.MethodGet, target, "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "browser-session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.runSummaryListRequest
	if recorded.GetScope().GetRef() != "project-1" || recorded.GetSessionId() != sessionID || recorded.GetRoleProfileId() != roleID {
		t.Fatalf("run summary request = %+v, want scope/session/role", recorded)
	}
	if recorded.GetStatus() != agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING ||
		recorded.GetProviderWorkItemRef() != "issue-1" ||
		recorded.GetProviderPullRequestRef() != "pr-1" {
		t.Fatalf("run filters = %+v, want status/provider filters", recorded)
	}
	if recorded.GetPage().GetPageSize() != 20 || recorded.GetPage().GetPageToken() != "cursor-1" {
		t.Fatalf("page = %+v, want pagination", recorded.GetPage())
	}
	var body generated.AgentRunSummaryListResponse
	decodeJSON(t, rec, &body)
	if len(body.Runs) != 1 || body.Runs[0].RunId != runID || body.Runs[0].RuntimeJobRef == nil {
		t.Fatalf("runs = %+v, want one run with runtime job ref", body.Runs)
	}
	if body.Runs[0].ProviderTarget == nil || body.Runs[0].ProviderTarget.WorkItemRef == nil {
		t.Fatalf("run = %+v, want provider target refs", body.Runs[0])
	}
	for _, forbidden := range []string{"raw prompt", "secret-token", "provider_payload", "workspace/path", "stdout", "stderr"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterListAgentRunsRejectsUnsupportedScope(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs?scope_type=service&scope_ref=service-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.runSummaryListRequest != nil {
		t.Fatalf("downstream was called for unsupported agent scope")
	}
}

func TestRouterListAgentRunSummariesErrorMapping(t *testing.T) {
	client := &fakeInteractionHubClient{runSummaryListErr: status.Error(codes.Unavailable, "unavailable")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, generated.SafeErrorCodeDownstreamUnavailable)
}

func TestRouterGetAgentRunRuntimeStatusUnknownStatusFallsBackToUnspecified(t *testing.T) {
	runID := uuid.NewString()
	response := sampleAgentRunRuntimeStatusResponse(runID)
	response.RuntimeStatus.RunStatus = agentsv1.AgentRunStatus(99)
	response.RuntimeStatus.ObservationState = agentsv1.AgentRunRuntimeObservationState(99)
	response.RuntimeStatus.RuntimeJobStatus = agentsv1.AgentRuntimeJobStatus(99)
	client := &fakeInteractionHubClient{runtimeStatusResponse: response}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+runID+"/runtime-status", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.AgentRunRuntimeStatusResponse
	decodeJSON(t, rec, &body)
	if body.RuntimeStatus.RunStatus != generated.AgentRunStatusUnspecified ||
		body.RuntimeStatus.ObservationState != generated.RuntimeObservationStateUnspecified ||
		body.RuntimeStatus.RuntimeJobStatus != generated.AgentRuntimeJobStatusUnspecified {
		t.Fatalf("runtime status = %+v, want unspecified fallbacks", body.RuntimeStatus)
	}
}

func TestRouterGetAgentRunRuntimeStatusErrorMapping(t *testing.T) {
	cases := map[string]struct {
		err        error
		statusCode int
		code       generated.SafeErrorCode
	}{
		"not_found": {
			err:        status.Error(codes.NotFound, "missing"),
			statusCode: http.StatusNotFound,
			code:       generated.SafeErrorCodeNotFound,
		},
		"permission_denied": {
			err:        status.Error(codes.PermissionDenied, "denied"),
			statusCode: http.StatusForbidden,
			code:       generated.SafeErrorCodePermissionDenied,
		},
		"stale": {
			err:        status.Error(codes.Aborted, "stale"),
			statusCode: http.StatusConflict,
			code:       generated.SafeErrorCodeStaleVersion,
		},
		"unavailable": {
			err:        status.Error(codes.Unavailable, "unavailable"),
			statusCode: http.StatusServiceUnavailable,
			code:       generated.SafeErrorCodeDownstreamUnavailable,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			client := &fakeInteractionHubClient{runtimeStatusErr: testCase.err}
			router := newTestRouter(t, client)
			req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+uuid.NewString()+"/runtime-status", "")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertErrorCode(t, rec, testCase.statusCode, testCase.code)
		})
	}
}

func TestRouterGetAgentRunRuntimeStatusEmptyDownstreamResponse(t *testing.T) {
	client := &fakeInteractionHubClient{runtimeStatusResponse: &agentsv1.AgentRunRuntimeStatusResponse{}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+uuid.NewString()+"/runtime-status", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, generated.SafeErrorCodeDownstreamUnavailable)
}

func TestRouterListAgentRunActivities(t *testing.T) {
	runID := uuid.NewString()
	client := &fakeInteractionHubClient{activitiesResponse: &agentsv1.ListAgentActivitiesResponse{
		Activities: []*agentsv1.AgentActivity{sampleAgentActivity(runID)},
		Page:       &agentsv1.PageResponse{NextPageToken: stringPtr("cursor-2")},
	}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+runID+"/activities?activity_kind=tool_use&status=succeeded&page_size=10&page_token=cursor-1", "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.activitiesRequest
	if recorded.GetRunId() != runID || recorded.GetActivityKind() != agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE {
		t.Fatalf("activities request = %+v, want run and kind filters", recorded)
	}
	if recorded.GetStatus() != agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED ||
		recorded.GetPage().GetPageSize() != 10 ||
		recorded.GetPage().GetPageToken() != "cursor-1" {
		t.Fatalf("activities filter/page = %+v, want status and pagination", recorded)
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" || recorded.GetMeta().GetRequestContext().GetSessionId() != "session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.AgentRunActivitiesResponse
	decodeJSON(t, rec, &body)
	if body.RunId == nil || *body.RunId != runID || len(body.Activities) != 1 {
		t.Fatalf("activities response = %+v, want run id and one activity", body)
	}
	activity := body.Activities[0]
	if activity.ActivityKind != generated.AgentActivityKindToolUse || activity.Status != generated.AgentActivityStatusSucceeded {
		t.Fatalf("activity = %+v, want tool_use/succeeded", activity)
	}
	if activity.ToolName == nil || *activity.ToolName != "apply_patch" || activity.PayloadDigest == nil {
		t.Fatalf("activity = %+v, want tool metadata and digest", activity)
	}
	if body.Page.NextPageToken == nil || *body.Page.NextPageToken != "cursor-2" {
		t.Fatalf("page = %+v, want cursor", body.Page)
	}
	for _, forbidden := range []string{"raw_tool_input", "raw_tool_output", "stdout", "stderr", "prompt body", "secret-token", "workspace/path", "provider_payload", "large log tail"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterListAgentRunActivitiesUnknownStatusFallsBackToUnspecified(t *testing.T) {
	runID := uuid.NewString()
	activity := sampleAgentActivity(runID)
	activity.ActivityKind = agentsv1.AgentActivityKind(99)
	activity.Status = agentsv1.AgentActivityStatus(99)
	client := &fakeInteractionHubClient{activitiesResponse: &agentsv1.ListAgentActivitiesResponse{Activities: []*agentsv1.AgentActivity{activity}}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+runID+"/activities", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.AgentRunActivitiesResponse
	decodeJSON(t, rec, &body)
	if body.Activities[0].ActivityKind != generated.AgentActivityKindUnspecified || body.Activities[0].Status != generated.AgentActivityStatusUnspecified {
		t.Fatalf("activity = %+v, want unspecified fallbacks", body.Activities[0])
	}
}

func TestRouterListAgentRunActivitiesRejectsBadRequest(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+uuid.NewString()+"/activities?activity_kind=raw_tool_input", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.activitiesRequest != nil {
		t.Fatalf("downstream was called after OpenAPI validation failure")
	}
}

func TestRouterListAgentRunActivitiesErrorMapping(t *testing.T) {
	cases := map[string]struct {
		err        error
		statusCode int
		code       generated.SafeErrorCode
	}{
		"not_found": {
			err:        status.Error(codes.NotFound, "missing"),
			statusCode: http.StatusNotFound,
			code:       generated.SafeErrorCodeNotFound,
		},
		"permission_denied": {
			err:        status.Error(codes.PermissionDenied, "denied"),
			statusCode: http.StatusForbidden,
			code:       generated.SafeErrorCodePermissionDenied,
		},
		"unavailable": {
			err:        status.Error(codes.Unavailable, "unavailable"),
			statusCode: http.StatusServiceUnavailable,
			code:       generated.SafeErrorCodeDownstreamUnavailable,
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			client := &fakeInteractionHubClient{activitiesErr: testCase.err}
			router := newTestRouter(t, client)
			req := authenticatedRequest(http.MethodGet, "/v1/agent-runs/"+uuid.NewString()+"/activities", "")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertErrorCode(t, rec, testCase.statusCode, testCase.code)
		})
	}
}

func TestRouterGetGovernanceSummaryByReleasePackage(t *testing.T) {
	packageID := uuid.NewString()
	client := &fakeInteractionHubClient{governanceSummaryResponse: sampleGovernanceSummaryResponse(packageID)}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/governance/summary?release_decision_package_id="+packageID, "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.governanceSummaryRequest
	if recorded.GetScope().GetReleaseDecisionPackageId() != packageID || recorded.GetMeta().GetActor().GetId() != "owner-1" {
		t.Fatalf("governance request = %+v, want package scope and actor", recorded)
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" || recorded.GetMeta().GetRequestContext().GetSessionId() != "session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.GovernanceSummaryResponse
	decodeJSON(t, rec, &body)
	if body.Summary.Scope.ReleaseDecisionPackageId == nil || *body.Summary.Scope.ReleaseDecisionPackageId != packageID {
		t.Fatalf("scope = %+v, want package id", body.Summary.Scope)
	}
	if len(body.Summary.PendingDecisions) != 1 || body.Summary.PendingDecisions[0].Kind != generated.GovernanceDecisionSummaryKindGateRequest {
		t.Fatalf("pending decisions = %+v, want gate request", body.Summary.PendingDecisions)
	}
	if body.Summary.PendingDecisions[0].AgentContext == nil || body.Summary.PendingDecisions[0].AgentContext.RunRef == nil {
		t.Fatalf("pending decision = %+v, want agent context", body.Summary.PendingDecisions[0])
	}
	if len(body.Summary.EvidenceSummaries) != 1 || body.Summary.EvidenceSummaries[0].SourceKind != "agent.acceptance" {
		t.Fatalf("evidence summaries = %+v, want agent acceptance", body.Summary.EvidenceSummaries)
	}
	for _, forbidden := range []string{"raw payload", "secret-token", "prompt body", "workspace/path", "stdout"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterGetGovernanceSummaryRejectsMixedSelectors(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/governance/summary?release_decision_package_id="+uuid.NewString()+"&target_type=pull_request&target_ref=pr-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.governanceSummaryRequest != nil {
		t.Fatalf("downstream was called for mixed governance selectors")
	}
}

func TestRouterGetGovernanceSummaryMapsDownstreamUnavailable(t *testing.T) {
	client := &fakeInteractionHubClient{governanceSummaryErr: status.Error(codes.Unavailable, "unavailable")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/governance/summary?release_decision_package_id="+uuid.NewString(), "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, generated.SafeErrorCodeDownstreamUnavailable)
}

func TestRouterGetSelfDeploySummary(t *testing.T) {
	client := &fakeInteractionHubClient{selfDeployListResponse: sampleSelfDeployPlansResponse()}
	router := newTestRouter(t, client)
	target := "/v1/self-deploy/summary?scope_type=project&scope_ref=project-1" +
		"&project_ref=project-1&repository_ref=repo-1&provider_signal_ref=provider-signal-1" +
		"&status=pending_approval"
	req := authenticatedRequest(http.MethodGet, target, "")
	req.Header.Set(headerTraceID, "trace-1")
	req.Header.Set(headerSessionID, "browser-session-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	recorded := client.selfDeployListRequest
	if recorded.GetScope().GetRef() != "project-1" || recorded.GetProjectRef() != "project-1" || recorded.GetRepositoryRef() != "repo-1" {
		t.Fatalf("self-deploy request = %+v, want scope/project/repository filters", recorded)
	}
	if recorded.GetProviderSignalRef() != "provider-signal-1" ||
		recorded.GetStatus() != agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL ||
		recorded.GetPage().GetPageSize() != selfDeploySummaryPageSize {
		t.Fatalf("self-deploy request = %+v, want signal/status/page", recorded)
	}
	if recorded.GetMeta().GetRequestContext().GetTraceId() != "trace-1" ||
		recorded.GetMeta().GetRequestContext().GetSessionId() != "browser-session-1" {
		t.Fatalf("request context = %+v, want trace and session", recorded.GetMeta().GetRequestContext())
	}
	var body generated.SelfDeploySummaryResponse
	decodeJSON(t, rec, &body)
	summary := body.Summary
	if summary.Availability != generated.SelfDeploySummaryAvailabilityReady ||
		summary.ChainStatus != generated.GovernanceGatePending ||
		summary.NextStep.Code != generated.ReviewGovernanceGate ||
		summary.ProviderSignal.Status != generated.SelfDeployProviderSignalStatusStoredRef ||
		summary.DeployPlan.Status != generated.SelfDeployPlanStatusPendingApproval ||
		summary.Governance.Status != generated.SelfDeployGovernanceStatusPending {
		t.Fatalf("summary = %+v, want ready/stored_ref/pending", summary)
	}
	if summary.ProjectRef == nil || *summary.ProjectRef != "project-1" ||
		summary.RepositoryRef == nil || *summary.RepositoryRef != "repo-1" ||
		summary.ServicesYamlDigest == nil || *summary.ServicesYamlDigest != "sha256:services-yaml" ||
		summary.PlanFingerprint == nil || *summary.PlanFingerprint != "sha256:plan-fingerprint" {
		t.Fatalf("summary refs = %+v, want project/repository/digests", summary)
	}
	if len(summary.AffectedServiceKeys) != 2 || len(summary.PathCategories) != 2 ||
		summary.PathCategories[0] != generated.SelfDeployPathCategoryServiceSource {
		t.Fatalf("summary = %+v, want affected services and path categories", summary)
	}
	for _, forbidden := range []string{"raw webhook", "provider response", "full services yaml", "secret-token", "OAuth state", "diff --git"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterGetSelfDeploySummaryEmptyReturnsUnavailable(t *testing.T) {
	client := &fakeInteractionHubClient{selfDeployListResponse: &agentsv1.ListSelfDeployPlansResponse{}}
	router := newTestRouter(t, client)
	projectID := uuid.NewString()
	req := authenticatedRequest(http.MethodGet, "/v1/self-deploy/summary?scope_type=project&scope_ref="+projectID, "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.SelfDeploySummaryResponse
	decodeJSON(t, rec, &body)
	if body.Summary.Availability != generated.SelfDeploySummaryAvailabilityUnavailable ||
		body.Summary.ChainStatus != generated.WaitingForProviderSignal ||
		body.Summary.NextStep.Code != generated.WaitProviderSignal ||
		body.Summary.ProviderSignal.Status != generated.SelfDeployProviderSignalStatusUnavailable ||
		body.Summary.DeployPlan.Status != generated.SelfDeployPlanStatusUnavailable ||
		body.Summary.SafeError == nil {
		t.Fatalf("summary = %+v, want unavailable states", body.Summary)
	}
}

func TestRouterGetSelfDeploySummaryWithoutProjectIsNotConfigured(t *testing.T) {
	client := &fakeInteractionHubClient{selfDeployListResponse: &agentsv1.ListSelfDeployPlansResponse{}}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/self-deploy/summary?scope_type=organization&scope_ref=org-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.SelfDeploySummaryResponse
	decodeJSON(t, rec, &body)
	if body.Summary.ChainStatus != generated.NotConfigured ||
		body.Summary.NextStep.Code != generated.ConfigureProject ||
		body.Summary.SafeError == nil ||
		body.Summary.SafeError.Code != "self_deploy_project_not_configured" {
		t.Fatalf("summary = %+v, want not_configured", body.Summary)
	}
}

func TestRouterGetSelfDeploySummaryRepositoryBindingMissing(t *testing.T) {
	client := &fakeInteractionHubClient{
		selfDeployListResponse: &agentsv1.ListSelfDeployPlansResponse{},
		selfDeploySignalResponse: &projectsv1.SelfDeploySignalResponse{
			Status:     projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_REPOSITORY_BINDING_NOT_FOUND,
			SafeReason: stringPtr("repository_binding_not_found"),
		},
	}
	router := newTestRouter(t, client)
	projectID := uuid.NewString()
	target := "/v1/self-deploy/summary?scope_type=project&scope_ref=" + projectID +
		"&project_ref=" + projectID + "&provider_signal_ref=provider-signal-1"
	req := authenticatedRequest(http.MethodGet, target, "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if client.selfDeploySignalRequest.GetProjectId() != projectID ||
		client.selfDeploySignalRequest.GetProviderSignalKey() != "provider-signal-1" {
		t.Fatalf("signal request = %+v, want project and provider signal key", client.selfDeploySignalRequest)
	}
	var body generated.SelfDeploySummaryResponse
	decodeJSON(t, rec, &body)
	if body.Summary.ChainStatus != generated.RepositoryBindingMissing ||
		body.Summary.NextStep.Code != generated.BindRepository ||
		body.Summary.SafeError == nil ||
		body.Summary.SafeError.Code != "repository_binding_missing" {
		t.Fatalf("summary = %+v, want repository_binding_missing", body.Summary)
	}
}

func TestRouterGetSelfDeploySummaryNeedsServicesPolicyReconcile(t *testing.T) {
	projectID := uuid.NewString()
	repositoryID := uuid.NewString()
	client := &fakeInteractionHubClient{
		selfDeployListResponse: &agentsv1.ListSelfDeployPlansResponse{},
		selfDeploySignalResponse: &projectsv1.SelfDeploySignalResponse{
			Status:     projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_SERVICES_POLICY_RECONCILE,
			SafeReason: stringPtr("services_policy_commit_not_reconciled"),
			Signal: &projectsv1.SelfDeploySignal{
				ProviderSignalRef:        "provider-signal-1",
				ProjectRef:               projectID,
				RepositoryRef:            repositoryID,
				SourceRef:                "refs/heads/main",
				MergeCommitSha:           "0123456789abcdef",
				AffectedServiceKeys:      []string{"staff-gateway"},
				PathCategories:           []*projectsv1.SelfDeployPathCategoryCount{{Category: projectsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICES_POLICY, Count: 1}},
				ExpectedRuntimeJobTypes:  []projectsv1.SelfDeployExpectedRuntimeJobType{projectsv1.SelfDeployExpectedRuntimeJobType_SELF_DEPLOY_EXPECTED_RUNTIME_JOB_TYPE_BUILD},
				ProjectSignalFingerprint: "sha256:project-signal",
				SafeSummary:              "services.yaml требует reconcile",
				ServicesYaml: &projectsv1.SelfDeployServicesYamlProjection{
					ServicesYamlRef:    "project-catalog:services-policy:policy-1:services.yaml",
					ServicesYamlDigest: "sha256:services",
				},
				Version: 2,
			},
		},
	}
	router := newTestRouter(t, client)
	target := "/v1/self-deploy/summary?scope_type=project&scope_ref=" + projectID +
		"&project_ref=" + projectID + "&repository_ref=" + repositoryID + "&provider_signal_ref=provider-signal-1"
	req := authenticatedRequest(http.MethodGet, target, "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.SelfDeploySummaryResponse
	decodeJSON(t, rec, &body)
	if body.Summary.ChainStatus != generated.NeedsServicesPolicyReconcile ||
		body.Summary.NextStep.Code != generated.ReconcileServicesPolicy ||
		len(body.Summary.PathCategories) != 1 ||
		body.Summary.PathCategories[0] != generated.SelfDeployPathCategoryServicesPolicy ||
		body.Summary.ServicesYamlDigest == nil ||
		*body.Summary.ServicesYamlDigest != "sha256:services" {
		t.Fatalf("summary = %+v, want services policy reconcile with safe refs", body.Summary)
	}
	for _, forbidden := range []string{"raw webhook", "provider response", "full services yaml", "secret-token", "OAuth state", "diff --git"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("response leaked %q marker: %s", forbidden, rec.Body.String())
		}
	}
}

func TestRouterGetSelfDeploySummaryRejectsUnsupportedScope(t *testing.T) {
	client := &fakeInteractionHubClient{}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/self-deploy/summary?scope_type=service&scope_ref=service-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusBadRequest, generated.SafeErrorCodeInvalidRequest)
	if client.selfDeployListRequest != nil {
		t.Fatalf("downstream was called for unsupported agent scope")
	}
}

func TestRouterGetSelfDeploySummaryMapsDownstreamUnavailable(t *testing.T) {
	client := &fakeInteractionHubClient{selfDeployListErr: status.Error(codes.Unavailable, "unavailable")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/self-deploy/summary?scope_type=project&scope_ref=project-1", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusServiceUnavailable, generated.SafeErrorCodeDownstreamUnavailable)
}

func TestRouterRequiresActorContext(t *testing.T) {
	router := newTestRouter(t, &fakeInteractionHubClient{})
	req := httptest.NewRequest(http.MethodGet, "/v1/owner-inbox/items?scope_type=project&scope_ref=project-1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertErrorCode(t, rec, http.StatusUnauthorized, generated.SafeErrorCodeUnauthenticated)
}

func newTestRouter(t *testing.T, client *fakeInteractionHubClient) *Router {
	t.Helper()
	router, err := NewRouter(context.Background(), Config{
		ServiceName:     "staff-gateway",
		OpenAPISpecPath: "../../../../../../specs/openapi/staff-gateway.v1.yaml",
		RequestTimeout:  time.Second,
		MaxBodyBytes:    65536,
	}, client, client, client, client, nil)
	if err != nil {
		t.Fatalf("NewRouter(): %v", err)
	}
	return router
}

func authenticatedRequest(method string, target string, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set(headerActorType, "user")
	req.Header.Set(headerActorID, "owner-1")
	req.Header.Set(headerRequestID, "request-1")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, output any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), output); err != nil {
		t.Fatalf("decode response JSON: %v body=%s", err, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, statusCode int, code generated.SafeErrorCode) {
	t.Helper()
	if rec.Code != statusCode {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body generated.SafeError
	decodeJSON(t, rec, &body)
	if body.Code != code {
		t.Fatalf("error code = %s, want %s", body.Code, code)
	}
}

type fakeInteractionHubClient struct {
	listRequest               *interactionsv1.ListOwnerInboxItemsRequest
	getRequest                *interactionsv1.GetOwnerInboxItemRequest
	recordRequest             *interactionsv1.RecordInteractionResponseRequest
	sessionListRequest        *agentsv1.ListAgentSessionsRequest
	runSummaryListRequest     *agentsv1.ListAgentRunSummariesRequest
	runtimeStatusRequest      *agentsv1.GetAgentRunRuntimeStatusRequest
	activitiesRequest         *agentsv1.ListAgentActivitiesRequest
	selfDeployListRequest     *agentsv1.ListSelfDeployPlansRequest
	selfDeploySignalRequest   *projectsv1.GetSelfDeploySignalRequest
	repositoryListRequest     *projectsv1.ListRepositoriesRequest
	governanceSummaryRequest  *governancev1.GetGovernanceSummaryRequest
	listResponse              *interactionsv1.ListOwnerInboxItemsResponse
	getResponse               *interactionsv1.OwnerInboxItemResponse
	recordResponse            *interactionsv1.InteractionResponseResponse
	sessionListResponse       *agentsv1.ListAgentSessionsResponse
	runSummaryListResponse    *agentsv1.ListAgentRunSummariesResponse
	runtimeStatusResponse     *agentsv1.AgentRunRuntimeStatusResponse
	activitiesResponse        *agentsv1.ListAgentActivitiesResponse
	selfDeployListResponse    *agentsv1.ListSelfDeployPlansResponse
	selfDeploySignalResponse  *projectsv1.SelfDeploySignalResponse
	repositoryListResponse    *projectsv1.ListRepositoriesResponse
	governanceSummaryResponse *governancev1.GovernanceSummaryResponse
	listErr                   error
	getErr                    error
	recordErr                 error
	sessionListErr            error
	runSummaryListErr         error
	runtimeStatusErr          error
	activitiesErr             error
	selfDeployListErr         error
	selfDeploySignalErr       error
	repositoryListErr         error
	governanceSummaryErr      error
}

func (f *fakeInteractionHubClient) ListOwnerInboxItems(_ context.Context, request *interactionsv1.ListOwnerInboxItemsRequest) (*interactionsv1.ListOwnerInboxItemsResponse, error) {
	f.listRequest = request
	return f.listResponse, f.listErr
}

func (f *fakeInteractionHubClient) GetOwnerInboxItem(_ context.Context, request *interactionsv1.GetOwnerInboxItemRequest) (*interactionsv1.OwnerInboxItemResponse, error) {
	f.getRequest = request
	return f.getResponse, f.getErr
}

func (f *fakeInteractionHubClient) RecordInteractionResponse(_ context.Context, request *interactionsv1.RecordInteractionResponseRequest) (*interactionsv1.InteractionResponseResponse, error) {
	f.recordRequest = request
	return f.recordResponse, f.recordErr
}

func (f *fakeInteractionHubClient) ListAgentSessions(_ context.Context, request *agentsv1.ListAgentSessionsRequest) (*agentsv1.ListAgentSessionsResponse, error) {
	f.sessionListRequest = request
	return f.sessionListResponse, f.sessionListErr
}

func (f *fakeInteractionHubClient) ListAgentRunSummaries(_ context.Context, request *agentsv1.ListAgentRunSummariesRequest) (*agentsv1.ListAgentRunSummariesResponse, error) {
	f.runSummaryListRequest = request
	return f.runSummaryListResponse, f.runSummaryListErr
}

func (f *fakeInteractionHubClient) GetAgentRunRuntimeStatus(_ context.Context, request *agentsv1.GetAgentRunRuntimeStatusRequest) (*agentsv1.AgentRunRuntimeStatusResponse, error) {
	f.runtimeStatusRequest = request
	return f.runtimeStatusResponse, f.runtimeStatusErr
}

func (f *fakeInteractionHubClient) ListAgentActivities(_ context.Context, request *agentsv1.ListAgentActivitiesRequest) (*agentsv1.ListAgentActivitiesResponse, error) {
	f.activitiesRequest = request
	return f.activitiesResponse, f.activitiesErr
}

func (f *fakeInteractionHubClient) ListSelfDeployPlans(_ context.Context, request *agentsv1.ListSelfDeployPlansRequest) (*agentsv1.ListSelfDeployPlansResponse, error) {
	f.selfDeployListRequest = request
	return f.selfDeployListResponse, f.selfDeployListErr
}

func (f *fakeInteractionHubClient) GetSelfDeploySignal(_ context.Context, request *projectsv1.GetSelfDeploySignalRequest) (*projectsv1.SelfDeploySignalResponse, error) {
	f.selfDeploySignalRequest = request
	return f.selfDeploySignalResponse, f.selfDeploySignalErr
}

func (f *fakeInteractionHubClient) ListRepositories(_ context.Context, request *projectsv1.ListRepositoriesRequest) (*projectsv1.ListRepositoriesResponse, error) {
	f.repositoryListRequest = request
	return f.repositoryListResponse, f.repositoryListErr
}

func (f *fakeInteractionHubClient) GetGovernanceSummary(_ context.Context, request *governancev1.GetGovernanceSummaryRequest) (*governancev1.GovernanceSummaryResponse, error) {
	f.governanceSummaryRequest = request
	return f.governanceSummaryResponse, f.governanceSummaryErr
}

func sampleOwnerInboxItem(status interactionsv1.InteractionRequestStatus) *interactionsv1.OwnerInboxItem {
	now := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &interactionsv1.OwnerInboxItem{
		RequestId:     uuid.NewString(),
		RequestKind:   interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE,
		RequestStatus: status,
		Scope: &interactionsv1.ScopeRef{
			Type: interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT,
			Ref:  "project-1",
		},
		Requester: &interactionsv1.SourceOwnerRef{
			Kind: interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER,
			Ref:  stringPtr("run-1"),
		},
		DecisionOwner: &interactionsv1.DecisionOwnerRef{
			OwnerKind:       interactionsv1.DecisionOwnerKind_DECISION_OWNER_KIND_AGENT_MANAGER,
			OwnerRequestRef: "gate-1",
		},
		AssigneeRefs: []*interactionsv1.ActorRef{{RefKind: "user", Ref: "owner-1"}},
		ContextRefs:  []*interactionsv1.ExternalRef{{RefKind: "agent_run", Ref: "run-1"}},
		Title:        "Проверить решение",
		Summary:      "Безопасная сводка",
		DeliverySummary: &interactionsv1.OwnerInboxDeliverySummary{
			AttemptCount:     1,
			LatestStatus:     interactionsv1.DeliveryAttemptStatus_DELIVERY_ATTEMPT_STATUS_SENT,
			LatestErrorClass: interactionsv1.DeliveryErrorClass_DELIVERY_ERROR_CLASS_UNSPECIFIED,
		},
		CreatedAt: now,
		UpdatedAt: now,
		Version:   3,
		AllowedActions: []*interactionsv1.InteractionAction{
			{ActionKey: "approve", IsTerminal: true},
			{ActionKey: "reject", IsTerminal: true},
			{ActionKey: "request_changes", IsTerminal: true},
		},
	}
}

func sampleOwnerInboxItemWithDiagnostics(status interactionsv1.InteractionRequestStatus) *interactionsv1.OwnerInboxItem {
	item := sampleOwnerInboxItem(status)
	now := time.Date(2026, 5, 28, 12, 2, 0, 0, time.UTC).Format(time.RFC3339Nano)
	item.LatestCallback = &interactionsv1.OwnerInboxCallbackSummary{
		CallbackRef:      "callback/ref-1",
		CallbackId:       uuid.NewString(),
		DeliveryId:       stringPtr(uuid.NewString()),
		SignatureStatus:  interactionsv1.CallbackSignatureStatus_CALLBACK_SIGNATURE_STATUS_TRUSTED_INTERNAL,
		ProcessingStatus: interactionsv1.CallbackProcessingStatus_CALLBACK_PROCESSING_STATUS_ACCEPTED,
		ActorRef:         stringPtr("user/owner-1"),
		Action:           stringPtr("request_changes"),
		ReceivedAt:       now,
		GatewayRef:       stringPtr("staff-gateway/request-1"),
		CorrelationId:    stringPtr("corr-1"),
	}
	item.LatestResponse = &interactionsv1.OwnerInboxResponseSummary{
		ResponseId:             uuid.NewString(),
		ResponseAction:         interactionsv1.InteractionResponseAction_INTERACTION_RESPONSE_ACTION_REQUEST_CHANGES,
		RespondedByActorRef:    "user/owner-1",
		SourceKind:             interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE,
		SourceRef:              stringPtr("staff-gateway/request-1"),
		OwnerDecisionRef:       stringPtr("decision/ref-1"),
		CreatedAt:              now,
		ResponseSummary:        stringPtr("Безопасная сводка без лишних данных"),
		ResponseSummaryDigest:  stringPtr("sha256:digest"),
		InteractionResponseRef: stringPtr("interaction-response/ref-1"),
	}
	item.AllowedActions = nil
	item.ResolvedAt = stringPtr(now)
	item.UpdatedAt = now
	return item
}

func sampleInteractionResponseResponse(requestID string, action interactionsv1.InteractionResponseAction) *interactionsv1.InteractionResponseResponse {
	now := time.Date(2026, 5, 28, 12, 1, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &interactionsv1.InteractionResponseResponse{
		Request: &interactionsv1.InteractionRequest{
			Id:          requestID,
			RequestKind: interactionsv1.InteractionRequestKind_INTERACTION_REQUEST_KIND_HUMAN_GATE,
			Scope: &interactionsv1.ScopeRef{
				Type: interactionsv1.InteractionScopeType_INTERACTION_SCOPE_TYPE_PROJECT,
				Ref:  "project-1",
			},
			SourceOwner: &interactionsv1.SourceOwnerRef{
				Kind: interactionsv1.SourceOwnerKind_SOURCE_OWNER_KIND_AGENT_MANAGER,
				Ref:  stringPtr("run-1"),
			},
			TargetRefs:    []*interactionsv1.ActorRef{{RefKind: "user", Ref: "owner-1"}},
			ContextRefs:   []*interactionsv1.ExternalRef{{RefKind: "agent_run", Ref: "run-1"}},
			PromptSummary: "Безопасная сводка",
			Status:        interactionsv1.InteractionRequestStatus_INTERACTION_REQUEST_STATUS_ANSWERED,
			Version:       4,
			CreatedAt:     now,
			UpdatedAt:     now,
			ResolvedAt:    stringPtr(now),
		},
		Response: &interactionsv1.InteractionResponse{
			Id:                  uuid.NewString(),
			RequestId:           requestID,
			ResponseAction:      action,
			RespondedByActorRef: "user/owner-1",
			ResponseSummary:     stringPtr("Нужна доработка проверки"),
			SourceKind:          interactionsv1.InteractionResponseSourceKind_INTERACTION_RESPONSE_SOURCE_KIND_WEB_CONSOLE,
			SourceRef:           stringPtr("staff-gateway/request-1"),
			CreatedAt:           now,
		},
	}
}

func sampleAgentRunRuntimeStatusResponse(runID string) *agentsv1.AgentRunRuntimeStatusResponse {
	now := time.Date(2026, 5, 28, 12, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.AgentRunRuntimeStatusResponse{
		Run: &agentsv1.AgentRun{
			Id:        runID,
			SessionId: "session-1",
			Status:    agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING,
			Version:   5,
			CreatedAt: now,
			UpdatedAt: now,
		},
		RuntimeStatus: &agentsv1.AgentRunRuntimeStatus{
			RunId:                runID,
			RunStatus:            agentsv1.AgentRunStatus_AGENT_RUN_STATUS_STARTING,
			RuntimeContext:       &agentsv1.RuntimeContextRef{SlotRef: stringPtr("slot-1"), WorkspaceRef: stringPtr("workspace/path/hidden"), ContextRef: stringPtr("runtime-context-1")},
			ObservationState:     agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_LIVE,
			RuntimeJobRef:        stringPtr("runtime-job-1"),
			RuntimeJobStatus:     agentsv1.AgentRuntimeJobStatus_AGENT_RUNTIME_JOB_STATUS_RUNNING,
			RuntimeJobCommandRef: stringPtr("runtime-command-1"),
			RuntimeJobVersion:    int64Ptr(8),
			RuntimeJobCreatedAt:  stringPtr(now),
			RuntimeJobStartedAt:  stringPtr(now),
			RuntimeJobUpdatedAt:  stringPtr(now),
			SafeSummary:          stringPtr("job_status=running"),
			RunStartedAt:         stringPtr(now),
			RunUpdatedAt:         now,
			RunVersion:           5,
			HumanGateWaiting:     true,
			HumanGateRequestRef:  stringPtr("human-gate-1"),
			HumanGateReasonCode:  stringPtr("owner_approval"),
		},
	}
}

func sampleAgentSessionListResponse(sessionID string, runID string) *agentsv1.ListAgentSessionsResponse {
	now := time.Date(2026, 5, 28, 12, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.ListAgentSessionsResponse{
		Sessions: []*agentsv1.AgentSessionListItem{{
			Session: &agentsv1.AgentSession{
				Id:                    sessionID,
				Scope:                 &agentsv1.ScopeRef{Type: agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, Ref: "project-1"},
				ProviderWorkItemRef:   stringPtr("issue-1"),
				FlowVersionId:         stringPtr("flow-version-1"),
				CurrentStageId:        stringPtr("stage-1"),
				LatestStateSnapshotId: stringPtr("snapshot-1"),
				Status:                agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_WAITING,
				CreatedByActorRef:     "user/owner-1",
				Version:               6,
				CreatedAt:             now,
				UpdatedAt:             now,
			},
			LatestRunId:          stringPtr(runID),
			LatestRunStatus:      agentRunStatusPtr(agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING),
			LatestRuntimeJobRef:  stringPtr("runtime-job-1"),
			LatestRunSafeSummary: stringPtr("Run ожидает владельца"),
			ActiveRunCount:       1,
			HumanGateWaiting:     true,
			HumanGateRequestRef:  stringPtr("human-gate-1"),
			HumanGateReasonCode:  stringPtr("owner_approval"),
			LatestActivity:       sampleAgentActivitySummary(),
			FollowUpWaiting:      true,
			FollowUpRef:          stringPtr("follow-up-1"),
		}},
		Page: &agentsv1.PageResponse{NextPageToken: stringPtr("cursor-2")},
	}
}

func sampleAgentRunSummaryListResponse(sessionID string, runID string, roleID string) *agentsv1.ListAgentRunSummariesResponse {
	now := time.Date(2026, 5, 28, 12, 3, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.ListAgentRunSummariesResponse{
		Runs: []*agentsv1.AgentRunListItem{{
			Run: &agentsv1.AgentRun{
				Id:                      runID,
				SessionId:               sessionID,
				FlowVersionId:           stringPtr("flow-version-1"),
				StageId:                 stringPtr("stage-1"),
				RoleProfileId:           roleID,
				RoleProfileVersion:      2,
				RoleProfileDigest:       "sha256:raw prompt secret-token-not-returned",
				PromptTemplateVersionId: "prompt-version-1",
				PromptTemplateDigest:    "sha256:prompt body not returned",
				RuntimeContext:          &agentsv1.RuntimeContextRef{SlotRef: stringPtr("slot-1"), JobRef: stringPtr("runtime-job-from-run"), WorkspaceRef: stringPtr("workspace/path/hidden"), ContextRef: stringPtr("runtime-context-1")},
				ProviderTarget:          &agentsv1.ProviderTargetRef{WorkItemRef: stringPtr("issue-1"), PullRequestRef: stringPtr("pr-1")},
				Status:                  agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING,
				ResultSummary:           stringPtr("Безопасная сводка Run"),
				FailureCode:             stringPtr(""),
				Version:                 7,
				StartedAt:               stringPtr(now),
				CreatedAt:               now,
				UpdatedAt:               now,
			},
			RuntimeJobRef:           stringPtr("runtime-job-1"),
			RuntimeObservationState: agentsv1.AgentRunRuntimeObservationState_AGENT_RUN_RUNTIME_OBSERVATION_STATE_STORED_REF,
			RuntimeSafeSummary:      stringPtr("runtime ref сохранён"),
			HumanGateWaiting:        true,
			HumanGateRequestRef:     stringPtr("human-gate-1"),
			HumanGateReasonCode:     stringPtr("owner_approval"),
			FollowUpWaiting:         true,
			LatestActivity:          sampleAgentActivitySummary(),
		}},
		Page: &agentsv1.PageResponse{NextPageToken: stringPtr("cursor-2")},
	}
}

func sampleAgentActivitySummary() *agentsv1.AgentActivitySummary {
	now := time.Date(2026, 5, 28, 12, 4, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.AgentActivitySummary{
		ActivityId:    uuid.NewString(),
		ActivityKind:  agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE,
		Status:        agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED,
		ToolName:      stringPtr("apply_patch"),
		ToolCategory:  stringPtr("code_edit"),
		SafeSummary:   stringPtr("Обновлён безопасный контракт"),
		PayloadDigest: stringPtr("sha256:activity-summary"),
		StartedAt:     stringPtr(now),
		FinishedAt:    stringPtr(now),
		UpdatedAt:     now,
		Version:       2,
	}
}

func sampleAgentActivity(runID string) *agentsv1.AgentActivity {
	now := time.Date(2026, 5, 28, 12, 4, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.AgentActivity{
		Id:              uuid.NewString(),
		SessionId:       "session-1",
		RunId:           stringPtr(runID),
		TurnId:          stringPtr("turn-1"),
		ToolUseId:       stringPtr("tool-use-1"),
		ActivityKind:    agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_USE,
		ToolName:        stringPtr("apply_patch"),
		ToolCategory:    stringPtr("code_edit"),
		Status:          agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED,
		StartedAt:       stringPtr(now),
		FinishedAt:      stringPtr(now),
		DurationMs:      int64Ptr(120),
		SafeSummary:     stringPtr("Изменён контракт staff-gateway"),
		PayloadDigest:   stringPtr("sha256:activity-digest"),
		BoundedError:    stringPtr(""),
		SafeRefsJson:    `{"agent_run_ref":"` + runID + `","tool_use_ref":"tool-use-1"}`,
		SafeDetailsJson: `{"display":"safe summary only"}`,
		CorrelationId:   stringPtr("corr-1"),
		IdempotencyKey:  "secret-token-not-returned",
		Version:         7,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func sampleSelfDeployPlansResponse() *agentsv1.ListSelfDeployPlansResponse {
	now := time.Date(2026, 5, 28, 12, 6, 0, 0, time.UTC).Format(time.RFC3339Nano)
	return &agentsv1.ListSelfDeployPlansResponse{
		SelfDeployPlans: []*agentsv1.SelfDeployPlan{{
			Id:                      "self-deploy-plan-1",
			Scope:                   &agentsv1.ScopeRef{Type: agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, Ref: "project-1"},
			ProjectRef:              "project-1",
			RepositoryRef:           "repo-1",
			ProviderSignalRef:       stringPtr("provider-signal-1"),
			SourceRef:               "refs/heads/main",
			MergeCommitSha:          "0123456789abcdef",
			ServicesYamlRef:         stringPtr("object/services-yaml-safe-ref"),
			ServicesYamlDigest:      "sha256:services-yaml",
			AffectedServiceKeys:     []string{"staff-gateway", "web-console"},
			PathCategories:          []agentsv1.SelfDeployPathCategory{agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_SERVICE_SOURCE, agentsv1.SelfDeployPathCategory_SELF_DEPLOY_PATH_CATEGORY_DEPLOY_MANIFEST},
			ExpectedRuntimeJobTypes: []runtimev1.JobType{runtimev1.JobType_JOB_TYPE_BUILD, runtimev1.JobType_JOB_TYPE_DEPLOY},
			GovernanceContext: &agentsv1.GovernanceContextRef{
				GateRequestRef:            stringPtr("gate-request-1"),
				ReleaseDecisionPackageRef: stringPtr("release-package-1"),
			},
			SafeSummary:     stringPtr("Webhook signal сохранён, self-deploy plan ожидает решения владельца."),
			PlanFingerprint: "sha256:plan-fingerprint",
			IdempotencyKey:  "secret-token-not-returned",
			Status:          agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL,
			Version:         4,
			CreatedAt:       now,
			UpdatedAt:       now,
		}},
		Page: &agentsv1.PageResponse{},
	}
}

func sampleGovernanceSummaryResponse(packageID string) *governancev1.GovernanceSummaryResponse {
	now := time.Date(2026, 5, 28, 12, 5, 0, 0, time.UTC).Format(time.RFC3339Nano)
	longUnsafeSummary := strings.Repeat("x", maxGovernanceTextBytes+1) + "secret-token raw payload"
	return &governancev1.GovernanceSummaryResponse{
		Summary: &governancev1.GovernanceSummary{
			Scope: &governancev1.GovernanceSummaryScope{ReleaseDecisionPackageId: stringPtr(packageID)},
			PendingDecisions: []*governancev1.GovernanceDecisionSummary{{
				Kind:                     governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_REQUEST,
				Attention:                governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_PENDING,
				Id:                       "gate-request-1",
				ReleaseDecisionPackageId: stringPtr(packageID),
				GateRequestStatus:        governancev1.GateRequestStatus_GATE_REQUEST_STATUS_AWAITING_DECISION,
				SafeSummary:              "Ожидается решение владельца",
				EvidenceRefs: []*governancev1.EvidenceRef{{
					Kind:    governancev1.EvidenceKind_EVIDENCE_KIND_AGENT_ACCEPTANCE,
					Ref:     "agent-acceptance-1",
					Summary: "Acceptance требует подтверждения владельца",
					Digest:  stringPtr("sha256:acceptance"),
				}},
				IntegrationRefs: []*governancev1.ReleaseIntegrationRef{{
					Domain:     "agent",
					Kind:       "acceptance",
					Ref:        "agent-acceptance-1",
					Status:     stringPtr("waiting_owner"),
					Summary:    stringPtr(longUnsafeSummary),
					Digest:     stringPtr("sha256:integration"),
					ObservedAt: stringPtr(now),
					Version:    stringPtr("7"),
				}},
				AgentContext: &governancev1.AgentContextRef{
					SessionRef:    stringPtr("session-1"),
					RunRef:        stringPtr("run-1"),
					StageRef:      stringPtr("stage-1"),
					AcceptanceRef: stringPtr("agent-acceptance-1"),
				},
				Version:    4,
				CreatedAt:  now,
				UpdatedAt:  now,
				ObservedAt: stringPtr(now),
			}},
			CompletedDecisions: []*governancev1.GovernanceDecisionSummary{{
				Kind:           governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_RISK_ASSESSMENT,
				Attention:      governancev1.GovernanceDecisionAttention_GOVERNANCE_DECISION_ATTENTION_COMPLETED,
				Id:             "risk-assessment-1",
				RiskClass:      governancev1.RiskClass_RISK_CLASS_R2,
				SafeSummary:    "Изменение требует владельческого gate",
				ProjectContext: &governancev1.ProjectContextRef{ProjectRef: stringPtr("project-1"), RepositoryRef: stringPtr("repo-1")},
				ProviderRefs:   []*governancev1.ProviderContextRef{{PullRequestRef: stringPtr("provider-pr-1"), ReviewSignalRef: stringPtr("review-signal-1")}},
				RuntimeRefs:    []*governancev1.RuntimeContextRef{{JobRef: stringPtr("runtime-job-1"), SummaryRef: stringPtr("runtime-summary-1")}},
				Version:        3,
				CreatedAt:      now,
				UpdatedAt:      now,
			}},
			EvidenceSummaries: []*governancev1.GovernanceEvidenceSummary{{
				SourceKind:  "agent.acceptance",
				SourceRef:   "agent-acceptance-1",
				Status:      stringPtr("waiting_owner"),
				Outcome:     stringPtr("needs_owner_decision"),
				SafeSummary: "Acceptance ожидает решения владельца",
				Digest:      stringPtr("sha256:evidence"),
				ObservedAt:  stringPtr(now),
				Version:     stringPtr("7"),
			}},
			Diagnostics: []string{"partial.provider_refs"},
		},
	}
}

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func agentRunStatusPtr(value agentsv1.AgentRunStatus) *agentsv1.AgentRunStatus {
	return &value
}
