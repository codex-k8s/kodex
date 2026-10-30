package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryIntegrationGitHubHistoricalFixture restores only exact historical package metadata in a disposable fixture; it never changes a live connection or credential.
// queryIntegrationGitHubHistoricalFixture восстанавливает только metadata точного исторического пакета в disposable fixture; живые подключения и credentials не меняются.
//
//go:embed testdata/sql/integration_github_historical_fixture.sql
var queryIntegrationGitHubHistoricalFixture string

// TestIntegrationGitHubHistoricalAdmissionComponent verifies native candidate and grant parity for both immutable GitHub releases without inheriting autonomous writes, accepting substituted digests, or exposing another tenant's resources.
// TestIntegrationGitHubHistoricalAdmissionComponent проверяет согласованность native каталога и выдачи grant двух неизменяемых выпусков GitHub без наследования автономных записей, подмены digest и выдачи ресурсов другого tenant.
func TestIntegrationGitHubHistoricalAdmissionComponent(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := definitions["github"]
	for _, historical := range []struct{ version, digest string }{
		{"6.0.0", "537a92f2e304a5d670bf0bacc5dfae7d805b981aba5b262d2eb423b1afaaf44e"},
		{"6.1.0", "057959121df2b88db45d9b0dba438a7ff39824992fcf1c13120a44162071c79f"},
		{"6.2.0", "96809e1f78320bc5788eb0e39c672818d8b37d03efbf91f1f083936161eb7a13"},
		{current.Metadata.Version, current.Digest},
	} {
		t.Run(historical.version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal("open isolated historical admission PostgreSQL")
			}
			defer pool.Close()
			r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
			seedObservedCatalogFixture(t, ctx, r)
			owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
			service, err := serviceplatform.New(r)
			if err != nil {
				t.Fatal(err)
			}
			connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "historical-connection"}, Payload: command.ConnectionInput{DefinitionKey: "github", Name: "Historical repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Connection.Ref); err != nil {
				t.Fatal("prepare isolated credential metadata")
			}
			project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "historical-project"}, Payload: command.ProjectInput{Name: "Historical admission", Language: "en"}})
			if err != nil {
				t.Fatal(err)
			}
			agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "historical-agent", "Historical operator")
			if _, err := pool.Exec(ctx, queryIntegrationGitHubHistoricalFixture, connection.Connection.Ref, historical.version, historical.digest); err != nil {
				t.Fatal("prepare exact historical package pins")
			}
			previous, found := integrationpackage.ResolveShippedRevision(current, historical.version, historical.digest)
			if !found {
				t.Fatal("historical package cannot resolve exact pins")
			}
			var raw []byte
			if historical.version != current.Metadata.Version {
				if err := pool.QueryRow(ctx, "SELECT control_plane.integration_github_legacy_capabilities($1,$2,$3,$4)", current.Metadata.Version, current.Digest, historical.version, historical.digest).Scan(&raw); err != nil {
					t.Fatal("read exact historical admission metadata")
				}
				var actual []map[string]string
				expected := make([]map[string]string, 0, len(previous.Spec.Capabilities))
				for _, capability := range previous.Spec.Capabilities {
					expected = append(expected, map[string]string{"key": capability.Key, "name": capability.Name, "operation": capability.Operation, "risk": capability.Risk})
				}
				if json.Unmarshal(raw, &actual) != nil || !reflect.DeepEqual(actual, expected) {
					t.Fatal("historical SQL admission differs from the immutable package snapshot")
				}
			}
			input := query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Connection.Ref, ProjectRef: project.Project.Ref, RecipientKind: "AGENT", RecipientRef: agent.Ref}, Filter: query.Filter{Page: query.Page{Size: 100}}}
			candidates, err := service.ListIntegrationGrantCapabilityCandidates(ctx, owner, input)
			if err != nil || int(candidates.Total) != len(previous.Spec.Capabilities) || len(candidates.Items) != len(previous.Spec.Capabilities) {
				t.Fatalf("historical catalog does not match its frozen package: total=%d err=%v", candidates.Total, err)
			}
			for _, item := range candidates.Items {
				if !item.Grantable || item.Pins.DefinitionDigest != historical.digest {
					t.Fatal("historical candidate lost readiness or exact digest")
				}
			}
			fresh, err := service.GetIntegrationConnection(ctx, owner, connection.Connection.Ref)
			if err != nil {
				t.Fatal(err)
			}
			granted, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "historical-read-grant", ExpectedVersion: &fresh.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: fresh.Ref, AgentRef: agent.Ref, CapabilityKey: "github.pull_request.read", ApprovalPolicy: "NONE", Enabled: true}})
			if err != nil || granted.Connection == nil {
				t.Fatal("historical native read grant was rejected")
			}
			for index, key := range []string{"github.issue.create", "github.issue.update", "github.repository.content.create", "github.repository.content.update", "github.repository.bundle.publish", "github.pull_request.merge", "github.pull_request.ready", "github.pull_request.comment.create", "github.pull_request.comment.update"} {
				result, err := service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: fmt.Sprintf("historical-policy-%d", index), ExpectedVersion: &granted.Connection.Version}, Payload: command.IntegrationGrantInput{ConnectionRef: fresh.Ref, AgentRef: agent.Ref, CapabilityKey: key, ApprovalPolicy: "NONE", Enabled: true}})
				_, present := previous.Capability(key)
				capability, _ := previous.Capability(key)
				allowed := present && capability.AllowsApprovalPolicy("NONE")
				if (err == nil) != allowed {
					t.Fatalf("native autonomous policy mismatch: operation=%s allowed=%t error_nil=%t", key, allowed, err == nil)
				}
				if allowed {
					granted = result
				}
			}
			foreign := owner
			foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
			if rows, err := service.ListIntegrationGrantCapabilityCandidates(ctx, foreign, input); err == nil && (rows.Total != 0 || len(rows.Items) != 0) {
				t.Fatal("historical catalog exposed another tenant's candidates")
			}
			if _, err := pool.Exec(ctx, queryIntegrationGitHubHistoricalFixture, fresh.Ref, historical.version, strings.Repeat("0", 64)); err != nil {
				t.Fatal("prepare substituted digest negative fixture")
			}
			if rows, err := service.ListIntegrationGrantCapabilityCandidates(ctx, owner, input); err == nil && (rows.Total != 0 || len(rows.Items) != 0) {
				t.Fatal("historical catalog accepted a substituted digest")
			}
		})
	}
}
