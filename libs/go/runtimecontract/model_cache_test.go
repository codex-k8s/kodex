package runtimecontract_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// cacheProviderFixture creates a synthetic operator-qualified text route; the fixture is never evidence of a real inference deployment.
// cacheProviderFixture создаёт synthetic operator-qualified text route; fixture не доказывает настоящий inference deployment.
func cacheProviderFixture() runtimecontract.ModelProviderConfiguration {
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", API: "CHAT_COMPLETIONS", RouteKind: "VLLM", RouteVersion: "0.23.0", AddressCIDRs: []string{"192.0.2.2/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "tiny", ContextWindowTokens: 32768, MaximumOutputTokens: 1024, CachePolicy: &runtimecontract.ModelCachePolicy{Revision: 1, Optimization: "OPTIMIZE", ReuseScope: "SESSION", Qualification: runtimecontract.ModelCacheQualification{Mechanism: "VLLM_CACHE_SALT", Status: "SUPPORTED", Source: "OBSERVED", BackendVersion: "0.23.0", EvidenceSHA256: strings.Repeat("b", 64)}}}}}
	config.Models[0].CachePolicy.Qualification.RouteSHA256, _ = config.ModelCacheRouteDigest("tiny")
	return config
}

// TestModelCacheQualificationDoesNotInventGuarantees separates requested/default, optional degradation and strict pre-inference refusal across APIs.
// TestModelCacheQualificationDoesNotInventGuarantees разделяет requested/default, optional degradation и strict pre-inference refusal разных API.
func TestModelCacheQualificationDoesNotInventGuarantees(t *testing.T) {
	for _, name := range []string{"default", "qualified salt", "no reuse", "no store optional", "no store required", "unknown optional", "unknown required", "declared required", "wrong API", "openai optional", "openai strict scope", "anthropic optional", "anthropic no reuse"} {
		t.Run(name, func(t *testing.T) {
			config := cacheProviderFixture()
			policy := config.Models[0].CachePolicy
			want, denied := "VLLM_CACHE_SALT", false
			switch name {
			case "default":
				config.Models[0].CachePolicy = nil
				want = "NONE"
			case "no reuse":
				policy.NoReuse = true
				policy.Required = true
			case "no store optional", "no store required":
				policy.NoStore = true
				policy.Required = name == "no store required"
				want = "NONE"
				denied = true
			case "unknown optional", "unknown required":
				policy.Qualification.Status = "UNKNOWN"
				policy.Qualification.Source = "NONE"
				policy.Qualification.BackendVersion = ""
				policy.Qualification.EvidenceSHA256 = ""
				policy.Required = name == "unknown required"
				want = "NONE"
				denied = policy.Required
			case "declared required":
				policy.Qualification.Source = "DECLARED"
				policy.Required = true
				denied = true
			case "wrong API":
				config.API = "MESSAGES"
				want = "NONE"
			case "openai optional", "openai strict scope":
				config.API = "RESPONSES"
				config.RouteKind = "DIRECT"
				policy.Qualification.Mechanism = "OPENAI_PROMPT_CACHE_KEY"
				policy.Required = name == "openai strict scope"
				want = "OPENAI_PROMPT_CACHE_KEY"
				denied = policy.Required
			case "anthropic optional", "anthropic no reuse":
				config.API = "MESSAGES"
				config.RouteKind = "DIRECT"
				policy.Qualification.Mechanism = "ANTHROPIC_EPHEMERAL"
				policy.NoReuse = name == "anthropic no reuse"
				policy.Required = policy.NoReuse
				want = "ANTHROPIC_EPHEMERAL"
				denied = policy.Required
			}
			if policy != nil && policy.Qualification.Source != "NONE" {
				policy.Qualification.RouteSHA256, _ = config.ModelCacheRouteDigest("tiny")
			} else if policy != nil {
				policy.Qualification.RouteSHA256 = ""
			}
			if config.Validate() != nil {
				t.Fatal("test policy is structurally invalid")
			}
			actual, err := config.ResolveModelCache("tiny")
			if (err != nil) != denied {
				t.Fatal("mandatory guarantee was bypassed or optional inference blocked")
			}
			if !denied && (actual.Mechanism != want || actual.NoStore || actual.PhysicalIsolation != "NOT_PROVEN") {
				t.Fatal("cache resolution fabricated a mechanism or physical guarantee")
			}
			if name == "no reuse" && !actual.NoReuse {
				t.Fatal("no reuse request lost")
			}
			if name == "openai optional" && actual.ReuseScope != "PROVIDER_ACCOUNT_ROUTING_KEY" {
				t.Fatal("routing key was misrepresented as tenant cache isolation")
			}
		})
	}
}

// TestModelCacheNamespaceTracksAuthorityNotRunNoise proves stable trusted partitions and separation across organizations, users, accounts, projects and sessions.
// TestModelCacheNamespaceTracksAuthorityNotRunNoise проверяет stable trusted partitions и разделение организаций, пользователей, accounts, проектов и сессий.
func TestModelCacheNamespaceTracksAuthorityNotRunNoise(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	input := runtimecontract.RunnerInput{OrganizationRef: "org_synthetic", ProjectRef: "prj_synthetic", RootInitiatorRef: "usr_synthetic", ProviderAccountRef: "pacc_synthetic", AgentRef: "agt_synthetic", SessionRef: "ses_synthetic"}
	base, err := runtimecontract.ModelCacheNamespace(key, input, "SESSION")
	if err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.RunRef = "run_other"
	changed.Attempt = 19
	changed.RuntimeRevisionDigest = strings.Repeat("f", 64)
	again, err := runtimecontract.ModelCacheNamespace(key, changed, "SESSION")
	if err != nil || again != base {
		t.Fatal("run noise invalidated stable trusted partition")
	}
	for _, field := range []string{"organization", "project", "initiator", "account", "agent", "session", "key"} {
		changed = input
		otherKey := append([]byte(nil), key...)
		switch field {
		case "organization":
			changed.OrganizationRef = "org_other"
		case "project":
			changed.ProjectRef = "prj_other"
		case "initiator":
			changed.RootInitiatorRef = "usr_other"
		case "account":
			changed.ProviderAccountRef = "pacc_other"
		case "agent":
			changed.AgentRef = "agt_other"
		case "session":
			changed.SessionRef = "ses_other"
		case "key":
			otherKey[0]++
		}
		next, err := runtimecontract.ModelCacheNamespace(otherKey, changed, "SESSION")
		if err != nil || next == base {
			t.Fatal("different authority reused native cache namespace")
		}
	}
	project, _ := runtimecontract.ModelCacheNamespace(key, input, "PROJECT")
	changed = input
	changed.RootInitiatorRef = "usr_other"
	other, _ := runtimecontract.ModelCacheNamespace(key, changed, "PROJECT")
	if project == other {
		t.Fatal("personal authority collapsed into common project namespace")
	}
	input.RootInitiatorRef = ""
	if _, err := runtimecontract.ModelCacheNamespace(key, input, "PROJECT"); err == nil {
		t.Fatal("missing root initiator accepted")
	}
}

// TestCacheNamespaceCredentialHasIndependentEntropyAndExactReconciliation validates independent secret generation, retry equivalence and rejection of different model credentials.
// TestCacheNamespaceCredentialHasIndependentEntropyAndExactReconciliation проверяет независимую генерацию secret, retry equivalence и отказ другого model credential.
func TestCacheNamespaceCredentialHasIndependentEntropyAndExactReconciliation(t *testing.T) {
	first, err := runtimecontract.EncodeGatewayProviderAuthentication("opencode-test-gateway", []byte("synthetic-key-first"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(first)
	second, err := runtimecontract.EncodeGatewayProviderAuthentication("opencode-test-gateway", []byte("synthetic-key-first"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(second)
	key, err := runtimecontract.DecodeModelCacheNamespaceKey(first, "opencode-test-gateway")
	if err != nil || len(key) != 32 {
		t.Fatal("independent cache key missing")
	}
	defer clear(key)
	if bytes.Equal(first, second) || !runtimecontract.SameGeneratedGatewayCredential(first, second) {
		t.Fatal("namespace entropy or exact-attempt reconciliation failed")
	}
	var different map[string]any
	if json.Unmarshal(second, &different) != nil {
		t.Fatal("invalid fixture")
	}
	different["API_KEY"] = "synthetic-key-other"
	raw, _ := json.Marshal(different)
	if runtimecontract.SameGeneratedGatewayCredential(first, raw) {
		t.Fatal("changed model API key was accepted as same materialization")
	}
	different["API_KEY"] = "synthetic-key-first"
	different["cache_namespace_key"] = "public-project-id"
	raw, _ = json.Marshal(different)
	if _, err := runtimecontract.DecodeModelCacheNamespaceKey(raw, "opencode-test-gateway"); err == nil {
		t.Fatal("public identifier was accepted as namespace key")
	}
}

// TestLiteLLMResponseCacheBypassRequiresQualifiedProxy rejects unqualified proxy versions even when prefix reuse is optional.
// TestLiteLLMResponseCacheBypassRequiresQualifiedProxy отклоняет неквалифицированную версию proxy даже при optional prefix reuse.
func TestLiteLLMResponseCacheBypassRequiresQualifiedProxy(t *testing.T) {
	for _, version := range []string{runtimecontract.QualifiedLiteLLMVersion, "1.104.1", "2.0.0"} {
		config := cacheProviderFixture()
		config.RouteKind = "LITELLM"
		config.RouteVersion = version
		config.Models[0].CachePolicy = nil
		resolution, err := config.ResolveModelCache("tiny")
		qualified := version == runtimecontract.QualifiedLiteLLMVersion
		if (err == nil) != qualified || !qualified && resolution.ProxyResponseCache != "UNQUALIFIED" {
			t.Fatal("unqualified proxy acquired response cache guarantee")
		}
	}
}
