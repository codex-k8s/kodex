package component

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

// deferredNativeMailboxRead retains the real TLS IMAP adapter and delays only the successful result handoff after native I/O has completed; it cannot manufacture mailbox data or a successful protocol result.
// deferredNativeMailboxRead сохраняет настоящий TLS IMAP adapter и задерживает только передачу успешного результата после native I/O; не создаёт mailbox data либо успешный protocol result.
type deferredNativeMailboxRead struct {
	mail.Provider
	completed atomic.Int32
}

// Read confirms a nonempty native result before waiting for the original credit context to expire, modelling post-I/O scheduling delay without repeating the mailbox request.
// Read проверяет непустой native result перед ожиданием expiry исходного credit context, моделируя задержку после I/O без повтора mailbox request.
func (provider *deferredNativeMailboxRead) Read(ctx context.Context, mailbox api.Mailbox, command api.Command) (api.Result, error) {
	result, err := provider.Provider.Read(ctx, mailbox, command)
	if err != nil {
		return result, err
	}
	if len(result.Mailboxes) == 0 && len(result.Headers) == 0 {
		return api.Result{}, errs.Invalid
	}
	provider.completed.Add(1)
	<-ctx.Done()
	return result, nil
}

// TestPersonalMailboxReadHandoffDeadline proves that successful real IMAP reads cannot disclose buffered data after the original authority deadline, independently of socket I/O cancellation. No SMTP effect, external account or PostgreSQL fixture is used.
// TestPersonalMailboxReadHandoffDeadline доказывает, что успешный настоящий IMAP read не выдаёт buffered data после исходного authority deadline, независимо от отмены socket I/O. SMTP effect, внешний аккаунт и PostgreSQL fixture не используются.
func TestPersonalMailboxReadHandoffDeadline(t *testing.T) {
	for _, operation := range []api.Operation{api.OperationMailboxes, api.OperationList} {
		t.Run(string(operation), func(t *testing.T) {
			fixture := newFixture(t, "implicit")
			_, address := imapFixture(t, fixture, "implicit")
			bridge, secrets, authority := service(t, fixture, "implicit", nil)
			mailbox := &bridge.Config.Mailboxes[0]
			endpoint := mailbox.Smtp
			endpoint.Port = 993
			mailbox.Imap, mailbox.Pop, mailbox.ReceiveProtocol = &endpoint, nil, "imap"
			native := &mailtransport.Provider{Secrets: secrets, Dialer: imapDialFixture{dialFixture{fixture.smtp, fixture.pop}, address}}
			provider := &deferredNativeMailboxRead{Provider: native}
			bridge.Provider = provider
			authority.mutate = func(decision *api.AuthorizationDecision) {
				decision.ExpiresAt = time.Now().Unix() + 2
			}
			ctx := executionContext(t.Context())
			result, err := bridge.Execute(ctx, "caller", "token", api.Command{Operation: operation, MailboxId: mailbox.Id})
			if provider.completed.Load() != 1 || ctx.Err() != nil {
				t.Fatal("read deadline fixture did not complete exactly one native TLS read with a live caller")
			}
			if !errors.Is(err, errs.Unavailable) || !reflect.DeepEqual(result, api.Result{}) {
				t.Fatal("expired mailbox credit disclosed a successful buffered native read")
			}
		})
	}
}
