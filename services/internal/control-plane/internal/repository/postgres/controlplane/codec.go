package controlplane

import (
	"encoding/json"
	"errors"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

type storedResource struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	ProjectID      string          `json:"projectId"`
	ParentID       string          `json:"parentId,omitempty"`
	OwnerActorID   string          `json:"ownerActorId"`
	Kind           enum.Kind       `json:"kind"`
	Name           string          `json:"name"`
	State          enum.State      `json:"state"`
	Version        uint64          `json:"version"`
	Spec           json.RawMessage `json:"spec"`
	CreatedAt      json.RawMessage `json:"createdAt"`
	UpdatedAt      json.RawMessage `json:"updatedAt"`
}

func marshalSpec(spec entity.Spec) ([]byte, error) {
	if spec == nil || spec.Validate() != nil {
		return nil, errors.New("resource specification is invalid")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, errors.New("marshal resource specification")
	}
	return raw, nil
}

func unmarshalSpec(kind enum.Kind, raw []byte) (entity.Spec, error) {
	var target entity.Spec
	switch kind {
	case enum.KindProject:
		target = &entity.ProjectSpec{}
	case enum.KindTeam:
		target = &entity.TeamSpec{}
	case enum.KindChat:
		target = &entity.ChatSpec{}
	case enum.KindRole:
		target = &entity.RoleSpec{}
	case enum.KindPromptProfile:
		target = &entity.PromptProfileSpec{}
	case enum.KindCredentialBinding:
		target = &entity.CredentialBindingSpec{}
	case enum.KindRepositoryWorkspace:
		target = &entity.RepositoryWorkspaceSpec{}
	case enum.KindIntegration:
		target = &entity.IntegrationSpec{}
	case enum.KindRuntimeRevision:
		target = &entity.RuntimeRevisionSpec{}
	case enum.KindSession:
		target = &entity.SessionSpec{}
	case enum.KindTurn:
		target = &entity.TurnSpec{}
	case enum.KindProcessRun:
		target = &entity.ProcessRunSpec{}
	case enum.KindSchedule:
		target = &entity.ScheduleSpec{}
	case enum.KindOwnerGate:
		target = &entity.OwnerGateSpec{}
	case enum.KindMemoryRecord:
		target = &entity.MemoryRecordSpec{}
	case enum.KindWorkClaim:
		target = &entity.WorkClaimSpec{}
	case enum.KindArtifact:
		target = &entity.ArtifactSpec{}
	default:
		return nil, errors.New("resource kind is invalid")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, errors.New("decode resource specification")
	}
	spec := dereferenceSpec(target)
	if spec == nil || spec.Validate() != nil {
		return nil, errors.New("stored resource specification is invalid")
	}
	return spec, nil
}

func dereferenceSpec(spec entity.Spec) entity.Spec {
	switch value := spec.(type) {
	case *entity.ProjectSpec:
		return *value
	case *entity.TeamSpec:
		return *value
	case *entity.ChatSpec:
		return *value
	case *entity.RoleSpec:
		return *value
	case *entity.PromptProfileSpec:
		return *value
	case *entity.CredentialBindingSpec:
		return *value
	case *entity.RepositoryWorkspaceSpec:
		return *value
	case *entity.IntegrationSpec:
		return *value
	case *entity.RuntimeRevisionSpec:
		return *value
	case *entity.SessionSpec:
		return *value
	case *entity.TurnSpec:
		return *value
	case *entity.ProcessRunSpec:
		return *value
	case *entity.ScheduleSpec:
		return *value
	case *entity.OwnerGateSpec:
		return *value
	case *entity.MemoryRecordSpec:
		return *value
	case *entity.WorkClaimSpec:
		return *value
	case *entity.ArtifactSpec:
		return *value
	default:
		return nil
	}
}

func marshalResource(resource entity.Resource) ([]byte, error) {
	if err := resource.Validate(); err != nil {
		return nil, err
	}
	spec, err := marshalSpec(resource.Spec)
	if err != nil {
		return nil, err
	}
	createdAt, err := resource.CreatedAt.MarshalJSON()
	if err != nil {
		return nil, errors.New("marshal resource creation time")
	}
	updatedAt, err := resource.UpdatedAt.MarshalJSON()
	if err != nil {
		return nil, errors.New("marshal resource update time")
	}
	return json.Marshal(storedResource{
		ID:             resource.ID,
		OrganizationID: resource.OrganizationID,
		ProjectID:      resource.ProjectID,
		ParentID:       resource.ParentID,
		OwnerActorID:   resource.OwnerActorID,
		Kind:           resource.Kind,
		Name:           resource.Name,
		State:          resource.State,
		Version:        resource.Version,
		Spec:           spec,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	})
}

func unmarshalResource(raw []byte) (entity.Resource, error) {
	var stored storedResource
	if err := json.Unmarshal(raw, &stored); err != nil {
		return entity.Resource{}, errors.New("decode stored resource")
	}
	spec, err := unmarshalSpec(stored.Kind, stored.Spec)
	if err != nil {
		return entity.Resource{}, err
	}
	resource := entity.Resource{
		ID:             stored.ID,
		OrganizationID: stored.OrganizationID,
		ProjectID:      stored.ProjectID,
		ParentID:       stored.ParentID,
		OwnerActorID:   stored.OwnerActorID,
		Kind:           stored.Kind,
		Name:           stored.Name,
		State:          stored.State,
		Version:        stored.Version,
		Spec:           spec,
	}
	if err := resource.CreatedAt.UnmarshalJSON(stored.CreatedAt); err != nil {
		return entity.Resource{}, errors.New("decode resource creation time")
	}
	if err := resource.UpdatedAt.UnmarshalJSON(stored.UpdatedAt); err != nil {
		return entity.Resource{}, errors.New("decode resource update time")
	}
	return resource, resource.Validate()
}
