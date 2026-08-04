package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

var permissionPattern = regexp.MustCompile(
	`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`,
)

// Spec — закрытый service-owned payload одного ResourceKind.
type Spec interface {
	Kind() enum.Kind
	Validate() error
}

// ConfigurationOwnership фиксирует единственный UI либо Git источник истины.
type ConfigurationOwnership struct {
	ManagedBy      string `json:"managedBy"`
	SourceRef      string `json:"sourceRef,omitempty"`
	SourceRevision uint64 `json:"sourceRevision,omitempty"`
}

func (ownership ConfigurationOwnership) Validate() error {
	switch ownership.ManagedBy {
	case "UI":
		if (ownership.SourceRef == "") != (ownership.SourceRevision == 0) ||
			(ownership.SourceRef != "" && (!validExternalRef(ownership.SourceRef) || ownership.SourceRevision == 0)) {
			return errors.New("UI configuration ownership is invalid")
		}
	case "GIT":
		if !validExternalRef(ownership.SourceRef) ||
			ownership.SourceRevision == 0 {
			return errors.New("git configuration ownership is invalid")
		}
	default:
		return errors.New("configuration ownership is invalid")
	}
	return nil
}

// ConfiguredSpec — управляемая из UI либо Git конфигурация.
type ConfiguredSpec interface {
	Spec
	ConfigurationOwnership() ConfigurationOwnership
}

// WithConfigurationOwnership возвращает копию только поддерживаемой конфигурации.
func WithConfigurationOwnership(
	spec Spec,
	ownership ConfigurationOwnership,
) (Spec, error) {
	if ownership.Validate() != nil {
		return nil, errors.New("configuration ownership is invalid")
	}
	switch value := spec.(type) {
	case ProjectSpec:
		value.Ownership = ownership
		return value, nil
	case TeamSpec:
		value.Ownership = ownership
		return value, nil
	case ChatSpec:
		value.Ownership = ownership
		return value, nil
	case RoleSpec:
		value.Ownership = ownership
		return value, nil
	case PromptProfileSpec:
		value.Ownership = ownership
		return value, nil
	case CredentialBindingSpec:
		value.Ownership = ownership
		return value, nil
	case RepositoryWorkspaceSpec:
		value.Ownership = ownership
		return value, nil
	case IntegrationSpec:
		value.Ownership = ownership
		return value, nil
	case ScheduleSpec:
		value.Ownership = ownership
		return value, nil
	default:
		return nil, errors.New("resource is not managed configuration")
	}
}

type ProjectSpec struct {
	Slug        string                 `json:"slug"`
	Description string                 `json:"description"`
	Locale      string                 `json:"locale"`
	Ownership   ConfigurationOwnership `json:"ownership"`
}

func (ProjectSpec) Kind() enum.Kind { return enum.KindProject }
func (spec ProjectSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec ProjectSpec) Validate() error {
	if value.ValidateStableKey(spec.Slug) != nil ||
		len(spec.Description) > 4096 ||
		(spec.Locale != "ru" && spec.Locale != "en") ||
		spec.Ownership.Validate() != nil {
		return errors.New("project specification is invalid")
	}
	return nil
}

type TeamSpec struct {
	StableKey       string                 `json:"stableKey"`
	ExternalTeamRef string                 `json:"externalTeamRef"`
	MemberActorIDs  []string               `json:"memberActorIds"`
	RoleIDs         []string               `json:"roleIds"`
	Ownership       ConfigurationOwnership `json:"ownership"`
}

func (TeamSpec) Kind() enum.Kind { return enum.KindTeam }
func (spec TeamSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec TeamSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		!validExternalRef(spec.ExternalTeamRef) ||
		len(spec.MemberActorIDs) > 1000 ||
		len(spec.RoleIDs) < 1 || len(spec.RoleIDs) > 64 ||
		spec.Ownership.Validate() != nil {
		return errors.New("team specification is invalid")
	}
	if !validUniqueIDs(spec.MemberActorIDs) || !validUniqueIDs(spec.RoleIDs) {
		return errors.New("team membership is invalid")
	}
	return nil
}

type ChatSpec struct {
	StableKey          string                 `json:"stableKey"`
	RoomType           string                 `json:"roomType"`
	DefaultAgentID     string                 `json:"defaultAgentId,omitempty"`
	ExternalChannelRef string                 `json:"externalChannelRef"`
	WorkPolicy         string                 `json:"workPolicy"`
	Ownership          ConfigurationOwnership `json:"ownership"`
}

func (ChatSpec) Kind() enum.Kind { return enum.KindChat }
func (spec ChatSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec ChatSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		(spec.RoomType != "USER" && spec.RoomType != "COORDINATION" &&
			spec.RoomType != "WORK_CONTROL" && spec.RoomType != "RUNS") ||
		!validExternalRef(spec.ExternalChannelRef) ||
		len(spec.WorkPolicy) > 8192 ||
		spec.Ownership.Validate() != nil {
		return errors.New("chat specification is invalid")
	}
	if spec.DefaultAgentID != "" && value.ValidateID(spec.DefaultAgentID) != nil {
		return errors.New("chat default agent is invalid")
	}
	return nil
}

type RoleSpec struct {
	StableKey                    string                 `json:"stableKey"`
	Capabilities                 []string               `json:"capabilities"`
	AllowedTargetRoleIDs         []string               `json:"allowedTargetRoleIds"`
	PromptProfileID              string                 `json:"promptProfileId"`
	ProviderCredentialBindingIDs []string               `json:"providerCredentialBindingIds"`
	RepositoryWorkspaceIDs       []string               `json:"repositoryWorkspaceIds"`
	IntegrationIDs               []string               `json:"integrationIds"`
	ProviderAccountPool          ProviderAccountPool    `json:"providerAccountPool"`
	Ownership                    ConfigurationOwnership `json:"ownership"`
}

// ProviderAccountPool задаёт закрытую серверную политику выбора учётной записи.
type ProviderAccountPool struct {
	Policy            string                       `json:"policy"`
	PolicyRevision    uint64                       `json:"policyRevision"`
	ObservationMaxAge time.Duration                `json:"observationMaxAge"`
	Bindings          []ProviderAccountPoolBinding `json:"bindings"`
}

// ProviderAccountPoolBinding связывает привязку с весом, а не с секретом.
type ProviderAccountPoolBinding struct {
	CredentialBindingID string `json:"credentialBindingId"`
	Weight              uint32 `json:"weight"`
}

func (pool ProviderAccountPool) Validate(allowed []string) error {
	if (pool.Policy != "least_used" && pool.Policy != "weighted") ||
		pool.PolicyRevision == 0 ||
		pool.ObservationMaxAge < time.Minute ||
		pool.ObservationMaxAge > 24*time.Hour ||
		len(pool.Bindings) != len(allowed) || len(pool.Bindings) == 0 {
		return errors.New("provider account pool is invalid")
	}
	seen := make(map[string]struct{}, len(pool.Bindings))
	for _, binding := range pool.Bindings {
		if value.ValidateID(binding.CredentialBindingID) != nil ||
			binding.Weight == 0 || binding.Weight > 10000 ||
			!slices.Contains(allowed, binding.CredentialBindingID) {
			return errors.New("provider account pool binding is invalid")
		}
		if _, exists := seen[binding.CredentialBindingID]; exists {
			return errors.New("provider account pool binding is duplicated")
		}
		seen[binding.CredentialBindingID] = struct{}{}
	}
	return nil
}

func (RoleSpec) Kind() enum.Kind { return enum.KindRole }
func (spec RoleSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec RoleSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		value.ValidateID(spec.PromptProfileID) != nil ||
		!validPermissions(spec.Capabilities, 64) ||
		len(spec.AllowedTargetRoleIDs) > 64 ||
		len(spec.ProviderCredentialBindingIDs) < 1 ||
		len(spec.ProviderCredentialBindingIDs) > 8 ||
		len(spec.RepositoryWorkspaceIDs) > 32 ||
		len(spec.IntegrationIDs) > 32 ||
		spec.ProviderAccountPool.Validate(spec.ProviderCredentialBindingIDs) != nil ||
		spec.Ownership.Validate() != nil {
		return errors.New("role specification is invalid")
	}
	for _, identifiers := range [][]string{
		spec.AllowedTargetRoleIDs,
		spec.ProviderCredentialBindingIDs,
		spec.RepositoryWorkspaceIDs,
		spec.IntegrationIDs,
	} {
		if !validUniqueIDs(identifiers) {
			return errors.New("role relationship is invalid")
		}
	}
	return nil
}

type PromptProfileSpec struct {
	Revision      uint64                 `json:"revision"`
	ContentSHA256 string                 `json:"contentSha256"`
	SourceRef     string                 `json:"sourceRef"`
	Locale        string                 `json:"locale"`
	Ownership     ConfigurationOwnership `json:"ownership"`
}

func (PromptProfileSpec) Kind() enum.Kind { return enum.KindPromptProfile }
func (spec PromptProfileSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec PromptProfileSpec) Validate() error {
	if spec.Revision == 0 || !validSHA256(spec.ContentSHA256) ||
		!validExternalRef(spec.SourceRef) ||
		(spec.Locale != "ru" && spec.Locale != "en") ||
		spec.Ownership.Validate() != nil {
		return errors.New("prompt profile specification is invalid")
	}
	return nil
}

type CredentialBindingSpec struct {
	Purpose                     string                 `json:"purpose"`
	SecretRef                   string                 `json:"secretRef"`
	PrincipalRef                string                 `json:"principalRef"`
	Revision                    uint64                 `json:"revision"`
	ExpiresAt                   time.Time              `json:"expiresAt,omitempty"`
	ProviderEligible            bool                   `json:"providerEligible"`
	ProviderCapabilities        []string               `json:"providerCapabilities,omitempty"`
	ProviderObservedUsage       uint64                 `json:"providerObservedUsage,omitempty"`
	ProviderObservedLimit       uint64                 `json:"providerObservedLimit,omitempty"`
	ProviderObservationRevision uint64                 `json:"providerObservationRevision,omitempty"`
	ProviderObservedAt          time.Time              `json:"providerObservedAt,omitempty"`
	ImmutableSecretRef          string                 `json:"immutableSecretRef"`
	ProviderContentVersion      string                 `json:"providerContentVersion"`
	ContentSHA256               string                 `json:"contentSha256"`
	Ownership                   ConfigurationOwnership `json:"ownership"`
}

func (CredentialBindingSpec) Kind() enum.Kind { return enum.KindCredentialBinding }
func (spec CredentialBindingSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec CredentialBindingSpec) Validate() error {
	if value.ValidateStableKey(spec.Purpose) != nil ||
		!validSecretRef(spec.SecretRef) ||
		!validImmutableSecretRef(spec.ImmutableSecretRef) ||
		!validExternalRef(spec.PrincipalRef) ||
		!validExternalRef(spec.ProviderContentVersion) ||
		!validSHA256(spec.ContentSHA256) ||
		spec.Revision == 0 || spec.Ownership.Validate() != nil {
		return errors.New("credential binding specification is invalid")
	}
	if spec.Purpose == "provider-account" {
		if !spec.ProviderEligible ||
			!validBoundedKeys(spec.ProviderCapabilities, 64) ||
			spec.ProviderObservedLimit == 0 ||
			spec.ProviderObservedUsage > spec.ProviderObservedLimit ||
			spec.ProviderObservationRevision == 0 ||
			spec.ProviderObservedAt.IsZero() {
			return errors.New("provider account observation is invalid")
		}
	} else if spec.ProviderEligible || len(spec.ProviderCapabilities) != 0 ||
		spec.ProviderObservedUsage != 0 || spec.ProviderObservedLimit != 0 ||
		spec.ProviderObservationRevision != 0 || !spec.ProviderObservedAt.IsZero() {
		return errors.New("non-provider observation is forbidden")
	}
	return nil
}

type RepositoryWorkspaceSpec struct {
	RepositoryRef       string                 `json:"repositoryRef"`
	WorkspaceMode       string                 `json:"workspaceMode"`
	DefaultBranch       string                 `json:"defaultBranch"`
	CredentialBindingID string                 `json:"credentialBindingId,omitempty"`
	Ownership           ConfigurationOwnership `json:"ownership"`
}

func (RepositoryWorkspaceSpec) Kind() enum.Kind { return enum.KindRepositoryWorkspace }
func (spec RepositoryWorkspaceSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec RepositoryWorkspaceSpec) Validate() error {
	if (spec.WorkspaceMode != "NONE" && spec.WorkspaceMode != "GIT") ||
		spec.Ownership.Validate() != nil {
		return errors.New("repository workspace mode is invalid")
	}
	if spec.WorkspaceMode == "GIT" {
		parsed, err := url.Parse(spec.RepositoryRef)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			len(spec.DefaultBranch) < 1 || len(spec.DefaultBranch) > 255 {
			return errors.New("repository workspace specification is invalid")
		}
	}
	if spec.CredentialBindingID != "" &&
		value.ValidateID(spec.CredentialBindingID) != nil {
		return errors.New("repository credential binding is invalid")
	}
	return nil
}

type IntegrationSpec struct {
	DefinitionRef        string                 `json:"definitionRef"`
	DefinitionVersion    uint64                 `json:"definitionVersion"`
	Capabilities         []string               `json:"capabilities"`
	CredentialBindingIDs []string               `json:"credentialBindingIds"`
	EndpointRef          string                 `json:"endpointRef"`
	Ownership            ConfigurationOwnership `json:"ownership"`
}

func (IntegrationSpec) Kind() enum.Kind { return enum.KindIntegration }
func (spec IntegrationSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec IntegrationSpec) Validate() error {
	if !validExternalRef(spec.DefinitionRef) || spec.DefinitionVersion == 0 ||
		!validBoundedKeys(spec.Capabilities, 128) ||
		len(spec.CredentialBindingIDs) > 32 ||
		!validUniqueIDs(spec.CredentialBindingIDs) ||
		!validExternalRef(spec.EndpointRef) ||
		spec.Ownership.Validate() != nil {
		return errors.New("integration specification is invalid")
	}
	for _, identifier := range spec.CredentialBindingIDs {
		if value.ValidateID(identifier) != nil {
			return errors.New("integration credential binding is invalid")
		}
	}
	return nil
}

type RuntimeRevisionSpec struct {
	ManifestSHA256              string                 `json:"manifestSha256"`
	ImageDigest                 string                 `json:"imageDigest"`
	PromptProfileID             string                 `json:"promptProfileId"`
	PromptRevision              uint64                 `json:"promptRevision"`
	CredentialBindingIDs        []string               `json:"credentialBindingIds"`
	IntegrationIDs              []string               `json:"integrationIds"`
	PredecessorRevisionID       string                 `json:"predecessorRevisionId,omitempty"`
	AuthorityPolicyVersion      uint64                 `json:"authorityPolicyRevision"`
	AuthorityPolicySHA256       string                 `json:"authorityPolicySha256"`
	Components                  []EffectiveResourceRef `json:"components"`
	CreatedAt                   time.Time              `json:"createdAt"`
	SessionID                   string                 `json:"sessionId"`
	RoleID                      string                 `json:"roleId"`
	ChatID                      string                 `json:"chatId,omitempty"`
	ProviderCredentialBindingID string                 `json:"providerCredentialBindingId"`
	EffectiveRuntimeSHA256      string                 `json:"effectiveRuntimeSha256"`
}

func (RuntimeRevisionSpec) Kind() enum.Kind { return enum.KindRuntimeRevision }
func (spec RuntimeRevisionSpec) Validate() error {
	if !validSHA256(spec.ManifestSHA256) ||
		!strings.HasPrefix(spec.ImageDigest, "sha256:") ||
		!validSHA256(strings.TrimPrefix(spec.ImageDigest, "sha256:")) ||
		value.ValidateID(spec.PromptProfileID) != nil ||
		spec.PromptRevision == 0 ||
		len(spec.CredentialBindingIDs) > 64 || len(spec.IntegrationIDs) > 64 ||
		!validUniqueIDs(spec.CredentialBindingIDs) ||
		!validUniqueIDs(spec.IntegrationIDs) ||
		spec.AuthorityPolicyVersion == 0 ||
		!validSHA256(spec.AuthorityPolicySHA256) ||
		len(spec.Components) < 5 || len(spec.Components) > 256 ||
		spec.CreatedAt.IsZero() ||
		value.ValidateID(spec.SessionID) != nil ||
		value.ValidateID(spec.RoleID) != nil ||
		value.ValidateID(spec.ProviderCredentialBindingID) != nil ||
		!validSHA256(spec.EffectiveRuntimeSHA256) ||
		(spec.ChatID != "" && value.ValidateID(spec.ChatID) != nil) {
		return errors.New("runtime revision specification is invalid")
	}
	if spec.PredecessorRevisionID != "" &&
		value.ValidateID(spec.PredecessorRevisionID) != nil {
		return errors.New("runtime revision predecessor is invalid")
	}
	for _, identifiers := range [][]string{spec.CredentialBindingIDs, spec.IntegrationIDs} {
		for _, identifier := range identifiers {
			if value.ValidateID(identifier) != nil {
				return errors.New("runtime revision binding is invalid")
			}
		}
	}
	seen := make(map[string]struct{}, len(spec.Components))
	for _, component := range spec.Components {
		if err := component.Validate(); err != nil {
			return err
		}
		key := string(component.Kind) + "\x00" + component.ResourceID
		if _, duplicate := seen[key]; duplicate {
			return errors.New("runtime revision component is duplicated")
		}
		seen[key] = struct{}{}
	}
	return nil
}

type EffectiveResourceRef struct {
	Kind             enum.Kind `json:"kind"`
	ResourceID       string    `json:"resourceId"`
	Version          uint64    `json:"version"`
	ProjectionSHA256 string    `json:"projectionSha256"`
}

func (reference EffectiveResourceRef) Validate() error {
	if !reference.Kind.Valid() ||
		value.ValidateID(reference.ResourceID) != nil ||
		reference.Version == 0 ||
		!validSHA256(reference.ProjectionSHA256) {
		return errors.New("runtime revision component is invalid")
	}
	return nil
}

type SessionSpec struct {
	AgentID                    string `json:"agentId"`
	ProviderAccountBindingID   string `json:"providerAccountBindingId"`
	ConversationID             string `json:"conversationId,omitempty"`
	ArchiveRef                 string `json:"archiveRef,omitempty"`
	LastTurnSequence           uint64 `json:"lastTurnSequence"`
	AgentSessionKey            string `json:"agentSessionKey,omitempty"`
	AgentSessionID             int64  `json:"agentSessionId,omitempty"`
	AgentSessionBindingVersion uint64 `json:"agentSessionBindingVersion,omitempty"`
	AgentSessionBindingSHA256  string `json:"agentSessionBindingSha256,omitempty"`
}

func (SessionSpec) Kind() enum.Kind { return enum.KindSession }
func (spec SessionSpec) Validate() error {
	if value.ValidateID(spec.AgentID) != nil ||
		value.ValidateID(spec.ProviderAccountBindingID) != nil {
		return errors.New("session specification is invalid")
	}
	for _, identifier := range []string{spec.ConversationID} {
		if identifier != "" && value.ValidateID(identifier) != nil {
			return errors.New("session binding is invalid")
		}
	}
	if spec.ArchiveRef != "" && !validExternalRef(spec.ArchiveRef) {
		return errors.New("session archive reference is invalid")
	}
	agentBinding := spec.AgentSessionKey != "" || spec.AgentSessionID != 0 ||
		spec.AgentSessionBindingVersion != 0 || spec.AgentSessionBindingSHA256 != ""
	if agentBinding && (len(spec.AgentSessionKey) < 1 || len(spec.AgentSessionKey) > 256 ||
		spec.AgentSessionID <= 0 || spec.AgentSessionBindingVersion == 0 ||
		!validSHA256(spec.AgentSessionBindingSHA256)) {
		return errors.New("agent session binding is invalid")
	}
	return nil
}

type TurnSpec struct {
	SessionID               string `json:"sessionId"`
	Sequence                uint64 `json:"sequence"`
	SourceRef               string `json:"sourceRef"`
	PromptArtifactID        string `json:"promptArtifactId"`
	RuntimeRevisionID       string `json:"runtimeRevisionId"`
	ProcessRunID            string `json:"processRunId,omitempty"`
	Attempt                 uint32 `json:"attempt"`
	Outcome                 string `json:"outcome,omitempty"`
	ResultArtifactID        string `json:"resultArtifactId,omitempty"`
	EffectiveInputSHA256    string `json:"effectiveInputSha256"`
	PredecessorTurnID       string `json:"predecessorTurnId,omitempty"`
	OwnerFeedback           string `json:"ownerFeedback,omitempty"`
	OwnerFeedbackGateID     string `json:"ownerFeedbackGateId,omitempty"`
	OwnerFeedbackVersion    uint64 `json:"ownerFeedbackGateVersion,omitempty"`
	OwnerFeedbackSHA256     string `json:"ownerFeedbackSha256,omitempty"`
	AgentSessionTurnID      int64  `json:"agentSessionTurnId,omitempty"`
	AgentRunID              string `json:"agentRunId,omitempty"`
	AgentTurnBindingVersion uint64 `json:"agentTurnBindingVersion,omitempty"`
	AgentTurnBindingSHA256  string `json:"agentTurnBindingSha256,omitempty"`
}

func (TurnSpec) Kind() enum.Kind { return enum.KindTurn }
func (spec TurnSpec) Validate() error {
	if value.ValidateID(spec.SessionID) != nil || spec.Sequence == 0 ||
		!validExternalRef(spec.SourceRef) ||
		value.ValidateID(spec.PromptArtifactID) != nil ||
		value.ValidateID(spec.RuntimeRevisionID) != nil ||
		spec.Attempt == 0 || spec.Attempt > 100 ||
		len(spec.Outcome) > 256 ||
		!validSHA256(spec.EffectiveInputSHA256) {
		return errors.New("turn specification is invalid")
	}
	for _, identifier := range []string{
		spec.ProcessRunID,
		spec.ResultArtifactID,
		spec.PredecessorTurnID,
		spec.OwnerFeedbackGateID,
	} {
		if identifier != "" && value.ValidateID(identifier) != nil {
			return errors.New("turn binding is invalid")
		}
	}
	feedback := spec.OwnerFeedback != "" || spec.OwnerFeedbackGateID != "" ||
		spec.OwnerFeedbackVersion != 0 || spec.OwnerFeedbackSHA256 != ""
	if feedback && (len(spec.OwnerFeedback) < 1 || len(spec.OwnerFeedback) > 2048 ||
		spec.OwnerFeedbackGateID == "" || spec.OwnerFeedbackVersion == 0 ||
		!validSHA256(spec.OwnerFeedbackSHA256) ||
		digestText(spec.OwnerFeedback) != spec.OwnerFeedbackSHA256) {
		return errors.New("turn owner feedback binding is invalid")
	}
	agentBinding := spec.AgentSessionTurnID != 0 || spec.AgentRunID != "" ||
		spec.AgentTurnBindingVersion != 0 || spec.AgentTurnBindingSHA256 != ""
	if agentBinding && (spec.AgentSessionTurnID <= 0 || len(spec.AgentRunID) < 1 ||
		len(spec.AgentRunID) > 256 || spec.AgentTurnBindingVersion == 0 ||
		!validSHA256(spec.AgentTurnBindingSHA256)) {
		return errors.New("agent turn binding is invalid")
	}
	return nil
}

type ProcessRunSpec struct {
	ParentProcessRunID                 string                       `json:"parentProcessRunId,omitempty"`
	PlaybookRef                        string                       `json:"playbookRef"`
	PolicyRevision                     uint64                       `json:"policyRevision"`
	RootTriggerRef                     string                       `json:"rootTriggerRef"`
	ResultArtifactID                   string                       `json:"resultArtifactId,omitempty"`
	RootInitiatorActorID               string                       `json:"rootInitiatorActorId"`
	RootSessionID                      string                       `json:"rootSessionId"`
	RootSessionVersion                 uint64                       `json:"rootSessionVersion"`
	RootTurnID                         string                       `json:"rootTurnId"`
	RootTurnVersion                    uint64                       `json:"rootTurnVersion"`
	RootAttempt                        uint32                       `json:"rootAttempt"`
	ImmutableInputSHA256               string                       `json:"immutableInputSha256"`
	RuntimeRevisionID                  string                       `json:"runtimeRevisionId"`
	LaunchingProcessRunID              string                       `json:"launchingProcessRunId,omitempty"`
	LaunchingTurnID                    string                       `json:"launchingTurnId,omitempty"`
	LaunchingAttempt                   uint32                       `json:"launchingAttempt,omitempty"`
	DelegationID                       string                       `json:"delegationId,omitempty"`
	TargetSessionID                    string                       `json:"targetSessionId,omitempty"`
	TargetSessionVersion               uint64                       `json:"targetSessionVersion,omitempty"`
	TargetTurnID                       string                       `json:"targetTurnId,omitempty"`
	TargetTurnVersion                  uint64                       `json:"targetTurnVersion,omitempty"`
	TargetAttempt                      uint32                       `json:"targetAttempt,omitempty"`
	Outcome                            string                       `json:"outcome,omitempty"`
	ScheduleID                         string                       `json:"scheduleId,omitempty"`
	OccurrenceID                       string                       `json:"occurrenceId,omitempty"`
	ContinuationGateID                 string                       `json:"continuationGateId,omitempty"`
	ContinuationTurnID                 string                       `json:"continuationTurnId,omitempty"`
	ContinuationTurnVersion            uint64                       `json:"continuationTurnVersion,omitempty"`
	ContinuationAttempt                uint32                       `json:"continuationAttempt,omitempty"`
	ContinuationRuntimeRevisionID      string                       `json:"continuationRuntimeRevisionId,omitempty"`
	ContinuationRuntimeRevisionVersion uint64                       `json:"continuationRuntimeRevisionVersion,omitempty"`
	ContinuationInputSHA256            string                       `json:"continuationInputSha256,omitempty"`
	ContinuationKind                   enum.ProcessContinuationKind `json:"continuationKind,omitempty"`
	ContinuationIntegrationID          string                       `json:"continuationIntegrationId,omitempty"`
	ContinuationOutcomeSHA256          string                       `json:"continuationOutcomeSha256,omitempty"`
	OwnerFeedbackSHA256                string                       `json:"ownerFeedbackSha256,omitempty"`
	CurrentSessionID                   string                       `json:"currentSessionId,omitempty"`
	CurrentSessionVersion              uint64                       `json:"currentSessionVersion,omitempty"`
	CurrentTurnID                      string                       `json:"currentTurnId,omitempty"`
	CurrentTurnVersion                 uint64                       `json:"currentTurnVersion,omitempty"`
	CurrentAttempt                     uint32                       `json:"currentAttempt,omitempty"`
	CurrentRuntimeRevisionID           string                       `json:"currentRuntimeRevisionId,omitempty"`
	CurrentRuntimeRevisionVersion      uint64                       `json:"currentRuntimeRevisionVersion,omitempty"`
	CurrentInputSHA256                 string                       `json:"currentInputSha256,omitempty"`
}

// ProcessContinuationBinding задаёт общую server-owned координату одного
// активного arm закрытого ProcessRun continuation union.
type ProcessContinuationBinding struct {
	TurnID                 string
	TurnVersion            uint64
	Attempt                uint32
	RuntimeRevisionID      string
	RuntimeRevisionVersion uint64
	InputSHA256            string
}

func (binding ProcessContinuationBinding) validate() error {
	if value.ValidateID(binding.TurnID) != nil || binding.TurnVersion == 0 ||
		binding.Attempt == 0 || binding.Attempt > 100 ||
		value.ValidateID(binding.RuntimeRevisionID) != nil ||
		binding.RuntimeRevisionVersion == 0 || !validSHA256(binding.InputSHA256) {
		return errors.New("process continuation binding is invalid")
	}
	return nil
}

// SetOwnerGateContinuation атомарно переключает union на OWNER_GATE и удаляет
// все поля INTEGRATION arm. История прежнего arm остаётся в owner aggregate и audit.
func (spec *ProcessRunSpec) SetOwnerGateContinuation(
	binding ProcessContinuationBinding,
	gateID, feedbackSHA256 string,
) error {
	if binding.validate() != nil || value.ValidateID(gateID) != nil ||
		!validSHA256(feedbackSHA256) {
		return errors.New("owner gate continuation binding is invalid")
	}
	spec.clearContinuation()
	spec.ContinuationKind = enum.ProcessContinuationOwnerGate
	spec.ContinuationGateID = gateID
	spec.OwnerFeedbackSHA256 = feedbackSHA256
	spec.setContinuationBinding(binding)
	return nil
}

// SetIntegrationContinuation атомарно переключает union на INTEGRATION и
// удаляет все поля OWNER_GATE arm.
func (spec *ProcessRunSpec) SetIntegrationContinuation(
	binding ProcessContinuationBinding,
	integrationContinuationID, outcomeSHA256 string,
) error {
	if binding.validate() != nil || value.ValidateID(integrationContinuationID) != nil ||
		!validSHA256(outcomeSHA256) {
		return errors.New("integration continuation binding is invalid")
	}
	spec.clearContinuation()
	spec.ContinuationKind = enum.ProcessContinuationIntegration
	spec.ContinuationIntegrationID = integrationContinuationID
	spec.ContinuationOutcomeSHA256 = outcomeSHA256
	spec.setContinuationBinding(binding)
	return nil
}

// ClearContinuation завершает активный continuation arm. Provenance не
// переносится между arms и сохраняется только в исходном owner aggregate/audit.
func (spec *ProcessRunSpec) ClearContinuation() {
	spec.clearContinuation()
}

func (spec *ProcessRunSpec) setContinuationBinding(binding ProcessContinuationBinding) {
	spec.ContinuationTurnID = binding.TurnID
	spec.ContinuationTurnVersion = binding.TurnVersion
	spec.ContinuationAttempt = binding.Attempt
	spec.ContinuationRuntimeRevisionID = binding.RuntimeRevisionID
	spec.ContinuationRuntimeRevisionVersion = binding.RuntimeRevisionVersion
	spec.ContinuationInputSHA256 = binding.InputSHA256
}

func (spec *ProcessRunSpec) clearContinuation() {
	spec.ContinuationKind = enum.ProcessContinuationNone
	spec.ContinuationGateID = ""
	spec.ContinuationIntegrationID = ""
	spec.ContinuationOutcomeSHA256 = ""
	spec.OwnerFeedbackSHA256 = ""
	spec.ContinuationTurnID = ""
	spec.ContinuationTurnVersion = 0
	spec.ContinuationAttempt = 0
	spec.ContinuationRuntimeRevisionID = ""
	spec.ContinuationRuntimeRevisionVersion = 0
	spec.ContinuationInputSHA256 = ""
}

func (ProcessRunSpec) Kind() enum.Kind { return enum.KindProcessRun }
func (spec ProcessRunSpec) Validate() error {
	if !validExternalRef(spec.PlaybookRef) || spec.PolicyRevision == 0 ||
		!validExternalRef(spec.RootTriggerRef) ||
		value.ValidateID(spec.RootInitiatorActorID) != nil ||
		value.ValidateID(spec.RootSessionID) != nil ||
		spec.RootSessionVersion == 0 ||
		value.ValidateID(spec.RootTurnID) != nil ||
		spec.RootTurnVersion == 0 ||
		spec.RootAttempt == 0 || spec.RootAttempt > 100 ||
		!validSHA256(spec.ImmutableInputSHA256) ||
		value.ValidateID(spec.RuntimeRevisionID) != nil {
		return errors.New("process run specification is invalid")
	}
	for _, identifier := range []string{
		spec.ParentProcessRunID,
		spec.ResultArtifactID,
		spec.LaunchingProcessRunID,
		spec.LaunchingTurnID,
		spec.DelegationID,
		spec.TargetSessionID,
		spec.TargetTurnID,
		spec.ScheduleID,
		spec.OccurrenceID,
		spec.ContinuationGateID,
		spec.ContinuationTurnID,
		spec.ContinuationRuntimeRevisionID,
		spec.ContinuationIntegrationID,
		spec.CurrentSessionID,
		spec.CurrentTurnID,
		spec.CurrentRuntimeRevisionID,
	} {
		if identifier != "" && value.ValidateID(identifier) != nil {
			return errors.New("process run binding is invalid")
		}
	}
	if len(spec.Outcome) > 256 {
		return errors.New("process outcome is invalid")
	}
	if spec.ParentProcessRunID == "" {
		if spec.LaunchingProcessRunID != "" || spec.LaunchingTurnID != "" ||
			spec.LaunchingAttempt != 0 || spec.DelegationID != "" ||
			spec.TargetSessionID != "" || spec.TargetSessionVersion != 0 ||
			spec.TargetTurnID != "" || spec.TargetTurnVersion != 0 ||
			spec.TargetAttempt != 0 {
			return errors.New("root process launching edge is invalid")
		}
	} else if spec.LaunchingProcessRunID != spec.ParentProcessRunID ||
		spec.LaunchingTurnID == "" || spec.LaunchingAttempt == 0 ||
		spec.LaunchingAttempt > 100 || spec.DelegationID == "" ||
		spec.TargetSessionID == "" || spec.TargetSessionVersion == 0 ||
		spec.TargetTurnID == "" || spec.TargetTurnVersion == 0 ||
		spec.TargetAttempt == 0 || spec.TargetAttempt > 100 {
		return errors.New("child process launching edge is invalid")
	}
	if (spec.ScheduleID == "") != (spec.OccurrenceID == "") {
		return errors.New("process schedule lineage is incomplete")
	}
	continuation := spec.ContinuationKind != enum.ProcessContinuationNone ||
		spec.ContinuationGateID != "" || spec.ContinuationIntegrationID != "" ||
		spec.ContinuationOutcomeSHA256 != "" || spec.ContinuationTurnID != "" ||
		spec.ContinuationTurnVersion != 0 || spec.ContinuationAttempt != 0 ||
		spec.ContinuationRuntimeRevisionID != "" ||
		spec.ContinuationRuntimeRevisionVersion != 0 ||
		spec.ContinuationInputSHA256 != "" || spec.OwnerFeedbackSHA256 != ""
	if !spec.ContinuationKind.Valid() {
		return errors.New("process continuation kind is invalid")
	}
	if continuation && (spec.ContinuationKind == enum.ProcessContinuationNone ||
		spec.ContinuationTurnID == "" ||
		spec.ContinuationTurnVersion == 0 || spec.ContinuationAttempt == 0 ||
		spec.ContinuationAttempt > 100 || spec.ContinuationRuntimeRevisionID == "" ||
		spec.ContinuationRuntimeRevisionVersion == 0 ||
		!validSHA256(spec.ContinuationInputSHA256)) {
		return errors.New("process continuation binding is invalid")
	}
	if !continuation && spec.ContinuationKind != enum.ProcessContinuationNone {
		return errors.New("process continuation binding is invalid")
	}
	if spec.ContinuationKind == enum.ProcessContinuationOwnerGate &&
		(spec.ContinuationGateID == "" || !validSHA256(spec.OwnerFeedbackSHA256) ||
			spec.ContinuationIntegrationID != "" || spec.ContinuationOutcomeSHA256 != "") {
		return errors.New("owner gate continuation binding is invalid")
	}
	if spec.ContinuationKind == enum.ProcessContinuationIntegration &&
		(spec.ContinuationIntegrationID == "" ||
			!validSHA256(spec.ContinuationOutcomeSHA256) ||
			spec.ContinuationGateID != "" || spec.OwnerFeedbackSHA256 != "") {
		return errors.New("integration continuation binding is invalid")
	}
	current := spec.CurrentSessionID != "" || spec.CurrentSessionVersion != 0 ||
		spec.CurrentTurnID != "" || spec.CurrentTurnVersion != 0 ||
		spec.CurrentAttempt != 0 || spec.CurrentRuntimeRevisionID != "" ||
		spec.CurrentRuntimeRevisionVersion != 0 || spec.CurrentInputSHA256 != ""
	if current && (spec.CurrentSessionID == "" || spec.CurrentSessionVersion == 0 ||
		spec.CurrentTurnID == "" || spec.CurrentTurnVersion == 0 ||
		spec.CurrentAttempt == 0 || spec.CurrentAttempt > 100 ||
		spec.CurrentRuntimeRevisionID == "" ||
		spec.CurrentRuntimeRevisionVersion == 0 ||
		!validSHA256(spec.CurrentInputSHA256)) {
		return errors.New("process current execution binding is invalid")
	}
	if spec.OccurrenceID != "" &&
		spec.RootTriggerRef != "schedule-occurrence:"+spec.OccurrenceID {
		return errors.New("process occurrence lineage is invalid")
	}
	return nil
}

type ScheduleSpec struct {
	TargetResourceID         string                 `json:"targetResourceId"`
	TargetKind               enum.Kind              `json:"targetKind"`
	TargetVersion            uint64                 `json:"targetVersion"`
	EffectiveInputSHA        string                 `json:"effectiveInputSha256"`
	Cron                     string                 `json:"cron,omitempty"`
	Interval                 time.Duration          `json:"interval,omitempty"`
	Timezone                 string                 `json:"timezone"`
	Calendar                 string                 `json:"calendar"`
	OverlapPolicy            string                 `json:"overlapPolicy"`
	MisfirePolicy            string                 `json:"misfirePolicy"`
	MisfireGrace             time.Duration          `json:"misfireGrace"`
	NextRunAt                time.Time              `json:"nextRunAt"`
	DeliveryPolicy           string                 `json:"deliveryPolicy"`
	MaximumAttempts          uint32                 `json:"maximumAttempts"`
	InitialBackoff           time.Duration          `json:"initialBackoff"`
	MaximumBackoff           time.Duration          `json:"maximumBackoff"`
	DeadLetterAfter          time.Duration          `json:"deadLetterAfter"`
	PromptProfileID          string                 `json:"promptProfileId"`
	PromptRevision           uint64                 `json:"promptRevision"`
	SessionPolicy            string                 `json:"sessionPolicy"`
	RoomID                   string                 `json:"roomId,omitempty"`
	NotificationPolicy       string                 `json:"notificationPolicy"`
	MaximumExecutionDuration time.Duration          `json:"maximumExecutionDuration"`
	Coalesce                 bool                   `json:"coalesce"`
	RuntimeRevisionID        string                 `json:"runtimeRevisionId"`
	TargetType               string                 `json:"targetType"`
	PlaybookRef              string                 `json:"playbookRef,omitempty"`
	PlaybookVersion          uint64                 `json:"playbookVersion,omitempty"`
	PromptArtifactID         string                 `json:"promptArtifactId"`
	ExecutionSessionID       string                 `json:"executionSessionId,omitempty"`
	Ownership                ConfigurationOwnership `json:"ownership"`
}

func (ScheduleSpec) Kind() enum.Kind { return enum.KindSchedule }
func (spec ScheduleSpec) ConfigurationOwnership() ConfigurationOwnership {
	return spec.Ownership
}

func (spec ScheduleSpec) Validate() error {
	if value.ValidateID(spec.TargetResourceID) != nil ||
		spec.TargetKind != enum.KindRole ||
		spec.TargetVersion == 0 || !validSHA256(spec.EffectiveInputSHA) ||
		value.ValidateID(spec.PromptProfileID) != nil ||
		spec.PromptRevision == 0 ||
		(spec.SessionPolicy != "NEW" && spec.SessionPolicy != "PERSISTENT" &&
			spec.SessionPolicy != "ROLLING") ||
		(spec.RoomID != "" && value.ValidateID(spec.RoomID) != nil) ||
		(spec.NotificationPolicy != "ALWAYS" &&
			spec.NotificationPolicy != "ON_ACTION" &&
			spec.NotificationPolicy != "ON_FAILURE" &&
			spec.NotificationPolicy != "ON_ACTION_OR_FAILURE" &&
			spec.NotificationPolicy != "AUDIT_ONLY") ||
		spec.MaximumExecutionDuration < time.Minute ||
		spec.MaximumExecutionDuration > 24*time.Hour ||
		value.ValidateID(spec.RuntimeRevisionID) != nil ||
		(spec.TargetType != "AGENT" && spec.TargetType != "PLAYBOOK") ||
		value.ValidateID(spec.PromptArtifactID) != nil ||
		(spec.Cron == "") == (spec.Interval == 0) ||
		len(spec.Cron) > 128 ||
		(spec.Interval != 0 && (spec.Interval < time.Minute || spec.Interval > 365*24*time.Hour)) ||
		spec.NextRunAt.IsZero() ||
		(spec.Calendar != "GREGORIAN" && spec.Calendar != "BUSINESS") ||
		(spec.OverlapPolicy != "FORBID" && spec.OverlapPolicy != "SKIP" &&
			spec.OverlapPolicy != "QUEUE") ||
		(spec.MisfirePolicy != "SKIP" && spec.MisfirePolicy != "RUN_ONCE" &&
			spec.MisfirePolicy != "CATCH_UP" && spec.MisfirePolicy != "WITHIN_GRACE") ||
		(spec.DeliveryPolicy != "AT_LEAST_ONCE" &&
			spec.DeliveryPolicy != "EXACTLY_ONCE_EFFECT") ||
		spec.MaximumAttempts == 0 || spec.MaximumAttempts > 100 ||
		spec.InitialBackoff < time.Second ||
		spec.MaximumBackoff < spec.InitialBackoff ||
		spec.MaximumBackoff > 24*time.Hour ||
		spec.DeadLetterAfter < spec.MaximumBackoff ||
		spec.DeadLetterAfter > 30*24*time.Hour ||
		spec.Ownership.Validate() != nil {
		return errors.New("schedule specification is invalid")
	}
	if spec.OverlapPolicy == "QUEUE" && spec.Coalesce {
		return errors.New("schedule queue policy cannot coalesce")
	}
	if spec.TargetType == "PLAYBOOK" {
		if !validExternalRef(spec.PlaybookRef) || spec.PlaybookVersion == 0 {
			return errors.New("schedule playbook target is invalid")
		}
	} else if spec.PlaybookRef != "" || spec.PlaybookVersion != 0 {
		return errors.New("schedule agent target is invalid")
	}
	if spec.SessionPolicy == "NEW" {
		if spec.ExecutionSessionID != "" {
			return errors.New("new schedule session binding is invalid")
		}
	} else if value.ValidateID(spec.ExecutionSessionID) != nil {
		return errors.New("persistent schedule session binding is invalid")
	}
	if spec.OverlapPolicy != "QUEUE" && !spec.Coalesce {
		return errors.New("schedule overlap policy requires coalesce")
	}
	if (spec.MisfirePolicy == "WITHIN_GRACE" &&
		(spec.MisfireGrace < time.Second || spec.MisfireGrace > 24*time.Hour)) ||
		(spec.MisfirePolicy != "WITHIN_GRACE" && spec.MisfireGrace != 0) {
		return errors.New("schedule misfire grace is invalid")
	}
	if _, err := time.LoadLocation(spec.Timezone); err != nil {
		return errors.New("schedule timezone is invalid")
	}
	return nil
}

type OwnerGateSpec struct {
	ProcessRunID             string    `json:"processRunId"`
	ResultRef                string    `json:"resultRef"`
	ResultSHA256             string    `json:"resultSha256"`
	ExpiresAt                time.Time `json:"expiresAt"`
	Decision                 string    `json:"decision,omitempty"`
	DecisionReason           string    `json:"decisionReason,omitempty"`
	RootInitiatorActorID     string    `json:"rootInitiatorActorId"`
	SessionID                string    `json:"sessionId"`
	TurnID                   string    `json:"turnId"`
	Attempt                  uint32    `json:"attempt"`
	ImmutableInputSHA256     string    `json:"immutableInputSha256"`
	RecipientActorID         string    `json:"recipientActorId"`
	DeliveryWorkloadID       string    `json:"deliveryWorkloadId"`
	DeliverySPIFFEID         string    `json:"deliverySpiffeId"`
	DeliveryID               string    `json:"deliveryId"`
	DeliveryPayloadSHA256    string    `json:"deliveryPayloadSha256"`
	DeliveryClaimTokenSHA256 string    `json:"deliveryClaimTokenSha256,omitempty"`
	DeliveryClaimKeySHA256   string    `json:"deliveryClaimKeySha256,omitempty"`
	DeliveryFence            uint64    `json:"deliveryFence,omitempty"`
	DeliveryClaimExpiresAt   time.Time `json:"deliveryClaimExpiresAt,omitempty"`
	MattermostPostID         string    `json:"mattermostPostId,omitempty"`
	MattermostChannelID      string    `json:"mattermostChannelId,omitempty"`
	MattermostRootPostID     string    `json:"mattermostRootPostId,omitempty"`
	DeliveredAt              time.Time `json:"deliveredAt,omitempty"`
	ScheduleID               string    `json:"scheduleId,omitempty"`
	OccurrenceID             string    `json:"occurrenceId,omitempty"`
	DecisionReceiptSHA256    string    `json:"decisionReceiptSha256,omitempty"`
	ContinuationTurnID       string    `json:"continuationTurnId,omitempty"`
	ContinuationTurnVersion  uint64    `json:"continuationTurnVersion,omitempty"`
	ContinuationInputSHA256  string    `json:"continuationInputSha256,omitempty"`
}

func (OwnerGateSpec) Kind() enum.Kind { return enum.KindOwnerGate }
func (spec OwnerGateSpec) Validate() error {
	if value.ValidateID(spec.ProcessRunID) != nil ||
		!validExternalRef(spec.ResultRef) ||
		!validSHA256(spec.ResultSHA256) ||
		spec.ExpiresAt.IsZero() ||
		value.ValidateID(spec.RootInitiatorActorID) != nil ||
		value.ValidateID(spec.SessionID) != nil ||
		value.ValidateID(spec.TurnID) != nil ||
		spec.Attempt == 0 || spec.Attempt > 100 ||
		!validSHA256(spec.ImmutableInputSHA256) ||
		value.ValidateID(spec.RecipientActorID) != nil ||
		value.ValidateStableKey(spec.DeliveryWorkloadID) != nil ||
		!strings.HasPrefix(spec.DeliverySPIFFEID, "spiffe://") ||
		len(spec.DeliverySPIFFEID) > 512 ||
		value.ValidateID(spec.DeliveryID) != nil ||
		!validSHA256(spec.DeliveryPayloadSHA256) ||
		(spec.ScheduleID != "" && value.ValidateID(spec.ScheduleID) != nil) ||
		(spec.OccurrenceID != "" && value.ValidateID(spec.OccurrenceID) != nil) ||
		(spec.ContinuationTurnID != "" && value.ValidateID(spec.ContinuationTurnID) != nil) ||
		(spec.Decision != "" && spec.Decision != "APPROVED" &&
			spec.Decision != "REJECTED" &&
			spec.Decision != "CHANGES_REQUESTED" &&
			spec.Decision != "CANCELLED") ||
		len(spec.DecisionReason) > 2048 {
		return errors.New("owner gate specification is invalid")
	}
	claimed := spec.DeliveryFence != 0 ||
		spec.DeliveryClaimTokenSHA256 != "" ||
		spec.DeliveryClaimKeySHA256 != "" ||
		!spec.DeliveryClaimExpiresAt.IsZero()
	if claimed && (spec.DeliveryFence == 0 ||
		!validSHA256(spec.DeliveryClaimTokenSHA256) ||
		!validSHA256(spec.DeliveryClaimKeySHA256) ||
		spec.DeliveryClaimExpiresAt.IsZero()) {
		return errors.New("owner gate delivery claim is invalid")
	}
	delivered := spec.MattermostPostID != "" || spec.MattermostChannelID != "" ||
		spec.MattermostRootPostID != "" || !spec.DeliveredAt.IsZero()
	if delivered {
		if !validExternalRef(spec.MattermostPostID) ||
			!validExternalRef(spec.MattermostChannelID) ||
			!validExternalRef(spec.MattermostRootPostID) ||
			spec.DeliveredAt.IsZero() || !claimed {
			return errors.New("owner gate delivery receipt is invalid")
		}
	}
	if (spec.Decision == "") != (spec.DecisionReason == "") {
		return errors.New("owner gate decision is incomplete")
	}
	if (spec.ScheduleID == "") != (spec.OccurrenceID == "") {
		return errors.New("owner gate schedule lineage is incomplete")
	}
	continuation := spec.DecisionReceiptSHA256 != "" || spec.ContinuationTurnID != "" ||
		spec.ContinuationTurnVersion != 0 || spec.ContinuationInputSHA256 != ""
	if continuation && (!validSHA256(spec.DecisionReceiptSHA256) ||
		spec.ContinuationTurnID == "" || spec.ContinuationTurnVersion == 0 ||
		!validSHA256(spec.ContinuationInputSHA256) ||
		spec.Decision != "CHANGES_REQUESTED") {
		return errors.New("owner gate continuation receipt is invalid")
	}
	return nil
}

type MemoryRecordSpec struct {
	Scope         string `json:"scope"`
	RoleID        string `json:"roleId,omitempty"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	ContentSHA256 string `json:"contentSha256"`
	Provenance    string `json:"provenance"`
	Importance    uint32 `json:"importance"`
}

func (MemoryRecordSpec) Kind() enum.Kind { return enum.KindMemoryRecord }
func (spec MemoryRecordSpec) Validate() error {
	if spec.Scope != "PROJECT" && spec.Scope != "ROLE" ||
		len(spec.Title) < 1 || len(spec.Title) > 256 ||
		len(spec.Content) < 1 || len(spec.Content) > 32768 ||
		!validSHA256(spec.ContentSHA256) ||
		digestText(spec.Content) != spec.ContentSHA256 ||
		!validExternalRef(spec.Provenance) ||
		spec.Importance > 100 {
		return errors.New("memory record specification is invalid")
	}
	if spec.Scope == "ROLE" {
		if value.ValidateID(spec.RoleID) != nil {
			return errors.New("memory role scope is invalid")
		}
	} else if spec.RoleID != "" {
		return errors.New("memory project scope is invalid")
	}
	return nil
}

type WorkClaimSpec struct {
	ProcessRunID        string    `json:"processRunId"`
	TurnID              string    `json:"turnId"`
	Summary             string    `json:"summary"`
	Domains             []string  `json:"domains"`
	ResourceKeys        []string  `json:"resourceKeys"`
	OwnerActorID        string    `json:"ownerActorId"`
	WorkloadID          string    `json:"workloadId"`
	SessionID           string    `json:"sessionId"`
	Attempt             uint32    `json:"attempt"`
	AuthorityGeneration uint64    `json:"authorityGeneration"`
	ExpiresAt           time.Time `json:"expiresAt"`
}

func (WorkClaimSpec) Kind() enum.Kind { return enum.KindWorkClaim }
func (spec WorkClaimSpec) Validate() error {
	if value.ValidateID(spec.ProcessRunID) != nil ||
		value.ValidateID(spec.TurnID) != nil ||
		len(spec.Summary) < 1 || len(spec.Summary) > 2048 ||
		!validBoundedKeys(spec.Domains, 32) ||
		!validBoundedKeys(spec.ResourceKeys, 128) ||
		value.ValidateID(spec.OwnerActorID) != nil ||
		value.ValidateStableKey(spec.WorkloadID) != nil ||
		value.ValidateID(spec.SessionID) != nil ||
		spec.Attempt == 0 || spec.Attempt > 100 ||
		spec.AuthorityGeneration == 0 ||
		spec.ExpiresAt.IsZero() {
		return errors.New("work claim specification is invalid")
	}
	return nil
}

type ArtifactSpec struct {
	ArtifactKind       string    `json:"kind"`
	Direction          string    `json:"direction"`
	StorageRef         string    `json:"storageRef"`
	SizeBytes          uint64    `json:"sizeBytes"`
	MediaType          string    `json:"mediaType"`
	SHA256             string    `json:"sha256"`
	ScanStatus         string    `json:"scanStatus"`
	RetentionPolicyRef string    `json:"retentionPolicyRef"`
	ScanPolicyRevision uint64    `json:"scanPolicyRevision,omitempty"`
	ScanEvidenceSHA256 string    `json:"scanEvidenceSha256,omitempty"`
	ScannerWorkloadID  string    `json:"scannerWorkloadId,omitempty"`
	ScannedAt          time.Time `json:"scannedAt,omitempty"`
}

func (ArtifactSpec) Kind() enum.Kind { return enum.KindArtifact }
func (spec ArtifactSpec) Validate() error {
	if value.ValidateStableKey(spec.ArtifactKind) != nil ||
		(spec.Direction != "INPUT" && spec.Direction != "OUTPUT" &&
			spec.Direction != "ARCHIVE") ||
		!validExternalRef(spec.StorageRef) ||
		spec.SizeBytes == 0 || spec.SizeBytes > 10<<30 ||
		len(spec.MediaType) < 3 || len(spec.MediaType) > 255 ||
		!validSHA256(spec.SHA256) ||
		(spec.ScanStatus != "PENDING" && spec.ScanStatus != "SCANNING" &&
			spec.ScanStatus != "CLEAN" &&
			spec.ScanStatus != "QUARANTINED" && spec.ScanStatus != "FAILED") ||
		!validExternalRef(spec.RetentionPolicyRef) {
		return errors.New("artifact specification is invalid")
	}
	if spec.ScanStatus == "PENDING" {
		if spec.ScanPolicyRevision != 0 || spec.ScanEvidenceSHA256 != "" ||
			spec.ScannerWorkloadID != "" || !spec.ScannedAt.IsZero() {
			return errors.New("pending artifact scan metadata is invalid")
		}
		return nil
	}
	if spec.ScanPolicyRevision == 0 ||
		value.ValidateStableKey(spec.ScannerWorkloadID) != nil {
		return errors.New("artifact scan metadata is invalid")
	}
	if spec.ScanStatus == "SCANNING" {
		if spec.ScanEvidenceSHA256 != "" || !spec.ScannedAt.IsZero() {
			return errors.New("in-progress artifact evidence is invalid")
		}
		return nil
	}
	if !validSHA256(spec.ScanEvidenceSHA256) || spec.ScannedAt.IsZero() {
		return errors.New("terminal artifact scan evidence is invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func validExternalRef(reference string) bool {
	if len(reference) < 1 || len(reference) > 512 ||
		reference != strings.TrimSpace(reference) {
		return false
	}
	for _, symbol := range reference {
		if symbol < 0x20 || symbol == 0x7f {
			return false
		}
	}
	return true
}

func validSecretRef(reference string) bool {
	if !validExternalRef(reference) {
		return false
	}
	parsed, err := url.Parse(reference)
	return err == nil &&
		(parsed.Scheme == "vault" || parsed.Scheme == "k8s-secret") &&
		parsed.Host != "" && parsed.Path != "" && parsed.Path != "/" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validImmutableSecretRef(reference string) bool {
	if !validExternalRef(reference) {
		return false
	}
	parsed, err := url.Parse(reference)
	return err == nil &&
		(parsed.Scheme == "vault-versioned" || parsed.Scheme == "k8s-immutable-secret") &&
		parsed.Host != "" && parsed.Path != "" && parsed.Path != "/" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validBoundedKeys(values []string, maximum int) bool {
	if len(values) == 0 || len(values) > maximum {
		return false
	}
	copyValues := slices.Clone(values)
	slices.Sort(copyValues)
	for index, item := range copyValues {
		if value.ValidateStableKey(item) != nil ||
			(index > 0 && item == copyValues[index-1]) {
			return false
		}
	}
	return true
}

func validUniqueIDs(values []string) bool {
	copyValues := slices.Clone(values)
	slices.Sort(copyValues)
	for index, item := range copyValues {
		if value.ValidateID(item) != nil ||
			(index > 0 && item == copyValues[index-1]) {
			return false
		}
	}
	return true
}

func validPermissions(values []string, maximum int) bool {
	if len(values) == 0 || len(values) > maximum {
		return false
	}
	copyValues := slices.Clone(values)
	slices.Sort(copyValues)
	for index, item := range copyValues {
		if len(item) > 128 || !permissionPattern.MatchString(item) ||
			(index > 0 && item == copyValues[index-1]) {
			return false
		}
	}
	return true
}

func digestText(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
