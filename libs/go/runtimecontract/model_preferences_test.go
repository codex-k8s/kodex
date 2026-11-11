package runtimecontract_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestRuntimePreferencesPreserveRouteQualification proves intent changes cannot mutate the shared descriptor or its exact route proof.
// TestRuntimePreferencesPreserveRouteQualification проверяет неизменность общего descriptor и qualification маршрута при изменении намерения.
func TestRuntimePreferencesPreserveRouteQualification(t *testing.T) {
	config := cacheProviderFixture()
	before, _ := json.Marshal(config)
	digest, _ := config.ModelCacheRouteDigest("tiny")
	result, err := config.ResolveRuntimeModelCache("tiny", "[cache]\noptimization = \"NO_HINTS\"\n")
	if err != nil || result.Mechanism != "NONE" || result.Requested.Optimization != "NO_HINTS" {
		t.Fatal("optional hints override failed")
	}
	result, err = config.ResolveRuntimeModelCache("tiny", "[cache]\nno_reuse = true\n")
	if err != nil || !result.NoReuse || !result.Requested.Required || result.Requested.Qualification.RouteSHA256 != digest {
		t.Fatal("qualified strict preference failed")
	}
	after, _ := json.Marshal(config)
	if string(before) != string(after) {
		t.Fatal("shared descriptor changed")
	}
	model := config.Models[0]
	contextPolicy, err := model.ResolveRuntimeContext("[context]\nauto_compaction = false\nprune = false\n")
	if err != nil || contextPolicy.AutoCompaction || contextPolicy.Prune || !model.EffectiveContextPolicy().AutoCompaction || model.ContextWindowTokens != 32768 {
		t.Fatal("native context override changed model limits or default")
	}
}

// TestRuntimePreferencesCannotWeakenMandatoryRoute exercises all downgrade channels and strict unsupported refusal before inference.
// TestRuntimePreferencesCannotWeakenMandatoryRoute проверяет все способы ослабления и отказ неподтверждённой строгой гарантии до inference.
func TestRuntimePreferencesCannotWeakenMandatoryRoute(t *testing.T) {
	config := cacheProviderFixture()
	config.Models[0].CachePolicy.Required = true
	config.Models[0].CachePolicy.NoReuse = true
	for _, optimization := range []string{"NO_HINTS", "PROVIDER_DEFAULT"} {
		candidate := cacheProviderFixture()
		if _, err := candidate.ResolveRuntimeModelCache("tiny", "[cache]\nrequired = true\noptimization = \""+optimization+"\"\n"); err == nil {
			t.Fatal("required policy without enforcement accepted")
		}
	}
	for _, raw := range []string{"required = false", "no_reuse = false", "reuse_scope = \"PROJECT\"", "reuse_scope = \"INITIATOR\"", "optimization = \"NO_HINTS\"", "optimization = \"PROVIDER_DEFAULT\""} {
		if result, err := config.ResolveRuntimeModelCache("tiny", "[cache]\n"+raw); err == nil || result.Reason != "MANDATORY_ROUTE_POLICY_CANNOT_WEAKEN" {
			t.Fatal("mandatory route constraint weakened")
		}
	}
	config = cacheProviderFixture()
	if _, err := config.ResolveRuntimeModelCache("tiny", "[cache]\nno_store = true\nrequired = false"); err == nil {
		t.Fatal("unsupported no-store accepted")
	}
	config.Models[0].CachePolicy = nil
	if result, err := config.ResolveRuntimeModelCache("tiny", "[cache]\noptimization = \"OPTIMIZE\""); err != nil || result.Mechanism != "NONE" || result.Status == "SUPPORTED" {
		t.Fatal("optional unqualified route did not degrade honestly")
	}
	if _, err := config.ResolveRuntimeModelCache("tiny", "[cache]\nno_reuse = true"); err == nil {
		t.Fatal("unknown strict guarantee accepted")
	}
	for _, raw := range []string{"[cache]\nqualification = \"SUPPORTED\"", "[context]\ncontext_window_tokens = 1000000", "[cache]\nnamespace = \"caller\"", "[cache]\nno_reuse = \"true\""} {
		if _, err := config.ResolveRuntimeModelCache("tiny", raw); err == nil {
			t.Fatal("untrusted policy field accepted")
		}
	}
}

// TestSafeEffectiveNativePolicyReadback distinguishes requested preferences from enforced results, including an unsupported strict request.
// TestSafeEffectiveNativePolicyReadback различает запрошенные настройки и действующий результат, включая неподдержанный строгий запрос.
func TestSafeEffectiveNativePolicyReadback(t *testing.T) {
	config := cacheProviderFixture()
	result, err := runtimecontract.RenderSafeEffectiveConfig(runtimecontract.SafeEffectiveConfigInput{Model: "tiny", Provider: config.DefinitionKey(), ModelProvider: &config, Overlay: "[context]\nprune = false\n[cache]\nno_store = true"})
	if err != nil || !strings.Contains(result, "UNSUPPORTED") || !strings.Contains(result, "NO_STORE_UNSUPPORTED") || !strings.Contains(result, "Prune = false") {
		t.Fatal("safe readback lost native effective status")
	}
}
