package integrationpackage

// CredentialResourcePermissionGitLabScopedV2 requires a fresh canonical project boundary and provider permission check at call time; scope health alone is never a WRITE proof.
// CredentialResourcePermissionGitLabScopedV2 требует fresh canonical project boundary и provider permission check при вызове; scope health не является WRITE proof.
const CredentialResourcePermissionGitLabScopedV2 = "GITLAB_SCOPED_RESOURCE_V2"

// CredentialResourcePermissionContractForProfile resolves a registered scoped successor or the unchanged legacy contract; an unknown profile never removes a required check.
// CredentialResourcePermissionContractForProfile разрешает registered scoped successor либо прежний legacy contract; неизвестный profile не снимает обязательную проверку.
func CredentialResourcePermissionContractForProfile(definitionKey, capabilityKey, profile string) (CredentialResourcePermissionContract, bool) {
	if definitionKey == "github" && profile == CredentialResourcePermissionGitHubScopedV1 {
		risk := gitHubScopedCapabilityRisk(capabilityKey)
		if risk == "" {
			return CredentialResourcePermissionContract{}, false
		}
		policy := "HUMAN_EACH_EFFECT"
		if risk == "READ" {
			policy = "NONE"
		}
		if risk == "WRITE" && capabilityKey != "github.repository.bundle.publish" {
			policy = "HUMAN_SCOPED"
		}
		return CredentialResourcePermissionContract{Profile: profile, Operation: capabilityKey, ResourceKind: "GITHUB_SCOPE", Risk: risk, ApprovalPolicy: policy, ReadCapabilities: []string{"github.identity.read"}}, true
	}

	if definitionKey == "gitlab" && profile == CredentialResourcePermissionGitLabScopedV2 {
		risk := gitLabScopedCapabilityRisk(capabilityKey)
		if risk == "" {
			return CredentialResourcePermissionContract{}, false
		}
		policy := "HUMAN_EACH_EFFECT"
		if risk == "READ" {
			policy = "NONE"
		}
		return CredentialResourcePermissionContract{Profile: profile, Operation: capabilityKey, ResourceKind: "GITLAB_SCOPE", Risk: risk,
			ApprovalPolicy: policy, ReadCapabilities: []string{"gitlab.scope.health.read"}}, true
	}
	return CredentialResourcePermissionContractFor(definitionKey, capabilityKey)
}

// gitLabScopedCapabilityRisk is a closed operation registry for the versioned typed GitLab consumer; unknown operations receive no personal credential authority.
// gitLabScopedCapabilityRisk — закрытый operation registry версионированного typed GitLab consumer; неизвестным operations не выдаются personal credential authority.
func gitLabScopedCapabilityRisk(operation string) string {
	switch operation {
	case "gitlab.project.list", "gitlab.repository.search", "gitlab.project.metadata.read", "gitlab.repository.file.read", "gitlab.issue.read", "gitlab.issue.list",
		"gitlab.merge_request.read", "gitlab.pipeline.read", "gitlab.branch.list", "gitlab.branch.read", "gitlab.commit.list", "gitlab.commit.read", "gitlab.commit.diff",
		"gitlab.repository.tree.list", "gitlab.issue.note.list", "gitlab.issue.note.read", "gitlab.merge_request.list", "gitlab.merge_request.discussion.list",
		"gitlab.pipeline.list", "gitlab.job.list", "gitlab.job.read", "gitlab.job.trace.read", "gitlab.merge_request.diff.list":
		return "READ"
	case "gitlab.issue.create", "gitlab.issue.update", "gitlab.merge_request.discussion.create", "gitlab.issue.note.create", "gitlab.issue.note.update", "gitlab.merge_request.update":
		return "WRITE"
	case "gitlab.branch.create", "gitlab.commit.create", "gitlab.merge_request.create", "gitlab.pipeline.retry", "gitlab.job.retry":
		return "SENSITIVE"
	case "gitlab.branch.delete", "gitlab.issue.note.delete", "gitlab.merge_request.merge", "gitlab.pipeline.cancel", "gitlab.job.cancel":
		return "DESTRUCTIVE"
	default:
		return ""
	}
}
