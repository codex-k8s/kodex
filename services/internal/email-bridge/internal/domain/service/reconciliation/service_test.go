package reconciliation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
)

type storeFixture struct {
	receipt.ReconciliationRepository
	pending func(context.Context, int) ([]receipt.Pending, error)
	commit  func(context.Context, receipt.Pending, receipt.Decision) (bool, error)
}

func (f storeFixture) Pending(ctx context.Context, b int) ([]receipt.Pending, error) {
	return f.pending(ctx, b)
}
func (f storeFixture) ClaimReconciliation(context.Context, receipt.Pending, time.Duration) (bool, error) {
	return true, nil
}
func (f storeFixture) CommitReconciliation(ctx context.Context, p receipt.Pending, d receipt.Decision) (bool, error) {
	return f.commit(ctx, p, d)
}

type ownerFixture struct {
	receipt.EffectAuthority
	resolve func(context.Context, receipt.OwnerReceipt, string) (receipt.Decision, error)
}

func (f ownerFixture) Reconcile(ctx context.Context, p receipt.OwnerReceipt, d string) (receipt.Decision, error) {
	return f.resolve(ctx, p, d)
}

type observer map[string]int

func (o observer) Reconciliation(v string) { o[v]++ }

func fixture() (receipt.Pending, receipt.Decision) {
	r := receipt.Record{ID: "receipt", Key: "key", Digest: api.Digest("input"), Status: "unknown", Audit: receipt.Audit{Actor: "actor", Agent: "agent", Grant: "grant", Operation: api.OperationMarkRead, ConfigurationRevision: 1, CredentialGeneration: 1}}
	scope := receipt.Scope{Tenant: "tenant", Mailbox: "mailbox"}
	o := receipt.OwnerReceipt{Ref: "owner_receipt", Version: 1, Invocation: "invocation", ExternalRef: r.ID, ExternalDigest: r.ExternalDigest(scope), InputDigest: r.Digest, EffectKey: r.Key, Mailbox: scope.Mailbox, Connection: "connection", ConfigurationRevision: 1, Outcome: receipt.Unknown}
	p := receipt.Pending{Scope: scope, Record: r, Owner: o}
	d := receipt.Decision{Ref: "decision", Version: 1, Actor: "owner", Grant: "fresh", Receipt: o, Outcome: receipt.EffectConfirmed, ExpiresAt: time.Now().Add(time.Minute)}
	return p, d
}

func TestWorkerStartupBarrierAndCancelJoin(t *testing.T) {
	for _, phase := range []string{"barrier", "pending", "rpc", "commit"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			done := make(chan error, 1)
			p, d := fixture()
			var commits atomic.Int32
			block := func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }
			s := Service{Interval: 5 * time.Second, Batch: 1, Barrier: func(ctx context.Context) error {
				if phase == "barrier" {
					return block(ctx)
				}
				return nil
			}, Repository: storeFixture{pending: func(ctx context.Context, b int) ([]receipt.Pending, error) {
				if phase == "barrier" {
					t.Error("poll before startup barrier")
				}
				if phase == "pending" {
					return nil, block(ctx)
				}
				return []receipt.Pending{p}, nil
			}, commit: func(ctx context.Context, _ receipt.Pending, _ receipt.Decision) (bool, error) {
				commits.Add(1)
				return false, block(ctx)
			}}, Authority: ownerFixture{resolve: func(ctx context.Context, _ receipt.OwnerReceipt, _ string) (receipt.Decision, error) {
				if phase == "rpc" {
					return receipt.Decision{}, block(ctx)
				}
				return d, nil
			}}}
			go func() { done <- s.Run(ctx) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("worker did not enter expected phase")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("worker did not join before dependency close")
			}
			if phase != "commit" && commits.Load() != 0 {
				t.Fatal("cancelled path reached commit")
			}
		})
	}
}

func TestWorkerPartialProgressAndFreshAuthorization(t *testing.T) {
	p, d := fixture()
	second := p
	second.Record.ID = "second"
	second.Owner.ExternalRef = "second"
	second.Owner.ExternalDigest = second.Record.ExternalDigest(second.Scope)
	o := observer{}
	calls := 0
	commits := 0
	s := Service{Batch: 2, Interval: 5 * time.Second, Observer: o, Repository: storeFixture{pending: func(context.Context, int) ([]receipt.Pending, error) { return []receipt.Pending{p, second}, nil }, commit: func(context.Context, receipt.Pending, receipt.Decision) (bool, error) { commits++; return true, nil }}, Authority: ownerFixture{resolve: func(_ context.Context, r receipt.OwnerReceipt, ref string) (receipt.Decision, error) {
		calls++
		if r.ExternalRef == "second" {
			return receipt.Decision{}, errs.Unavailable
		}
		if calls == 1 && ref != "" || calls == 2 && ref != d.Ref {
			t.Fatal("fresh explicit decision recheck missing")
		}
		return d, nil
	}}}
	if err := s.Cycle(t.Context()); err != nil || commits != 1 || calls != 3 || o["committed"] != 1 || o["error"] != 1 {
		t.Fatal("partial progress was lost")
	}
}

func TestWorkerBoundsAndNoDecision(t *testing.T) {
	p, _ := fixture()
	for _, limits := range []struct {
		batch    int
		interval time.Duration
	}{{0, 5 * time.Second}, {65, 5 * time.Second}, {1, time.Second}, {1, 301 * time.Second}} {
		s := Service{Batch: limits.batch, Interval: limits.interval, Repository: storeFixture{}, Authority: ownerFixture{}, Barrier: func(context.Context) error { return nil }}
		if err := s.Run(t.Context()); !errors.Is(err, errs.Invalid) {
			t.Fatal("unbounded worker configuration accepted")
		}
	}
	calls := 0
	o := observer{}
	s := Service{Batch: 1, Interval: 5 * time.Second, Observer: o, Repository: storeFixture{pending: func(context.Context, int) ([]receipt.Pending, error) { return []receipt.Pending{p}, nil }, commit: func(context.Context, receipt.Pending, receipt.Decision) (bool, error) {
		t.Fatal("no decision unlocked source")
		return false, nil
	}}, Authority: ownerFixture{resolve: func(context.Context, receipt.OwnerReceipt, string) (receipt.Decision, error) {
		calls++
		return receipt.Decision{}, errs.NotFound
	}}}
	if err := s.Cycle(t.Context()); err != nil || calls != 1 || o["none"] != 1 {
		t.Fatal("NOT_FOUND was not a bounded no-op")
	}
}
