package provider

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	responseapi "github.com/openai/openai-go/v3/responses"
)

func TestBuildPromptKeepsBoundedTypedInput(t *testing.T) {
	t.Parallel()

	prompt, err := buildPrompt(Request{
		Task:            "Подготовь ответ клиенту",
		InputJSON:       json.RawMessage(`{"ticket":"SUP-42"}`),
		AllowDelegation: true,
		DelegationTargets: []DelegationTarget{{
			Ref: "agt_support", Name: "Специалист поддержки",
		}},
	})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	for _, expected := range []string{"Подготовь ответ клиенту", `"ticket":"SUP-42"`, "agt_support"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("buildPrompt() does not contain %q", expected)
		}
	}
}

func TestBuildPromptRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := buildPrompt(Request{Task: "task", InputJSON: json.RawMessage(`{"broken"`)})
	var safe *SafeError
	if !errors.As(err, &safe) || safe.Code != "RUNTIME_INPUT_TOO_LARGE" {
		t.Fatalf("buildPrompt() error = %v", err)
	}
}

func TestDecodeToolCallRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := decodeToolCall(responseapi.ResponseFunctionToolCall{
		Name: "delegate_agent", CallID: "call_1",
		Arguments: `{"target_agent_ref":"agt_support","task":"reply","authority":"owner"}`,
	})
	var safe *SafeError
	if !errors.As(err, &safe) || safe.Code != "PROVIDER_TOOL_INVALID" {
		t.Fatalf("decodeToolCall() error = %v", err)
	}
}

func TestReadCredentialRejectsBroadPermissions(t *testing.T) {
	t.Parallel()

	fileName := filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(fileName, []byte("test-key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredential(fileName); err == nil {
		t.Fatal("readCredential() accepted a group/world-readable file")
	}
}
