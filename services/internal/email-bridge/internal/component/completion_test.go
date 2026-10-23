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

// cancellingProvider cancels the caller during a synthetic mutation and counts
// attempts, allowing completion-context tests without any external mail transport.
// cancellingProvider отменяет вызывающий контекст во время искусственного изменения
// и считает попытки, позволяя проверить завершение без внешнего почтового транспорта.
type cancellingProvider struct {
	mail.Provider
	cancel context.CancelFunc
	calls  int
}

// Send cancels the request but returns synthetic SMTP acceptance, modelling an
// accepted external effect whose receipt still needs independent completion.
// Send отменяет запрос, но возвращает искусственное принятие SMTP, моделируя
// принятый внешний эффект, квитанцию которого ещё нужно независимо завершить.
func (p *cancellingProvider) Send(ctx context.Context, _ api.Mailbox, _ api.Command, _ string) (string, error) {
	p.calls++
	p.cancel()
	return "accepted", nil
}

// Apply cancels the request and returns partial IMAP coordinates with its error;
// tests require that uncertainty and the source identity survive receipt completion.
// Apply отменяет запрос и возвращает частичные координаты IMAP с ошибкой;
// тесты требуют сохранить неопределённость и источник при завершении квитанции.
func (p *cancellingProvider) Apply(ctx context.Context, _ api.Mailbox, _ api.Command, _ string) (api.Result, error) {
	p.calls++
	p.cancel()
	return api.Result{Status: "unknown", Uid: "42", UidValidity: 7, Folder: "INBOX", ContentDigest: strings.Repeat("a", 64)}, ctx.Err()
}

// completionStore wraps a fixture repository to inspect completion deadlines
// and optionally reject persistence after the provider has already been invoked.
// completionStore оборачивает тестовый репозиторий для проверки срока завершения
// и управляемого отказа сохранения после уже выполненного вызова провайдера.
type completionStore struct {
	port.Repository
	port.ReportRepository
	t    *testing.T
	fail bool
}

// CompleteEffect requires a live independent context bounded by three seconds;
// it either injects Unavailable or delegates the unchanged completion to the store.
// CompleteEffect требует действующий независимый контекст с бюджетом до трёх секунд;
// затем возвращает искусственный Unavailable либо передаёт завершение хранилищу.
func (s *completionStore) CompleteEffect(ctx context.Context, scope port.Scope, record port.Record, status string, source port.ReportSource) (port.Record, error) {
	s.t.Helper()
	deadline, ok := ctx.Deadline()
	if ctx.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 3*time.Second {
		s.t.Fatal("receipt completion must have an independent bounded context")
	}
	if s.fail {
		return port.Record{}, errs.Unavailable
	}
	return s.ReportRepository.CompleteEffect(ctx, scope, record, status, source)
}

// TestReceiptCompletionAfterCancellation runs the cancellation/replay matrix
// against memory only; it does not establish persistent receipt recovery.
// TestReceiptCompletionAfterCancellation выполняет матрицу отмены/повтора только
// в памяти; устойчивое восстановление квитанций этим тестом не доказывается.
func TestReceiptCompletionAfterCancellation(t *testing.T) {
	testReceiptCompletion(t, nil)
}

// TestMutationRequiresCompletionLifecycle rejects a missing or cancelled cleanup
// base before reserving a receipt or calling the synthetic provider.
// TestMutationRequiresCompletionLifecycle отклоняет отсутствующую или отменённую
// основу завершения до резервирования квитанции и вызова тестового провайдера.
func TestMutationRequiresCompletionLifecycle(t *testing.T) {
	for _, missing := range []bool{false, true} {
		base, cancel := context.WithCancel(t.Context())
		cancel()
		if missing {
			base = nil
		}
		store := &memory{rows: map[string]port.Record{}}
		provider := &cancellingProvider{cancel: func() {}}
		service := &mail.Service{Reports: store, Ledger: store, CompletionBase: base, Config: configuration("implicit"), Authority: &authorityFixture{}, Effects: effectFixture{}, Provider: provider, Receipts: store}
		_, err := service.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", send(api.OperationSend, "missing-cleanup"))
		if !errors.Is(err, errs.Unavailable) || provider.calls != 0 || len(store.rows) != 0 {
			t.Fatal("mutation started without a completion lifecycle")
		}
	}
}

// TestPostgresReceiptCompletionAfterCancellation runs the same lifecycle checks
// with a disposable database, skipping when its explicit fixture is unavailable.
// TestPostgresReceiptCompletionAfterCancellation проверяет тот же жизненный цикл
// с временной БД и пропускается при отсутствии явно настроенного стенда.
func TestPostgresReceiptCompletionAfterCancellation(t *testing.T) {
	testReceiptCompletion(t, postgresFixture(t))
}

// testReceiptCompletion covers accepted SMTP, partial IMAP and failed completion;
// it verifies replay without a second provider call and locking of unknown IMAP sources.
// testReceiptCompletion проверяет принятое SMTP, частичное IMAP и сбой завершения;
// повтор не должен снова вызвать провайдера или снять блокировку неопределённого источника.
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
			store := &completionStore{Repository: base, ReportRepository: base.(port.ReportRepository), t: t, fail: scenario == "store-failure"}
			provider := &cancellingProvider{cancel: cancel}
			service := &mail.Service{Reports: store, Ledger: base.(port.ReconciliationRepository), CompletionBase: t.Context(), Config: receiptConfiguration(), Authority: &authorityFixture{}, Effects: effectFixture{}, Provider: provider, Receipts: store}
			command := send(api.OperationSend, "completion-key")
			want := "accepted"
			if scenario == "imap-partial" {
				command = api.Command{Operation: api.OperationMarkRead, MailboxId: "mailbox", EffectKey: "completion-key", Uid: "1", UidValidity: 7}
				want = "unknown"
			} else if store.fail {
				want = "unknown"
			}
			command.EffectKey += "-" + scenario
			result, err := service.Execute(executionContext(ctx), httptransport.CallerSPIFFE, "fixture-token", command)
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
			replayed, err := service.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", command)
			if err != nil || replayed.Status != want || provider.calls != 1 {
				t.Fatal("receipt replay repeated the provider effect")
			}
			if scenario == "imap-partial" {
				command.EffectKey = "another-key"
				if _, err := service.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", command); !errors.Is(err, errs.Conflict) || provider.calls != 1 {
					t.Fatal("unknown source lock was bypassed")
				}
			}
		})
	}
}
