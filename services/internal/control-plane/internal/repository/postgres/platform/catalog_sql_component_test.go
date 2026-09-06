package platform

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testCatalogSQLParity(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT subject.organization_id::text, subject.id::text, subject.ref
FROM control_plane.subjects subject WHERE subject.active AND subject.kind = 'USER' ORDER BY subject.ref LIMIT 12`)
	if err != nil {
		t.Fatal(err)
	}
	var actors []scope
	for rows.Next() {
		var actor scope
		if err := rows.Scan(&actor.organizationID, &actor.actorID, &actor.actorRef); err != nil {
			t.Fatal(err)
		}
		actors = append(actors, actor)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	checked := 0
	for _, actor := range actors {
		subject, err := repository.resolveAccessSubject(ctx, tx, actor.organizationID, actor.actorRef)
		if err != nil {
			t.Fatal(err)
		}
		bindings, err := repository.loadAccessBindings(ctx, tx, actor.organizationID, subject)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := tx.Query(ctx, `SELECT kind, target.ref, COALESCE(project.ref, '') FROM control_plane.catalog_access_targets target
LEFT JOIN control_plane.projects project ON project.id = target.project_id WHERE target.organization_id = $1::uuid
ORDER BY kind, target.ref LIMIT 80`, actor.organizationID)
		if err != nil {
			t.Fatal(err)
		}
		type candidate struct{ kind, ref, project string }
		var targets []candidate
		for rows.Next() {
			var target candidate
			if err := rows.Scan(&target.kind, &target.ref, &target.project); err != nil {
				t.Fatal(err)
			}
			targets = append(targets, target)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		at := time.Now().UTC()
		for _, target := range targets {
			permission := visibilityPermission(target.kind)
			expected, err := repository.resourceVisible(ctx, tx, actor, subject.AccessSubject, bindings, target.kind, target.ref, target.project, at)
			if err != nil {
				t.Fatal(err)
			}
			var actual bool
			if err := tx.QueryRow(ctx, `SELECT control_plane.catalog_resource_visible($1::uuid, $2::uuid, $3, target.kind,
target.id, target.project_id, target.owner_id, target.related_ids, $6::timestamptz, target.kind = 'PROJECT')
FROM control_plane.catalog_access_targets target WHERE target.organization_id = $1::uuid AND target.kind = $4 AND target.ref = $5`,
				actor.organizationID, actor.actorID, permission, target.kind, target.ref, at).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			if expected != actual {
				t.Fatalf("catalog eligibility mismatch actor=%s kind=%s ref=%s expected=%v actual=%v", actor.actorRef, target.kind, target.ref, expected, actual)
			}
			checked++
		}
	}
	if checked < 10 {
		t.Fatalf("insufficient parity coverage: %d", checked)
	}
}
