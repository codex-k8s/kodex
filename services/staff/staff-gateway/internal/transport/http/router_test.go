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

func TestRouterMapsDownstreamUnavailable(t *testing.T) {
	client := &fakeInteractionHubClient{getErr: status.Error(codes.Unavailable, "unavailable")}
	router := newTestRouter(t, client)
	req := authenticatedRequest(http.MethodGet, "/v1/owner-inbox/items/"+uuid.NewString()+"?scope_type=project&scope_ref=project-1", "")
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
	}, client, nil)
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
	listRequest    *interactionsv1.ListOwnerInboxItemsRequest
	getRequest     *interactionsv1.GetOwnerInboxItemRequest
	recordRequest  *interactionsv1.RecordInteractionResponseRequest
	listResponse   *interactionsv1.ListOwnerInboxItemsResponse
	getResponse    *interactionsv1.OwnerInboxItemResponse
	recordResponse *interactionsv1.InteractionResponseResponse
	listErr        error
	getErr         error
	recordErr      error
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

func stringPtr(value string) *string {
	return &value
}
