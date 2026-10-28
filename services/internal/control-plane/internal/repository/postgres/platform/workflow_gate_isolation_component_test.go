package platform

import (
	_ "embed"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowGateExecutionProof observes exact execution/gate states and immutable clock anchors without preparing business rows.
// queryWorkflowGateExecutionProof читает exact execution/gate states и clock anchors без подготовки business rows.
//
//go:embed testdata/sql/workflow_gate_execution_proof.sql
var queryWorkflowGateExecutionProof string

// workflowGateExecutionProof preserves only bounded owner state, counters and clock anchors for the isolation regression.
// workflowGateExecutionProof сохраняет только bounded state/counters и clock anchors для isolation regression.
type workflowGateExecutionProof struct {
	root, physical, node, lease, rootDeadline, physicalDeadline string
	rootClock, physicalClock                                    time.Time
	ownOpen, allOpen, invocations, receipts, models             int64
}

// readWorkflowGateExecutionProof binds its SELECT to exact native node/lease refs and never logs their values.
// readWorkflowGateExecutionProof связывает SELECT с exact native node/lease refs и не логирует их значения.
func readWorkflowGateExecutionProof(t *testing.T, f *workflowRecoveryRegression, lease map[string]any) workflowGateExecutionProof {
	t.Helper()
	var p workflowGateExecutionProof
	if err := f.pool.QueryRow(f.ctx, queryWorkflowGateExecutionProof, pgx.StrictNamedArgs{"node_ref": stringMap(lease, "nodeRef"), "lease_ref": stringMap(lease, "leaseRef")}).Scan(&p.root, &p.physical, &p.node, &p.lease, &p.rootClock, &p.physicalClock, &p.ownOpen, &p.allOpen, &p.invocations, &p.receipts, &p.models, &p.rootDeadline, &p.physicalDeadline); err != nil {
		t.Fatal("read native gate isolation proof", completionOutcomeClass(err))
	}
	return p
}

// runWorkflowGateIsolationScenario uses actual renew/expiry/claim and sequential owner decisions; open gates keep provider work closed while independent models remain executable.
// runWorkflowGateIsolationScenario использует actual renew/expiry/claim и owner decisions; OPEN Gates держат provider work закрытым, независимые модели исполняются.
func runWorkflowGateIsolationScenario(t *testing.T, f *workflowRecoveryRegression, gated, independent map[string]any, invocation map[string]any, publication entity.RepositoryBundlePublication, resolve func(map[string]any, string, string, map[string]any) (map[string]any, error), input map[string]any) {
	t.Helper()
	ownerGate, err := f.service.GetOwnerGate(f.ctx, f.owner, stringMap(invocation, "gateRef"))
	if err != nil {
		t.Fatal("read PR owner gate")
	}
	bundleGate, err := f.service.GetOwnerGate(f.ctx, f.owner, publication.GateRef)
	if err != nil {
		t.Fatal("read bundle owner gate")
	}
	a, b := readWorkflowGateExecutionProof(t, f, independent), readWorkflowGateExecutionProof(t, f, gated)
	if a.root != "WAITING_HUMAN" || a.physical != "RUNNING" || b.physical != "RUNNING" || a.node != "RUNNING" || b.node != "RUNNING" || a.ownOpen != 0 || b.ownOpen != 2 || a.allOpen != 2 || a.models != 2 {
		t.Fatal("owner gate globally paused an independent physical execution")
	}
	gateway := resolvedTestPrincipal(t, f.ctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	if work, err := f.service.ClaimIntegrationInvocations(f.ctx, gateway, "isolation-before-decisions", 2); err != nil || len(work) != 0 {
		t.Fatal("OPEN owner gate admitted provider work")
	}
	var expiresAt time.Time
	for index, lease := range []map[string]any{gated, independent} {
		result, err := f.service.Execute(f.ctx, command.Command{Kind: command.RenewExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: []string{"isolation-renew-gated", "isolation-renew-independent"}[index]}, Payload: command.LeaseInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation")}})
		if err != nil {
			t.Fatal("live lease renew failed while another owner gate was OPEN", completionOutcomeClass(err))
		}
		if index == 1 {
			raw, _ := json.Marshal(result.Runtime["expiresAt"])
			if json.Unmarshal(raw, &expiresAt) != nil || expiresAt.IsZero() {
				t.Fatal("native renewed expiry is missing")
			}
		}
	}
	afterRenew := readWorkflowGateExecutionProof(t, f, independent)
	if afterRenew.rootClock != a.rootClock || afterRenew.physicalClock != a.physicalClock || afterRenew.rootDeadline != a.rootDeadline || afterRenew.physicalDeadline != a.physicalDeadline {
		t.Fatal("gate or renew reset the workflow clock")
	}
	wait := time.Until(expiresAt) + 150*time.Millisecond
	if wait < 0 || wait > 35*time.Second {
		t.Fatal("gate expiry fixture wait exceeded budget")
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-f.ctx.Done():
		t.Fatal("gate expiry wait exceeded context")
	case <-timer.C:
	}
	claims := f.claims("isolation-expired-independent")
	if len(claims) != 1 || stringMap(claims[0], "nodeRef") != stringMap(independent, "nodeRef") || stringMap(claims[0], "runtimeRevisionRef") == stringMap(independent, "runtimeRevisionRef") {
		t.Fatal("sibling OPEN gate prevented exact independent expiry/reclaim")
	}
	freshIndependent := claims[0]
	bAfter := readWorkflowGateExecutionProof(t, f, gated)
	if bAfter.lease != "EXPIRED" || bAfter.node != "QUEUED" || bAfter.ownOpen != 2 || bAfter.invocations != 2 || bAfter.receipts != 0 || bAfter.models != 1 {
		t.Fatal("expired own-gated execution was claimed or lost its durable queue")
	}
	for _, old := range []entity.OwnerGate{ownerGate, bundleGate} {
		current, err := f.service.GetOwnerGate(f.ctx, f.owner, old.Ref)
		if err != nil || current.State != "OPEN" || current.Version != old.Version {
			t.Fatal("model reclaim resolved or changed an owner gate")
		}
	}
	f.execute(command.CompleteExecution, "isolation-independent-complete", nil, command.CompleteExecutionInput{LeaseRef: stringMap(freshIndependent, "leaseRef"), Fence: stringMap(freshIndependent, "fence"), Generation: runtimeRevisionMapInt64(freshIndependent, "generation"), Success: true, Usage: turnUsageFixture(), ResultSummary: `{"schema":"kodex.workflow-result.v1","results":[{"key":"report","source":{"kind":"CURRENT_COMPLETION"}}]}`, Artifacts: []command.CompletedArtifact{workflowCaptureProgressArtifact("independent.md", []byte("Независимый результат при двух OPEN Gates.\n"))}})
	f.execute(command.ResolveOwnerGate, "isolation-approve-one", &ownerGate.Version, command.GateResolutionInput{GateRef: ownerGate.Ref, Decision: "APPROVE"})
	if claims := f.claims("isolation-one-own-gate-left"); len(claims) != 0 {
		t.Fatal("one remaining own OPEN gate admitted a fresh model")
	}
	stillOpen, err := f.service.GetOwnerGate(f.ctx, f.owner, bundleGate.Ref)
	if err != nil || stillOpen.State != "OPEN" || stillOpen.Version != bundleGate.Version {
		t.Fatal("resolving another gate changed the bundle decision")
	}
	f.execute(command.ResolveOwnerGate, "isolation-approve-second", &bundleGate.Version, command.GateResolutionInput{GateRef: bundleGate.Ref, Decision: "APPROVE"})
	claims = f.claims("isolation-after-own-decision")
	if len(claims) != 1 || stringMap(claims[0], "nodeRef") != stringMap(gated, "nodeRef") || stringMap(claims[0], "runtimeRevisionRef") == stringMap(gated, "runtimeRevisionRef") {
		t.Fatal("owner decision lost an already-expired queued execution")
	}
	freshGated := claims[0]
	current := readWorkflowGateExecutionProof(t, f, freshGated)
	if current.root != "RUNNING" || current.physical != "RUNNING" || current.ownOpen != 0 || current.rootClock != b.rootClock || current.physicalClock != b.physicalClock || current.rootDeadline != b.rootDeadline || current.physicalDeadline != b.physicalDeadline {
		t.Fatal("gate recovery reopened a clock or left physical execution paused")
	}
	if _, err := resolve(freshGated, "isolation-no-duplicate-intent", "github.pull_request.create", input); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("fresh model duplicated a pending authorized effect")
	}
	run, err := f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil {
		t.Fatal("read exact cancel version")
	}
	f.execute(command.CancelRun, "isolation-terminal-cancel", &run.Version, command.RunCommandInput{RunRef: f.runRef, Reason: "Native gate isolation cleanup"})
	closed := readWorkflowGateExecutionProof(t, f, freshGated)
	if closed.root != "CANCELLED" || closed.node != "CANCELLED" || closed.models != 0 || closed.invocations != 2 || closed.receipts != 0 {
		t.Fatal("terminal cancel did not close the complete model graph")
	}
	if claims := f.claims("isolation-terminal-claim"); len(claims) != 0 {
		t.Fatal("terminal root reopened a model after gate decisions")
	}
	if _, err := f.service.Execute(f.ctx, command.Command{Kind: command.RenewExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "isolation-terminal-renew"}, Payload: command.LeaseInput{LeaseRef: stringMap(freshGated, "leaseRef"), Fence: stringMap(freshGated, "fence"), Generation: runtimeRevisionMapInt64(freshGated, "generation")}}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("terminal lease renewal did not preserve its exact denial", completionOutcomeClass(err))
	}
	if afterDeniedRenew := readWorkflowGateExecutionProof(t, f, freshGated); afterDeniedRenew != closed {
		t.Fatal("denied terminal renewal changed graph, lease, counters or clock anchors")
	}
}
