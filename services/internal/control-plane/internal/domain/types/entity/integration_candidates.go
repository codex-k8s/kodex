package entity

// IntegrationCandidateContext selects a connection, project, recipient, capability, and optional
// workflow step for candidate discovery. The owner resolves these references within authenticated
// scope; selectors do not grant access to the referenced resources.
// IntegrationCandidateContext выбирает подключение, проект, получателя, возможность и необязательный
// этап процесса для поиска кандидатов. Владелец разрешает ссылки в удостоверенной области; признаки
// выбора не предоставляют доступ к ресурсам.
type IntegrationCandidateContext struct {
	ConnectionRef, ProjectRef, RecipientKind, RecipientRef string
	CapabilityKey, WorkflowRef, StepKey                    string
}

// IntegrationCandidatePins binds a candidate page or item to the context digest, definition version
// and digest, connection, project and recipient versions, and optional workflow revision. The owner
// revalidates these pins when granting instead of trusting an earlier list response.
// IntegrationCandidatePins связывает страницу или элемент кандидатов с дайджестом контекста, версией и
// дайджестом определения, версиями подключения, проекта и получателя и необязательной ревизией
// процесса. При выдаче разрешения владелец повторно проверяет привязки, а не доверяет прежнему ответу
// списка.
type IntegrationCandidatePins struct {
	ContextDigest, DefinitionVersion, DefinitionDigest  string
	ConnectionVersion, ProjectVersion, RecipientVersion int64
	WorkflowRevisionRef                                 string
}

// IntegrationConnectionCandidate describes one connection's presentation, credential kind, resource
// scope, grantability, usability, and exact candidate pins. CredentialKind is descriptive metadata,
// not credential bytes; eligibility flags do not issue a grant.
// IntegrationConnectionCandidate описывает представление подключения, вид реквизитов, область
// ресурсов, допустимость выдачи и использования и точные привязки кандидата. CredentialKind является
// описанием, а не байтами реквизитов; признаки допустимости не выдают разрешение.
type IntegrationConnectionCandidate struct {
	ConnectionRef, Name, DefinitionKey, ProviderName, CredentialKind string
	ProjectRef, Reason                                               string
	ResourceScope                                                    map[string]string
	Grantable, Usable                                                bool
	Pins                                                             IntegrationCandidatePins
}

// IntegrationProjectCandidate presents one project eligible or ineligible for the current integration
// context, including the reason and version pins. The selection remains subject to owner validation
// when a binding is created.
// IntegrationProjectCandidate представляет допустимый или недопустимый проект для текущего контекста
// интеграции с причиной и привязками версий. Выбор проверяется владельцем при создании привязки.
type IntegrationProjectCandidate struct {
	ProjectRef, Name, Reason string
	Grantable                bool
	Pins                     IntegrationCandidatePins
}

// IntegrationRecipientCandidate describes an agent or workflow recipient within a project with
// eligibility reason and exact candidate pins. Recipient identity is a selection to resolve, not
// authority supplied by the caller.
// IntegrationRecipientCandidate описывает получателя-сотрудника или процесс в проекте с причиной
// допустимости и точными привязками кандидата. Личность получателя является разрешаемым выбором, а не
// переданными вызывающим кодом полномочиями.
type IntegrationRecipientCandidate struct {
	RecipientRef, Name, RecipientKind, ProjectRef, Reason string
	Grantable                                             bool
	Pins                                                  IntegrationCandidatePins
}

// IntegrationCapabilityCandidate pairs a capability with grantability, explanation, current grant
// identity and version, and candidate pins. CurrentGrantRef is observed state; editing it requires the
// exact specialized owner command.
// IntegrationCapabilityCandidate связывает возможность с допустимостью выдачи, пояснением, личностью и
// версией текущего разрешения и привязками кандидата. CurrentGrantRef является наблюдаемым состоянием;
// изменение требует точной специализированной команды владельца.
type IntegrationCapabilityCandidate struct {
	Capability          IntegrationCapability
	Grantable           bool
	Reason              string
	CurrentGrantRef     string
	CurrentGrantVersion int64
	Pins                IntegrationCandidatePins
}

// IntegrationCandidatePage stores the common context, dependency pins, total, and opaque next-page
// token of a candidate listing. The token preserves query context and does not authenticate an actor
// or widen resource scope.
// IntegrationCandidatePage хранит общие контекст, привязки зависимостей, количество и непрозрачный
// токен следующей страницы списка кандидатов. Токен сохраняет контекст запроса, но не удостоверяет
// субъекта и не расширяет область ресурсов.
type IntegrationCandidatePage struct {
	NextPageToken, ContextDigest string
	Total                        int64
	Context                      IntegrationCandidateContext
	Pins                         IntegrationCandidatePins
}

// IntegrationConnectionCandidates combines a context-pinned candidate page with typed connection
// items. Consumers must preserve page and item pins together when presenting a later grant request.
// IntegrationConnectionCandidates объединяет закреплённую контекстом страницу кандидатов с
// типизированными подключениями. Потребители сохраняют привязки страницы и элементов вместе при
// последующем запросе разрешения.
type IntegrationConnectionCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationConnectionCandidate
}

// IntegrationProjectCandidates combines common candidate pagination with project selections for the
// resolved integration context. Listing a project does not bind the connection to it.
// IntegrationProjectCandidates объединяет общую пагинацию кандидатов с выбором проектов для
// разрешённого контекста интеграции. Перечисление проекта не привязывает к нему подключение.
type IntegrationProjectCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationProjectCandidate
}

// IntegrationRecipientCandidates combines the pinned candidate page with typed recipient selections.
// Recipient eligibility is observed at listing time and must be checked again by the mutation owner.
// IntegrationRecipientCandidates объединяет закреплённую страницу кандидатов с типизированным выбором
// получателей. Допустимость получателя наблюдается при чтении списка и повторно проверяется владельцем
// изменения.
type IntegrationRecipientCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationRecipientCandidate
}

// IntegrationCapabilityCandidates combines common candidate context and pagination with typed
// capability choices and current-grant observations. It contains candidates, not newly issued
// capability grants.
// IntegrationCapabilityCandidates объединяет общие контекст и пагинацию кандидатов с типизированными
// возможностями и наблюдениями текущих разрешений. Структура содержит кандидатов, а не вновь выданные
// разрешения возможностей.
type IntegrationCapabilityCandidates struct {
	IntegrationCandidatePage
	Items []IntegrationCapabilityCandidate
}
