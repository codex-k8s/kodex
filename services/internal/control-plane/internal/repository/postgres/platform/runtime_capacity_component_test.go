package platform

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestRuntimeResourceQueueComponent exercises real owner transactions without Kubernetes or provider calls.
// Waiting creates no revision/lease, fitting work bypasses oversized headroom demand, fresh capacity admits the original node,
// repeated observations do not duplicate events, and cancelling queued work prevents later admission.
// TestRuntimeResourceQueueComponent выполняет настоящие owner-транзакции без Kubernetes и вызовов провайдера.
// Ожидание не создаёт revision/lease, подходящая работа проходит мимо запроса сверх свободного запаса,
// свежая ёмкость допускает исходный узел; повторный опрос не плодит события, отмена запрещает последующий claim.
func TestRuntimeResourceQueueComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	defer pool.Close()
	_, service, owner, worker, err := newRunComponentFixture(t, ctx, pool, true)
	if err != nil {
		t.Fatal(err)
	}
	reportReadyUsingWorkerFixture(t, ctx, service, owner, worker)
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "resource-queue-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic resource queue", Language: "en"}).Project
	large := createLifecycleAgent(t, ctx, service, owner, project.Ref, "resource-queue-large", "Large fixture")
	small := createLifecycleAgent(t, ctx, service, owner, project.Ref, "resource-queue-small", "Small fixture")
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, small.Ref)
	if err != nil {
		t.Fatal(err)
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	policy.Resources.CPURequestMilli = 250
	policy.Resources.MemoryRequestMiB = 256
	environment := execute(command.CreateRuntimeEnvironment, owner, "small-environment", nil, command.RuntimeEnvironmentInput{ProjectRef: project.Ref, Name: "Synthetic small resources", ImageArtifactRef: configuration.Environment.CurrentVersion.Image.ArtifactRef, Policy: policy}).RuntimeEnvironment
	execute(command.BindAgentRuntimeEnvironment, owner, "small-bind", &small.Version, command.RuntimeEnvironmentBindingInput{AgentRef: small.Ref, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref})
	launch := func(agent entity.Agent, key string) entity.Run {
		return *execute(command.LaunchRun, owner, key, nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Synthetic queue task", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	}
	bigRun := launch(large, "large-run")
	waitingPeer := launch(large, "waiting-peer")
	capacity := func(memory int64) *command.RuntimeSchedulingCapacity {
		return &command.RuntimeSchedulingCapacity{ObservedAt: time.Now(), Overhead: command.RuntimeNodeCapacity{CPU: 50, Memory: 128 << 20, Pods: 1}, Nodes: []command.RuntimeNodeCapacity{{CPU: 8000, Memory: memory, Ephemeral: 20 << 30, Pods: 50, TotalCPU: 8000, TotalMemory: 16 << 30, TotalEphemeral: 20 << 30, TotalPods: 50}}}
	}
	claim := func(key string, memory int64) []map[string]any {
		return execute(command.ClaimExecution, worker, key, nil, command.LeaseInput{WorkloadInstance: "synthetic-resource-worker", Limit: 1, SchedulingCapacity: capacity(memory)}).RuntimeItems
	}
	empty := capacity(0)
	empty.Nodes = nil
	if len(execute(command.ClaimExecution, worker, "no-eligible-nodes", nil, command.LeaseInput{WorkloadInstance: "synthetic-resource-worker", Limit: 1, SchedulingCapacity: empty}).RuntimeItems) != 0 {
		t.Fatal("missing nodes admitted work")
	}
	noNodes, err := service.GetRun(ctx, owner, bigRun.Ref)
	if err != nil || noNodes.QueueReason != "WAITING_RESOURCES" {
		t.Fatal("temporary lack of eligible nodes misclassified as oversized request")
	}
	if len(claim("first-wait", 1<<30)) != 0 {
		t.Fatal("large demand admitted without headroom")
	}
	read, err := service.GetRun(ctx, owner, bigRun.Ref)
	if err != nil || read.QueueReason != "WAITING_RESOURCES" || read.State != "QUEUED" || read.StartedAt != nil {
		t.Fatal("authoritative queue reason missing")
	}
	peer, err := service.GetRun(ctx, owner, waitingPeer.Ref)
	if err != nil || peer.State != "QUEUED" || peer.QueueReason != "WAITING_RESOURCES" {
		t.Fatal("later blocked run was left falsely running")
	}
	execute(command.CancelRun, owner, "cancel-peer", &peer.Version, command.RunCommandInput{RunRef: peer.Ref})
	firstSequence := read.EventSequence
	if len(claim("same-wait", 1<<30)) != 0 {
		t.Fatal("waiting demand claimed")
	}
	read, err = service.GetRun(ctx, owner, bigRun.Ref)
	if err != nil || read.EventSequence != firstSequence {
		t.Fatal("unchanged poll duplicated event")
	}
	var leases, turns, revisions int64
	if err := pool.QueryRow(ctx, queryRuntimeClaimFixtureGraphCounts, bigRun.Ref).Scan(&leases, &turns, &revisions); err != nil || leases != 0 || revisions != 0 || turns != 1 {
		t.Fatal("waiting minted authority or lost original turn")
	}
	tooSmall := capacity(1 << 30)
	tooSmall.Nodes[0].TotalMemory = 1 << 30
	if len(execute(command.ClaimExecution, worker, "request-too-large", nil, command.LeaseInput{WorkloadInstance: "synthetic-resource-worker", Limit: 1, SchedulingCapacity: tooSmall}).RuntimeItems) != 0 {
		t.Fatal("request exceeding all nodes admitted")
	}
	read, err = service.GetRun(ctx, owner, bigRun.Ref)
	if err != nil || read.QueueReason != "REQUEST_EXCEEDS_CAPACITY" {
		t.Fatal("oversized request has no actionable reason")
	}
	smallRun := launch(small, "small-run")
	items := claim("fit-small", 1<<30)
	if len(items) != 1 || stringMap(items[0], "runRef") != smallRun.Ref {
		t.Fatal("unfitting queue head starved fitting work")
	}
	current := items[0]
	execute(command.CompleteExecution, worker, "small-complete", nil, command.CompleteExecutionInput{LeaseRef: stringMap(current, "leaseRef"), Fence: stringMap(current, "fence"), Generation: runtimeRevisionMapInt64(current, "generation"), Success: false, SafeErrorCode: "RUNTIME_UNAVAILABLE", ResultSummary: "Synthetic execution completed"})
	items = claim("released-capacity", 8<<30)
	if len(items) != 1 || stringMap(items[0], "runRef") != bigRun.Ref {
		t.Fatal("released capacity did not admit original queued run")
	}
	read, err = service.GetRun(ctx, owner, bigRun.Ref)
	if err != nil || read.QueueReason != "" || read.State != "RUNNING" || read.StartedAt == nil {
		t.Fatal("queue reason survived admission")
	}
	execute(command.CancelRun, owner, "stop-admitted", &read.Version, command.RunCommandInput{RunRef: bigRun.Ref})
	cancelled := launch(large, "cancelled-run")
	if len(claim("empty-capacity", 0)) != 0 {
		t.Fatal("empty capacity admitted work")
	}
	fresh, err := service.GetRun(ctx, owner, cancelled.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel-waiting", &fresh.Version, command.RunCommandInput{RunRef: cancelled.Ref})
	if len(claim("after-cancel", 8<<30)) != 0 {
		t.Fatal("cancelled wait was resurrected")
	}
	fresh, err = service.GetRun(ctx, owner, cancelled.Ref)
	if err != nil || fresh.State != "CANCELLED" || fresh.QueueReason != "" {
		t.Fatal("cancelled queue presentation was retained")
	}
	if err := pool.QueryRow(ctx, queryRuntimeClaimFixtureGraphCounts, cancelled.Ref).Scan(&leases, &turns, &revisions); err != nil || leases != 0 || revisions != 0 {
		t.Fatal("cancelled wait minted execution authority")
	}
}
