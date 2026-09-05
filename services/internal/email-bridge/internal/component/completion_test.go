package component

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

type cancellingProvider struct {
	mail.Provider
	cancel context.CancelFunc
	calls  int
}

func (p *cancellingProvider) Send(ctx context.Context, _ api.Mailbox, _ api.Command, _ string) (string, error) {
	p.calls++
	p.cancel()
	return "accepted", nil
}

func (p *cancellingProvider) Apply(ctx context.Context, _ api.Mailbox, _ api.Command, _ string) (api.Result, error) {
	p.calls++
	p.cancel()
	return api.Result{Status: "unknown", Uid: "42", UidValidity: 7, Folder: "INBOX", ContentDigest: strings.Repeat("a", 64)}, ctx.Err()
}

type completionStore struct {
	port.Repository
	t    *testing.T
	fail bool
}

func (s *completionStore) Complete(ctx context.Context, scope port.Scope, record port.Record, status string) error {
	s.t.Helper()
	deadline, ok := ctx.Deadline()
	if ctx.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 3*time.Second {
		s.t.Fatal("receipt completion must have an independent bounded context")
	}
	if s.fail {
		return errs.Unavailable
	}
	return s.Repository.Complete(ctx, scope, record, status)
}

func TestReceiptCompletionAfterCancellation(t *testing.T) {
	testReceiptCompletion(t, nil)
}

func TestMutationRequiresCompletionLifecycle(t *testing.T) {
	for _, missing := range []bool{false, true} {
		base, cancel := context.WithCancel(t.Context())
		cancel()
		if missing {
			base = nil
		}
		store := &memory{rows: map[string]port.Record{}}
		provider := &cancellingProvider{cancel: func() {}}
		service := &mail.Service{CompletionBase: base, Config: configuration("implicit"), Authority: &authorityFixture{}, Effects: effectFixture{}, Provider: provider, Receipts: store}
		_, err := service.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", send(api.OperationSend, "missing-cleanup"))
		if !errors.Is(err, errs.Unavailable) || provider.calls != 0 || len(store.rows) != 0 {
			t.Fatal("mutation started without a completion lifecycle")
		}
	}
}

func TestPostgresReceiptCompletionAfterCancellation(t *testing.T) {
	testReceiptCompletion(t, postgresFixture(t))
}

func testReceiptCompletion(t *testing.T, durable port.Repository) {
	t.Helper()
	for _, scenario := range []string{"smtp-accepted", "imap-partial", "store-failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := durable
			if base == nil {
				base = &memory{rows: map[string]port.Record{}}
			}
			store := &completionStore{Repository: base, t: t, fail: scenario == "store-failure"}
			provider := &cancellingProvider{cancel: cancel}
			service := &mail.Service{CompletionBase: t.Context(), Config: receiptConfiguration(), Authority: &authorityFixture{}, Effects: effectFixture{}, Provider: provider, Receipts: store}
			command := send(api.OperationSend, "completion-key")
			want := "accepted"
			if scenario == "imap-partial" {
				command = api.Command{Operation: api.OperationMarkRead, MailboxId: "mailbox", EffectKey: "completion-key", Uid: "1", UidValidity: 7}
				want = "unknown"
			} else if store.fail {
				want = "unknown"
			}
			command.EffectKey += "-" + scenario
			result, err := service.Execute(ctx, httptransport.CallerSPIFFE, "fixture-token", command)
			if err != nil || result.Status != want || ctx.Err() == nil {
				t.Fatal("unexpected completion outcome")
			}
			record, err := store.Get(t.Context(), port.Scope{Tenant: "tenant", Mailbox: "mailbox"}, result.MessageId, "")
			if err != nil || record.Status != want {
				t.Fatal("durable receipt outcome lost")
			}
			if scenario == "imap-partial" && (record.UID != "42" || record.UIDValidity != 7 || record.Folder != "INBOX" || record.ContentDigest != strings.Repeat("a", 64)) {
				t.Fatal("partial provider coordinates lost")
			}
			replayed, err := service.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", command)
			if err != nil || replayed.Status != want || provider.calls != 1 {
				t.Fatal("receipt replay repeated the provider effect")
			}
			if scenario == "imap-partial" {
				command.EffectKey = "another-key"
				if _, err := service.Execute(t.Context(), httptransport.CallerSPIFFE, "fixture-token", command); !errors.Is(err, errs.Conflict) || provider.calls != 1 {
					t.Fatal("unknown source lock was bypassed")
				}
			}
		})
	}
}
