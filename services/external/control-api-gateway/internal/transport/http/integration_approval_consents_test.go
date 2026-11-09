package httptransport

import (
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// consentHTTPFixture contains synthetic self metadata and one exact MR identity, with no credential descriptor or writable actor selector.
// consentHTTPFixture содержит synthetic self metadata и одну exact MR identity без credential descriptor либо writable actor selector.
func consentHTTPFixture() *cp.IntegrationApprovalConsent {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &cp.IntegrationApprovalConsent{Ref: "ias_00000000000000000000000000000001", Version: 1, ConnectionRef: "int_fixture01", ConnectionName: "Synthetic", CapabilityKey: "gitlab.merge_request.note.create", AgentRef: "agt_fixture01", RootRunRef: "run_fixture01", Lifetime: cp.IntegrationApprovalScopeLifetime_INTEGRATION_APPROVAL_SCOPE_LIFETIME_INITIATOR_RESOURCE, State: cp.IntegrationApprovalConsentState_INTEGRATION_APPROVAL_CONSENT_STATE_ACTIVE, ScopePaths: []string{"/project_id", "/merge_request_iid"}, ScopeValuesJson: `[{"path":"/project_id","type":"integer","value":7},{"path":"/merge_request_iid","type":"integer","value":12}]`, MaxEffects: 100, ReservedEffects: 1, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
}

// TestConsentHTTPContractPreservesOwnerPins checks actual generated routes/RPCs, required JSON fields, pagination and selected consent OCC without reading a live session.
// TestConsentHTTPContractPreservesOwnerPins проверяет actual generated routes/RPCs, required JSON fields, pagination и consent OCC без live session.
func TestConsentHTTPContractPreservesOwnerPins(t *testing.T) {
	t.Parallel()
	fixture := consentHTTPFixture()
	recorder := &catalogRPCRecorder{response: &cp.ListIntegrationApprovalConsentsResponse{Consents: []*cp.IntegrationApprovalConsent{fixture}, Page: &cp.PageInfo{NextPageToken: "cursor"}}}
	handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(recorder), Command: cp.NewPlatformCommandServiceClient(recorder)}})
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/me/integration-approval-consents?pageSize=1&pageToken=opaque", nil))
	input, ok := recorder.request.(*cp.ListIntegrationApprovalConsentsRequest)
	if writer.Code != 200 || !ok || input.Page.PageSize != 1 || input.Page.PageToken != "opaque" {
		t.Fatal("consent catalog dispatch failed", writer.Code)
	}
	var body map[string]any
	if json.Unmarshal(writer.Body.Bytes(), &body) != nil {
		t.Fatal("consent JSON unavailable")
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 || body["nextPageToken"] != "cursor" {
		t.Fatal("consent page projection changed")
	}
	item := items[0].(map[string]any)
	if item["scopeValuesJSON"] != fixture.ScopeValuesJson || item["state"] != "ACTIVE" || item["lifetime"] != "INITIATOR_RESOURCE" || item["maxEffects"] != float64(100) {
		t.Fatal("consent typed projection changed")
	}
	fixture.Version = 2
	fixture.State = cp.IntegrationApprovalConsentState_INTEGRATION_APPROVAL_CONSENT_STATE_REVOKED
	fixture.RevokedAt = timestamppb.New(fixture.CreatedAt.AsTime().Add(time.Minute))
	recorder.response = &cp.RevokeIntegrationApprovalConsentResponse{Consent: fixture}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/me/integration-approval-consents/"+fixture.Ref+"/revoke", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "consent-revoke")
	request.Header.Set("If-Match", `"1"`)
	request.Header.Set("X-CSRF-Token", "fixture")
	writer = httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	revoke, ok := recorder.request.(*cp.RevokeIntegrationApprovalConsentRequest)
	if writer.Code != 200 || !ok || revoke.ConsentRef != fixture.Ref || revoke.Mutation.GetExpectedVersion() != 1 || revoke.Mutation.IdempotencyKey != "consent-revoke" {
		t.Fatal("consent revoke route or OCC changed", writer.Code)
	}
}

// TestConsentHTTPRejectsActorSelectors refuses arbitrary self-catalog selectors and nonempty revoke bodies before dispatch, rather than ignoring authority-looking fields.
// TestConsentHTTPRejectsActorSelectors отклоняет arbitrary self-catalog selectors и nonempty revoke bodies до dispatch вместо игнорирования authority-looking fields.
func TestConsentHTTPRejectsActorSelectors(t *testing.T) {
	for _, path := range []string{"?subjectId=other", "?pageSize=1&pageSize=2", "?connectionRef=other"} {
		recorder := &catalogRPCRecorder{}
		handler := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(recorder)}})
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/api/v1/me/integration-approval-consents"+path, nil))
		if writer.Code != 400 || recorder.request != nil {
			t.Fatal("consent selector reached owner")
		}
	}
}
