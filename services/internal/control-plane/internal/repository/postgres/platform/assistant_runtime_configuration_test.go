package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestAssistantReasoningOverlayPreservesSettings proves reasoning edits preserve native preferences and reject retired engine settings.
// TestAssistantReasoningOverlayPreservesSettings проверяет сохранение native preferences и отказ удалённым настройкам движка.
func TestAssistantReasoningOverlayPreservesSettings(t *testing.T) {
	before := `model_reasoning_effort = "high"
[context]
prune = false
[cache]
optimization = "NO_HINTS"
`
	content, err := assistantRuntimeOverlay(before, "medium")
	parsed, parseErr := runtimecontract.ParseConfigOverlay(content)
	if err != nil || parseErr != nil || parsed.ModelReasoningEffort != "medium" || parsed.Context == nil || parsed.Context.Prune == nil || *parsed.Context.Prune || parsed.Cache == nil || parsed.Cache.Optimization != "NO_HINTS" {
		t.Fatal("native preferences lost")
	}
	for _, raw := range []string{`unknown_setting = true`, `web_search = "live"`, `personality = "pragmatic"`, `[history]
persistence = "save-all"`} {
		if _, err := assistantRuntimeOverlay(raw, "low"); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("retired or unknown setting accepted")
		}
	}
}

// TestAssistantRuntimeConfigurationNoChangeExactPins checks exact unchanged
// settings/catalog/profile pins, rejects search-only or identity/version/weight changes, and
// proves ignored publication fields are normalized on copies. Ordinary failures never match the
// dedicated no-change marker, which remains Conflict-compatible.
// TestAssistantRuntimeConfigurationNoChangeExactPins проверяет точные неизменённые
// settings/catalog/profile pins, отклоняет search-only либо identity/version/weight changes и
// подтверждает нормализацию игнорируемых publication fields на копиях. Обычные ошибки не совпадают
// с отдельным no-change marker, сохраняющим совместимость с Conflict.
func TestAssistantRuntimeConfigurationNoChangeExactPins(t *testing.T) {
	persisted := []entity.ProviderAccountCandidate{{AccountRef: "pacc_synthetic", Weight: 1, CatalogRevision: "rev-1", CatalogDigest: "digest-1", ProviderDefinitionKey: "provider-synthetic"}}
	pin := entity.AssistantRuntimeProfilePin{Ref: "profile-synthetic", Version: 2, RuntimeRevision: "runtime-2"}
	before := map[string]any{"runtimeProfileRef": pin.Ref, "model": "model-synthetic", "reasoningEffort": "medium", "providerPolicyMode": "PRIMARY", "runtimeProfilePin": pin, "providerAccounts": minimalAssistantRuntimeAccounts(persisted)}
	after := cloneAssistantFields(before)
	after["providerCatalogPins"] = persisted
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

// TestAssistantRuntimeAccountsClosedFields checks a synthetic account list rejects extra
// credential fields and weights outside1..100 without querying account state.
// TestAssistantRuntimeAccountsClosedFields проверяет отклонение лишних credential fields и весов
// вне1..100 в синтетическом account list без запросов account state.
func TestAssistantRuntimeAccountsClosedFields(t *testing.T) {
	for _, accounts := range []any{[]any{map[string]any{"accountRef": "pacc_test_one", "weight": 1, "credentialRef": "forbidden"}}, []any{map[string]any{"accountRef": "pacc_test_one", "weight": 0}}, []any{map[string]any{"accountRef": "pacc_test_one", "weight": 101}}} {
		if _, err := assistantRuntimeAccounts(map[string]any{"providerAccounts": accounts}); err == nil {
			t.Fatalf("invalid account payload accepted: %+v", accounts)
		}
	}
}

// TestAssistantJSONEqualCanonicalPins checks typed and map pins compare identically while distinct
// int64 versions above the float64 exact-integer boundary remain unequal.
// TestAssistantJSONEqualCanonicalPins проверяет одинаковое сравнение typed и map pins при
// сохранении различия int64 versions выше границы точных целых float64.
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
