package runtimecontract

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// QualifiedLiteLLMVersion pins the proxy release whose read/write bypass was qualified with a real Redis backend.
// QualifiedLiteLLMVersion закрепляет proxy release с проверенным на настоящем Redis read/write bypass.
const QualifiedLiteLLMVersion = "1.104.2"

// ErrInferenceCachePolicy rejects an unqualified mandatory guarantee before a model request, without disclosing routing or secret material.
// ErrInferenceCachePolicy отклоняет неподтверждённую обязательную гарантию до model request без раскрытия routing или secret material.
var ErrInferenceCachePolicy = errors.New("inference cache policy cannot be enforced")

// ModelCachePolicy separates requested behavior from operator-owned qualification for this exact model and route.
// NoHints and provider defaults never assert that the upstream cache is disabled.
// ModelCachePolicy отделяет requested behavior от operator-owned qualification точной модели и маршрута.
// NoHints и provider defaults никогда не утверждают, что upstream cache отключён.
type ModelCachePolicy struct {
	Revision      int                     `json:"revision"`
	Optimization  string                  `json:"optimization"`
	ReuseScope    string                  `json:"reuse_scope"`
	NoReuse       bool                    `json:"no_reuse"`
	NoStore       bool                    `json:"no_store"`
	Required      bool                    `json:"required"`
	Qualification ModelCacheQualification `json:"qualification"`
}

// ModelCacheQualification records an operator attestation, never a capability inferred from HTTP200 or a synthetic backend.
// A changed adapter, model or route changes the containing configuration digest and requires renewed qualification.
// ModelCacheQualification хранит operator attestation, не capability из HTTP200 или synthetic backend.
// Смена adapter, модели или маршрута меняет configuration digest и требует новой qualification.
type ModelCacheQualification struct {
	Mechanism      string `json:"mechanism"`
	Status         string `json:"status"`
	Source         string `json:"source"`
	BackendVersion string `json:"backend_version"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	RouteSHA256    string `json:"route_sha256"`
}

// EffectiveModelCache is a nonsecret read projection; it neither contains the namespace nor grants execution authority.
// EffectiveModelCache — несекретная read projection без namespace и без выдачи execution authority.
type EffectiveModelCache struct {
	Requested          *ModelCachePolicy `json:"requested,omitempty"`
	Mechanism          string            `json:"mechanism"`
	ReuseScope         string            `json:"reuse_scope"`
	Status             string            `json:"status"`
	Reason             string            `json:"reason"`
	NoReuse            bool              `json:"no_reuse"`
	NoStore            bool              `json:"no_store"`
	ProxyResponseCache string            `json:"proxy_response_cache"`
	PhysicalIsolation  string            `json:"physical_isolation"`
}

// validate checks a closed intent/qualification vocabulary without trusting a declared capability as observed enforcement.
// validate проверяет закрытый intent/qualification vocabulary, не принимая declared capability за observed enforcement.
func (policy ModelCachePolicy) validate() error {
	if policy.Revision != 1 || !oneCacheValue(policy.Optimization, "PROVIDER_DEFAULT", "NO_HINTS", "OPTIMIZE") ||
		!oneCacheValue(policy.ReuseScope, "PROJECT", "INITIATOR", "SESSION") ||
		!oneCacheValue(policy.Qualification.Mechanism, "NONE", "VLLM_CACHE_SALT", "OPENAI_PROMPT_CACHE_KEY", "ANTHROPIC_EPHEMERAL") ||
		!oneCacheValue(policy.Qualification.Status, "SUPPORTED", "UNSUPPORTED", "UNKNOWN") ||
		!oneCacheValue(policy.Qualification.Source, "NONE", "DECLARED", "OBSERVED") {
		return ErrInferenceCachePolicy
	}
	q := policy.Qualification
	if q.Source == "NONE" {
		if q.Status != "UNKNOWN" || q.BackendVersion != "" || q.EvidenceSHA256 != "" || q.RouteSHA256 != "" {
			return ErrInferenceCachePolicy
		}
	} else if len(q.BackendVersion) > 64 || !modelRouteVersionPattern.MatchString(q.BackendVersion) || !sha256Pattern.MatchString(q.EvidenceSHA256) || !sha256Pattern.MatchString(q.RouteSHA256) {
		return ErrInferenceCachePolicy
	}
	return nil
}

// oneCacheValue checks a small closed enum without normalization or caller-defined extensions.
// oneCacheValue проверяет малый закрытый enum без normalization или caller-defined extensions.
func oneCacheValue(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}

// ResolveModelCache selects only qualified protocol-specific controls and fails closed for mandatory unsupported guarantees.
// Salt qualifies reuse partitioning, not physical memory isolation; no implemented mechanism promises inference-cache NO_STORE.
// ResolveModelCache выбирает лишь qualified protocol-specific controls и закрыто отклоняет mandatory unsupported guarantees.
// Salt подтверждает reuse partitioning, не physical memory isolation; ни один механизм не обещает inference-cache NO_STORE.
func (config ModelProviderConfiguration) ResolveModelCache(modelID string) (EffectiveModelCache, error) {
	result := EffectiveModelCache{Mechanism: "NONE", ReuseScope: "PROVIDER_BOUNDARY", Status: "UNKNOWN", Reason: "NO_MANAGED_HINTS", ProxyResponseCache: "NOT_APPLICABLE", PhysicalIsolation: "NOT_PROVEN"}
	if config.RouteKind == "LITELLM" {
		result.ProxyResponseCache = "BYPASS_READ_WRITE"
	}
	model, found := config.Model(modelID)
	if !found {
		return result, ErrModelProviderConfiguration
	}
	if config.RouteKind == "LITELLM" && config.RouteVersion != QualifiedLiteLLMVersion {
		result.Status, result.Reason, result.ProxyResponseCache = "UNKNOWN", "PROXY_RESPONSE_CACHE_VERSION_UNQUALIFIED", "UNQUALIFIED"
		result.Requested = model.CachePolicy
		return result, ErrInferenceCachePolicy
	}
	policy := model.CachePolicy
	result.Requested = policy
	if policy == nil {
		return result, nil
	}
	if policy.validate() != nil {
		return result, ErrInferenceCachePolicy
	}
	if policy.Optimization != "OPTIMIZE" && !policy.NoReuse && !policy.NoStore {
		if policy.Required {
			result.Status, result.Reason = "UNSUPPORTED", "REQUIRED_POLICY_HAS_NO_ENFORCEMENT"
			return result, ErrInferenceCachePolicy
		}
		return result, nil
	}
	q := policy.Qualification
	result.Status, result.Reason = q.Status, "QUALIFICATION_UNAVAILABLE"
	routeDigest, _ := config.ModelCacheRouteDigest(modelID)
	compatible := q.RouteSHA256 == routeDigest && q.Status == "SUPPORTED" && q.Source == "OBSERVED" && q.Mechanism != "NONE"
	switch q.Mechanism {
	case "VLLM_CACHE_SALT":
		compatible = compatible && (config.API == "" || config.API == ModelAPIChatCompletions) && (config.RouteKind == "VLLM" || config.RouteKind == "LITELLM") && (config.RouteKind != "VLLM" || q.BackendVersion == config.RouteVersion)
	case "OPENAI_PROMPT_CACHE_KEY":
		compatible = compatible && config.API == ModelAPIResponses && (config.RouteKind == "" || config.RouteKind == "DIRECT" || config.RouteKind == "LITELLM") && !policy.NoReuse
	case "ANTHROPIC_EPHEMERAL":
		compatible = compatible && config.API == ModelAPIMessages && !policy.NoReuse
	default:
		compatible = false
	}
	if policy.NoStore {
		compatible = false
		result.Reason = "NO_STORE_UNSUPPORTED"
	}
	if !compatible {
		if policy.NoStore {
			result.Status = "UNSUPPORTED"
		} else if q.Source != "OBSERVED" || q.RouteSHA256 != routeDigest {
			result.Status = "UNKNOWN"
		} else if q.Status == "SUPPORTED" {
			result.Status = "UNSUPPORTED"
		}
		if result.Reason != "NO_STORE_UNSUPPORTED" && q.Status == "SUPPORTED" {
			result.Reason = "EXACT_ROUTE_NOT_QUALIFIED"
		}
		if policy.Required || policy.NoReuse || policy.NoStore {
			return result, ErrInferenceCachePolicy
		}
		return result, nil
	}
	result.Mechanism, result.ReuseScope, result.Status, result.Reason = q.Mechanism, policy.ReuseScope, "SUPPORTED", "OPERATOR_OBSERVED"
	result.NoReuse = policy.NoReuse
	if q.Mechanism == "OPENAI_PROMPT_CACHE_KEY" {
		result.ReuseScope = "PROVIDER_ACCOUNT_ROUTING_KEY"
	}
	if q.Mechanism == "ANTHROPIC_EPHEMERAL" {
		result.ReuseScope = "PROVIDER_ACCOUNT"
	}
	if policy.Required && policy.ReuseScope != "" && q.Mechanism != "VLLM_CACHE_SALT" {
		return result, ErrInferenceCachePolicy
	}
	return result, nil
}

// ModelCacheNamespace derives a stable secret partition from trusted immutable owner identity and a separate backend key.
// Initiator is always included even for project reuse: different personal authorities never share a project-wide salt.
// ModelCacheNamespace выводит stable secret partition из trusted immutable owner identity и отдельного backend key.
// Initiator включается даже при project reuse: разные personal authorities не делят project-wide salt.
func ModelCacheNamespace(key []byte, input RunnerInput, scope string) (string, error) {
	if len(key) != 32 || input.OrganizationRef == "" || input.RootInitiatorRef == "" || input.ProviderAccountRef == "" || input.AgentRef == "" ||
		!oneCacheValue(scope, "PROJECT", "INITIATOR", "SESSION") || scope == "SESSION" && input.SessionRef == "" {
		return "", ErrInferenceCachePolicy
	}
	parts := []string{"kodex.inference-cache.v1", input.OrganizationRef, input.ProjectRef, input.RootInitiatorRef, input.ProviderAccountRef, scope}
	if input.ProjectRef == "" {
		parts = append(parts, string(input.AssistantScope), input.AgentRef)
	}
	if scope == "SESSION" {
		parts = append(parts, input.AgentRef, input.SessionRef)
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		return "", ErrInferenceCachePolicy
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// ModelCacheRouteDigest binds qualification to exact nonsecret engine, model and transport pins without hashing the qualification into itself.
// ModelCacheRouteDigest связывает qualification с exact nonsecret engine, model и transport pins без self-referential hash.
func (config ModelProviderConfiguration) ModelCacheRouteDigest(modelID string) (string, error) {
	model, found := config.Model(modelID)
	if !found {
		return "", ErrModelProviderConfiguration
	}
	model.CachePolicy = nil
	route := struct {
		Engine, EngineVersion, ProviderID, BaseURL, API, RouteKind, RouteVersion string
		AddressCIDRs                                                             []string
		Model                                                                    ModelProviderModel
	}{config.Engine, config.EngineVersion, config.ProviderID, config.BaseURL, config.API, config.RouteKind, config.RouteVersion, config.AddressCIDRs, model}
	raw, err := json.Marshal(route)
	if err != nil {
		return "", ErrInferenceCachePolicy
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
