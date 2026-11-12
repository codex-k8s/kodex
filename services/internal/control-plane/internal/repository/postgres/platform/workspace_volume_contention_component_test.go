package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryWorkspaceVolumeContentionLock holds the exact fixture root independently of its worker.
// queryWorkspaceVolumeContentionLock удерживает точный корень fixture независимо от worker.
//
//go:embed testdata/sql/workspace_volume_contention_lock.sql
var queryWorkspaceVolumeContentionLock string

// queryWorkspaceVolumeContentionWait proves an actual database waiter blocked by the fixture connection.
// queryWorkspaceVolumeContentionWait доказывает настоящее ожидание блокировки соединением fixture.
//
//go:embed testdata/sql/workspace_volume_contention_wait.sql
var queryWorkspaceVolumeContentionWait string

// testWorkspaceVolumeRenewContention requires a known grant to wait for its busy owner root,
// then revalidate and renew the same tuple. It never models a physical PVC or a successful Job.
// testWorkspaceVolumeRenewContention требует ожидания занятого owner root для известного grant,
// затем свежей проверки и renewal той же пары. Физический PVC либо успешный Job не моделируются.
func testWorkspaceVolumeRenewContention(t *testing.T, parent context.Context, repository *Repository,
	service *platformservice.Service, principal value.Principal, runRef string, proof command.WorkspaceVolumeTaskInput,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	locker, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin physical owner lock", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), time.Second)
		defer stop()
		_ = locker.Rollback(cleanup)
	}()
	var rootID string
	if err := locker.QueryRow(ctx, queryWorkspaceVolumeContentionLock, runRef).Scan(&rootID); err != nil {
		t.Fatal("lock physical owner root", err)
	}
	bounded, stopBounded := context.WithTimeout(ctx, 40*time.Millisecond)
	_, boundedErr := service.RenewWorkspaceVolumeTask(bounded, principal, proof)
	stopBounded()
	if !errors.Is(boundedErr, errs.ErrUnavailable) {
		t.Fatal("expired lock-wait budget did not reject renewal", boundedErr)
	}
	finished := make(chan error, 1)
	go func() {
		result, err := service.RenewWorkspaceVolumeTask(ctx, principal, proof)
		if err == nil && (stringMap(result, "leaseRef") != proof.LeaseRef || stringMap(result, "fence") != proof.Fence ||
			runtimeRevisionMapInt64(result, "generation") != proof.Generation) {
			err = context.Canceled
		}
		finished <- err
	}()
	waiting := false
	var early error
	returned := false
	for !waiting && !returned && ctx.Err() == nil {
		select {
		case early = <-finished:
			returned = true
		default:
			if err := repository.pool.QueryRow(ctx, queryWorkspaceVolumeContentionWait, locker.Conn().PgConn().PID()).Scan(&waiting); err != nil {
				cancel()
				break
			}
			if !waiting {
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	releaseErr := locker.Rollback(cleanup)
	stop()
	if releaseErr != nil {
		cancel()
	}
	if !returned {
		early = <-finished
	}
	if releaseErr != nil || !waiting || early != nil {
		t.Fatalf("known physical grant did not survive owner contention: waited=%t renewal=%v release=%v", waiting, early, releaseErr)
	}
}
