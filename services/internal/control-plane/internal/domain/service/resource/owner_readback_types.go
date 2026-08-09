package resource

import (
	"time"

	domainobjectstore "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/objectstore"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

type OwnerProjectionStatus string

const (
	OwnerProjectionPresent     OwnerProjectionStatus = "PRESENT"
	OwnerProjectionUnavailable OwnerProjectionStatus = "UNAVAILABLE"
	OwnerProjectionStale       OwnerProjectionStatus = "STALE"
	OwnerProjectionIneligible  OwnerProjectionStatus = "INELIGIBLE"
)

type AgentBotIdentityProjection struct {
	Status             string
	Username           string
	MaskedStatus       string
	ProviderGeneration uint64
}

type AgentRuntimeSelectionProjection struct {
	SelectionKey, DisplayName                    string
	RoleDefinitionVersion, RuntimeProfileVersion uint64
	RoleDefinitionSHA256, RuntimeProfileSHA256   string
	Status                                       OwnerProjectionStatus
}

type AgentOwnerProjection struct {
	AgentRef, DisplayName, StableKey string
	Version                          uint64
	State                            enum.State
	Enabled                          bool
	Capabilities                     []string
	BotIdentity                      AgentBotIdentityProjection
	RuntimeSelection                 AgentRuntimeSelectionProjection
}

type OwnerRuntimeSelectionCatalogEntry struct {
	SelectionKey, DisplayName, Description       string
	RoleDefinitionVersion, RuntimeProfileVersion uint64
	RoleDefinitionSHA256, RuntimeProfileSHA256   string
	Capabilities                                 []string
	Status                                       OwnerProjectionStatus
}

type OwnerSchedulePreset struct {
	Key, DisplayName, Description, SHA256, Cron string
	Revision                                    uint64
}

type OwnerScheduleDefaults struct {
	Revision                                       uint64
	SHA256, Calendar, OverlapPolicy, MisfirePolicy string
	DeliveryPolicy, SessionPolicy                  string
	NotificationPolicy                             string
	MaximumAttempts                                uint32
	InitialBackoff, MaximumBackoff                 time.Duration
	DeadLetterAfter, MaximumExecutionDuration      time.Duration
	Coalesce                                       bool
}

type OwnerConfigurationCatalog struct {
	RuntimeSelections []OwnerRuntimeSelectionCatalogEntry
	SchedulePresets   []OwnerSchedulePreset
	ScheduleDefaults  OwnerScheduleDefaults
	NextPageToken     string
}

type OwnerSchedulePromptInput struct {
	Kind, InlineMarkdown, ArtifactName string
	Object                             domainobjectstore.Object
}

type OwnerScheduleOverrides struct {
	Cron, Calendar, OverlapPolicy, MisfirePolicy, DeliveryPolicy string
	SessionPolicy, NotificationPolicy                            string
	Interval, MisfireGrace, InitialBackoff, MaximumBackoff       time.Duration
	DeadLetterAfter, MaximumExecutionDuration                    time.Duration
	MaximumAttempts                                              uint32
	Coalesce                                                     bool
	Present                                                      map[string]bool
}

type OwnerScheduleSelection struct {
	PresetKey, Timezone, RoomStableKey string
	Prompt                             OwnerSchedulePromptInput
	Overrides                          OwnerScheduleOverrides
}

type ManageOwnerScheduleInput struct {
	Principal                                                      value.Principal
	IdempotencyKey, Action, ScheduleID, Name                       string
	AgentStableKey, InstructionSetStableKey, ProviderPoolStableKey string
	ExpectedVersion                                                uint64
	Selection                                                      OwnerScheduleSelection
}

type OwnerScheduleProjection struct {
	ScheduleRef, DisplayName                    string
	Version                                     uint64
	State                                       enum.State
	PresetKey, PresetSHA256                     string
	PresetRevision, DefaultsRevision            uint64
	DefaultsSHA256, Timezone, Cron, Calendar    string
	Interval, MisfireGrace                      time.Duration
	InitialBackoff, MaximumBackoff              time.Duration
	DeadLetterAfter, MaximumExecutionDuration   time.Duration
	DeliveryPolicy                              string
	MaximumAttempts                             uint32
	PromptKind, PromptDisplay, PromptSHA256     string
	PromptVersion                               uint64
	AdvancedOverrides                           []string
	OverlapPolicy, MisfirePolicy, SessionPolicy string
	NotificationPolicy                          string
	Coalesce                                    bool
	NextRunAt                                   time.Time
}

type OwnerDisplayValue struct {
	Status OwnerProjectionStatus
	Value  string
}

type RunOwnerProjection struct {
	RunRef, DisplayName               string
	Version                           uint64
	State                             enum.State
	Workspace, Trigger, RuntimeStatus OwnerDisplayValue
	Attempt                           uint32
	StartedAt, UpdatedAt              time.Time
	Duration                          time.Duration
	NextActions                       []string
}

type RunTimelineProjection struct {
	EventRef, Kind, Display, Outcome string
	Version                          uint64
	OccurredAt                       time.Time
	NextActions                      []string
}

type RunLineageProjection struct {
	NodeRef, ParentRef, Kind, State string
	Version                         uint64
	Attempt                         uint32
	CreatedAt, UpdatedAt            time.Time
}

type RunArtifactProjection struct {
	ArtifactRef, DisplayName, Kind, MediaType string
	SizeBytes                                 uint64
	SHA256, Status                            string
	CreatedAt                                 time.Time
}

type RuntimeIncidentOwnerProjection struct {
	IncidentRef, Kind, State, Severity, Impact string
	Version                                    uint64
	Workspace, Run                             OwnerDisplayValue
	SafeCorrelation, DiagnosticSummary         string
	RunbookURL                                 string
	OccurredAt, UpdatedAt                      time.Time
	NextActions                                []string
}

type WorkspaceRestoreOwnerProjection struct {
	RestoreRef, DisplayName, State, TerminalReasonCode string
	Version, Generation                                uint64
	Attempt, MemberCount                               uint32
	CreatedAt, UpdatedAt                               time.Time
	NextActions                                        []string
}

type ConfigurationChange struct {
	Kind, Path, Display, Before, After string
}

type OwnerConfigurationPage struct {
	Changes       []ConfigurationChange
	Truncated     bool
	NextPageToken string
}

func scheduleResourceProjection(resource entity.Resource) (OwnerScheduleProjection, bool) {
	spec, ok := resource.Spec.(entity.ScheduleSpec)
	if !ok || spec.OwnerPresetKey == "" {
		return OwnerScheduleProjection{}, false
	}
	return OwnerScheduleProjection{
		ScheduleRef: resource.ID, DisplayName: resource.Name, Version: resource.Version, State: resource.State,
		PresetKey: spec.OwnerPresetKey, PresetRevision: spec.OwnerPresetRevision, PresetSHA256: spec.OwnerPresetSHA256,
		DefaultsRevision: spec.OwnerDefaultsRevision, DefaultsSHA256: spec.OwnerDefaultsSHA256,
		Timezone: spec.Timezone, Cron: spec.Cron, Interval: spec.Interval, Calendar: spec.Calendar,
		MisfireGrace: spec.MisfireGrace, DeliveryPolicy: spec.DeliveryPolicy,
		MaximumAttempts: spec.MaximumAttempts, InitialBackoff: spec.InitialBackoff,
		MaximumBackoff: spec.MaximumBackoff, DeadLetterAfter: spec.DeadLetterAfter,
		MaximumExecutionDuration: spec.MaximumExecutionDuration, PromptKind: spec.PromptIntentKind,
		PromptDisplay: spec.PromptDisplay, PromptVersion: spec.PromptArtifactVersion, PromptSHA256: spec.PromptSHA256,
		AdvancedOverrides: append([]string(nil), spec.AdvancedOverrides...), OverlapPolicy: spec.OverlapPolicy,
		MisfirePolicy: spec.MisfirePolicy, SessionPolicy: spec.SessionPolicy,
		NotificationPolicy: spec.NotificationPolicy, Coalesce: spec.Coalesce, NextRunAt: spec.NextRunAt,
	}, true
}

// OwnerScheduleProjectionFromResource redacts server-owned pins from the basic owner readback.
func OwnerScheduleProjectionFromResource(resource entity.Resource) (OwnerScheduleProjection, bool) {
	return scheduleResourceProjection(resource)
}
