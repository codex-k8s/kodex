package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeJSONWithEndpointSpecificLimit checks that the generic body budget rejects an oversized payload while a larger endpoint budget accepts its exact content.
// TestDecodeJSONWithEndpointSpecificLimit проверяет, что общий лимит тела отклоняет слишком большой ввод, а увеличенный лимит отдельного endpoint принимает его точное содержимое.
func TestDecodeJSONWithEndpointSpecificLimit(t *testing.T) {
	t.Parallel()

	// body models content whose size crosses the generic JSON budget but fits the assistant-specific budget.
	// body описывает содержимое, которое превышает общий лимит JSON, но укладывается в отдельный лимит помощника.
	type body struct {
		Content string `json:"content"`
	}
	payload := `{"content":"` + strings.Repeat("a", maximumJSONBody) + `"}`

	genericResponse := httptest.NewRecorder()
	genericRequest := httptest.NewRequest("PUT", "/generic", strings.NewReader(payload))
	if _, ok := decodeJSON[body](genericResponse, genericRequest); ok {
		t.Fatal("generic JSON decoder accepted an oversized request")
	}

	assistantResponse := httptest.NewRecorder()
	assistantRequest := httptest.NewRequest("PUT", "/assistant-plan", strings.NewReader(payload))
	decoded, ok := decodeJSONWithLimit[body](assistantResponse, assistantRequest, maximumAssistantPlanDraftJSONBody)
	if !ok {
		t.Fatalf("assistant plan decoder rejected the endpoint-specific payload: status=%d body=%s", assistantResponse.Code, assistantResponse.Body.String())
	}
	if decoded.Content != strings.Repeat("a", maximumJSONBody) {
		t.Fatal("assistant plan decoder changed the request payload")
	}
}

// TestDecodeMutationBodyPreservesBodyAndHeaderBoundary checks strict JSON parsing before mutation syntax and preserves the idempotency/version pins without invoking an owner RPC.
// TestDecodeMutationBodyPreservesBodyAndHeaderBoundary проверяет строгий разбор JSON перед синтаксисом изменения и сохраняет ключ идемпотентности и версию без вызова RPC владельца.
func TestDecodeMutationBodyPreservesBodyAndHeaderBoundary(t *testing.T) {
	t.Parallel()
	// body keeps the accepted field narrow so unknown JSON keys must fail before mutation parsing.
	// body оставляет одно допустимое поле, чтобы неизвестные ключи JSON отклонялись до разбора контекста изменения.
	type body struct {
		Name string `json:"name"`
	}
	for _, test := range []struct {
		name, input, etag string
		valid             bool
		version           int64
	}{
		{"create", `{"name":"Ada"}`, "", true, 0},
		{"versioned", `{"name":"Ada"}`, `"4"`, true, 4},
		{"malformed-body", `{`, `"4"`, false, 0},
		{"unknown-field", `{"name":"Ada","private":true}`, `"4"`, false, 0},
		{"trailing-body", `{"name":"Ada"}{}`, `"4"`, false, 0},
		{"malformed-version", `{"name":"Ada"}`, `"invalid"`, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/roles", strings.NewReader(test.input))
			decoded, mutation, ok := decodeMutationBody[body](writer, request, "key-fixture01", test.etag)
			if ok != test.valid {
				t.Fatalf("unexpected request preparation result: valid=%t status=%d", ok, writer.Code)
			}
			if !ok {
				if mutation != nil || writer.Code != http.StatusBadRequest {
					t.Fatal("invalid request reached mutation/RPC boundary")
				}
				return
			}
			if decoded.Name != "Ada" || mutation.GetIdempotencyKey() != "key-fixture01" || writer.Body.Len() != 0 {
				t.Fatal("accepted request changed its body or idempotency key")
			}
			if test.version == 0 && mutation.ExpectedVersion != nil || test.version != 0 && mutation.GetExpectedVersion() != test.version {
				t.Fatal("accepted request changed its version pin")
			}
		})
	}
}
