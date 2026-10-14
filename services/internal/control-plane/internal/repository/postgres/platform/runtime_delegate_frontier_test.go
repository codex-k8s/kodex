package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

type delegationFrontierRow func(...any) error

func (row delegationFrontierRow) Scan(dest ...any) error { return row(dest...) }

type delegationFrontierTx struct {
	pgx.Tx
	t                          *testing.T
	calls                      int
	capability, relationship   bool
	guardError                 error
	stale, expired, wrongFence bool
}

func (tx *delegationFrontierTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.calls++
	if sql == queryRuntimeLeaseForUpdateSelectRuntimeLeasesOrganizationIdRef {
		return delegationFrontierRow(func(dest ...any) error {
			for _, index := range []int{0, 1, 2, 3, 4, 5, 6, 7, 12, 13} {
				*dest[index].(*string) = "server-owned"
			}
			digest := sha256.Sum256([]byte("exact-fence"))
			*dest[8].(*string) = hex.EncodeToString(digest[:])
			*dest[9].(*int64) = 1
			*dest[10].(*string) = "CLAIMED"
			*dest[11].(*time.Time) = time.Now().Add(time.Minute)
			if tx.stale {
				*dest[9].(*int64) = 2
			}
			if tx.expired {
				*dest[11].(*time.Time) = time.Now().Add(-time.Minute)
			}
			if tx.wrongFence {
				*dest[8].(*string) = "invalid"
			}
			return nil
		})
	}
	if sql == queryRuntimeDelegateexecutionSelectRunNodesId {
		return delegationFrontierRow(func(dest ...any) error {
			if tx.guardError != nil {
				return tx.guardError
			}
			*dest[0].(*bool), *dest[1].(*bool), *dest[2].(*bool) = tx.capability, tx.relationship, false
			for _, target := range dest[3:] {
				*target.(*string) = ""
			}
			return nil
		})
	}
	tx.t.Fatal("closed preflight reached downstream read or child effect")
	return delegationFrontierRow(func(...any) error { return errs.ErrUnavailable })
}

func TestDelegateExecutionFrontierPreflight(t *testing.T) {
	for _, test := range []struct {
		name                                                 string
		capability, relationship, stale, expired, wrongFence bool
		guardError, want                                     error
		calls                                                int
	}{
		{name: "blocked-dependency", capability: true, relationship: true, want: errs.ErrConflict, calls: 2},
		{name: "capability-before-frontier", relationship: true, want: errs.ErrForbidden, calls: 2},
		{name: "relationship-before-frontier", capability: true, want: errs.ErrForbidden, calls: 2},
		{name: "stale-lease-before-frontier", capability: true, relationship: true, stale: true, want: errs.ErrForbidden, calls: 1},
		{name: "expired-lease-before-frontier", capability: true, relationship: true, expired: true, want: errs.ErrForbidden, calls: 1},
		{name: "wrong-fence-before-frontier", capability: true, relationship: true, wrongFence: true, want: errs.ErrForbidden, calls: 1},
		{name: "source-failure-closed", capability: true, relationship: true, guardError: errors.New("synthetic guard failure"), want: errs.ErrForbidden, calls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &delegationFrontierTx{t: t, capability: test.capability, relationship: test.relationship, stale: test.stale, expired: test.expired, wrongFence: test.wrongFence, guardError: test.guardError}
			_, err := (&Repository{}).delegateExecution(t.Context(), tx, scope{}, command.Command{Payload: command.DelegateInput{LeaseRef: "lease", Fence: "exact-fence", Generation: 1, TargetAgentRef: "target", WorkflowStepKey: "later", Task: "Exact task"}})
			if !errors.Is(err, test.want) || tx.calls != test.calls {
				t.Fatalf("preflight = %v after %d reads; want %v after %d", err, tx.calls, test.want, test.calls)
			}
		})
	}
}
