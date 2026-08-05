package resource

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestLifecycleReceiptValidationPrecedesLookup(t *testing.T) {
	content, err := os.ReadFile("runtime_continuation.go")
	if err != nil {
		t.Fatalf("read lifecycle source: %v", err)
	}
	source := string(content)
	start := strings.Index(source, "func (service *Service) withLifecycleReceipt(")
	end := strings.Index(source[start:], "\nfunc (service *Service) appendLifecycleAudit(")
	if start < 0 || end < 0 {
		t.Fatal("lifecycle receipt wrapper was not found")
	}
	body := source[start : start+end]
	validated := strings.Index(body, "disposition, err := validate(tx)")
	lookup := strings.Index(body, "tx.GetReceipt(")
	if validated < 0 || lookup < 0 || validated > lookup {
		t.Fatal("receipt lookup is reachable before authoritative validation")
	}
}

func productionFunctionSource(t *testing.T, fileName, functionName string) string {
	t.Helper()
	content, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("read %s: %v", fileName, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, fileName, content, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", fileName, err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != functionName {
			continue
		}
		start := fset.Position(function.Pos()).Offset
		end := fset.Position(function.End()).Offset
		return string(content[start:end])
	}
	t.Fatalf("function %s was not found in %s", functionName, fileName)
	return ""
}

func TestRemediationEntryPointsKeepCriticalGuards(t *testing.T) {
	manageSession := productionFunctionSource(t, "cycle_two.go", "ManageSession")
	for _, guard := range []string{
		"lockSessionLifecycleGraph", "SessionHasLiveRuntimeExecution",
		"IntegrationContinuationBlocksCleanup", "withValidatedResourceReceipt",
	} {
		if !strings.Contains(manageSession, guard) {
			t.Fatalf("ManageSession guard %s is absent", guard)
		}
	}

	for _, functionName := range []string{"AdmitRuntimeExecution", "HeartbeatRuntimeExecution"} {
		source := productionFunctionSource(t, "runtime_continuation.go", functionName)
		if !strings.Contains(source, "requireActiveRuntimeLeaseGraph") ||
			!strings.Contains(source, "RenewTurnLease") {
			t.Fatalf("%s does not renew the exact two-part runtime lease", functionName)
		}
	}

	workClaim := productionFunctionSource(t, "cycle_two.go", "ManageWorkClaim")
	if !strings.Contains(workClaim, "tx.CurrentTime") ||
		!strings.Contains(workClaim, "requireUnexpiredWorkClaim") {
		t.Fatal("ManageWorkClaim can classify RENEW without PostgreSQL-clock expiry")
	}

	ownerGate := productionFunctionSource(t, "specialized.go", "RequestOwnerGate")
	deadline := strings.Index(ownerGate, "requireOwnerGateSuspensionLease")
	receipt := strings.Index(ownerGate, "tx.GetReceipt")
	if deadline < 0 || receipt < 0 || deadline > receipt {
		t.Fatal("RequestOwnerGate reads receipt before exact lease deadline validation")
	}

	manageSchedule := productionFunctionSource(t, "specialized.go", "ManageSchedule")
	scheduleLock := strings.Index(manageSchedule, "scheduleMutationRequiresClosedGraph")
	pinnedLock := strings.Index(manageSchedule, "input.Spec.TargetResourceID")
	if scheduleLock < 0 || pinnedLock < 0 || scheduleLock > pinnedLock {
		t.Fatal("ManageSchedule can lock pinned resource before Schedule/open-graph validation")
	}
	if !strings.Contains(manageSchedule, "withValidatedResourceReceipt") {
		t.Fatal("ManageSchedule existing-row action can expose receipt before owner validation")
	}
}

func TestPublicGraphEntryPointsDoNotLockSharedRowsBeforeResolver(t *testing.T) {
	checks := []struct {
		file     string
		function string
		resolver string
	}{
		{"cycle_two.go", "ManageWorkClaim", "lockOwnerGraphByTurn"},
		{"specialized.go", "StartProcess", "lockOwnerGraphSet"},
		{"runtime.go", "EnqueueTurn", "lockOwnerGraphSet"},
		{"specialized.go", "CompleteProcess", "lockOwnerGraphByProcess"},
		{"specialized.go", "CancelProcess", "lockOwnerGraphByProcess"},
	}
	for _, check := range checks {
		t.Run(check.function, func(t *testing.T) {
			source := productionFunctionSource(t, check.file, check.function)
			resolver := strings.Index(source, check.resolver)
			manualLock := strings.Index(source, "tx.GetForUpdate")
			if resolver < 0 || (manualLock >= 0 && manualLock < resolver) {
				t.Fatalf("%s can lock a shared row before %s", check.function, check.resolver)
			}
		})
	}
}

func TestBatchOwnerGraphResolverKeepsGlobalAcquisitionOrder(t *testing.T) {
	source := productionFunctionSource(t, "owner_graph_lock.go", "lockOwnerGraphSet")
	calls := []string{
		"GetRuntimeExecutionByTurnForUpdate", "GetScheduleOccurrenceForUpdate",
		"GetForUpdate(\n\t\t\tctx, principal.OrganizationID, principal.ProjectID, scheduleID",
		"GetScheduledRunForUpdate",
		"GetForUpdate(\n\t\t\tctx, principal.OrganizationID, principal.ProjectID, sessionID",
		"GetForUpdate(\n\t\t\tctx, principal.OrganizationID, principal.ProjectID, turnID",
		"GetForUpdate(\n\t\t\tctx, principal.OrganizationID, principal.ProjectID, processID",
	}
	previous := -1
	for _, call := range calls {
		position := strings.Index(source, call)
		if position < 0 || position <= previous {
			t.Fatalf("batch owner graph order is broken at %s", call)
		}
		previous = position
	}
	postLockAbsenceCheck := strings.LastIndex(source, "tx.GetRuntimeExecutionByTurn(")
	if postLockAbsenceCheck <= previous {
		t.Fatal("runtime absence is not rechecked after Session/Turn/Process locks")
	}
}

func TestLeaseAndExpiryPredicatesFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	claim := entity.WorkClaimSpec{ExpiresAt: now.Add(time.Second)}
	if err := requireUnexpiredWorkClaim(claim, now); err != nil {
		t.Fatalf("live work claim was rejected: %v", err)
	}
	claim.ExpiresAt = now
	if err := requireUnexpiredWorkClaim(claim, now); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("expired work claim returned %v", err)
	}

	turnLease := domainrepo.TurnLease{ExpiresAt: now.Add(time.Minute)}
	pending := RuntimeExecution{State: "PENDING"}
	if err := requireOwnerGateSuspensionLease(&pending, turnLease, now); err != nil {
		t.Fatalf("live pending runtime was rejected: %v", err)
	}
	leased := RuntimeExecution{
		State: "RUNNING", LeaseID: "lease",
		LeaseTokenSHA256: strings.Repeat("a", 64),
		LeaseExpiresAt:   turnLease.ExpiresAt,
	}
	if err := requireOwnerGateSuspensionLease(&leased, turnLease, now); err != nil {
		t.Fatalf("coherent runtime lease was rejected: %v", err)
	}
	leased.LeaseExpiresAt = now
	if err := requireOwnerGateSuspensionLease(
		&leased, turnLease, now,
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("expired runtime suspension returned %v", err)
	}
}

func TestDeadlineSensitiveAuthorityUsesPostLockDatabaseTime(t *testing.T) {
	checks := []struct {
		file      string
		function  string
		lock      string
		clock     string
		receipt   string
		lastLock  string
		lastClock bool
	}{
		{"runtime.go", "ClaimTurn", "tx.GetTurnLeaseForUpdate", "tx.CurrentTime", "", "tx.NextQueuedTurn", true},
		{"runtime.go", "RenewTurn", "tx.GetTurnLeaseForUpdate", "tx.CurrentTime", "tx.GetReceipt", "", false},
		{"specialized.go", "RequestOwnerGate", "tx.GetTurnLeaseForUpdate", "tx.CurrentTime", "tx.GetReceipt", "", false},
		{"specialized.go", "claimScheduleOccurrence", "tx.NextScheduleOccurrence", "tx.CurrentTime", "", "", true},
		{"specialized.go", "replayScheduleOccurrenceClaim", "service.lockOwnerGraphByTurn", "tx.CurrentTime", "tx.GetReceipt", "", false},
		{"runtime_continuation.go", "integrationSessionContext", "tx.GetForUpdate", "tx.CurrentTime", "", "", true},
		{"runtime_continuation.go", "resolveSelectedIntegrationBinding", "tx.GetForUpdate", "tx.CurrentTime", "", "", true},
		{"runtime_continuation.go", "validatePinnedIntegrationContinuation", "tx.GetForUpdate", "tx.CurrentTime", "", "", true},
	}
	for _, check := range checks {
		t.Run(check.function, func(t *testing.T) {
			source := productionFunctionSource(t, check.file, check.function)
			lock := strings.Index(source, check.lock)
			clock := strings.Index(source, check.clock)
			if check.lastClock {
				clock = strings.LastIndex(source, check.clock)
			}
			if lock < 0 || clock < 0 || lock > clock {
				t.Fatalf("%s can read decision time before authoritative row lock", check.function)
			}
			if check.lastLock != "" {
				lastLock := strings.LastIndex(source, check.lastLock)
				if lastLock < 0 || lastLock > clock {
					t.Fatalf("%s does not refresh decision time after %s", check.function, check.lastLock)
				}
			}
			if check.receipt != "" {
				receipt := strings.Index(source, check.receipt)
				if receipt < 0 || clock > receipt {
					t.Fatalf("%s can expose a receipt before post-lock deadline validation", check.function)
				}
			}
		})
	}

	workClaim := productionFunctionSource(t, "cycle_two.go", "ManageWorkClaim")
	if strings.Count(workClaim, "tx.CurrentTime(ctx)") != 3 {
		t.Fatal("ManageWorkClaim must refresh the decision clock after every exact claim lock")
	}
	nonCreate := strings.Index(workClaim, "candidate, err := tx.Get(")
	rowLock := strings.Index(workClaim[nonCreate:], "current, err := tx.GetForUpdate(")
	clock := strings.Index(workClaim[nonCreate:], "workClaimNow, err = tx.CurrentTime(ctx)")
	if nonCreate < 0 || rowLock < 0 || clock < 0 || rowLock > clock {
		t.Fatal("WorkClaim RENEW/RELEASE decision time is not after exact claim row lock")
	}
	createLock := strings.Index(workClaim, "service.lockOwnerGraphByTurn")
	createClock := strings.Index(workClaim, "workClaimNow, err = tx.CurrentTime(ctx)")
	if createLock < 0 || createClock < 0 || createLock > createClock {
		t.Fatal("WorkClaim CREATE decision time is not after canonical graph lock")
	}
	replayLock := strings.Index(workClaim, "func(tx domainrepo.Transaction, stored entity.Resource) error")
	replayClock := strings.Index(workClaim[replayLock:], "replayNow, err := tx.CurrentTime(ctx)")
	replayClaimLock := strings.Index(workClaim[replayLock:], "current, err := tx.GetForUpdate(")
	if replayLock < 0 || replayClaimLock < 0 || replayClock < 0 || replayClaimLock > replayClock {
		t.Fatal("WorkClaim CREATE/RENEW receipt replay time is not after stored claim lock")
	}
	for _, claim := range []struct {
		file     string
		function string
		lock     string
	}{
		{"runtime.go", "ClaimTurn", "tx.GetTurnLeaseForUpdate"},
		{"specialized.go", "replayScheduleOccurrenceClaim", "service.lockOwnerGraphByTurn"},
	} {
		source := productionFunctionSource(t, claim.file, claim.function)
		lock := strings.Index(source, claim.lock)
		clock := strings.Index(source, "tx.CurrentTime(ctx)")
		receipt := strings.Index(source, "tx.GetReceipt")
		if lock < 0 || clock < lock || receipt < clock {
			t.Fatalf("%s can expose a receipt before post-lock deadline validation", claim.function)
		}
	}
}

func TestSchedulerMaintenanceCommitsBeforeCandidateSelection(t *testing.T) {
	claim := productionFunctionSource(t, "specialized.go", "claimScheduleOccurrence")
	for _, required := range []string{
		"service.recoverExpiredScheduleOccurrences",
		"service.skipOverlappedScheduleOccurrences",
		"tx.NextScheduleOccurrence",
		"tx.HasBlockingScheduleExecution",
	} {
		if !strings.Contains(claim, required) {
			t.Fatalf("scheduler claim misses %s", required)
		}
	}
	for _, forbidden := range []string{
		"tx.ExpiredScheduleOccurrenceCandidates",
		"tx.SkipOverlappedScheduleOccurrences",
		"service.recoverExpiredScheduleOccurrence(",
	} {
		if strings.Contains(claim, forbidden) {
			t.Fatalf("scheduler maintenance remains inside candidate transaction: %s", forbidden)
		}
	}
	recovery := productionFunctionSource(t, "specialized.go", "recoverExpiredScheduleOccurrences")
	if strings.Count(recovery, "service.repository.Transact") < 2 ||
		!strings.Contains(recovery, "service.recoverExpiredScheduleOccurrence") {
		t.Fatal("watchdog discovery and exact disposition do not have independent commits")
	}
	skip := productionFunctionSource(t, "specialized.go", "skipOverlappedScheduleOccurrences")
	if !strings.Contains(skip, "service.repository.Transact") ||
		!strings.Contains(skip, "appendScheduleOccurrenceAudit") {
		t.Fatal("overlap skip and audit are not an independent atomic fact")
	}
	next := strings.Index(claim, "tx.NextScheduleOccurrence")
	if next < 0 {
		t.Fatal("scheduler selection is absent")
	}
	scheduleLock := strings.Index(claim[next:], "tx.GetForUpdate")
	cardinality := strings.Index(claim[next:], "tx.HasBlockingScheduleExecution")
	firstEffect := strings.Index(claim[next:], "service.prepareScheduleSession")
	if scheduleLock < 0 || cardinality < scheduleLock ||
		firstEffect < 0 || cardinality > firstEffect {
		t.Fatal("schedule cardinality is not rechecked after schedule lock and before graph effects")
	}
}

func TestIntegrationMaterializationReplacesPredecessorTurn(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	organizationID := "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526"
	projectID := "fd0570db-07c9-4a9a-8d35-3657119068c3"
	actorID := "5574792c-5721-4b85-83b7-e8c6857b8fef"
	sessionID := "1373ea94-fdda-47f7-adbe-7ae3bc633c03"
	turnID := "8bdfe85e-8ddf-4904-b139-bfa9139df42e"
	processID := "e910cf2c-702b-4f8a-806f-6cfd094696cd"
	revisionID := "ca9787b5-0ebf-44bb-bdb5-64b4f35c1713"
	continuationID := "c27fc37f-c9ec-4c95-a307-101f30d3bc97"
	digest := strings.Repeat("a", 64)
	requestDigest := strings.Repeat("b", 64)

	turn, err := entity.New(
		turnID, organizationID, projectID, sessionID, actorID, enum.KindTurn,
		"Integration source turn",
		entity.TurnSpec{
			SessionID: sessionID, Sequence: 1, SourceRef: "integration:test",
			PromptArtifactID:  "3f1c3ac0-cd38-4e83-a7ae-68a03df08a96",
			RuntimeRevisionID: revisionID, ProcessRunID: processID, Attempt: 1,
			EffectiveInputSHA256: digest,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create source turn: %v", err)
	}
	turn, err = turn.Transition(enum.StateClaimed, now.Add(time.Second))
	if err != nil {
		t.Fatalf("claim source turn: %v", err)
	}
	turn, err = turn.Transition(enum.StateWaitingExternal, now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("suspend source turn: %v", err)
	}
	continuation := IntegrationContinuation{
		ID: continuationID, OrganizationID: organizationID, ProjectID: projectID,
		ProcessID: processID, SessionID: sessionID,
		ThreadID: sessionID, RoleID: actorID, TurnID: turnID,
		TurnVersion: turn.Version, Attempt: 1, RuntimeRevisionID: revisionID,
		RuntimeRevisionVersion: 4, RuntimeRevisionSHA256: digest,
		ImmutableInputSHA256: digest,
		GrantGeneration:      7, RequestSHA256: requestDigest,
		ContinuationState: "SUSPENDED",
	}
	runtime := RuntimeExecution{
		OrganizationID: organizationID, ProjectID: projectID, ProcessID: processID,
		SessionID: sessionID, ThreadID: sessionID, RoleID: actorID,
		TurnID: turnID, Attempt: 1,
		RuntimeRevisionID: revisionID, RuntimeRevisionVersion: 4,
		RuntimeRevisionSHA256: digest, ImmutableInputSHA256: digest, GrantGeneration: 7,
		WorkloadID:       "runtime-controller",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		State:            "SUSPENDED",
		TerminalOutcome:  "SUSPENDED", TerminalReference: continuationID,
		TerminalSHA256: requestDigest,
	}
	attempt := domainrepo.TurnAttempt{
		TurnID: turnID, Attempt: 1, WorkloadID: agentRunnerWorkload,
		AuthorityGeneration: 7, State: "WAITING_EXTERNAL", InputSHA256: digest,
		LeaseFence: turn.Version - 1, StartedAt: now.Add(time.Second),
		FinishedAt: now.Add(2 * time.Second), Outcome: "integration_approval",
	}

	outcomes := []struct {
		name      string
		approval  string
		execution string
		digest    string
	}{
		{name: "rejected", approval: "REJECTED", execution: "NOT_APPLICABLE", digest: requestDigest},
		{name: "approval expired", approval: "EXPIRED", execution: "NOT_APPLICABLE", digest: requestDigest},
		{name: "pending cancel", approval: "CANCELLED", execution: "NOT_APPLICABLE", digest: requestDigest},
		{name: "approved cancel", approval: "CANCELLED", execution: "NOT_APPLICABLE", digest: requestDigest},
		{name: "execution success", approval: "APPROVED", execution: "SUCCEEDED", digest: digest},
		{name: "execution error", approval: "APPROVED", execution: "FAILED", digest: digest},
	}
	for _, outcome := range outcomes {
		t.Run(outcome.name, func(t *testing.T) {
			candidate := continuation
			candidate.ApprovalState = outcome.approval
			candidate.ExecutionState = outcome.execution
			candidate.DecisionSHA256 = requestDigest
			candidate.ResultSHA256 = digest
			candidate.ErrorSHA256 = digest
			gotDigest, err := integrationMaterializationOutcomeDigest(candidate)
			if err != nil || gotDigest != outcome.digest {
				t.Fatalf("terminal outcome is not materializable: %s %v", gotDigest, err)
			}
			replaced, err := replaceIntegrationPredecessor(
				candidate, runtime, turn, attempt, agentRunnerWorkload,
				"runtime-controller",
				"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
				now.Add(3*time.Second),
			)
			if err != nil {
				t.Fatalf("replace predecessor: %v", err)
			}
			replacedSpec := replaced.Spec.(entity.TurnSpec)
			if replaced.State != enum.StateCancelled || replaced.Version != turn.Version+1 ||
				replacedSpec.Outcome != integrationPredecessorOutcome {
				t.Fatalf("predecessor did not become an immutable replaced terminal: %#v", replaced)
			}
		})
	}
	pending := continuation
	pending.ApprovalState = "PENDING"
	pending.ExecutionState = "NOT_STARTED"
	if _, err := integrationMaterializationOutcomeDigest(pending); !errors.Is(
		err, errs.ErrStateConflict,
	) {
		t.Fatalf("nonterminal outcome was materialized: %v", err)
	}
	alreadyMaterialized := continuation
	alreadyMaterialized.ContinuationTurnID = "35d9336c-1ace-4f1d-822d-021a44a36e5a"
	if _, err := replaceIntegrationPredecessor(
		alreadyMaterialized, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("second successor was accepted: %v", err)
	}

	runtime.LeaseID = "stale-runtime-lease"
	if _, err := replaceIntegrationPredecessor(
		continuation, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("live predecessor authority was accepted: %v", err)
	}
	runtime.LeaseID = ""
	attempt.State = "CLAIMED"
	if _, err := replaceIntegrationPredecessor(
		continuation, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("open predecessor attempt was accepted: %v", err)
	}
	attempt.State = "WAITING_EXTERNAL"
	attempt.WorkloadID = "runtime-controller"
	if _, err := replaceIntegrationPredecessor(
		continuation, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("executor substituted for claimant: %v", err)
	}
	attempt.WorkloadID = agentRunnerWorkload
	attempt.LeaseFence--
	if _, err := replaceIntegrationPredecessor(
		continuation, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("stale claimant lease fence was accepted: %v", err)
	}
	attempt.LeaseFence++
	runtime.WorkloadSPIFFEID = agentRunnerSPIFFEID
	if _, err := replaceIntegrationPredecessor(
		continuation, runtime, turn, attempt, agentRunnerWorkload,
		"runtime-controller",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
		now.Add(3*time.Second),
	); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("claimant SPIFFE substituted for runtime executor: %v", err)
	}
}

func TestIntegrationSuspensionPinsExactCurrentVersions(t *testing.T) {
	now := time.Date(2026, 8, 3, 11, 0, 0, 0, time.UTC)
	organizationID := "3a3ed463-59fe-4a2b-9f96-58cd7d3dd526"
	projectID := "fd0570db-07c9-4a9a-8d35-3657119068c3"
	actorID := "5574792c-5721-4b85-83b7-e8c6857b8fef"
	sessionID := "1373ea94-fdda-47f7-adbe-7ae3bc633c03"
	turnID := "8bdfe85e-8ddf-4904-b139-bfa9139df42e"
	processID := "e910cf2c-702b-4f8a-806f-6cfd094696cd"
	revisionID := "ca9787b5-0ebf-44bb-bdb5-64b4f35c1713"
	digest := strings.Repeat("a", 64)

	for _, scheduled := range []bool{false, true} {
		name := "unscheduled"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			session := entity.Resource{
				ID: sessionID, OrganizationID: organizationID, ProjectID: projectID,
				OwnerActorID: actorID, Kind: enum.KindSession, State: enum.StateActive,
				Version: 5,
			}
			turnSpec := entity.TurnSpec{
				SessionID: sessionID, ProcessRunID: processID, Attempt: 1,
				RuntimeRevisionID: revisionID, EffectiveInputSHA256: digest,
			}
			turn := entity.Resource{
				ID: turnID, OrganizationID: organizationID, ProjectID: projectID,
				ParentID: sessionID, OwnerActorID: actorID, Kind: enum.KindTurn,
				State: enum.StateClaimed, Version: 7, Spec: turnSpec,
			}
			processSpec := entity.ProcessRunSpec{
				PlaybookRef: "playbook:test", PolicyRevision: 1,
				RootTriggerRef: "manual:test", RootInitiatorActorID: actorID,
				RootSessionID: sessionID, RootSessionVersion: session.Version,
				RootTurnID: turnID, RootTurnVersion: turn.Version, RootAttempt: 1,
				ImmutableInputSHA256: digest, RuntimeRevisionID: revisionID,
				CurrentSessionID: sessionID, CurrentSessionVersion: session.Version,
				CurrentTurnID: turnID, CurrentTurnVersion: turn.Version,
				CurrentAttempt: 1, CurrentRuntimeRevisionID: revisionID,
				CurrentRuntimeRevisionVersion: 4, CurrentInputSHA256: digest,
			}
			if scheduled {
				processSpec.ScheduleID = "b6ad5c57-e199-4e47-a93b-d99bb19b21e1"
				processSpec.OccurrenceID = "35d9336c-1ace-4f1d-822d-021a44a36e5a"
				processSpec.RootTriggerRef = "schedule-occurrence:" + processSpec.OccurrenceID
			}
			process := entity.Resource{
				ID: processID, OrganizationID: organizationID, ProjectID: projectID,
				OwnerActorID: actorID, Kind: enum.KindProcessRun, Name: "Process",
				State: enum.StateRunning, Version: 9, Spec: processSpec,
				CreatedAt: now, UpdatedAt: now,
			}
			resolved := resolvedExecution{
				Turn: turn, TurnSpec: turnSpec, Session: session,
				Process: process, ProcessSpec: processSpec,
				Revision: entity.Resource{ID: revisionID, Version: 4},
			}
			suspendedSession := session
			suspendedSession.State = enum.StateWaitingExternal
			suspendedSession.Version++
			suspendedTurn := turn
			suspendedTurn.State = enum.StateWaitingExternal
			suspendedTurn.Version++
			got, err := suspendIntegrationProcessRun(
				resolved, suspendedSession, suspendedTurn, now.Add(time.Second),
			)
			if err != nil {
				t.Fatalf("suspend process: %v", err)
			}
			gotSpec := got.Spec.(entity.ProcessRunSpec)
			current, err := currentExecution(gotSpec)
			if err != nil || got.State != enum.StateWaitingExternal ||
				current.SessionID != sessionID ||
				current.SessionVersion != suspendedSession.Version ||
				current.TurnID != turnID ||
				current.TurnVersion != suspendedTurn.Version ||
				current.RuntimeRevisionID != revisionID ||
				current.RuntimeRevisionVersion != 4 ||
				current.InputSHA256 != digest {
				t.Fatalf("suspended current tuple is stale: %#v %v", current, err)
			}

			stale := resolved
			stale.ProcessSpec.CurrentTurnVersion--
			if _, err := suspendIntegrationProcessRun(
				stale, suspendedSession, suspendedTurn, now.Add(time.Second),
			); !errors.Is(err, errs.ErrStateConflict) {
				t.Fatalf("stale process current tuple was accepted: %v", err)
			}
		})
	}
}

func TestIntegrationMaterializationClosesSourceBeforeSuccessorInsert(t *testing.T) {
	claim := productionFunctionSource(t, "runtime.go", "ClaimTurn")
	if !strings.Contains(claim, "agentRunnerWorkload") ||
		!strings.Contains(claim, "agentRunnerSPIFFEID") {
		t.Fatal("TurnAttempt claimant is not pinned to the exact agent-runner identity")
	}
	suspension := productionFunctionSource(
		t, "runtime_continuation.go", "suspendIntegrationGraph",
	)
	processRebind := strings.Index(suspension, "suspendIntegrationProcessRun")
	leaseRevocation := strings.Index(suspension, "tx.DeleteTurnLease")
	if processRebind < 0 || leaseRevocation < 0 || processRebind > leaseRevocation {
		t.Fatal("integration suspension revokes authority before exact ProcessRun rebind validation")
	}
	source := productionFunctionSource(
		t, "runtime_continuation.go", "materializeIntegrationContinuation",
	)
	if strings.Contains(source, "tx.GetRuntimeExecutionByTurnForUpdate(") {
		t.Fatal("materialization acquires RuntimeExecution after Session/Turn/ProcessRun")
	}
	prelock := productionFunctionSource(
		t, "runtime_continuation.go", "prelockIntegrationTerminalGraph",
	)
	runtimeLock := strings.Index(prelock, "GetRuntimeExecutionByTurnForUpdate")
	sessionLock := strings.Index(prelock, "snapshot.SessionID")
	if runtimeLock < 0 || sessionLock < 0 || runtimeLock > sessionLock {
		t.Fatal("integration terminal graph does not lock RuntimeExecution before Session")
	}
	leaseCheck := strings.Index(source, "tx.GetTurnLeaseForUpdate")
	replace := strings.Index(source, "replaceIntegrationPredecessor")
	openWork := strings.Index(source, "tx.ProcessHasOpenWork")
	update := strings.Index(source, "tx.Update(ctx, replacedTurn")
	insert := strings.Index(source, "tx.Insert(ctx, turn)")
	if leaseCheck < 0 || replace < leaseCheck || openWork < replace ||
		update < openWork || insert < update {
		t.Fatal("integration successor can be inserted before predecessor authority is closed")
	}
}

func TestWorkClaimExpiryMigrationIsForwardOnly(t *testing.T) {
	baseMigration, err := os.ReadFile(
		"../../../../cmd/cli/migrations/20260731000500_control_plane_owner_wave_two.sql",
	)
	if err != nil {
		t.Fatalf("read applied migration: %v", err)
	}
	functionStart := strings.Index(string(baseMigration), "CREATE FUNCTION control_plane.work_claim_graph_is_active")
	functionEnd := strings.Index(string(baseMigration)[functionStart:], "$function$;")
	if functionStart < 0 || functionEnd < 0 {
		t.Fatal("work_claim_graph_is_active is absent in applied migration")
	}
	if strings.Contains(
		string(baseMigration)[functionStart:functionStart+functionEnd],
		"statement_timestamp()",
	) {
		t.Fatal("already-applied migration was destructively rewritten")
	}
	upgrade, err := os.ReadFile(
		"../../../../cmd/cli/migrations/20260803000100_control_plane_work_claim_expiry.sql",
	)
	if err != nil {
		t.Fatalf("read forward migration: %v", err)
	}
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION control_plane.work_claim_graph_is_active",
		"(claim.spec ->> 'expiresAt')::timestamptz > statement_timestamp()",
		"REVOKE ALL ON FUNCTION control_plane.work_claim_graph_is_active",
		"version = 20260803000100",
		"migration 20260803000100 is forward-only",
	} {
		if !strings.Contains(string(upgrade), required) {
			t.Fatalf("forward migration guard is absent: %s", required)
		}
	}
}

func TestSharedGraphEntryPointsUseCanonicalResolver(t *testing.T) {
	targets := map[string]struct {
		file                 string
		mustValidateReceipt  bool
		mustDisposition      bool
		receiptAfterResolver bool
	}{
		"ManageWorkClaim":            {"cycle_two.go", true, true, false},
		"ManageSession":              {"cycle_two.go", true, true, false},
		"StartProcess":               {"specialized.go", true, true, false},
		"EnqueueTurn":                {"runtime.go", true, true, false},
		"CompleteProcess":            {"specialized.go", true, true, false},
		"CancelProcess":              {"specialized.go", true, true, false},
		"CompleteTurn":               {"runtime.go", true, true, false},
		"RetryTurn":                  {"runtime.go", true, true, false},
		"CancelTurn":                 {"runtime.go", true, true, false},
		"ClaimTurn":                  {"runtime.go", false, true, true},
		"RenewTurn":                  {"runtime.go", false, true, true},
		"RequestOwnerGate":           {"specialized.go", false, true, true},
		"ResolveOwnerGate":           {"runtime.go", false, true, true},
		"ExpireOwnerGate":            {"final_owner_wave.go", false, true, true},
		"CompleteScheduleOccurrence": {"specialized.go", false, true, true},
		"CancelScheduleOccurrence":   {"specialized.go", false, true, true},
		"claimScheduleOccurrence":    {"specialized.go", false, true, true},
		"ClaimOwnerGateDelivery":     {"cycle_two.go", false, true, true},
		"RecordOwnerGateDelivery":    {"cycle_two.go", true, true, false},
	}
	files := map[string]*ast.File{}
	fset := token.NewFileSet()
	for _, target := range targets {
		if files[target.file] != nil {
			continue
		}
		parsed, err := parser.ParseFile(fset, target.file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", target.file, err)
		}
		files[target.file] = parsed
	}
	for name, target := range targets {
		t.Run(name, func(t *testing.T) {
			var declaration *ast.FuncDecl
			for _, item := range files[target.file].Decls {
				candidate, ok := item.(*ast.FuncDecl)
				if ok && candidate.Name.Name == name {
					declaration = candidate
					break
				}
			}
			if declaration == nil {
				t.Fatal("production entry point was not found")
			}
			calls := map[string][]token.Pos{}
			ast.Inspect(declaration.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok {
					calls[selector.Sel.Name] = append(calls[selector.Sel.Name], call.Pos())
				}
				identifier, ok := call.Fun.(*ast.Ident)
				if ok {
					calls[identifier.Name] = append(calls[identifier.Name], call.Pos())
				}
				return true
			})
			resolver := calls["lockOwnerGraphByTurn"]
			resolver = append(resolver, calls["lockOwnerGraphByProcess"]...)
			resolver = append(resolver, calls["lockOwnerGraphSet"]...)
			resolver = append(resolver, calls["lockSessionLifecycleGraph"]...)
			resolver = append(resolver, calls["prelockScheduledGraphByTurn"]...)
			resolver = append(resolver, calls["lockOwnerGateAfterGraph"]...)
			resolver = append(resolver, calls["replayScheduleOccurrenceClaim"]...)
			if len(resolver) == 0 {
				t.Fatal("canonical owner graph resolver is absent")
			}
			if target.mustValidateReceipt && len(calls["withValidatedResourceReceipt"]) == 0 {
				t.Fatal("shared graph command bypasses validated receipt wrapper")
			}
			if target.mustDisposition && len(calls["requireOwnerGraphRuntimeDisposition"]) == 0 &&
				len(calls["requireClosedRuntimeConsistentWithTurn"]) == 0 &&
				len(calls["replayScheduleOccurrenceClaim"]) == 0 {
				t.Fatal("RuntimeExecution disposition is not explicit")
			}
			if target.receiptAfterResolver && len(calls["GetReceipt"]) > 0 {
				firstResolver := resolver[0]
				for _, position := range resolver[1:] {
					if position < firstResolver {
						firstResolver = position
					}
				}
				if calls["GetReceipt"][0] < firstResolver {
					t.Fatal("receipt is read before canonical owner resolution")
				}
			}
		})
	}
}

func TestRetryAndOwnerGateCannotBypassLockedGraph(t *testing.T) {
	checks := []struct {
		file       string
		function   string
		mustCall   string
		forbidden  []string
		beforeCall string
	}{
		{
			file: "current_execution.go", function: "prepareRetriedExecution",
			forbidden: []string{
				"GetScheduleOccurrenceForUpdate", "GetScheduledRunForUpdate",
				"GetScheduledRunByCurrentTurnForUpdate", "GetForUpdate",
			},
		},
		{
			file: "specialized.go", function: "RequestOwnerGate",
			mustCall: "suspendRuntimeExecutionForOwnerGate", beforeCall: "ReplaceAndTransition",
		},
	}
	for _, check := range checks {
		t.Run(check.function, func(t *testing.T) {
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, check.file, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", check.file, err)
			}
			var declaration *ast.FuncDecl
			for _, item := range parsed.Decls {
				candidate, ok := item.(*ast.FuncDecl)
				if ok && candidate.Name.Name == check.function {
					declaration = candidate
					break
				}
			}
			if declaration == nil {
				t.Fatal("production function was not found")
			}
			calls := map[string][]token.Pos{}
			ast.Inspect(declaration.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					calls[selector.Sel.Name] = append(calls[selector.Sel.Name], call.Pos())
				}
				return true
			})
			for _, forbidden := range check.forbidden {
				if len(calls[forbidden]) != 0 {
					t.Fatalf("late shared graph lock %s remains", forbidden)
				}
			}
			if check.mustCall != "" {
				if len(calls[check.mustCall]) == 0 || len(calls[check.beforeCall]) == 0 ||
					calls[check.mustCall][0] > calls[check.beforeCall][0] {
					t.Fatalf("%s must precede %s", check.mustCall, check.beforeCall)
				}
			}
		})
	}
}

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

func TestProcessContinuationProductionPathsUseDomainArmOperations(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read resource package: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for lineNumber, line := range strings.Split(string(content), "\n") {
			left, _, assignment := strings.Cut(line, " = ")
			if assignment && (strings.Contains(left, "processSpec.Continuation") ||
				strings.Contains(left, "processSpec.OwnerFeedbackSHA256")) {
				t.Fatalf("manual continuation arm write in %s:%d: %s",
					entry.Name(), lineNumber+1, strings.TrimSpace(line))
			}
		}
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

	binding := entity.ProcessContinuationBinding{
		TurnID: base.ContinuationTurnID, TurnVersion: base.ContinuationTurnVersion,
		Attempt:                base.ContinuationAttempt,
		RuntimeRevisionID:      base.ContinuationRuntimeRevisionID,
		RuntimeRevisionVersion: base.ContinuationRuntimeRevisionVersion,
		InputSHA256:            base.ContinuationInputSHA256,
	}
	switched := ownerGate
	if err := switched.SetIntegrationContinuation(
		binding, integration.ContinuationIntegrationID, digest,
	); err != nil {
		t.Fatalf("OWNER_GATE -> INTEGRATION: %v", err)
	}
	if switched.ContinuationKind != enum.ProcessContinuationIntegration ||
		switched.ContinuationGateID != "" || switched.OwnerFeedbackSHA256 != "" ||
		switched.Validate() != nil {
		t.Fatalf("OWNER_GATE fields survived arm switch: %#v", switched)
	}
	if err := switched.SetOwnerGateContinuation(
		binding, ownerGate.ContinuationGateID, digest,
	); err != nil {
		t.Fatalf("INTEGRATION -> OWNER_GATE: %v", err)
	}
	if switched.ContinuationKind != enum.ProcessContinuationOwnerGate ||
		switched.ContinuationIntegrationID != "" ||
		switched.ContinuationOutcomeSHA256 != "" || switched.Validate() != nil {
		t.Fatalf("INTEGRATION fields survived arm switch: %#v", switched)
	}
	switched.ClearContinuation()
	if switched.ContinuationKind != enum.ProcessContinuationNone ||
		switched.ContinuationTurnID != "" || switched.ContinuationGateID != "" ||
		switched.ContinuationIntegrationID != "" || switched.Validate() != nil {
		t.Fatalf("continuation arm was not cleared: %#v", switched)
	}
}

func TestScheduledGraphCanonicalLockOrder(t *testing.T) {
	want := []string{
		"runtime_execution", "schedule_occurrence", "schedule", "scheduled_run",
		"session", "turn", "process_run", "pinned_resource", "owner_gate",
		"integration_continuation",
	}
	if !slices.Equal(scheduledGraphLockOrder[:], want) {
		t.Fatalf("unexpected scheduled graph lock order: %v", scheduledGraphLockOrder)
	}
	if got := ownerGraphLockPlan(true, true, true); !slices.Equal(
		got,
		[]string{"runtime_execution", "schedule_occurrence", "schedule", "scheduled_run", "session", "turn", "process_run"},
	) {
		t.Fatalf("unexpected scheduled owner graph trace: %v", got)
	}
	if got := ownerGraphLockPlan(false, false, true); !slices.Equal(
		got, []string{"session", "turn", "process_run"},
	) {
		t.Fatalf("unexpected unscheduled owner graph trace: %v", got)
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

func TestAdmissionReceiptIsExposedOnlyWhileLeaseIsCurrent(t *testing.T) {
	token := strings.Repeat("a", 64)
	current := RuntimeExecution{
		ID:      "3ed0d109-5eba-4e4e-8b98-f755f6e6fc6b",
		TurnID:  "bd823044-f6c9-43df-a56f-44a9870ef57d",
		Attempt: 2, GrantGeneration: 7,
		State: "ADMITTED", Version: 2, Fence: 2,
		LeaseTokenSHA256: hashString(token),
	}
	now := time.Now()
	current.LeaseExpiresAt = now.Add(time.Minute)
	turnLease := domainrepo.TurnLease{
		TurnID: current.TurnID, Attempt: current.Attempt,
		AuthorityGeneration: current.GrantGeneration,
		ExpiresAt:           current.LeaseExpiresAt,
	}
	stored := AdmitRuntimeExecutionResult{Execution: current, LeaseToken: token}
	if err := validateAdmitRuntimeReceipt(current, turnLease, stored, now); err != nil {
		t.Fatalf("live admission replay was rejected: %v", err)
	}
	expired := current
	expired.LeaseExpiresAt = now
	expiredStored := stored
	expiredStored.Execution = expired
	expiredLease := turnLease
	expiredLease.ExpiresAt = now
	if err := validateAdmitRuntimeReceipt(expired, expiredLease, expiredStored, now); !errors.Is(
		err, errs.ErrStateConflict,
	) {
		t.Fatalf("expired admission lease token was exposed: %v", err)
	}
	revoked := current
	revoked.State = "CANCELLED"
	revoked.Version++
	revoked.Fence++
	revoked.LeaseTokenSHA256 = ""
	if err := validateAdmitRuntimeReceipt(revoked, turnLease, stored, now); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("revoked lease token was exposed: %v", err)
	}
}

func TestRuntimeReceiptDispositionRejectsTerminalOrSuccessor(t *testing.T) {
	input := RuntimeExecutionInput{ExpectedVersion: 4, ExpectedFence: 9}
	current := RuntimeExecution{State: "RUNNING", Version: 4, Fence: 9}
	disposition, err := runtimeMutationReceiptDisposition(
		current, input, []string{"RUNNING"}, []string{"SUCCEEDED"}, 1,
	)
	if err != nil || disposition != lifecycleReceiptApply {
		t.Fatalf("current source was rejected: %v/%d", err, disposition)
	}
	current.State = "SUCCEEDED"
	current.Version++
	current.Fence++
	disposition, err = runtimeMutationReceiptDisposition(
		current, input, []string{"RUNNING"}, []string{"SUCCEEDED"}, 1,
	)
	if err != nil || disposition != lifecycleReceiptReplay {
		t.Fatalf("exact terminal outcome was not replayable: %v/%d", err, disposition)
	}
	current.Version++
	current.Fence++
	if _, err := runtimeMutationReceiptDisposition(
		current, input, []string{"RUNNING"}, []string{"SUCCEEDED"}, 1,
	); err == nil {
		t.Fatal("superseded terminal outcome remained replayable")
	}
}

func TestRuntimeExecutionDispositionIsMandatoryForGraphTransitions(t *testing.T) {
	absent := lockedOwnerGraph{}
	if err := requireOwnerGraphRuntimeDisposition(absent, runtimeDispositionAbsent); err != nil {
		t.Fatalf("absent runtime was rejected: %v", err)
	}
	live := lockedOwnerGraph{
		Runtime: &RuntimeExecution{State: "RUNNING"},
		Turn:    entity.Resource{State: enum.StateRunning},
	}
	if err := requireClosedRuntimeConsistentWithTurn(live); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("generic path accepted live runtime: %v", err)
	}
	terminal := lockedOwnerGraph{
		Runtime: &RuntimeExecution{State: "SUCCEEDED", TerminalOutcome: "SUCCEEDED"},
		Turn:    entity.Resource{State: enum.StateSucceeded},
	}
	if err := requireClosedRuntimeConsistentWithTurn(terminal); err != nil {
		t.Fatalf("consistent terminal runtime was rejected: %v", err)
	}
	terminal.Turn.State = enum.StateCancelled
	if err := requireClosedRuntimeConsistentWithTurn(terminal); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("mixed terminal graph was accepted: %v", err)
	}
	suspended := lockedOwnerGraph{
		Runtime: &RuntimeExecution{State: "SUSPENDED", TerminalOutcome: "SUSPENDED"},
		Turn:    entity.Resource{State: enum.StateWaitingOwner},
	}
	if err := requireClosedRuntimeConsistentWithTurn(suspended); err != nil {
		t.Fatalf("owner-gate suspension was rejected: %v", err)
	}
	suspended.Turn.State = enum.StateRunning
	if err := requireClosedRuntimeConsistentWithTurn(suspended); !errors.Is(err, errs.ErrStateConflict) {
		t.Fatalf("live Turn with suspended runtime was accepted: %v", err)
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
