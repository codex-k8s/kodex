package command

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

type AccessRoleInput struct {
	RoleRef, Name, Description, ChangeComment string
	PermissionKeys, AllowedScopes             []string
}

type AccessBindingInput struct {
	BindingRef, SubjectKind, SubjectRef, RoleVersionRef string
	Scope                                               entity.AccessScope
	Conditions                                          entity.AccessConditions
}

type AccessSimulationInput struct {
	SubjectRef, PermissionKey string
	Target                    entity.AccessScope
	Role                      AccessRoleInput
	Binding                   AccessBindingInput
	EvaluatedAt               *time.Time
}
