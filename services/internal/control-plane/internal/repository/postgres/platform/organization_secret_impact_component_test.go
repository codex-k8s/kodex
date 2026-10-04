package platform

import (
	"context"
	_ "embed"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/impact_permission_registry_readback.sql
var queryImpactPermissionRegistryReadback string

//go:embed testdata/sql/impact_legacy_receipt.sql
var queryImpactLegacyReceipt string

// Проверяется доменный fenced path на disposable БД без внешних Secret writes.
func testOrganizationSecretImpact(t *testing.T, ctx context.Context, r *Repository, service *platformservice.Service, pool *pgxpool.Pool, owner value.Principal, current scope, assistantRef string, base entity.RuntimeEnvironmentDraftSpecification, projectID string) {
	t.Helper()
	rows, err := pool.Query(ctx, queryImpactPermissionRegistryReadback)
	if err != nil {
		t.Fatal(err)
	}
	expectedKinds := map[string][]string{"secret.view": {"ORGANIZATION", "PROJECT", "SECRET"}, "secret.create": {"ORGANIZATION", "PROJECT"},
		"secret.rotate": {"ORGANIZATION", "SECRET"}, "secret.revoke": {"ORGANIZATION", "SECRET"}, "secret.reveal": {"ORGANIZATION", "SECRET"}}
	count := 0
	for rows.Next() {
		var permission string
		var kinds []string
		if err := rows.Scan(&permission, &kinds); err != nil || !reflect.DeepEqual(kinds, expectedKinds[permission]) {
			rows.Close()
			t.Fatalf("permission registry canonical resource kinds: %s: %v", permission, err)
		}
		count++
	}
	rows.Close()
	if rows.Err() != nil || count != len(expectedKinds) {
		t.Fatal("permission registry fixture is incomplete")
	}
	secret, err := service.GetRuntimeSecret(ctx, runtimeSecretOwnerPrincipal(owner, "secret.view"), "sec_env_scope_organization")
	if err != nil {
		t.Fatal(err)
	}
	claim := organizationEnvironmentClaimFixture(t, ctx, r, service, owner)
	bootstrapView, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
	if err != nil {
		t.Fatal(err)
	}
	claim("bootstrap-null-current", bootstrapView.Environment)
	createEnvironment := func(key string) entity.RuntimeEnvironmentSet {
		t.Helper()
		spec := base
		spec.Name = key
		spec.SecretBindings = []entity.RuntimeSecretBinding{{Name: "TOKEN", SecretRef: secret.Ref, Revision: secret.CurrentRevision}}
		invoke := func(kind command.Kind, suffix string, version *int64, input command.RuntimeEnvironmentDraftInput) command.Result {
			t.Helper()
			result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + suffix, ExpectedVersion: version}, Payload: input})
			if err != nil {
				t.Fatalf("organization impact environment %s: %v", suffix, err)
			}
			return result
		}
		draft := invoke(command.CreateOrganizationRuntimeEnvironmentDraft, "-create", nil, command.RuntimeEnvironmentDraftInput{Specification: spec}).RuntimeEnvironmentDraft
		draft = invoke(command.ValidateRuntimeEnvironmentDraft, "-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RuntimeEnvironmentDraft
		if draft.State != "VALID" {
			t.Fatal("organization impact environment is invalid")
		}
		plan := invoke(command.PrepareEnvironmentDraftImpact, "-impact", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RevisionImpactPlan
		return *invoke(command.PublishRuntimeEnvironmentDraft, "-publish", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref}).RuntimeEnvironment
	}
	boundEnvironment := createEnvironment("org-impact-bound")
	unboundEnvironment := createEnvironment("org-impact-unbound")
	view, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistantRef)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "org-impact-bind", ExpectedVersion: &view.AgentVersion},
		Payload:  command.RuntimeEnvironmentBindingInput{AgentRef: assistantRef, EnvironmentRef: boundEnvironment.Ref, VersionRef: boundEnvironment.CurrentVersion.Ref}})
	if err != nil || bound.RuntimeConfiguration == nil {
		t.Fatalf("pin organization assistant environment: %v", err)
	}
	consume := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.consume")
	complete := runtimeSecretSystemPrincipal(t, ctx, r, "platform.runtime-secrets.operations.complete")
	rotated := completeRuntimeSecretRotate(t, ctx, service, runtimeSecretOwnerPrincipal(owner, "secret.rotate"), consume, complete, secret, runtimeSecretHashB, "org-impact-rotate")
	impact, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, rotated.CurrentRevision, "", query.Page{Size: 20})
	if err != nil || impact.Total != 2 || len(impact.Consumers) != 2 {
		t.Fatalf("organization secret impact includes bound and unbound environments: total=%d: %v", impact.Total, err)
	}
	var selections []entity.RuntimeSecretRebindSelection
	for _, item := range impact.Consumers {
		if item.ScopeKind != "ORGANIZATION" || item.OrganizationRef != current.organizationRef || item.ProjectRef != "" ||
			item.Consumer.ScopeKind != item.ScopeKind || item.Consumer.OrganizationRef != item.OrganizationRef || item.Consumer.ProjectRef != "" {
			t.Fatal("organization impact snapshot owner mismatch")
		}
		selection := entity.RuntimeSecretRebindSelection{EnvironmentRef: item.EnvironmentRef, SourceVersionRef: item.EnvironmentVersionRef,
			ExpectedEnvironmentVersion: item.EnvironmentVersion, ScopeKind: item.ScopeKind, OrganizationRef: item.OrganizationRef, ProjectRef: item.ProjectRef}
		if item.Consumer.AgentRef != "" {
			selection.Consumers = []entity.RuntimeEnvironmentConsumer{item.Consumer}
		}
		selections = append(selections, selection)
	}
	invoke := func(p value.Principal, key string, items []entity.RuntimeSecretRebindSelection, version int64) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.RebindRuntimeSecret, Principal: p,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
			Payload:  command.RuntimeSecretRebindInput{SecretRef: secret.Ref, Revision: rotated.CurrentRevision, Selections: items}})
	}
	for _, test := range []struct {
		name   string
		mutate func(*entity.RuntimeSecretRebindSelection)
		want   error
	}{
		{"missing-scope", func(s *entity.RuntimeSecretRebindSelection) { s.ScopeKind = "" }, errs.ErrInvalid},
		{"unknown-scope", func(s *entity.RuntimeSecretRebindSelection) { s.ScopeKind = "UNKNOWN" }, errs.ErrInvalid},
		{"foreign-organization", func(s *entity.RuntimeSecretRebindSelection) { s.OrganizationRef = "org_foreign00000001" }, errs.ErrNotFound},
		{"project-tuple", func(s *entity.RuntimeSecretRebindSelection) {
			s.ScopeKind, s.ProjectRef = "PROJECT", "prj_foreign00000001"
		}, errs.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			items := append([]entity.RuntimeSecretRebindSelection(nil), selections...)
			test.mutate(&items[0])
			if _, err := invoke(owner, "org-impact-"+test.name, items, rotated.Version+100); !errors.Is(err, test.want) {
				t.Fatalf("owner boundary must precede OCC: %v", err)
			}
		})
	}
	signed := owner
	signed.ProjectRef = projectID
	if _, err := service.GetRuntimeSecretImpact(ctx, signed, secret.Ref, rotated.CurrentRevision, "", query.Page{}); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("signed project read organization impact: %v", err)
	}
	if _, err := invoke(signed, "org-impact-signed", selections, rotated.Version); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("signed project applied organization selection: %v", err)
	}
	result, err := invoke(owner, "org-impact-apply", selections, rotated.Version)
	if err != nil || len(result.RuntimeEnvironments) != 2 || len(result.EnvironmentBindings) != 1 {
		t.Fatalf("organization secret selected rebind: %v", err)
	}
	for _, environment := range result.RuntimeEnvironments {
		if environment.ScopeKind != "ORGANIZATION" || environment.OrganizationRef != current.organizationRef || environment.ProjectRef != "" || len(environment.CurrentVersion.SecretDescriptors) != 1 || environment.CurrentVersion.SecretDescriptors[0].Revision != rotated.CurrentRevision {
			t.Fatal("rebound organization environment owner or secret revision mismatch")
		}
	}
	for _, environment := range []entity.RuntimeEnvironmentSet{boundEnvironment, unboundEnvironment} {
		updated, err := service.GetRuntimeEnvironment(ctx, owner, environment.Ref)
		if err != nil || updated.Version != environment.Version+1 {
			t.Fatalf("selected environment publication: %v", err)
		}
	}
	if _, err := invoke(owner, "org-impact-apply", selections, rotated.Version); err != nil {
		t.Fatalf("organization selected rebind replay: %v", err)
	}
	legacy := result
	legacy.RuntimeEnvironments = append([]entity.RuntimeEnvironmentSet(nil), result.RuntimeEnvironments...)
	for i := range legacy.RuntimeEnvironments {
		legacy.RuntimeEnvironments[i].ScopeKind, legacy.RuntimeEnvironments[i].OrganizationRef = "", ""
	}
	if tag, err := pool.Exec(ctx, queryImpactLegacyReceipt, current.organizationID, current.actorID,
		"org-impact-apply", "org-impact-legacy-receipt", string(asJSON(legacy))); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("legacy receipt fixture: %v", err)
	}
	if _, err := invoke(owner, "org-impact-legacy-receipt", selections, rotated.Version); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("legacy response without owner snapshot was replayed: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(owner, "org-impact-apply", selections, rotated.Version); !errors.Is(err, errs.ErrNotFound) && !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("revoked owner replayed organization receipt: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, current.organizationID, current.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
	testOrganizationEnvironmentPinnedClaim(t, ctx, service, owner, current, assistantRef, base, claim)
	testSecretDraftImpactRebind(t, ctx, r, service, owner, rotated)
}
