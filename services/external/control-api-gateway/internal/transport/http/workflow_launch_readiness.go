package httptransport

import (
	"slices"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func validWorkflowLaunchReadiness(workflow *cp.Workflow) bool {
	r := workflow.GetLaunchReadiness()
	if r == nil || !validManagedVersion(r.WorkflowVersion) || r.WorkflowVersion != workflow.Version || !validManagedDigest(r.ContextDigest) ||
		!slices.Contains([]string{"READY", "PERMISSION_REQUIRED", "UNPUBLISHED", "DEPENDENCY_UNAVAILABLE"}, r.Reason) || !slices.Contains([]string{"READY", "BLOCKED", "UNKNOWN"}, r.OperationalState) ||
		(r.Reason == "READY") != r.AllowedToSubmit || !r.AllowedToSubmit && r.OperationalState != "BLOCKED" || r.RevisionRef != workflow.GetPublishedVersion().GetRef() {
		return false
	}
	return (r.RevisionRef == "" || fileTargetRef(r.RevisionRef)) && (!r.AllowedToSubmit || r.RevisionRef != "" && workflow.State == cp.WorkflowState_WORKFLOW_STATE_PUBLISHED)
}
