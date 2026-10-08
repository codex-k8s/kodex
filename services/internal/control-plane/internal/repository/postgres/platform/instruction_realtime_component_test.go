package platform

import (
	"context"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Проверяется сохранённый owner outbox, а не только commandOutcome: тот же
// факт должен доходить до AGENT/INSTRUCTIONS каталога после standalone и plan apply.
func instructionRealtimeEventCount(t *testing.T, ctx context.Context, repository *Repository, agentRef string) int {
	t.Helper()
	var count int
	if err := repository.pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.outbox_events WHERE convert_from(payload,'UTF8')::jsonb->>'aggregateRef'=$1 AND convert_from(payload,'UTF8')::jsonb->>'eventName' IN ('AGENT_CHANGED','INSTRUCTIONS_PUBLISHED')`, agentRef).Scan(&count); err != nil {
		t.Fatal("read instruction realtime event count", err)
	}
	return count
}

func assertInstructionRealtimeEvent(t *testing.T, ctx context.Context, repository *Repository, agent entity.Agent, before int, name string) {
	t.Helper()
	if count := instructionRealtimeEventCount(t, ctx, repository, agent.Ref); count != before+1 {
		t.Fatalf("instruction change emitted %d events, want one", count-before)
	}
	var eventName, kind, projectRef string
	var version int64
	if err := repository.pool.QueryRow(ctx, `SELECT event->>'eventName',event->'data'->>'kind',COALESCE(event->>'projectRef',''),(event->>'aggregateVersion')::bigint FROM (SELECT sequence,convert_from(payload,'UTF8')::jsonb AS event FROM control_plane.outbox_events) events WHERE event->>'aggregateRef'=$1 AND event->>'eventName' IN ('AGENT_CHANGED','INSTRUCTIONS_PUBLISHED') ORDER BY sequence DESC LIMIT 1`, agent.Ref).Scan(&eventName, &kind, &projectRef, &version); err != nil {
		t.Fatal("read instruction realtime event", err)
	}
	if eventName != name || kind != platformEventKind(name) || projectRef != agent.ProjectRef || version != agent.Version {
		t.Fatalf("instruction realtime event lost exact target: name=%s kind=%s project=%s version=%d", eventName, kind, projectRef, version)
	}
}

func testAssistantInstructionRealtime(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner, worker value.Principal, lease map[string]any, agentRef, key string) entity.Agent {
	t.Helper()
	const instructions = "Prepare intake from Issue, scope, acceptance and available base SHA. Inspect PR and diff when they exist."
	proposed := executeWorkerAssistantPlan(t, ctx, service, worker, lease, key, entity.AssistantPlanOperation{
		Key: "instructions", Type: "CREATE_INSTRUCTION_DRAFT", Title: "Prepare intake instructions", Summary: "Prepare a native draft for owner publication",
		Parameters: map[string]any{"agentRef": agentRef, "instructions": instructions},
	})
	if proposed.Plan == nil || len(proposed.Plan.Operations) != 1 || proposed.Plan.Operations[0].Target.Ref != agentRef {
		t.Fatal("instruction plan lost exact Agent target")
	}
	plan := proposed.Plan
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-validate", ExpectedVersion: &plan.Version}, Payload: command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}})
	if err != nil || validated.Plan == nil || validated.Plan.State != "VALID" {
		t.Fatal("validate instruction plan", err)
	}
	before := instructionRealtimeEventCount(t, ctx, repository, agentRef)
	apply := command.Command{Kind: command.ApplyAssistantPlan, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-apply", ExpectedVersion: &validated.Plan.Version}, Payload: command.AssistantPlanInput{PlanRef: plan.Ref, Revision: plan.Revision}}
	result, err := service.Execute(ctx, apply)
	if err != nil || result.PlanReceipt == nil || result.PlanReceipt.Outcome != "APPLIED" {
		t.Fatal("apply instruction plan", err)
	}
	agent, err := service.GetAgent(ctx, owner, agentRef)
	if err != nil || agent.DraftInstructions == nil || agent.DraftInstructions.Content != instructions || agent.DraftInstructions.State != "DRAFT" {
		t.Fatal("instruction plan authoritative readback", err)
	}
	assertInstructionRealtimeEvent(t, ctx, repository, agent, before, "AGENT_CHANGED")
	if _, err := service.Execute(ctx, apply); err != nil {
		t.Fatal("replay instruction plan", err)
	}
	if count := instructionRealtimeEventCount(t, ctx, repository, agentRef); count != before+1 {
		t.Fatal("instruction plan replay emitted a duplicate Agent event")
	}
	return agent
}
