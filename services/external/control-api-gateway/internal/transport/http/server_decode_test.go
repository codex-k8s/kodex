package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONWithEndpointSpecificLimit(t *testing.T) {
	t.Parallel()

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
