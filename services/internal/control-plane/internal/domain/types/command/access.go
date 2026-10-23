package command

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// AccessRoleInput proposes role metadata, permission keys, and allowed scopes. The owner validates the
// permission catalogue and produces the persisted role version; this value does not itself grant
// access.
// AccessRoleInput предлагает метаданные роли, ключи разрешений и допустимые области. Владелец
// проверяет каталог разрешений и создаёт сохраняемую версию роли; само значение не предоставляет
// доступ.
type AccessRoleInput struct {
	RoleRef, Name, Description, ChangeComment string
	PermissionKeys, AllowedScopes             []string
}

// AccessBindingInput proposes binding a subject to an exact role version with scope and time or
// ownership conditions. Subject references and condition flags are inputs to owner validation, not
// authenticated identity or proof of ownership.
// AccessBindingInput предлагает привязать субъект к точной версии роли с областью и условиями времени
// или владения. Ссылки на субъект и признаки условий проверяет владелец; они не удостоверяют личность
// и не доказывают владение.
type AccessBindingInput struct {
	BindingRef, SubjectKind, SubjectRef, RoleVersionRef string
	Scope                                               entity.AccessScope
	Conditions                                          entity.AccessConditions
}

// AccessSimulationInput supplies hypothetical role and binding changes for comparison at an optional
// evaluation time. Simulation must not persist the proposed grants, and its SubjectRef is distinct
// from the authenticated caller.
// AccessSimulationInput передаёт предполагаемые изменения роли и привязки для сравнения на
// необязательный момент оценки. Моделирование не должно сохранять предлагаемые разрешения, а
// SubjectRef не тождественен проверенному вызывающему субъекту.
type AccessSimulationInput struct {
	SubjectRef, PermissionKey string
	Target                    entity.AccessScope
	Role                      AccessRoleInput
	Binding                   AccessBindingInput
	EvaluatedAt               *time.Time
}
