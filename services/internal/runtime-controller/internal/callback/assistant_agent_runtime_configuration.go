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
}

func castAssistantAgentRuntimeConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant agent runtime configuration response is invalid")
	if !assistantAgentConfigurationAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "AGENT" || request.GetEntityRef() != input.AssistantContext.EntityRef || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef || response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil || response.GetWorkflowConfiguration() != nil || response.GetAgentConfiguration() != nil {
		return nil, invalid
	}
	configuration := response.GetAgentRuntimeConfiguration()
	if configuration == nil || len(configuration.ProtoReflect().GetUnknown()) != 0 || configuration.GetAgentRef() != request.GetEntityRef() || configuration.GetProjectRef() != response.GetProjectRef() || configuration.GetVersion() != *input.AssistantContext.EntityVersion || configuration.GetVersion() > 9007199254740991 {
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
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 16 || !onlyKeys(snapshot, "agent_ref", "project_ref", "version", "binding_ref", "binding_version", "binding_digest", "environment_ref", "environment_name", "environment_version", "published_version_ref", "published_revision", "published_digest", "image", "configured_tools", "verified_tool_inventory", "verified_tool_inventory_sha256") {
		return nil, invalid
	}
	image, ok := snapshot["image"].(map[string]any)
	if !ok || len(image) != 5 || !onlyKeys(image, "artifact_ref", "recipe_ref", "reference", "digest", "recipe_generation") {
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

func validAssistantAgentRuntimeReadSnapshot(value assistantAgentRuntimeReadSnapshot) bool {
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
