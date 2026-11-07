package platform

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryIntegrationJiraHistoricalFixture changes only inert historical metadata in disposable SQL.
// queryIntegrationJiraHistoricalFixture меняет только историческую metadata одноразовой SQL fixture.
//
//go:embed testdata/sql/integration_jira_historical_fixture.sql
var queryIntegrationJiraHistoricalFixture string

// TestIntegrationJiraHistoricalAdmissionComponent proves the native SQL and Go admission agree
// for old/current exact pins. Old connections retain grants without acquiring the new tool input;
// a substituted digest remains closed. No external request or live credential is used.
// TestIntegrationJiraHistoricalAdmissionComponent сверяет native SQL и Go admission старых/новых
// exact pins. Старые grants доступны без новых inputs; подменённый digest отклоняется. Live вызовов нет.
func TestIntegrationJiraHistoricalAdmissionComponent(t *testing.T) {
	for _, version := range []string{"1.0.0", "1.1.0", "substituted"} {
		t.Run(version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
			seedObservedCatalogFixture(t, ctx, r)
			owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
			service, err := serviceplatform.New(r)
			if err != nil {
				t.Fatal(err)
			}
			execute := newComponentCommandExecutor(t, ctx, service, "jira-history-")
			connection := execute(command.CreateConnection, owner, "connection", nil, command.ConnectionInput{DefinitionKey: "jira-server-dc", Name: "Synthetic historical issues", PublicConfiguration: map[string]any{"base_url": "https://jira.example.invalid", "context_path": "/tracker", "auth_scheme": "BEARER", "project_key": "DEMO", "issue_type": "Task"}}).Connection
			if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
				t.Fatal(err)
			}
			project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Synthetic historical admission", Language: "ru"}).Project
			agent := createLifecycleAgent(t, ctx, service, owner, project.Ref, "jira-history-agent", "Historical issue operator")
			current := r.integrationDefinitions["jira-server-dc"]
			digest := current.Digest
			selected := version
			if version == "1.0.0" {
				digest = "838878a5a984dafaa8d1c46563e3e660fc3b074515e25376e53ae0cae204fec8"
			}
			if version == "substituted" {
				selected = "1.0.0"
			}
			if _, err := pool.Exec(ctx, queryIntegrationJiraHistoricalFixture, connection.Ref, selected, digest); err != nil {
				t.Fatal(err)
			}
			input := query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref, ProjectRef: project.Ref, RecipientKind: "AGENT", RecipientRef: agent.Ref}, Filter: query.Filter{Page: query.Page{Size: 100}}}
			candidates, err := service.ListIntegrationGrantCapabilityCandidates(ctx, owner, input)
			if version == "substituted" {
				if err == nil && candidates.Total != 0 {
					t.Fatal("substituted historical pins became grantable")
				}
				return
			}
			definition, ok := integrationpackage.ResolveShippedRevision(current, selected, digest)
			if !ok || err != nil || int(candidates.Total) != len(definition.Spec.Capabilities) {
				t.Fatal("historical admission diverged", err, candidates.Total)
			}
			for _, item := range candidates.Items {
				if !item.Grantable || item.Pins.DefinitionDigest != digest {
					t.Fatal("historical candidate lost exact authority")
				}
			}
			fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			granted := execute(command.ChangeIntegrationGrant, owner, "read-grant", &fresh.Version, command.IntegrationGrantInput{ConnectionRef: connection.Ref, AgentRef: agent.Ref, CapabilityKey: "jira.project.read", ApprovalPolicy: "NONE", Enabled: true}).Connection
			if granted == nil || granted.DefinitionDigest != digest || granted.DefinitionVersion != selected {
				t.Fatal("read grant changed historical pins")
			}
		})
	}
}
