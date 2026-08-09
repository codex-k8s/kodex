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
	ProjectionChannelWorkspaceTeams       ProjectionChannel = "WORKSPACE_TEAMS"
	ProjectionChannelProviders            ProjectionChannel = "PROVIDERS"
	ProjectionChannelIntegrations         ProjectionChannel = "INTEGRATIONS"
	ProjectionChannelApprovals            ProjectionChannel = "APPROVALS"
	ProjectionChannelBackups              ProjectionChannel = "BACKUPS"
	ProjectionChannelHealth               ProjectionChannel = "HEALTH"
)

func (value ProjectionChannel) valid() bool {
	switch value {
	case ProjectionChannelRuns, ProjectionChannelIncidents,
		ProjectionChannelResources, ProjectionChannelConfigurationChanges,
		ProjectionChannelWorkspaceTeams, ProjectionChannelProviders,
		ProjectionChannelIntegrations, ProjectionChannelApprovals,
		ProjectionChannelBackups, ProjectionChannelHealth:
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
	ResourceKindRoleDefinition      ResourceKind = "ROLE_DEFINITION"
	ResourceKindAgent               ResourceKind = "AGENT"
	ResourceKindAgentAssignment     ResourceKind = "AGENT_ASSIGNMENT"
	ResourceKindInstructionSet      ResourceKind = "INSTRUCTION_SET"
	ResourceKindProviderConnection  ResourceKind = "PROVIDER_CONNECTION_REFERENCE"
	ResourceKindProviderPool        ResourceKind = "PROVIDER_POOL"
	ResourceKindWorkspaceBackup     ResourceKind = "WORKSPACE_BACKUP"
	ResourceKindWorkspaceRestore    ResourceKind = "WORKSPACE_RESTORE"
	ResourceKindWorkspaceMapping    ResourceKind = "WORKSPACE_MATTERMOST_MAPPING"
)

func (value ResourceKind) valid() bool {
	switch value {
	case ResourceKindProject, ResourceKindTeam, ResourceKindChat, ResourceKindRole,
		ResourceKindPromptProfile, ResourceKindCredentialBinding, ResourceKindRepositoryWorkspace,
		ResourceKindIntegration, ResourceKindRuntimeRevision, ResourceKindSession, ResourceKindTurn,
		ResourceKindProcessRun, ResourceKindSchedule, ResourceKindOwnerGate, ResourceKindMemoryRecord,
		ResourceKindWorkClaim, ResourceKindArtifact, ResourceKindRoleImageRecipe,
		ResourceKindImageBuild, ResourceKindImageArtifact, ResourceKindRoleDefinition,
		ResourceKindAgent, ResourceKindAgentAssignment, ResourceKindInstructionSet,
		ResourceKindProviderConnection, ResourceKindProviderPool, ResourceKindWorkspaceBackup,
		ResourceKindWorkspaceRestore, ResourceKindWorkspaceMapping:
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
	Runs                 []httpgenerated.RunView                  `json:"-"`
	Resources            []httpgenerated.Resource                 `json:"-"`
	Incidents            []httpgenerated.IncidentView             `json:"-"`
	ConfigurationChanges []httpgenerated.ConfigurationChange      `json:"-"`
	Teams                []httpgenerated.MattermostTeam           `json:"-"`
	ProviderConnections  []httpgenerated.ProviderConnection       `json:"-"`
	IntegrationConfigs   []httpgenerated.IntegrationConfiguration `json:"-"`
	Approvals            []httpgenerated.IntegrationApproval      `json:"-"`
	Health               []httpgenerated.HealthObservation        `json:"-"`
}

func (items SnapshotItems) MarshalJSON() ([]byte, error) {
	switch {
	case items.Runs != nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.Runs {
			if !items.Runs[index].State.Valid() || items.Runs[index].Version < 1 {
				return nil, errors.New(errClosedEnum)
			}
			for _, action := range items.Runs[index].NextActions {
				if !action.Valid() {
					return nil, errors.New(errClosedEnum)
				}
			}
		}
		return json.Marshal(struct {
			Runs []httpgenerated.RunView `json:"runs"`
		}{Runs: items.Runs})
	case items.Runs == nil && items.Resources != nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.Resources {
			if err := validateResourceProjection(items.Resources[index]); err != nil {
				return nil, err
			}
		}
		return json.Marshal(struct {
			Resources []httpgenerated.Resource `json:"resources"`
		}{Resources: items.Resources})
	case items.Runs == nil && items.Resources == nil && items.Incidents != nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.Incidents {
			if !items.Incidents[index].Kind.Valid() || !items.Incidents[index].State.Valid() || !items.Incidents[index].Severity.Valid() || items.Incidents[index].Version < 1 {
				return nil, errors.New(errClosedEnum)
			}
			for _, action := range items.Incidents[index].NextActions {
				if !action.Valid() {
					return nil, errors.New(errClosedEnum)
				}
			}
		}
		return json.Marshal(struct {
			Incidents []httpgenerated.IncidentView `json:"incidents"`
		}{Incidents: items.Incidents})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges != nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.ConfigurationChanges {
			change := items.ConfigurationChanges[index]
			if !change.Action.Valid() || !change.Outcome.Valid() || !change.ResourceKind.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			ConfigurationChanges []httpgenerated.ConfigurationChange `json:"configurationChanges"`
		}{ConfigurationChanges: items.ConfigurationChanges})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams != nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.Teams {
			if !items.Teams[index].Status.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			Teams []httpgenerated.MattermostTeam `json:"teams"`
		}{Teams: items.Teams})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections != nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health == nil:
		for index := range items.ProviderConnections {
			if !items.ProviderConnections[index].State.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			ProviderConnections []httpgenerated.ProviderConnection `json:"providerConnections"`
		}{ProviderConnections: items.ProviderConnections})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs != nil && items.Approvals == nil && items.Health == nil:
		for index := range items.IntegrationConfigs {
			if !items.IntegrationConfigs[index].EffectKind.Valid() || !items.IntegrationConfigs[index].State.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			IntegrationConfigurations []httpgenerated.IntegrationConfiguration `json:"integrationConfigurations"`
		}{IntegrationConfigurations: items.IntegrationConfigs})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals != nil && items.Health == nil:
		for index := range items.Approvals {
			if !items.Approvals[index].Status.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			Approvals []httpgenerated.IntegrationApproval `json:"approvals"`
		}{Approvals: items.Approvals})
	case items.Runs == nil && items.Resources == nil && items.Incidents == nil && items.ConfigurationChanges == nil && items.Teams == nil && items.ProviderConnections == nil && items.IntegrationConfigs == nil && items.Approvals == nil && items.Health != nil:
		for index := range items.Health {
			if !items.Health[index].Source.Valid() || !items.Health[index].Status.Valid() {
				return nil, errors.New(errClosedEnum)
			}
		}
		return json.Marshal(struct {
			Health []httpgenerated.HealthObservation `json:"health"`
		}{Health: items.Health})
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
		if !value.TargetKind.Valid() || !value.Calendar.Valid() || !value.OverlapPolicy.Valid() || !value.MisfirePolicy.Valid() ||
			!value.DeliveryPolicy.Valid() || !value.SessionPolicy.Valid() || !value.NotificationPolicy.Valid() || !value.TargetType.Valid() ||
			!validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.OwnerGate; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindOWNERGATE
		if !value.Decision.Valid() || !value.DeliveryState.Valid() || !value.NextAction.Valid() {
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
	if value := resource.Spec.RoleDefinition; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindROLEDEFINITION
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.Agent; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindAGENT
		if !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if resource.Spec.AgentAssignment != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindAGENTASSIGNMENT
	}
	if value := resource.Spec.InstructionSet; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindINSTRUCTIONSET
		if !value.VersionState.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.ProviderConnectionReference; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindPROVIDERCONNECTIONREFERENCE
		if !value.MaskedStatus.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.ProviderPool; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindPROVIDERPOOL
		if !value.Policy.Valid() || !validateOwnership(value.Ownership) {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.WorkspaceBackup; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindWORKSPACEBACKUP
		if !value.Scope.Valid() || !value.State.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.WorkspaceRestore; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindWORKSPACERESTORE
		if !value.State.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if value := resource.Spec.WorkspaceMattermostMapping; value != nil {
		selected++
		validKind = validKind || resource.Kind == httpgenerated.ResourceKindWORKSPACEMATTERMOSTMAPPING
		if !value.State.Valid() {
			return errors.New(errClosedEnum)
		}
	}
	if selected != 1 || !validKind {
		return errors.New("resource projection variant is invalid")
	}
	return nil
}
