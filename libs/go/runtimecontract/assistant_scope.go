package runtimecontract

import "errors"

// AssistantScope is assigned by the immutable owner revision, not the currently opened screen;
// a system assistant may work in a project context without inheriting its credentials.
// AssistantScope назначается неизменной версией владельца, а не текущим экраном;
// системный помощник может работать в контексте проекта, не наследуя его учётные данные.
type AssistantScope string

const (
	// AssistantScopeNone denotes an ordinary non-assistant agent run.
	// AssistantScopeNone обозначает запуск обычного агента, не помощника.
	AssistantScopeNone AssistantScope = "NONE"
	// AssistantScopeSystem denotes the organization-level system assistant.
	// AssistantScopeSystem обозначает общесистемного помощника организации.
	AssistantScopeSystem AssistantScope = "SYSTEM"
	// AssistantScopeProject denotes an assistant bound to one project profile.
	// AssistantScopeProject обозначает помощника, привязанного к профилю проекта.
	AssistantScopeProject AssistantScope = "PROJECT"
)

// IsAssistant distinguishes assistant revisions from ordinary agent revisions.
// IsAssistant отличает версии помощника от версий обычного агента.
func (input RunnerInput) IsAssistant() bool {
	return input.AssistantScope == AssistantScopeSystem || input.AssistantScope == AssistantScopeProject
}

// IsSystemAssistant identifies the organization-level assistant even within project context.
// IsSystemAssistant определяет общесистемного помощника даже в контексте проекта.
func (input RunnerInput) IsSystemAssistant() bool {
	return input.AssistantScope == AssistantScopeSystem
}

// validateAssistantScope rejects inconsistent project and profile references for each scope.
// validateAssistantScope отклоняет несовместимые ссылки проекта и профиля для каждой области.
func (input RunnerInput) validateAssistantScope() error {
	switch input.AssistantScope {
	case AssistantScopeNone:
		if !opaqueReferencePattern.MatchString(input.ProjectRef) || input.AssistantProfileRef != "" {
			return errors.New("ordinary runtime scope is invalid")
		}
	case AssistantScopeSystem:
		if input.AssistantProfileRef != "" || input.ProjectRef != "" && !opaqueReferencePattern.MatchString(input.ProjectRef) {
			return errors.New("system assistant scope is invalid")
		}
	case AssistantScopeProject:
		if !opaqueReferencePattern.MatchString(input.ProjectRef) || !opaqueReferencePattern.MatchString(input.AssistantProfileRef) {
			return errors.New("project assistant scope is invalid")
		}
	default:
		return errors.New("assistant scope is unknown")
	}
	return nil
}
