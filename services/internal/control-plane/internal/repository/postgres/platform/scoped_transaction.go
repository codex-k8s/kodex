package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// beginScopedTransaction resolves the existing principal before opening a transaction with the
// caller's exact server-selected options. Scope errors pass through; BeginTx errors become
// Unavailable. No deadline, rollback, commit, lock, resource permission or action is selected here.
// Each native wrapper retains its fixed isolation/access mode and complete authority/lifecycle flow.
// beginScopedTransaction разрешает прежний principal до открытия transaction с точными выбранными
// сервером options caller. Scope errors возвращаются без изменения; BeginTx errors дают Unavailable.
// Deadline, rollback, commit, lock, resource permission и action здесь не выбираются. Каждый native
// wrapper сохраняет фиксированные isolation/access mode и полный authority/lifecycle flow.
func (r *Repository) beginScopedTransaction(ctx context.Context, p value.Principal, options pgx.TxOptions) (scope, pgx.Tx, error) {
	s, err := r.resolveScope(ctx, p)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := r.pool.BeginTx(ctx, options)
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	return s, tx, nil
}
