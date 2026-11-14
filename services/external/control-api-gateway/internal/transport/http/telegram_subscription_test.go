package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// telegramHTTPFixture supplies only safe subscription metadata with explicit false and zero fields.
// telegramHTTPFixture предоставляет лишь безопасные metadata подписки с явными false и zero полями.
func telegramHTTPFixture() *cp.TelegramSubscription {
	return &cp.TelegramSubscription{Ref: "tgs_fixture01", ProjectRef: "prj_fixture01", ConnectionRef: "int_fixture01", Version: 2,
		Enabled: true, State: "ACTIVE", EventTypes: []string{"FINISHED", "FAILED", "HUMAN_GATE"}, ScopeDigest: strings.Repeat("a", 64),
		BotId: 12345678, ChatDisplay: "…1234", TopicDisplay: "NONE", ExpiresAt: timestamppb.New(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)),
		MaxEffects: 100, UsedEffects: 0, UpdatedAt: timestamppb.New(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)), CanManage: true}
}

// telegramHTTPHandler binds actual generated routes to generated owner clients; the recorder replaces transport only, not DTO mapping.
// telegramHTTPHandler связывает реальные generated routes с generated owner clients; recorder заменяет лишь transport, не DTO mapping.
func telegramHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(recorder), Query: cp.NewPlatformQueryServiceClient(recorder)}})
}

// TestTelegramConfigureHTTPRejectsAmbiguousAndAuthorityInput proves invalid bodies never reach owner RPC, including missing booleans, duplicate keys and private destination selectors.
// TestTelegramConfigureHTTPRejectsAmbiguousAndAuthorityInput доказывает отказ неверным телам до owner RPC, включая отсутствующий boolean, duplicate keys и private destination selectors.
func TestTelegramConfigureHTTPRejectsAmbiguousAndAuthorityInput(t *testing.T) {
	t.Parallel()
	const valid = `{"enabled":true,"connectionRef":"int_fixture01","eventTypes":["FINISHED"],"durationSeconds":86400,"maxEffects":100}`
	bodies := []string{`{}`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":false,"connectionRef":"int_fixture01"}`,
		`{"enabled":false,"enabled":true}`, `{"Enabled":false}`, `{"enabled":false,"actorRef":"usr_fixture01"}`,
		strings.Replace(valid, `"FINISHED"`, `"FINISHED","FINISHED"`, 1), strings.Replace(valid, `86400`, `604801`, 1),
		strings.Replace(valid, `"maxEffects":100`, `"maxEffects":0`, 1), strings.Replace(valid, `"enabled":true`, `"enabled":true,"botId":12`, 1),
		valid + `{}`, strings.Repeat(" ", 4097)}
	for _, body := range bodies {
		recorder := &catalogRPCRecorder{}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/prj_fixture01/telegram-subscription", strings.NewReader(body))
		request.Header.Set("Idempotency-Key", "telegram-http-fixture")
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		writer := httptest.NewRecorder()
		telegramHTTPHandler(recorder).ServeHTTP(writer, request)
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatalf("invalid subscription proposal reached RPC: status=%d", writer.Code)
		}
	}
}

// TestTelegramConfigureHTTPPinsOwnerCommandAndPublicReceipt checks first-create optional OCC, exact update OCC and complete no-store projections without private owner fields.
// TestTelegramConfigureHTTPPinsOwnerCommandAndPublicReceipt проверяет optional OCC первого create, exact OCC update и полную no-store projection без private owner fields.
func TestTelegramConfigureHTTPPinsOwnerCommandAndPublicReceipt(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"", `"1"`} {
		recorder := &catalogRPCRecorder{response: &cp.ConfigureTelegramSubscriptionResponse{Subscription: telegramHTTPFixture()}}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/projects/prj_fixture01/telegram-subscription", strings.NewReader(`{"enabled":true,"connectionRef":"int_fixture01","eventTypes":["FINISHED","FAILED","HUMAN_GATE"],"durationSeconds":86400,"maxEffects":100}`))
		request.Header.Set("Idempotency-Key", "telegram-http-fixture")
		request.Header.Set("X-CSRF-Token", "synthetic-csrf")
		if version != "" {
			request.Header.Set("If-Match", version)
		}
		writer := httptest.NewRecorder()
		telegramHTTPHandler(recorder).ServeHTTP(writer, request)
		input, ok := recorder.request.(*cp.ConfigureTelegramSubscriptionRequest)
		if writer.Code != http.StatusOK || !ok || recorder.method != cp.PlatformCommandService_ConfigureTelegramSubscription_FullMethodName ||
			input.ProjectRef != "prj_fixture01" || input.ConnectionRef != "int_fixture01" || input.GetMutation().GetIdempotencyKey() != "telegram-http-fixture" ||
			input.DurationSeconds != 86400 || input.MaxEffects != 100 || version != "" && input.GetMutation().GetExpectedVersion() != 1 || version == "" && input.GetMutation().GetExpectedVersion() != 0 {
			t.Fatalf("subscription owner mapping failed: status=%d", writer.Code)
		}
		var body map[string]any
		if json.Unmarshal(writer.Body.Bytes(), &body) != nil || body["usedEffects"] != float64(0) || body["canManage"] != true || body["projectRef"] != "prj_fixture01" ||
			writer.Header().Get("Cache-Control") != "no-store" || writer.Header().Get("ETag") != `"2"` {
			t.Fatal("subscription public receipt lost required metadata")
		}
		for _, field := range []string{"credentialRevision", "snapshot", "grantId", "botToken", "chatId", "approvedBy"} {
			if strings.Contains(writer.Body.String(), field) {
				t.Fatal("subscription exposed private owner metadata")
			}
		}
	}
}

// TestTelegramHTTPRejectsPartialForeignAndUnboundedReadback verifies absence remains explicit and malformed current authority projections cannot become successful reads.
// TestTelegramHTTPRejectsPartialForeignAndUnboundedReadback проверяет явное отсутствие и отказ неверным current authority projections вместо успешного чтения.
func TestTelegramHTTPRejectsPartialForeignAndUnboundedReadback(t *testing.T) {
	t.Parallel()
	const path = "/api/v1/projects/prj_fixture01/telegram-subscription"
	for _, mutate := range []func(*cp.TelegramSubscription){
		func(value *cp.TelegramSubscription) { value.ProjectRef = "prj_foreign01" },
		func(value *cp.TelegramSubscription) { value.ScopeDigest = "" },
		func(value *cp.TelegramSubscription) { value.CanManage = false },
		func(value *cp.TelegramSubscription) { value.State = "UNKNOWN" },
		func(value *cp.TelegramSubscription) { value.ChatDisplay = "-10012345678" },
	} {
		value := telegramHTTPFixture()
		mutate(value)
		recorder := &catalogRPCRecorder{response: &cp.GetTelegramSubscriptionResponse{Subscription: value, CanManage: true}}
		writer := httptest.NewRecorder()
		telegramHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
		if writer.Code != http.StatusBadGateway {
			t.Fatalf("malformed subscription accepted: %d", writer.Code)
		}
	}
	recorder := &catalogRPCRecorder{response: &cp.GetTelegramSubscriptionResponse{CanManage: false}}
	writer := httptest.NewRecorder()
	telegramHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
	if writer.Code != http.StatusOK || !strings.Contains(writer.Body.String(), `"canManage":false`) || strings.Contains(writer.Body.String(), `subscription`) {
		t.Fatal("unconfigured state was fabricated or lost")
	}
	for _, suffix := range []string{"?subjectRef=usr_foreign01", "?projectRef=prj_foreign01"} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		telegramHTTPHandler(recorder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path+suffix, nil))
		if writer.Code != http.StatusBadRequest || recorder.request != nil {
			t.Fatal("authority selector reached owner RPC")
		}
	}
}
