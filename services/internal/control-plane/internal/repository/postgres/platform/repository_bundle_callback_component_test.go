package platform

import (
	_ "embed"
	"fmt"
	"strings"
	"testing"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryRepositoryBundleCancelledDelivery observes only exact terminal graph/lease/delivery metadata after native owner cancellation; it writes no fixture business state.
// queryRepositoryBundleCancelledDelivery читает лишь exact terminal graph/lease/delivery metadata после native owner cancellation; business state оснасткой не записывается.
//
//go:embed testdata/sql/repository_bundle_cancelled_delivery.sql
var queryRepositoryBundleCancelledDelivery string

// testRepositoryBundleLateDisable preserves a visible owner delivery barrier if an already-completed producer loses launch authority before publication settlement; no writer turn or successful parent result is invented.
// testRepositoryBundleLateDisable сохраняет видимый owner delivery barrier, когда completed producer теряет launch authority до publication settlement; writer turn либо успешный parent result не выдумываются.
func testRepositoryBundleLateDisable(t *testing.T) {
	f := newRepositoryBundleFixtureMode(t, true)
	f.prepare()
	f.completeOrigin()
	repositoryBundleChildState(t, f, 0, "RUNNING")
	agent, err := f.service.GetAgent(f.ctx, f.owner, f.agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	f.execute(command.SetAgentEnabled, "late-disable", &agent.Version, command.AgentInput{Ref: agent.Ref, Enabled: false})
	owner := resolvedTestPrincipal(t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.repository-bundles.get"}, "control-api-gateway")
	view, err := f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
	if err != nil || view.Proposal.State != "CANCELLED" || view.Proposal.CandidateCommitSHA != "" || view.Proposal.ContinuationState != "BLOCKED" || view.Proposal.ContinuationCode != "CONTINUATION_AUTHORITY_CHANGED" || view.Proposal.ContinuationNodeRef != stringMap(f.origin, "nodeRef") {
		t.Fatal("late disable lost known no-effect/delivery outcome", err)
	}
	f.graph(0)
	repositoryBundleChildState(t, f, 0, "RUNNING")
	work, err := f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "disabled-no-work", 1)
	if err != nil || len(work) != 0 {
		t.Fatal("disabled agent obtained work", err)
	}
	root, err := f.service.GetRun(f.ctx, f.owner, f.parentRun.Ref)
	if err != nil {
		t.Fatal(err)
	}
	f.execute(command.CancelRun, "late-disable-cancel", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Owner resolves cancelled publication and blocked delivery"})
	view, err = f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
	if err != nil || view.Proposal.State != "CANCELLED" || view.Proposal.ContinuationState != "CANCELLED" || view.Proposal.ContinuationCode != "" {
		t.Fatal("owner cancel changed no-effect fact or retained delivery", err)
	}
	f.noObjectEffects()
}

// testRepositoryBundleOversizeCallback proves nine actual child summaries cannot roll back an externally confirmed Git outcome. The owner retains complete child readback and can cancel the durably blocked graph without another effect or continuation.
// testRepositoryBundleOversizeCallback доказывает, что девять actual child summaries не откатывают подтверждённый Git outcome. Owner сохраняет полное child readback и отменяет устойчиво blocked graph без нового effect либо continuation.
func testRepositoryBundleOversizeCallback(t *testing.T) {
	f := newRepositoryBundleFixtureMode(t, true)
	f.prepare()
	nestedAgent := createLifecycleAgent(t, f.ctx, f.service, f.owner, f.project, "oversize-helper", "Oversize helper")
	var children []string
	summary := strings.Repeat("x", 4000)
	for i := 0; i < 9; i++ {
		key := fmt.Sprintf("oversize-%d", i)
		child, err := f.service.Execute(f.ctx, command.Command{Kind: command.DelegateExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: key + "-delegate"}, Payload: command.DelegateInput{LeaseRef: f.input.LeaseRef, Fence: f.input.Fence, Generation: f.input.Generation, TargetAgentRef: nestedAgent.Ref, Task: "Return the complete synthetic result"}})
		if err != nil || child.Run == nil {
			t.Fatal("oversize native delegation", err)
		}
		children = append(children, child.Run.Ref)
		lease := claimBundleFixtureExecution(t, f, key+"-claim", child.Run.Ref)
		completeClaimedExecutionWithSummary(t, f.ctx, f.service, f.worker, lease, key+"-complete", false, summary)
	}
	f.completeOrigin()
	f.graph(0)
	f.decide("APPROVE")
	effect := f.claim()
	f.begin(effect)
	f.complete(effect)
	owner := resolvedTestPrincipal(t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.repository-bundles.get"}, "control-api-gateway")
	view, err := f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
	if err != nil || view.Proposal.State != "SUCCEEDED" || view.Proposal.CandidateCommitSHA != effect.CandidateCommitSHA || view.Proposal.ConfirmedAt == nil || view.Proposal.ContinuationState != "BLOCKED" || view.Proposal.ContinuationCode != "CALLBACK_INPUT_TOO_LARGE" || view.Proposal.ContinuationNodeRef != stringMap(f.origin, "nodeRef") {
		t.Fatal("oversize presentation erased Git receipt or blocked reason", err)
	}
	f.graph(0)
	repositoryBundleChildState(t, f, 0, "RUNNING")
	for _, ref := range children {
		run, err := f.service.GetRun(f.ctx, f.owner, ref)
		if err != nil || run.State != "SUCCEEDED" || run.ResultSummary != summary {
			t.Fatal("owner lost complete child result", err)
		}
	}
	work, err := f.service.ClaimRepositoryBundles(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.claim"), "oversize-no-repeat", 1)
	if err != nil || len(work) != 0 {
		t.Fatal("blocked presentation reissued Git work", err)
	}
	var state, rootID string
	var leases, deliveries, nodes int
	if err := f.pool.QueryRow(f.ctx, queryRepositoryBundleCancelledDelivery, f.parentRun.Ref).Scan(&state, &rootID, &leases, &deliveries, &nodes); err != nil || state == "SUCCEEDED" || leases != 0 || deliveries != 1 || nodes != 0 {
		t.Fatal("blocked graph metadata is incomplete", err)
	}
	var active, planned, gateActive int
	if err := f.pool.QueryRow(f.ctx, queryRuntimeCompleteexecutionSelectRunNodesRootRunIdType, rootID).Scan(&active, &planned); err != nil || active != 1 || planned != 0 {
		t.Fatal("native completion counter lost blocked delivery", err)
	}
	if err := f.pool.QueryRow(f.ctx, queryCommandsResolvegateSelectActiveAgentNodes, rootID).Scan(&gateActive); err != nil || gateActive != 1 {
		t.Fatal("native gate completion counter lost blocked delivery", err)
	}
	root, err := f.service.GetRun(f.ctx, f.owner, f.parentRun.Ref)
	if err != nil {
		t.Fatal(err)
	}
	f.execute(command.CancelRun, "cancel-oversize", &root.Version, command.RunCommandInput{RunRef: root.Ref, Reason: "Owner cancels oversized continuation; Git fact remains"})
	view, err = f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
	if err != nil || view.Proposal.State != "SUCCEEDED" || view.Proposal.CandidateCommitSHA != effect.CandidateCommitSHA || view.Proposal.ContinuationState != "CANCELLED" || view.Proposal.ContinuationCode != "" {
		t.Fatal("cancel changed Git fact or left blocked delivery", err)
	}
	if err := f.pool.QueryRow(f.ctx, queryRepositoryBundleCancelledDelivery, f.parentRun.Ref).Scan(&state, &rootID, &leases, &deliveries, &nodes); err != nil || state != "CANCELLED" || leases != 0 || deliveries != 0 || nodes != 0 {
		t.Fatal("cancel did not close blocked graph/leases/delivery", err)
	}
	f.noObjectEffects()
}
