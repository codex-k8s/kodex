package platform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// TestMembershipLockHelpersPreserveProjectionAndOwnerScope checks both distinct locking queries,
// exact owner/project/ref arguments and eleven/ten-field projections, including separate subject
// and binding activity. Missing/wrapped rows and scan/partial failures retain their domain mapping
// with no commit or policy/write calls; platform reads preserve existing project-permission data.
// TestMembershipLockHelpersPreserveProjectionAndOwnerScope проверяет два отдельных locking query,
// точные owner/project/ref arguments и проекции одиннадцати/десяти полей, включая разные subject
// и binding activity. Missing/wrapped rows и scan/partial failures сохраняют domain mapping без
// commit либо policy/write calls; platform reads сохраняют исходные project-permission data.
func TestMembershipLockHelpersPreserveProjectionAndOwnerScope(t *testing.T) {
	driverFailure := errors.New("synthetic private driver detail")
	for _, mode := range []struct {
		name    string
		project bool
	}{{"project", true}, {"platform", false}} {
		for _, test := range []struct {
			name        string
			cause, want error
			fill        bool
		}{
			{name: "success", fill: true},
			{name: "no-row", cause: pgx.ErrNoRows, want: errs.ErrNotFound},
			{name: "wrapped-no-row", cause: errors.Join(driverFailure, pgx.ErrNoRows), want: errs.ErrNotFound},
			{name: "scan-failure", cause: driverFailure, want: errs.ErrUnavailable},
			{name: "partial-failure", cause: driverFailure, want: errs.ErrUnavailable, fill: true},
		} {
			t.Run(mode.name+"/"+test.name, func(t *testing.T) {
				item := entity.Membership{ProjectRef: "caller-project", Permissions: []string{"preserved-permission"}}
				expected := item
				expected.Ref = "binding-public"
				expected.User.Ref = "subject-public"
				expected.User.DisplayName = "Synthetic subject"
				expected.User.EmailMasked = "synthetic-masked"
				expected.User.Active = true
				expected.Role = "MEMBER"
				expected.Active = false
				expected.Version = 14
				if mode.project {
					expected.Permissions = []string{"VIEW", "RUN"}
				}
				values := []any{"binding-internal", "subject-internal", "binding-public", "subject-public", "Synthetic subject", "synthetic-masked", true, "MEMBER"}
				if mode.project {
					values = append(values, []string{"VIEW", "RUN"})
				}
				values = append(values, false, int64(14))
				query := queryPlatformMembershipResolveForUpdate
				args := pgx.StrictNamedArgs{"organization_id": "org-owned", "membership_ref": "binding-public"}
				if mode.project {
					query = queryProjectMembershipResolveForUpdate
					args["project_id"] = "project-owned"
				}
				tx := &proofResolutionTx{t: t, ctx: t.Context(), query: query, args: []any{args}}
				tx.row = proofResolutionRow(func(dest ...any) error {
					if len(dest) != len(values) {
						t.Fatal("membership lock changed SQL projection arity")
					}
					if test.fill {
						for i, value := range values {
							reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
						}
					}
					return test.cause
				})
				var id, subject string
				var err error
				if mode.project {
					id, subject, err = lockProjectMembership(tx.ctx, tx, "org-owned", "project-owned", "binding-public", &item)
				} else {
					id, subject, err = lockPlatformMembership(tx.ctx, tx, "org-owned", "binding-public", &item)
				}
				if tx.queryCalls != 1 || tx.commitCalls != 0 {
					t.Fatal("membership lock changed read-only call boundary")
				}
				if test.want != nil {
					if !errors.Is(err, test.want) || id != "" || subject != "" || strings.Contains(err.Error(), "private") {
						t.Fatal("membership lock failure mapping or zero identity changed")
					}
				} else if err != nil || id != "binding-internal" || subject != "subject-internal" {
					t.Fatal("membership lock lost internal identities")
				}
				if test.fill && !reflect.DeepEqual(item, expected) {
					t.Fatal("membership lock mixed subject, binding, permission or version columns")
				}
				if !test.fill && !reflect.DeepEqual(item, entity.Membership{ProjectRef: "caller-project", Permissions: []string{"preserved-permission"}}) {
					t.Fatal("membership lock mutated untouched output")
				}
			})
		}
	}
}

// TestMembershipMutationLocksPreserveOrder checks invalid input before SQL, owner-scoped lock,
// version-before-self denial and delegation of platform owner policy to the caller.
// TestMembershipMutationLocksPreserveOrder проверяет invalid input до SQL, owner-scoped lock,
// отказ по версии до self и передачу platform owner policy вызывающему.
func TestMembershipMutationLocksPreserveOrder(t *testing.T) {
	for _, mode := range []struct {
		name    string
		project bool
	}{{"project", true}, {"platform", false}} {
		for _, test := range []struct {
			name     string
			ref      string
			expected *int64
			actor    string
			want     error
			queries  int
		}{
			{name: "missing version", ref: "binding", want: errs.ErrInvalid},
			{name: "missing ref", expected: new(int64), want: errs.ErrInvalid},
			{name: "stale before self", ref: "binding", expected: new(int64), actor: "subject", want: errs.ErrVersionMismatch, queries: 1},
			{name: "matching self", ref: "binding", expected: func() *int64 { v := int64(14); return &v }(), actor: "subject", queries: 1},
			{name: "matching other", ref: "binding", expected: func() *int64 { v := int64(14); return &v }(), actor: "other", queries: 1},
		} {
			t.Run(mode.name+"/"+test.name, func(t *testing.T) {
				query := queryPlatformMembershipResolveForUpdate
				args := pgx.StrictNamedArgs{"organization_id": "org", "membership_ref": test.ref}
				values := []any{"membership", "subject", "binding", "subject-public", "name", "masked", true, "OWNER"}
				if mode.project {
					query = queryProjectMembershipResolveForUpdate
					args["project_id"] = "project"
					values = append(values, []string{"VIEW"})
				}
				values = append(values, true, int64(14))
				tx := &proofResolutionTx{t: t, ctx: t.Context(), query: query, args: []any{args}}
				tx.row = proofResolutionRow(func(dest ...any) error {
					if len(dest) != len(values) {
						t.Fatal("unexpected membership projection")
					}
					for index, value := range values {
						reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
					}
					return nil
				})
				var item entity.Membership
				var err error
				if mode.project {
					_, err = lockMutableProjectMembership(tx.ctx, tx, scope{organizationID: "org", actorID: test.actor}, "project", test.ref, test.expected, &item)
				} else {
					_, _, err = lockVersionedPlatformMembership(tx.ctx, tx, "org", test.ref, test.expected, &item)
				}
				want := test.want
				if mode.project && test.name == "matching self" {
					want = errs.ErrForbidden
				}
				if !errors.Is(err, want) || tx.queryCalls != test.queries || tx.commitCalls != 0 {
					t.Fatal("membership validation order or boundary changed")
				}
			})
		}
	}
}
