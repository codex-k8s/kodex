package httptransport

import cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// validRunIdentity checks CP run target and assistant-pin consistency: ordinary agent/workflow
// runs need a project and no assistant pin; assistant runs require matching conversation,
// organization, scope and profile coordinates. Shape checks never replace transport identity or
// owner authorization.
// validRunIdentity проверяет согласованность цели запуска CP и assistant pin: обычному
// агенту/workflow нужен проект без assistant pin; помощнику нужны согласованные conversation,
// organization, scope и profile. Проверка формы не заменяет transport identity и авторизацию
// владельца.
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
