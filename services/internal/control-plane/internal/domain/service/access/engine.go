// Package access реализует закрытый allow-only registry и вычисление
// application RBAC без доверия к transport locator.
package access

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const maximumBindingWindow = 366 * 24 * time.Hour

var definitions = []entity.PermissionDefinition{
	permission("organization.view", "READ", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("organization.manage", "ADMIN", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("access.view", "READ", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("access.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT"}, false),
	permission("project.create", "WRITE", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("project.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROJECT"}, false),
	permission("project.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT"}, []string{"PROJECT"}, false),
	permission("agent.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("agent.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("agent.launch", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("workflow.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("workflow.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("workflow.launch", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("run.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, false),
	permission("run.cancel.own", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, true),
	permission("run.cancel.any", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, false),
	permission("gate.resolve", "APPROVE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"OWNER_GATE"}, false),
	permission("artifact.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.download", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.manage", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("schedule.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"SCHEDULE"}, false),
	permission("schedule.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"SCHEDULE"}, false),
	permission("integration.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"INTEGRATION"}, false),
	permission("integration.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"INTEGRATION"}, false),
	permission("audit.view", "READ", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT"}, false),
}

func permission(key, risk string, scopes, resources []string, ownerCondition bool) entity.PermissionDefinition {
	upper := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	return entity.PermissionDefinition{
		Key: key, NameKey: "i18n:" + "PERMISSION_" + upper + "_NAME",
		DescriptionKey: "i18n:" + "PERMISSION_" + upper + "_DESCRIPTION", Risk: risk,
		AllowedScopes: scopes, ResourceKinds: resources, OwnerConditionSupported: ownerCondition,
	}
}

func Definitions() []entity.PermissionDefinition {
	result := make([]entity.PermissionDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = definition
		result[index].AllowedScopes = append([]string(nil), definition.AllowedScopes...)
		result[index].ResourceKinds = append([]string(nil), definition.ResourceKinds...)
	}
	return result
}

func Permission(key string) (entity.PermissionDefinition, bool) {
	for _, definition := range definitions {
		if definition.Key == key {
			return definition, true
		}
	}
	return entity.PermissionDefinition{}, false
}

func ValidateRole(input command.AccessRoleInput) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Name) != input.Name || len([]rune(input.Name)) > 160 ||
		len(input.Description) > 2000 || len(input.ChangeComment) > 500 ||
		len(input.PermissionKeys) == 0 || len(input.PermissionKeys) > len(definitions) ||
		len(input.AllowedScopes) == 0 || len(input.AllowedScopes) > 4 {
		return errors.New("access role is invalid")
	}
	if !uniqueKnown(input.PermissionKeys, func(value string) bool { _, ok := Permission(value); return ok }) ||
		!uniqueKnown(input.AllowedScopes, knownScope) {
		return errors.New("access role contains an unknown value")
	}
	for _, permissionKey := range input.PermissionKeys {
		definition, _ := Permission(permissionKey)
		for _, scope := range input.AllowedScopes {
			if !contains(definition.AllowedScopes, scope) {
				return errors.New("access role scope is incompatible with a permission")
			}
		}
	}
	return nil
}

func ValidateBinding(input command.AccessBindingInput, role entity.AccessRoleVersion, now time.Time) error {
	if !knownSubjectKind(input.SubjectKind) || strings.TrimSpace(input.SubjectRef) == "" || strings.TrimSpace(input.RoleVersionRef) == "" ||
		!contains(role.AllowedScopes, input.Scope.Kind) || ValidateScope(input.Scope) != nil || ValidateConditions(input.Conditions, now) != nil {
		return errors.New("access binding is invalid")
	}
	if input.Conditions.RequireOwner {
		supported := false
		for _, permissionKey := range role.PermissionKeys {
			definition, _ := Permission(permissionKey)
			supported = supported || definition.OwnerConditionSupported
		}
		if !supported {
			return errors.New("access binding owner condition is unsupported")
		}
	}
	return nil
}

func ValidateScope(scope entity.AccessScope) error {
	if !knownScope(scope.Kind) {
		return errors.New("access scope kind is invalid")
	}
	switch scope.Kind {
	case "ORGANIZATION":
		if scope.ProjectRef != "" || scope.ResourceKind != "" && scope.ResourceKind != "ORGANIZATION" {
			return errors.New("organization scope contains a narrower locator")
		}
	case "PROJECT":
		if scope.ProjectRef == "" || scope.ResourceKind != "" || scope.ResourceRef != "" {
			return errors.New("project scope is incomplete")
		}
	case "RESOURCE_KIND":
		if scope.ResourceKind == "" || scope.ResourceKind == "ORGANIZATION" || scope.ResourceRef != "" || !knownResourceKind(scope.ResourceKind) {
			return errors.New("resource-kind scope is incomplete")
		}
	case "RESOURCE_INSTANCE":
		organizationScoped := scope.ResourceKind == "INTEGRATION" || scope.ResourceKind == "ARTIFACT" && scope.ProjectRef == ""
		if scope.ResourceKind == "" || scope.ResourceKind == "ORGANIZATION" || scope.ResourceRef == "" ||
			!knownResourceKind(scope.ResourceKind) || organizationScoped && scope.ProjectRef != "" ||
			!organizationScoped && scope.ProjectRef == "" {
			return errors.New("resource-instance scope is incomplete")
		}
	}
	return nil
}

func ValidateConditions(conditions entity.AccessConditions, now time.Time) error {
	if conditions.ValidFrom != nil && conditions.ValidFrom.IsZero() || conditions.ValidUntil != nil && conditions.ValidUntil.IsZero() {
		return errors.New("access condition timestamp is invalid")
	}
	if conditions.ValidFrom != nil && conditions.ValidUntil != nil {
		if !conditions.ValidUntil.After(*conditions.ValidFrom) || conditions.ValidUntil.Sub(*conditions.ValidFrom) > maximumBindingWindow {
			return errors.New("access condition window is invalid")
		}
	}
	if conditions.ValidUntil != nil && !conditions.ValidUntil.After(now.Add(-time.Minute)) {
		return errors.New("access condition window is already expired")
	}
	return nil
}

func Evaluate(subject entity.AccessSubject, permissionKey string, target entity.AccessScope, ownerSubjectRef string, bindings []entity.AccessBinding, at time.Time) entity.EffectiveAccessDecision {
	decision := entity.EffectiveAccessDecision{PermissionKey: permissionKey, Target: target}
	definition, known := Permission(permissionKey)
	if !known || ValidateScope(target) != nil || !contains(definition.ResourceKinds, target.ResourceKind) {
		decision.Explanation = []entity.AccessExplanationStep{{Code: "NO_ALLOW_BINDING", SourceKind: subject.Kind}}
		return decision
	}
	for _, binding := range bindings {
		if binding.State != "ACTIVE" || !bindingTargetsSubject(binding.Subject, subject) ||
			!contains(binding.RoleVersion.PermissionKeys, permissionKey) || !scopeMatches(binding.Scope, target) ||
			!conditionsMatch(binding.Conditions, permissionKey, subject.Ref, ownerSubjectRef, at) {
			continue
		}
		decision.Allowed = true
		code := "DIRECT_BINDING"
		if binding.Subject.Kind == "OIDC_GROUP" {
			code = "OIDC_GROUP_BINDING"
		} else if binding.Subject.Kind == "SERVICE" {
			code = "SERVICE_BINDING"
		}
		decision.Explanation = append(decision.Explanation,
			entity.AccessExplanationStep{Code: code, BindingRef: binding.Ref, RoleRef: binding.RoleVersion.RoleRef, RoleVersionRef: binding.RoleVersion.Ref, SourceKind: binding.Subject.Kind, SourceRef: binding.Subject.Ref, Scope: binding.Scope},
			entity.AccessExplanationStep{Code: "ROLE_PERMISSION", BindingRef: binding.Ref, RoleRef: binding.RoleVersion.RoleRef, RoleVersionRef: binding.RoleVersion.Ref, SourceKind: binding.Subject.Kind, SourceRef: binding.Subject.Ref, Scope: binding.Scope},
			entity.AccessExplanationStep{Code: "SCOPE_MATCH", BindingRef: binding.Ref, RoleRef: binding.RoleVersion.RoleRef, RoleVersionRef: binding.RoleVersion.Ref, SourceKind: binding.Subject.Kind, SourceRef: binding.Subject.Ref, Scope: binding.Scope},
			entity.AccessExplanationStep{Code: "CONDITION_MATCH", BindingRef: binding.Ref, RoleRef: binding.RoleVersion.RoleRef, RoleVersionRef: binding.RoleVersion.Ref, SourceKind: binding.Subject.Kind, SourceRef: binding.Subject.Ref, Scope: binding.Scope},
		)
	}
	if !decision.Allowed {
		decision.Explanation = []entity.AccessExplanationStep{{Code: "NO_ALLOW_BINDING", SourceKind: subject.Kind}}
	}
	sort.SliceStable(decision.Explanation, func(i, j int) bool {
		return decision.Explanation[i].BindingRef < decision.Explanation[j].BindingRef
	})
	return decision
}

func bindingTargetsSubject(binding, subject entity.AccessSubject) bool {
	if binding.Kind == "OIDC_GROUP" {
		return binding.Ref == subject.Ref || contains(subject.OIDCGroupRefs, binding.Ref)
	}
	return binding.Kind == subject.Kind && binding.Ref == subject.Ref
}

func scopeMatches(binding, target entity.AccessScope) bool {
	switch binding.Kind {
	case "ORGANIZATION":
		return true
	case "PROJECT":
		return binding.ProjectRef != "" && binding.ProjectRef == target.ProjectRef
	case "RESOURCE_KIND":
		return (binding.ProjectRef == "" || binding.ProjectRef == target.ProjectRef) && binding.ResourceKind == target.ResourceKind
	case "RESOURCE_INSTANCE":
		if binding.ProjectRef != target.ProjectRef {
			return false
		}
		if binding.ResourceKind == target.ResourceKind && binding.ResourceRef == target.ResourceRef {
			return true
		}
		return target.RelatedResourceRefs[binding.ResourceKind] == binding.ResourceRef
	default:
		return false
	}
}

func conditionsMatch(conditions entity.AccessConditions, permissionKey, subjectRef, ownerSubjectRef string, at time.Time) bool {
	if conditions.ValidFrom != nil && at.Before(*conditions.ValidFrom) || conditions.ValidUntil != nil && !at.Before(*conditions.ValidUntil) {
		return false
	}
	requireOwner := conditions.RequireOwner || permissionKey == "run.cancel.own"
	return !requireOwner || ownerSubjectRef != "" && ownerSubjectRef == subjectRef
}

func uniqueKnown(values []string, known func(string) bool) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !known(value) {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func knownSubjectKind(value string) bool {
	return value == "USER" || value == "OIDC_GROUP" || value == "SERVICE"
}
func knownScope(value string) bool {
	return value == "ORGANIZATION" || value == "PROJECT" || value == "RESOURCE_KIND" || value == "RESOURCE_INSTANCE"
}
func knownResourceKind(value string) bool {
	return value == "ORGANIZATION" || value == "PROJECT" || value == "AGENT" || value == "WORKFLOW" || value == "RUN" ||
		value == "OWNER_GATE" || value == "ARTIFACT" || value == "SCHEDULE" || value == "INTEGRATION"
}
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
