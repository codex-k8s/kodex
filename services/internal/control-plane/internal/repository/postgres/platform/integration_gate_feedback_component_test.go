package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationGateFeedbackSnapshotQuery reads the exact persisted continuation task using
// revision, node, run and session pins; it never changes fixture business state.
// integrationGateFeedbackSnapshotQuery читает точную сохранённую задачу продолжения по pins
// revision, node, run и session; запрос не меняет бизнес-состояние тестовой оснастки.
//
//go:embed testdata/sql/integration_gate_feedback_snapshot.sql
var integrationGateFeedbackSnapshotQuery string

// integrationPendingConnectionFixture changes only the named synthetic Connection;
// retaining the same owner models health metadata drift without configuration drift.
// integrationPendingConnectionFixture меняет лишь названное synthetic Connection;
// прежний owner моделирует изменение health metadata без изменения конфигурации.
//
//go:embed testdata/sql/integration_pending_connection_fixture.sql
var integrationPendingConnectionFixture string

// integrationPendingExpireFixture expires only receipts of the named synthetic invocation.
// integrationPendingExpireFixture просрочивает лишь receipts названного synthetic invocation.
//
//go:embed testdata/sql/integration_pending_expire_fixture.sql
var integrationPendingExpireFixture string

// TestIntegrationGateFeedbackComponent exercises owner decisions after the original execution
// completes, then claims a real immutable continuation with exact feedback and closed replay,
// project, terminal-root and public-redaction boundaries. No external provider is contacted.
// TestIntegrationGateFeedbackComponent проводит решения владельца после завершения исходного
// execution и получает настоящее immutable continuation с точным замечанием и закрытыми
// границами replay, проекта, terminal root и публичной проекции. Внешний провайдер не вызывается.
func TestIntegrationGateFeedbackComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated feedback PostgreSQL")
	}
	defer pool.Close()
	r := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		return resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation}, workload)
	}
	owner := principal("control-api-gateway", "platform.command.integrations.create", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	gateway := principal("integration-gateway", "platform.runtime.integrations.claim", "kodex-system-subject", "kodex-installation")
	reader := principal("runtime-controller", "platform.runtime.execution.integration.outcomes.read", "kodex-system-subject", "kodex-installation")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(t *testing.T, kind command.Kind, p value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: p, Mutation: value.Mutation{IdempotencyKey: "feedback-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s (%s): %v", kind, key, err)
		}
		return result
	}
	connection := execute(t, command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Feedback repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	project := execute(t, command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Feedback", Language: "en"}).Project
	other := execute(t, command.CreateProject, owner, "other-project", nil, command.ProjectInput{Name: "Other feedback", Language: "en"}).Project
	foreign := owner
	foreign.ProjectRef = gateTestProjectID(t, ctx, r, owner, other.Ref)
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "feedback-agent", "Feedback operator")
	currentConnection, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, command.ChangeIntegrationGrant, owner, "grant", &currentConnection.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "github.pull_request.review.create", AgentRef: agent.Ref, Enabled: true, ApprovalPolicy: "HUMAN_EACH_EFFECT"})
	previousInvocation := ""
	for _, decision := range []string{"APPROVE", "APPROVE_FAILED", "REJECT", "CANCEL", "EMPTY_REJECT", "TERMINAL", "APPROVE_LONG_INLINE", "APPROVE_LARGE", "APPROVE_EARLY_ALIAS", "APPROVE_NEW_INTENT", "APPROVE_UNKNOWN_ALIAS", "EXPIRED_ALIAS"} {
		t.Run(decision, func(t *testing.T) {
			run := execute(t, command.LaunchRun, owner, decision+"-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Feedback effect", Task: "Preserve canonical input", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			claimed := execute(t, command.ClaimExecution, worker, decision+"-origin", nil, command.LeaseInput{WorkloadInstance: "feedback-runtime", Limit: 1}).RuntimeItems
			if len(claimed) != 1 {
				t.Fatal("origin was not claimed")
			}
			origin := claimed[0]
			request := map[string]string{"run_ref": run.Ref, "node_ref": stringMap(origin, "nodeRef"), "connection_ref": connection.Ref, "capability_key": "github.pull_request.review.create", "idempotency_key": "feedback-effect-" + decision}
			bounded := map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Fixture review", "event": "COMMENT"}
			invocation, err := service.ResolveIntegrationInvocation(ctx, worker, request, bounded)
			if err != nil {
				t.Fatal(err)
			}
			if previousInvocation != "" && stringMap(invocation, "invocationRef") == previousInvocation {
				t.Fatal("pending reuse crossed physical run/node boundaries")
			}
			request["idempotency_key"] += "-alias"
			aliased, err := service.ResolveIntegrationInvocation(ctx, worker, request, bounded)
			if err != nil || stringMap(aliased, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(aliased, "gateRef") != stringMap(invocation, "gateRef") {
				t.Fatal("identical pending tool call created another invocation or gate", err)
			}
			beforeDecision, err := service.ClaimIntegrationInvocations(ctx, gateway, "feedback-before-decision", 1)
			if err != nil || len(beforeDecision) != 0 {
				t.Fatal("pending alias admitted an external effect before the owner decision", err)
			}
			if decision == "APPROVE_EARLY_ALIAS" {
				results := make(chan struct {
					value map[string]any
					err   error
				}, 3)
				for _, key := range []string{"parallel-a", "parallel-b", "parallel-a"} {
					parallel := maps.Clone(request)
					parallel["idempotency_key"] += "-" + key
					go func() {
						value, err := service.ResolveIntegrationInvocation(ctx, worker, parallel, bounded)
						results <- struct {
							value map[string]any
							err   error
						}{value, err}
					}()
				}
				for range 3 {
					result := <-results
					if result.err != nil || stringMap(result.value, "invocationRef") != stringMap(invocation, "invocationRef") {
						t.Fatal("concurrent pending calls did not join one intent", result.err)
					}
				}
				if _, err := pool.Exec(ctx, integrationPendingConnectionFixture, pgx.StrictNamedArgs{"connection_ref": connection.Ref, "owner": "fixture"}); err != nil {
					t.Fatal(err)
				}
				healthRequest := maps.Clone(request)
				healthRequest["idempotency_key"] += "-after-health"
				result, err := service.ResolveIntegrationInvocation(ctx, worker, healthRequest, bounded)
				if err != nil || stringMap(result, "invocationRef") != stringMap(invocation, "invocationRef") {
					t.Fatal("health metadata drift broke pending reuse", err)
				}
				if _, err := pool.Exec(ctx, integrationPendingConnectionFixture, pgx.StrictNamedArgs{"connection_ref": connection.Ref, "owner": "changed"}); err != nil {
					t.Fatal(err)
				}
				healthRequest["idempotency_key"] += "-different-configuration"
				if _, err := service.ResolveIntegrationInvocation(ctx, worker, healthRequest, bounded); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("new alias accepted a changed Connection configuration", err)
				}
				if _, err := pool.Exec(ctx, integrationPendingConnectionFixture, pgx.StrictNamedArgs{"connection_ref": connection.Ref, "owner": "fixture"}); err != nil {
					t.Fatal(err)
				}
			}
			if decision == "EXPIRED_ALIAS" {
				if _, err := pool.Exec(ctx, integrationPendingExpireFixture, pgx.StrictNamedArgs{"operation": pendingInvocationReceiptOperation, "invocation_ref": stringMap(invocation, "invocationRef")}); err != nil {
					t.Fatal(err)
				}
				if _, err := service.ResolveIntegrationInvocation(ctx, worker, request, bounded); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("expired alias permitted allocation", err)
				}
				current, err := service.GetRun(ctx, owner, run.Ref)
				if err != nil {
					t.Fatal(err)
				}
				execute(t, command.CancelRun, owner, decision+"-cancel", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Close expired synthetic alias"})
				return
			}
			completeOrigin := func() {
				execute(t, command.CompleteExecution, worker, decision+"-complete-origin", nil, command.CompleteExecutionInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, ResultSummary: "Waiting for owner", Usage: turnUsageFixture()})
			}
			early := decision == "APPROVE_EARLY_ALIAS" || decision == "APPROVE_NEW_INTENT" || decision == "APPROVE_UNKNOWN_ALIAS"
			if !early {
				completeOrigin()
			}
			gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
			if err != nil {
				t.Fatal(err)
			}
			comment := "Сохранить точные 624B паспорта\nи canonical input. Не повторять WRITE. 🧪 <>&"
			actualDecision := decision
			if strings.HasPrefix(decision, "APPROVE") {
				actualDecision = "APPROVE"
			}
			if decision == "EMPTY_REJECT" {
				actualDecision = "REJECT"
				comment = ""
			}
			if decision == "TERMINAL" {
				actualDecision = "REJECT"
			}
			payload := command.GateResolutionInput{GateRef: gate.Ref, Decision: actualDecision, Comment: comment}
			mutate := func(p value.Principal, key string, version int64) error {
				_, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: p, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, Payload: payload})
				return err
			}
			if err := mutate(foreign, "feedback-foreign-"+decision, gate.Version); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("foreign decision escaped project boundary: %v", err)
			}
			if decision == "TERMINAL" {
				fresh, err := service.GetRun(ctx, owner, run.Ref)
				if err != nil {
					t.Fatal(err)
				}
				execute(t, command.CancelRun, owner, decision+"-cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Terminal root fixture"})
				if _, err := service.ResolveIntegrationInvocation(ctx, worker, request, bounded); err == nil {
					t.Fatal("alias replay revived a cancelled root")
				}
				if err := mutate(owner, "feedback-terminal", gate.Version); err == nil {
					t.Fatal("terminal root gate reopened")
				}
				_, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
				if err != nil {
					t.Fatal(err)
				}
				for _, edge := range graph.Edges {
					if edge.Type == "CONTINUES" {
						t.Fatal("terminal root received continuation")
					}
				}
				return
			}
			if err := mutate(owner, "feedback-stale-"+decision, gate.Version+1); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("stale decision accepted: %v", err)
			}
			resolved := execute(t, command.ResolveOwnerGate, owner, decision+"-resolve", &gate.Version, payload)
			if resolved.Gate.DecisionComment != comment || resolved.Gate.Version != gate.Version+1 {
				t.Fatal("owner gate lost exact decision")
			}
			execute(t, command.ResolveOwnerGate, owner, decision+"-resolve", &gate.Version, payload)
			if err := mutate(foreign, "feedback-"+decision+"-resolve", gate.Version); !errors.Is(err, errs.ErrNotFound) {
				t.Fatalf("receipt replay bypassed project boundary: %v", err)
			}
			expectedSummary := ""
			if actualDecision == "APPROVE" {
				work, err := service.ClaimIntegrationInvocations(ctx, gateway, "feedback-gateway", 1)
				if err != nil || len(work) != 1 {
					t.Fatal("approved synthetic effect was not claimed", err)
				}
				completion := command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}
				if decision == "APPROVE_UNKNOWN_ALIAS" {
					completion.UnknownOutcome = true
					completion.SafeErrorCode = "INTEGRATION_OUTCOME_UNKNOWN"
				} else if decision != "APPROVE_FAILED" {
					summary := `{"fixture":"exact provider result, not owner instructions"}`
					if decision == "APPROVE_LONG_INLINE" || decision == "APPROVE_LARGE" {
						repeat := 700
						if decision == "APPROVE_LARGE" {
							repeat = 5000
						}
						summary = string(asJSON(map[string]any{"body": strings.Repeat("Synthetic ", repeat) + "🧪 конец", "object_id": "fixture-tail-after-4000", "version": 2}))
					}
					expectedSummary = summary
					responseDigest := sha256.Sum256([]byte(summary))
					completion.Success = true
					completion.SafeErrorCode = ""
					completion.ResultSummary = summary
					completion.EffectKey = stringMap(work[0], "effectKey")
					completion.InputDigest = stringMap(work[0], "inputDigest")
					completion.ProviderEffectRef = "synthetic-github:review:1"
					completion.ResponseDigest = hex.EncodeToString(responseDigest[:])
				}
				execute(t, command.CompleteIntegrationInvocation, gateway, decision+"-effect", nil, completion)
				execute(t, command.CompleteIntegrationInvocation, gateway, decision+"-effect", nil, completion)
			} else {
				work, err := service.ClaimIntegrationInvocations(ctx, gateway, "feedback-gateway", 1)
				if err != nil || len(work) != 0 {
					t.Fatal("negative owner decision allowed an effect", err)
				}
			}
			if early {
				restarted := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
				replayService, err := serviceplatform.New(restarted)
				if err != nil {
					t.Fatal(err)
				}
				replayed, err := replayService.ResolveIntegrationInvocation(ctx, worker, request, bounded)
				expectedState := "SUCCEEDED"
				if decision == "APPROVE_UNKNOWN_ALIAS" {
					expectedState = "UNKNOWN_OUTCOME"
				}
				if err != nil || stringMap(replayed, "invocationRef") != stringMap(invocation, "invocationRef") || stringMap(replayed, "state") != expectedState {
					t.Fatal("lost alias response after decision or restart allocated another effect", err)
				}
				changed := map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Different intent", "event": "COMMENT"}
				if _, err := replayService.ResolveIntegrationInvocation(ctx, worker, request, changed); !errors.Is(err, errs.ErrIdempotencyReuse) {
					t.Fatal("alias accepted changed input", err)
				}
				if decision == "APPROVE_UNKNOWN_ALIAS" {
					current, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
					if err != nil {
						t.Fatal(err)
					}
					for _, edge := range graph.Edges {
						if edge.Type == "CONTINUES" {
							t.Fatal("unknown alias fabricated a successful continuation")
						}
					}
					execute(t, command.CancelRun, owner, decision+"-close", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Retain uncertain synthetic outcome without another effect"})
					return
				}
				if decision == "APPROVE_NEW_INTENT" {
					request["idempotency_key"] += "-intentional-new-call"
					fresh, err := replayService.ResolveIntegrationInvocation(ctx, worker, request, bounded)
					if err != nil || stringMap(fresh, "invocationRef") == stringMap(invocation, "invocationRef") || stringMap(fresh, "state") != "WAITING_APPROVAL" {
						t.Fatal("new intentional call after terminal was silently suppressed", err)
					}
					current, err := service.GetRun(ctx, owner, run.Ref)
					if err != nil {
						t.Fatal(err)
					}
					execute(t, command.CancelRun, owner, decision+"-cancel-unapproved", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Close unapproved synthetic new intent"})
					return
				}
				completeOrigin()
				current, err := service.GetRun(ctx, owner, run.Ref)
				if err != nil || current.State != "SUCCEEDED" {
					t.Fatal("same physical turn did not finish after consuming the aliased result", err)
				}
				remaining, err := service.ClaimIntegrationInvocations(ctx, gateway, "feedback-after-alias", 1)
				if err != nil || len(remaining) != 0 {
					t.Fatal("terminal alias replay queued another external effect", err)
				}
				return
			}
			// Восстанавливаем adapter перед claim: immutable input не зависит от памяти scheduler.
			restarted := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
			restartedService, err := serviceplatform.New(restarted)
			if err != nil {
				t.Fatal(err)
			}
			continuation, err := restartedService.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "feedback-continuation-" + decision}, Payload: command.LeaseInput{WorkloadInstance: "feedback-restarted", Limit: 1}})
			if err != nil || len(continuation.RuntimeItems) != 1 {
				t.Fatal("continuation was not claimed after adapter restart", err)
			}
			lease := continuation.RuntimeItems[0]
			if stringMap(lease, "sessionRef") != stringMap(origin, "sessionRef") || stringMap(lease, "turnRef") == stringMap(origin, "turnRef") || stringMap(lease, "runtimeRevisionRef") == stringMap(origin, "runtimeRevisionRef") || runtimeRevisionMapInt64(lease, "attempt") != runtimeRevisionMapInt64(origin, "attempt")+1 {
				t.Fatal("continuation lost exact session/turn/attempt/revision lineage")
			}
			var snapshot integrationContinuationSnapshot
			var pagedRequest *query.ExecutionIntegrationOutcomes
			read := func(input query.ExecutionIntegrationOutcomes) (entity.ExecutionIntegrationOutcomes, error) {
				return restartedService.GetExecutionIntegrationOutcomes(ctx, reader, input)
			}
			if decision == "APPROVE_LARGE" {
				var request query.ExecutionIntegrationOutcomes
				snapshot, request = readOrdinaryIntegrationContinuationFixture(t, lease, read, previousInvocation)
				pagedRequest = &request
			} else {
				snapshot = decodeIntegrationContinuationTask(t, stringMap(lease, "task"))
			}
			if expectedSummary != "" && snapshot.ResultSummary != expectedSummary {
				t.Fatal("continuation truncated the complete provider result")
			}
			if snapshot.InvocationRef != stringMap(invocation, "invocationRef") || snapshot.OwnerDecision != (integrationContinuationDecision{GateRef: gate.Ref, GateVersion: resolved.Gate.Version, Decision: actualDecision, Comment: comment}) {
				t.Fatal("claimed immutable task lost authoritative feedback pins")
			}
			if actualDecision == "APPROVE" && decision != "APPROVE_FAILED" {
				readback, err := service.GetIntegrationInvocation(ctx, worker, snapshot.InvocationRef)
				if err != nil || !strings.HasPrefix(snapshot.ReceiptRef, "erc_") || snapshot.ReceiptRef != stringMap(readback, "effectReceiptRef") {
					t.Fatal("claimed continuation lost the exact successful effect receipt")
				}
			} else if snapshot.ReceiptRef != "" {
				t.Fatal("unsuccessful invocation acquired an effect receipt")
			}
			var persistedTask, digest string
			if err := pool.QueryRow(ctx, integrationGateFeedbackSnapshotQuery, stringMap(lease, "runtimeRevisionRef"), stringMap(lease, "nodeRef"), run.Ref, stringMap(lease, "sessionRef")).Scan(&persistedTask, &digest); err != nil {
				t.Fatal("read exact persisted revision", err)
			}
			computed, err := runtimeRevisionDigestFromSnapshot(lease)
			if err != nil || computed != digest || persistedTask != stringMap(lease, "task") {
				t.Fatal("persisted immutable revision differs from runner input", err)
			}
			_, graph, err := service.GetRunGraph(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, edge := range graph.Edges {
				if edge.Type == "CONTINUES" {
					count++
				}
			}
			if count != 1 {
				t.Fatal("decision replay duplicated continuation")
			}
			events, _, _, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run.Ref, Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events {
				if event.Delta.Message != nil && event.Delta.Message.Source.Origin == "CALLBACK_CONTINUATION" {
					found = true
					if event.Delta.Message.Text != callbackContinuationPublicText || event.Delta.Node == nil || event.Delta.Node.InputSummary != callbackContinuationPublicText {
						t.Fatal("public callback exposed private runtime task")
					}
				}
			}
			if !found {
				t.Fatal("continuation lost its public typed source")
			}
			fresh, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal(err)
			}
			execute(t, command.CancelRun, owner, decision+"-cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Feedback fixture cleanup"})
			if pagedRequest != nil {
				if _, err := read(*pagedRequest); !errors.Is(err, errs.ErrNotFound) {
					t.Fatal("cancelled continuation still read its result", err)
				}
			}
			if previousInvocation == "" {
				previousInvocation = snapshot.InvocationRef
			}
		})
	}
}
