package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// assistantAgentRuntimeReadSnapshot is a closed safe runtime read shape with model,
// resources, published environment/image pins, configured tool names and observed
// inventory provenance; environment values, credentials and instructions are absent.
// assistantAgentRuntimeReadSnapshot — closed safe runtime read shape с model,resources,
// published environment/image pins,configured tool names и inventory provenance;
// environment values,credentials и instructions отсутствуют.
type assistantAgentRuntimeReadSnapshot struct {
	AgentRef            string `json:"agent_ref"`
	ProjectRef          string `json:"project_ref"`
	Version             int64  `json:"version"`
	BindingRef          string `json:"binding_ref"`
	BindingVersion      int64  `json:"binding_version"`
	BindingDigest       string `json:"binding_digest"`
	EnvironmentRef      string `json:"environment_ref"`
	EnvironmentName     string `json:"environment_name"`
	EnvironmentVersion  int64  `json:"environment_version"`
	PublishedVersionRef string `json:"published_version_ref"`
	PublishedRevision   int64  `json:"published_revision"`
	PublishedDigest     string `json:"published_digest"`
	Image               struct {
		ArtifactRef      string `json:"artifact_ref"`
		RecipeRef        string `json:"recipe_ref"`
		Reference        string `json:"reference"`
		Digest           string `json:"digest"`
		RecipeGeneration int64  `json:"recipe_generation"`
	} `json:"image"`
	ConfiguredTools           []string                            `json:"configured_tools"`
	VerifiedToolInventory     *runtimecontract.ImageToolInventory `json:"verified_tool_inventory"`
	VerifiedToolInventoryHash string                              `json:"verified_tool_inventory_sha256"`
	ModelConfiguration        struct {
		Ref                string `json:"ref"`
		Version            int64  `json:"version"`
		Digest             string `json:"digest"`
		Provider           string `json:"provider"`
		Model              string `json:"model"`
		RuntimeProfileRef  string `json:"runtime_profile_ref"`
		ProviderPolicyMode string `json:"provider_policy_mode"`
		ProviderAccounts   []struct {
			AccountRef string `json:"accountRef"`
			Weight     int64  `json:"weight"`
		} `json:"provider_accounts"`
		ReasoningEffort         string `json:"reasoning_effort"`
		WebSearchMode           string `json:"web_search_mode"`
		PublishedOverlayRef     string `json:"published_overlay_ref"`
		PublishedOverlayVersion int64  `json:"published_overlay_version"`
		PublishedOverlayDigest  string `json:"published_overlay_digest"`
		HasDraftOverlay         bool   `json:"has_draft_overlay"`
	} `json:"model_configuration"`
	Resources runtimecontract.RuntimeResourcePolicy `json:"resources"`
}

// assistantAgentRuntimeConfigurationAvailable advertises safe named runtime reads
// for SYSTEM or PROJECT with a valid project ref, without requiring an agent screen.
// Current recipient eligibility is still resolved by the owner.
// assistantAgentRuntimeConfigurationAvailable публикует safe named runtime reads
// для SYSTEM либо PROJECT с valid project ref без требования agent screen.
// Текущую recipient eligibility по-прежнему определяет owner.
func assistantAgentRuntimeConfigurationAvailable(input runtimecontract.RunnerInput) bool {
	return input.IsAssistant() && (input.AssistantScope == runtimecontract.AssistantScopeSystem ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && validAssistantResourceRef(input.ProjectRef))
}

// castAssistantAgentRuntimeConfiguration binds exact owner response/entity/project
// and version to bounded canonical JSON, checks closed nested image/model/resources
// and the digest of exact projected inventory bytes. It rejects mixed/private shapes
// without treating a reconstructed struct digest as inventory admission evidence.
// castAssistantAgentRuntimeConfiguration связывает exact owner response/entity/project
// и version с bounded canonical JSON, проверяет closed image/model/resources
// и digest точных projected inventory bytes. Mixed/private shapes отвергаются;
// digest восстановленного struct не выдаётся за inventory admission evidence.
func castAssistantAgentRuntimeConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant agent runtime configuration response is invalid")
	if !assistantAgentRuntimeConfigurationAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "AGENT" || !validAssistantResourceRef(request.GetEntityRef()) || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef || response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil || response.GetWorkflowConfiguration() != nil || response.GetAgentConfiguration() != nil {
		return nil, invalid
	}
	configuration := response.GetAgentRuntimeConfiguration()
	if configuration == nil || len(configuration.ProtoReflect().GetUnknown()) != 0 || configuration.GetAgentRef() != request.GetEntityRef() || configuration.GetProjectRef() != response.GetProjectRef() || configuration.GetVersion() < 1 || configuration.GetVersion() > 9007199254740991 {
		return nil, invalid
	}
	raw := configuration.GetConfigurationJson()
	hash := sha256.Sum256(raw)
	if len(raw) == 0 || len(raw) > maximumAssistantCurrentConfigurationBytes || !utf8.Valid(raw) || hex.EncodeToString(hash[:]) != configuration.GetConfigurationSha256() {
		return nil, invalid
	}
	var typed assistantAgentRuntimeReadSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil || decoder.Decode(new(any)) != io.EOF || typed.AgentRef != configuration.GetAgentRef() || typed.ProjectRef != configuration.GetProjectRef() || typed.Version != configuration.GetVersion() || !validAssistantAgentRuntimeReadSnapshot(typed) {
		return nil, invalid
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 18 || !onlyKeys(snapshot, "agent_ref", "project_ref", "version", "binding_ref", "binding_version", "binding_digest", "environment_ref", "environment_name", "environment_version", "published_version_ref", "published_revision", "published_digest", "image", "configured_tools", "verified_tool_inventory", "verified_tool_inventory_sha256", "model_configuration", "resources") {
		return nil, invalid
	}
	image, ok := snapshot["image"].(map[string]any)
	if !ok || len(image) != 5 || !onlyKeys(image, "artifact_ref", "recipe_ref", "reference", "digest", "recipe_generation") {
		return nil, invalid
	}
	model, ok := snapshot["model_configuration"].(map[string]any)
	if !ok {
		return nil, invalid
	}
	legacy := len(model) == 5 && onlyKeys(model, "ref", "version", "digest", "provider", "model")
	prepared := len(model) == 14 && onlyKeys(model, "ref", "version", "digest", "provider", "model", "runtime_profile_ref", "provider_policy_mode", "provider_accounts", "reasoning_effort", "web_search_mode", "published_overlay_ref", "published_overlay_version", "published_overlay_digest", "has_draft_overlay")
	if !legacy && !prepared {
		return nil, invalid
	}
	if prepared {
		if _, ok := model["has_draft_overlay"].(bool); !ok {
			return nil, invalid
		}
		for _, key := range []string{"reasoning_effort", "web_search_mode"} {
			if _, ok := model[key].(string); !ok {
				return nil, invalid
			}
		}
		if !validAssistantAgentRuntimePreparationSnapshot(typed) {
			return nil, invalid
		}
		accounts, ok := model["provider_accounts"].([]any)
		if !ok || len(accounts) != len(typed.ModelConfiguration.ProviderAccounts) {
			return nil, invalid
		}
		for _, entry := range accounts {
			account, ok := entry.(map[string]any)
			if !ok || len(account) != 2 || !onlyKeys(account, "accountRef", "weight") {
				return nil, invalid
			}
		}
	}
	resources, ok := snapshot["resources"].(map[string]any)
	if !ok || len(resources) != 6 || !onlyKeys(resources, "cpu_request_milli", "cpu_limit_milli", "memory_request_mib", "memory_limit_mib", "ephemeral_storage_request_mib", "ephemeral_storage_limit_mib") {
		return nil, invalid
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	// SHA связывает exact canonical whitelist inventory из ответа, а не
	// восстановленный struct и не raw receipt admission bridge.
	inventoryRaw, err := json.Marshal(snapshot["verified_tool_inventory"])
	if err != nil || runtimecontract.ImageInventorySHA256(inventoryRaw) != typed.VerifiedToolInventoryHash {
		return nil, invalid
	}
	return map[string]any{"kind": "AGENT_RUNTIME_CONFIGURATION", "assistant_ref": input.AgentRef, "scope_kind": "PROJECT", "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "entries": []any{}, "next_offset": 0,
		"agent_runtime_configuration": map[string]any{"agent_ref": configuration.GetAgentRef(), "project_ref": configuration.GetProjectRef(), "version": configuration.GetVersion(), "configuration_sha256": configuration.GetConfigurationSha256(), "configuration": snapshot}}, nil
}

// validAssistantAgentRuntimeReadSnapshot validates safe model text, normalized resource
// bounds, refs/versions/digests, pinned image identity and inventory structure for that
// image. Configured tool names are bounded metadata, not proof each tool is present
// or executable; reference validity does not resolve current owner eligibility.
// validAssistantAgentRuntimeReadSnapshot проверяет safe model text,normalized resource
// bounds,refs/versions/digests,pinned image identity и inventory structure этого image.
// Configured tool names — bounded metadata, не доказательство наличия/исполнения
// каждого tool; форма refs не определяет текущую owner eligibility.
func validAssistantAgentRuntimeReadSnapshot(value assistantAgentRuntimeReadSnapshot) bool {
	model, resources := value.ModelConfiguration, value.Resources
	if !validAssistantResourceRef(model.Ref) || model.Version < 1 || model.Version > 9007199254740991 || !validAssistantCatalogDigest(model.Digest) ||
		strings.TrimSpace(model.Provider) == "" || len(model.Provider) > 96 || strings.TrimSpace(model.Model) == "" || len(model.Model) > 160 ||
		!utf8.ValidString(model.Provider) || !utf8.ValidString(model.Model) || strings.ContainsRune(model.Provider, 0) || strings.ContainsRune(model.Model, 0) ||
		resources.WorkspaceLimits != nil {
		return false
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicyWithoutDigests()
	policy.Resources = resources
	if _, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy); err != nil {
		return false
	}
	for _, ref := range []string{value.AgentRef, value.ProjectRef, value.BindingRef, value.EnvironmentRef, value.PublishedVersionRef, value.Image.ArtifactRef, value.Image.RecipeRef} {
		if !validAssistantResourceRef(ref) {
			return false
		}
	}
	for _, version := range []int64{value.Version, value.BindingVersion, value.EnvironmentVersion, value.PublishedRevision, value.Image.RecipeGeneration} {
		if version < 1 || version > 9007199254740991 {
			return false
		}
	}
	if !validAssistantCatalogDigest(value.BindingDigest) || !validAssistantCatalogDigest(value.PublishedDigest) || !assistantCatalogPinnedImagePattern.MatchString(value.Image.Reference) || !strings.HasSuffix(value.Image.Reference, "@"+value.Image.Digest) || !strings.HasPrefix(value.Image.Digest, "sha256:") || !validAssistantCatalogDigest(strings.TrimPrefix(value.Image.Digest, "sha256:")) ||
		strings.TrimSpace(value.EnvironmentName) == "" || !utf8.ValidString(value.EnvironmentName) || utf8.RuneCountInString(value.EnvironmentName) > 160 || strings.ContainsRune(value.EnvironmentName, 0) || len(value.ConfiguredTools) > 128 || value.ConfiguredTools == nil || value.VerifiedToolInventory == nil || value.VerifiedToolInventory.Validate() != nil || value.VerifiedToolInventory.ImageDigest != value.Image.Digest {
		return false
	}
	for _, name := range value.ConfiguredTools {
		if strings.TrimSpace(name) != name || name == "" || len(name) > 160 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
			return false
		}
	}
	return validAssistantCatalogDigest(value.VerifiedToolInventoryHash)
}

// validAssistantAgentRuntimePreparationSnapshot validates all v2-only profile/policy/overlay metadata; legacy reads never synthesize those missing pins.
// validAssistantAgentRuntimePreparationSnapshot проверяет все новые метаданные профиля, политики и overlay v2; устаревшее чтение не синтезирует отсутствующие pins.
func validAssistantAgentRuntimePreparationSnapshot(value assistantAgentRuntimeReadSnapshot) bool {
	model := value.ModelConfiguration
	if !validAssistantResourceRef(model.RuntimeProfileRef) || !validAssistantResourceRef(model.PublishedOverlayRef) || model.PublishedOverlayVersion < 1 || model.PublishedOverlayVersion > 9007199254740991 || !validAssistantCatalogDigest(model.PublishedOverlayDigest) ||
		model.WebSearchMode != "" && !runtimecontract.ValidWebSearchMode(model.WebSearchMode) || model.ReasoningEffort != "" && runtimecontract.ValidateEffectiveReasoningEffort("", model.ReasoningEffort, runtimecontract.ReasoningSupported) != nil ||
		len(model.ProviderAccounts) < 1 || len(model.ProviderAccounts) > 128 || model.ProviderPolicyMode != "FIXED" && model.ProviderPolicyMode != "LEAST_USED" && model.ProviderPolicyMode != "WEIGHTED" || model.ProviderPolicyMode == "FIXED" && len(model.ProviderAccounts) != 1 {
		return false
	}
	seen := make(map[string]bool, len(model.ProviderAccounts))
	for _, account := range model.ProviderAccounts {
		if !validAssistantResourceRef(account.AccountRef) || account.Weight < 1 || account.Weight > 100 || model.ProviderPolicyMode != "WEIGHTED" && account.Weight != 1 || seen[account.AccountRef] {
			return false
		}
		seen[account.AccountRef] = true
	}
	return true
}
