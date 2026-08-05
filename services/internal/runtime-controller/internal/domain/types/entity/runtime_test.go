package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
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

func TestExecutionValidateBindsDeliveryRecoveryToRetry(t *testing.T) {
	execution := validExecution()
	source := uuid.NewString()
	execution.CodexDeliveryRecoverySourceExecutionID = source
	execution.CodexSessionID = uuid.NewString()
	execution.CodexArchiveRelativePath = ".matter-codex/state/codex-home/sessions/2026/08/04/rollout-session.jsonl"
	execution.CodexArchiveSHA256 = strings.Repeat("6", 64)
	execution.CodexArchiveProvenance = "codex-app-server-rollout-v1:" + source + ":" +
		execution.CodexArchiveRelativePath + ":" + execution.CodexArchiveSHA256
	if err := execution.Validate(); err == nil {
		t.Fatal("delivery recovery marker was accepted for first attempt")
	}
	execution.Attempt = 2
	if err := execution.Validate(); err != nil {
		t.Fatalf("delivery recovery marker was rejected for retry: %v", err)
	}
	execution.CodexDeliveryRecoverySourceExecutionID = "not-an-execution-id"
	if err := execution.Validate(); err == nil {
		t.Fatal("invalid delivery recovery execution was accepted")
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
		ProviderBindingID: uuid.NewString(), ProviderBindingVersion: 1, ProviderBindingSHA256: strings.Repeat("7", 64),
		CleanupAuthorizationState: "NONE",
	}
	execution.Materializations = []Materialization{
		{Kind: "PROMPT", ArtifactID: uuid.NewString(), ArtifactVersion: 1, SHA256: strings.Repeat("1", 64), SizeBytes: 1, RelativePath: ".matter-codex/inbox/prompt.md", MediaType: "text/markdown", StorageRef: "s3://runtime/prompt"},
		{Kind: "INSTRUCTION", ArtifactID: uuid.NewString(), ArtifactVersion: 1, SHA256: strings.Repeat("2", 64), SizeBytes: 1, RelativePath: "AGENTS.md", MediaType: "text/markdown", StorageRef: "s3://runtime/instructions"},
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
	promptID := uuid.NewString()
	revision := Revision{
		ProviderAccountName: "primary",
		ID:                  execution.RuntimeRevisionID, Version: execution.RuntimeRevisionVersion,
		ManifestSHA256: strings.Repeat("c", 64), EffectiveRuntimeSHA256: execution.EffectiveRuntimeSHA256,
		ImageDigest: "sha256:" + strings.Repeat("d", 64),
		SessionID:   execution.SessionID, RoleID: execution.RoleID,
		ProviderCredentialBindingID: execution.ProviderBindingID, PromptProfileID: promptID, PromptRevision: 1,
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: strings.Repeat("e", 64),
		ProviderObservedUsage: 1, ProviderObservedLimit: 2, ProviderObservationRevision: 1,
		ProviderObservedAt: time.Now().UTC(), ProviderObservationMaxAge: time.Hour,
		AgentProfile: "developer",
		Components: []Component{
			{Kind: "RESOURCE_KIND_PROJECT", ResourceID: execution.ProjectID, Version: 1, ProjectionSHA256: strings.Repeat("1", 64)},
			{Kind: "RESOURCE_KIND_SESSION", ResourceID: execution.SessionID, Version: 1, ProjectionSHA256: strings.Repeat("2", 64)},
			{Kind: "RESOURCE_KIND_ROLE", ResourceID: execution.RoleID, Version: 1, ProjectionSHA256: strings.Repeat("3", 64)},
			{Kind: "RESOURCE_KIND_PROMPT_PROFILE", ResourceID: promptID, Version: 1, ProjectionSHA256: strings.Repeat("4", 64)},
		},
	}
	purposes := []string{"provider-account", "control-plane-application-grant",
		"runtime-materialization-application-grant", "mcp-token", "handoff-private-key",
		"control-plane-client-tls", "interaction-gateway-client-tls", "mcp-client-tls"}
	for _, purpose := range purposes {
		identifier := uuid.NewString()
		if purpose == "provider-account" {
			identifier = execution.ProviderBindingID
		}
		revision.CredentialBindingIDs = append(revision.CredentialBindingIDs, identifier)
		revision.Credentials = append(revision.Credentials, CredentialRef{ResourceID: identifier, Purpose: purpose,
			Reference: "k8s-immutable-secret://mattercodex-system/credential-" + identifier, Version: 1,
			ProviderContentVersion: "uid:1", ContentSHA256: strings.Repeat("6", 64)})
		revision.Components = append(revision.Components, Component{Kind: "RESOURCE_KIND_CREDENTIAL_BINDING",
			ResourceID: identifier, Version: 1, ProjectionSHA256: strings.Repeat("6", 64)})
	}
	type snapshotEntry struct {
		ID, Purpose, ImmutableSecretRef, ProviderContentVersion, ContentSHA256 string
		Version                                                                uint64
	}
	snapshot := make([]snapshotEntry, 0, len(revision.Credentials))
	for _, credential := range revision.Credentials {
		snapshot = append(snapshot, snapshotEntry{credential.ResourceID, credential.Purpose, credential.Reference,
			credential.ProviderContentVersion, credential.ContentSHA256, credential.Version})
	}
	slices.SortFunc(snapshot, func(left, right snapshotEntry) int { return strings.Compare(left.ID, right.ID) })
	raw, _ := json.Marshal(struct {
		ExecutionID string
		Credentials []snapshotEntry
	}{execution.ID, snapshot})
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
