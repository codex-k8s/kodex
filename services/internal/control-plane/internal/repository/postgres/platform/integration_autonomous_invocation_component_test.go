package platform

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationAutonomousGitHubInvocationComponent proves native NONE grants reach durable
// invocations and actual gateway claims; replay and cancellation retain exact owner lifecycle
// without performing external GitHub requests or treating a READY queue entry as delivery.
// TestIntegrationAutonomousGitHubInvocationComponent доказывает создание устойчивых invocations
// по native NONE grants и их захват gateway; replay и cancel сохраняют owner lifecycle без
// внешних запросов GitHub и без подмены доставки наличием READY в очереди.
func TestIntegrationAutonomousGitHubInvocationComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated autonomous invocation PostgreSQL")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, principal value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: principal, Mutation: value.Mutation{IdempotencyKey: "autonomous-invocation-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native autonomous command %s failed: %v", kind, err)
		}
		return result
	}
	connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "github", Name: "Autonomous invocation repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}).Connection
	if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare isolated autonomous credential metadata")
	}
	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Autonomous invocation", Language: "en"}).Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "autonomous-invocation-agent", "Autonomous contributor")
	operations := []struct {
		key   string
		input map[string]any
	}{
		{"github.issue.create", map[string]any{"title": "Fixture issue"}},
		{"github.issue.update", map[string]any{"issue_number": 1, "title": "Updated fixture issue"}},
		{"github.repository.content.create", map[string]any{"path": "requirements.md", "branch": "feature", "message": "Fixture document", "content_base64": "VGV4dA=="}},
		{"github.repository.content.update", map[string]any{"path": "requirements.md", "branch": "feature", "message": "Fixture document", "content_base64": "VGV4dA==", "sha": strings.Repeat("a", 40)}},
		{"github.pull_request.comment.create", map[string]any{"pull_request_number": 1, "body": "Fixture response"}},
		{"github.pull_request.comment.update", map[string]any{"pull_request_number": 1, "comment_id": 4, "body": "Fixture response"}},
		{"github.pull_request.ready", map[string]any{"pull_request_number": 1, "expected_head_sha": strings.Repeat("a", 40)}},
	}
	grantKeys := []string{"github.pull_request.read", "github.repository.content.read"}
	for _, operation := range operations {
		grantKeys = append(grantKeys, operation.key)
	}
	for _, key := range grantKeys {
		fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.ChangeIntegrationGrant, owner, key, &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: key, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true})
	}
	run := execute(command.LaunchRun, owner, "run", nil, command.LaunchRunInput{ProjectRef: project.Ref, Title: "Autonomous invocation", Task: "Fixture native admission", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}}).Run
	claimed := execute(command.ClaimExecution, worker, "runtime", nil, command.LeaseInput{WorkloadInstance: "autonomous-invocation-runtime", Limit: 1}).RuntimeItems
	if len(claimed) != 1 {
		t.Fatal("autonomous runtime was not claimed")
	}
	for _, operation := range operations {
		input := map[string]string{"run_ref": run.Ref, "node_ref": stringMap(claimed[0], "nodeRef"), "connection_ref": connection.Ref, "capability_key": operation.key, "idempotency_key": "autonomous-effect-" + operation.key}
		first, err := service.ResolveIntegrationInvocation(ctx, worker, input, operation.input)
		if err != nil || stringMap(first, "state") != "READY" || stringMap(first, "gateRef") != "" {
			t.Fatalf("native NONE effect did not reach READY: operation=%s err=%v", operation.key, err)
		}
		replayed, err := service.ResolveIntegrationInvocation(ctx, worker, input, operation.input)
		if err != nil || stringMap(first, "invocationRef") != stringMap(replayed, "invocationRef") {
			t.Fatal("native invocation replay created another effect")
		}
	}
	_, _, gateway := resolveIntegrationEffectTestPrincipals(t, ctx, r)
	work, err := service.ClaimIntegrationInvocations(ctx, gateway, "autonomous-invocation-gateway", int32(len(operations)))
	if err != nil || len(work) != len(operations) {
		t.Fatalf("native NONE invocations were not claimed by the gateway: got=%d want=%d err=%v", len(work), len(operations), err)
	}
	seen := map[string]bool{}
	receipts := map[string]string{}
	for _, claim := range work {
		operation := stringMap(claim, "operation")
		if seen[operation] || stringMap(claim, "approvalPolicy") != "NONE" || stringMap(claim, "leaseRef") == "" {
			t.Fatalf("gateway claim lost its exact autonomous policy or lease: operation=%s", operation)
		}
		seen[operation] = true
		var output map[string]any
		provider := "github-issue:1"
		switch operation {
		case "github.issue.create", "github.issue.update":
			output = map[string]any{"number": 1, "title": "Fixture", "state": "open"}
		case "github.repository.content.create", "github.repository.content.update":
			output = map[string]any{"sha": strings.Repeat("a", 40), "path": claim["boundedInput"].(map[string]any)["path"]}
			provider = "github-commit:" + strings.Repeat("a", 40)
		case "github.pull_request.comment.create", "github.pull_request.comment.update":
			output = map[string]any{"comment_id": 1, "pull_request_number": 1}
			provider = "github-comment:1"
		case "github.pull_request.ready":
			output = map[string]any{"number": 1, "sha": strings.Repeat("a", 40), "state": "open", "draft": false, "url": "https://github.com/fixture/repository/pull/1", "already_ready": false}
			provider = "github-pull:1"
		default:
			t.Fatalf("unexpected autonomous gateway operation: %s", operation)
		}
		summary := string(asJSON(output))
		completion := command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
			Mutation: value.Mutation{IdempotencyKey: "autonomous-worker-complete-" + operation},
			Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
				Fence: stringMap(claim, "fence"), Generation: runtimeRevisionMapInt64(claim, "generation"), Success: true,
				EffectKey: stringMap(claim, "effectKey"), InputDigest: stringMap(claim, "inputDigest"),
				ProviderEffectRef: provider, ResultSummary: summary, ResponseDigest: workflowResultSHA([]byte(summary))}}
		completed, completeErr := service.Execute(ctx, completion)
		if completeErr != nil || completed.Event == nil || completed.Event.Delta.IntegrationInvocationRef != stringMap(claim, "invocationRef") {
			t.Fatalf("autonomous worker completion failed: operation=%s err=%v", operation, completeErr)
		}
		stored, readErr := service.GetIntegrationInvocation(ctx, gateway, stringMap(claim, "invocationRef"))
		if readErr != nil || stringMap(stored, "state") != "SUCCEEDED" || stringMap(stored, "effectReceiptRef") == "" || stringMap(stored, "gateRef") != "" || stringMap(stored, "resultSummary") != summary {
			t.Fatalf("autonomous completion did not persist an exact ungated receipt: operation=%s err=%v", operation, readErr)
		}
		receipts[operation] = stringMap(stored, "effectReceiptRef")
		_, beforeReplay, _, eventErr := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run.Ref})
		if eventErr != nil {
			t.Fatal("read autonomous event sequence before replay", eventErr)
		}
		if _, replayErr := service.Execute(ctx, completion); replayErr != nil {
			t.Fatalf("autonomous completion replay failed: operation=%s err=%v", operation, replayErr)
		}
		_, afterReplay, _, eventErr := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: run.Ref})
		if eventErr != nil || afterReplay != beforeReplay {
			t.Fatalf("autonomous completion replay appended another event: operation=%s err=%v", operation, eventErr)
		}
		replayed, replayErr := service.GetIntegrationInvocation(ctx, gateway, stringMap(claim, "invocationRef"))
		if replayErr != nil || !reflect.DeepEqual(replayed, stored) {
			t.Fatalf("autonomous completion replay changed its stored receipt: operation=%s err=%v", operation, replayErr)
		}
	}
	if remaining, claimErr := service.ClaimIntegrationInvocations(ctx, gateway, "autonomous-invocation-gateway", 10); claimErr != nil || len(remaining) != 0 {
		t.Fatalf("completed autonomous effects were claimed twice: count=%d err=%v", len(remaining), claimErr)
	}
	workflowPDLCReadOrderFixture{ctx: ctx, repository: r, service: service, pool: pool, owner: owner, worker: worker, gateway: gateway,
		runRef: run.Ref, connectionRef: connection.Ref, lease: claimed[0], writeReceiptRef: receipts["github.repository.content.update"]}.prove(t)
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Fixture cleanup"})
}
