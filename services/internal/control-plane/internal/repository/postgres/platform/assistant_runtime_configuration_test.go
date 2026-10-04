package platform

import (
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"testing"
)

func TestAssistantReasoningOverlayPreservesSettings(t *testing.T) {
	before := "model_reasoning_effort = \"high\"\npersonality = \"pragmatic\"\n[history]\npersistence = \"save-all\"\n"
	content, err := assistantReasoningOverlay(before, "medium")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := runtimecontract.ParseConfigOverlay(content)
	if err != nil || parsed.ModelReasoningEffort != "medium" || parsed.Personality != "pragmatic" || parsed.History.Persistence != "save-all" {
		t.Fatalf("unrelated settings lost: %+v %v", parsed, err)
	}
	if _, err := assistantReasoningOverlay("unknown_setting = true\n", "low"); err == nil {
		t.Fatal("unknown manual configuration silently discarded")
	}
}

func TestAssistantRuntimeAccountsClosedFields(t *testing.T) {
	for _, accounts := range []any{[]any{map[string]any{"accountRef": "pacc_test_one", "weight": 1, "credentialRef": "forbidden"}}, []any{map[string]any{"accountRef": "pacc_test_one", "weight": 0}}, []any{map[string]any{"accountRef": "pacc_test_one", "weight": 101}}} {
		if _, err := assistantRuntimeAccounts(map[string]any{"providerAccounts": accounts}); err == nil {
			t.Fatalf("invalid account payload accepted: %+v", accounts)
		}
	}
}

func TestAssistantJSONEqualCanonicalPins(t *testing.T) {
	typed := struct {
		Ref     string `json:"ref"`
		Version int64  `json:"version"`
	}{Ref: "profile_test", Version: 9007199254740993}
	if !assistantJSONEqual(typed, map[string]any{"version": int64(9007199254740993), "ref": "profile_test"}) {
		t.Fatal("equivalent typed/persisted object differs")
	}
	if assistantJSONEqual(typed, map[string]any{"version": int64(9007199254740992), "ref": "profile_test"}) {
		t.Fatal("revision equality rounded a high watermark")
	}
}
