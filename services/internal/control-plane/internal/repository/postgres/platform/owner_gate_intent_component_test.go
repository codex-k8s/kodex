package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/sql/gate_intent_source_turn.sql
var gateIntentSourceTurnQuery string

//go:embed testdata/sql/gate_intent_active_node.sql
var gateIntentActiveNodeQuery string

func gateTestProjectID(t *testing.T, ctx context.Context, repository *Repository, owner value.Principal, ref string) string {
	t.Helper()
	principal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "project.view", "PROJECT", ref, ref)
	if err != nil {
		t.Fatal(err)
	}
	return target.projectID
}

func testGateSourceAndHistoricalConsequences(t *testing.T, ctx context.Context, repository *Repository, service *platformservice.Service, owner value.Principal, gate entity.OwnerGate) {
	t.Helper()
	artifact, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "gate-source-artifact"}, platformrepo.ArtifactUpload{ProjectRef: gate.ProjectRef, FileName: "gate-source.txt", MediaType: "text/plain", SizeBytes: 6, Reader: strings.NewReader("source")})
	if err != nil {
		t.Fatalf("source fixture upload: %v", err)
	}
	ref := finalizedAttachmentSetRef(t, ctx, service, owner, gate.ProjectRef, "RUN_INPUT", "gate-source-set", artifact.Ref)
	principal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, target, err := repository.resolveCommandTarget(ctx, tx, current, "project.view", "PROJECT", gate.ProjectRef, gate.ProjectRef)
	if err != nil {
		t.Fatal(err)
	}
	set, err := repository.resolveFinalizedAttachmentSet(ctx, tx, current, target.projectID, ref, "RUN_INPUT", true)
	if err != nil {
		t.Fatal(err)
	}
	var turnID string
	if err := tx.QueryRow(ctx, gateIntentSourceTurnQuery, current.organizationID, gate.Ref).Scan(&turnID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, queryAttachmentSetsBindTurn, pgx.StrictNamedArgs{"attachment_set_id": set.ID, "turn_id": turnID}); err != nil {
		t.Fatal(err)
	}
	copy := gate
	copy.DecisionConsequences = nil
	if err := repository.projectGateIntent(ctx, tx, current, &copy, true); err != nil || copy.SourceAttachmentSetRef != ref {
		t.Fatalf("exact source attachment lineage lost: %v", err)
	}
	if copy.IntegrationIntent != nil || !copy.DecisionConsequences[0].TerminalForRun {
		t.Fatal("ordinary terminal gate consequences incorrect")
	}
	if tag, err := tx.Exec(ctx, gateIntentActiveNodeQuery, current.organizationID, gate.Ref); err != nil || tag.RowsAffected() == 0 {
		t.Fatalf("materialized active-node fixture: %v", err)
	}
	copy.DecisionConsequences = nil
	if err := repository.projectGateIntent(ctx, tx, current, &copy, true); err != nil || !copy.DecisionConsequences[0].TerminalForRun {
		t.Fatalf("historical terminal consequence changed with active nodes: %v", err)
	}
}

func testGateIntentReadBoundary(t *testing.T, ctx context.Context, repository *Repository, principal value.Principal, original entity.OwnerGate) {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, principal)
	if err != nil {
		t.Fatalf("resolve gate fixture principal: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, variant := range []string{"revoked-membership", "foreign-tenant", "signed-project"} {
		unauthorized := current
		if variant == "revoked-membership" {
			unauthorized.role = "VIEWER"
			unauthorized.actorID = "90000000-0000-4000-8000-000000000099"
		} else if variant == "foreign-tenant" {
			unauthorized.organizationID = "90000000-0000-4000-8000-000000000098"
		} else {
			unauthorized.authorityProjectID = "90000000-0000-4000-8000-000000000097"
		}
		copy := original
		if err := repository.projectGateIntent(ctx, tx, unauthorized, &copy, true); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("%s gate projection accepted: %v", variant, err)
		}
		copy = original
		result := command.Result{Gate: &copy}
		if err := repository.applyResultActionPermissions(ctx, tx, unauthorized, &result, copy.ProjectRef); err == nil {
			t.Fatalf("%s receipt projection accepted", variant)
		}
	}
	copy := original
	if err := repository.projectGateIntent(ctx, tx, current, &copy, true); err != nil || copy.IntegrationIntent == nil || copy.IntegrationIntent.EffectKey != original.IntegrationIntent.EffectKey {
		t.Fatalf("authorized historical gate projection failed: %v", err)
	}
}
