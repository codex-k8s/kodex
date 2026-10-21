package component

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	httptransport "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http"
)

// effectFixture simulates owner receipt reporting with an optional inspection
// hook; generated references and versions are fixture data, not control-plane proof.
// effectFixture имитирует отчёт владельцу квитанции с необязательной проверкой;
// созданные ссылки и версии — данные стенда, а не доказательство работы управляющего сервиса.
type effectFixture struct {
	report func(context.Context, port.Report) (port.OwnerReceipt, error)
}

// Report runs the optional hook and supplies a synthetic owner reference,
// invocation and version, distinguishing initial UNKNOWN from a terminal outcome.
// Report вызывает необязательную проверку и задаёт тестовую ссылку владельца,
// вызов и версию, различая начальный UNKNOWN и окончательный исход.
func (f effectFixture) Report(ctx context.Context, r port.Report) (port.OwnerReceipt, error) {
	result := r.Receipt
	if f.report != nil {
		var err error
		result, err = f.report(ctx, r)
		if err != nil {
			return result, err
		}
	}
	result.Ref, result.Version = "receipt_"+r.Receipt.ExternalRef, 1
	if result.Outcome != port.Unknown {
		result.Version = 2
	}
	if result.Invocation == "" {
		result.Invocation = "inv_fixture01"
	}
	return result, nil
}

// Reconcile always denies fixture reconciliation; tests needing an actual owner
// decision must install a different client rather than implicitly releasing a lock.
// Reconcile всегда запрещает тестовую сверку; для решения владельца тест обязан
// установить другой клиент вместо неявного снятия блокировки.
func (effectFixture) Reconcile(context.Context, port.OwnerReceipt, string) (port.Decision, error) {
	return port.Decision{}, errs.Denied
}

// TestDurableUnknownBeforeCPAndProvider checks receipt/report ordering against
// the memory fixture; the separately named PostgreSQL case establishes durability.
// TestDurableUnknownBeforeCPAndProvider проверяет порядок квитанции/отчёта в памяти;
// устойчивость проверяется отдельно названным случаем с PostgreSQL.
func TestDurableUnknownBeforeCPAndProvider(t *testing.T) {
	testDurableUnknown(t, nil)
}

// failingLedger injects failure when binding a local receipt to its owner,
// exercising the barrier that must precede credential access and provider effects.
// failingLedger создаёт отказ при привязке локальной квитанции к владельцу,
// проверяя барьер перед доступом к реквизитам и эффектами провайдера.
type failingLedger struct{ port.ReconciliationRepository }

// Remember always returns Unavailable without storing a binding; the fixture
// must never be interpreted as a successful durable journal write.
// Remember всегда возвращает Unavailable без сохранения привязки; эта заглушка
// не должна трактоваться как успешная запись устойчивого журнала.
func (failingLedger) Remember(context.Context, port.Scope, port.Record, port.OwnerReceipt, time.Time) error {
	return errs.Unavailable
}

// TestOwnerBindingMustPersistBeforeProvider requires a journal-write failure
// to stop mutation before the secret reader can reach any transport credentials.
// TestOwnerBindingMustPersistBeforeProvider требует остановить изменение при сбое
// записи журнала до чтения любых транспортных реквизитов.
func TestOwnerBindingMustPersistBeforeProvider(t *testing.T) {
	f := newFixture(t, "implicit")
	s, sec, _ := service(t, f, "implicit", nil)
	s.Ledger = failingLedger{}
	if _, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture", send(api.OperationSend, "journal-failure")); !errors.Is(err, errs.Unavailable) || sec.reads.Load() != 0 {
		t.Fatal("provider started without durable CP identity")
	}
}

// TestEmptyMailboxConfigurationFailsClosed requires both readiness and send
// requests for an unconfigured mailbox to return NotFound without reading secrets.
// TestEmptyMailboxConfigurationFailsClosed требует NotFound без чтения секретов
// для проверки готовности и отправки через ненастроенный ящик.
func TestEmptyMailboxConfigurationFailsClosed(t *testing.T) {
	f := newFixture(t, "implicit")
	s, sec, _ := service(t, f, "implicit", nil)
	s.Config.Mailboxes = nil
	for _, op := range []api.Operation{api.OperationHealth, api.OperationSend} {
		if _, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture", send(op, "not-configured")); !errors.Is(err, errs.NotFound) || sec.reads.Load() != 0 {
			t.Fatal("unconfigured mailbox reached provider")
		}
	}
}

// TestPostgresDurableUnknownBeforeCPAndProvider repeats the report-order matrix
// using the explicitly configured disposable database; an absent fixture skips it.
// TestPostgresDurableUnknownBeforeCPAndProvider повторяет матрицу порядка отчётов
// в явно настроенной временной БД; отсутствие стенда приводит к пропуску.
func TestPostgresDurableUnknownBeforeCPAndProvider(t *testing.T) {
	testDurableUnknown(t, postgresFixture(t))
}

// testDurableUnknown verifies UNKNOWN persistence and owner acknowledgement before
// credentials, plus terminal-report recovery without repeating the SMTP effect.
// testDurableUnknown проверяет сохранение UNKNOWN и подтверждение владельца до
// реквизитов, затем восстановление итогового отчёта без повторного SMTP-эффекта.
func testDurableUnknown(t *testing.T, store port.Repository) {
	t.Helper()
	for _, fail := range []string{"none", "unknown-report", "terminal-report", "missing-client"} {
		t.Run(fail, func(t *testing.T) {
			f := newFixture(t, "implicit")
			s, sec, _ := service(t, f, "implicit", store)
			if store != nil {
				s.Config = receiptConfiguration()
			}
			command := send(api.OperationSend, "report-order-"+fail)
			scope := port.Scope{Tenant: "tenant", Mailbox: "mailbox"}
			calls := 0
			var digest string
			s.Effects = effectFixture{report: func(ctx context.Context, request port.Report) (port.OwnerReceipt, error) {
				calls++
				r, err := s.Receipts.Get(ctx, scope, request.Receipt.ExternalRef, "")
				if err != nil || r.ExternalDigest(scope) != request.Receipt.ExternalDigest {
					t.Fatal("report preceded durable receipt")
				}
				if calls == 1 {
					digest = request.Receipt.ExternalDigest
					if r.Status != "unknown" || request.Receipt.Outcome != port.Unknown || sec.reads.Load() != 0 {
						t.Fatal("provider preceded durable UNKNOWN acknowledgement")
					}
				} else if digest != request.Receipt.ExternalDigest {
					t.Fatal("receipt identity changed with outcome")
				}
				if fail == "unknown-report" || fail == "terminal-report" && request.Receipt.Outcome != port.Unknown {
					return port.OwnerReceipt{}, errs.Unavailable
				}
				return request.Receipt, nil
			}}
			if fail == "missing-client" {
				s.Effects = nil
			}
			result, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", command)
			if fail == "none" && (err != nil || result.Status != "accepted" || calls != 2) {
				t.Fatalf("report lifecycle failed: %v", err)
			}
			if fail != "none" && !errors.Is(err, errs.Unavailable) {
				t.Fatalf("expected closed refusal, got %v", err)
			}
			if (fail == "unknown-report" || fail == "missing-client") && sec.reads.Load() != 0 {
				t.Fatal("failed CP allowed credential access")
			}
			before := sec.reads.Load()
			if fail == "terminal-report" {
				// Восстановление CP допубликовывает receipt, но не повторяет SMTP.
				s.Effects = effectFixture{}
				replay, err := s.Execute(executionContext(t.Context()), httptransport.CallerSPIFFE, "fixture-token", command)
				if err != nil || replay.Status != "accepted" || sec.reads.Load() != before {
					t.Fatal("receipt recovery repeated provider effect")
				}
			}
		})
	}
}
