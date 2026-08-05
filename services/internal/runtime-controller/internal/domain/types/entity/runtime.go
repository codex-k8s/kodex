// Package entity содержит проверяемые runtime-снимки без transport DTO.
package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var threadPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,256}$`)
var codexRolloutPattern = regexp.MustCompile(`^\.matter-codex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`)

type Execution struct {
	ID, OrganizationID, ProjectID, ProcessID, SessionID, ThreadID, RoleID, TurnID string
	Attempt                                                                       uint32
	RuntimeRevisionID                                                             string
	RuntimeRevisionVersion                                                        uint64
	RuntimeRevisionSHA256                                                         string
	EffectiveRuntimeSHA256                                                        string
	ImmutableInputSHA256                                                          string
	AgentSessionKey, AgentRunID, AgentBindingSHA256                               string
	AgentSessionID, AgentSessionTurnID                                            int64
	ResourceClass                                                                 enum.ResourceClass
	AccessProfile                                                                 enum.AccessProfile
	WorkloadID, WorkloadSPIFFEID                                                  string
	GrantGeneration, Version, Fence                                               uint64
	State                                                                         enum.ExecutionState
	LeaseID                                                                       string
	LeaseExpiresAt                                                                time.Time
	ArchiveReference, ArchiveSHA256                                               string
	ArchiveObjectKey, ArchiveVersionID, ArchiveKMSKeyARN, ArchiveObjectLockMode   string
	ArchiveProvenanceSHA256                                                       string
	RestoreProofReference, RestoreProofSHA256                                     string
	CleanupAuthorizationID                                                        string
	CleanupAuthorizationGeneration                                                uint64
	CleanupAuthorizationState                                                     string
	CleanupAuthorizationExpiresAt                                                 time.Time
	CleanupPVCName, CleanupPVCUID, CleanupPVCResourceVersion                      string
	CleanupClaimedAt, CleanupEligibleAt, CleanupNotFoundAt                        time.Time
	CleanupDeletionProofSHA256                                                    string
	RestoreSourceExecutionID, RestoreSourceArchiveReference                       string
	RestoreSourceArchiveSHA256, RestoreSourceRuntimeRevisionSHA256                string
	RestoreSourceImmutableInputSHA256, RestoreSourceProofSHA256                   string
	RestoreSourceVersion                                                          uint64
	RestoreSourceArchiveObjectKey, RestoreSourceArchiveVersionID                  string
	RestoreSourceArchiveKMSKeyARN, RestoreSourceArchiveObjectLockMode             string
	RestoreSourceArchiveRetainUntil                                               time.Time
	RestoreSourceRetentionPolicyID, RestoreSourceProvenanceSHA256                 string
	RestoreSourceRetentionPolicyVersion                                           uint64
	RetentionPolicyID                                                             string
	RetentionPolicyVersion, PVCRetentionSeconds, ArchiveRetentionSeconds          uint64
	ArchiveRetainUntil                                                            time.Time
	PVCCleanupEligibleAt, CapacityObservationExpiresAt, RescheduleAfter           time.Time
	RestoreAssignmentState                                                        string
	RestoreAssignmentGeneration                                                   uint64
	RestoreTargetPVCName, RestoreTargetPVCUID, RestoreTargetPVCResourceVersion    string
	RehydrateProofReference, RehydrateProofSHA256                                 string
	CredentialSnapshotSHA256, WorkloadTicketSHA256                                string
	ProviderBindingID, ProviderBindingSHA256, CodexSessionID                      string
	CodexArchiveRelativePath, CodexArchiveSHA256, CodexArchiveProvenance          string
	ProviderBindingVersion                                                        uint64
	Materializations                                                              []Materialization
	WorkloadTicket                                                                string `json:"-"`
	ArchiveWorkloadTicket                                                         string `json:"-"`
	RestoreWorkloadTicket                                                         string `json:"-"`
}

type Materialization struct {
	Kind, ArtifactID, SHA256, RelativePath, MediaType, StorageRef string
	ArtifactVersion, SizeBytes                                    uint64
}

// ArchiveEvidence — exact owner-bound S3 readback, а не вычисленные target поля.
type ArchiveEvidence struct {
	Reference, SHA256, ObjectKey, VersionID string
	KMSKeyARN, ObjectLockMode               string
	RetainUntil                             time.Time
	ProvenanceSHA256                        string
}

func (execution Execution) Validate() error {
	for _, identifier := range []string{execution.ID, execution.OrganizationID,
		execution.ProjectID, execution.ProcessID, execution.SessionID,
		execution.RoleID, execution.TurnID,
		execution.RuntimeRevisionID} {
		parsed, err := uuid.Parse(identifier)
		if err != nil || parsed.String() != identifier {
			return errors.New("runtime execution identity is invalid")
		}
	}
	if !threadPattern.MatchString(execution.ThreadID) {
		return errors.New("runtime thread identity is invalid")
	}
	if execution.Attempt == 0 || execution.RuntimeRevisionVersion == 0 ||
		!sha256Pattern.MatchString(execution.RuntimeRevisionSHA256) ||
		!sha256Pattern.MatchString(execution.EffectiveRuntimeSHA256) ||
		!sha256Pattern.MatchString(execution.ImmutableInputSHA256) ||
		execution.AgentSessionKey == "" || len(execution.AgentSessionKey) > 256 ||
		execution.AgentSessionID <= 0 || execution.AgentSessionTurnID <= 0 || execution.AgentRunID == "" ||
		!sha256Pattern.MatchString(execution.AgentBindingSHA256) ||
		execution.RetentionPolicyID == "" || execution.RetentionPolicyVersion == 0 ||
		execution.PVCRetentionSeconds < 86400 || execution.ArchiveRetentionSeconds < 7776000 ||
		execution.PVCCleanupEligibleAt.IsZero() || execution.CapacityObservationExpiresAt.IsZero() ||
		execution.RescheduleAfter.IsZero() ||
		!sha256Pattern.MatchString(execution.CredentialSnapshotSHA256) ||
		!sha256Pattern.MatchString(execution.WorkloadTicketSHA256) ||
		uuid.Validate(execution.ProviderBindingID) != nil || execution.ProviderBindingVersion == 0 ||
		!sha256Pattern.MatchString(execution.ProviderBindingSHA256) || len(execution.Materializations) < 2 ||
		execution.GrantGeneration == 0 || execution.Version == 0 || execution.Fence == 0 ||
		execution.WorkloadID == "" || execution.WorkloadSPIFFEID == "" {
		return errors.New("runtime execution binding is invalid")
	}
	switch execution.ResourceClass {
	case enum.ResourceStandard, enum.ResourceHighMemory, enum.ResourceAccelerated:
	default:
		return errors.New("runtime resource class is invalid")
	}
	switch execution.AccessProfile {
	case enum.AccessNone, enum.AccessProjectRead, enum.AccessClusterAdmin:
	default:
		return errors.New("runtime access profile is invalid")
	}
	switch execution.State {
	case enum.ExecutionPending, enum.ExecutionAdmitted, enum.ExecutionRunning,
		enum.ExecutionSucceeded, enum.ExecutionFailed, enum.ExecutionCancelled,
		enum.ExecutionExpired, enum.ExecutionRetried, enum.ExecutionSuspended:
	default:
		return errors.New("runtime execution state is invalid")
	}
	if !strings.HasPrefix(execution.WorkloadSPIFFEID, "spiffe://") ||
		!validArchiveState(execution) || !validCleanupState(execution) ||
		!validRestoreSource(execution) || !validRestoreAssignment(execution) ||
		!validCodexArchive(execution) || !validMaterializations(execution.Materializations) {
		return errors.New("runtime execution lifecycle evidence is invalid")
	}
	return nil
}

func validCodexArchive(execution Execution) bool {
	empty := execution.CodexSessionID == "" && execution.CodexArchiveRelativePath == "" &&
		execution.CodexArchiveSHA256 == "" && execution.CodexArchiveProvenance == ""
	present := uuid.Validate(execution.CodexSessionID) == nil &&
		codexRolloutPattern.MatchString(execution.CodexArchiveRelativePath) &&
		!strings.Contains(execution.CodexArchiveRelativePath, "..") &&
		sha256Pattern.MatchString(execution.CodexArchiveSHA256) &&
		validCodexArchiveProvenance(execution.CodexArchiveProvenance,
			execution.CodexArchiveRelativePath, execution.CodexArchiveSHA256)
	return empty || present
}

func validCodexArchiveProvenance(value, path, digest string) bool {
	const prefix = "codex-app-server-rollout-v1:"
	suffix := ":" + path + ":" + digest
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	sourceExecutionID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return uuid.Validate(sourceExecutionID) == nil
}

func validMaterializations(items []Materialization) bool {
	seen := make(map[string]struct{}, len(items))
	prompt, instructions := 0, 0
	for _, item := range items {
		if uuid.Validate(item.ArtifactID) != nil || item.ArtifactVersion == 0 || item.SizeBytes == 0 ||
			!sha256Pattern.MatchString(item.SHA256) || item.RelativePath == "" || item.MediaType == "" ||
			item.StorageRef == "" {
			return false
		}
		if _, duplicate := seen[item.RelativePath]; duplicate {
			return false
		}
		seen[item.RelativePath] = struct{}{}
		if item.Kind == "PROMPT" && item.RelativePath == ".matter-codex/inbox/prompt.md" {
			prompt++
		}
		if item.Kind == "INSTRUCTION" && item.RelativePath == "AGENTS.md" {
			instructions++
		}
	}
	return prompt == 1 && instructions == 1
}

func validRestoreAssignment(execution Execution) bool {
	switch execution.RestoreAssignmentState {
	case "NONE":
		return execution.RestoreAssignmentGeneration == 0 &&
			execution.RestoreSourceExecutionID == "" && execution.RestoreTargetPVCName == "" &&
			execution.RestoreTargetPVCUID == "" && execution.RestoreTargetPVCResourceVersion == "" &&
			execution.RehydrateProofReference == "" && execution.RehydrateProofSHA256 == ""
	case "ASSIGNED":
		return execution.RestoreAssignmentGeneration > 0 && execution.RestoreSourceExecutionID != "" &&
			execution.RestoreTargetPVCName == "" && execution.RestoreTargetPVCUID == "" &&
			execution.RehydrateProofReference == "" && execution.RehydrateProofSHA256 == ""
	case "BOUND":
		return execution.RestoreAssignmentGeneration > 0 && validRestoreTarget(execution) &&
			execution.RehydrateProofReference == "" && execution.RehydrateProofSHA256 == ""
	case "CONSUMED":
		return execution.RestoreAssignmentGeneration > 0 && validRestoreTarget(execution) &&
			execution.RehydrateProofReference != "" && sha256Pattern.MatchString(execution.RehydrateProofSHA256)
	default:
		return false
	}
}

func validRestoreTarget(execution Execution) bool {
	return execution.RestoreTargetPVCName != "" && uuid.Validate(execution.RestoreTargetPVCUID) == nil &&
		execution.RestoreTargetPVCResourceVersion != ""
}

func validRestoreSource(execution Execution) bool {
	empty := execution.RestoreSourceExecutionID == "" && execution.RestoreSourceArchiveReference == "" &&
		execution.RestoreSourceArchiveSHA256 == "" && execution.RestoreSourceRuntimeRevisionSHA256 == "" &&
		execution.RestoreSourceImmutableInputSHA256 == "" && execution.RestoreSourceProofSHA256 == "" &&
		execution.RestoreSourceVersion == 0 && execution.RestoreSourceArchiveObjectKey == "" &&
		execution.RestoreSourceArchiveVersionID == "" && execution.RestoreSourceArchiveKMSKeyARN == "" &&
		execution.RestoreSourceArchiveObjectLockMode == "" && execution.RestoreSourceArchiveRetainUntil.IsZero() &&
		execution.RestoreSourceRetentionPolicyID == "" && execution.RestoreSourceRetentionPolicyVersion == 0 &&
		execution.RestoreSourceProvenanceSHA256 == ""
	present := uuid.Validate(execution.RestoreSourceExecutionID) == nil &&
		execution.RestoreSourceArchiveReference != "" &&
		sha256Pattern.MatchString(execution.RestoreSourceArchiveSHA256) &&
		sha256Pattern.MatchString(execution.RestoreSourceRuntimeRevisionSHA256) &&
		sha256Pattern.MatchString(execution.RestoreSourceImmutableInputSHA256) &&
		sha256Pattern.MatchString(execution.RestoreSourceProofSHA256) && execution.RestoreSourceVersion > 0 &&
		execution.RestoreSourceArchiveObjectKey != "" && execution.RestoreSourceArchiveVersionID != "" &&
		strings.HasPrefix(execution.RestoreSourceArchiveKMSKeyARN, "arn:") &&
		execution.RestoreSourceArchiveObjectLockMode == "COMPLIANCE" &&
		!execution.RestoreSourceArchiveRetainUntil.IsZero() &&
		execution.RestoreSourceRetentionPolicyID != "" && execution.RestoreSourceRetentionPolicyVersion > 0 &&
		sha256Pattern.MatchString(execution.RestoreSourceProvenanceSHA256)
	return empty || present
}

func validArchiveState(execution Execution) bool {
	archiveEmpty := execution.ArchiveReference == "" && execution.ArchiveSHA256 == "" &&
		execution.ArchiveObjectKey == "" && execution.ArchiveVersionID == "" &&
		execution.ArchiveKMSKeyARN == "" && execution.ArchiveObjectLockMode == "" &&
		execution.ArchiveProvenanceSHA256 == ""
	archivePresent := execution.ArchiveReference != "" && sha256Pattern.MatchString(execution.ArchiveSHA256) &&
		execution.ArchiveObjectKey != "" && execution.ArchiveVersionID != "" &&
		strings.HasPrefix(execution.ArchiveKMSKeyARN, "arn:") &&
		execution.ArchiveObjectLockMode == "COMPLIANCE" &&
		sha256Pattern.MatchString(execution.ArchiveProvenanceSHA256)
	restoreEmpty := execution.RestoreProofReference == "" && execution.RestoreProofSHA256 == ""
	restorePresent := execution.RestoreProofReference != "" && sha256Pattern.MatchString(execution.RestoreProofSHA256)
	retentionReady := !execution.State.Terminal() || !execution.ArchiveRetainUntil.IsZero()
	return retentionReady && (archiveEmpty || archivePresent) && (restoreEmpty || restorePresent) &&
		(!restorePresent || archivePresent) && (archiveEmpty || execution.State.Terminal())
}

func validCleanupState(execution Execution) bool {
	switch execution.CleanupAuthorizationState {
	case "NONE":
		return execution.CleanupAuthorizationGeneration == 0 &&
			execution.CleanupAuthorizationID == "" && execution.CleanupAuthorizationExpiresAt.IsZero() &&
			execution.CleanupPVCName == "" && execution.CleanupPVCUID == "" &&
			execution.CleanupPVCResourceVersion == "" && execution.CleanupClaimedAt.IsZero() &&
			execution.CleanupEligibleAt.IsZero() && execution.CleanupNotFoundAt.IsZero() &&
			execution.CleanupDeletionProofSHA256 == ""
	case "ACTIVE", "EXPIRED":
		return execution.CleanupAuthorizationGeneration > 0 &&
			uuid.Validate(execution.CleanupAuthorizationID) == nil &&
			!execution.CleanupAuthorizationExpiresAt.IsZero() &&
			sha256Pattern.MatchString(execution.ArchiveSHA256) &&
			sha256Pattern.MatchString(execution.RestoreProofSHA256) && validPVCTuple(execution) &&
			!execution.CleanupClaimedAt.IsZero() && !execution.CleanupEligibleAt.After(execution.CleanupClaimedAt) &&
			execution.CleanupNotFoundAt.IsZero() && execution.CleanupDeletionProofSHA256 == ""
	case "CONSUMED":
		return execution.CleanupAuthorizationGeneration > 0 &&
			uuid.Validate(execution.CleanupAuthorizationID) == nil &&
			!execution.CleanupAuthorizationExpiresAt.IsZero() &&
			sha256Pattern.MatchString(execution.ArchiveSHA256) &&
			sha256Pattern.MatchString(execution.RestoreProofSHA256) && validPVCTuple(execution) &&
			!execution.CleanupClaimedAt.IsZero() && !execution.CleanupEligibleAt.After(execution.CleanupClaimedAt) &&
			!execution.CleanupNotFoundAt.Before(execution.CleanupClaimedAt) &&
			sha256Pattern.MatchString(execution.CleanupDeletionProofSHA256)
	default:
		return false
	}
}

func validPVCTuple(execution Execution) bool {
	return execution.CleanupPVCName != "" && uuid.Validate(execution.CleanupPVCUID) == nil &&
		execution.CleanupPVCResourceVersion != ""
}

type Component struct {
	Kind             string
	ResourceID       string
	Version          uint64
	ProjectionSHA256 string
}

type Revision struct {
	ID                                            string
	Version                                       uint64
	ManifestSHA256                                string
	EffectiveRuntimeSHA256                        string
	ImageDigest                                   string
	SessionID, RoleID, ChatID                     string
	ProviderCredentialBindingID                   string
	ProviderAccountName                           string
	PromptProfileID                               string
	PromptRevision                                uint64
	AuthorityPolicyRevision                       uint64
	AuthorityPolicySHA256                         string
	CredentialBindingIDs                          []string
	IntegrationIDs                                []string
	Components                                    []Component
	Credentials                                   []CredentialRef
	ProviderObservedUsage                         uint64
	ProviderObservedLimit                         uint64
	ProviderObservationRevision                   uint64
	ProviderObservedAt                            time.Time
	ProviderObservationMaxAge                     time.Duration
	AgentProfile                                  string
	CodexModel, CodexSandbox, CodexApprovalPolicy string
}

type CredentialRef struct {
	ResourceID             string
	Purpose                string
	Reference              string
	Version                uint64
	ProviderContentVersion string
	ContentSHA256          string
}

func (revision Revision) ValidateFor(execution Execution) error {
	if revision.ID != execution.RuntimeRevisionID ||
		revision.Version != execution.RuntimeRevisionVersion ||
		revision.SessionID != execution.SessionID || revision.RoleID != execution.RoleID ||
		revision.ProviderCredentialBindingID != execution.ProviderBindingID ||
		uuid.Validate(revision.ID) != nil || uuid.Validate(revision.SessionID) != nil ||
		uuid.Validate(revision.RoleID) != nil || uuid.Validate(revision.PromptProfileID) != nil ||
		uuid.Validate(revision.ProviderCredentialBindingID) != nil ||
		(revision.ChatID != "" && uuid.Validate(revision.ChatID) != nil) ||
		!sha256Pattern.MatchString(revision.ManifestSHA256) ||
		revision.EffectiveRuntimeSHA256 != execution.EffectiveRuntimeSHA256 ||
		!sha256Pattern.MatchString(revision.EffectiveRuntimeSHA256) ||
		!sha256Pattern.MatchString(execution.RuntimeRevisionSHA256) ||
		!sha256Pattern.MatchString(revision.AuthorityPolicySHA256) ||
		revision.PromptRevision == 0 || revision.AuthorityPolicyRevision == 0 ||
		len(revision.Components) == 0 || len(revision.Components) > 256 ||
		len(revision.CredentialBindingIDs) > 64 || len(revision.Credentials) > 64 ||
		revision.ProviderObservedLimit == 0 ||
		revision.ProviderObservedUsage > revision.ProviderObservedLimit ||
		revision.ProviderObservationRevision == 0 || revision.ProviderObservedAt.IsZero() ||
		revision.ProviderObservationMaxAge < time.Minute || revision.ProviderObservationMaxAge > 24*time.Hour ||
		!regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,47}[a-z0-9])?$`).MatchString(revision.ProviderAccountName) ||
		!regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(revision.AgentProfile) ||
		len(revision.ImageDigest) != 71 || revision.ImageDigest[:7] != "sha256:" ||
		!sha256Pattern.MatchString(revision.ImageDigest[7:]) {
		return errors.New("runtime revision does not match execution")
	}
	components := make(map[string]Component, len(revision.Components))
	credentialComponents := make(map[string]Component)
	roleFound := false
	for _, component := range revision.Components {
		if uuid.Validate(component.ResourceID) != nil || !validResourceKind(component.Kind) ||
			component.Version == 0 || !sha256Pattern.MatchString(component.ProjectionSHA256) {
			return errors.New("runtime revision component is invalid")
		}
		key := component.Kind + ":" + component.ResourceID
		if _, duplicate := components[key]; duplicate {
			return errors.New("runtime revision component is duplicated")
		}
		components[key] = component
		if component.Kind == "RESOURCE_KIND_ROLE" && component.ResourceID == revision.RoleID {
			roleFound = true
		}
		if component.Kind == "RESOURCE_KIND_CREDENTIAL_BINDING" {
			credentialComponents[component.ResourceID] = component
		}
	}
	if !roleFound || len(credentialComponents) != len(revision.CredentialBindingIDs) ||
		len(revision.Credentials) != len(revision.CredentialBindingIDs) {
		return errors.New("runtime revision component set is incomplete")
	}
	credentialRefs := make(map[string]CredentialRef, len(revision.Credentials))
	requiredPurposes := map[string]int{
		"control-plane-application-grant":           0,
		"runtime-materialization-application-grant": 0,
		"mcp-token":                      0,
		"handoff-private-key":            0,
		"control-plane-client-tls":       0,
		"interaction-gateway-client-tls": 0,
		"mcp-client-tls":                 0,
	}
	for _, credential := range revision.Credentials {
		component, exists := credentialComponents[credential.ResourceID]
		if !exists || credential.Version != component.Version || credential.Purpose == "" ||
			len(credential.Purpose) > 128 || credential.Reference == "" || len(credential.Reference) > 1024 ||
			credential.ProviderContentVersion == "" || len(credential.ProviderContentVersion) > 256 ||
			!sha256Pattern.MatchString(credential.ContentSHA256) {
			return errors.New("runtime credential reference is invalid")
		}
		if _, duplicate := credentialRefs[credential.ResourceID]; duplicate {
			return errors.New("runtime credential reference is duplicated")
		}
		credentialRefs[credential.ResourceID] = credential
		if credential.ResourceID == revision.ProviderCredentialBindingID && credential.Purpose != "provider-account" {
			return errors.New("runtime provider credential purpose is invalid")
		}
		if _, required := requiredPurposes[credential.Purpose]; required {
			requiredPurposes[credential.Purpose]++
		}
	}
	for _, count := range requiredPurposes {
		if count != 1 {
			return errors.New("runtime authority credential set is incomplete")
		}
	}
	seenBindings := make(map[string]struct{}, len(revision.CredentialBindingIDs))
	for _, identifier := range revision.CredentialBindingIDs {
		if uuid.Validate(identifier) != nil || credentialRefs[identifier].ResourceID == "" {
			return errors.New("runtime credential binding set is invalid")
		}
		if _, duplicate := seenBindings[identifier]; duplicate {
			return errors.New("runtime credential binding is duplicated")
		}
		seenBindings[identifier] = struct{}{}
	}
	if _, exists := seenBindings[revision.ProviderCredentialBindingID]; !exists {
		return errors.New("runtime provider credential binding is missing")
	}
	type credentialSnapshotEntry struct {
		ID, Purpose, ImmutableSecretRef, ProviderContentVersion, ContentSHA256 string
		Version                                                                uint64
	}
	snapshot := make([]credentialSnapshotEntry, 0, len(revision.Credentials))
	for _, credential := range revision.Credentials {
		snapshot = append(snapshot, credentialSnapshotEntry{ID: credential.ResourceID,
			Purpose: credential.Purpose, ImmutableSecretRef: credential.Reference,
			ProviderContentVersion: credential.ProviderContentVersion,
			ContentSHA256:          credential.ContentSHA256, Version: credential.Version})
	}
	slices.SortFunc(snapshot, func(left, right credentialSnapshotEntry) int {
		return strings.Compare(left.ID, right.ID)
	})
	raw, err := json.Marshal(struct {
		ExecutionID string
		Credentials []credentialSnapshotEntry
	}{execution.ID, snapshot})
	if err != nil {
		return errors.New("runtime credential snapshot is invalid")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != execution.CredentialSnapshotSHA256 {
		return errors.New("runtime credential snapshot digest mismatch")
	}
	seenIntegrations := make(map[string]struct{}, len(revision.IntegrationIDs))
	for _, identifier := range revision.IntegrationIDs {
		if uuid.Validate(identifier) != nil || components["RESOURCE_KIND_INTEGRATION:"+identifier].ResourceID == "" {
			return errors.New("runtime integration set is invalid")
		}
		if _, duplicate := seenIntegrations[identifier]; duplicate {
			return errors.New("runtime integration is duplicated")
		}
		seenIntegrations[identifier] = struct{}{}
	}
	for _, required := range []string{
		"RESOURCE_KIND_ROLE:" + revision.RoleID,
		"RESOURCE_KIND_PROMPT_PROFILE:" + revision.PromptProfileID,
		"RESOURCE_KIND_CREDENTIAL_BINDING:" + revision.ProviderCredentialBindingID,
		"RESOURCE_KIND_SESSION:" + revision.SessionID,
	} {
		if components[required].ResourceID == "" {
			return errors.New("runtime revision required component is missing")
		}
	}
	if revision.ChatID != "" && components["RESOURCE_KIND_CHAT:"+revision.ChatID].ResourceID == "" {
		return errors.New("runtime revision chat component is missing")
	}
	return nil
}

func validResourceKind(value string) bool {
	switch value {
	case "RESOURCE_KIND_PROJECT", "RESOURCE_KIND_TEAM", "RESOURCE_KIND_CHAT", "RESOURCE_KIND_ROLE",
		"RESOURCE_KIND_PROMPT_PROFILE", "RESOURCE_KIND_CREDENTIAL_BINDING",
		"RESOURCE_KIND_REPOSITORY_WORKSPACE", "RESOURCE_KIND_INTEGRATION",
		"RESOURCE_KIND_RUNTIME_REVISION", "RESOURCE_KIND_SESSION", "RESOURCE_KIND_TURN",
		"RESOURCE_KIND_PROCESS_RUN", "RESOURCE_KIND_SCHEDULE", "RESOURCE_KIND_OWNER_GATE",
		"RESOURCE_KIND_MEMORY_RECORD", "RESOURCE_KIND_WORK_CLAIM", "RESOURCE_KIND_ARTIFACT":
		return true
	default:
		return false
	}
}

type RuntimeStatus struct {
	ExecutionID                                                              string
	Version, Fence, GrantGeneration                                          uint64
	PodName, PodUID, PodResourceVersion, PVCName, PVCUID, PVCResourceVersion string
	Phase                                                                    string
	Ready                                                                    bool
	LastTransition                                                           time.Time
	AccessProfile                                                            enum.AccessProfile
	JournalName                                                              string
	PVCDeleted                                                               bool
	PVCDeletionStarted                                                       bool
	PVCNotFoundAt                                                            time.Time
	PVCDeletionProofSHA256                                                   string
	PVCDeletionAuthorizationID                                               string
	PVCDeletionAuthorizationGeneration                                       uint64
	ArchiveSnapshotPVCUID                                                    string
	RetentionOwner                                                           bool
	RuntimeRevisionSHA256                                                    string
	Handoff                                                                  *RuntimeHandoff
}

type RuntimeHandoff struct {
	Schema, ExecutionID, RuntimeRevisionSHA256, ImmutableInputSHA256      string
	EffectiveRuntimeSHA256, SessionID, TurnID                             string
	ExecutionVersion, Fence, GrantGeneration                              uint64
	Attempt                                                               uint32
	ProviderBindingID, ProviderBindingSHA256                              string
	ProviderBindingVersion                                                uint64
	Outcome, TerminalReference, TerminalSHA256                            string
	Outputs                                                               []RuntimeOutput
	CodexSessionID, ArchiveRelativePath, ArchiveSHA256, ArchiveProvenance string
	ObservedAt                                                            time.Time
}

type RuntimeOutput struct {
	Kind, ArtifactID, ArtifactSHA256, ArtifactName, ArtifactMediaType string
	ArtifactVersion                                                   uint64
	ArtifactPayload                                                   []byte
	ArtifactStorageRef                                                string
	ArtifactSizeBytes                                                 uint64
	Sequence, Total                                                   uint32
}

type PVCDeletionProof struct {
	PVCName, PVCUID, PVCResourceVersion, SHA256 string
	ObservedNotFoundAt                          time.Time
	CleanupAuthorizationID                      string
	CleanupAuthorizationGeneration              uint64
}

type RuntimeJournal struct {
	Execution                   Execution `json:"execution"`
	AdmissionRequest            Execution `json:"admission_request"`
	Phase                       string    `json:"phase"`
	AdmitKey                    string    `json:"admit_idempotency_key"`
	HeartbeatKey                string    `json:"heartbeat_idempotency_key"`
	CompleteKey                 string    `json:"complete_idempotency_key"`
	IncidentKey                 string    `json:"incident_idempotency_key"`
	ArchiveKey                  string    `json:"archive_idempotency_key"`
	RestoreKey                  string    `json:"restore_idempotency_key"`
	CleanupKey                  string    `json:"cleanup_idempotency_key"`
	PodName                     string    `json:"pod_name"`
	PVCName                     string    `json:"pvc_name"`
	CreatedAt                   time.Time `json:"created_at"`
	LastTransition              time.Time `json:"last_transition"`
	PVCUID                      string    `json:"pvc_uid,omitempty"`
	PVCResourceVersion          string    `json:"pvc_resource_version,omitempty"`
	PVCDeletionOwner            bool      `json:"pvc_deletion_owner,omitempty"`
	PVCDeleted                  bool      `json:"pvc_deleted,omitempty"`
	PVCNotFoundAt               time.Time `json:"pvc_not_found_at,omitempty"`
	PVCDeletionProofSHA256      string    `json:"pvc_deletion_proof_sha256,omitempty"`
	PVCDeletionAuthorizationID  string    `json:"pvc_deletion_authorization_id,omitempty"`
	PVCDeletionGeneration       uint64    `json:"pvc_deletion_authorization_generation,omitempty"`
	RehydratePhase              string    `json:"rehydrate_phase"`
	RehydratePVCUID             string    `json:"rehydrate_pvc_uid,omitempty"`
	RehydrateProofReference     string    `json:"rehydrate_proof_reference,omitempty"`
	RehydrateProofSHA256        string    `json:"rehydrate_proof_sha256,omitempty"`
	CapacityProviderBindingID   string    `json:"capacity_provider_binding_id,omitempty"`
	CapacityObservationRevision uint64    `json:"capacity_observation_revision,omitempty"`
	CapacityObservedAt          time.Time `json:"capacity_observed_at,omitempty"`
	CapacityObservedUsage       uint64    `json:"capacity_observed_usage,omitempty"`
	CapacityObservedLimit       uint64    `json:"capacity_observed_limit,omitempty"`
	CapacityObservationMaxAge   int64     `json:"capacity_observation_max_age_nanos,omitempty"`
	CapacityOrganizationLimit   int       `json:"capacity_organization_limit,omitempty"`
}

type CapacityDecision struct {
	Admitted bool
	Reason   string
	Eviction *RuntimeStatus
}
