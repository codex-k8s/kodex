package app

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestClaimBudgetAvailable distinguishes unbounded, sufficient, short, cancelled and expired contexts without claiming work.
// RU: TestClaimBudgetAvailable различает неограниченный, достаточный, короткий, отменённый и истёкший контексты без получения работы.
func TestClaimBudgetAvailable(t *testing.T) {
	t.Parallel()
	base := context.WithoutCancel(t.Context())
	far, cancelFar := context.WithTimeout(base, time.Minute)
	defer cancelFar()
	short, cancelShort := context.WithTimeout(base, time.Second)
	defer cancelShort()
	cancelled, cancel := context.WithCancel(base)
	cancel()
	expired, cancelExpired := context.WithDeadline(base, time.Now().Add(-time.Second))
	defer cancelExpired()
	for _, test := range []struct {
		name string
		ctx  context.Context
		want bool
		err  error
	}{
		{name: "unbounded", ctx: base, want: true},
		{name: "sufficient", ctx: far, want: true},
		{name: "short", ctx: short},
		{name: "cancelled", ctx: cancelled, err: context.Canceled},
		{name: "expired", ctx: expired, err: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := claimBudgetAvailable(test.ctx, time.Second)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("claim budget = %v, %v; want %v, %v", got, err, test.want, test.err)
			}
		})
	}
}

// TestWorkLoopsDoNotClaimWithoutBudget proves both queue consumers return before touching absent RPC clients when cancelled or nearly out of time.
// RU: TestWorkLoopsDoNotClaimWithoutBudget доказывает, что оба потребителя очередей возвращаются до обращения к отсутствующим RPC клиентам при отмене или малом остатке времени.
func TestWorkLoopsDoNotClaimWithoutBudget(t *testing.T) {
	t.Parallel()
	config := Config{ClaimLimit: 1, RequestTimeout: time.Second}
	for name, run := range map[string]func(context.Context) error{
		"delivery":    func(ctx context.Context) error { return processDeliveries(ctx, nil, nil, config) },
		"integration": func(ctx context.Context) error { return processIntegrationWork(ctx, nil, nil, config) },
	} {
		t.Run(name, func(t *testing.T) {
			cancelled, cancel := context.WithCancel(t.Context())
			cancel()
			if err := run(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled loop = %v", err)
			}
			short, stop := context.WithTimeout(t.Context(), time.Second)
			defer stop()
			if err := run(short); err != nil {
				t.Fatalf("short-budget loop = %v", err)
			}
		})
	}
}

// TestLeaseContextUsesEarlierDeadline checks that either parent or lease expiry bounds dispatch before the unchanged completion reserve.
// RU: TestLeaseContextUsesEarlierDeadline проверяет ограничение отправки более ранним сроком родителя или аренды с неизменным запасом завершения.
func TestLeaseContextUsesEarlierDeadline(t *testing.T) {
	t.Parallel()
	for _, parentEarlier := range []bool{false, true} {
		lease := deliveryClaim().GetLease()
		lease.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute))
		parentDeadline := lease.GetExpiresAt().AsTime().Add(time.Minute)
		want := lease.GetExpiresAt().AsTime().Add(-2 * time.Second)
		if parentEarlier {
			parentDeadline = lease.GetExpiresAt().AsTime().Add(-30 * time.Second)
			want = parentDeadline.Add(-2 * time.Second)
		}
		parent, cancelParent := context.WithDeadline(t.Context(), parentDeadline)
		child, cancelChild, err := leaseContext(parent, lease, time.Second)
		if err != nil {
			cancelParent()
			t.Fatal(err)
		}
		got, ok := child.Deadline()
		cancelChild()
		cancelParent()
		if !ok || !got.Equal(want) {
			t.Fatalf("dispatch deadline = %v, want %v", got, want)
		}
	}
}

// TestWorkCompletionKeyPreservesIdentity checks the pre-refactor encoding and separates work, lease and generation while keeping exact replay stable.
// RU: TestWorkCompletionKeyPreservesIdentity проверяет прежнее кодирование и разделение работы, аренды и поколения при устойчивом точном повторе.
func TestWorkCompletionKeyPreservesIdentity(t *testing.T) {
	t.Parallel()
	lease := &controlplanev1.WorkLease{Ref: "lease", Generation: 7}
	key := workCompletionKey("delivery", lease)
	if key != stableKey("delivery", "lease:7:complete") || key != workCompletionKey("delivery", lease) {
		t.Fatal("completion identity encoding or replay changed")
	}
	for _, changed := range []string{
		workCompletionKey("other", lease),
		workCompletionKey("delivery", &controlplanev1.WorkLease{Ref: "other", Generation: 7}),
		workCompletionKey("delivery", &controlplanev1.WorkLease{Ref: "lease", Generation: 8}),
	} {
		if changed == key {
			t.Fatal("distinct work attempt reused completion identity")
		}
	}
}
