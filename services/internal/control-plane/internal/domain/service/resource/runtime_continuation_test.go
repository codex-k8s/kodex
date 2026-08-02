package resource

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestSemanticCommandHashIgnoresOneTimeCorrelationID(t *testing.T) {
	principal := value.Principal{
		ActorID:        "5574792c-5721-4b85-83b7-e8c6857b8fef",
		OrganizationID: "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526",
		ProjectID:      "fd0570db-07c9-4a9a-8d35-3657119068c3",
		Permission:     permissionIntegrationAcknowledge, PolicyRevision: 8,
		AuthorityGeneration: 12, CallerWorkload: "agent-runner",
		CallerSPIFFEID:           "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner",
		AuthoritySource:          "AGENT_SESSION",
		AuthorityReference:       "1373ea94-fdda-47f7-adbe-7ae3bc633c03",
		AuthorityRevision:        2,
		AuthorityDigest:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		AuthorityGrantGeneration: 21,
		CorrelationID:            "8bdfe85e-8ddf-4904-b139-bfa9139df42e",
	}
	intent := acknowledgeIntegrationIntent{
		ExpectedVersion: 7, ExpectedFence: 9,
		ExpectedInputSHA256: principal.AuthorityDigest,
	}
	first, err := semanticCommandHash(principal, intent)
	if err != nil {
		t.Fatalf("first semantic hash: %v", err)
	}
	principal.CorrelationID = "e910cf2c-702b-4f8a-806f-6cfd094696cd"
	second, err := semanticCommandHash(principal, intent)
	if err != nil || first != second {
		t.Fatalf("new proof JTI changed semantic intent: %s %s %v", first, second, err)
	}
	principal.AuthorityGrantGeneration++
	changed, err := semanticCommandHash(principal, intent)
	if err != nil || changed == first {
		t.Fatalf("authority-critical generation did not change semantic intent")
	}
}

func TestProcessContinuationBindingClosedUnion(t *testing.T) {
	digest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	base := entity.ProcessRunSpec{
		PlaybookRef: "playbook:v1", PolicyRevision: 1, RootTriggerRef: "manual:test",
		RootInitiatorActorID: "5574792c-5721-4b85-83b7-e8c6857b8fef",
		RootSessionID:        "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526", RootSessionVersion: 1,
		RootTurnID: "fd0570db-07c9-4a9a-8d35-3657119068c3", RootTurnVersion: 1,
		RootAttempt: 1, ImmutableInputSHA256: digest,
		RuntimeRevisionID:       "1373ea94-fdda-47f7-adbe-7ae3bc633c03",
		ContinuationTurnID:      "8bdfe85e-8ddf-4904-b139-bfa9139df42e",
		ContinuationTurnVersion: 1, ContinuationAttempt: 1,
		ContinuationRuntimeRevisionID:      "e910cf2c-702b-4f8a-806f-6cfd094696cd",
		ContinuationRuntimeRevisionVersion: 1, ContinuationInputSHA256: digest,
	}
	ownerGate := base
	ownerGate.ContinuationKind = enum.ProcessContinuationOwnerGate
	ownerGate.ContinuationGateID = "ca9787b5-0ebf-44bb-bdb5-64b4f35c1713"
	ownerGate.OwnerFeedbackSHA256 = digest
	if err := ownerGate.Validate(); err != nil {
		t.Fatalf("valid owner gate continuation rejected: %v", err)
	}
	integration := base
	integration.ContinuationKind = enum.ProcessContinuationIntegration
	integration.ContinuationIntegrationID = "c27fc37f-c9ec-4c95-a307-101f30d3bc97"
	integration.ContinuationOutcomeSHA256 = digest
	if err := integration.Validate(); err != nil {
		t.Fatalf("valid integration continuation rejected: %v", err)
	}
	missingKind := integration
	missingKind.ContinuationKind = enum.ProcessContinuationNone
	if err := missingKind.Validate(); err == nil {
		t.Fatal("integration continuation without discriminator was accepted")
	}
	incomplete := integration
	incomplete.ContinuationOutcomeSHA256 = ""
	if err := incomplete.Validate(); err == nil {
		t.Fatal("incomplete integration continuation was accepted")
	}
	integration.ContinuationGateID = ownerGate.ContinuationGateID
	if err := integration.Validate(); err == nil {
		t.Fatal("mixed owner gate and integration continuation was accepted")
	}
}

func TestScheduledGraphCanonicalLockOrder(t *testing.T) {
	want := []string{
		"runtime_execution", "schedule_occurrence", "schedule", "scheduled_run",
		"session", "turn", "process_run", "integration_continuation",
	}
	if !slices.Equal(scheduledGraphLockOrder[:], want) {
		t.Fatalf("unexpected scheduled graph lock order: %v", scheduledGraphLockOrder)
	}
}

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

func TestIntegrationDeliveryRetryRebindsImmutableOutcome(t *testing.T) {
	now := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
	oldInput := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	newInput := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	turnID := "a189a33f-fea7-4d20-96f0-b5a05c6a5c5c"
	processID := "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526"
	sessionID := "fd0570db-07c9-4a9a-8d35-3657119068c3"
	oldRevisionID := "1373ea94-fdda-47f7-adbe-7ae3bc633c03"
	newRevisionID := "8bdfe85e-8ddf-4904-b139-bfa9139df42e"
	previous := RuntimeExecution{
		ProcessID: processID, SessionID: sessionID, TurnID: turnID, Attempt: 1,
		RuntimeRevisionID: oldRevisionID, RuntimeRevisionVersion: 4,
		ImmutableInputSHA256: oldInput,
	}
	base := IntegrationContinuation{
		ProcessID: processID, SessionID: sessionID,
		ApprovalState: "APPROVED", ExecutionState: "FAILED",
		ContinuationState: "READY", Version: 7, Fence: 9,
		ContinuationTurnID: turnID, ContinuationTurnVersion: 3,
		ContinuationAttempt: 1, ContinuationRuntimeRevisionID: oldRevisionID,
		ContinuationRuntimeRevisionVersion: 4, ContinuationInputSHA256: oldInput,
	}
	retried := entity.Resource{ID: turnID, Version: 4}
	retriedSpec := entity.TurnSpec{
		SessionID: sessionID, ProcessRunID: processID, Attempt: 2,
		RuntimeRevisionID: newRevisionID, EffectiveInputSHA256: newInput,
	}
	revision := entity.Resource{
		ID: newRevisionID, Kind: enum.KindRuntimeRevision,
		State: enum.StateActive, Version: 1,
	}
	for _, previousState := range []string{"READY", "REJOINED"} {
		continuation := base
		continuation.ContinuationState = previousState
		rebound, err := rebindIntegrationDelivery(
			continuation, previous, retried, retriedSpec, revision, now,
		)
		if err != nil {
			t.Fatalf("%s delivery rebind failed: %v", previousState, err)
		}
		if rebound.ContinuationState != "READY" || rebound.Version != 8 ||
			rebound.Fence != 10 || rebound.ContinuationAttempt != 2 ||
			rebound.ContinuationRuntimeRevisionID != newRevisionID ||
			rebound.ContinuationInputSHA256 != newInput {
			t.Fatalf("unexpected rebound delivery: %#v", rebound)
		}
	}
	stale := base
	stale.ContinuationAttempt = 2
	if _, err := rebindIntegrationDelivery(
		stale, previous, retried, retriedSpec, revision, now,
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("stale delivery binding returned %v", err)
	}
}

func TestScheduledExecutionMaySuspendExternal(t *testing.T) {
	for _, states := range [][2]string{{"CLAIMED", "CLAIMED"}, {"CONTINUATION", "CONTINUATION"}} {
		if !scheduledExecutionMaySuspendExternal(states[0], states[1]) {
			t.Fatalf("scheduled graph %v must suspend atomically", states)
		}
	}
	for _, states := range [][2]string{{"CLAIMED", "CONTINUATION"}, {"FAILED", "FAILED"}, {"WAITING_OWNER", "WAITING_OWNER"}} {
		if scheduledExecutionMaySuspendExternal(states[0], states[1]) {
			t.Fatalf("incoherent scheduled graph %v must fail closed", states)
		}
	}
}
