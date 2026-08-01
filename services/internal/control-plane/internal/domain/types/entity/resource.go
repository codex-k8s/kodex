package entity

import (
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

// Resource — единая версионированная оболочка типизированного агрегата.
type Resource struct {
	ID             string
	OrganizationID string
	ProjectID      string
	ParentID       string
	OwnerActorID   string
	Kind           enum.Kind
	Name           string
	State          enum.State
	Version        uint64
	Spec           Spec
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Validate закрыто проверяет согласованность aggregate envelope и spec.
func (resource Resource) Validate() error {
	if value.ValidateID(resource.ID) != nil ||
		value.ValidateID(resource.OrganizationID) != nil ||
		value.ValidateID(resource.OwnerActorID) != nil ||
		!resource.Kind.Valid() ||
		value.ValidateName(resource.Name) != nil ||
		resource.Version == 0 ||
		resource.Spec == nil ||
		resource.Spec.Kind() != resource.Kind ||
		resource.CreatedAt.IsZero() || resource.UpdatedAt.IsZero() ||
		resource.UpdatedAt.Before(resource.CreatedAt) {
		return errors.New("resource envelope is invalid")
	}
	if resource.ProjectID != "" && value.ValidateID(resource.ProjectID) != nil {
		return errors.New("resource project is invalid")
	}
	if resource.ParentID != "" && value.ValidateID(resource.ParentID) != nil {
		return errors.New("resource parent is invalid")
	}
	return resource.Spec.Validate()
}

// New создаёт назначенные сервером ID агрегата, владельца, состояние, версию
// и временные отметки.
func New(
	id string,
	organizationID string,
	projectID string,
	parentID string,
	ownerActorID string,
	kind enum.Kind,
	name string,
	spec Spec,
	now time.Time,
) (Resource, error) {
	resource := Resource{
		ID:             id,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ParentID:       parentID,
		OwnerActorID:   ownerActorID,
		Kind:           kind,
		Name:           name,
		State:          enum.InitialState(kind),
		Version:        1,
		Spec:           spec,
		CreatedAt:      now.UTC().Truncate(time.Microsecond),
		UpdatedAt:      now.UTC().Truncate(time.Microsecond),
	}
	if err := resource.Validate(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

// Update применяет прикладное обновление и увеличивает версию OCC.
func (resource Resource) Update(name string, spec Spec, now time.Time) (Resource, error) {
	if resource.State.Terminal() || resource.State == enum.StateDeletionPending ||
		spec == nil || spec.Kind() != resource.Kind {
		return Resource{}, errors.New("resource update is not allowed")
	}
	resource.Name = name
	resource.Spec = spec
	resource.Version++
	resource.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	if err := resource.Validate(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

// Transition применяет переход закрытого жизненного цикла.
func (resource Resource) Transition(target enum.State, now time.Time) (Resource, error) {
	if !enum.TransitionAllowed(resource.Kind, resource.State, target) {
		return Resource{}, errors.New("resource transition is not allowed")
	}
	resource.State = target
	resource.Version++
	resource.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return resource, resource.Validate()
}

// ReplaceAndTransition атомарно меняет типизированную нагрузку и состояние
// с одним увеличением версии OCC.
func (resource Resource) ReplaceAndTransition(
	spec Spec,
	target enum.State,
	now time.Time,
) (Resource, error) {
	if spec == nil || spec.Kind() != resource.Kind ||
		!enum.TransitionAllowed(resource.Kind, resource.State, target) {
		return Resource{}, errors.New("resource replacement transition is not allowed")
	}
	resource.Spec = spec
	resource.State = target
	resource.Version++
	resource.UpdatedAt = now.UTC().Truncate(time.Microsecond)
	return resource, resource.Validate()
}
