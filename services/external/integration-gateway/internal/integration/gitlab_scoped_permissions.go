package integration

import (
	"context"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// requireGitLabScopedCatalogWrite checks current token/project rights before the scoped catalog's personal mutation; it accepts only observable legacy api scopes for this broader operation set.
// requireGitLabScopedCatalogWrite проверяет current token/project rights перед personal mutation scoped catalog; для расширенного набора допускаются лишь observable legacy api scopes.
// Unsupported granular actions/ancestry fail closed without pretending that health, metadata or reusable human consent proves those rights.
// Unsupported granular actions/ancestry закрыто отклоняются; health, metadata либо reusable human consent не выдаются за подтверждение этих прав.
func (adapter *Adapter) requireGitLabScopedCatalogWrite(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, raw []byte) error {
	if !request.PersonalCredentialAdmission {
		return nil
	}
	if request.ResourcePermissionProfile != integrationpackage.CredentialResourcePermissionGitLabScopedV2 {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	metadata, err := adapter.readGitLabWriteMetadata(ctx, request, capability, configuration)
	if err != nil {
		return err
	}
	if !gitLabPermissionBool(metadata.token, "granular", false) || !gitLabTokenScopeAllowsWrite(metadata.token, metadata.projectID, "") ||
		!gitLabProjectMembershipAllowsWrite(metadata.project) || !gitLabPermissionBool(metadata.project, "archived", false) {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	for _, key := range []string{"marked_for_deletion_at", "marked_for_deletion_on"} {
		if value, present := metadata.project[key]; present && string(value) != "null" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	features := []string{"repository_access_level"}
	switch request.Operation {
	case "gitlab.issue.update", "gitlab.issue.note.create", "gitlab.issue.note.update", "gitlab.issue.note.delete":
		features = []string{"issues_access_level"}
	case "gitlab.merge_request.discussion.create", "gitlab.merge_request.update", "gitlab.merge_request.merge":
		features = append(features, "merge_requests_access_level")
	case "gitlab.pipeline.retry", "gitlab.pipeline.cancel", "gitlab.job.retry", "gitlab.job.cancel":
		features = append(features, "builds_access_level")
	case "gitlab.branch.delete", "gitlab.commit.create":
	default:
		return &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	for _, key := range features {
		value, valid := gitLabPermissionString(metadata.project, key)
		if !valid || value != "enabled" && value != "private" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
	}
	if request.Operation == "gitlab.branch.delete" || request.Operation == "gitlab.commit.create" {
		var input struct {
			Branch string `json:"branch"`
		}
		if json.Unmarshal(raw, &input) != nil || input.Branch == "" {
			return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		return adapter.requireGitLabUnprotectedBranch(ctx, request, capability, configuration, input.Branch)
	}
	return nil
}
