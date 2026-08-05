package websockettransport

import (
	"encoding/json"
	"errors"

	httpgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
)

const errClosedEnum = "closed WebSocket enum is invalid"

type SubscribeMessageType string

const SubscribeMessageTypeSubscribe SubscribeMessageType = "SUBSCRIBE"

func (value SubscribeMessageType) valid() bool { return value == SubscribeMessageTypeSubscribe }

func (value *SubscribeMessageType) UnmarshalJSON(raw []byte) error {
	return unmarshalClosedEnum(raw, value, SubscribeMessageType.valid)
}

func (value SubscribeMessageType) MarshalJSON() ([]byte, error) {
	return marshalClosedEnum(value, value.valid())
}

type SnapshotMessageType string

const SnapshotMessageTypeSnapshot SnapshotMessageType = "SNAPSHOT"

func (value SnapshotMessageType) valid() bool { return value == SnapshotMessageTypeSnapshot }

func (value *SnapshotMessageType) UnmarshalJSON(raw []byte) error {
	return unmarshalClosedEnum(raw, value, SnapshotMessageType.valid)
}

func (value SnapshotMessageType) MarshalJSON() ([]byte, error) {
	return marshalClosedEnum(value, value.valid())
}

type ProblemMessageType string

const ProblemMessageTypeProblem ProblemMessageType = "PROBLEM"

func (value ProblemMessageType) valid() bool { return value == ProblemMessageTypeProblem }

func (value *ProblemMessageType) UnmarshalJSON(raw []byte) error {
	return unmarshalClosedEnum(raw, value, ProblemMessageType.valid)
}

func (value ProblemMessageType) MarshalJSON() ([]byte, error) {
	return marshalClosedEnum(value, value.valid())
}

type ProjectionChannel string

const (
	ProjectionChannelRuns                 ProjectionChannel = "RUNS"
	ProjectionChannelIncidents            ProjectionChannel = "INCIDENTS"
	ProjectionChannelResources            ProjectionChannel = "RESOURCES"
	ProjectionChannelConfigurationChanges ProjectionChannel = "CONFIGURATION_CHANGES"
)

func (value ProjectionChannel) valid() bool {
	switch value {
	case ProjectionChannelRuns, ProjectionChannelIncidents,
		ProjectionChannelResources, ProjectionChannelConfigurationChanges:
		return true
	default:
		return false
	}
}

func (value *ProjectionChannel) UnmarshalJSON(raw []byte) error {
	return unmarshalClosedEnum(raw, value, ProjectionChannel.valid)
}

func (value ProjectionChannel) MarshalJSON() ([]byte, error) {
	return marshalClosedEnum(value, value.valid())
}

type ResourceKind string

const (
	ResourceKindProject             ResourceKind = "PROJECT"
	ResourceKindTeam                ResourceKind = "TEAM"
	ResourceKindChat                ResourceKind = "CHAT"
	ResourceKindRole                ResourceKind = "ROLE"
	ResourceKindPromptProfile       ResourceKind = "PROMPT_PROFILE"
	ResourceKindCredentialBinding   ResourceKind = "CREDENTIAL_BINDING"
	ResourceKindRepositoryWorkspace ResourceKind = "REPOSITORY_WORKSPACE"
	ResourceKindIntegration         ResourceKind = "INTEGRATION"
	ResourceKindRuntimeRevision     ResourceKind = "RUNTIME_REVISION"
	ResourceKindSession             ResourceKind = "SESSION"
	ResourceKindTurn                ResourceKind = "TURN"
	ResourceKindProcessRun          ResourceKind = "PROCESS_RUN"
	ResourceKindSchedule            ResourceKind = "SCHEDULE"
	ResourceKindOwnerGate           ResourceKind = "OWNER_GATE"
	ResourceKindMemoryRecord        ResourceKind = "MEMORY_RECORD"
	ResourceKindWorkClaim           ResourceKind = "WORK_CLAIM"
	ResourceKindArtifact            ResourceKind = "ARTIFACT"
	ResourceKindRoleImageRecipe     ResourceKind = "ROLE_IMAGE_RECIPE"
	ResourceKindImageBuild          ResourceKind = "IMAGE_BUILD"
	ResourceKindImageArtifact       ResourceKind = "IMAGE_ARTIFACT"
)

func (value ResourceKind) valid() bool {
	switch value {
	case ResourceKindProject, ResourceKindTeam, ResourceKindChat, ResourceKindRole,
		ResourceKindPromptProfile, ResourceKindCredentialBinding, ResourceKindRepositoryWorkspace,
		ResourceKindIntegration, ResourceKindRuntimeRevision, ResourceKindSession, ResourceKindTurn,
		ResourceKindProcessRun, ResourceKindSchedule, ResourceKindOwnerGate, ResourceKindMemoryRecord,
		ResourceKindWorkClaim, ResourceKindArtifact, ResourceKindRoleImageRecipe,
		ResourceKindImageBuild, ResourceKindImageArtifact:
		return true
	default:
		return false
	}
}

func (value *ResourceKind) UnmarshalJSON(raw []byte) error {
	return unmarshalClosedEnum(raw, value, ResourceKind.valid)
}

func (value ResourceKind) MarshalJSON() ([]byte, error) {
	return marshalClosedEnum(value, value.valid())
}

func unmarshalClosedEnum[T ~string](raw []byte, target *T, valid func(T) bool) error {
	if target == nil {
		return errors.New(errClosedEnum)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return errors.New(errClosedEnum)
	}
	value := T(decoded)
	if !valid(value) {
		return errors.New(errClosedEnum)
	}
	*target = value
	return nil
}

func marshalClosedEnum[T ~string](value T, valid bool) ([]byte, error) {
	if !valid {
		return nil, errors.New(errClosedEnum)
	}
	return json.Marshal(string(value))
}

type SubscribeEnvelope struct {
	Type          SubscribeMessageType `json:"type"`
	RequestID     string               `json:"requestId"`
	Channels      []ProjectionChannel  `json:"channels"`
	ResourceKinds []ResourceKind       `json:"resourceKinds,omitempty"`
}

type SnapshotEnvelope struct {
	Type       SnapshotMessageType `json:"type"`
	RequestID  string              `json:"requestId"`
	Channel    ProjectionChannel   `json:"channel"`
	Sequence   uint64              `json:"sequence"`
	SnapshotID string              `json:"snapshotId"`
	Complete   bool                `json:"complete"`
	ServerTime string              `json:"serverTime"`
	Items      SnapshotItems       `json:"items"`
}

type ProblemEnvelope struct {
	Type      ProblemMessageType `json:"type"`
	RequestID string             `json:"requestId"`
	Code      string             `json:"code"`
	Retryable bool               `json:"retryable"`
}

type SnapshotItems struct {
	Resources            []httpgenerated.Resource            `json:"-"`
	Incidents            []httpgenerated.RuntimeIncident     `json:"-"`
	ConfigurationChanges []httpgenerated.ConfigurationChange `json:"-"`
}

func (items SnapshotItems) MarshalJSON() ([]byte, error) {
	switch {
	case items.Resources != nil && items.Incidents == nil && items.ConfigurationChanges == nil:
		for index := range items.Resources {
			if err := validateResourceProjection(items.Resources[index]); err != nil {
				return nil, err
			}
		}
		return json.Marshal(struct {
			Resources []httpgenerated.Resource `json:"resources"`
		}{Resources: items.Resources})
	case items.Resources == nil && items.Incidents != nil && items.ConfigurationChanges == nil:
		for index := range items.Incidents {
			if !items.Incidents[index].Kind.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			Incidents []httpgenerated.RuntimeIncident `json:"incidents"`
		}{Incidents: items.Incidents})
	case items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges != nil:
		for index := range items.ConfigurationChanges {
			change := items.ConfigurationChanges[index]
			if !change.Action.Valid() || !change.Outcome.Valid() || !change.ResourceKind.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			ConfigurationChanges []httpgenerated.ConfigurationChange `json:"configurationChanges"`
		}{ConfigurationChanges: items.ConfigurationChanges})
	default:
		return nil, errors.New("snapshot items must contain exactly one projection")
	}
}

func validateResourceProjection(resource httpgenerated.Resource) error {
	if !resource.Kind.Valid() || !resource.State.Valid() {
		return errors.New(errClosedEnum)
	}
	selected := 0
	validKind := false
	validateOwnership := func(ownership httpgenerated.ConfigurationOwnershipProjection) bool {
		return ownership.ManagedBy.Valid()
	}
	if value := resource.Spec.Project; value != nil {
		selected++
		validKind = resource.Kind == httpgenerated.ResourceKindPROJECT
		if !value.Locale.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.Team; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindTEAM
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.Chat; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindCHAT
		if !value.RoomType.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.Role; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindROLE
		if !value.ProviderAccountPool.Policy.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.PromptProfile; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindPROMPTPROFILE
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.CredentialBinding; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindCREDENTIALBINDING
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.RepositoryWorkspace; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindREPOSITORYWORKSPACE
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.Integration; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindINTEGRATION
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if resource.Spec.RuntimeRevision != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindRUNTIMEREVISION
	}
	if resource.Spec.Session != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindSESSION
	}
	if resource.Spec.Turn != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindTURN
	}
	if resource.Spec.ProcessRun != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindPROCESSRUN
	}
	if value := resource.Spec.Schedule; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindSCHEDULE
		if !value.TargetKind.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.OwnerGate; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindOWNERGATE
		if !value.Decision.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if resource.Spec.MemoryRecord != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindMEMORYRECORD
	}
	if resource.Spec.WorkClaim != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindWORKCLAIM
	}
	if value := resource.Spec.Artifact; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindARTIFACT
		if !value.ScanStatus.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.RoleImageRecipe; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindROLEIMAGERECIPE
		for _, item := range value.Input.Packages {
			if !item.Manager.Valid() {
				return errors.New(errClosedEnum)
			}
		}
		for _, platform := range value.Input.Platforms {
			if !platform.Os.Valid() || !platform.Architecture.Valid() {
				return errors.New(errClosedEnum)
			}
		}
	}
	if value := resource.Spec.ImageBuild; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindIMAGEBUILD
		if !value.Stage.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.ImageArtifact; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindIMAGEARTIFACT
		if value.AdmissionVerdict != nil && !value.AdmissionVerdict.Valid() {
			return errors.New(errClosedEnum)
		}
		for _, platform := range value.Platforms {
			if !platform.Os.Valid() || !platform.Architecture.Valid() {
				return errors.New(errClosedEnum)
			}
		}
	}
	if selected != 1 || !validKind {
		return errors.New("resource projection variant is invalid")
	}
	return nil
}
