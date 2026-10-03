package runtimecontract

import "errors"

// AssistantScope назначается владельцем immutable runtime revision, а не
// контекстом открытого экрана. SYSTEM может работать в контексте проекта,
// не превращая его ресурсы в источники организационных credentials.
type AssistantScope string

const (
	AssistantScopeNone    AssistantScope = "NONE"
	AssistantScopeSystem  AssistantScope = "SYSTEM"
	AssistantScopeProject AssistantScope = "PROJECT"
)

func (input RunnerInput) IsAssistant() bool {
	return input.AssistantScope == AssistantScopeSystem || input.AssistantScope == AssistantScopeProject
}

func (input RunnerInput) IsSystemAssistant() bool {
	return input.AssistantScope == AssistantScopeSystem
}

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
