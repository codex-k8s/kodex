package entity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

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
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// MarshalSnapshot кодирует каноническую принадлежащую сервису проекцию без
// метаданных интерфейса.
func MarshalSnapshot(resource Resource) ([]byte, error) {
	if err := resource.Validate(); err != nil {
		return nil, err
	}
	spec, err := MarshalSpec(resource.Spec)
	if err != nil {
		return nil, err
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
		CreatedAt:      resource.CreatedAt,
		UpdatedAt:      resource.UpdatedAt,
	})
}

// UnmarshalSnapshot отклоняет неизвестные поля и семантически неверную проекцию.
func UnmarshalSnapshot(raw []byte) (Resource, error) {
	var stored storedResource
	if err := decodeStrict(raw, &stored); err != nil {
		return Resource{}, errors.New("decode stored resource")
	}
	spec, err := UnmarshalSpec(stored.Kind, stored.Spec)
	if err != nil {
		return Resource{}, err
	}
	resource := Resource{
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
		CreatedAt:      stored.CreatedAt,
		UpdatedAt:      stored.UpdatedAt,
	}
	return resource, resource.Validate()
}

// ProjectionSHA256 связывает неизменяемую ссылку с точной проекцией.
func ProjectionSHA256(resource Resource) (string, error) {
	raw, err := MarshalSnapshot(resource)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// MarshalSpec кодирует только предварительно проверенную закрытую спецификацию.
func MarshalSpec(spec Spec) ([]byte, error) {
	if spec == nil || spec.Validate() != nil {
		return nil, errors.New("resource specification is invalid")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, errors.New("marshal resource specification")
	}
	return raw, nil
}

// UnmarshalSpec выбирает целевой тип только по назначенному сервером виду и
// отклоняет расширения.
func UnmarshalSpec(kind enum.Kind, raw []byte) (Spec, error) {
	var target Spec
	switch kind {
	case enum.KindProject:
		target = &ProjectSpec{}
	case enum.KindTeam:
		target = &TeamSpec{}
	case enum.KindChat:
		target = &ChatSpec{}
	case enum.KindRole:
		target = &RoleSpec{}
	case enum.KindPromptProfile:
		target = &PromptProfileSpec{}
	case enum.KindCredentialBinding:
		target = &CredentialBindingSpec{}
	case enum.KindRepositoryWorkspace:
		target = &RepositoryWorkspaceSpec{}
	case enum.KindIntegration:
		target = &IntegrationSpec{}
	case enum.KindRuntimeRevision:
		target = &RuntimeRevisionSpec{}
	case enum.KindSession:
		target = &SessionSpec{}
	case enum.KindTurn:
		target = &TurnSpec{}
	case enum.KindProcessRun:
		target = &ProcessRunSpec{}
	case enum.KindSchedule:
		target = &ScheduleSpec{}
	case enum.KindOwnerGate:
		target = &OwnerGateSpec{}
	case enum.KindMemoryRecord:
		target = &MemoryRecordSpec{}
	case enum.KindWorkClaim:
		target = &WorkClaimSpec{}
	case enum.KindArtifact:
		target = &ArtifactSpec{}
	case enum.KindRoleImageRecipe:
		target = &RoleImageRecipeSpec{}
	case enum.KindImageBuild:
		target = &ImageBuildSpec{}
	case enum.KindImageArtifact:
		target = &ImageArtifactSpec{}
	default:
		return nil, errors.New("resource kind is invalid")
	}
	if err := decodeStrict(raw, target); err != nil {
		return nil, errors.New("decode resource specification")
	}
	spec := dereferenceSpec(target)
	if spec == nil || spec.Validate() != nil {
		return nil, errors.New("stored resource specification is invalid")
	}
	return spec, nil
}

func dereferenceSpec(spec Spec) Spec {
	switch value := spec.(type) {
	case *ProjectSpec:
		return *value
	case *TeamSpec:
		return *value
	case *ChatSpec:
		return *value
	case *RoleSpec:
		return *value
	case *PromptProfileSpec:
		return *value
	case *CredentialBindingSpec:
		return *value
	case *RepositoryWorkspaceSpec:
		return *value
	case *IntegrationSpec:
		return *value
	case *RuntimeRevisionSpec:
		return *value
	case *SessionSpec:
		return *value
	case *TurnSpec:
		return *value
	case *ProcessRunSpec:
		return *value
	case *ScheduleSpec:
		return *value
	case *OwnerGateSpec:
		return *value
	case *MemoryRecordSpec:
		return *value
	case *WorkClaimSpec:
		return *value
	case *ArtifactSpec:
		return *value
	case *RoleImageRecipeSpec:
		return *value
	case *ImageBuildSpec:
		return *value
	case *ImageArtifactSpec:
		return *value
	default:
		return nil
	}
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON has trailing data")
	}
	return nil
}
