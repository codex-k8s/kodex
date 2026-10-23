package entity

import "time"

// PermissionDefinition describes a catalogue permission's presentation keys, risk, scopes, resource
// kinds, and supported ownership condition. It defines available policy vocabulary, not an issued
// grant or a completed authorization check.
// PermissionDefinition описывает ключи представления, риск, области, виды ресурсов и поддержку условия
// владения для разрешения каталога. Это доступный словарь политики, а не выданное разрешение или
// выполненная проверка доступа.
type PermissionDefinition struct {
	Key, NameKey, DescriptionKey string
	Risk                         string
	AllowedScopes, ResourceKinds []string
	OwnerConditionSupported      bool
}

// AccessSubject is a read model of an access-control subject and its known OIDC group references.
// Active and group membership are state to be evaluated by the owner, not a replacement for transport
// authentication.
// AccessSubject является моделью чтения субъекта управления доступом и известных ссылок на группы
// OIDC. Активность и членство в группах оценивает владелец; они не заменяют аутентификацию транспорта.
type AccessSubject struct {
	Ref, Kind, DisplayName string
	Active                 bool
	OIDCGroupRefs          []string
}

// OIDCGroup presents a synchronized identity group with member and binding counts and observation
// times. Counts and timestamps describe the projection and do not prove that an arbitrary caller
// currently belongs to the group.
// OIDCGroup представляет синхронизированную группу личности с количеством участников и привязок и
// временем наблюдений. Счётчики и отметки характеризуют проекцию и не доказывают текущее членство
// произвольного вызывающего субъекта.
type OIDCGroup struct {
	Ref, DisplayName, State    string
	MemberCount, BindingCount  int32
	LastSeenAt, SynchronizedAt time.Time
}

// AccessRoleVersion captures the permissions, scopes, metadata, revision, and author of a stored role
// version. Bindings refer to this version explicitly so that later role edits are not silently
// substituted during evaluation.
// AccessRoleVersion фиксирует разрешения, области, метаданные, ревизию и автора сохранённой версии
// роли. Привязки явно ссылаются на эту версию, чтобы последующие изменения роли не подменялись
// незаметно при оценке.
type AccessRoleVersion struct {
	Ref, RoleRef, Name, Description, ChangeComment string
	Revision                                       int64
	PermissionKeys, AllowedScopes                  []string
	CreatedAt                                      time.Time
	CreatedBy                                      User
}

// AccessRole presents role identity, lifecycle state, current immutable version, and binding count.
// Version is the role aggregate's concurrency marker, distinct from the revision of CurrentVersion.
// AccessRole представляет личность роли, состояние жизненного цикла, текущую неизменяемую версию и
// количество привязок. Version является маркером конкуренции агрегата роли и отличается от ревизии
// CurrentVersion.
type AccessRole struct {
	Ref, Kind, State string
	Version          int64
	CurrentVersion   AccessRoleVersion
	BindingCount     int32
	UpdatedAt        time.Time
}

// AccessScope describes a platform, project, or resource target and related resource references for
// policy matching. These selectors must be resolved within the owner's boundary; a matching string
// does not prove access to the referenced resource.
// AccessScope описывает платформенную, проектную или ресурсную цель и связанные ссылки для
// сопоставления политики. Эти признаки разрешаются в области владельца; совпадение строки не
// доказывает доступ к указанному ресурсу.
type AccessScope struct {
	Kind, ProjectRef, ResourceKind, ResourceRef string
	RelatedResourceRefs                         map[string]string
}

// AccessConditions carries optional validity bounds and a requirement for resource ownership. It
// declares conditions for evaluation and does not check the clock or establish ownership by itself.
// AccessConditions передаёт необязательные границы срока действия и требование владения ресурсом.
// Структура объявляет условия оценки, но сама не проверяет время и не устанавливает владение.
type AccessConditions struct {
	ValidFrom, ValidUntil *time.Time
	RequireOwner          bool
}

// AccessBinding presents a versioned subject-to-role-version association with scope, conditions,
// lifecycle state, and timestamps. Effective access still depends on the owner's eligibility
// evaluation rather than the existence of this record alone.
// AccessBinding представляет версионированную связь субъекта с версией роли, областью, условиями,
// состоянием и временем. Фактический доступ по-прежнему зависит от оценки допустимости владельцем, а
// не только от наличия записи.
type AccessBinding struct {
	Ref, State, PresentationKind string
	Version                      int64
	Subject                      AccessSubject
	RoleVersion                  AccessRoleVersion
	Scope                        AccessScope
	Conditions                   AccessConditions
	CreatedAt, UpdatedAt         time.Time
}

// AccessExplanationStep records one source and binding reference used to explain an access decision.
// It is diagnostic provenance for an evaluated scope, not a reusable authorization token.
// AccessExplanationStep фиксирует источник и ссылку на привязку для объяснения решения о доступе. Это
// диагностическое происхождение оценки области, а не повторно используемый токен авторизации.
type AccessExplanationStep struct {
	Code, BindingRef, RoleRef, RoleVersionRef string
	SourceKind, SourceRef                     string
	Scope                                     AccessScope
}

// EffectiveAccessDecision reports whether one permission is allowed for an evaluated target and
// includes explanation steps. Consumers must preserve the associated subject, scope, and evaluation
// time; the boolean is not a transferable grant.
// EffectiveAccessDecision сообщает допустимость одного разрешения для оценённой цели и шаги
// объяснения. Потребители сохраняют связанные субъект, область и время оценки; логический признак не
// является передаваемым разрешением.
type EffectiveAccessDecision struct {
	PermissionKey string
	Allowed       bool
	Target        AccessScope
	Explanation   []AccessExplanationStep
}

// EffectiveAccess groups evaluated permission decisions for one subject at a recorded time. This
// snapshot describes the evaluation and does not automatically refresh when bindings or membership
// later change.
// EffectiveAccess объединяет оценённые решения по разрешениям для одного субъекта на зафиксированный
// момент. Снимок описывает оценку и не обновляется автоматически при последующем изменении привязок
// или членства.
type EffectiveAccess struct {
	Subject     AccessSubject
	Decisions   []EffectiveAccessDecision
	EvaluatedAt time.Time
}

// AccessSimulation compares the current and hypothetical decision for one subject at an evaluation
// time. The simulated result is explanatory and must not be used as a persisted binding or production
// grant.
// AccessSimulation сравнивает текущее и предполагаемое решение для одного субъекта на момент оценки.
// Результат моделирования служит объяснением и не используется как сохранённая привязка или рабочее
// разрешение.
type AccessSimulation struct {
	Subject            AccessSubject
	Current, Simulated EffectiveAccessDecision
	EvaluatedAt        time.Time
}
