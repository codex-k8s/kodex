package httptransport

import (
	"slices"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// Проверка wire-инвариантов не заменяет owner eligibility и не фильтрует страницу.
func validResumableRunPage(response *cp.ListRunsResponse, projectRef, targetType, targetRef, cursor string, size *int) bool {
	items := response.GetRuns()
	next := response.GetPage().GetNextPageToken()
	if len(items) > 100 || size != nil && len(items) > *size || next != "" && (next == cursor || len(items) == 0) {
		return false
	}
	sessions, runs := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		if item == nil || !opaqueHTTPReference.MatchString(item.GetRef()) || !opaqueHTTPReference.MatchString(item.GetSessionRef()) ||
			!validManagedVersion(item.GetVersion()) || item.GetState() != cp.RunState_RUN_STATE_SUCCEEDED ||
			!slices.Contains(item.GetNextActions(), cp.NextAction_NEXT_ACTION_ADD_TURN) ||
			projectRef != "" && item.GetProjectRef() != projectRef || sessions[item.GetSessionRef()] || runs[item.GetRef()] {
			return false
		}
		agent, workflow := item.GetTarget().GetAgentRef(), item.GetTarget().GetWorkflowRef()
		if (agent == "") == (workflow == "") || agent != "" && !opaqueHTTPReference.MatchString(agent) || workflow != "" && !opaqueHTTPReference.MatchString(workflow) ||
			targetType == "AGENT" && agent != targetRef || targetType == "WORKFLOW" && workflow != targetRef {
			return false
		}
		sessions[item.GetSessionRef()], runs[item.GetRef()] = true, true
	}
	return true
}
