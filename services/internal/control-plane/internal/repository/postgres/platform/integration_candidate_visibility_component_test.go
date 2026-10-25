package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestIntegrationCandidateVisibilityComponent instruments the unchanged candidate SQL with a
// temporary transaction-local visibility counter and checks exact
// RECIPIENT/PROJECT/CONNECTION/CAPABILITY call cardinality. It also checks unique totals, one-item
// pagination, changed-query cursor rejection and foreign authority denial. The counter/fixtures
// roll back in a disposable database and do not change the production predicate or prove external
// GitHub eligibility.
// TestIntegrationCandidateVisibilityComponent инструментирует неизменённый candidate SQL временным
// transaction-local visibility counter и проверяет точную кратность вызовов
// RECIPIENT/PROJECT/CONNECTION/CAPABILITY. Также проверяет unique totals, страницы по одному item,
// отказ cursor при смене query и чужих полномочий. Counter/fixtures откатываются в disposable БД,
// не меняют production predicate и не доказывают внешнюю GitHub eligibility.
func TestIntegrationCandidateVisibilityComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated candidate PostgreSQL")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "visibility-project"}, Payload: command.ProjectInput{Name: "Visibility cardinality", Language: "en"}})
	if err != nil {
		t.Fatal(err)
	}
	project := *created.Project
	var agent entity.Agent
	for index := 0; index < 6; index++ {
		agent = createLifecycleAgent(t, ctx, service, owner, project.Ref, fmt.Sprintf("visibility-agent-%d", index), fmt.Sprintf("Visibility agent %d", index))
	}
	connectionResult, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "visibility-connection"}, Payload: command.ConnectionInput{DefinitionKey: "github", Name: "Visibility repository", PublicConfiguration: map[string]any{"owner": "fixture", "repository": "repository"}}})
	if err != nil {
		t.Fatal(err)
	}
	connection := *connectionResult.Connection
	if _, err = pool.Exec(ctx, queryIntegrationGrantPolicyCredentialFixture, connection.Ref); err != nil {
		t.Fatal("prepare synthetic credential metadata")
	}
	resolvedOwner, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve owner for isolated visibility snapshot")
	}
	current, err := r.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		stage        string
		calls, total int64
	}{{"RECIPIENT", 6, 6}, {"PROJECT", 1, 1}, {"CONNECTION", 1, 1}, {"CAPABILITY", 0, 0}} {
		t.Run(fixture.stage, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			// Только disposable transaction: считаем реальные вызовы того же
			// eligibility predicate, не меняя production function или права.
			if _, err = tx.Exec(ctx, `CREATE TEMP SEQUENCE candidate_visibility_calls;
CREATE FUNCTION pg_temp.candidate_visible(uuid,uuid,text,text,uuid,uuid,uuid,jsonb,timestamptz)
RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$ BEGIN
PERFORM nextval('pg_temp.candidate_visibility_calls');
RETURN control_plane.catalog_resource_visible($1,$2,$3,$4,$5,$6,$7,$8,$9);
END $$`); err != nil {
				t.Fatal("create isolated visibility counter")
			}
			recipientRef := ""
			if fixture.stage == "CAPABILITY" {
				recipientRef = agent.Ref
			}
			args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project_id": current.authorityProjectID, "connection_ref": connection.Ref, "project_ref": project.Ref, "recipient_kind": "AGENT", "recipient_ref": recipientRef, "capability_key": "", "purpose": "GRANT", "workflow_capabilities": []string(nil), "stage": fixture.stage, "query": "", "after_ref": "", "page_limit": 101}
			var total, calls int64
			var raw []byte
			instrumented := strings.ReplaceAll(queryIntegrationGrantCandidates, "control_plane.catalog_resource_visible(", "pg_temp.candidate_visible(")
			if err = tx.QueryRow(ctx, instrumented, args).Scan(&total, &raw); err != nil {
				t.Fatal("read instrumented owner candidates")
			}
			if err = tx.QueryRow(ctx, `SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM pg_temp.candidate_visibility_calls`).Scan(&calls); err != nil {
				t.Fatal("read isolated visibility count")
			}
			var rows []integrationCandidateRow
			if json.Unmarshal(raw, &rows) != nil || calls != fixture.calls || fixture.total > 0 && total != fixture.total || fixture.stage == "CAPABILITY" && total < 2 {
				t.Fatalf("visibility calls=%d want=%d total=%d", calls, fixture.calls, total)
			}
			t.Logf("stage=%s visibility_calls=%d unique_candidates=%d", fixture.stage, calls, total)
		})
	}
	input := query.IntegrationCandidates{Purpose: "GRANT", Context: query.IntegrationCandidateContext{ConnectionRef: connection.Ref, ProjectRef: project.Ref, RecipientKind: "AGENT"}, Filter: query.Filter{Page: query.Page{Size: 1}}}
	page, err := service.ListIntegrationGrantRecipientCandidates(ctx, owner, input)
	if err != nil || page.Total != 6 || len(page.Items) != 1 || page.NextPageToken == "" {
		t.Fatal("owner page lost unique count or cursor")
	}
	input.Filter.Page.Token = page.NextPageToken
	next, err := service.ListIntegrationGrantRecipientCandidates(ctx, owner, input)
	if err != nil || next.Total != 6 || len(next.Items) != 1 || next.Items[0].RecipientRef == page.Items[0].RecipientRef {
		t.Fatal("owner continuation lost exact unique candidates")
	}
	input.Filter.Query = "different"
	if _, err := service.ListIntegrationGrantRecipientCandidates(ctx, owner, input); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("cursor accepted changed query")
	}
	foreign := owner
	foreign.AuthorityTenant = "90000000-0000-4000-8000-000000000099"
	if _, err := service.ListIntegrationGrantRecipientCandidates(ctx, foreign, input); err == nil {
		t.Fatal("foreign authority received candidate counts")
	}
}
