package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// emailReceiptCommitDelay identifies only a successful native COMMIT after the exact receipt decision read; principal resolution commits cannot supply the test's expiry evidence.
// emailReceiptCommitDelay выделяет только успешный native COMMIT после exact receipt decision read; commits разрешения principal не становятся доказательством expiry.
type emailReceiptCommitDelay struct{}

// emailReceiptDeadlineTrace delegates SQL-boundary enforcement and delays one real COMMIT acknowledgement until the original finite ACL expires, without fabricating a query or transaction result.
// emailReceiptDeadlineTrace сохраняет SQL-boundary enforcement и задерживает один настоящий COMMIT ACK до expiry исходного finite ACL, не подменяя query либо результат transaction.
type emailReceiptDeadlineTrace struct {
	base     pgx.QueryTracer
	mode     string
	armed    atomic.Bool
	read     atomic.Bool
	hits     atomic.Int32
	deadline atomic.Pointer[time.Time]
}

// TraceQueryStart arms the delayed acknowledgement only inside the receipt/decision transaction selected by the fixture.
// TraceQueryStart допускает задержку ACK только в выбранной fixture транзакции receipt/decision.
func (trace *emailReceiptDeadlineTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.base.TraceQueryStart(ctx, connection, data)
	if trace.armed.Load() && data.SQL == queryEmailDecisionGet {
		trace.read.Store(true)
	}
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") && trace.read.Load() && trace.armed.CompareAndSwap(true, false) {
		return context.WithValue(ctx, emailReceiptCommitDelay{}, trace.deadline.Load())
	}
	return ctx
}

// TraceQueryEnd waits only after successful database COMMIT; cancellation remains a separate failure and does not prove permission expiry.
// TraceQueryEnd ждёт только после успешного database COMMIT; cancellation остаётся отдельным отказом и не доказывает expiry разрешения.
func (trace *emailReceiptDeadlineTrace) TraceQueryEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.base.TraceQueryEnd(ctx, connection, data)
	until, _ := ctx.Value(emailReceiptCommitDelay{}).(*time.Time)
	if until == nil || data.Err != nil {
		return
	}
	trace.hits.Add(1)
	timer := time.NewTimer(time.Until(*until) + 20*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// TestEmailReceiptDisclosureDeadlineComponent reuses the real synthetic receipt/reconciliation harness and access commands to reject metadata and usable grants after a successful delayed COMMIT ACK. It performs no mailbox I/O or live authentication.
// TestEmailReceiptDisclosureDeadlineComponent переиспользует настоящую synthetic receipt/reconciliation оснастку и access commands для отказа metadata и usable grants после успешного задержанного COMMIT ACK. Mailbox I/O и live authentication не выполняются.
func TestEmailReceiptDisclosureDeadlineComponent(t *testing.T) {
	for _, mode := range []string{"metadata", "reconciliation"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			configuration, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
			if err != nil {
				t.Fatal("parse isolated email receipt database", err)
			}
			trace := &emailReceiptDeadlineTrace{base: componentQueryRejectionTracer{t: t}, mode: mode}
			configuration.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal("open isolated email receipt database", err)
			}
			defer pool.Close()
			repository := newSyntheticRuntimeComponentRepository(t, ctx, pool)
			testEmailReceiptReconciliation(t, ctx, repository, pool, trace)

		})
	}
}

// personalEmailReceiptDeadlineFixture changes only the reader's exact integration binding, crosses real ACL expiry at two native COMMIT handoffs, restores access and proves fresh readback retains the original receipt and decision. Durable facts are neither rewritten nor rolled back by a disclosure failure.
// personalEmailReceiptDeadlineFixture меняет только exact integration binding читателя, пересекает настоящий ACL expiry при двух native COMMIT handoffs, восстанавливает доступ и проверяет исходные receipt/decision свежим readback. Отказ disclosure не переписывает durable facts и не откатывает их.
func personalEmailReceiptDeadlineFixture(t *testing.T, ctx context.Context, service *platformservice.Service, admin, reader, bridge value.Principal, binding *entity.AccessBinding, original entity.EmailEffectReceiptView, trace *emailReceiptDeadlineTrace) *entity.AccessBinding {
	t.Helper()
	for _, mode := range []string{trace.mode} {
		until := time.Now().UTC().Add(3 * time.Second)
		conditions := binding.Conditions
		conditions.ValidUntil = &until
		changed, err := service.Execute(ctx, command.Command{Kind: command.ChangeAccessBinding, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-receipt-finite-" + mode, ExpectedVersion: &binding.Version}, Payload: command.AccessBindingInput{BindingRef: binding.Ref, SubjectKind: binding.Subject.Kind, SubjectRef: binding.Subject.Ref, RoleVersionRef: binding.RoleVersion.Ref, Scope: binding.Scope, Conditions: conditions}})
		if err != nil || changed.AccessBinding == nil || changed.AccessBinding.Conditions.ValidUntil == nil {
			t.Fatal("set finite email receipt access", err)
		}
		before := trace.hits.Load()
		trace.deadline.Store(changed.AccessBinding.Conditions.ValidUntil)
		trace.read.Store(false)
		trace.armed.Store(true)
		var result entity.EmailEffectReceiptView
		if mode == "metadata" {
			result, err = service.GetEmailEffectReceipt(ctx, reader, original.Receipt.InvocationRef)
		} else {
			result, err = service.ResolveEmailReconciliation(ctx, bridge, original.Receipt.Ref, original.Decision.Ref, original.Receipt.ExternalReceiptRef, original.Receipt.ExternalReceiptDigest)
		}
		want := errs.ErrNotFound
		if mode == "reconciliation" {
			want = errs.ErrForbidden
		}
		if !errors.Is(err, want) || !reflect.DeepEqual(result, entity.EmailEffectReceiptView{}) || trace.hits.Load() != before+1 || ctx.Err() != nil {
			t.Fatal("expired email receipt allowance escaped a successful native COMMIT", mode, err)
		}
		restored, err := service.Execute(ctx, command.Command{Kind: command.ChangeAccessBinding, Principal: admin, Mutation: value.Mutation{IdempotencyKey: "email-receipt-restore-" + mode, ExpectedVersion: &changed.AccessBinding.Version}, Payload: command.AccessBindingInput{BindingRef: binding.Ref, SubjectKind: binding.Subject.Kind, SubjectRef: binding.Subject.Ref, RoleVersionRef: binding.RoleVersion.Ref, Scope: binding.Scope, Conditions: binding.Conditions}})
		if err != nil || restored.AccessBinding == nil {
			t.Fatal("restore email receipt reader access", err)
		}
		binding = restored.AccessBinding
		fresh, err := service.GetEmailEffectReceipt(ctx, reader, original.Receipt.InvocationRef)
		if err != nil || fresh.Receipt.Ref != original.Receipt.Ref || fresh.Receipt.Version != original.Receipt.Version || fresh.Receipt.Outcome != original.Receipt.Outcome || fresh.Decision == nil || fresh.Decision.Ref != original.Decision.Ref || !fresh.Decision.ExpiresAt.Equal(original.Decision.ExpiresAt) {
			t.Fatal("expired disclosure changed durable email receipt or decision", err)
		}
	}
	return binding
}
