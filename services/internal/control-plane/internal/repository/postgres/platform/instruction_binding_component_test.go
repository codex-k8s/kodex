package platform

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testInstructionBindingLifecycle(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, owner value.Principal, agentRef string) {
	agent, err := service.GetAgent(ctx, owner, agentRef)
	if err != nil || agent.InstructionBinding == nil || !agent.InstructionBinding.Effective || agent.InstructionBinding.Version != 1 {
		t.Fatalf("initial instruction binding: %v", err)
	}
	original := *agent.InstructionBinding
	run := func(kind command.Kind, key, content string) {
		t.Helper()
		payload := command.AgentInput{Ref: agentRef, Instructions: content}
		if kind == command.PublishInstructions {
			prepared, prepareErr := service.Execute(ctx, command.Command{Kind: command.PrepareInstructionsImpact, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-prepare", ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: agentRef}})
			if prepareErr != nil || prepared.RevisionImpactPlan == nil {
				t.Fatalf("prepare instruction impact: %v", prepareErr)
			}
			page, pageErr := service.GetRevisionImpactPlan(ctx, owner, prepared.RevisionImpactPlan.Ref, "", query.Page{Size: 100})
			if pageErr != nil || len(page.Items) != 1 {
				t.Fatalf("instruction impact items: %v", pageErr)
			}
			payload.PlanRef = page.Plan.Ref
			payload.SelectedItemRefs = []string{page.Items[0].Ref}
		}
		_, err = service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &agent.Version}, Payload: payload})
		if err != nil {
			t.Fatalf("instruction lifecycle %s: %v", key, err)
		}
		agent, err = service.GetAgent(ctx, owner, agentRef)
		if err != nil {
			t.Fatal(err)
		}
	}
	run(command.ValidateInstructions, "instruction-binding-validate", "")
	if agent.InstructionBinding.RevisionRef != original.RevisionRef || agent.InstructionBinding.Version != original.Version {
		t.Fatal("validation moved active binding")
	}
	run(command.PublishInstructions, "instruction-binding-publish", "")
	if agent.InstructionBinding.Ref != original.Ref || agent.InstructionBinding.Version != 2 || agent.InstructionBinding.RevisionRef == original.RevisionRef || agent.InstructionBinding.RevisionRef != agent.PublishedInstructions.Ref {
		t.Fatal("publication did not move exact binding")
	}
	published := agent.InstructionBinding.RevisionRef
	run(command.CreateInstructions, "instruction-binding-unselected-draft", "Published history must not activate an unselected consumer.")
	run(command.ValidateInstructions, "instruction-binding-unselected-validate", "")
	prepared, err := service.Execute(ctx, command.Command{Kind: command.PrepareInstructionsImpact, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "instruction-binding-unselected-plan", ExpectedVersion: &agent.Version}, Payload: command.AgentInput{Ref: agentRef}})
	if err != nil || prepared.RevisionImpactPlan == nil {
		t.Fatalf("unselected instruction plan: %v", err)
	}
	publishVersion := agent.Version
	publish := command.Command{Kind: command.PublishInstructions, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "instruction-binding-unselected-publish", ExpectedVersion: &publishVersion}, Payload: command.AgentInput{Ref: agentRef, PlanRef: prepared.RevisionImpactPlan.Ref, SelectedItemRefs: []string{}}}
	result, err := service.Execute(ctx, publish)
	if err != nil || result.RevisionImpactPlan == nil || result.RevisionImpactPlan.State != "APPLIED" {
		t.Fatalf("unselected instruction publication: %v", err)
	}
	agent, err = service.GetAgent(ctx, owner, agentRef)
	if err != nil || agent.InstructionBinding.RevisionRef != published || agent.InstructionBinding.Version != 2 || agent.PublishedInstructions.Ref == published {
		t.Fatalf("unselected publication moved active instructions: %v", err)
	}
	page, err := service.GetRevisionImpactPlan(ctx, owner, prepared.RevisionImpactPlan.Ref, "", query.Page{Size: 100})
	if err != nil || len(page.Items) != 1 || page.Items[0].Outcome != "NOT_SELECTED" {
		t.Fatalf("unselected instruction receipt: %v", err)
	}
	if replay, replayErr := service.Execute(ctx, publish); replayErr != nil || replay.RevisionImpactPlan == nil || replay.RevisionImpactPlan.Digest != result.RevisionImpactPlan.Digest {
		t.Fatalf("unselected instruction replay: %v", replayErr)
	}
	run(command.RollbackInstructions, "instruction-binding-rollback", original.RevisionRef)
	if agent.InstructionBinding.Ref != original.Ref || agent.InstructionBinding.Version != 3 || agent.InstructionBinding.RevisionRef == original.RevisionRef || agent.InstructionBinding.RevisionRef == published || agent.PublishedInstructions.ParentRef != original.RevisionRef {
		t.Fatal("rollback did not create and bind immutable revision")
	}
	var dependenciesRef, digest string
	var version int64
	var caps []string
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	var projectID string
	if err = r.pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE organization_id=$1::uuid AND ref=$2`, current.organizationID, agent.ProjectRef).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = r.pool.QueryRow(ctx, queryRunAttachmentAgentDependencies, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "agent_ref": agentRef}).Scan(&version, &caps, &dependenciesRef, &digest); err != nil || dependenciesRef != agent.InstructionBinding.RevisionRef {
		t.Fatalf("attachment dependency selected historical publication: %v", err)
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.agent_instruction_bindings SET version=version+2 WHERE ref=$1`, original.Ref); err == nil {
		t.Fatal("binding accepted skipped version")
	}
	if _, err = r.pool.Exec(ctx, `UPDATE control_plane.agent_instruction_bindings b SET instruction_id=(SELECT i.id FROM control_plane.instruction_versions i WHERE i.agent_id<>b.agent_id AND i.state='PUBLISHED' LIMIT 1),version=b.version+1 WHERE b.ref=$1`, original.Ref); err == nil {
		t.Fatal("binding accepted foreign Agent revision")
	}
}
