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
	interactionsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/interactions/v1"
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
	}, client, client, nil)
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
	listRequest           *interactionsv1.ListOwnerInboxItemsRequest
	getRequest            *interactionsv1.GetOwnerInboxItemRequest
	recordRequest         *interactionsv1.RecordInteractionResponseRequest
	runtimeStatusRequest  *agentsv1.GetAgentRunRuntimeStatusRequest
	listResponse          *interactionsv1.ListOwnerInboxItemsResponse
	getResponse           *interactionsv1.OwnerInboxItemResponse
	recordResponse        *interactionsv1.InteractionResponseResponse
	runtimeStatusResponse *agentsv1.AgentRunRuntimeStatusResponse
	listErr               error
	getErr                error
	recordErr             error
	runtimeStatusErr      error
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

func (f *fakeInteractionHubClient) GetAgentRunRuntimeStatus(_ context.Context, request *agentsv1.GetAgentRunRuntimeStatusRequest) (*agentsv1.AgentRunRuntimeStatusResponse, error) {
	f.runtimeStatusRequest = request
	return f.runtimeStatusResponse, f.runtimeStatusErr
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

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
