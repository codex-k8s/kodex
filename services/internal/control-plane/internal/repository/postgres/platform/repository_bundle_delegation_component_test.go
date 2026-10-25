package platform

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryRepositoryBundleChildFixtureState observes actual child/root/callback cardinality and immutable result pins without writing ready business state.
// queryRepositoryBundleChildFixtureState читает actual child/root/callback cardinality и immutable result pins без записи готового бизнес-состояния.
//
//go:embed testdata/sql/repository_bundle_child_fixture_state.sql
var queryRepositoryBundleChildFixtureState string

// repositoryBundleChildState checks the original server-owned parent edge has delivered exactly the expected number of callbacks and parent continuations.
// repositoryBundleChildState проверяет точное ожидаемое число callbacks/parent continuations на original server-owned parent edge.
func repositoryBundleChildState(t *testing.T, f *repositoryBundleFixture, callbacks int, childState string) []byte {
	t.Helper()
	var child, root string
	var receipts, continuations int
	var artifacts []byte
	if err := f.pool.QueryRow(f.ctx, queryRepositoryBundleChildFixtureState, f.run.Ref, stringMap(f.parentOrigin, "nodeRef")).Scan(&child, &root, &receipts, &continuations, &artifacts); err != nil {
		t.Fatal(err)
	}
	if child != childState || root == "SUCCEEDED" || receipts != callbacks || continuations != callbacks {
		t.Fatalf("premature child/parent closure: child=%s root=%s receipts=%d continuations=%d expected=%s/%d", child, root, receipts, continuations, childState, callbacks)
	}
	return artifacts
}

// testRepositoryBundleNestedOrder joins native nested-child delivery and publication settlement in both orders before a single immutable child continuation; the original parent receives only its final pinned callback.
// testRepositoryBundleNestedOrder соединяет native nested-child delivery и publication settlement в обоих порядках до единственного immutable child continuation; исходный parent получает лишь final pinned callback.
func testRepositoryBundleNestedOrder(t *testing.T, order string) {
	f := newRepositoryBundleFixtureMode(t, true)
	nestedAgent := f.execute(command.CreateAgent, "nested-reader", nil, command.AgentInput{ProjectRef: f.project, Name: "Nested reader", Purpose: "Return independent child evidence", RoleDescription: "Read-only helper", Instructions: "Return one receipt", InitialCapabilities: []string{runtimecontract.ArtifactCapability}}).Agent
	f.prepare()
	f.decide("APPROVE")
	nested, err := f.service.Execute(f.ctx, command.Command{Kind: command.DelegateExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: "bundle-nested-delegate"}, Payload: command.DelegateInput{LeaseRef: f.input.LeaseRef, Fence: f.input.Fence, Generation: f.input.Generation, TargetAgentRef: nestedAgent.Ref, Task: "Return a nested receipt without Git effects"}})
	if err != nil || nested.Run == nil {
		t.Fatal("native nested delegation", err)
	}
	f.completeOrigin()
	f.graph(0)
	repositoryBundleChildState(t, f, 0, "RUNNING")
	nestedLease := claimBundleFixtureExecution(t, f, "bundle-nested-claim", nested.Run.Ref)
	settle := func() {
		effect := f.claim()
		f.begin(effect)
		before := f.initialPuts
		f.initialPuts = f.store.puts
		f.complete(effect)
		f.initialPuts = before
	}
	if order == "nested-child-first" {
		completeBundleChildContinuation(t, f, nestedLease, "bundle-nested-complete", true)
	} else if order == "effect-first" {
		settle()
	} else {
		t.Fatal("unsupported nested ordering")
	}
	f.graph(0)
	repositoryBundleChildState(t, f, 0, "RUNNING")
	if order == "nested-child-first" {
		settle()
	} else {
		completeBundleChildContinuation(t, f, nestedLease, "bundle-nested-complete", true)
	}
	f.graph(1)
	repositoryBundleChildState(t, f, 0, "RUNNING")
	final := claimBundleFixtureExecution(t, f, "bundle-nested-joined", f.run.Ref)
	if stringMap(final, "sessionRef") != stringMap(f.origin, "sessionRef") || runtimeRevisionMapInt64(final, "attempt") != runtimeRevisionMapInt64(f.origin, "attempt")+1 {
		t.Fatal("joint continuation lost immutable origin")
	}
	task := stringMap(final, "task")
	separator := strings.Index(task, "\n\n")
	var input struct {
		CompletedChildren []struct {
			RunRef       string
			ArtifactRefs []string
		}
		RepositoryPublicationOutcome struct {
			Total, Pending int
			AllSucceeded   bool
			Latest         struct{ Ref, State, CommitSha string }
		}
		RepositoryPublicationIntegration integrationContinuationSnapshot
	}
	if separator < 0 || json.Unmarshal([]byte(task[separator+2:]), &input) != nil || len(input.CompletedChildren) != 1 || input.CompletedChildren[0].RunRef != nested.Run.Ref || len(input.CompletedChildren[0].ArtifactRefs) != 1 {
		t.Fatal("joint continuation lost native nested result pins")
	}
	outcome := input.RepositoryPublicationOutcome
	decision := input.RepositoryPublicationIntegration.OwnerDecision
	if outcome.Total != 1 || outcome.Pending != 0 || !outcome.AllSucceeded || outcome.Latest.Ref != f.proposal.Ref || outcome.Latest.State != "SUCCEEDED" || outcome.Latest.CommitSha != strings.Repeat("b", 40) || decision.GateRef != f.proposal.GateRef || decision.GateVersion < 2 || decision.Decision != "APPROVE" || decision.Comment != "Preserve exact source pins. Do not repeat WRITE." {
		t.Fatal("joint continuation lost authoritative publication/owner feedback")
	}
	completeBundleChildContinuation(t, f, final, "bundle-nested-final", true)
	completeBundleChildContinuation(t, f, final, "bundle-nested-final", true)
	pins := repositoryBundleChildState(t, f, 1, "SUCCEEDED")
	var files []struct{ Ref, RuntimeRevisionRef string }
	if json.Unmarshal(pins, &files) != nil || len(files) != 2 {
		t.Fatal("final callback lost nested or final result custody")
	}
	seen := map[string]bool{}
	parent := claimBundleFixtureExecution(t, f, "bundle-nested-parent", f.parentRun.Ref)
	for _, pin := range files {
		seen[pin.RuntimeRevisionRef] = true
		file, err := f.service.ReadExecutionArtifact(f.ctx, f.principal("runtime-controller", "platform.runtime.execution.artifact.read"), stringMap(parent, "leaseRef"), stringMap(parent, "fence"), runtimeRevisionMapInt64(parent, "generation"), pin.Ref)
		if err != nil {
			t.Fatal("parent cannot read nested/final pinned result", err)
		}
		body, err := io.ReadAll(file.Reader)
		_ = file.Reader.Close()
		if err != nil || !bytes.Equal(body, []byte("Exact final child receipt.\n")) {
			t.Fatal("callback custody changed bytes", err)
		}
	}
	if !seen[stringMap(final, "runtimeRevisionRef")] || !seen[stringMap(nestedLease, "runtimeRevisionRef")] || !strings.Contains(stringMap(parent, "task"), f.proposal.Ref) || f.store.puts != f.initialPuts+2 || f.store.deletes != 0 {
		t.Fatal("joint callback provenance or external effect accounting changed")
	}
}

// completeBundleChildContinuation reports one actual final artifact with the exact native execution lease and supports command receipt replay; source publication itself still performs no Put.
// completeBundleChildContinuation сообщает один actual final artifact с exact native execution lease и поддерживает command receipt replay; source publication сама Put не выполняет.
func completeBundleChildContinuation(t *testing.T, f *repositoryBundleFixture, lease map[string]any, key string, artifact bool) {
	t.Helper()
	artifacts := []command.CompletedArtifact{}
	if artifact {
		raw := []byte("Exact final child receipt.\n")
		digest := sha256.Sum256(raw)
		artifacts = append(artifacts, command.CompletedArtifact{FileName: "publication-receipt.txt", MediaType: "text/plain", SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(digest[:]), Content: raw})
	}
	_, err := f.service.Execute(f.ctx, command.Command{Kind: command.CompleteExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "published successfully", Artifacts: artifacts, Usage: turnUsageFixture()}})
	if err != nil {
		t.Fatal("final child completion", err)
	}
}

// claimBundleFixtureExecution takes one real immutable execution and requires the exact expected run; no model is started by the fixture.
// claimBundleFixtureExecution получает actual immutable execution и требует exact expected run; модель оснасткой не запускается.
func claimBundleFixtureExecution(t *testing.T, f *repositoryBundleFixture, key, run string) map[string]any {
	t.Helper()
	result, err := f.service.Execute(f.ctx, command.Command{Kind: command.ClaimExecution, Principal: f.worker, Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: "bundle-delegation", Limit: 1}})
	if err != nil || len(result.RuntimeItems) != 1 || stringMap(result.RuntimeItems[0], "runRef") != run {
		t.Fatal("wrong native continuation selected", err)
	}
	return result.RuntimeItems[0]
}

// testRepositoryBundleDelegatedOrder exercises both actual origin/effect orderings and settled negative owner outcomes with an intentionally misleading model summary; only authoritative owner publication facts reach the parent's decision input.
// testRepositoryBundleDelegatedOrder проверяет оба actual origin/effect порядка и settled negative owner outcomes с намеренно ложным model summary; в parent decision input publication facts назначает лишь owner.
func testRepositoryBundleDelegatedOrder(t *testing.T, order string) {
	f := newRepositoryBundleFixtureMode(t, true)
	f.originResultSummary = "published successfully"
	f.prepare()
	repositoryBundleChildState(t, f, 0, "WAITING_HUMAN")
	expectedState := "SUCCEEDED"
	switch order {
	case "origin-first":
		f.completeOrigin()
		repositoryBundleChildState(t, f, 0, "RUNNING")
		f.decide("APPROVE")
		effect := f.claim()
		f.begin(effect)
		f.complete(effect)
	case "effect-first":
		f.decide("APPROVE")
		effect := f.claim()
		f.begin(effect)
		f.complete(effect)
		repositoryBundleChildState(t, f, 0, "WAITING_HUMAN")
		f.completeOrigin()
	case "settled-cancelled-agent-disabled":
		agent, err := f.service.GetAgent(f.ctx, f.owner, f.agent.Ref)
		if err != nil {
			t.Fatal(err)
		}
		f.execute(command.SetAgentEnabled, "cancel-child-authority", &agent.Version, command.AgentInput{Ref: agent.Ref, Enabled: false})
		expectedState = "CANCELLED"
		f.completeOrigin()
	case "settled-failed":
		f.decide("APPROVE")
		effect := f.claim()
		failed, err := f.service.FailRepositoryBundle(f.ctx, f.principal("integration-gateway", "platform.repository-bundles.work.fail"), effect.Lease, "CONTENT_INVALID")
		if err != nil || failed.State != "FAILED" || failed.FailureCode != "CONTENT_INVALID" {
			t.Fatal("known worker failure did not settle", err)
		}
		expectedState = "FAILED"
		f.completeOrigin()
	default:
		t.Fatal("unsupported fixture order")
	}
	var final map[string]any
	if order != "settled-cancelled-agent-disabled" {
		repositoryBundleChildState(t, f, 0, "RUNNING")
		final = claimBundleFixtureExecution(t, f, "bundle-final-child", f.run.Ref)
		if stringMap(final, "sessionRef") != stringMap(f.origin, "sessionRef") || stringMap(final, "turnRef") == stringMap(f.origin, "turnRef") || runtimeRevisionMapInt64(final, "attempt") != runtimeRevisionMapInt64(f.origin, "attempt")+1 {
			t.Fatal("child continuation crossed session/turn/attempt")
		}
		if expectedState == "FAILED" {
			task := stringMap(final, "task")
			separator := strings.Index(task, "\n\n")
			var feedback integrationContinuationSnapshot
			if separator < 0 || json.Unmarshal([]byte(task[separator+2:]), &feedback) != nil || feedback.InvocationRef != f.proposal.InvocationRef || feedback.State != "FAILED" || feedback.SafeErrorCode != "INTEGRATION_REQUEST_REJECTED" {
				t.Fatal("child continuation lost exact native failure contract")
			}
			owner := resolvedTestPrincipal(t, f.ctx, f.r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.repository-bundles.get"}, "control-api-gateway")
			view, err := f.service.GetRepositoryBundle(f.ctx, owner, port.RepositoryBundleRead{GateRef: f.proposal.GateRef, Limit: 16})
			if err != nil || view.Proposal.Ref != f.proposal.Ref || view.Proposal.InvocationRef != feedback.InvocationRef || view.Proposal.State != "FAILED" || view.Proposal.FailureCode != "CONTENT_INVALID" {
				t.Fatal("owner read lost specialized publication failure", err)
			}
		}
		completeBundleChildContinuation(t, f, final, "bundle-final-child-complete", true)
		completeBundleChildContinuation(t, f, final, "bundle-final-child-complete", true)
	}
	pins := repositoryBundleChildState(t, f, 1, "SUCCEEDED")
	parent := claimBundleFixtureExecution(t, f, "bundle-final-parent", f.parentRun.Ref)
	task := stringMap(parent, "task")
	index := strings.Index(task, "\n\n")
	if index < 0 || !strings.Contains(task, "SUCCEEDED child execution and model resultSummary do not prove") {
		t.Fatal("parent input lacks authoritative effect separation")
	}
	var snapshot struct {
		CompletedChildren []struct {
			RunRef, State, ResultSummary string
			RepositoryPublicationOutcome struct {
				Kind                                    string
				Total, Succeeded, Unsuccessful, Pending int
				AllSucceeded                            bool
				Latest                                  struct{ Ref, State, CommitSha, ManifestSha256, FailureCode string }
			}
		}
	}
	if json.Unmarshal([]byte(task[index+2:]), &snapshot) != nil || len(snapshot.CompletedChildren) != 1 {
		t.Fatal("parent did not receive exact child context")
	}
	child := snapshot.CompletedChildren[0]
	outcome := child.RepositoryPublicationOutcome
	if child.RunRef != f.run.Ref || child.State != "SUCCEEDED" || child.ResultSummary != "published successfully" || outcome.Kind != "REPOSITORY_BUNDLE_PUBLICATION_OUTCOME" || outcome.Total != 1 || outcome.Pending != 0 || outcome.Latest.Ref != f.proposal.Ref || outcome.Latest.State != expectedState || outcome.Latest.ManifestSha256 != f.input.Manifest.SHA256 {
		t.Fatal("parent lost or fabricated authoritative publication outcome")
	}
	if expectedState == "SUCCEEDED" {
		if !outcome.AllSucceeded || outcome.Succeeded != 1 || outcome.Latest.CommitSha != strings.Repeat("b", 40) {
			t.Fatal("verified publication receipt lost")
		}
	} else if outcome.AllSucceeded || outcome.Succeeded != 0 || outcome.Unsuccessful != 1 || outcome.Latest.CommitSha != "" {
		t.Fatal("model success became a false Git publication")
	}
	if expectedState == "FAILED" && outcome.Latest.FailureCode != "CONTENT_INVALID" {
		t.Fatal("parent lost specialized publication failure code")
	}
	var files []struct{ Ref, RuntimeRevisionRef string }
	if json.Unmarshal(pins, &files) != nil {
		t.Fatal("decode immutable result pins")
	}
	if final != nil {
		if len(files) != 1 || files[0].RuntimeRevisionRef != stringMap(final, "runtimeRevisionRef") {
			t.Fatal("callback artifacts bound to original instead of final revision")
		}
		download, err := f.service.ReadExecutionArtifact(f.ctx, f.principal("runtime-controller", "platform.runtime.execution.artifact.read"), stringMap(parent, "leaseRef"), stringMap(parent, "fence"), runtimeRevisionMapInt64(parent, "generation"), files[0].Ref)
		if err != nil {
			t.Fatal("parent cannot read exact final child result", err)
		}
		raw, err := io.ReadAll(download.Reader)
		_ = download.Reader.Close()
		if err != nil || !bytes.Equal(raw, []byte("Exact final child receipt.\n")) {
			t.Fatal("parent result bytes substituted", err)
		}
	}
	if final == nil && len(files) != 0 {
		t.Fatal("cancelled publication invented final result artifacts")
	}
	// The only new Put after publication source capture is the explicitly reported final model artifact.
	// Единственный новый Put после source capture принадлежит явно сообщённому final model artifact.
	expectedPuts := f.initialPuts
	if final != nil {
		expectedPuts++
	}
	if f.store.puts != expectedPuts || f.store.deletes != 0 {
		t.Fatal("callback replay changed source custody")
	}
}
