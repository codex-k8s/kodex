package platform

import (
	"context"
	_ "embed"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// personalEmailMailboxBindingACLQuery checks effective runtime privileges and explicit PUBLIC/grant-option drift on the two immutable mailbox pin tables; it reads catalogs without mailbox data or application authority.
// personalEmailMailboxBindingACLQuery проверяет effective runtime privileges и лишние PUBLIC/grant-option права двух immutable mailbox pin tables; читает каталоги без mailbox data либо application authority.
//
//go:embed testdata/sql/personal_email_binding_acl.sql
var personalEmailMailboxBindingACLQuery string

// personalEmailMailboxBindingACLFixture checks the actual migrated tables from a separate nonowner session, then proves table UPDATE/DELETE, column UPDATE and PUBLIC SELECT overgrants are rejected. Each injected grant is removed before the next case, even after failure; existing purge privileges and old tables are untouched.
// personalEmailMailboxBindingACLFixture проверяет actual migrated tables отдельной nonowner session и отказ при лишних table UPDATE/DELETE, column UPDATE и PUBLIC SELECT. Каждый injected grant снимается до следующего случая, включая failure; прежние purge privileges и старые таблицы не меняются.
func personalEmailMailboxBindingACLFixture(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	admin, probe := newReadinessPrivilegeProbe(t, ctx, repository.pool.Config().ConnConfig)
	check := func(t *testing.T, current context.Context, expected bool) {
		t.Helper()
		var valid bool
		if err := probe.QueryRow(current, personalEmailMailboxBindingACLQuery).Scan(&valid); err != nil || valid != expected {
			t.Fatal("immutable mailbox pin privilege contract differs", err)
		}
	}
	check(t, ctx, true)
	for _, table := range []string{"email_invocation_mailbox_bindings", "email_approval_scope_bindings"} {
		for _, grant := range []struct{ name, privilege, grantee string }{
			{"runtime-update", "UPDATE", "control_plane_runtime"},
			{"runtime-delete", "DELETE", "control_plane_runtime"},
			{"runtime-column-update", "UPDATE(mailbox_ref)", "control_plane_runtime"},
			{"public-select", "SELECT", "PUBLIC"},
		} {
			t.Run(table+"/"+grant.name, func(t *testing.T) {
				object := pgx.Identifier{"control_plane", table}.Sanitize()
				if _, err := admin.Exec(ctx, "GRANT "+grant.privilege+" ON TABLE "+object+" TO "+grant.grantee); err != nil {
					t.Fatal("inject isolated mailbox pin overgrant", err)
				}
				t.Cleanup(func() {
					cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					defer cancel()
					if _, err := admin.Exec(cleanup, "REVOKE "+grant.privilege+" ON TABLE "+object+" FROM "+grant.grantee); err != nil {
						t.Error("restore exact mailbox pin privileges", err)
						return
					}
					check(t, cleanup, true)
				})
				check(t, ctx, false)
			})
		}
	}
}
