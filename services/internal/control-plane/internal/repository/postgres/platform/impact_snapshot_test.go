package platform

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// impactSnapshotFixtureTx implements only rollback so an unexpected helper commit/query fails.
// It records whether cleanup runs before child cancellation and returns an independent error.
// impactSnapshotFixtureTx реализует только rollback, поэтому неожиданный helper commit/query
// завершится отказом. Записывает cleanup до child cancellation и возвращает отдельную ошибку.
type impactSnapshotFixtureTx struct {
	pgx.Tx
	trace        *[]string
	ctx          context.Context
	errAtCleanup error
	err          error
}

// Rollback captures the exact snapshot context without changing state or masking reader output.
// Rollback сохраняет точный snapshot context без изменения state либо сокрытия reader output.
func (tx *impactSnapshotFixtureTx) Rollback(ctx context.Context) error {
	*tx.trace = append(*tx.trace, "rollback")
	tx.ctx, tx.errAtCleanup = ctx, ctx.Err()
	return tx.err
}

// impactSnapshotFixtureResult models partial metadata returned with an error independently of
// native plan production, without creating a real plan, grant or verified principal.
// impactSnapshotFixtureResult задаёт partial metadata с ошибкой независимо от native plan
// production, не создавая настоящий plan, grant либо verified principal.
type impactSnapshotFixtureResult struct {
	ref   string
	items []string
}

// TestImpactSnapshotSearchGuard checks trimming and the exact UTF-8/rune/NUL boundary before begin.
// Accepted empty searches stay empty, and rejected inputs return only a zero result.
// TestImpactSnapshotSearchGuard проверяет trimming и точную UTF-8/rune/NUL границу до begin.
// Допустимый пустой поиск остаётся пустым; ошибочный input возвращает только zero result.
func TestImpactSnapshotSearchGuard(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, search, normalized string
		invalid                  bool
	}{
		{"empty", "", "", false},
		{"whitespace", " \n\t ", "", false},
		{"trimmed", "  exact search\n", "exact search", false},
		{"200_runes", strings.Repeat("я", 200), strings.Repeat("я", 200), false},
		{"200_emoji", strings.Repeat("😀", 200), strings.Repeat("😀", 200), false},
		{"trim_before_length", " \n" + strings.Repeat("я", 200) + "\t", strings.Repeat("я", 200), false},
		{"201_runes", strings.Repeat("я", 201), "", true},
		{"invalid_utf8", "bad\xff", "", true},
		{"nul", "exact\x00search", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var trace []string
			tx := &impactSnapshotFixtureTx{trace: &trace}
			calls := 0
			result, err := readImpactSnapshot(t.Context(), value.Principal{}, test.search,
				func(context.Context, value.Principal) (scope, pgx.Tx, error) {
					trace = append(trace, "begin")
					return scope{}, tx, nil
				},
				func(_ context.Context, _ pgx.Tx, _ scope, search string) (impactSnapshotFixtureResult, error) {
					calls++
					if search != test.normalized {
						t.Fatal("search normalization changed")
					}
					return impactSnapshotFixtureResult{ref: "visible"}, nil
				})
			if test.invalid {
				if err != errs.ErrInvalid || !reflect.DeepEqual(result, impactSnapshotFixtureResult{}) || len(trace) != 0 || calls != 0 {
					t.Fatal("invalid search reached scope/transaction read")
				}
			} else if err != nil || result.ref != "visible" || calls != 1 || !reflect.DeepEqual(trace, []string{"begin", "rollback"}) {
				t.Fatal("valid search result or cleanup changed")
			}
		})
	}
}

// TestImpactSnapshotResultAndCleanup pins principal/scope/context forwarding, begin-before-read,
// exact error identity and preservation of partial native results. Cleanup errors are ignored
// after the reader, rollback precedes child cancel, and the helper never commits or issues SQL.
// TestImpactSnapshotResultAndCleanup закрепляет principal/scope/context forwarding, begin до read,
// точную error identity и сохранение partial native results. Cleanup errors игнорируются после
// reader, rollback предшествует child cancel; helper не выполняет commit либо SQL.
func TestImpactSnapshotResultAndCleanup(t *testing.T) {
	t.Parallel()
	beginError := errors.New("synthetic snapshot begin failure")
	readError := errors.New("synthetic native read failure")
	rollbackError := errors.New("synthetic rollback failure")
	for _, test := range []struct {
		name                                 string
		beginError, readError, rollbackError error
	}{
		{"success", nil, nil, nil},
		{"begin_error", beginError, nil, nil},
		{"partial_error", nil, readError, nil},
		{"partial_unavailable", nil, errs.ErrUnavailable, nil},
		{"rollback_error", nil, nil, rollbackError},
		{"read_and_rollback_errors", nil, readError, rollbackError},
	} {
		t.Run(test.name, func(t *testing.T) {
			var trace []string
			tx := &impactSnapshotFixtureTx{trace: &trace, err: test.rollbackError}
			parent := t.Context()
			principal := value.Principal{ActorID: "synthetic-actor", AuthorityTenant: "synthetic-tenant", ProjectRef: "synthetic-project", Permission: "synthetic.exact"}
			current := scope{actorID: "resolved-actor", organizationID: "resolved-tenant", authorityProjectID: "resolved-project"}
			want := impactSnapshotFixtureResult{ref: "partial-plan", items: []string{"visible-item"}}
			var child context.Context
			result, err := readImpactSnapshot(parent, principal, " query ",
				func(ctx context.Context, p value.Principal) (scope, pgx.Tx, error) {
					trace = append(trace, "begin")
					child = ctx
					deadline, ok := ctx.Deadline()
					if ctx == parent || !ok || deadline.After(time.Now().Add(5*time.Second)) || !reflect.DeepEqual(p, principal) {
						t.Fatal("snapshot context budget or principal changed")
					}
					return current, tx, test.beginError
				},
				func(ctx context.Context, actual pgx.Tx, s scope, search string) (impactSnapshotFixtureResult, error) {
					trace = append(trace, "read")
					if ctx != child || actual != tx || !reflect.DeepEqual(s, current) || search != "query" {
						t.Fatal("native reader bindings changed")
					}
					return want, test.readError
				})
			if test.beginError != nil {
				if err != test.beginError || !reflect.DeepEqual(result, impactSnapshotFixtureResult{}) || !reflect.DeepEqual(trace, []string{"begin"}) {
					t.Fatal("begin error reached reader or rollback")
				}
			} else if err != test.readError || !reflect.DeepEqual(result, want) || !reflect.DeepEqual(trace, []string{"begin", "read", "rollback"}) || tx.ctx != child || tx.errAtCleanup != nil {
				t.Fatal("native partial result/error or cleanup order changed")
			}
			if child == nil || child.Err() != context.Canceled || parent.Err() != nil {
				t.Fatal("child cancellation or parent ownership changed")
			}
		})
	}
}

// TestImpactSnapshotParentDeadlineAndCancellation keeps an earlier parent deadline and preserves
// cancellation through rollback instead of inventing a fresh cleanup context or retry.
// TestImpactSnapshotParentDeadlineAndCancellation сохраняет более ранний parent deadline и
// cancellation при rollback без создания нового cleanup context либо retry.
func TestImpactSnapshotParentDeadlineAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelInRead := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelInRead), func(t *testing.T) {
			parent, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			deadline, _ := parent.Deadline()
			var trace []string
			tx := &impactSnapshotFixtureTx{trace: &trace}
			_, err := readImpactSnapshot(parent, value.Principal{}, "",
				func(ctx context.Context, _ value.Principal) (scope, pgx.Tx, error) {
					actual, ok := ctx.Deadline()
					if !ok || !actual.Equal(deadline) {
						t.Fatal("snapshot extended parent deadline")
					}
					return scope{}, tx, nil
				},
				func(context.Context, pgx.Tx, scope, string) (int, error) {
					if cancelInRead {
						cancel()
					}
					return 7, nil
				})
			if err != nil || !reflect.DeepEqual(trace, []string{"rollback"}) {
				t.Fatal("parent cancellation changed native result")
			}
			if cancelInRead && tx.errAtCleanup != context.Canceled {
				t.Fatal("request cancellation was replaced during cleanup")
			}
			if !cancelInRead && tx.errAtCleanup != nil {
				t.Fatal("child was cancelled before rollback")
			}
		})
	}
}

// TestImpactNativeSearchGuard rejects bad searches at both real reader entrypoints with no
// repository dependency, proving that the moved guard still precedes native owner lookups.
// TestImpactNativeSearchGuard отклоняет ошибочный поиск в обоих настоящих reader entrypoints без
// repository dependency, подтверждая сохранение guard до native owner lookups.
func TestImpactNativeSearchGuard(t *testing.T) {
	t.Parallel()
	var r *Repository
	for _, search := range []string{"bad\xff", strings.Repeat("я", 201), "a\x00b"} {
		revision, err := r.GetRevisionImpactPlan(t.Context(), value.Principal{}, "fixture", search, query.Page{})
		if err != errs.ErrInvalid || !reflect.DeepEqual(revision, entity.RevisionImpactPage{}) {
			t.Fatal("native revision search reached dependencies")
		}
		image, err := r.GetRoleImageImpactPlan(t.Context(), value.Principal{}, "fixture", search, query.Page{})
		if err != errs.ErrInvalid || !reflect.DeepEqual(image, entity.RoleImageImpactPage{}) {
			t.Fatal("native role-image search reached dependencies")
		}
	}
}
