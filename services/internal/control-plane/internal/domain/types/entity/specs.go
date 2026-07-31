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

type ProjectSpec struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Locale      string `json:"locale"`
}

func (ProjectSpec) Kind() enum.Kind { return enum.KindProject }
func (spec ProjectSpec) Validate() error {
	if value.ValidateStableKey(spec.Slug) != nil ||
		len(spec.Description) > 4096 ||
		(spec.Locale != "ru" && spec.Locale != "en") {
		return errors.New("project specification is invalid")
	}
	return nil
}

type TeamSpec struct {
	StableKey       string   `json:"stableKey"`
	ExternalTeamRef string   `json:"externalTeamRef"`
	MemberActorIDs  []string `json:"memberActorIds"`
	RoleIDs         []string `json:"roleIds"`
}

func (TeamSpec) Kind() enum.Kind { return enum.KindTeam }
func (spec TeamSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		!validExternalRef(spec.ExternalTeamRef) ||
		len(spec.MemberActorIDs) > 1000 ||
		len(spec.RoleIDs) < 1 || len(spec.RoleIDs) > 64 {
		return errors.New("team specification is invalid")
	}
	if !validUniqueIDs(spec.MemberActorIDs) || !validUniqueIDs(spec.RoleIDs) {
		return errors.New("team membership is invalid")
	}
	return nil
}

type ChatSpec struct {
	StableKey          string `json:"stableKey"`
	RoomType           string `json:"roomType"`
	DefaultAgentID     string `json:"defaultAgentId,omitempty"`
	ExternalChannelRef string `json:"externalChannelRef"`
	WorkPolicy         string `json:"workPolicy"`
}

func (ChatSpec) Kind() enum.Kind { return enum.KindChat }
func (spec ChatSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		(spec.RoomType != "USER" && spec.RoomType != "COORDINATION" &&
			spec.RoomType != "WORK_CONTROL" && spec.RoomType != "RUNS") ||
		!validExternalRef(spec.ExternalChannelRef) ||
		len(spec.WorkPolicy) > 8192 {
		return errors.New("chat specification is invalid")
	}
	if spec.DefaultAgentID != "" && value.ValidateID(spec.DefaultAgentID) != nil {
		return errors.New("chat default agent is invalid")
	}
	return nil
}

type RoleSpec struct {
	StableKey            string   `json:"stableKey"`
	Capabilities         []string `json:"capabilities"`
	AllowedTargetRoleIDs []string `json:"allowedTargetRoleIds"`
	PromptProfileID      string   `json:"promptProfileId"`
}

func (RoleSpec) Kind() enum.Kind { return enum.KindRole }
func (spec RoleSpec) Validate() error {
	if value.ValidateStableKey(spec.StableKey) != nil ||
		value.ValidateID(spec.PromptProfileID) != nil ||
		!validPermissions(spec.Capabilities, 64) ||
		len(spec.AllowedTargetRoleIDs) > 64 {
		return errors.New("role specification is invalid")
	}
	for _, identifier := range spec.AllowedTargetRoleIDs {
		if value.ValidateID(identifier) != nil {
			return errors.New("role relationship is invalid")
		}
	}
	return nil
}

type PromptProfileSpec struct {
	Revision      uint64 `json:"revision"`
	ContentSHA256 string `json:"contentSha256"`
	SourceRef     string `json:"sourceRef"`
	Locale        string `json:"locale"`
}

func (PromptProfileSpec) Kind() enum.Kind { return enum.KindPromptProfile }
func (spec PromptProfileSpec) Validate() error {
	if spec.Revision == 0 || !validSHA256(spec.ContentSHA256) ||
		!validExternalRef(spec.SourceRef) ||
		(spec.Locale != "ru" && spec.Locale != "en") {
		return errors.New("prompt profile specification is invalid")
	}
	return nil
}

type CredentialBindingSpec struct {
	Purpose      string    `json:"purpose"`
	SecretRef    string    `json:"secretRef"`
	PrincipalRef string    `json:"principalRef"`
	Revision     uint64    `json:"revision"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
}

func (CredentialBindingSpec) Kind() enum.Kind { return enum.KindCredentialBinding }
func (spec CredentialBindingSpec) Validate() error {
	if value.ValidateStableKey(spec.Purpose) != nil ||
		!validSecretRef(spec.SecretRef) ||
		!validExternalRef(spec.PrincipalRef) ||
		spec.Revision == 0 {
		return errors.New("credential binding specification is invalid")
	}
	return nil
}

type RepositoryWorkspaceSpec struct {
	RepositoryRef       string `json:"repositoryRef"`
	WorkspaceMode       string `json:"workspaceMode"`
	DefaultBranch       string `json:"defaultBranch"`
	CredentialBindingID string `json:"credentialBindingId,omitempty"`
}

func (RepositoryWorkspaceSpec) Kind() enum.Kind { return enum.KindRepositoryWorkspace }
func (spec RepositoryWorkspaceSpec) Validate() error {
	if spec.WorkspaceMode != "NONE" && spec.WorkspaceMode != "GIT" {
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
	DefinitionRef        string   `json:"definitionRef"`
	DefinitionVersion    uint64   `json:"definitionVersion"`
	Capabilities         []string `json:"capabilities"`
	CredentialBindingIDs []string `json:"credentialBindingIds"`
	EndpointRef          string   `json:"endpointRef"`
}

func (IntegrationSpec) Kind() enum.Kind { return enum.KindIntegration }
func (spec IntegrationSpec) Validate() error {
	if !validExternalRef(spec.DefinitionRef) || spec.DefinitionVersion == 0 ||
		!validBoundedKeys(spec.Capabilities, 128) ||
		len(spec.CredentialBindingIDs) > 32 ||
		!validUniqueIDs(spec.CredentialBindingIDs) ||
		!validExternalRef(spec.EndpointRef) {
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
	ManifestSHA256         string                 `json:"manifestSha256"`
	ImageDigest            string                 `json:"imageDigest"`
	PromptProfileID        string                 `json:"promptProfileId"`
	PromptRevision         uint64                 `json:"promptRevision"`
	CredentialBindingIDs   []string               `json:"credentialBindingIds"`
	IntegrationIDs         []string               `json:"integrationIds"`
	PredecessorRevisionID  string                 `json:"predecessorRevisionId,omitempty"`
	AuthorityPolicyVersion uint64                 `json:"authorityPolicyRevision"`
	AuthorityPolicySHA256  string                 `json:"authorityPolicySha256"`
	Components             []EffectiveResourceRef `json:"components"`
	CreatedAt              time.Time              `json:"createdAt"`
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
		spec.CreatedAt.IsZero() {
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
	AgentID                  string `json:"agentId"`
	ProviderAccountBindingID string `json:"providerAccountBindingId"`
	ConversationID           string `json:"conversationId,omitempty"`
	ArchiveRef               string `json:"archiveRef,omitempty"`
	LastTurnSequence         uint64 `json:"lastTurnSequence"`
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
	return nil
}

type TurnSpec struct {
	SessionID            string `json:"sessionId"`
	Sequence             uint64 `json:"sequence"`
	SourceRef            string `json:"sourceRef"`
	PromptArtifactID     string `json:"promptArtifactId"`
	RuntimeRevisionID    string `json:"runtimeRevisionId"`
	ProcessRunID         string `json:"processRunId,omitempty"`
	Attempt              uint32 `json:"attempt"`
	Outcome              string `json:"outcome,omitempty"`
	ResultArtifactID     string `json:"resultArtifactId,omitempty"`
	EffectiveInputSHA256 string `json:"effectiveInputSha256"`
	PredecessorTurnID    string `json:"predecessorTurnId,omitempty"`
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
	} {
		if identifier != "" && value.ValidateID(identifier) != nil {
			return errors.New("turn binding is invalid")
		}
	}
	return nil
}

type ProcessRunSpec struct {
	ParentProcessRunID   string `json:"parentProcessRunId,omitempty"`
	PlaybookRef          string `json:"playbookRef"`
	PolicyRevision       uint64 `json:"policyRevision"`
	RootTriggerRef       string `json:"rootTriggerRef"`
	ResultArtifactID     string `json:"resultArtifactId,omitempty"`
	RootInitiatorActorID string `json:"rootInitiatorActorId"`
	RootSessionID        string `json:"rootSessionId"`
	RootTurnID           string `json:"rootTurnId"`
	RootAttempt          uint32 `json:"rootAttempt"`
	ImmutableInputSHA256 string `json:"immutableInputSha256"`
}

func (ProcessRunSpec) Kind() enum.Kind { return enum.KindProcessRun }
func (spec ProcessRunSpec) Validate() error {
	if !validExternalRef(spec.PlaybookRef) || spec.PolicyRevision == 0 ||
		!validExternalRef(spec.RootTriggerRef) ||
		value.ValidateID(spec.RootInitiatorActorID) != nil ||
		value.ValidateID(spec.RootSessionID) != nil ||
		value.ValidateID(spec.RootTurnID) != nil ||
		spec.RootAttempt == 0 || spec.RootAttempt > 100 ||
		!validSHA256(spec.ImmutableInputSHA256) {
		return errors.New("process run specification is invalid")
	}
	for _, identifier := range []string{spec.ParentProcessRunID, spec.ResultArtifactID} {
		if identifier != "" && value.ValidateID(identifier) != nil {
			return errors.New("process run binding is invalid")
		}
	}
	return nil
}

type ScheduleSpec struct {
	TargetResourceID  string        `json:"targetResourceId"`
	TargetKind        enum.Kind     `json:"targetKind"`
	TargetVersion     uint64        `json:"targetVersion"`
	EffectiveInputSHA string        `json:"effectiveInputSha256"`
	Cron              string        `json:"cron,omitempty"`
	Interval          time.Duration `json:"interval,omitempty"`
	Timezone          string        `json:"timezone"`
	Calendar          string        `json:"calendar"`
	OverlapPolicy     string        `json:"overlapPolicy"`
	MisfirePolicy     string        `json:"misfirePolicy"`
	MisfireGrace      time.Duration `json:"misfireGrace"`
	NextRunAt         time.Time     `json:"nextRunAt"`
	DeliveryPolicy    string        `json:"deliveryPolicy"`
	MaximumAttempts   uint32        `json:"maximumAttempts"`
	InitialBackoff    time.Duration `json:"initialBackoff"`
	MaximumBackoff    time.Duration `json:"maximumBackoff"`
	DeadLetterAfter   time.Duration `json:"deadLetterAfter"`
}

func (ScheduleSpec) Kind() enum.Kind { return enum.KindSchedule }
func (spec ScheduleSpec) Validate() error {
	if value.ValidateID(spec.TargetResourceID) != nil ||
		!spec.TargetKind.Valid() || spec.TargetKind == enum.KindSchedule ||
		spec.TargetVersion == 0 || !validSHA256(spec.EffectiveInputSHA) ||
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
		spec.DeadLetterAfter > 30*24*time.Hour {
		return errors.New("schedule specification is invalid")
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
	ProcessRunID         string    `json:"processRunId"`
	ResultRef            string    `json:"resultRef"`
	ResultSHA256         string    `json:"resultSha256"`
	ExpiresAt            time.Time `json:"expiresAt"`
	Decision             string    `json:"decision,omitempty"`
	DecisionReason       string    `json:"decisionReason,omitempty"`
	RootInitiatorActorID string    `json:"rootInitiatorActorId"`
	SessionID            string    `json:"sessionId"`
	TurnID               string    `json:"turnId"`
	Attempt              uint32    `json:"attempt"`
	ImmutableInputSHA256 string    `json:"immutableInputSha256"`
	RecipientActorID     string    `json:"recipientActorId"`
	DeliveryWorkloadID   string    `json:"deliveryWorkloadId"`
	DeliverySPIFFEID     string    `json:"deliverySpiffeId"`
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
		(spec.Decision != "" && spec.Decision != "APPROVED" &&
			spec.Decision != "REJECTED" &&
			spec.Decision != "CHANGES_REQUESTED" &&
			spec.Decision != "CANCELLED") ||
		len(spec.DecisionReason) > 2048 {
		return errors.New("owner gate specification is invalid")
	}
	if (spec.Decision == "") != (spec.DecisionReason == "") {
		return errors.New("owner gate decision is incomplete")
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
	ProcessRunID string   `json:"processRunId"`
	TurnID       string   `json:"turnId"`
	Summary      string   `json:"summary"`
	Domains      []string `json:"domains"`
	ResourceKeys []string `json:"resourceKeys"`
}

func (WorkClaimSpec) Kind() enum.Kind { return enum.KindWorkClaim }
func (spec WorkClaimSpec) Validate() error {
	if value.ValidateID(spec.ProcessRunID) != nil ||
		value.ValidateID(spec.TurnID) != nil ||
		len(spec.Summary) < 1 || len(spec.Summary) > 2048 ||
		!validBoundedKeys(spec.Domains, 32) ||
		!validBoundedKeys(spec.ResourceKeys, 128) {
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
