package resource

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestRuntimeResourcePolicyClosedCapabilities(t *testing.T) {
	tests := []struct {
		name           string
		capabilities   []string
		resourceClass  string
		clusterProfile string
	}{
		{name: "default", resourceClass: "STANDARD", clusterProfile: "NONE"},
		{
			name:          "high memory read only",
			capabilities:  []string{"runtime.resource.high-memory", "runtime.cluster.read"},
			resourceClass: "HIGH_MEMORY", clusterProfile: "PROJECT_READ_ONLY",
		},
		{
			name: "accelerated cluster admin",
			capabilities: []string{
				"runtime.resource.high-memory", "runtime.resource.accelerated",
				"runtime.cluster.read", "runtime.cluster.admin",
			},
			resourceClass: "ACCELERATED", clusterProfile: "CLUSTER_ADMIN",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceClass, clusterProfile := runtimeResourcePolicy(entity.RoleSpec{
				Capabilities: test.capabilities,
			})
			if resourceClass != test.resourceClass || clusterProfile != test.clusterProfile {
				t.Fatalf("unexpected runtime policy: %s/%s", resourceClass, clusterProfile)
			}
		})
	}
}

func TestRuntimeMutationRejectsStaleFenceAndAuthority(t *testing.T) {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	execution := RuntimeExecution{
		TurnID: "3ed0d109-5eba-4e4e-8b98-f755f6e6fc6b", Attempt: 2,
		ImmutableInputSHA256: digest, WorkloadID: "runtime-controller",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		GrantGeneration:  7, Version: 4, Fence: 9, State: "RUNNING",
	}
	principal := value.Principal{
		CallerWorkload: "runtime-controller", CallerSPIFFEID: execution.WorkloadSPIFFEID,
		AuthorityReference: execution.TurnID, AuthorityRevision: 2,
		AuthorityDigest: digest, AuthorityGrantGeneration: 7,
	}
	input := RuntimeExecutionInput{
		Principal: principal, ExpectedVersion: 4, ExpectedFence: 9,
		ExpectedGrantGeneration: 7,
	}
	if err := matchRuntimeMutation(execution, input, "RUNNING"); err != nil {
		t.Fatalf("exact mutation rejected: %v", err)
	}

	staleFence := input
	staleFence.ExpectedFence = 8
	if err := matchRuntimeMutation(execution, staleFence); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale fence returned %v", err)
	}

	staleGrant := input
	staleGrant.Principal.AuthorityGrantGeneration = 6
	if err := matchRuntimeMutation(execution, staleGrant); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("stale authority returned %v", err)
	}

	foreignSPIFFE := input
	foreignSPIFFE.Principal.CallerSPIFFEID = "spiffe://mattercodex.local/ns/foreign/sa/runtime-controller"
	if err := matchRuntimeMutation(execution, foreignSPIFFE); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("foreign SPIFFE returned %v", err)
	}
}

func TestIntegrationGatewayBindingIsExact(t *testing.T) {
	digest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	continuation := IntegrationContinuation{
		TurnID: "a189a33f-fea7-4d20-96f0-b5a05c6a5c5c", Attempt: 3,
		ImmutableInputSHA256: digest, GrantGeneration: 11,
	}
	principal := value.Principal{
		AuthorityReference: continuation.TurnID, AuthorityRevision: 3,
		AuthorityDigest: digest, AuthorityGrantGeneration: 11,
	}
	if err := matchIntegrationGateway(continuation, principal); err != nil {
		t.Fatalf("exact integration binding rejected: %v", err)
	}
	principal.AuthorityDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if err := matchIntegrationGateway(continuation, principal); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("changed request tuple returned %v", err)
	}
}

func TestRuntimeExpiryIsClosedTurnTransition(t *testing.T) {
	for _, state := range []enum.State{enum.StateClaimed, enum.StateRunning} {
		if !enum.TransitionAllowed(enum.KindTurn, state, enum.StateExpired) {
			t.Fatalf("runtime expiry transition from %s is unavailable", state)
		}
	}
	if enum.TransitionAllowed(enum.KindTurn, enum.StateWaitingExternal, enum.StateExpired) {
		t.Fatal("suspended integration turn must not expire through runtime path")
	}
}

func TestRuntimeSuspensionAndRetryPredecessorsAreClosed(t *testing.T) {
	if !runtimeTerminal("SUSPENDED") {
		t.Fatal("suspended runtime execution must revoke the previous authority")
	}
	for _, state := range []string{"FAILED", "EXPIRED"} {
		if !retryableRuntimePredecessor(state) {
			t.Fatalf("retryable predecessor was rejected: %s", state)
		}
	}
	for _, state := range []string{"SUCCEEDED", "CANCELLED", "SUSPENDED", "RETRIED"} {
		if retryableRuntimePredecessor(state) {
			t.Fatalf("non-retryable predecessor was accepted: %s", state)
		}
	}
	for _, target := range []enum.State{
		enum.StateSucceeded, enum.StateFailed, enum.StateCancelled, enum.StateExpired,
	} {
		if !enum.TransitionAllowed(enum.KindProcessRun, enum.StateRunning, target) {
			t.Fatalf("process terminal transition is unavailable: %s", target)
		}
	}
	if scheduledTerminalState(enum.StateExpired) != "FAILED" {
		t.Fatal("expired runtime must remain reachable to scheduled retry/dead-letter")
	}
}

func TestCleanupAuthorizationCrashSafeLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	authorizationID := "5574792c-5721-4b85-83b7-e8c6857b8fef"
	tests := []struct {
		name       string
		execution  RuntimeExecution
		expected   uint64
		mustExpire bool
		wantErr    error
	}{
		{name: "first issue", execution: RuntimeExecution{CleanupAuthorizationState: "NONE"}},
		{
			name: "active blocks duplicate",
			execution: RuntimeExecution{
				CleanupAuthorizationState: "ACTIVE", CleanupAuthorizationGeneration: 1,
				CleanupAuthorizationID: authorizationID, CleanupAuthorizationExpiresAt: now.Add(time.Minute),
			},
			expected: 1, wantErr: errs.ErrStateConflict,
		},
		{
			name: "expired active is fenced before reissue",
			execution: RuntimeExecution{
				CleanupAuthorizationState: "ACTIVE", CleanupAuthorizationGeneration: 1,
				CleanupAuthorizationID: authorizationID, CleanupAuthorizationExpiresAt: now,
			},
			expected: 1, mustExpire: true,
		},
		{
			name: "explicitly expired reissues",
			execution: RuntimeExecution{
				CleanupAuthorizationState: "EXPIRED", CleanupAuthorizationGeneration: 2,
				CleanupAuthorizationID: authorizationID, CleanupAuthorizationExpiresAt: now,
			},
			expected: 2,
		},
		{
			name: "consumed never reissues",
			execution: RuntimeExecution{
				CleanupAuthorizationState: "CONSUMED", CleanupAuthorizationGeneration: 2,
				CleanupAuthorizationID: authorizationID, CleanupAuthorizationExpiresAt: now,
			},
			expected: 2, wantErr: errs.ErrStateConflict,
		},
		{
			name: "stale generation",
			execution: RuntimeExecution{
				CleanupAuthorizationState: "EXPIRED", CleanupAuthorizationGeneration: 3,
				CleanupAuthorizationID: authorizationID, CleanupAuthorizationExpiresAt: now,
			},
			expected: 2, wantErr: errs.ErrVersionMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mustExpire, err := cleanupAuthorizationIssueDisposition(
				test.execution, test.expected, now,
			)
			if !errors.Is(err, test.wantErr) || mustExpire != test.mustExpire {
				t.Fatalf("unexpected disposition: expire=%t err=%v", mustExpire, err)
			}
		})
	}
}

func TestIntegrationBindingAndApprovedCancelCompetition(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	if !validPinnedIntegrationResources(nil) {
		t.Fatal("credentialless integration must preserve an exact empty binding set")
	}
	digest := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	bindings := []PinnedIntegrationResource{
		{ResourceID: "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526", Version: 1, ProjectionSHA256: digest},
		{ResourceID: "fd0570db-07c9-4a9a-8d35-3657119068c3", Version: 2, ProjectionSHA256: digest},
	}
	if !validPinnedIntegrationResources(bindings) {
		t.Fatal("exact sorted integration bindings were rejected")
	}
	bindings[0], bindings[1] = bindings[1], bindings[0]
	if validPinnedIntegrationResources(bindings) {
		t.Fatal("non-canonical binding order was accepted")
	}
	approved := IntegrationContinuation{
		ApprovalState: "APPROVED", ExecutionState: "NOT_STARTED",
		ApprovalExpiresAt: now.Add(time.Minute),
	}
	if !integrationDecisionAllowed(approved, "CANCELLED", now) {
		t.Fatal("approved not-started cancellation is unreachable")
	}
	approved.ExecutionState = "EXECUTING"
	if integrationDecisionAllowed(approved, "CANCELLED", now) {
		t.Fatal("cancel must lose after begin wins")
	}
	pending := IntegrationContinuation{
		ApprovalState: "PENDING", ExecutionState: "NOT_STARTED",
		ApprovalExpiresAt: now.Add(time.Minute),
	}
	if !integrationDecisionAllowed(pending, "APPROVED", now) {
		t.Fatal("pending approval decision was rejected")
	}
	pending.ApprovalExpiresAt = now
	if integrationDecisionAllowed(pending, "APPROVED", now) {
		t.Fatal("expired approval decision was accepted")
	}
}
