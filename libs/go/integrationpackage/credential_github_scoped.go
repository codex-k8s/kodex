package integrationpackage

// CredentialResourcePermissionGitHubScopedV1 requires fresh identity, repository ownership coordinates and provider permissions at every call.
// CredentialResourcePermissionGitHubScopedV1 требует актуальной identity, координат репозитория и прав провайдера при каждом вызове.
const CredentialResourcePermissionGitHubScopedV1 = "GITHUB_SCOPED_RESOURCE_V1"

// gitHubScopedCapabilityRisk is the closed operation registry; unknown operations never inherit a personal credential authorization.
// gitHubScopedCapabilityRisk — закрытый реестр операций; неизвестная операция не наследует полномочий личного ключа.
func gitHubScopedCapabilityRisk(operation string) string {
	switch operation {
	case "github.repository.list", "github.repository.metadata.read", "github.issue.list", "github.issue.read", "github.repository.content.list", "github.repository.content.read", "github.branch.list", "github.branch.read", "github.commit.list", "github.commit.read", "github.pull_request.list", "github.pull_request.read", "github.pull_request.review.list", "github.pull_request.review.read", "github.issue.comment.list", "github.issue.comment.read", "github.check_run.list", "github.check_run.read", "github.actions.workflow.list", "github.actions.workflow.read", "github.actions.run.list", "github.actions.run.read", "github.actions.job.list", "github.actions.job.read", "github.pull_request.file.list", "github.pull_request.comment.list", "github.pull_request.comment.read":
		return "READ"
	case "github.issue.create", "github.issue.comment.create", "github.issue.update", "github.repository.bundle.publish", "github.repository.content.create", "github.repository.content.update", "github.branch.create", "github.pull_request.create", "github.pull_request.update", "github.pull_request.review.create", "github.issue.comment.update", "github.pull_request.comment.create", "github.pull_request.comment.update", "github.pull_request.ready":
		return "WRITE"
	case "github.repository.content.delete", "github.branch.delete", "github.pull_request.merge", "github.issue.comment.delete", "github.actions.run.cancel":
		return "DESTRUCTIVE"
	case "github.actions.workflow.dispatch", "github.actions.run.rerun":
		return "SENSITIVE"
	default:
		return ""
	}
}
