package platform

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHExecutionTestDependency observes the original Gate, origin and continuation cardinality without manufacturing any owner state.
// querySSHExecutionTestDependency наблюдает original Gate, origin и continuation cardinality без создания owner state.
//
//go:embed testdata/sql/ssh_execution_test__dependency.sql
var querySSHExecutionTestDependency string

// TestSSHExecutionRevokeContinuationOwnerComponent revokes real personal authority after the original turn completes, retaining approval history and distinguishing unstarted cancellation from an uncertain attempted effect.
// TestSSHExecutionRevokeContinuationOwnerComponent отзывает real personal authority после завершения original turn, сохраняя approval history и отличая unstarted cancellation от uncertain attempted effect.
func TestSSHExecutionRevokeContinuationOwnerComponent(t *testing.T) {
	for _, scenario := range []string{"open_gate", "approved_gate", "attempted", "grant_revoke_sibling"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSSHSetupNativeFixture(t)
			authenticateSSHExecutionFixture(t, f)
			seedObservedCatalogFixture(t, f.ctx, f.repository)
			execute := newComponentCommandExecutor(t, f.ctx, f.service, "ssh-revoke-native-")
			project := execute(command.CreateProject, f.transport, "project", nil, command.ProjectInput{Name: "Disposable SSH revoke continuation", Language: "ru"}).Project
			agent := createLifecycleAgent(t, f.ctx, f.service, f.transport, project.Ref, "ssh-revoke-agent", "SSH reviewed service owner")
			f.connection = *execute(command.ChangeIntegrationGrant, f.transport, "grant", &f.connection.Version,
				command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: agent.Ref, CapabilityKey: "ssh.service.restart", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
			if scenario == "grant_revoke_sibling" {
				f.connection = *execute(command.ChangeIntegrationGrant, f.transport, "sibling-grant", &f.connection.Version,
					command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: agent.Ref, CapabilityKey: "ssh.file.create", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: true}).Connection
			}
			run := execute(command.LaunchRun, f.transport, "launch", nil, command.LaunchRunInput{ProjectRef: project.Ref,
				Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Task: "Wait for a reviewed synthetic SSH service operation"}).Run
			worker := resolvedTestPrincipal(t, f.ctx, f.repository, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
				CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
			leases := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-revoke-runtime", Limit: 1}).RuntimeItems
			if len(leases) != 1 || stringMap(leases[0], "runRef") != run.Ref {
				t.Fatal("claim original revoke runtime revision")
			}
			origin := leases[0]
			invocation, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(origin, "nodeRef"),
				"connection_ref": f.connection.Ref, "capability_key": "ssh.service.restart", "idempotency_key": "ssh-revoke-reviewed"}, map[string]any{"resource": "service_synthetic"})
			if err != nil || stringMap(invocation, "state") != "WAITING_APPROVAL" || stringMap(invocation, "gateRef") == "" {
				t.Fatal("resolve original reviewed SSH invocation", err)
			}
			gateRef := stringMap(invocation, "gateRef")
			var siblingRef string
			if scenario == "grant_revoke_sibling" {
				sibling, err := f.service.ResolveIntegrationInvocation(f.ctx, worker, map[string]string{"run_ref": run.Ref, "node_ref": stringMap(origin, "nodeRef"),
					"connection_ref": f.connection.Ref, "capability_key": "ssh.file.create", "idempotency_key": "ssh-revoke-sibling"},
					map[string]any{"resource": "files_synthetic", "path": "sibling.txt", "content_base64": "dGVzdA=="})
				if err != nil || stringMap(sibling, "state") != "WAITING_APPROVAL" || stringMap(sibling, "gateRef") == "" {
					t.Fatal("resolve independently reviewed SSH sibling", err)
				}
				siblingRef = stringMap(sibling, "gateRef")
			}
			if scenario == "approved_gate" || scenario == "attempted" {
				owner := f.transport
				owner.Permission = "platform.query.owner-gates.get"
				gate, err := f.service.GetOwnerGate(f.ctx, owner, gateRef)
				if err != nil {
					t.Fatal("read original SSH Gate", err)
				}
				execute(command.ResolveOwnerGate, f.transport, "approve", &gate.Version, command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE"})
			}
			if scenario == "attempted" {
				_, _, gateway := resolveIntegrationEffectTestPrincipals(t, f.ctx, f.repository)
				claims, err := f.service.ClaimIntegrationInvocationsWithCapacity(f.ctx, gateway, "ssh-revoke-gateway", 1, value.IntegrationInvocationDeliveryCapacity{SSH: 1})
				if err != nil || len(claims) != 1 || stringMap(claims[0], "invocationRef") != stringMap(invocation, "invocationRef") {
					t.Fatal("claim original attempted SSH operation", err)
				}
				claim := claims[0]
				binding := managedssh.Binding{WorkloadInstance: "ssh-revoke-gateway", InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
					Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), InputDigest: stringMap(claim, "inputDigest")}
				broker := f.broker
				broker.Permission = "platform.runtime.ssh.execution.begin"
				if _, err := f.repository.BeginSSHExecution(f.ctx, broker, binding); err != nil {
					t.Fatal("record original SSH dispatch intent", err)
				}
			}
			execute(command.CompleteExecution, worker, "complete-origin", nil, command.CompleteExecutionInput{NativeSession: nativeCheckpointForClaimFixture(origin),
				LeaseRef: stringMap(origin, "leaseRef"), Fence: stringMap(origin, "fence"), Generation: runtimeRevisionMapInt64(origin, "generation"),
				Success: true, ResultSummary: "Waiting for the SSH owner outcome", Usage: turnUsageFixture()})
			owner := f.transport
			if scenario == "approved_gate" {
				owner.Permission = "platform.query.runs.get"
				root, err := f.service.GetRun(f.ctx, owner, run.Ref)
				if err != nil || root.State == "SUCCEEDED" || root.State == "FAILED" || root.State == "CANCELLED" {
					t.Fatal("pending approved SSH invocation lost root liveness before revoke", err)
				}
			}
			owner.Permission = "platform.personal-integrations.get"
			personal, err := f.service.GetPersonalIntegrationCredential(f.ctx, owner, f.connection.Ref)
			if err != nil {
				t.Fatal("read exact personal authority before revoke", err)
			}
			revokeActor := f.transport
			revokeActor.Permission = "platform.personal-integrations.revoke"
			if scenario == "grant_revoke_sibling" {
				f.connection = *execute(command.ChangeIntegrationGrant, f.transport, "revoke-one-grant", &f.connection.Version,
					command.IntegrationGrantInput{ConnectionRef: f.connection.Ref, AgentRef: agent.Ref, CapabilityKey: "ssh.service.restart", ApprovalPolicy: "HUMAN_EACH_EFFECT", Enabled: false}).Connection
			} else {
				execute(command.RevokePersonalIntegrationCredential, revokeActor, "revoke", &personal.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref})
			}
			current, err := f.repository.resolveScope(f.ctx, f.owner)
			if err != nil {
				t.Fatal("resolve original revoke observer", err)
			}
			var state, reason, gateState, decision, rootState, nodeState string
			var continuations int64
			// read queries only durable original lineage; a result is not inferred from a computed view.
			// read запрашивает лишь durable original lineage; result не выводится из computed view.
			read := func() {
				t.Helper()
				if err := f.pool.QueryRow(f.ctx, querySSHExecutionTestDependency, pgx.StrictNamedArgs{"organization_id": current.organizationID,
					"invocation_ref": stringMap(invocation, "invocationRef")}).Scan(&state, &reason, &gateState, &decision, &rootState, &nodeState, &continuations); err != nil {
					t.Fatal("read exact SSH revoke/continuation facts", err)
				}
			}
			read()
			if nodeState != "SUCCEEDED" {
				t.Fatal("revoke fixture did not complete the original turn")
			}
			if scenario == "attempted" {
				if state != "UNKNOWN_OUTCOME" || reason != "INTEGRATION_OUTCOME_UNKNOWN" || gateState != "APPROVED" || decision != "APPROVE" || continuations != 0 {
					t.Fatal("attempted SSH revoke invented a known result or continuation")
				}
				return
			}
			if scenario == "grant_revoke_sibling" {
				owner.Permission = "platform.query.owner-gates.get"
				sibling, err := f.service.GetOwnerGate(f.ctx, owner, siblingRef)
				if err != nil || sibling.State != "OPEN" || state != "CANCELLED" || gateState != "CANCELLED" ||
					decision != "CANCEL" || rootState != "WAITING_HUMAN" {
					t.Fatal("grant revoke changed an unrelated SSH Gate or removed its wait", err)
				}
				return
			}
			if state != "CANCELLED" || reason != "INTEGRATION_AUTHORIZATION_DENIED" || continuations != 1 || rootState == "WAITING_HUMAN" {
				t.Fatalf("unstarted SSH revoke lost continuation: state=%s reason=%s root=%s continuations=%d", state, reason, rootState, continuations)
			}
			if scenario == "open_gate" && (gateState != "CANCELLED" || decision != "CANCEL") || scenario == "approved_gate" && (gateState != "APPROVED" || decision != "APPROVE") {
				t.Fatal("SSH revoke rewrote historical Gate authority")
			}
			execute(command.RevokePersonalIntegrationCredential, revokeActor, "revoke", &personal.Version, command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref})
			read()
			if continuations != 1 {
				t.Fatal("revoke replay duplicated the continuation")
			}
			next := execute(command.ClaimExecution, worker, "next-runtime", nil, command.LeaseInput{WorkloadInstance: "ssh-revoke-runtime-next", Limit: 1}).RuntimeItems
			if len(next) != 1 || stringMap(next[0], "runRef") != run.Ref || stringMap(next[0], "nodeRef") == stringMap(origin, "nodeRef") ||
				stringMap(next[0], "leaseRef") == stringMap(origin, "leaseRef") || !strings.Contains(stringMap(next[0], "task"), "CANCELLED") {
				t.Fatal("SSH revoke continuation was not a fresh claim with the original closed outcome")
			}
		})
	}
}
