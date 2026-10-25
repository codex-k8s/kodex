package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryOwnerGateExpiryProof observes closure and event counts without injecting business state.
// queryOwnerGateExpiryProof читает закрытие и число событий без вставки готового бизнес-состояния.
//
//go:embed testdata/sql/owner_gate_expiry_proof.sql
var queryOwnerGateExpiryProof string

// TestOwnerGateExpiryComponent exercises real published policies, claims, completion and owner
// commands against disposable PostgreSQL. Time advances normally; no clock or state SQL is written.
// TestOwnerGateExpiryComponent проверяет опубликованные политики, claims, completion и owner-команды
// на одноразовом PostgreSQL. Время течёт обычно; тест не меняет часы или бизнес-состояние через SQL.
func TestOwnerGateExpiryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated gate expiry database")
	}
	defer pool.Close()
	r, service, owner, worker, _ := newReadyAssistantComponentService(t, ctx, pool)
	mutate := func(actor value.Principal, kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: actor, Mutation: value.Mutation{IdempotencyKey: "gate-expiry-" + key, ExpectedVersion: version}, Payload: payload})
	}
	execute := func(t *testing.T, actor value.Principal, kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := mutate(actor, kind, key, version, payload)
		if err != nil {
			t.Fatalf("gate expiry command %s failed: invalid=%t conflict=%t unavailable=%t", kind, errors.Is(err, errs.ErrInvalid), errors.Is(err, errs.ErrConflict), errors.Is(err, errs.ErrUnavailable))
		}
		return result
	}
	project := execute(t, owner, command.CreateProject, "project", nil, command.ProjectInput{Name: "Согласование результата", Language: "ru"}).Project
	other := execute(t, owner, command.CreateProject, "other", nil, command.ProjectInput{Name: "Другая область", Language: "ru"}).Project
	foreign := owner
	foreign.ProjectRef = gateTestProjectID(t, ctx, r, owner, other.Ref)
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Ref, "gate-expiry-coordinator", "Координатор")
	coordinator = *execute(t, owner, command.ChangeAgentCapability, "delegate", &coordinator.Version, command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true}).Agent
	specialist := createLifecycleAgent(t, ctx, service, owner, project.Ref, "gate-expiry-specialist", "Исполнитель")
	for _, name := range []string{"poll", "late-approve", "republished", "legacy", "cancel", "approve", "workflow-first", "race"} {
		if !t.Run(name, func(t *testing.T) {
			ttl := int32(2)
			stepTimeout := int32(600)
			if name == "workflow-first" {
				ttl, stepTimeout = 4, 2
			}
			step := entity.WorkflowStep{Key: "review", Position: 1, Name: "Результат", AgentRef: specialist.Ref, Instructions: "Подготовить результат для согласования.", ExpectedResult: "Проверенный результат", TimeoutSeconds: stepTimeout, HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "CANCEL"}, ApprovalTimeoutSeconds: &ttl}
			if name == "legacy" {
				step.ApprovalTimeoutSeconds = nil
			}
			draft := entity.WorkflowVersion{Name: name, CoordinatorAgentRef: coordinator.Ref, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}, Steps: []entity.WorkflowStep{step}}
			key := name + "-"
			workflow := execute(t, owner, command.CreateWorkflow, key+"create", nil, command.WorkflowInput{ProjectRef: project.Ref, Name: name, CoordinatorAgentRef: coordinator.Ref, Draft: &draft}).Workflow
			workflow = execute(t, owner, command.ValidateWorkflow, key+"validate", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
			workflow = execute(t, owner, command.PublishWorkflow, key+"publish", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
			run := execute(t, owner, command.LaunchRun, key+"launch", nil, command.LaunchRunInput{ProjectRef: project.Ref, Task: "Согласовать результат работы.", Target: entity.RunTarget{Type: "WORKFLOW", Ref: workflow.Ref}}).Run
			claim := func(suffix, expected string) map[string]any {
				items := execute(t, worker, command.ClaimExecution, key+suffix, nil, command.LeaseInput{WorkloadInstance: "gate-expiry-fixture", Limit: 1}).RuntimeItems
				if len(items) != 1 || stringMap(items[0], "runRef") != expected {
					t.Fatal("exact workflow claim missing")
				}
				return items[0]
			}
			complete := func(suffix string, lease map[string]any) {
				execute(t, worker, command.CompleteExecution, key+suffix, nil, command.CompleteExecutionInput{LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: runtimeRevisionMapInt64(lease, "generation"), Success: true, ResultSummary: "Результат подготовлен", Usage: turnUsageFixture()})
			}
			initial := claim("coordinator", run.Ref)
			child := execute(t, worker, command.DelegateExecution, key+"child", nil, command.DelegateInput{LeaseRef: stringMap(initial, "leaseRef"), Fence: stringMap(initial, "fence"), Generation: runtimeRevisionMapInt64(initial, "generation"), TargetAgentRef: specialist.Ref, WorkflowStepKey: "review", Task: "Подготовить результат."}).Run
			complete("coordinator-done", initial)
			lease := claim("child-claim", child.Ref)
			if name == "republished" {
				longTTL := int32(600)
				draft.Steps[0].ApprovalTimeoutSeconds = &longTTL
				workflow = execute(t, owner, command.UpdateWorkflow, key+"edit", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref, Draft: &draft}).Workflow
				workflow = execute(t, owner, command.ValidateWorkflow, key+"validate-new", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref}).Workflow
				execute(t, owner, command.PublishWorkflow, key+"publish-new", &workflow.Version, command.WorkflowInput{Ref: workflow.Ref})
			}
			complete("child-done", lease)
			current, err := service.GetRun(ctx, owner, run.Ref)
			if err != nil || len(current.GateRefs) != 1 {
				t.Fatal("result gate missing")
			}
			gate, err := service.GetOwnerGate(ctx, owner, current.GateRefs[0])
			if err != nil || gate.State != "OPEN" || (gate.ExpiresAt == nil) != (name == "legacy") {
				t.Fatal("authoritative gate expiry projection missing")
			}
			if gate.ExpiresAt != nil && gate.ExpiresAt.Sub(gate.CreatedAt) != time.Duration(ttl)*time.Second {
				t.Fatal("gate did not use one database instant and pinned policy")
			}
			payload := command.GateResolutionInput{GateRef: gate.Ref, Decision: "APPROVE", Comment: "Проверено"}
			if _, err := mutate(foreign, command.ResolveOwnerGate, key+"foreign", &gate.Version, payload); !errors.Is(err, errs.ErrNotFound) {
				t.Fatal("foreign gate escaped owner boundary")
			}
			if name == "cancel" || name == "approve" {
				decision := payload
				if name == "cancel" {
					decision.Decision = "CANCEL"
				}
				execute(t, owner, command.ResolveOwnerGate, key+"early", &gate.Version, decision)
			}
			waitUntil := gate.CreatedAt.Add(2300 * time.Millisecond)
			if gate.ExpiresAt != nil {
				waitUntil = gate.ExpiresAt.Add(200 * time.Millisecond)
			}
			timer := time.NewTimer(time.Until(waitUntil))
			defer timer.Stop()
			select {
			case <-ctx.Done():
				t.Fatal("bounded expiry wait exhausted")
			case <-timer.C:
			}
			if name == "race" {
				results := make(chan error, 2)
				go func() {
					_, err := mutate(owner, command.ResolveOwnerGate, key+"race-owner", &gate.Version, payload)
					if errors.Is(err, errs.ErrAlreadyResolved) {
						err = nil
					} else if err == nil {
						err = errs.ErrConflict
					}
					results <- err
				}()
				go func() {
					_, err := mutate(worker, command.ClaimExecution, key+"race-poll", nil, command.LeaseInput{WorkloadInstance: "gate-expiry-fixture", Limit: 1})
					results <- err
				}()
				for i := 0; i < 2; i++ {
					if <-results != nil {
						t.Fatal("concurrent expiry and late approval did not converge")
					}
				}
			} else if name == "late-approve" {
				for i := 0; i < 2; i++ {
					if _, err := mutate(owner, command.ResolveOwnerGate, key+"late", &gate.Version, payload); !errors.Is(err, errs.ErrAlreadyResolved) {
						t.Fatal("late approval or its receipt replay was accepted")
					}
				}
			} else {
				execute(t, worker, command.ClaimExecution, key+"tick", nil, command.LeaseInput{WorkloadInstance: "gate-expiry-fixture", Limit: 1})
			}
			closed, err := service.GetOwnerGate(ctx, owner, gate.Ref)
			expected := "EXPIRED"
			switch name {
			case "legacy":
				expected = "OPEN"
			case "cancel", "workflow-first":
				expected = "CANCELLED"
			case "approve":
				expected = "APPROVED"
			}
			if err != nil || closed.State != expected {
				t.Fatalf("unexpected gate outcome: expected=%s actual=%s", expected, closed.State)
			}
			if expected == "EXPIRED" {
				if closed.Decision != "" || closed.ResolvedByName != "" || closed.ResolvedAt == nil || closed.Version != gate.Version+1 {
					t.Fatal("server expiry impersonated an owner decision")
				}
				var active, leases, open, resolutions, events, invocations int64
				proof := func() {
					if pool.QueryRow(ctx, queryOwnerGateExpiryProof, pgx.StrictNamedArgs{"root_ref": run.Ref, "gate_ref": gate.Ref}).Scan(&active, &leases, &open, &resolutions, &events, &invocations) != nil {
						t.Fatal("read gate closure proof")
					}
				}
				proof()
				if active+leases+open+invocations != 0 || resolutions != 1 {
					t.Fatal("expiry left live work or duplicate resolution events")
				}
				before := events
				execute(t, worker, command.ClaimExecution, key+"second-tick", nil, command.LeaseInput{WorkloadInstance: "gate-expiry-fixture", Limit: 1})
				if _, err := mutate(owner, command.ResolveOwnerGate, key+"closed", &closed.Version, payload); !errors.Is(err, errs.ErrAlreadyResolved) {
					t.Fatal("fresh late approval accepted")
				}
				proof()
				if events != before {
					t.Fatal("closed expiry repeated graph events")
				}
			}
			current, err = service.GetRun(ctx, owner, run.Ref)
			if err != nil {
				t.Fatal("read final gate root")
			}
			if expected == "EXPIRED" && (current.State != "FAILED" || current.SafeErrorCode != "OWNER_GATE_EXPIRED") {
				t.Fatal("gate expiry was relabeled as execution timeout")
			}
			if !contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, current.State) {
				execute(t, owner, command.CancelRun, key+"cleanup", &current.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Завершить отдельную проверку"})
			}
		}) {
			t.FailNow()
		}
	}
}
