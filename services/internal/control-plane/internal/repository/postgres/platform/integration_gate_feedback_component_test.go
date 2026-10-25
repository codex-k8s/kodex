package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationGateFeedbackSnapshotQuery reads the exact persisted continuation task using
// revision, node, run and session pins; it never changes fixture business state.
// integrationGateFeedbackSnapshotQuery читает точную сохранённую задачу продолжения по pins
// revision, node, run и session; запрос не меняет бизнес-состояние тестовой оснастки.
//
//go:embed testdata/sql/integration_gate_feedback_snapshot.sql
var integrationGateFeedbackSnapshotQuery string

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
	for _, decision := range []string{"APPROVE", "APPROVE_FAILED", "REJECT", "CANCEL", "EMPTY_REJECT", "TERMINAL"} {
		t.Run(decision, func(t *testing.T) {
			run := execute(t, command.LaunchRun, owner, decision+"-run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Feedback effect", Task: "Preserve canonical input", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
			claimed := execute(t, command.ClaimExecution, worker, decision+"-origin", nil, command.LeaseInput{WorkloadInstance: "feedback-runtime", Limit: 1}).RuntimeItems
			if len(claimed) != 1 {
				t.Fatal("origin was not claimed")
			}
			origin := claimed[0]
			invocation, err := service.ResolveIntegrationInvocation(ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(origin, "nodeRef"), "connection_ref": connection.Ref, "capability_key": "github.pull_request.review.create", "idempotency_key": "feedback-effect-" + decision}, map[string]any{"pull_request_number": 1, "sha": strings.Repeat("a", 40), "body": "Fixture review", "event": "COMMENT"})
			if err != nil {
				t.Fatal(err)
			}
			execute(t, command.CompleteExecution, worker, decision+"-complete-origin", nil, command.CompleteExecutionInput{LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"), Success: true, ResultSummary: "Waiting for owner", Usage: turnUsageFixture()})
			gate, err := service.GetOwnerGate(ctx, owner, stringMap(invocation, "gateRef"))
			if err != nil {
				t.Fatal(err)
			}
			comment := "Сохранить точные 624B паспорта\nи canonical input. Не повторять WRITE. 🧪 <>&"
			actualDecision := decision
			if decision == "APPROVE_FAILED" {
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
			if actualDecision == "APPROVE" {
				work, err := service.ClaimIntegrationInvocations(ctx, gateway, "feedback-gateway", 1)
				if err != nil || len(work) != 1 {
					t.Fatal("approved synthetic effect was not claimed", err)
				}
				completion := command.IntegrationInvocationInput{InvocationRef: stringMap(work[0], "invocationRef"), LeaseRef: stringMap(work[0], "leaseRef"), Fence: stringMap(work[0], "fence"), Generation: runtimeRevisionMapInt64(work[0], "generation"), SafeErrorCode: "INTEGRATION_REQUEST_REJECTED"}
				if decision == "APPROVE" {
					summary := `{"fixture":"exact provider result, not owner instructions"}`
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
			snapshot := decodeIntegrationContinuationTask(t, stringMap(lease, "task"))
			if snapshot.InvocationRef != stringMap(invocation, "invocationRef") || snapshot.OwnerDecision != (integrationContinuationDecision{GateRef: gate.Ref, GateVersion: resolved.Gate.Version, Decision: actualDecision, Comment: comment}) {
				t.Fatal("claimed immutable task lost authoritative feedback pins")
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
		})
	}
}
