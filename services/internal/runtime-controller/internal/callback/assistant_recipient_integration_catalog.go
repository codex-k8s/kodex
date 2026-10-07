package callback

import (
	"errors"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func assistantRecipientIntegrationCatalogAvailable(input runtimecontract.RunnerInput) bool {
	context := input.AssistantContext
	if !input.IsAssistant() || context == nil || (context.EntityKind != "AGENT" && context.EntityKind != "WORKFLOW") || !validAssistantResourceRef(context.EntityRef) || context.EntityVersion == nil || *context.EntityVersion < 1 {
		return false
	}
	for _, operation := range context.AllowedOperations {
		if operation == "CHANGE_INTEGRATION_GRANT" {
			return true
		}
	}
	return false
}

func castAssistantRecipientIntegrationCatalog(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("assistant recipient integration catalog response is invalid")
	if !assistantRecipientIntegrationCatalogAvailable(input) || request.GetAssistantRef() != input.AgentRef || response == nil || !validAssistantCurrentReadMessage(response.ProtoReflect(), 0) ||
		response.GetKind() != request.GetKind() || response.GetAssistantRef() != input.AgentRef || response.GetOrganizationRef() != input.OrganizationRef ||
		response.GetScopeKind() != "PROJECT" || !validAssistantResourceRef(response.GetProjectRef()) || response.GetAssistantProfileRef() != "" ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && response.GetProjectRef() != input.ProjectRef || response.GetCurrentConfiguration() != nil || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) != 0 ||
		response.GetNextOffset() < 0 || response.GetNextOffset() > 10000 || response.GetNextOffset() != 0 && response.GetNextOffset() != request.GetOffset()+10 {
		return nil, invalid
	}
	catalog := response.GetRecipientIntegrationGrants()
	if catalog == nil || catalog.GetRecipientKind() != input.AssistantContext.EntityKind || catalog.GetRecipientRef() != input.AssistantContext.EntityRef ||
		catalog.GetRecipientVersion() != *input.AssistantContext.EntityVersion || catalog.GetProjectVersion() < 1 || strings.TrimSpace(catalog.GetRecipientName()) == "" ||
		!utf8.ValidString(catalog.GetRecipientName()) || utf8.RuneCountInString(catalog.GetRecipientName()) > 160 || len(catalog.GetEntries()) > maximumAssistantConfigurationEntries ||
		response.GetNextOffset() != 0 && len(catalog.GetEntries()) != 10 {
		return nil, invalid
	}
	entries := []map[string]any{}
	seen := map[string]bool{}
	for _, entry := range catalog.GetEntries() {
		grant, pins := entry.GetGrant(), entry.GetPins()
		if grant == nil || pins == nil || !validAssistantResourceRef(grant.GetConnectionRef()) || grant.GetConnectionVersion() < 1 ||
			strings.TrimSpace(grant.GetConnectionName()) == "" || !utf8.ValidString(grant.GetConnectionName()) || utf8.RuneCountInString(grant.GetConnectionName()) > 160 ||
			grant.GetDefinitionVersion() == "" || len(grant.GetDefinitionVersion()) > 128 || !validAssistantCatalogDigest(grant.GetDefinitionDigest()) ||
			!validAssistantCatalogDigest(pins.GetContextDigest()) || pins.GetConnectionVersion() != grant.GetConnectionVersion() || pins.GetDefinitionVersion() != grant.GetDefinitionVersion() || pins.GetDefinitionDigest() != grant.GetDefinitionDigest() ||
			pins.GetProjectVersion() != catalog.GetProjectVersion() || pins.GetRecipientVersion() != catalog.GetRecipientVersion() || pins.GetWorkflowRevisionRef() != "" {
			return nil, invalid
		}
		candidate, err := projectIntegrationCandidateProjection(grant.GetCandidate())
		if err != nil {
			return nil, invalid
		}
		key := grant.GetConnectionRef() + "\x00" + grant.GetCandidate().GetCapability().GetKey()
		if seen[key] {
			return nil, invalid
		}
		seen[key] = true
		entries = append(entries, map[string]any{"connection_ref": grant.GetConnectionRef(), "connection_name": grant.GetConnectionName(), "connection_version": grant.GetConnectionVersion(), "definition_version": grant.GetDefinitionVersion(), "definition_digest": grant.GetDefinitionDigest(), "candidate": candidate,
			"pins": map[string]any{"context_digest": pins.GetContextDigest(), "connection_version": pins.GetConnectionVersion(), "definition_version": pins.GetDefinitionVersion(), "definition_digest": pins.GetDefinitionDigest(), "project_version": pins.GetProjectVersion(), "recipient_version": pins.GetRecipientVersion()}})
	}
	return map[string]any{"kind": "RECIPIENT_INTEGRATION_GRANTS", "assistant_ref": response.GetAssistantRef(), "scope_kind": "PROJECT", "organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "next_offset": response.GetNextOffset(),
		"recipient_integration_grants": map[string]any{"recipient_kind": catalog.GetRecipientKind(), "recipient_ref": catalog.GetRecipientRef(), "recipient_name": catalog.GetRecipientName(), "recipient_version": catalog.GetRecipientVersion(), "project_version": catalog.GetProjectVersion(), "entries": entries}}, nil
}
