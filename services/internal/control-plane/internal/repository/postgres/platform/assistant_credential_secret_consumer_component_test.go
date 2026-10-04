package platform

import (
	"context"
	_ "embed"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/assistant_credential_secret_seed.sql
var queryAssistantCredentialSecretSeed string

//go:embed testdata/sql/assistant_credential_secret_revoke.sql
var queryAssistantCredentialSecretRevoke string

// Проверяется только owner SQL consumer: фикстура не подменяет RuntimeRevision,
// не доказывает создание свежего claim и не обращается к Kubernetes Secrets.
func testAssistantCredentialSecretConsumer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actorID, organizationID, projectID string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	type scopedDescriptor struct {
		ref, scope, project string
		descriptor          entity.RuntimeSecretRevisionDescriptor
	}
	fixtures := make(map[string]scopedDescriptor)
	for _, scope := range []string{"ORGANIZATION", "PROJECT"} {
		prefix := strings.ToLower(scope)
		project := projectID
		if scope == "ORGANIZATION" {
			project = ""
		}
		fixture := scopedDescriptor{scope: scope, project: project}
		err := tx.QueryRow(ctx, queryAssistantCredentialSecretSeed, pgx.StrictNamedArgs{
			"secret_ref": "sec_credential_scope_" + prefix, "revision_ref": "secr_credential_scope_" + prefix,
			"organization_id": organizationID, "actor_id": actorID, "project_id": project, "scope_kind": scope,
			"name": "credential-scope-" + prefix, "secret_name": "runtime-credential-scope-" + prefix + "-r1",
			"secret_uid": "10000000-0000-4000-8000-000000000111", "content_sha256": strings.Repeat("a", 64),
		}).Scan(&fixture.ref, &fixture.descriptor.Revision, &fixture.descriptor.Namespace,
			&fixture.descriptor.SecretName, &fixture.descriptor.SecretKey, &fixture.descriptor.SecretUID,
			&fixture.descriptor.SecretResourceVersion, &fixture.descriptor.ContentSHA256)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[scope] = fixture
	}
	for _, scope := range []string{"ORGANIZATION", "PROJECT"} {
		t.Run(scope, func(t *testing.T) {
			fixture := fixtures[scope]
			args := pgx.StrictNamedArgs{
				"organization_id": organizationID, "project_id": nullUUID(fixture.project), "scope_kind": fixture.scope,
				"secret_name": fixture.descriptor.SecretName, "secret_key": fixture.descriptor.SecretKey,
				"secret_uid": fixture.descriptor.SecretUID, "secret_resource_version": fixture.descriptor.SecretResourceVersion,
				"content_sha256": fixture.descriptor.ContentSHA256,
			}
			assertRead := func(args pgx.StrictNamedArgs, want bool) {
				t.Helper()
				rows, err := tx.Query(ctx, queryCredentialProjectionResolveRuntimeSecret, args)
				if err != nil {
					t.Fatal(err)
				}
				found := rows.Next()
				if found && want {
					var actualRef string
					var descriptor entity.RuntimeSecretRevisionDescriptor
					if err := rows.Scan(&actualRef, &descriptor.Revision, &descriptor.Namespace, &descriptor.SecretName,
						&descriptor.SecretKey, &descriptor.SecretUID, &descriptor.SecretResourceVersion, &descriptor.ContentSHA256); err != nil ||
						actualRef != fixture.ref || descriptor != fixture.descriptor {
						rows.Close()
						t.Fatalf("owner secret descriptor lost exact binding: %v", err)
					}
				}
				rows.Close()
				if found != want || rows.Err() != nil {
					t.Fatalf("scoped secret eligibility want=%v got=%v err=%v", want, found, rows.Err())
				}
			}
			assertRead(args, true)
			for name, mutate := range map[string]func(pgx.StrictNamedArgs){
				"foreign tenant":  func(args pgx.StrictNamedArgs) { args["organization_id"] = "30000000-0000-4000-8000-000000000001" },
				"foreign project": func(args pgx.StrictNamedArgs) { args["project_id"] = "30000000-0000-4000-8000-000000000002" },
				"opposite scope": func(args pgx.StrictNamedArgs) {
					if scope == "ORGANIZATION" {
						args["scope_kind"] = "PROJECT"
						args["project_id"] = projectID
					} else {
						args["scope_kind"] = "ORGANIZATION"
						args["project_id"] = nil
					}
				},
				"unknown scope":              func(args pgx.StrictNamedArgs) { args["scope_kind"] = "UNKNOWN" },
				"different name":             func(args pgx.StrictNamedArgs) { args["secret_name"] = "runtime-credential-foreign-r1" },
				"different key":              func(args pgx.StrictNamedArgs) { args["secret_key"] = "foreign-value" },
				"different uid":              func(args pgx.StrictNamedArgs) { args["secret_uid"] = "10000000-0000-4000-8000-000000000222" },
				"different resource version": func(args pgx.StrictNamedArgs) { args["secret_resource_version"] = "112" },
				"different digest":           func(args pgx.StrictNamedArgs) { args["content_sha256"] = strings.Repeat("b", 64) },
			} {
				t.Run(name, func(t *testing.T) {
					changed := make(pgx.StrictNamedArgs, len(args))
					for key, value := range args {
						changed[key] = value
					}
					mutate(changed)
					assertRead(changed, false)
				})
			}
			if tag, err := tx.Exec(ctx, queryAssistantCredentialSecretRevoke, pgx.StrictNamedArgs{"organization_id": organizationID, "secret_ref": fixture.ref}); err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("revoke exact fixture secret: %v", err)
			}
			assertRead(args, false)
		})
	}
}
