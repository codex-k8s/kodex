package entity

import (
	"strings"
	"testing"

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
	return Execution{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), ProjectID: uuid.NewString(),
		ProcessID: uuid.NewString(), SessionID: uuid.NewString(), ThreadID: "thread-1",
		RoleID: uuid.NewString(), TurnID: uuid.NewString(), Attempt: 1,
		RuntimeRevisionID: uuid.NewString(), RuntimeRevisionVersion: 1,
		RuntimeRevisionSHA256: strings.Repeat("a", 64), ImmutableInputSHA256: strings.Repeat("b", 64),
		ResourceClass: enum.ResourceStandard, AccessProfile: enum.AccessNone,
		WorkloadID: "runtime-controller", WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		GrantGeneration: 1, Version: 1, Fence: 1, State: enum.ExecutionPending,
		CleanupAuthorizationState: "NONE",
	}
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
		ManifestSHA256: strings.Repeat("c", 64), ImageDigest: "sha256:" + strings.Repeat("d", 64),
		SessionID: execution.SessionID, RoleID: execution.RoleID,
		ProviderCredentialBindingID: credentialID, PromptProfileID: promptID, PromptRevision: 1,
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: strings.Repeat("e", 64),
		CredentialBindingIDs: []string{credentialID},
		Credentials:          []CredentialRef{{ResourceID: credentialID, Purpose: "provider", Reference: "k8s-secret://provider", Version: 1}},
		Components: []Component{
			{Kind: "RESOURCE_KIND_PROJECT", ResourceID: execution.ProjectID, Version: 1, ProjectionSHA256: strings.Repeat("1", 64)},
			{Kind: "RESOURCE_KIND_SESSION", ResourceID: execution.SessionID, Version: 1, ProjectionSHA256: strings.Repeat("2", 64)},
			{Kind: "RESOURCE_KIND_ROLE", ResourceID: execution.RoleID, Version: 1, ProjectionSHA256: strings.Repeat("3", 64)},
			{Kind: "RESOURCE_KIND_PROMPT_PROFILE", ResourceID: promptID, Version: 1, ProjectionSHA256: strings.Repeat("4", 64)},
			{Kind: "RESOURCE_KIND_CREDENTIAL_BINDING", ResourceID: credentialID, Version: 1, ProjectionSHA256: strings.Repeat("5", 64)},
		},
	}
	if err := revision.ValidateFor(execution); err != nil {
		t.Fatalf("valid runtime revision rejected: %v", err)
	}
	revision.Components[0].Kind = "RESOURCE_KIND_UNKNOWN"
	if err := revision.ValidateFor(execution); err == nil {
		t.Fatal("unknown runtime revision component accepted")
	}
}
