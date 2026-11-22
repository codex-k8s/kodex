package mail

import (
	"context"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
)

// mailboxReadCreditLive requires the existing bounded operation context to remain live at result handoff. Timer delivery alone is insufficient when a successful protocol read finishes after its deadline.
// mailboxReadCreditLive требует живой существующий bounded operation context при передаче результата. Доставки timer недостаточно, если успешный protocol read завершается после deadline.
func mailboxReadCreditLive(ctx context.Context) bool {
	deadline, bounded := ctx.Deadline()
	return bounded && ctx.Err() == nil && time.Now().Before(deadline)
}

// readWithCredit preserves native read errors while withholding all mailbox data after the original authorization/operation deadline. Mutation completion and historical receipt maintenance do not use this path.
// readWithCredit сохраняет native read errors, но не выдаёт mailbox data после исходного authorization/operation deadline. Mutation completion и historical receipt maintenance этот путь не используют.
func (s *Service) readWithCredit(ctx context.Context, mailbox api.Mailbox, command api.Command) (api.Result, error) {
	result, err := s.Provider.Read(ctx, mailbox, command)
	if !mailboxReadCreditLive(ctx) {
		return api.Result{}, errs.Unavailable
	}
	return result, err
}
