package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/protobuf/proto"
)

func castProjectAssistantIntegrationCatalog(input runtimecontract.RunnerInput, request *controlplanev1.AssistantConfigurationCatalogRequest, response *controlplanev1.AssistantConfigurationCatalogResponse) (map[string]any, error) {
	invalid := errors.New("project assistant integration catalog response is invalid")
	if input.AssistantScope != runtimecontract.AssistantScopeProject || request.GetAssistantRef() != input.AgentRef ||
		response.GetScopeKind() != "PROJECT" || response.GetProjectRef() != input.ProjectRef || response.GetAssistantProfileRef() != input.AssistantProfileRef ||
		response.GetCurrentConfiguration() != nil || len(response.GetEntries()) != 0 || len(response.GetProjectIntegrationGrants()) > maximumAssistantConfigurationEntries {
		return nil, invalid
	}
	entries := make([]map[string]any, 0, len(response.GetProjectIntegrationGrants()))
	seen := map[string]bool{}
	for _, entry := range response.GetProjectIntegrationGrants() {
		if entry == nil || !projectCatalogEntryMessageValid(entry) || !validAssistantResourceRef(entry.GetConnectionRef()) || entry.GetConnectionVersion() < 1 ||
			strings.TrimSpace(entry.GetConnectionName()) == "" || !utf8.ValidString(entry.GetConnectionName()) || utf8.RuneCountInString(entry.GetConnectionName()) > 160 ||
			entry.GetDefinitionVersion() == "" || len(entry.GetDefinitionVersion()) > 128 || !validAssistantCatalogDigest(entry.GetDefinitionDigest()) {
			return nil, invalid
		}
		candidate, err := projectIntegrationCandidateProjection(entry.GetCandidate())
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(entry.GetCandidate().GetCapability().GetKey(), "github.") && entry.GetCandidate().GetResourceScope() == nil {
			return nil, invalid
		}
		key := entry.GetConnectionRef() + "\x00" + entry.GetCandidate().GetCapability().GetKey()
		if seen[key] {
			return nil, invalid
		}
		seen[key] = true
		entries = append(entries, map[string]any{"connection_ref": entry.GetConnectionRef(), "connection_name": entry.GetConnectionName(), "connection_version": entry.GetConnectionVersion(),
			"definition_version": entry.GetDefinitionVersion(), "definition_digest": entry.GetDefinitionDigest(), "candidate": candidate})
	}
	return map[string]any{"kind": "PROJECT_INTEGRATION_GRANTS", "assistant_ref": response.GetAssistantRef(), "scope_kind": response.GetScopeKind(),
		"organization_ref": response.GetOrganizationRef(), "project_ref": response.GetProjectRef(), "assistant_profile_ref": response.GetAssistantProfileRef(),
		"project_integration_grants": entries, "next_offset": response.GetNextOffset()}, nil
}

func projectIntegrationCandidateProjection(candidate *controlplanev1.SystemAssistantIntegrationGrantCandidate) (map[string]any, error) {
	invalid := errors.New("project assistant integration catalog candidate is invalid")
	if candidate == nil || !projectCatalogCandidateMessageValid(candidate) {
		return nil, invalid
	}
	capability := candidate.GetCapability()
	if capability == nil || capability.GetKey() == "" || len(capability.GetKey()) > 160 || strings.TrimSpace(capability.GetName()) == "" ||
		utf8.RuneCountInString(capability.GetName()) > 160 || !utf8.ValidString(capability.GetDescription()) || utf8.RuneCountInString(capability.GetDescription()) > 2000 ||
		len(capability.GetInputSchema()) > maximumAssistantCurrentConfigurationBytes || !validAssistantCatalogDigest(capability.GetInputSchemaSha256()) {
		return nil, invalid
	}
	schemaDigest := sha256.Sum256([]byte(capability.GetInputSchema()))
	if hex.EncodeToString(schemaDigest[:]) != capability.GetInputSchemaSha256() {
		return nil, invalid
	}
	var schema map[string]any
	if json.Unmarshal([]byte(capability.GetInputSchema()), &schema) != nil || schema == nil {
		return nil, invalid
	}
	reason := strings.TrimPrefix(candidate.GetReason().String(), "INTEGRATION_CANDIDATE_REASON_")
	switch reason {
	case "READY", "CONNECTION_UNAVAILABLE", "RECIPIENT_UNAVAILABLE", "PACKAGE_UNAVAILABLE", "GRANT_UNAVAILABLE", "WORKFLOW_EXCLUDED":
	default:
		return nil, invalid
	}
	if candidate.GetGrantable() && reason != "READY" || candidate.GetCurrentGrantVersion() < 0 ||
		(candidate.GetCurrentGrantRef() != "") != (candidate.GetCurrentGrantVersion() > 0) || candidate.GetCurrentGrantEnabled() && candidate.GetCurrentGrantRef() == "" ||
		len(candidate.GetCurrentApprovalScopePaths()) > 16 {
		return nil, invalid
	}
	if candidate.GetCurrentGrantRef() != "" && !validAssistantResourceRef(candidate.GetCurrentGrantRef()) {
		return nil, invalid
	}
	policies := []string{}
	allowed := map[string]bool{}
	for _, value := range capability.GetAllowedApprovalPolicies() {
		policy, ok := projectCatalogApprovalPolicy(value)
		if !ok || allowed[policy] {
			return nil, invalid
		}
		allowed[policy] = true
		policies = append(policies, policy)
	}
	defaultPolicy, ok := projectCatalogApprovalPolicy(capability.GetApprovalPolicy())
	if !ok || !allowed[defaultPolicy] {
		return nil, invalid
	}
	currentPolicy := ""
	if candidate.GetCurrentGrantRef() != "" {
		currentPolicy, ok = projectCatalogApprovalPolicy(candidate.GetCurrentApprovalPolicy())
		if !ok {
			return nil, invalid
		}
	}
	if currentPolicy != "HUMAN_SCOPED" && len(candidate.GetCurrentApprovalScopePaths()) != 0 {
		return nil, invalid
	}
	seenPaths := map[string]bool{}
	for _, path := range candidate.GetCurrentApprovalScopePaths() {
		if path == "" || len(path) > 160 || seenPaths[path] {
			return nil, invalid
		}
		seenPaths[path] = true
	}
	risk := strings.TrimPrefix(capability.GetTypedRisk().String(), "INTEGRATION_RISK_")
	if risk != "READ" && risk != "WRITE" && risk != "SENSITIVE" && risk != "DESTRUCTIVE" {
		return nil, invalid
	}
	result := map[string]any{"capability": map[string]any{"key": capability.GetKey(), "name": capability.GetName(), "description": capability.GetDescription(),
		"risk": risk, "approval_policy": defaultPolicy, "allowed_approval_policies": policies, "input_schema": schema, "input_schema_sha256": capability.GetInputSchemaSha256()},
		"grantable": candidate.GetGrantable(), "reason": reason, "current_grant_ref": candidate.GetCurrentGrantRef(), "current_grant_version": candidate.GetCurrentGrantVersion(),
		"current_grant_enabled": candidate.GetCurrentGrantEnabled(), "current_approval_policy": currentPolicy, "current_approval_scope_paths": append([]string{}, candidate.GetCurrentApprovalScopePaths()...)}
	if scope := candidate.GetResourceScope(); scope != nil {
		projection, err := projectCatalogPublicGitHubScope(candidate)
		if err != nil {
			return nil, err
		}
		result["resource_scope"] = projection
	}
	return result, nil
}

var projectCatalogGitHubIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// Новый map разрешён только отдельной закрытой проверкой public GitHub scope.
// Общий current-read validator по-прежнему запрещает произвольные maps.
func projectCatalogEntryMessageValid(entry *controlplanev1.ProjectAssistantIntegrationGrantCatalogEntry) bool {
	copy := proto.Clone(entry).(*controlplanev1.ProjectAssistantIntegrationGrantCatalogEntry)
	if copy.Candidate != nil {
		copy.Candidate.ResourceScope = nil
	}
	return validAssistantCurrentReadMessage(copy.ProtoReflect(), 0)
}

func projectCatalogCandidateMessageValid(candidate *controlplanev1.SystemAssistantIntegrationGrantCandidate) bool {
	copy := proto.Clone(candidate).(*controlplanev1.SystemAssistantIntegrationGrantCandidate)
	copy.ResourceScope = nil
	return validAssistantCurrentReadMessage(copy.ProtoReflect(), 0)
}

func projectCatalogPublicGitHubScope(candidate *controlplanev1.SystemAssistantIntegrationGrantCandidate) (map[string]any, error) {
	invalid := errors.New("project assistant public resource scope is invalid")
	scope := candidate.GetResourceScope()
	if scope == nil || len(scope.ProtoReflect().GetUnknown()) != 0 || !strings.HasPrefix(candidate.GetCapability().GetKey(), "github.") ||
		candidate.GetCapability().GetResourceKind() != controlplanev1.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY ||
		scope.GetKind() != controlplanev1.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY || len(scope.GetValues()) != 2 || !validAssistantCatalogDigest(scope.GetDigest()) {
		return nil, invalid
	}
	values := map[string]string{}
	for _, key := range []string{"owner", "repository"} {
		value := scope.GetValues()[key]
		if !projectCatalogGitHubIdentifier.MatchString(value) || value == "." || value == ".." {
			return nil, invalid
		}
		values[key] = value
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, invalid
	}
	digest := sha256.Sum256(encoded)
	if hex.EncodeToString(digest[:]) != scope.GetDigest() {
		return nil, invalid
	}
	return map[string]any{"kind": "GITHUB_REPOSITORY", "values": values, "digest": scope.GetDigest()}, nil
}
func projectCatalogApprovalPolicy(policy controlplanev1.IntegrationApprovalPolicy) (string, bool) {
	value := strings.TrimPrefix(policy.String(), "INTEGRATION_APPROVAL_POLICY_")
	return value, value == "NONE" || value == "HUMAN_EACH_EFFECT" || value == "HUMAN_SCOPED"
}
