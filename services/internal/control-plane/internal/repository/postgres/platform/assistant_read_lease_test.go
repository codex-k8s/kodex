package platform

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestAssistantReadLeasePreservesBindingsAndErrorPolicy checks ten synthetic outcomes with exact
// context, SQL, organization/ref/generation and a literal fence-hash golden. It verifies persisted
// actor/project destinations, unchanged other scope fields, partial scan semantics, no commit,
// NoRows/wrapped behavior and retry only for 40001 without driver detail. This is a helper
// boundary test, not a live lease or PostgreSQL lock proof.
// TestAssistantReadLeasePreservesBindingsAndErrorPolicy проверяет десять синтетических исходов с
// точными context, SQL, организацией, ссылкой, поколением и буквальным ожидаемым хешем fence.
// Проверяет destinations субъекта и проекта, остальные поля области, частичный Scan, отсутствие
// commit, NoRows и wrapped, повтор только для 40001 без деталей драйвера. Это проверка границы
// helper, не доказательство живой lease или PostgreSQL locks.
func TestAssistantReadLeasePreservesBindingsAndErrorPolicy(t *testing.T) {
	for _, test := range []struct {
		name      string
		project   string
		scanError error
		written   int
		wantError error
		wantRetry bool
	}{
		{"system", "", nil, 4, nil, false},
		{"project", "project-ref", nil, 4, nil, false},
		{"missing", "", pgx.ErrNoRows, 0, errs.ErrNotFound, false},
		{"wrapped missing", "", fmt.Errorf("fixture: %w", pgx.ErrNoRows), 0, errs.ErrNotFound, false},
		{"serialization", "", &pgconn.PgError{Code: "40001", Message: "fixture driver detail"}, 0, errs.ErrUnavailable, true},
		{"wrapped serialization", "", fmt.Errorf("fixture: %w", &pgconn.PgError{Code: "40001", Message: "fixture driver detail"}), 0, errs.ErrUnavailable, true},
		{"deadlock", "", &pgconn.PgError{Code: "40P01", Message: "fixture driver detail"}, 0, errs.ErrUnavailable, false},
		{"other error", "", errors.New("fixture driver detail"), 0, errs.ErrUnavailable, false},
		{"commit rollback", "", pgx.ErrTxCommitRollback, 0, errs.ErrUnavailable, false},
		{"partial scan", "project-ref", errors.New("fixture driver detail"), 2, errs.ErrUnavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			current := scope{organizationID: "organization-id", organizationRef: "organization-ref", actorID: "worker-id", actorRef: "worker-ref", actorName: "worker", role: "MEMBER", authorityProjectID: "original-project-id"}
			want := current
			values := []string{"owner-ref", "owner-id", "", test.project}
			if test.project != "" {
				values[2] = "project-id"
			}
			if test.written > 0 {
				want.actorRef, want.actorID = values[0], values[1]
			}
			if test.written == 4 {
				want.authorityProjectID = values[2]
			}
			tx := &proofResolutionTx{t: t, ctx: ctx, query: queryAssistantSearchResolveLease, args: []any{pgx.StrictNamedArgs{
				"organization_id": "organization-id", "lease_ref": "lease-ref", "fence_digest": "6c64faf4a4e3f32e32a8305465ae677af89256062a38c1cdc44d6dd91a6dfc09", "generation": int64(7),
			}}}
			tx.row = proofResolutionRow(func(destinations ...any) error {
				if len(destinations) != 4 || destinations[0] != &current.actorRef || destinations[1] != &current.actorID || destinations[2] != &current.authorityProjectID {
					t.Fatal("assistant read changed its actor or project scan destinations")
				}
				for index := 0; index < test.written; index++ {
					*destinations[index].(*string) = values[index]
				}
				return test.scanError
			})
			project, err := (&Repository{}).resolveAssistantReadLease(ctx, tx, &current, "lease-ref", "assistant-read-fence-fixture", 7)
			if !errors.Is(err, test.wantError) || errors.Is(err, errSerializableTransactionRetry) != test.wantRetry {
				t.Fatal("assistant read changed missing, retry or unavailable error mapping")
			}
			wantProject := test.project
			if test.wantError != nil {
				wantProject = ""
			}
			if project != wantProject || !reflect.DeepEqual(current, want) || tx.queryCalls != 1 || tx.commitCalls != 0 {
				t.Fatal("assistant read changed scope fields, project result or transaction ownership")
			}
			if err != nil && strings.Contains(err.Error(), "fixture driver detail") {
				t.Fatal("assistant read exposed a driver diagnostic")
			}
		})
	}
}
