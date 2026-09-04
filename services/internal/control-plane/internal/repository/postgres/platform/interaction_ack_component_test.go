package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testInteractionACK(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool, owner value.Principal, connectionRef string) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "ack-project"}, Payload: command.ProjectInput{Name: "ACK project", Purpose: "Durable acceptance", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("ack project: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "ack-agent", "ACK agent")
	_, err = pool.Exec(ctx, `INSERT INTO control_plane.integration_grants
(ref,organization_id,connection_id,capability_key,target_kind,target_ref,created_by,resource_kind,definition_version,definition_digest)
SELECT 'ack_inbound_grant',organization_id,id,'mattermost.inbound','AGENT',$2,created_by,'MATTERMOST_CHANNEL',definition_version,definition_digest
FROM control_plane.integration_connections WHERE ref=$1`, connectionRef, agent.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET credential_materialization_ref='ack_fixture_credential' WHERE ref=$1`, connectionRef); err != nil {
		t.Fatal(err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "interaction-gateway", Operation: "platform.interactions.messages.accept",
	}, "interaction-gateway")
	message := command.Command{Kind: command.AcceptInteractionMessage, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "ack-accept"}, Payload: command.InteractionMessageInput{
		ConnectionRef: connectionRef, ExternalTeamRef: "team-ref", ExternalChannelRef: "channel-ref", ExternalUserDigest: strings.Repeat("b", 64),
		ExternalEventRef: "ack-event", ExternalPostRef: "ack-post", ExternalRootPostRef: "ack-root", Message: "Prepare bounded result",
	}}
	accepted, err := service.Execute(ctx, message)
	if err != nil || accepted.Run == nil {
		t.Fatalf("accept inbound: %v", err)
	}
	if _, err := service.Execute(ctx, message); err != nil {
		t.Fatalf("accept replay: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.interaction_deliveries delivery
JOIN control_plane.runs run ON run.id=delivery.root_run_id WHERE run.ref=$1 AND delivery.capability_key='mattermost.acknowledgements'`, accepted.Run.Ref).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ack cardinality %d: %v", count, err)
	}
	claims, err := service.ClaimInteractionDeliveries(ctx, worker, "ack-worker", 32)
	if err != nil {
		t.Fatal(err)
	}
	var claim map[string]any
	for _, item := range claims {
		if stringMap(item, "runRef") == accepted.Run.Ref && stringMap(item, "capabilityKey") == "mattermost.acknowledgements" {
			claim = item
		}
	}
	if claim == nil || stringMap(claim, "externalTeamRef") != "team-ref" || stringMap(claim, "externalChannelRef") != "channel-ref" ||
		stringMap(claim, "externalRootPostRef") != "ack-root" || stringMap(claim, "acceptanceReceiptRef") == "" {
		t.Fatalf("exact ACK claim missing: %v", claim != nil)
	}
	complete := command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker, Mutation: value.Mutation{IdempotencyKey: "ack-complete-wrong-channel"}, Payload: command.InteractionDeliveryInput{
		DeliveryRef: stringMap(claim, "deliveryRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
		Success: true, ExternalTeamRef: "team-ref", ExternalChannelRef: "other-channel", ExternalPostRef: "ack-result", ExternalThreadRef: "ack-root",
	}}
	if _, err := service.Execute(ctx, complete); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("wrong ACK channel: %v", err)
	}
	payload := complete.Payload.(command.InteractionDeliveryInput)
	payload.ExternalChannelRef = "channel-ref"
	payload.ExternalThreadRef = "other-root"
	complete.Payload = payload
	complete.Mutation.IdempotencyKey = "ack-complete-wrong-root"
	if _, err := service.Execute(ctx, complete); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("wrong ACK root: %v", err)
	}
	payload.ExternalThreadRef = "ack-root"
	complete.Payload = payload
	complete.Mutation.IdempotencyKey = "ack-complete"
	if _, err := service.Execute(ctx, complete); err != nil {
		t.Fatalf("ACK completion: %v", err)
	}
	if _, err := service.Execute(ctx, complete); err != nil {
		t.Fatalf("ACK completion replay: %v", err)
	}
}
