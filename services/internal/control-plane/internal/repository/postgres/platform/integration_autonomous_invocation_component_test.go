package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationAutonomousGitHubInvocationComponent proves native NONE grants actually reach durable READY invocations, rather than stopping at a successful grant catalog; replay and cancellation retain exact owner lifecycle without external GitHub requests.
// TestIntegrationAutonomousGitHubInvocationComponent доказывает создание устойчивых READY invocations по native NONE grants, а не только успешный каталог; replay и cancel сохраняют owner lifecycle без внешних запросов GitHub.
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
	for _, operation := range operations {
		fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
		if err != nil {
			t.Fatal(err)
		}
		execute(command.ChangeIntegrationGrant, owner, operation.key, &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: operation.key, AgentRef: agent.Ref, ApprovalPolicy: "NONE", Enabled: true})
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
	fresh, err := service.GetRun(ctx, owner, run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	execute(command.CancelRun, owner, "cancel", &fresh.Version, command.RunCommandInput{RunRef: run.Ref, Reason: "Fixture cleanup"})
}
