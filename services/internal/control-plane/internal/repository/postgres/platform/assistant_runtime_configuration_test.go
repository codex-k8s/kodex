package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestAssistantReasoningOverlayPreservesSettings(t *testing.T) {
	before := "model_reasoning_effort = \"high\"\npersonality = \"pragmatic\"\n[history]\npersistence = \"save-all\"\n"
	content, err := assistantRuntimeOverlay(before, "medium", "")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := runtimecontract.ParseConfigOverlay(content)
	if err != nil || parsed.ModelReasoningEffort != "medium" || parsed.Personality != "pragmatic" || parsed.History.Persistence != "save-all" {
		t.Fatalf("unrelated settings lost: %+v %v", parsed, err)
	}
	if _, err := assistantRuntimeOverlay("unknown_setting = true\n", "low", ""); err == nil {
		t.Fatal("unknown manual configuration silently discarded")
	}
}

func TestAssistantRuntimeOverlayClosedSearchModes(t *testing.T) {
	before := "web_search = \"indexed\"\npersonality = \"pragmatic\"\n"
	for _, mode := range []string{"", "disabled", "cached", "indexed", "live", "future", "LIVE"} {
		content, err := assistantRuntimeOverlay(before, "medium", mode)
		if mode == "future" || mode == "LIVE" {
			if !errors.Is(err, errs.ErrInvalid) {
				t.Fatal("unknown search mode was accepted")
			}
			continue
		}
		parsed, parseErr := runtimecontract.ParseConfigOverlay(content)
		want := mode
		if want == "" {
			want = "indexed"
		}
		if err != nil || parseErr != nil || parsed.WebSearchMode != want || parsed.Personality != "pragmatic" || parsed.ModelReasoningEffort != "medium" {
			t.Fatal("search update changed unrelated fields or omitted mode was reset")
		}
	}
}

func TestAssistantRuntimeConfigurationNoChangeExactPins(t *testing.T) {
	persisted := []entity.ProviderAccountCandidate{{AccountRef: "pacc_synthetic", Weight: 1, CatalogRevision: "rev-1", CatalogDigest: "digest-1", ProviderDefinitionKey: "provider-synthetic"}}
	pin := entity.AssistantRuntimeProfilePin{Ref: "profile-synthetic", Version: 2, RuntimeRevision: "runtime-2"}
	before := map[string]any{"runtimeProfileRef": pin.Ref, "model": "model-synthetic", "reasoningEffort": "medium", "providerPolicyMode": "PRIMARY", "runtimeProfilePin": pin, "providerAccounts": minimalAssistantRuntimeAccounts(persisted)}
	after := cloneAssistantFields(before)
	after["providerCatalogPins"] = persisted
	searchChange := cloneAssistantFields(after)
	searchChange["webSearchMode"] = "live"
	if assistantRuntimeConfigurationUnchanged(before, searchChange, persisted) {
		t.Fatal("search-only owner configuration was discarded as unchanged")
	}
	if !assistantRuntimeConfigurationUnchanged(before, after, persisted) {
		t.Fatal("same authoritative settings and persisted catalog pins were not recognized")
	}
	for _, field := range []string{"runtimeProfileRef", "model", "reasoningEffort", "providerPolicyMode", "runtimeProfilePin", "providerAccounts"} {
		t.Run(field, func(t *testing.T) {
			changed := cloneAssistantFields(after)
			changed[field] = "different"
			if assistantRuntimeConfigurationUnchanged(before, changed, persisted) {
				t.Fatal("changed settings or profile pin were discarded")
			}
		})
	}
	for _, field := range []string{"account", "weight", "catalog-revision", "catalog-digest", "provider", "profile-version", "profile-revision"} {
		t.Run(field, func(t *testing.T) {
			changed := cloneAssistantFields(after)
			accounts := append([]entity.ProviderAccountCandidate(nil), persisted...)
			profile := pin
			switch field {
			case "account":
				accounts[0].AccountRef += "-changed"
			case "weight":
				accounts[0].Weight++
			case "catalog-revision":
				accounts[0].CatalogRevision += "-changed"
			case "catalog-digest":
				accounts[0].CatalogDigest += "-changed"
			case "provider":
				accounts[0].ProviderDefinitionKey += "-changed"
			case "profile-version":
				profile.Version++
			case "profile-revision":
				profile.RuntimeRevision += "-changed"
			}
			changed["providerCatalogPins"], changed["runtimeProfilePin"] = accounts, profile
			if assistantRuntimeConfigurationUnchanged(before, changed, persisted) {
				t.Fatal("changed authoritative pin was discarded")
			}
		})
	}
	prepared := append([]entity.ProviderAccountCandidate(nil), persisted...)
	prepared[0].DefaultReasoningEffort, prepared[0].ModelCapabilityDigest = "high", "capability-current"
	after["providerCatalogPins"] = prepared
	if !assistantRuntimeConfigurationUnchanged(before, after, persisted) || prepared[0].DefaultReasoningEffort == "" || prepared[0].ModelCapabilityDigest == "" {
		t.Fatal("publication-only normalization failed or changed producer input")
	}
	for _, failure := range []error{errs.ErrConflict, errs.ErrVersionMismatch, errs.ErrForbidden, errs.ErrInvalid, errs.ErrUnavailable} {
		if errors.Is(failure, errAssistantRuntimeConfigurationNoChange) {
			t.Fatal("ordinary failure was treated as unchanged")
		}
	}
	if !errors.Is(errAssistantRuntimeConfigurationNoChange, errs.ErrConflict) {
		t.Fatal("existing non-proposal callers lost their conflict boundary")
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
