package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/integration_cancel_graph_readback.sql
var queryIntegrationCancelGraphReadback string

// Авторитетный readback общей отмены: lease/fence не живут после terminal,
// неизвестный результат WRITE сохраняется, события имеют ровно одного origin.
func assertIntegrationCancelGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool, invocationRef, runRef, wantState string) [3]int64 {
	t.Helper()
	var state, runState string
	var closed bool
	var activeLeases, activeScopes, events int64
	var counts [3]int64
	if err := pool.QueryRow(ctx, queryIntegrationCancelGraphReadback, invocationRef, runRef).Scan(&state, &closed, &runState, &activeLeases, &activeScopes, &events, &counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatal("read cancelled integration graph")
	}
	if state != wantState || !closed || runState != "CANCELLED" || activeLeases != 0 || activeScopes != 0 || events != 1 {
		t.Fatalf("cancel graph incomplete: invocation=%s run=%s fenceClosed=%v leases=%d scopes=%d events=%d", state, runState, closed, activeLeases, activeScopes, events)
	}
	return counts
}

func exerciseIntegrationCancelGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool, service *serviceplatform.Service, owner, worker, gateway value.Principal, connectionRef, prefix string, changeGrant func(string, string), launch func(string) string) {
	t.Helper()
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: prefix + "-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("cancel graph execute %s: %v", kind, err)
		}
		return result
	}
	for _, running := range []bool{false, true} {
		key, policy, wantState := "ready", "NONE", "CANCELLED"
		if running {
			key, policy, wantState = "running", "HUMAN_SCOPED", "UNKNOWN_OUTCOME"
		}
		changeGrant(key, policy)
		runRef := launch(key)
		leases := execute(command.ClaimExecution, worker, key+"-runtime", nil, command.LeaseInput{WorkloadInstance: prefix + "-runtime", Limit: 1}).RuntimeItems
		if len(leases) != 1 || stringMap(leases[0], "runRef") != runRef {
			t.Fatal("cancel graph runtime was not claimed")
		}
		invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": runRef, "node_ref": stringMap(leases[0], "nodeRef"), "connection_ref": connectionRef, "capability_key": "github.pull_request.review.create", "idempotency_key": prefix + "-" + key + "-effect"}, map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Synthetic cancel comment", "event": "COMMENT"})
		if err != nil {
			t.Fatal(err)
		}
		var claimed map[string]any
		if running {
			gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
			if err != nil {
				t.Fatal(err)
			}
			execute(command.ResolveOwnerGate, owner, key+"-approve", &gate.Version, command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE"})
			work, err := service.ClaimIntegrationInvocations(ctx, gateway, prefix+"-gateway", 1)
			if err != nil || len(work) != 1 {
				t.Fatal("cancel graph integration WRITE was not claimed")
			}
			claimed = work[0]
		}
		run, err := service.GetRun(ctx, owner, runRef)
		if err != nil {
			t.Fatal(err)
		}
		payload := command.RunCommandInput{RunRef: runRef, Reason: "Synthetic complete cancel graph"}
		cancelled := execute(command.CancelRun, owner, key+"-cancel", &run.Version, payload)
		counts := assertIntegrationCancelGraph(t, ctx, pool, stringMap(invocation, "invocationRef"), runRef, wantState)
		replayed := execute(command.CancelRun, owner, key+"-cancel", &run.Version, payload)
		if cancelled.Run.Version != replayed.Run.Version || assertIntegrationCancelGraph(t, ctx, pool, stringMap(invocation, "invocationRef"), runRef, wantState) != counts {
			t.Fatal("CancelRun replay duplicated effects")
		}
		if running {
			_, err = service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway, Mutation: value.Mutation{IdempotencyKey: prefix + "-late-completion"}, Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(claimed, "invocationRef"), LeaseRef: stringMap(claimed, "leaseRef"), Fence: stringMap(claimed, "fence"), Generation: claimed["generation"].(int64), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}})
			if !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("late completion resurrected cancelled effect: %v", err)
			}
			if assertIntegrationCancelGraph(t, ctx, pool, stringMap(invocation, "invocationRef"), runRef, wantState) != counts {
				t.Fatal("rejected late completion created durable effects")
			}
		}
		work, err := service.ClaimIntegrationInvocations(ctx, gateway, prefix+"-after-cancel", 1)
		if err != nil || len(work) != 0 {
			t.Fatal("cancelled invocation was still claimable")
		}
	}
}
