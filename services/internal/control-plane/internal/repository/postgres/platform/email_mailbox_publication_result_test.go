package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestEmailMailboxPublicationWriteRequiresOneRow checks the exact stage/applied/callback result
// contract: one row succeeds, zero/multiple rows conflict, and execution failure wins even if the
// command tag says one row. Missing-row errors are unavailable too; driver details stay hidden.
// TestEmailMailboxPublicationWriteRequiresOneRow проверяет точный контракт результата stage,
// applied и callback: одна строка успешна, ноль или несколько дают Conflict, ошибка исполнения
// имеет приоритет даже при одной строке в tag. ErrNoRows также даёт Unavailable; детали скрываются.
func TestEmailMailboxPublicationWriteRequiresOneRow(t *testing.T) {
	driverFailure := errors.New("synthetic private driver detail")
	for _, test := range []struct {
		name, tag   string
		cause, want error
	}{
		{name: "updated", tag: "UPDATE 1"},
		{name: "missing-update", tag: "UPDATE 0", want: errs.ErrConflict},
		{name: "multiple-updates", tag: "UPDATE 2", want: errs.ErrConflict},
		{name: "execution-failure", cause: driverFailure, want: errs.ErrUnavailable},
		{name: "failure-before-count", tag: "UPDATE 1", cause: driverFailure, want: errs.ErrUnavailable},
		{name: "missing-row-error", cause: pgx.ErrNoRows, want: errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := emailMailboxPublicationWriteResult(pgconn.NewCommandTag(test.tag), test.cause)
			if !errors.Is(err, test.want) {
				t.Fatal("mailbox publication changed write error priority or count rule")
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("mailbox publication exposed driver detail")
			}
		})
	}
}
