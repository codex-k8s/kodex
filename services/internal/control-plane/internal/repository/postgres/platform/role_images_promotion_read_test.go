package platform

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// promotionQueryFixture captures only the scoped query; all other transaction methods stay unused.
// promotionQueryFixture фиксирует только запрос в нужной области; прочие методы транзакции не используются.
type promotionQueryFixture struct {
	pgx.Tx
	ctx  context.Context
	sql  string
	args []any
	row  pgx.Row
}

// QueryRow records the request and returns the injected row without opening a database connection.
// QueryRow фиксирует запрос и возвращает переданную строку без открытия подключения к БД.
func (tx *promotionQueryFixture) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.ctx, tx.sql, tx.args = ctx, sql, args
	return tx.row
}

// promotionRowFixture provides ordered synthetic columns or an exact injected scan error.
// promotionRowFixture задаёт упорядоченные синтетические колонки либо точную переданную ошибку scan.
type promotionRowFixture struct{ err error }

// Scan fills all eight expected destinations or returns the original error without mapping it.
// Scan заполняет все восемь ожидаемых значений либо возвращает исходную ошибку без преобразования.
func (row promotionRowFixture) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	values := []string{"id", "ref", "provenance", "manifest", "receipt", "PROMOTING", "actor"}
	for i, value := range values {
		*dest[i].(*string) = value
	}
	*dest[7].(*time.Time) = time.Unix(123, 0).UTC()
	return nil
}

// TestReadLockedPromotionRequest verifies exact scope and artifact bindings, ordered row fields
// and unmodified no-row/driver errors; it does not exercise a live transaction.
// TestReadLockedPromotionRequest проверяет точную привязку scope и artifact, порядок колонок
// и исходные ошибки no-row/driver; живую транзакцию он не выполняет.
func TestReadLockedPromotionRequest(t *testing.T) {
	sentinel := errors.New("synthetic driver error")
	for _, test := range []struct {
		name    string
		failure error
	}{{"found", nil}, {"missing", pgx.ErrNoRows}, {"failure", sentinel}} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			tx := &promotionQueryFixture{row: promotionRowFixture{err: test.failure}}
			got, err := readLockedPromotionRequest(ctx, tx, scope{organizationID: "org"}, "artifact")
			if err != test.failure {
				t.Fatalf("error = %v, want %v", err, test.failure)
			}
			if tx.ctx != ctx || tx.sql != queryRoleImagesLockPromotionRequest || !reflect.DeepEqual(tx.args, []any{pgx.StrictNamedArgs{"organization_id": "org", "image_artifact_id": "artifact"}}) {
				t.Fatal("query scope or identity changed")
			}
			if test.failure != nil {
				if got != (lockedPromotionRequest{}) {
					t.Fatal("scan failure returned nonzero result")
				}
				return
			}
			expected := lockedPromotionRequest{ID: "id", Ref: "ref", ExpectedProvenanceSHA256: "provenance", ManifestDigest: "manifest", ReceiptSHA256: "receipt", State: "PROMOTING", RequestedBy: "actor", CreatedAt: time.Unix(123, 0).UTC()}
			if got != expected {
				t.Fatalf("result = %#v", got)
			}
		})
	}
}

// promotionArtifactFixture returns a minimal decodable artifact or a selected scan failure.
// promotionArtifactFixture возвращает минимальный декодируемый artifact либо выбранную ошибку scan.
type promotionArtifactFixture struct{ err error }

// Scan supplies the synthetic identity and empty specification to the existing scanner.
// Scan передаёт синтетическую identity и пустую specification существующему scanner.
func (row promotionArtifactFixture) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	*dest[0].(*string) = "artifact-id"
	*dest[9].(*[]byte) = []byte("{}")
	return nil
}

// promotionLockFixture records ordered queries and supplies independent artifact/request rows.
// promotionLockFixture фиксирует порядок запросов и передаёт отдельные artifact/request rows.
type promotionLockFixture struct {
	pgx.Tx
	queries                 []string
	args                    [][]any
	artifactErr, requestErr error
}

// QueryRow retains query order and returns only the expected synthetic row type.
// QueryRow сохраняет порядок запросов и возвращает только ожидаемый тип синтетической строки.
func (tx *promotionLockFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	tx.args = append(tx.args, args)
	if sql == queryRoleImagesLockArtifact {
		return promotionArtifactFixture{err: tx.artifactErr}
	}
	return promotionRowFixture{err: tx.requestErr}
}

// TestLockPromotionArtifactAndRequest checks lock order, scope and transition-specific error
// mapping without committing, acquiring an actual lock or running a real database.
// TestLockPromotionArtifactAndRequest проверяет порядок блокировок, scope и сопоставление ошибок
// конкретного перехода без commit, реальной блокировки и запуска БД.
func TestLockPromotionArtifactAndRequest(t *testing.T) {
	sentinel := errors.New("synthetic scan failure")
	for _, test := range []struct {
		name                          string
		artifactErr, requestErr, want error
		calls                         int
	}{
		{"found", nil, nil, nil, 2},
		{"artifact missing", pgx.ErrNoRows, nil, errs.ErrNotFound, 1},
		{"artifact error", sentinel, nil, errs.ErrUnavailable, 1},
		{"request missing", nil, pgx.ErrNoRows, errs.ErrForbidden, 2},
		{"request error", nil, sentinel, errs.ErrUnavailable, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &promotionLockFixture{artifactErr: test.artifactErr, requestErr: test.requestErr}
			artifact, request, err := lockPromotionArtifactAndRequest(t.Context(), tx, scope{organizationID: "org"}, "artifact-ref")
			if err != test.want || len(tx.queries) != test.calls {
				t.Fatalf("error = %v, queries = %d", err, len(tx.queries))
			}
			if tx.queries[0] != queryRoleImagesLockArtifact || !reflect.DeepEqual(tx.args[0], []any{"org", "artifact-ref"}) {
				t.Fatal("artifact lock scope changed")
			}
			if test.calls == 2 && (tx.queries[1] != queryRoleImagesLockPromotionRequest || !reflect.DeepEqual(tx.args[1], []any{pgx.StrictNamedArgs{"organization_id": "org", "image_artifact_id": "artifact-id"}})) {
				t.Fatal("request lock scope or order changed")
			}
			if err == nil && (artifact.ID != "artifact-id" || request.ID != "id") {
				t.Fatal("locked values changed")
			}
		})
	}
}
