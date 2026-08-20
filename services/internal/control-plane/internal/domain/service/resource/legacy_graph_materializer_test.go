package resource

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	legacyTestSHA      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	legacyTestOtherSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func legacyTestSource(localRef string) entity.LegacyOperationSource {
	return entity.LegacyOperationSource{
		SourceTable:    "matter_codex_projects",
		SourceRef:      "source-" + localRef,
		SourceRevision: 1,
		SourceSHA256:   legacyTestSHA,
		LocalRef:       localRef,
	}
}

func TestLegacyOperationOrderRejectsDependencyRegression(t *testing.T) {
	operations := []entity.LegacyGraphOperation{
		{Project: &entity.LegacyProjectInput{Source: legacyTestSource("project")}},
		{RoleDefinition: &entity.LegacyRoleDefinitionInput{Source: legacyTestSource("role")}},
		{Artifact: &entity.LegacyArtifactInput{Source: legacyTestSource("artifact")}},
	}
	if !errors.Is(validateLegacyOperationOrder(operations), errs.ErrInvalidInput) {
		t.Fatal("operation with a lower dependency rank must be rejected")
	}
}

func TestLegacyMaterializedSourceCountsRejectPartialPlan(t *testing.T) {
	expected := map[string]uint64{"matter_codex_projects": 2}
	actual := map[string]uint64{"matter_codex_projects": 1}
	if !errors.Is(validateLegacyMaterializedSourceCounts(expected, actual), errs.ErrFailedPrecondition) {
		t.Fatal("partial materialized source table must be rejected")
	}
}

func TestLegacySourceSnapshotIsCanonicalAndExact(t *testing.T) {
	dispositions := []entity.LegacySourceDisposition{
		{SourceTable: "matter_codex_projects", Disposition: entity.LegacyDispositionMaterialize,
			RowCount: 1, SourceSHA256: legacyTestSHA},
		{SourceTable: "matter_codex_work_claims", Disposition: entity.LegacyDispositionRejectNonempty,
			SourceSHA256: legacyTestOtherSHA},
	}
	first, err := legacySourceSnapshotSHA256(dispositions)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(dispositions)
	second, err := legacySourceSnapshotSHA256(dispositions)
	if err != nil || second != first {
		t.Fatal("source snapshot must be independent of disposition order")
	}
	dispositions[1].RowCount = 2
	third, err := legacySourceSnapshotSHA256(dispositions)
	if err != nil || third == first {
		t.Fatal("source snapshot must bind exact row count")
	}
}

func TestLegacySemanticPlanRejectsClientTargetWithoutMutatingInput(t *testing.T) {
	plan := entity.LegacyGraphPlan{Operations: []entity.LegacyGraphOperation{{
		TargetID: "11111111-1111-4111-8111-111111111111",
		Project:  &entity.LegacyProjectInput{Source: legacyTestSource("project")},
	}}}
	if _, err := legacySemanticPlan(plan); !errors.Is(err, errs.ErrInvalidInput) {
		t.Fatal("client-owned target identifier must be rejected")
	}
	if plan.Operations[0].TargetID == "" {
		t.Fatal("semantic plan preparation must not mutate the caller input")
	}
}

func TestLegacyProviderObservationProducesValidReferenceAndPoolBinding(t *testing.T) {
	at := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	reference := entity.ProviderConnectionReferenceSpec{
		StableKey: "openai-main", Provider: "openai", ServerReference: "provider://openai/openai-main",
		ReferenceVersion: 1, ReferenceGeneration: 1, ReferenceSHA256: legacyTestSHA,
		MaskedLabel: "OpenAI main", MaskedStatus: "AVAILABLE", Capabilities: []string{"chat"}, Eligible: true,
		ReceiptID: "11111111-1111-4111-8111-111111111111", ReceiptVersion: 1,
		ReceiptSHA256: legacyTestSHA, CredentialBindingID: "22222222-2222-4222-8222-222222222222",
		CredentialBindingVersion: 1, CredentialBindingSHA256: legacyTestOtherSHA,
	}
	credential := entity.CredentialBindingSpec{
		ProviderEligible: true, ProviderObservedUsage: 7, ProviderObservedLimit: 100,
		ProviderObservationRevision: 3, ProviderObservedAt: at,
	}
	if err := applyLegacyProviderObservation(&reference, credential, legacyTestOtherSHA); err != nil {
		t.Fatalf("apply legacy provider observation: %v", err)
	}
	if err := reference.Validate(); err != nil {
		t.Fatalf("provider reference is invalid: %v", err)
	}
	binding := entity.ProviderPoolBinding{
		ProviderConnectionReferenceID: reference.CredentialBindingID,
		ProviderConnectionStableKey:   reference.StableKey,
		ReferenceVersion:              reference.ReferenceVersion,
		ReferenceSHA256:               reference.ReferenceSHA256,
		Weight:                        100,
		Eligible:                      reference.Eligible,
		MaskedStatus:                  reference.MaskedStatus,
		ObservedUsage:                 reference.ObservedUsage,
		ObservedLimit:                 reference.ObservedLimit,
		ObservationRevision:           reference.ObservationRevision,
		ObservedAt:                    reference.ObservedAt,
		ObservationExpiresAt:          reference.ObservationExpiresAt,
		ObservationSHA256:             reference.ObservationSHA256,
		WindowDurationSeconds:         reference.WindowDurationSeconds,
		ResetsAt:                      reference.ResetsAt,
	}
	pool := entity.ProviderPoolSpec{
		StableKey: "manager", Policy: "weighted", PolicyRevision: 1,
		ObservationMaxAge: 24 * time.Hour, Bindings: []entity.ProviderPoolBinding{binding},
		EligibilitySnapshotSHA256: legacyTestSHA,
		Ownership:                 entity.ConfigurationOwnership{ManagedBy: "UI"},
	}
	if err := pool.Validate(); err != nil {
		t.Fatalf("provider pool is invalid: %v", err)
	}
}

func TestLegacyLifecycleRejectsClaimedAttemptWithoutLease(t *testing.T) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	operations := legacyLineageFixture(at)
	operations[3].TurnAttempt.State = string(enum.StateClaimed)
	operations[3].TurnAttempt.FinishedAt = time.Time{}
	if !errors.Is(validateLegacyLifecycleAndLineage(operations), errs.ErrFailedPrecondition) {
		t.Fatal("CLAIMED historical attempt without an owner lease must be rejected")
	}
}

func TestLegacyLifecycleRejectsProcessProvenanceMismatch(t *testing.T) {
	operations := legacyDelegationFixture(time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC))
	if err := validateLegacyLifecycleAndLineage(operations); err != nil {
		t.Fatalf("valid inherited child lineage rejected: %v", err)
	}
	operations[len(operations)-2].ProcessRun.RootSessionRef = "child-session"
	if !errors.Is(validateLegacyLifecycleAndLineage(operations), errs.ErrFailedPrecondition) {
		t.Fatal("child process must inherit its exact parent root lineage")
	}
}

func TestLegacyCallbackRequiresTerminalDeliveryForEveryDestination(t *testing.T) {
	at := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	operations := append(legacyLineageFixture(at),
		entity.LegacyGraphOperation{CallbackManifest: &entity.LegacyCallbackManifestInput{
			Source: legacyTestSource("manifest"), DelegationRef: "delegation",
			CallbackProcessRef: "process", ManifestSHA256: legacyTestSHA,
			Destinations: []string{"one", "two"},
		}},
		entity.LegacyGraphOperation{CallbackDelivery: &entity.LegacyCallbackDeliveryInput{
			Source: legacyTestSource("delivery-one"), CallbackManifestRef: "manifest",
			Destination: "one", ReceiptSHA256: legacyTestSHA,
			TerminalState: "DELIVERED", DeliveredAt: at,
		}},
	)
	if !errors.Is(validateLegacyLifecycleAndLineage(operations), errs.ErrFailedPrecondition) {
		t.Fatal("partial callback terminal envelope must be rejected")
	}
}

func TestLegacyCallbackReceiptUsesLifecycleState(t *testing.T) {
	state, err := legacyCallbackReceiptState("DELIVERED")
	if err != nil || state != enum.StateSucceeded {
		t.Fatalf("DELIVERED callback must map to SUCCEEDED receipt, got %q: %v", state, err)
	}
	if _, err := legacyCallbackReceiptState("UNKNOWN"); !errors.Is(err, errs.ErrInvalidInput) {
		t.Fatal("unknown callback state must be rejected")
	}
}

func TestLegacyEvidenceFailureCodeIdentifiesFirstFailedPredicate(t *testing.T) {
	receipt := domainrepo.LegacyOperationRecord{TargetKind: "AGENT", Ordinal: 42}
	evidence := domainrepo.LegacyOperationEvidence{Audit: true, Events: true, Target: true}
	if code := legacyEvidenceFailureCode(receipt, evidence); code != "LEGACY_EVIDENCE_PROVENANCE_AGENT_42" {
		t.Fatalf("unexpected safe evidence code: %s", code)
	}
}

func TestLegacyDriftFailureCodeIdentifiesProjectionOrdinal(t *testing.T) {
	drift := []entity.LegacyGraphDrift{{Ordinal: 17, Predicate: "target projection does not match committed receipt"}}
	if code := legacyDriftFailureCode(drift); code != "LEGACY_DRIFT_TARGET_PROJECTION_17" {
		t.Fatalf("unexpected safe drift code: %s", code)
	}
}

func TestPersistedLegacyOperationIntentDetectsExactReplayDrift(t *testing.T) {
	operation := entity.LegacyGraphOperation{
		TargetID: "11111111-1111-4111-8111-111111111111",
		Project: &entity.LegacyProjectInput{
			Source: legacyTestSource("project"), Name: "Legacy", Slug: "legacy", Locale: "ru",
		},
	}
	plan := entity.LegacyGraphPlan{
		PlanID:              "22222222-2222-4222-8222-222222222222",
		SourceRootReference: "33333333-3333-4333-8333-333333333333",
		SourceRootSHA256:    legacyTestSHA,
		Operations:          []entity.LegacyGraphOperation{operation},
	}
	withoutTarget := operation
	withoutTarget.TargetID = ""
	inputSHA256, err := canonicalHash(withoutTarget)
	if err != nil {
		t.Fatal(err)
	}
	lineageSHA256, err := canonicalHash(struct {
		RootReference, RootSHA256 string
		Source                    entity.LegacyOperationSource
		Kind                      string
		Operation                 entity.LegacyGraphOperation
	}{plan.SourceRootReference, plan.SourceRootSHA256, operation.Project.Source, "PROJECT", withoutTarget})
	if err != nil {
		t.Fatal(err)
	}
	receipt := domainrepo.LegacyOperationRecord{
		PlanID: plan.PlanID, Ordinal: 1, OperationKind: "PROJECT", TargetKind: "PROJECT",
		TargetID: operation.TargetID, InputSHA256: inputSHA256, ProvenanceSHA256: lineageSHA256,
	}
	if err := validatePersistedLegacyOperationIntents(plan, []domainrepo.LegacyOperationRecord{receipt}); err != nil {
		t.Fatalf("exact immutable replay rejected: %v", err)
	}
	receipt.InputSHA256 = legacyTestOtherSHA
	if !errors.Is(validatePersistedLegacyOperationIntents(plan, []domainrepo.LegacyOperationRecord{receipt}), errs.ErrDataLoss) {
		t.Fatal("drifted persisted intent must fail closed")
	}
}

func TestLegacyPlanOwnerHidesCrossTenantReference(t *testing.T) {
	principal := value.Principal{
		OrganizationID:     "11111111-1111-4111-8111-111111111111",
		ActorID:            "22222222-2222-4222-8222-222222222222",
		AuthorityReference: "33333333-3333-4333-8333-333333333333",
		AuthorityDigest:    legacyTestSHA,
	}
	record := domainrepo.LegacyGraphPlanRecord{
		OrganizationID: "44444444-4444-4444-8444-444444444444",
		OwnerActorID:   principal.ActorID, SourceRootReference: principal.AuthorityReference,
		SourceRootSHA256: principal.AuthorityDigest,
	}
	if !errors.Is(requireLegacyPlanOwner(principal, record), errs.ErrNotFound) {
		t.Fatal("foreign organization must be hidden as not found")
	}
}

func legacyLineageFixture(at time.Time) []entity.LegacyGraphOperation {
	operations := []entity.LegacyGraphOperation{
		{Session: &entity.LegacySessionInput{Source: legacyTestSource("session")}},
		{RuntimeRevision: &entity.LegacyRuntimeRevisionInput{
			Source: legacyTestSource("runtime"), SessionRef: "session",
		}},
		{Turn: &entity.LegacyTurnInput{
			Source: legacyTestSource("turn"), SessionRef: "session", RuntimeRevisionRef: "runtime",
			Sequence: 1, Attempt: 1, EffectiveInputSHA256: legacyTestSHA,
		}},
		{TurnAttempt: &entity.LegacyTurnAttemptInput{
			Source: legacyTestSource("attempt"), TurnRef: "turn", RuntimeRevisionRef: "runtime",
			Attempt: 1, ImmutableInputSHA256: legacyTestSHA, State: string(enum.StateBlocked),
			Outcome: "cutover", StartedAt: at, FinishedAt: at,
		}},
		{ProcessRun: &entity.LegacyProcessRunInput{
			Source: legacyTestSource("process"), RootSessionRef: "session", RootTurnRef: "turn",
			RootAttemptRef: "attempt", RuntimeRevisionRef: "runtime", ImmutableInputSHA256: legacyTestSHA,
			LegacyPolicyRevision: 1, LegacyPolicySHA256: legacyTestSHA, State: enum.StateBlocked,
		}},
	}
	return operations
}

func legacyDelegationFixture(at time.Time) []entity.LegacyGraphOperation {
	operations := legacyLineageFixture(at)
	return append(operations,
		entity.LegacyGraphOperation{Agent: &entity.LegacyAgentInput{
			Source: legacyTestSource("child-agent"),
		}},
		entity.LegacyGraphOperation{Session: &entity.LegacySessionInput{
			Source: legacyTestSource("child-session"), AgentRef: "child-agent",
		}},
		entity.LegacyGraphOperation{RuntimeRevision: &entity.LegacyRuntimeRevisionInput{
			Source: legacyTestSource("child-runtime"), SessionRef: "child-session",
		}},
		entity.LegacyGraphOperation{Turn: &entity.LegacyTurnInput{
			Source: legacyTestSource("child-turn"), SessionRef: "child-session",
			RuntimeRevisionRef: "child-runtime", Sequence: 1, Attempt: 1,
			EffectiveInputSHA256: legacyTestOtherSHA,
		}},
		entity.LegacyGraphOperation{TurnAttempt: &entity.LegacyTurnAttemptInput{
			Source: legacyTestSource("child-attempt"), TurnRef: "child-turn",
			RuntimeRevisionRef: "child-runtime", Attempt: 1,
			ImmutableInputSHA256: legacyTestOtherSHA, State: string(enum.StateBlocked),
			Outcome: "delegated", StartedAt: at, FinishedAt: at,
		}},
		entity.LegacyGraphOperation{ProcessRun: &entity.LegacyProcessRunInput{
			Source: legacyTestSource("child-process"), RootSessionRef: "session", RootTurnRef: "turn",
			RootAttemptRef: "attempt", RuntimeRevisionRef: "child-runtime", ParentProcessRef: "process",
			LaunchingTurnRef: "turn", LaunchingAttemptRef: "attempt", DelegationRef: "delegation",
			TargetSessionRef: "child-session", TargetTurnRef: "child-turn", TargetAttemptRef: "child-attempt",
			ImmutableInputSHA256: legacyTestOtherSHA, LegacyPolicyRevision: 1,
			LegacyPolicySHA256: legacyTestSHA, State: enum.StateBlocked,
		}},
		entity.LegacyGraphOperation{DelegationEdge: &entity.LegacyDelegationEdgeInput{
			Source: legacyTestSource("delegation"), ParentProcessRef: "process",
			ParentSessionRef: "session", ParentTurnRef: "turn", ParentAttemptRef: "attempt",
			ChildRoleRef: "child-agent", ChildSessionRef: "child-session", ChildTurnRef: "child-turn",
			ChildAttemptRef: "child-attempt", ChildProcessRef: "child-process", GrantGeneration: 1,
		}},
	)
}
