package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/interaction_approval_fixture.sql
var interactionApprovalFixture string

//go:embed testdata/sql/interaction_approval_root.sql
var interactionApprovalRoot string

//go:embed testdata/sql/interaction_approval_readback.sql
var interactionApprovalReadback string

//go:embed testdata/sql/interaction_approval_tamper.sql
var interactionApprovalTamper string

func testInteractionDeliveryApproval(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool, owner value.Principal, connectionRef, originalRunRef string) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "interaction-gateway", Operation: "platform.interactions.deliveries.claim",
	}, "interaction-gateway")
	for _, decision := range []string{"APPROVE", "REJECT", "CANCEL", "PIN_STALE", "OWNER_REVOKE"} {
		t.Run("delivery_gate_"+decision, func(t *testing.T) {
			key := "approval_" + strings.ToLower(decision)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var runID, projectID string
			if err := tx.QueryRow(ctx, interactionApprovalFixture, pgx.StrictNamedArgs{
				"run_ref": key + "_run", "node_ref": key + "_node", "grant_ref": "approval_notifications_grant",
				"original_run_ref": originalRunRef, "connection_ref": connectionRef,
			}).Scan(&runID, &projectID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, interactionApprovalRoot, runID); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, projectID, runID); err != nil {
					t.Fatalf("enqueue terminal approval: %v", err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			var gateRef, gateState, deliveryRef, deliveryState, runState string
			var runVersion, deliveryCount int64
			readback := func() {
				t.Helper()
				if err := pool.QueryRow(ctx, interactionApprovalReadback, key+"_run").Scan(&gateRef, &gateState, &deliveryRef, &deliveryState, &runState, &runVersion, &deliveryCount); err != nil {
					t.Fatal(err)
				}
				if runState != "SUCCEEDED" || deliveryCount != 1 {
					t.Fatal("optional delivery changed terminal run or duplicated effect")
				}
			}
			readback()
			initialVersion := runVersion
			if gateState != "OPEN" || deliveryState != "WAITING_APPROVAL" {
				t.Fatal("terminal delivery skipped human approval")
			}
			claimForRun := func() map[string]any {
				t.Helper()
				claims, err := service.ClaimInteractionDeliveries(ctx, worker, "approval-worker", 32)
				if err != nil {
					t.Fatal(err)
				}
				for _, claim := range claims {
					if claim["runRef"] == key+"_run" {
						return claim
					}
				}
				return nil
			}
			if claimForRun() != nil {
				t.Fatal("unapproved terminal delivery was claimed")
			}
			gate, err := service.GetOwnerGate(ctx, owner, gateRef)
			if err != nil || gate.ContextSummary == "" || len(gate.AllowedDecisions) != 3 {
				t.Fatal("optional approval lacks authoritative readback")
			}
			if decision == "OWNER_REVOKE" {
				connection, err := service.GetIntegrationConnection(ctx, owner, connectionRef)
				if err != nil {
					t.Fatal(err)
				}
				run, err := service.GetRun(ctx, owner, originalRunRef)
				if err != nil {
					t.Fatal(err)
				}
				_, err = service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: key + "-withdraw", ExpectedVersion: &connection.Version},
					Payload:  command.IntegrationGrantInput{ConnectionRef: connectionRef, CapabilityKey: "mattermost.notifications", AgentRef: run.Target.Ref, Enabled: false}})
				if err != nil {
					t.Fatalf("withdraw delivery authority: %v", err)
				}
				readback()
				if gateState != "CANCELLED" || deliveryState != "CANCELLED" || runVersion != initialVersion || claimForRun() != nil {
					t.Fatal("withdraw grant left a partial optional gate transition")
				}
				return
			}
			version := gate.Version + 1
			requestedDecision := decision
			if decision == "PIN_STALE" {
				requestedDecision = "APPROVE"
			}
			resolve := command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: key + "-stale", ExpectedVersion: &version},
				Payload:  command.GateResolutionInput{GateRef: gateRef, Decision: requestedDecision}}
			if _, err := service.Execute(ctx, resolve); !errors.Is(err, errs.ErrVersionMismatch) {
				t.Fatalf("stale optional gate: %v", err)
			}
			version = gate.Version
			resolve.Mutation.IdempotencyKey = key + "-decision"
			if decision == "PIN_STALE" {
				if _, err := pool.Exec(ctx, interactionApprovalTamper, deliveryRef); err != nil {
					t.Fatal(err)
				}
				resolve.Payload = command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE"}
				if _, err := service.Execute(ctx, resolve); !errors.Is(err, errs.ErrConflict) {
					t.Fatalf("invalid intent or revoked grant accepted approval: %v", err)
				}
				resolve.Mutation.IdempotencyKey = key + "-cancel"
				resolve.Payload = command.GateResolutionInput{GateRef: gateRef, Decision: "CANCEL"}
			}
			result, err := service.Execute(ctx, resolve)
			if err != nil || result.Gate == nil || result.Run == nil || result.Run.State != "SUCCEEDED" {
				t.Fatalf("optional gate decision changed core run: %v", err)
			}
			if _, err := service.Execute(ctx, resolve); err != nil {
				t.Fatalf("optional gate receipt replay: %v", err)
			}
			readback()
			if runVersion != initialVersion {
				t.Fatal("optional gate advanced core run version")
			}
			claim := claimForRun()
			if decision != "APPROVE" {
				if claim != nil || deliveryState != "CANCELLED" {
					t.Fatal("rejected optional effect remained executable")
				}
				return
			}
			if claim == nil || claim["approvalGateRef"] != gateRef || claim["approvalGateVersion"] != gate.Version+1 {
				t.Fatal("approved claim lost exact approval receipt")
			}
			definition, err := integrationpackage.Parse(claim["definitionPackage"].([]byte))
			if err != nil || definition.Digest != claim["definitionDigest"] {
				t.Fatal("delivery claim lost exact package")
			}
			_, err = service.Execute(ctx, command.Command{Kind: command.CompleteInteractionDelivery, Principal: worker,
				Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: command.InteractionDeliveryInput{
					DeliveryRef: deliveryRef, LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64),
					Success: true, ExternalPostRef: "approval-post", ExternalThreadRef: "approval-post", ExternalTeamRef: "team-ref", ExternalChannelRef: "channel-ref",
				}})
			if err != nil {
				t.Fatalf("approved effect completion: %v", err)
			}
			readback()
			if deliveryState != "SUCCEEDED" {
				t.Fatal("approved effect missing success readback")
			}
		})
	}
}
