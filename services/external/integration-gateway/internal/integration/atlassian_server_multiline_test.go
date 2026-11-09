package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAtlassianServerMultilineOutput exercises normal execution and receipt validation for
// provider text with CRLF; lone CR, NUL and oversized responses retain closed failures.
// TestAtlassianServerMultilineOutput проверяет штатное исполнение и receipt для provider text
// с CRLF; одиночный CR, NUL и слишком большой ответ сохраняют закрытый отказ.
func TestAtlassianServerMultilineOutput(t *testing.T) {
	for _, definition := range []string{jiraServerDefinition, confluenceServerDefinition} {
		for _, sample := range []struct {
			name, text, expected string
			invalid              bool
		}{
			{name: "lf", text: "First\nSecond\n", expected: "First\nSecond\n"},
			{name: "crlf", text: "First\r\nSecond\r\n", expected: "First\nSecond\n"},
			{name: "unicode-crlf", text: "Первая\r\nВторая", expected: "Первая\nВторая"},
			{name: "empty"},
			{name: "lone-cr", text: "First\rSecond", invalid: true},
			{name: "nul", text: "First\x00Second", invalid: true},
			{name: "oversized", text: strings.Repeat("a", maximumResponseBytes+1), invalid: true},
		} {
			t.Run(definition+"/"+sample.name, func(t *testing.T) {
				adapter := testAdapter(t)
				credential := testCredential(t, adapter, "synthetic-server-token")
				operation, field := "jira.issue.read", "description"
				if definition == confluenceServerDefinition {
					operation, field = "confluence.page.read", "body"
				}
				request := atlassianServerInvocation(t, adapter, definition, operation, atlassianServerInput(operation), credential)
				calls := 0
				adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					response := atlassianServerResponse(t, request, r)
					raw, err := io.ReadAll(response.Body)
					_ = response.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					var value map[string]any
					if json.Unmarshal(raw, &value) != nil {
						t.Fatal("invalid synthetic response")
					}
					if definition == jiraServerDefinition {
						value["fields"].(map[string]any)["description"] = sample.text
					} else {
						value["body"].(map[string]any)["storage"].(map[string]any)["value"] = sample.text
					}
					raw, _ = json.Marshal(value)
					response.Body = io.NopCloser(strings.NewReader(string(raw)))
					response.ContentLength = int64(len(raw))
					return response, nil
				})}
				result, err := adapter.Execute(t.Context(), request)
				wantCalls := 1
				if definition == confluenceServerDefinition {
					wantCalls = 2
				}
				if calls > wantCalls || calls < 1 {
					t.Fatalf("unexpected provider call count: %d", calls)
				}
				if sample.invalid {
					var safe *SafeError
					if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Summary != "" {
						t.Fatalf("unsafe output admitted: %v", err)
					}
					return
				}
				if err != nil || calls != wantCalls {
					t.Fatalf("normal text rejected: calls=%d error=%v", calls, err)
				}
				var output map[string]any
				if json.Unmarshal([]byte(result.Summary), &output) != nil {
					t.Fatal("invalid canonical output")
				}
				text, _ := output[field].(string)
				if text != sample.expected {
					t.Fatal("provider text changed beyond CRLF normalization")
				}
				responseDigest := sha256.Sum256([]byte(result.Summary))
				if result.Receipt.ResponseDigest != hex.EncodeToString(responseDigest[:]) || result.Receipt.InputDigest != request.InputDigest || result.Receipt.EffectKey != request.EffectKey {
					t.Fatal("canonical output receipt binding changed")
				}
			})
		}
	}
}

// TestAtlassianServerMultilineDoesNotRepairScope rejects foreign project/space metadata
// before text projection and before a second Confluence storage request.
// TestAtlassianServerMultilineDoesNotRepairScope отклоняет чужие project/space metadata
// до текстовой проекции и до второго запроса storage в Confluence.
func TestAtlassianServerMultilineDoesNotRepairScope(t *testing.T) {
	for _, definition := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(definition, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			operation := "jira.issue.read"
			if definition == confluenceServerDefinition {
				operation = "confluence.page.read"
			}
			request := atlassianServerInvocation(t, adapter, definition, operation, atlassianServerInput(operation), credential)
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				response := atlassianServerResponse(t, request, r)
				var value map[string]any
				if json.NewDecoder(response.Body).Decode(&value) != nil {
					t.Fatal("invalid synthetic response")
				}
				_ = response.Body.Close()
				if definition == jiraServerDefinition {
					fields := value["fields"].(map[string]any)
					fields["description"] = "First\r\nSecond"
					fields["project"] = map[string]string{"key": "OTHER"}
				} else {
					value["space"] = map[string]string{"key": "OTHER"}
					value["body"].(map[string]any)["storage"].(map[string]any)["value"] = "First\r\nSecond"
				}
				raw, _ := json.Marshal(value)
				response.Body = io.NopCloser(strings.NewReader(string(raw)))
				response.ContentLength = int64(len(raw))
				return response, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			var safe *SafeError
			if !errors.As(err, &safe) || result.Summary != "" || calls != 1 {
				t.Fatalf("foreign resource admitted: calls=%d error=%v", calls, err)
			}
		})
	}
}
