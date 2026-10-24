package httptransport

import (
	"slices"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// validWorkflowLaunchReadiness binds owner-provided launch readiness to the workflow version and
// published revision, validates the digest and reason/state relationship, and permits submit only
// for a published workflow with a revision. It does not itself authorize or launch work.
// validWorkflowLaunchReadiness связывает готовность запуска от владельца с версией workflow и
// опубликованной ревизией, проверяет digest и связь reason/state и допускает submit только
// опубликованному workflow с ревизией. Авторизация и запуск здесь не выполняются.
func validWorkflowLaunchReadiness(workflow *cp.Workflow) bool {
	r := workflow.GetLaunchReadiness()
	if r == nil || !validManagedVersion(r.WorkflowVersion) || r.WorkflowVersion != workflow.Version || !validManagedDigest(r.ContextDigest) ||
		!slices.Contains([]string{"READY", "PERMISSION_REQUIRED", "UNPUBLISHED", "DEPENDENCY_UNAVAILABLE"}, r.Reason) || !slices.Contains([]string{"READY", "BLOCKED", "UNKNOWN"}, r.OperationalState) ||
		(r.Reason == "READY") != r.AllowedToSubmit || !r.AllowedToSubmit && r.OperationalState != "BLOCKED" || r.RevisionRef != workflow.GetPublishedVersion().GetRef() {
		return false
	}
	return (r.RevisionRef == "" || fileTargetRef(r.RevisionRef)) && (!r.AllowedToSubmit || r.RevisionRef != "" && workflow.State == cp.WorkflowState_WORKFLOW_STATE_PUBLISHED)
}
