package platform

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// beginImpactSnapshot resolves the existing principal before opening the original RepeatableRead
// transaction with no access-mode override, preserving locks needed by native impact guards.
// Scope errors pass through; BeginTx failure becomes Unavailable. No plan, owner or item access
// is authorized here, and no commit, retry or background operation is introduced.
// beginImpactSnapshot разрешает прежний principal до открытия исходной RepeatableRead transaction
// без изменения access mode, сохраняя locks native impact guards. Scope errors возвращаются
// без изменения; ошибка BeginTx даёт Unavailable. Plan/owner/item access здесь не разрешается;
// commit, retry и фоновые операции не добавляются.
func (r *Repository) beginImpactSnapshot(ctx context.Context, p value.Principal) (scope, pgx.Tx, error) {
	return r.beginScopedTransaction(ctx, p, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
}

// readImpactSnapshot trims/validates the original 200-rune search before any scope or transaction
// read, then installs the existing five-second child context and rollback-before-cancel cleanup.
// Setup failures return a zero result; the native reader's result/error, including partial data,
// passes through unchanged. The reader retains all plan ownership, immutable commitment, item
// eligibility, cursor, state and commit decisions. Rollback still uses the request-derived context
// and ignores its error; no independent cleanup budget or generic protected action is added.
// readImpactSnapshot нормализует и проверяет исходный поиск до200 рун до scope/transaction reads,
// затем устанавливает прежний пятисекундный child context и cleanup rollback до cancel. Ошибки
// подготовки возвращают zero result; результат/error native reader, включая partial data,
// передаётся без изменения. Plan ownership, immutable commitment, item eligibility, cursor,
// state и commit остаются у reader. Rollback по-прежнему использует request-derived context
// и игнорирует ошибку; independent cleanup budget и generic protected action не добавляются.
func readImpactSnapshot[Result any](ctx context.Context, p value.Principal, search string,
	begin func(context.Context, value.Principal) (scope, pgx.Tx, error),
	read func(context.Context, pgx.Tx, scope, string) (Result, error),
) (Result, error) {
	var result Result
	search = strings.TrimSpace(search)
	if !utf8.ValidString(search) || utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s, tx, err := begin(ctx, p)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return read(ctx, tx, s, search)
}
