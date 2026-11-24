package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationGitLabApprovalPredecessorsComponent keeps legacy and scoped
// connections at their original pinned authority after catalog promotion in a disposable database.
// TestIntegrationGitLabApprovalPredecessorsComponent сохраняет исходные полномочия
// прежних подключений после обновления каталога в одноразовой базе.
func TestIntegrationGitLabApprovalPredecessorsComponent(t *testing.T) {
	catalog, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	current := catalog["gitlab"]
	for _, historical := range []struct {
		version       string
		digest        string
		configuration map[string]any
	}{
		{"1.2.1", "1662b4b83b9a6b62c13ed448e8457ba547eb743f9c296349a4389c8ac1909062",
			map[string]any{"base_url": "https://git.example.invalid", "project_path": "fixture/project"}},
		{"2.0.0", "ce63966754a8575e0139b7f2887a6d0e4a8516e7fcfc0e4c4d888f134c51499e",
			map[string]any{"base_url": "https://git.example.invalid", "scope_kind": "GROUP", "group_path": "fixture"}},
	} {
		t.Run(historical.version, func(t *testing.T) {
			previous, ok := integrationpackage.ResolveShippedRevision(current, historical.version, historical.digest)
			if !ok {
				t.Fatal("missing predecessor fixture")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			repository := newSyntheticRuntimeComponentRepository(t, ctx, pool, func(r *Repository) {
				r.integrationDefinitions["gitlab"] = previous
			})
			owner, _, _ := resolveIntegrationEffectTestPrincipals(t, ctx, repository)
			service, err := platformservice.New(repository)
			if err != nil {
				t.Fatal(err)
			}
			execute := newComponentCommandExecutor(t, ctx, service, "gitlab-approval-predecessor")
			connection := execute(command.CreateConnection, owner, "create", nil, command.ConnectionInput{
				DefinitionKey: "gitlab", Name: "Previous GitLab package", PublicConfiguration: historical.configuration,
			}).Connection
			repository.integrationDefinitions["gitlab"] = current
			if err := repository.Bootstrap(ctx); err != nil {
				t.Fatal("promote GitLab registry", err)
			}
			var raw []byte
			var allowed bool
			if err := pool.QueryRow(ctx, `
				SELECT control_plane.integration_pinned_capabilities(id),
				       control_plane.integration_declared_policy_allowed(id,'gitlab.merge_request.approval.read',
				          'gitlab.merge_request.approval.read','READ','GITLAB_SCOPE','NONE','{}'::jsonb)
				FROM control_plane.integration_connections WHERE ref=$1`, connection.Ref).Scan(&raw, &allowed); err != nil {
				t.Fatal("read native predecessor admission", err)
			}
			var projected []struct{ Key, Risk, ApprovalPolicy, ResourceKind string }
			if allowed || json.Unmarshal(raw, &projected) != nil || len(projected) != len(previous.Spec.Capabilities) {
				t.Fatal("predecessor inherited the new grant or lost its previous catalog")
			}
			for index, capability := range previous.Spec.Capabilities {
				got := projected[index]
				if got.Key != capability.Key || got.Risk != capability.Risk || got.ApprovalPolicy != capability.ApprovalPolicy || got.ResourceKind != capability.ResourceScope.Kind {
					t.Fatal("SQL predecessor projection changed permission metadata")
				}
			}
		})
	}
}
