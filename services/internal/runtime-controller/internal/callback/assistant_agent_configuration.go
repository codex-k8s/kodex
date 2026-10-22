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

// assistantAgentConfigurationAvailable requires assistant scope, an exact AGENT
// native context with positive version and instruction-draft or update permission
// in the snapshot. It is a discovery predicate, not current owner authorization.
// assistantAgentConfigurationAvailable требует assistant scope, точный AGENT native
// context с positive version и snapshot instruction-draft/update permission.
// Это discovery predicate, не текущая owner authorization.
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

// assistantAgentInstructionRead is the bounded published/effective instruction
// read shape carrying exact content digest and revision metadata, not a mutation grant.
// assistantAgentInstructionRead — форма чтения published/effective instruction
// с content digest и revision metadata, не mutation grant.
type assistantAgentInstructionRead struct {
	Ref, State, Content, Digest string
	VersionNumber               int32
}

// assistantAgentReadSnapshot models the full eligible employee configuration,
// including runtime identity, published/effective instructions and binding provenance.
// It contains no credential descriptors and does not represent current execution pins.
// assistantAgentReadSnapshot моделирует полную eligible employee configuration
// с runtime identity,published/effective instructions и binding provenance.
// Credential descriptors отсутствуют; это не current execution pins.
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

// castAssistantAgentConfiguration checks exact native context, tenant/project,
// entity version and allowed response shape, then binds canonical closed JSON and
// instruction metadata to its digest. Mismatches return a fixed error with no snapshot;
// it relies on the protected caller/owner read for authority and does not apply changes.
// castAssistantAgentConfiguration проверяет native context,tenant/project,entity version
// и допустимую форму response, связывая canonical closed JSON и instruction metadata
// с digest. Mismatch даёт фиксированную ошибку без snapshot; authority устанавливает
// protected caller/owner read, а не этот caster; изменения не применяются.
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
	var typed assistantAgentReadSnapshot
	if !decodeAssistantConfigurationSnapshot(raw, configuration.GetConfigurationSha256(), &typed) || typed.AgentRef != configuration.GetAgentRef() || typed.ProjectRef != configuration.GetProjectRef() || !validAssistantAgentReadSnapshot(typed) {
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

// validAssistantAgentReadSnapshot checks bounded state/profile text, UTF-8/NUL,
// instruction content digests and binding/effective-revision consistency, allowing
// a larger effective template and an unbound nil effective instruction. It does not
// resolve refs or grant access, and different fields retain their byte-based bounds.
// validAssistantAgentReadSnapshot проверяет state/profile text bounds,UTF-8/NUL,
// instruction digests и binding/effective consistency; допускает больший effective
// template и nil effective instruction без binding. Refs не разрешаются и доступ
// не выдаётся; byte-based bounds разных полей сохранены.
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
