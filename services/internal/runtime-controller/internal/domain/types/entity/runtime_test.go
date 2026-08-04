package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
)

func TestExecutionValidateAcceptsMattermostThreadAndRejectsUnboundTuple(t *testing.T) {
	execution := validExecution()
	execution.ThreadID = "de04a9b5fe7d7c1e-role-41"
	if err := execution.Validate(); err != nil {
		t.Fatalf("valid execution rejected: %v", err)
	}
	execution.ImmutableInputSHA256 = strings.Repeat("0", 63)
	if err := execution.Validate(); err == nil {
		t.Fatal("invalid immutable input digest accepted")
	}
}

func validExecution() Execution {
	execution := Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		ProcessID: uuid.NewString(), SessionID: uuid.NewString(), ThreadID: "thread-1",
		RoleID: uuid.NewString(), TurnID: uuid.NewString(), Attempt: 1,
		RuntimeRevisionID: uuid.NewString(), RuntimeRevisionVersion: 1,
		RuntimeRevisionSHA256: strings.Repeat("a", 64), EffectiveRuntimeSHA256: strings.Repeat("f", 64),
		ImmutableInputSHA256: strings.Repeat("b", 64), AgentSessionKey: "agent-session", AgentSessionID: 1,
		AgentSessionTurnID: 1, AgentRunID: "run-1", AgentBindingSHA256: strings.Repeat("8", 64),
		ResourceClass: enum.ResourceStandard, AccessProfile: enum.AccessNone,
		WorkloadID: "runtime-controller", WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		GrantGeneration: 1, Version: 1, Fence: 1, State: enum.ExecutionPending,
		RetentionPolicyID: "default", RetentionPolicyVersion: 1, PVCRetentionSeconds: 86400,
		ArchiveRetentionSeconds: 7776000, PVCCleanupEligibleAt: time.Now().UTC().Add(24 * time.Hour),
		CapacityObservationExpiresAt: time.Now().UTC().Add(time.Hour), RescheduleAfter: time.Now().UTC().Add(time.Minute),
		RestoreAssignmentState: "NONE", WorkloadTicketSHA256: strings.Repeat("9", 64),
		CleanupAuthorizationState: "NONE",
	}
	raw, _ := json.Marshal(struct {
		ExecutionID string
		Credentials []struct{}
	}{execution.ID, []struct{}{}})
	digest := sha256.Sum256(raw)
	execution.CredentialSnapshotSHA256 = hex.EncodeToString(digest[:])
	return execution
}

func TestExecutionValidateRejectsOpenLifecycleValues(t *testing.T) {
	execution := validExecution()
	execution.State = "UNKNOWN"
	if err := execution.Validate(); err == nil {
		t.Fatal("unknown execution state accepted")
	}
	execution = validExecution()
	execution.CleanupAuthorizationState = "UNKNOWN"
	if err := execution.Validate(); err == nil {
		t.Fatal("unknown cleanup authorization state accepted")
	}
}

func TestRevisionValidateRequiresClosedCompleteComponentSet(t *testing.T) {
	execution := validExecution()
	credentialID, promptID := uuid.NewString(), uuid.NewString()
	revision := Revision{
		ID: execution.RuntimeRevisionID, Version: execution.RuntimeRevisionVersion,
		ManifestSHA256: strings.Repeat("c", 64), EffectiveRuntimeSHA256: execution.EffectiveRuntimeSHA256,
		ImageDigest: "sha256:" + strings.Repeat("d", 64),
		SessionID:   execution.SessionID, RoleID: execution.RoleID,
		ProviderCredentialBindingID: credentialID, PromptProfileID: promptID, PromptRevision: 1,
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: strings.Repeat("e", 64),
		CredentialBindingIDs: []string{credentialID},
		Credentials: []CredentialRef{{ResourceID: credentialID, Purpose: "provider",
			Reference: "k8s-immutable-secret://mattercodex-system/provider", Version: 1,
			ProviderContentVersion: "uid:1", ContentSHA256: strings.Repeat("6", 64)}},
		ProviderObservedUsage: 1, ProviderObservedLimit: 2, ProviderObservationRevision: 1,
		ProviderObservedAt: time.Now().UTC(), ProviderObservationMaxAge: time.Hour,
		AgentProfile: "developer",
		Components: []Component{
			{Kind: "RESOURCE_KIND_PROJECT", ResourceID: execution.ProjectID, Version: 1, ProjectionSHA256: strings.Repeat("1", 64)},
			{Kind: "RESOURCE_KIND_SESSION", ResourceID: execution.SessionID, Version: 1, ProjectionSHA256: strings.Repeat("2", 64)},
			{Kind: "RESOURCE_KIND_ROLE", ResourceID: execution.RoleID, Version: 1, ProjectionSHA256: strings.Repeat("3", 64)},
			{Kind: "RESOURCE_KIND_PROMPT_PROFILE", ResourceID: promptID, Version: 1, ProjectionSHA256: strings.Repeat("4", 64)},
			{Kind: "RESOURCE_KIND_CREDENTIAL_BINDING", ResourceID: credentialID, Version: 1, ProjectionSHA256: strings.Repeat("5", 64)},
		},
	}
	type snapshotEntry struct {
		ID, Purpose, ImmutableSecretRef, ProviderContentVersion, ContentSHA256 string
		Version                                                                uint64
	}
	raw, _ := json.Marshal(struct {
		ExecutionID string
		Credentials []snapshotEntry
	}{execution.ID, []snapshotEntry{{credentialID, "provider", revision.Credentials[0].Reference,
		revision.Credentials[0].ProviderContentVersion, revision.Credentials[0].ContentSHA256, 1}}})
	digest := sha256.Sum256(raw)
	execution.CredentialSnapshotSHA256 = hex.EncodeToString(digest[:])
	if err := revision.ValidateFor(execution); err != nil {
		t.Fatalf("valid runtime revision rejected: %v", err)
	}
	revision.Components[0].Kind = "RESOURCE_KIND_UNKNOWN"
	if err := revision.ValidateFor(execution); err == nil {
		t.Fatal("unknown runtime revision component accepted")
	}
}
