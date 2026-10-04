package httptransport

import cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// Привязка только проверяет авторитетную read-only проекцию CP. Она не
// подменяет transport actor, owner eligibility или полномочие команды.
func validRunIdentity(run *cp.Run) bool {
	if run == nil || run.Target == nil {
		return false
	}
	switch target := run.Target.Target.(type) {
	case *cp.RunTarget_AgentRef:
		return fileTargetRef(target.AgentRef) && fileTargetRef(run.ProjectRef) && run.AssistantPin == nil
	case *cp.RunTarget_WorkflowRef:
		return fileTargetRef(target.WorkflowRef) && fileTargetRef(run.ProjectRef) && run.AssistantPin == nil
	case *cp.RunTarget_SystemAssistantRef:
		pin := run.AssistantPin
		if run.Source != cp.RunSource_RUN_SOURCE_SYSTEM_ASSISTANT || pin == nil ||
			!fileTargetRef(target.SystemAssistantRef) || !fileTargetRef(pin.OrganizationRef) ||
			!fileTargetRef(pin.ConversationRef) || pin.AssistantRef != target.SystemAssistantRef ||
			pin.ProjectRef != run.ProjectRef {
			return false
		}
		switch pin.Scope {
		case cp.AssistantScope_ASSISTANT_SCOPE_SYSTEM:
			return pin.ProfileRef == "" && (pin.ProjectRef == "" || fileTargetRef(pin.ProjectRef))
		case cp.AssistantScope_ASSISTANT_SCOPE_PROJECT:
			return fileTargetRef(pin.ProjectRef) && fileTargetRef(pin.ProfileRef)
		default:
			return false
		}
	default:
		return false
	}
}
