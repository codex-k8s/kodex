package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// observeGitLabPersonalWriteScope performs only requested fixed metadata GETs after health.
// Known refusals restrict launch; unknown shapes/versions/transports leave writes unconfirmed.
// A401 invalidates the batch. No provider identities, scopes or raw payload leave this process.
// observeGitLabPersonalWriteScope выполняет только запрошенные fixed metadata GET после health.
// Известные отказы ограничивают launch; unknown shapes/versions/transports оставляют write unconfirmed.
// 401 отменяет batch. Provider identities, scopes и raw payload остаются в процессе.
func (adapter *Adapter) observeGitLabPersonalWriteScope(ctx context.Context, request Request, definition integrationpackage.Package, configuration map[string]string, result *CredentialProbeResult) error {
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || definition.Metadata.Key != "gitlab" {
		return &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	metadata, err := adapter.readGitLabWriteMetadata(ctx, request, health, configuration)
	if err != nil {
		var safe *SafeError
		if errors.As(err, &safe) && safe.Code == "INTEGRATION_AUTH_REJECTED" && safe.AuthenticationHTTPStatus != http.StatusForbidden {
			return err
		}
		return nil
	}
	denied := []string{}
	for _, key := range request.PersonalDeniedWriteCapabilityKeys {
		permission := map[string]string{"gitlab.issue.create": "create_issue", "gitlab.branch.create": "create_branch", "gitlab.merge_request.create": "create_merge_request"}[key]
		allowed, known := gitLabObservedWriteEligibility(metadata, permission)
		if !known {
			return nil
		}
		if !allowed {
			denied = append(denied, key)
		}
	}
	result.ObservedPermissionProfiles = []string{integrationpackage.CredentialObservationGitLabProjectWriteScopeV1}
	result.DeniedCapabilityKeys = append(result.DeniedCapabilityKeys, denied...)
	result.ExpiresAt = metadata.expiresAt
	return nil
}

// gitLabObservedWriteEligibility separates explicit denial from unsupported metadata. Positive
// metadata is conditional: actual inputs and external rights are checked again before one POST.
// gitLabObservedWriteEligibility отличает явный отказ от unsupported metadata. Положительные
// metadata условны: фактические inputs и внешние права перепроверяются перед одним POST.
func gitLabObservedWriteEligibility(metadata gitLabWriteMetadata, permission string) (allowed, known bool) {
	if permission == "" || !gitLabObservedTokenScopeKnown(metadata.token) {
		return false, false
	}
	if !gitLabTokenScopeAllowsWrite(metadata.token, metadata.projectID, permission) {
		return false, true
	}
	if gitLabPermissionBool(metadata.project, "archived", true) {
		return false, true
	}
	for _, key := range []string{"marked_for_deletion_at", "marked_for_deletion_on"} {
		if raw, exists := metadata.project[key]; exists && string(bytes.TrimSpace(raw)) != "null" {
			if _, valid := gitLabPermissionString(metadata.project, key); valid {
				return false, true
			}
			return false, false
		}
	}
	var memberships map[string]json.RawMessage
	if json.Unmarshal(metadata.project["permissions"], &memberships) != nil || memberships == nil {
		return false, false
	}
	observedMember := false
	for _, key := range []string{"project_access", "group_access"} {
		raw, exists := memberships[key]
		if !exists {
			return false, false
		}
		if string(bytes.TrimSpace(raw)) == "null" {
			continue
		}
		var member map[string]json.RawMessage
		if json.Unmarshal(raw, &member) != nil {
			return false, false
		}
		role, valid := gitLabPermissionID(member, "access_level")
		if !valid {
			return false, false
		}
		switch role {
		case 5, 10, 15, 20, 25, 30, 40, 50:
		default:
			return false, false
		}
		observedMember = true
	}
	if !observedMember {
		return false, false
	}
	if !gitLabProjectMembershipAllowsWrite(metadata.project) {
		return false, true
	}
	features := []string{"repository_access_level"}
	if permission == "create_issue" {
		features = []string{"issues_access_level"}
	}
	if permission == "create_merge_request" {
		features = append(features, "merge_requests_access_level")
	}
	for _, key := range features {
		value, valid := gitLabPermissionString(metadata.project, key)
		if !valid {
			return false, false
		}
		switch value {
		case "disabled":
			return false, true
		case "enabled", "private":
		default:
			return false, false
		}
	}
	return true, true
}

// gitLabObservedTokenScopeKnown admits only fully understood legacy or exact project granular
// shapes. Unsupported group/personal ancestry, missing fields and malformed lists stay unknown.
// gitLabObservedTokenScopeKnown допускает только полностью понятные legacy либо exact project
// granular shapes. Unsupported group/personal ancestry, missing fields и неверные списки остаются unknown.
func gitLabObservedTokenScopeKnown(token map[string]json.RawMessage) bool {
	if gitLabPermissionBool(token, "granular", false) {
		var scopes []string
		if json.Unmarshal(token["scopes"], &scopes) != nil || len(scopes) == 0 || len(scopes) > 64 {
			return false
		}
		seen := map[string]bool{}
		for _, value := range scopes {
			if value == "" || len(value) > 128 || seen[value] {
				return false
			}
			switch value {
			case "api", "read_api", "read_repository", "write_repository", "read_registry", "write_registry",
				"self_rotate", "read_virtual_registry", "write_virtual_registry", "create_runner", "manage_runner",
				"ai_features", "k8s_proxy", "admin_mode", "read_service_ping", "sudo", "read_user":
			default:
				return false
			}
			seen[value] = true
		}
		if raw, exists := token["granular_scopes"]; exists {
			var scopes []json.RawMessage
			if json.Unmarshal(raw, &scopes) != nil || len(scopes) != 0 {
				return false
			}
		}
		return true
	}
	if !gitLabPermissionBool(token, "granular", true) {
		return false
	}
	var scopes []map[string]json.RawMessage
	if json.Unmarshal(token["granular_scopes"], &scopes) != nil || scopes == nil || len(scopes) > 128 {
		return false
	}
	for _, scope := range scopes {
		access, valid := gitLabPermissionString(scope, "access")
		if !valid || (access != "selected_memberships" && access != "all_memberships") || string(bytes.TrimSpace(scope["group_id"])) != "null" {
			return false
		}
		_, selected := gitLabPermissionID(scope, "project_id")
		if access == "selected_memberships" && !selected || access == "all_memberships" && string(bytes.TrimSpace(scope["project_id"])) != "null" {
			return false
		}
		var permissions []string
		if json.Unmarshal(scope["permissions"], &permissions) != nil || len(permissions) == 0 || len(permissions) > 128 {
			return false
		}
		seen := map[string]bool{}
		for _, value := range permissions {
			if value == "" || len(value) > 128 || seen[value] {
				return false
			}
			seen[value] = true
		}
	}
	return true
}
