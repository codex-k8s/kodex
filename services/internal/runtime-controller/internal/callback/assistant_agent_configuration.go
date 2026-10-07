package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func assistantAgentConfigurationAvailable(input runtimecontract.RunnerInput) bool {
	context := input.AssistantContext
	if !input.IsAssistant() || context == nil || context.EntityKind != "AGENT" || !validAssistantResourceRef(context.EntityRef) || context.EntityVersion == nil || *context.EntityVersion < 1 {
		return false
	}
	for _, operation := range context.AllowedOperations {
		if operation == "CREATE_INSTRUCTION_DRAFT" || operation == "UPDATE_AGENT" {
			return true
		}
	}
	return false
}

type assistantAgentInstructionRead struct {
	Ref, State, Content, Digest string
	VersionNumber               int32
}

type assistantAgentReadSnapshot struct {
	AgentRef, ProjectRef, Name, Purpose, RoleDefinitionRef, RoleDefinitionName, RoleDescription, AvatarURL, State string
	Enabled                                                                                                       bool
	Runtime                                                                                                       struct{ Key, Name, Provider, Model, Revision string }
	PublishedInstructions, EffectiveInstructions                                                                  *assistantAgentInstructionRead
	InstructionBinding                                                                                            struct {
		Ref, RevisionRef string
		Version          int64
		Effective        bool
	}
}

func castAssistantAgentConfiguration(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant agent configuration response is invalid")
	if !assistantAgentConfigurationAvailable(input) || request.GetAssistantRef() != input.AgentRef || request.GetEntityKind() != "AGENT" || request.GetEntityRef() != input.AssistantContext.EntityRef || response == nil ||
		len(response.ProtoReflect().GetUnknown()) != 0 || response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef || response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetNextOffset() != 0 || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 || response.GetRecipientIntegrationGrants() != nil || response.GetCurrentConfiguration() != nil || response.GetWorkflowConfiguration() != nil {
		return nil, invalid
	}
	configuration := response.GetAgentConfiguration()
	if configuration == nil || len(configuration.ProtoReflect().GetUnknown()) != 0 || configuration.GetAgentRef() != request.GetEntityRef() || configuration.GetProjectRef() != response.GetProjectRef() || configuration.GetVersion() != *input.AssistantContext.EntityVersion || configuration.GetVersion() > 9007199254740991 || len(configuration.GetConfigurationJson()) == 0 || len(configuration.GetConfigurationJson()) > maximumAssistantCurrentConfigurationBytes {
		return nil, invalid
	}
	raw := configuration.GetConfigurationJson()
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != configuration.GetConfigurationSha256() {
		return nil, invalid
	}
	var typed assistantAgentReadSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&typed) != nil || typed.AgentRef != configuration.GetAgentRef() || typed.ProjectRef != configuration.GetProjectRef() || !validAssistantAgentReadSnapshot(typed) {
		return nil, invalid
	}
	var snapshot map[string]any
	if json.Unmarshal(raw, &snapshot) != nil || len(snapshot) != 14 || !onlyKeys(snapshot, "agentRef", "projectRef", "name", "purpose", "roleDefinitionRef", "roleDefinitionName", "roleDescription", "avatarUrl", "state", "enabled", "runtime", "publishedInstructions", "effectiveInstructions", "instructionBinding") {
		return nil, invalid
	}
	for _, nested := range []struct {
		key      string
		fields   []string
		nullable bool
	}{
		{"runtime", []string{"key", "name", "provider", "model", "revision"}, false},
		{"publishedInstructions", []string{"ref", "versionNumber", "state", "content", "digest"}, true},
		{"effectiveInstructions", []string{"ref", "versionNumber", "state", "content", "digest"}, true},
		{"instructionBinding", []string{"ref", "version", "revisionRef", "effective"}, false},
	} {
		if snapshot[nested.key] == nil && nested.nullable {
			continue
		}
		fields, ok := snapshot[nested.key].(map[string]any)
		if !ok || len(fields) != len(nested.fields) || !onlyKeys(fields, nested.fields...) {
			return nil, invalid
		}
	}
	canonical, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(canonical, raw) {
		return nil, invalid
	}
	return map[string]any{"kind": "AGENT_CONFIGURATION", "assistant_ref": input.AgentRef, "scope_kind": "PROJECT", "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "entries": []any{}, "next_offset": 0,
		"agent_configuration": map[string]any{"agent_ref": configuration.GetAgentRef(), "project_ref": configuration.GetProjectRef(), "version": configuration.GetVersion(), "configuration_sha256": configuration.GetConfigurationSha256(), "configuration": snapshot}}, nil
}

func validAssistantAgentReadSnapshot(value assistantAgentReadSnapshot) bool {
	if !validAssistantResourceRef(value.RoleDefinitionRef) || strings.TrimSpace(value.Name) == "" || len(value.Name) > 160 || len(value.Purpose) > 2000 || len(value.RoleDescription) > 20000 || len(value.AvatarURL) > 2048 || value.State != "READY" && value.State != "DRAFT" && value.State != "RUNNING" && value.State != "DISABLED" || !validAssistantResourceRef(value.InstructionBinding.Ref) || value.InstructionBinding.Version < 1 || value.InstructionBinding.Version > 9007199254740991 {
		return false
	}
	for _, text := range []string{value.Name, value.Purpose, value.RoleDefinitionName, value.RoleDescription, value.AvatarURL, value.Runtime.Key, value.Runtime.Name, value.Runtime.Provider, value.Runtime.Model, value.Runtime.Revision} {
		if len(text) > 65536 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
			return false
		}
	}
	for index, item := range []*assistantAgentInstructionRead{value.PublishedInstructions, value.EffectiveInstructions} {
		if item == nil {
			continue
		}
		digest := sha256.Sum256([]byte(item.Content))
		maximumBytes := 65536
		if index == 1 {
			maximumBytes = 256 << 10
		}
		if !validAssistantResourceRef(item.Ref) || item.VersionNumber < 1 || item.State != "PUBLISHED" || len(item.Content) > maximumBytes || !utf8.ValidString(item.Content) || strings.ContainsRune(item.Content, 0) || hex.EncodeToString(digest[:]) != item.Digest {
			return false
		}
	}
	if value.EffectiveInstructions == nil {
		return value.InstructionBinding.RevisionRef == "" && !value.InstructionBinding.Effective
	}
	return value.PublishedInstructions != nil && validAssistantResourceRef(value.InstructionBinding.RevisionRef) && (!value.InstructionBinding.Effective || value.InstructionBinding.RevisionRef == value.EffectiveInstructions.Ref)
}
