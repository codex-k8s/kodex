package platform

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func testSTTRoleMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	content, err := os.ReadFile("../../../../cmd/cli/migrations/20260904000610_issue_1046_stt_system_roles.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Штатный runner проверяет owner-role wrapper; здесь DML повторяется в rollback fixture.
	body := strings.TrimPrefix(string(content), "-- +goose Up\nSET ROLE control_plane_owner;\n")
	body = strings.TrimSuffix(body, "RESET ROLE;\n")
	if strings.Contains(body, "SET ROLE") || body == string(content) {
		t.Fatal("migration role wrapper changed")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roleID, oldVersionID string
	var oldRevision int64
	if err := tx.QueryRow(ctx, `WITH prior AS (
SELECT role.id owner_role_id, revision.* FROM control_plane.application_roles role
JOIN control_plane.application_role_versions revision ON revision.id=role.current_version_id
WHERE role.kind='SYSTEM' AND role.stable_key='OWNER' ORDER BY role.id LIMIT 1
), inserted AS (
INSERT INTO control_plane.application_role_versions
(ref,organization_id,role_id,revision,name,description,permission_keys,allowed_scopes,change_comment,created_by)
SELECT 'arv_stt_prior_fixture',organization_id,owner_role_id,revision+1,name,description,array_remove(permission_keys,'platform.stt.use'),allowed_scopes,'Prior fixture',created_by
FROM prior RETURNING role_id,id,revision)
SELECT role_id::text,id::text,revision FROM inserted`).Scan(&roleID, &oldVersionID, &oldRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.access_bindings SET role_version_id=$2::uuid WHERE role_version_id=(SELECT current_version_id FROM control_plane.application_roles WHERE id=$1::uuid) AND state='ACTIVE'`, roleID, oldVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.application_roles SET current_version_id=$2::uuid WHERE id=$1::uuid`, roleID, oldVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, body); err != nil {
		t.Fatal(err)
	}
	var currentVersionID string
	var revision int64
	var permission bool
	if err := tx.QueryRow(ctx, `SELECT version.id::text,version.revision,'platform.stt.use'=ANY(version.permission_keys)
FROM control_plane.application_roles role JOIN control_plane.application_role_versions version ON version.id=role.current_version_id
WHERE role.id=$1::uuid`, roleID).Scan(&currentVersionID, &revision, &permission); err != nil || !permission || revision != oldRevision+1 || currentVersionID == oldVersionID {
		t.Fatalf("STT role did not advance immutably: %v", err)
	}
	var staleBindings int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM control_plane.access_bindings WHERE role_version_id=$1::uuid AND state='ACTIVE'`, oldVersionID).Scan(&staleBindings); err != nil || staleBindings != 0 {
		t.Fatalf("active role bindings not advanced: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT 'platform.stt.use'=ANY(permission_keys) FROM control_plane.application_role_versions WHERE id=$1::uuid`, oldVersionID).Scan(&permission); err != nil || permission {
		t.Fatalf("prior role revision mutated: %v", err)
	}
	if _, err := tx.Exec(ctx, body); err != nil {
		t.Fatalf("migration reapplication: %v", err)
	}
}
