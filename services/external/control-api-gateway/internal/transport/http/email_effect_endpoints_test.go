package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	emailReceiptPath  = "/api/v1/integration-invocations/inv_fixture01/email-effect-receipt"
	emailDecisionPath = "/api/v1/email-effect-receipts/erc_fixture01/reconciliation"
)

type emailEffectRecorder struct {
	grpc.ClientConnInterface
	method       string
	request      proto.Message
	failure      error
	corrupt      func(proto.Message)
	withDecision bool
	readOutcome  controlplanev1.EmailEffectOutcome
	beforeInvoke func()
	calls        atomic.Int32
}

func emailReceiptFixture() *controlplanev1.EmailEffectReceipt {
	created := timestamppb.New(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	return &controlplanev1.EmailEffectReceipt{
		Ref: "erc_fixture01", Version: 3, InvocationRef: "inv_fixture01", ExternalReceiptRef: "private-external-receipt",
		ExternalReceiptDigest: strings.Repeat("a", 64), SemanticInputDigest: strings.Repeat("b", 64), EffectKey: "private-effect-key",
		Outcome: controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME, MailboxRef: "mailbox-sales", ConfigurationRevision: 7,
		ConnectionRef: "conn_fixture01", ProjectRef: "prj_fixture01", CreatedAt: created, UpdatedAt: created,
	}
}

func emailDecisionFixture() *controlplanev1.EmailReconciliationDecision {
	created := time.Date(2026, 9, 5, 0, 0, 10, 0, time.UTC)
	return &controlplanev1.EmailReconciliationDecision{
		Ref: "erd_fixture01", Version: 1, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64),
		InvocationRef: "inv_fixture01", Outcome: controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED,
		GrantRef: "private-worker-grant", ActorRef: "sub_fixture01", CreatedAt: timestamppb.New(created), ExpiresAt: timestamppb.New(created.Add(time.Minute)),
	}
}

func (client *emailEffectRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.calls.Add(1)
	if client.beforeInvoke != nil {
		client.beforeInvoke()
	}
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	var output proto.Message
	switch method {
	case controlplanev1.PlatformQueryService_GetEmailEffectReceipt_FullMethodName:
		result := &controlplanev1.GetEmailEffectReceiptResponse{Receipt: emailReceiptFixture()}
		if client.readOutcome != 0 {
			result.Receipt.Outcome = client.readOutcome
		}
		if client.withDecision {
			result.Decision = emailDecisionFixture()
		}
		output = result
	case controlplanev1.PlatformCommandService_ReconcileEmailEffect_FullMethodName:
		decision := emailDecisionFixture()
		decision.Outcome = request.(*controlplanev1.ReconcileEmailEffectRequest).Outcome
		output = &controlplanev1.ReconcileEmailEffectResponse{Decision: decision}
	default:
		return status.Error(codes.Unimplemented, "unexpected test method")
	}
	if client.corrupt != nil {
		client.corrupt(output)
	}
	proto.Merge(response.(proto.Message), output)
	return nil
}

func emailEffectHandler(client *emailEffectRecorder) http.Handler {
	fixture := newEmailHTTPFixture(client)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.authorizeRequest(r)
		fixture.handler.ServeHTTP(w, r)
	})
}

func emailDecisionBody(outcome string) string {
	return `{"expectedReceiptDigest":"` + strings.Repeat("a", 64) + `","outcome":"` + outcome + `","note":"Проверено владельцем"}`
}

func TestEmailEffectExactReadback(t *testing.T) {
	for _, outcome := range []controlplanev1.EmailEffectOutcome{
		controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME,
		controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED,
		controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED,
	} {
		for _, withDecision := range []bool{false, true} {
			client := &emailEffectRecorder{readOutcome: outcome, withDecision: withDecision}
			w := httptest.NewRecorder()
			emailEffectHandler(client).ServeHTTP(w, managedTestRequest("GET", emailReceiptPath, ""))
			var view generated.EmailEffectReceiptView
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || w.Header().Get("ETag") != `"3"` || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
			}
			input, ok := client.request.(*controlplanev1.GetEmailEffectReceiptRequest)
			if !ok || input.InvocationRef != "inv_fixture01" || client.method != controlplanev1.PlatformQueryService_GetEmailEffectReceipt_FullMethodName ||
				view.Receipt.Ref != "erc_fixture01" || view.Receipt.ConfigurationRevision != 7 || (view.Decision != nil) != withDecision ||
				string(view.Receipt.Outcome) != strings.TrimPrefix(outcome.String(), "EMAIL_EFFECT_OUTCOME_") {
				t.Fatalf("incorrect mapping: request=%v view=%+v", client.request, view)
			}
			if strings.Contains(w.Body.String(), "private-") {
				t.Fatal("worker-only fields escaped")
			}
		}
	}
}

func TestEmailEffectExactReconciliation(t *testing.T) {
	for _, outcome := range []string{"EFFECT_CONFIRMED", "NO_EFFECT_CONFIRMED"} {
		client := &emailEffectRecorder{}
		w := httptest.NewRecorder()
		emailEffectHandler(client).ServeHTTP(w, managedTestRequest("POST", emailDecisionPath, emailDecisionBody(outcome)))
		var view generated.EmailReconciliationDecision
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || w.Header().Get("ETag") != `"1"` || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		input, ok := client.request.(*controlplanev1.ReconcileEmailEffectRequest)
		if !ok || input.ReceiptRef != "erc_fixture01" || input.ExpectedReceiptDigest != strings.Repeat("a", 64) || input.Note != "Проверено владельцем" ||
			input.Mutation.GetExpectedVersion() != 3 || input.Mutation.IdempotencyKey != "managed-fixture-01" ||
			input.Outcome.String() != "EMAIL_EFFECT_OUTCOME_"+outcome || string(view.Outcome) != outcome || view.ReceiptVersion != 3 || view.Version != 1 {
			t.Fatalf("incorrect mapping: request=%v view=%+v", client.request, view)
		}
		if strings.Contains(w.Body.String(), "private-") {
			t.Fatal("worker-only fields escaped")
		}
	}
}

func TestEmailEffectInvalidInputDoesNotInvokeRPC(t *testing.T) {
	valid := emailDecisionBody("EFFECT_CONFIRMED")
	for _, tc := range []struct{ name, body, header, value string }{
		{"unknown-outcome", emailDecisionBody("UNKNOWN_OUTCOME"), "", ""},
		{"unspecified-outcome", emailDecisionBody("UNSPECIFIED"), "", ""},
		{"wrong-case", emailDecisionBody("effect_confirmed"), "", ""},
		{"missing-outcome", `{"expectedReceiptDigest":"` + strings.Repeat("a", 64) + `"}`, "", ""},
		{"missing-digest", `{"outcome":"EFFECT_CONFIRMED"}`, "", ""},
		{"wrong-digest", strings.Replace(valid, strings.Repeat("a", 64), "sha256:"+strings.Repeat("a", 64), 1), "", ""},
		{"uppercase-digest", strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), "", ""},
		{"forged-actor", strings.TrimSuffix(valid, "}") + `,"actorRef":"sub_forged01"}`, "", ""},
		{"forged-grant", strings.TrimSuffix(valid, "}") + `,"grantRef":"grt_forged01"}`, "", ""},
		{"forged-project", strings.TrimSuffix(valid, "}") + `,"projectRef":"prj_forged01"}`, "", ""},
		{"oversize-note", strings.Replace(valid, "Проверено владельцем", strings.Repeat("я", 2001), 1), "", ""},
		{"nul-note", strings.Replace(valid, "Проверено владельцем", `note\u0000`, 1), "", ""},
		{"null-body", "null", "", ""},
		{"trailing-json", valid + ` {}`, "", ""},
		{"missing-occ", valid, "If-Match", ""},
		{"weak-occ", valid, "If-Match", `W/"3"`},
		{"unsafe-occ", valid, "If-Match", `"9007199254740992"`},
		{"missing-idempotency", valid, "Idempotency-Key", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &emailEffectRecorder{}
			r := managedTestRequest("POST", emailDecisionPath, tc.body)
			if tc.header != "" {
				r.Header.Del(tc.header)
				if tc.value != "" {
					r.Header.Set(tc.header, tc.value)
				}
			}
			w := httptest.NewRecorder()
			emailEffectHandler(client).ServeHTTP(w, r)
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d called=%t body=%s", w.Code, client.request != nil, w.Body.String())
			}
		})
	}
	for _, method := range []string{"GET", "POST"} {
		path := strings.Replace(emailReceiptPath, "inv_fixture01", "short", 1)
		if method == "POST" {
			path = strings.Replace(emailDecisionPath, "erc_fixture01", "short", 1)
		}
		client := &emailEffectRecorder{}
		w := httptest.NewRecorder()
		emailEffectHandler(client).ServeHTTP(w, managedTestRequest(method, path, valid))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid path accepted: %s %d", path, w.Code)
		}
	}
}

func TestEmailEffectUnicodeNoteBoundary(t *testing.T) {
	for _, note := range []string{"", strings.Repeat("я", 2000), "Строка 1\nСтрока 2"} {
		body, _ := json.Marshal(map[string]string{"expectedReceiptDigest": strings.Repeat("a", 64), "outcome": "EFFECT_CONFIRMED", "note": note})
		client := &emailEffectRecorder{}
		w := httptest.NewRecorder()
		emailEffectHandler(client).ServeHTTP(w, managedTestRequest("POST", emailDecisionPath, string(body)))
		if w.Code != 200 || client.request.(*controlplanev1.ReconcileEmailEffectRequest).Note != note {
			t.Fatalf("note rejected: status=%d", w.Code)
		}
	}
}

func TestEmailEffectAuthorityFailuresStayClosed(t *testing.T) {
	for _, method := range []string{"GET", "POST"} {
		for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.Unauthenticated, codes.Aborted, codes.FailedPrecondition, codes.Unavailable} {
			client := &emailEffectRecorder{failure: status.Error(code, "private owner details")}
			path := emailReceiptPath
			if method == "POST" {
				path = emailDecisionPath
			}
			w := httptest.NewRecorder()
			emailEffectHandler(client).ServeHTTP(w, managedTestRequest(method, path, emailDecisionBody("EFFECT_CONFIRMED")))
			want := map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unauthenticated: 401, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503}[code]
			if w.Code != want || strings.Contains(w.Body.String(), "private owner details") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		}
	}
	client := &emailEffectRecorder{failure: rpcStatusWithErrorInfo(t, codes.PermissionDenied, controlPlaneErrorDomain, freshAuthenticationRequiredReason)}
	w := httptest.NewRecorder()
	emailEffectHandler(client).ServeHTTP(w, managedTestRequest("POST", emailDecisionPath, emailDecisionBody("EFFECT_CONFIRMED")))
	if w.Code != 403 || !strings.Contains(w.Body.String(), freshAuthenticationRequiredReason) {
		t.Fatalf("fresh authentication reason lost: %d %s", w.Code, w.Body.String())
	}
}

func TestEmailEffectRejectsCorruptOwnerReceipt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*controlplanev1.GetEmailEffectReceiptResponse)
	}{
		{"missing-receipt", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt = nil }},
		{"wrong-invocation", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.InvocationRef = "inv_other01" }},
		{"invalid-version", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.Version = maximumSafeJSONInteger + 1 }},
		{"invalid-configuration", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.ConfigurationRevision = 0 }},
		{"missing-project", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.ProjectRef = "" }},
		{"invalid-outcome", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.Outcome = 99 }},
		{"invalid-digest", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.ExternalReceiptDigest = "bad" }},
		{"missing-created", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Receipt.CreatedAt = nil }},
		{"invalid-time", func(r *controlplanev1.GetEmailEffectReceiptResponse) {
			r.Receipt.UpdatedAt = &timestamppb.Timestamp{Seconds: 253402300800}
		}},
		{"time-order", func(r *controlplanev1.GetEmailEffectReceiptResponse) {
			r.Receipt.UpdatedAt = timestamppb.New(r.Receipt.CreatedAt.AsTime().Add(-time.Second))
		}},
		{"wrong-decision-receipt", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Decision.ReceiptRef = "erc_other01" }},
		{"wrong-decision-version", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Decision.ReceiptVersion = 2 }},
		{"wrong-decision-digest", func(r *controlplanev1.GetEmailEffectReceiptResponse) {
			r.Decision.ReceiptDigest = strings.Repeat("c", 64)
		}},
		{"wrong-decision-invocation", func(r *controlplanev1.GetEmailEffectReceiptResponse) { r.Decision.InvocationRef = "inv_other01" }},
		{"unknown-decision", func(r *controlplanev1.GetEmailEffectReceiptResponse) {
			r.Decision.Outcome = controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME
		}},
		{"decision-precedes-receipt", func(r *controlplanev1.GetEmailEffectReceiptResponse) {
			r.Decision.CreatedAt = timestamppb.New(r.Receipt.CreatedAt.AsTime().Add(-time.Second))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &emailEffectRecorder{withDecision: true, corrupt: func(m proto.Message) { tc.corrupt(m.(*controlplanev1.GetEmailEffectReceiptResponse)) }}
			w := httptest.NewRecorder()
			emailEffectHandler(client).ServeHTTP(w, managedTestRequest("GET", emailReceiptPath, ""))
			if w.Code != 502 || w.Header().Get("ETag") != "" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestEmailEffectRejectsCorruptOwnerDecision(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(*controlplanev1.ReconcileEmailEffectResponse)
	}{
		{"missing-decision", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision = nil }},
		{"wrong-receipt", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.ReceiptRef = "erc_other01" }},
		{"wrong-version", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.ReceiptVersion = 2 }},
		{"wrong-digest", func(r *controlplanev1.ReconcileEmailEffectResponse) {
			r.Decision.ReceiptDigest = strings.Repeat("c", 64)
		}},
		{"wrong-outcome", func(r *controlplanev1.ReconcileEmailEffectResponse) {
			r.Decision.Outcome = controlplanev1.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED
		}},
		{"unsafe-version", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.Version = maximumSafeJSONInteger + 1 }},
		{"missing-actor", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.ActorRef = "" }},
		{"missing-expiry", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.ExpiresAt = nil }},
		{"invalid-expiry", func(r *controlplanev1.ReconcileEmailEffectResponse) { r.Decision.ExpiresAt = r.Decision.CreatedAt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &emailEffectRecorder{corrupt: func(m proto.Message) { tc.corrupt(m.(*controlplanev1.ReconcileEmailEffectResponse)) }}
			w := httptest.NewRecorder()
			emailEffectHandler(client).ServeHTTP(w, managedTestRequest("POST", emailDecisionPath, emailDecisionBody("EFFECT_CONFIRMED")))
			if w.Code != 502 || w.Header().Get("ETag") != "" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
