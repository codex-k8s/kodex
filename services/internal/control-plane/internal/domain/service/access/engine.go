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

// maximumBindingWindow caps the interval only when both binding timestamps exist;
// it does not impose a lifetime on an open-ended binding.
// maximumBindingWindow ограничивает интервал только при наличии обеих временных
// границ привязки; бессрочной привязке срок жизни не назначается.
const maximumBindingWindow = 366 * 24 * time.Hour

// definitions is the closed application permission catalog, including compatible
// scopes, resource kinds and explicit support for ownership conditions. Entries
// describe available permissions; they do not grant any caller access.
// definitions задаёт закрытый каталог прикладных разрешений с допустимыми
// областями, видами ресурсов и поддержкой проверки владельца. Записи описывают
// разрешения, но сами по себе не предоставляют доступ вызывающему.
var definitions = []entity.PermissionDefinition{
	permission("organization.view", "READ", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("platform.stt.use", "WRITE", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("organization.manage", "ADMIN", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("access.view", "READ", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("access.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT"}, false),
	permission("project.create", "WRITE", []string{"ORGANIZATION"}, []string{"ORGANIZATION"}, false),
	permission("project.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROJECT", "ROLE_IMAGE"}, false),
	permission("project.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT"}, []string{"PROJECT"}, false),
	permission("agent.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("agent.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("agent.launch", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("workflow.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("workflow.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("workflow.launch", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"WORKFLOW"}, false),
	permission("run.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, false),
	permission("run.cancel.own", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, true),
	permission("run.cancel", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUN"}, false),
	permission("session.cancel", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"SESSION"}, false),
	permission("prompt.full.view", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "AGENT", "WORKFLOW", "RUN", "SESSION", "SCHEDULE"}, false),
	permission("gate.resolve", "APPROVE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"OWNER_GATE"}, false),
	permission("artifact.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.download", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.upload", "WRITE", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT", "ARTIFACT"}, false),
	permission("artifact.revision.create", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.bind", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.delete", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.restore", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("artifact.purge", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ARTIFACT"}, false),
	permission("schedule.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"SCHEDULE"}, false),
	permission("schedule.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"SCHEDULE"}, false),
	permission("agent.avatar.manage", "WRITE", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"AGENT"}, false),
	permission("integration.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"INTEGRATION"}, false),
	permission("integration.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"INTEGRATION"}, false),
	permission("image.build", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "ROLE_IMAGE"}, false),
	permission("image.source.view", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "ROLE_IMAGE"}, false),
	permission("image.source.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "ROLE_IMAGE"}, false),
	permission("image.promote", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ROLE_IMAGE"}, false),
	permission("environment.privileged.manage", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "RUNTIME_ENVIRONMENT"}, false),
	permission("runtime.environment.disable", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUNTIME_ENVIRONMENT"}, false),
	permission("runtime.environment.delete", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"RUNTIME_ENVIRONMENT"}, false),
	permission("provider.account.view", "READ", []string{"ORGANIZATION", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROVIDER_ACCOUNT"}, false),
	permission("provider.account.manage", "ADMIN", []string{"ORGANIZATION", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROVIDER_ACCOUNT"}, false),
	permission("provider.account.authorize", "ADMIN", []string{"ORGANIZATION", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROVIDER_ACCOUNT"}, false),
	permission("provider.account.revoke", "ADMIN", []string{"ORGANIZATION", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"PROVIDER_ACCOUNT"}, false),
	permission("secret.view", "READ", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "PROJECT", "SECRET"}, false),
	permission("secret.create", "WRITE", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT"}, false),
	permission("secret.rotate", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "SECRET"}, false),
	permission("secret.revoke", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "SECRET"}, false),
	permission("secret.reveal", "ADMIN", []string{"ORGANIZATION", "PROJECT", "RESOURCE_KIND", "RESOURCE_INSTANCE"}, []string{"ORGANIZATION", "SECRET"}, false),
	permission("audit.view", "READ", []string{"ORGANIZATION", "PROJECT"}, []string{"ORGANIZATION", "PROJECT"}, false),
}

// permission builds catalog metadata and stable localization keys from a known
// permission key. Callers supply the allowed scopes and resource kinds explicitly.
// permission собирает метаданные каталога и устойчивые ключи перевода из известного
// ключа разрешения. Допустимые области и виды ресурсов явно передаёт вызывающий.
func permission(key, risk string, scopes, resources []string, ownerCondition bool) entity.PermissionDefinition {
	upper := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	return entity.PermissionDefinition{
		Key: key, NameKey: "i18n:" + "PERMISSION_" + upper + "_NAME",
		DescriptionKey: "i18n:" + "PERMISSION_" + upper + "_DESCRIPTION", Risk: risk,
		AllowedScopes: scopes, ResourceKinds: resources, OwnerConditionSupported: ownerCondition,
	}
}

// Definitions returns the catalog in declaration order with independent outer,
// scope and resource slices, so metadata consumers cannot mutate the registry.
// Definitions возвращает каталог в порядке объявления с независимыми внешним
// срезом и срезами областей и ресурсов, защищая реестр от изменений потребителем.
func Definitions() []entity.PermissionDefinition {
	result := make([]entity.PermissionDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = definition
		result[index].AllowedScopes = append([]string(nil), definition.AllowedScopes...)
		result[index].ResourceKinds = append([]string(nil), definition.ResourceKinds...)
	}
	return result
}

// Permission looks up an exact registered key without aliases or normalization.
// Its slice fields reference registry data and must be treated as read-only;
// unknown keys return a zero definition and false.
// Permission ищет точный зарегистрированный ключ без псевдонимов и нормализации.
// Срезы результата ссылаются на реестр и предназначены только для чтения;
// неизвестный ключ возвращает нулевое определение и false.
func Permission(key string) (entity.PermissionDefinition, bool) {
	for _, definition := range definitions {
		if definition.Key == key {
			return definition, true
		}
	}
	return entity.PermissionDefinition{}, false
}

// ValidateRole checks role text bounds, nonempty unique known permission/scope
// lists and compatibility of every permission with every requested scope. It
// validates a role definition, without loading its owner or assigning authority.
// ValidateRole проверяет ограничения текстов роли, непустые уникальные списки
// известных разрешений и областей и совместимость каждой пары. Функция проверяет
// определение роли, не загружая её владельца и не назначая полномочий.
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

// ValidateBinding checks a binding against the supplied role version, structural
// scope rules and time conditions. RequireOwner needs at least one supporting
// permission; subject existence, role ownership and authorization remain external.
// ValidateBinding проверяет привязку по переданной версии роли, структуре области
// и временным условиям. RequireOwner требует хотя бы одного поддерживающего
// разрешения; существование субъекта, владение ролью и авторизация проверяются снаружи.
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

// ValidateScope accepts only known scope kinds and compatible locator shapes,
// including the distinction between project and organization resource instances.
// It does not resolve resource ownership or verify that a locator exists.
// ValidateScope допускает только известные виды областей и совместимые формы
// адресации, различая проектные и организационные экземпляры ресурсов.
// Функция не разрешает владение ресурсом и не проверяет существование адресата.
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
		organizationScoped := scope.ResourceKind == "INTEGRATION" || scope.ResourceKind == "PROVIDER_ACCOUNT" ||
			(scope.ResourceKind == "ROLE_IMAGE" || scope.ResourceKind == "SECRET" || scope.ResourceKind == "RUNTIME_ENVIRONMENT") && scope.ProjectRef == "" ||
			(scope.ResourceKind == "ARTIFACT" || scope.ResourceKind == "RUN" || scope.ResourceKind == "OWNER_GATE") && scope.ProjectRef == ""
		if scope.ResourceKind == "" || scope.ResourceKind == "ORGANIZATION" || scope.ResourceRef == "" ||
			!knownResourceKind(scope.ResourceKind) || organizationScoped && scope.ProjectRef != "" ||
			!organizationScoped && scope.ProjectRef == "" {
			return errors.New("resource-instance scope is incomplete")
		}
	}
	return nil
}

// ValidateConditions rejects zero timestamps, reversed or oversized bounded
// windows and an end more than one minute behind now. Single-sided bounds remain
// valid; actual applicability at the evaluation instant is checked separately.
// ValidateConditions отклоняет нулевые даты, обратные или слишком длинные замкнутые
// интервалы и окончание более чем на минуту раньше now. Односторонние границы
// допустимы; применимость в момент вычисления проверяется отдельно.
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

// Evaluate computes an allow-only decision from the supplied authoritative subject,
// target and bindings. Active matching bindings contribute ordered explanation
// steps; unknown permissions, invalid targets and no matches deny without exposing
// binding details. Callers own subject eligibility, tenant isolation and related refs.
// Evaluate вычисляет решение только по разрешающим привязкам из переданных
// авторитетных субъекта, цели и привязок. Активные совпадения добавляют упорядоченные
// шаги объяснения; неизвестное разрешение, неверная цель и отсутствие совпадений
// дают отказ без сведений о привязках. Допуск субъекта, tenant и связанные refs
// устанавливает вызывающий.
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

// bindingTargetsSubject matches an exact typed subject or an OIDC group present
// in the supplied subject snapshot. Group membership must already be trusted;
// the helper does not fetch identity state or authenticate a caller.
// bindingTargetsSubject сопоставляет точного типизированного субъекта либо OIDC
// группу из его снимка. Членство должно быть доверенным до вызова; helper не
// загружает состояние идентичности и не аутентифицирует вызывающего.
func bindingTargetsSubject(binding, subject entity.AccessSubject) bool {
	if binding.Kind == "OIDC_GROUP" {
		return binding.Ref == subject.Ref || contains(subject.OIDCGroupRefs, binding.Ref)
	}
	return binding.Kind == subject.Kind && binding.Ref == subject.Ref
}

// scopeMatches applies scope breadth to an already resolved target. Instance
// bindings require the same project and either exact identity or a related ref
// supplied by the owner; the helper never discovers relationships from payloads.
// scopeMatches применяет ширину области к уже разрешённой цели. Привязки экземпляра
// требуют того же проекта и точного ресурса либо связанного ref от владельца;
// helper самостоятельно не выводит связи из пользовательских данных.
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

// conditionsMatch uses an inclusive start and exclusive end, then enforces exact
// nonempty ownership when requested or for run.cancel.own. It evaluates conditions
// without revalidating their stored shape or changing timestamps.
// conditionsMatch включает начало интервала и исключает окончание, затем требует
// точного непустого владельца по условию или для run.cancel.own. Условия вычисляются
// без повторной проверки сохранённой структуры и без изменения временных границ.
func conditionsMatch(conditions entity.AccessConditions, permissionKey, subjectRef, ownerSubjectRef string, at time.Time) bool {
	if conditions.ValidFrom != nil && at.Before(*conditions.ValidFrom) || conditions.ValidUntil != nil && !at.Before(*conditions.ValidUntil) {
		return false
	}
	requireOwner := conditions.RequireOwner || permissionKey == "run.cancel.own"
	return !requireOwner || ownerSubjectRef != "" && ownerSubjectRef == subjectRef
}

// uniqueKnown rejects unknown and repeated values using the supplied closed-set
// predicate. Empty input succeeds here; callers enforce required list cardinality.
// uniqueKnown отклоняет неизвестные и повторные значения по переданному предикату
// закрытого набора. Пустой список допустим здесь; обязательность задают callers.
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

// knownSubjectKind recognizes only user, OIDC group and service binding subjects;
// accepting a kind does not establish the existence or eligibility of a subject.
// knownSubjectKind распознаёт только пользователя, OIDC группу и сервис как виды
// субъектов привязки; известный вид не доказывает существование или допуск субъекта.
func knownSubjectKind(value string) bool {
	return value == "USER" || value == "OIDC_GROUP" || value == "SERVICE"
}

// knownScope recognizes the four supported scope discriminators without aliases;
// locator fields and resource ownership require their separate checks.
// knownScope распознаёт четыре поддерживаемых вида области без псевдонимов;
// поля адресации и владение ресурсом проверяются отдельно.
func knownScope(value string) bool {
	return value == "ORGANIZATION" || value == "PROJECT" || value == "RESOURCE_KIND" || value == "RESOURCE_INSTANCE"
}

// knownResourceKind recognizes the closed set of resources addressable by RBAC.
// Permission compatibility and concrete resource eligibility are checked elsewhere.
// knownResourceKind распознаёт закрытый набор ресурсов, адресуемых через RBAC.
// Совместимость разрешения и допуск конкретного ресурса проверяются отдельно.
func knownResourceKind(value string) bool {
	return value == "ORGANIZATION" || value == "PROJECT" || value == "AGENT" || value == "WORKFLOW" || value == "RUN" ||
		value == "OWNER_GATE" || value == "ARTIFACT" || value == "SCHEDULE" || value == "INTEGRATION" ||
		value == "RUNTIME_ENVIRONMENT" || value == "ROLE_IMAGE" || value == "SESSION" || value == "SECRET" ||
		value == "PROVIDER_ACCOUNT"
}

// contains tests exact string membership without case folding or normalization;
// a nil or empty list never matches, preserving closed-set comparisons.
// contains проверяет точное строковое вхождение без изменения регистра и нормализации;
// nil и пустой список не совпадают, сохраняя сравнение по закрытому набору.
func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
