package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesGateOverlapMarker identifies the one completed root update held before owner commit.
// kubernetesGateOverlapMarker помечает единственный completed root update, удерживаемый до owner commit.
type kubernetesGateOverlapMarker struct{}

// kubernetesGateOverlapTrace holds the real opener after its root UPDATE, preserving all production SQL.
// kubernetesGateOverlapTrace удерживает real opener после root UPDATE, сохраняя production SQL.
type kubernetesGateOverlapTrace struct {
	fired   atomic.Bool
	ready   chan uint32
	release chan struct{}
}

// TraceQueryStart marks only the first canonical event-sequence root update and records its backend identity.
// TraceQueryStart помечает лишь первый canonical event-sequence root update и его backend identity.
func (trace *kubernetesGateOverlapTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt && trace.fired.CompareAndSwap(false, true) {
		return context.WithValue(ctx, kubernetesGateOverlapMarker{}, conn.PgConn().PID())
	}
	return ctx
}

// TraceQueryEnd exposes a barrier only after the actual root row is locked and updated by PostgreSQL.
// TraceQueryEnd открывает barrier лишь после actual root row lock и update в PostgreSQL.
func (trace *kubernetesGateOverlapTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	pid, matched := ctx.Value(kubernetesGateOverlapMarker{}).(uint32)
	if !matched || data.Err != nil {
		return
	}
	trace.ready <- pid
	select {
	case <-trace.release:
	case <-ctx.Done():
	}
}

// assertKubernetesGateRootOverlap exercises the shared terminal trigger at READ COMMITTED, the isolation
// used by ordinary owner commands. An actual broker Begin creates the sibling Gate while the trigger's
// root update waits on that opener. No timing sleep substitutes for observed pg_blocking_pids evidence.
// assertKubernetesGateRootOverlap проверяет shared terminal trigger при READ COMMITTED, как ordinary
// owner commands. Actual broker Begin создаёт sibling Gate, а trigger ждёт его root lock.
// Timing sleep не заменяет наблюдение pg_blocking_pids; fixture не меняет production SQL либо grants.
func assertKubernetesGateRootOverlap(t *testing.T, ctx context.Context, repository *Repository, broker value.Principal, original, sibling value.ManagedKubernetesBinding, opening command.ManagedKubernetesBeginEffect) {
	t.Helper()
	work, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	trace := &kubernetesGateOverlapTrace{ready: make(chan uint32, 1), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(trace.release) }) }
	defer unblock()
	config := repository.pool.Config()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(work, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	opener := *repository
	opener.pool = pool
	openResult, closeResult := make(chan error, 1), make(chan error, 1)
	openJoined, closeJoined := false, true
	defer func() {
		unblock()
		cancel()
		if !openJoined {
			select {
			case <-openResult:
			case <-time.After(2 * time.Second):
				t.Error("native Gate opener did not join")
			}
		}
		if !closeJoined {
			select {
			case <-closeResult:
			case <-time.After(2 * time.Second):
				t.Error("native Gate closer did not join")
			}
		}
	}()
	go func() {
		_, err := opener.BeginManagedKubernetesEffect(work, broker, sibling, opening)
		openResult <- err
	}()
	var openerPID uint32
	select {
	case openerPID = <-trace.ready:
	case err := <-openResult:
		openJoined = true
		t.Fatal("opener did not reach root barrier", err)
	case <-work.Done():
		t.Fatal("opener root barrier timed out")
	}
	closerStarted := make(chan uint32, 1)
	closeJoined = false
	go func() {
		tx, err := repository.pool.BeginTx(work, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			closeResult <- err
			return
		}
		defer tx.Rollback(context.WithoutCancel(work))
		current, err := repository.resolveScope(work, broker)
		if err != nil {
			closeResult <- err
			return
		}
		args, err := kubernetesInvocationCompletionArgs(current.organizationID, original)
		if err != nil {
			closeResult <- err
			return
		}
		delete(args, "binding_digest")
		var id string
		if err = tx.QueryRow(work, queryKubernetesInvocationCancelLock, args).Scan(&id); err != nil {
			closeResult <- err
			return
		}
		var pid uint32
		if err = tx.QueryRow(work, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			closeResult <- err
			return
		}
		closerStarted <- pid
		tag, err := tx.Exec(work, queryKubernetesInvocationCancel, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id})
		if err != nil {
			closeResult <- err
			return
		}
		if tag.RowsAffected() != 1 {
			closeResult <- errors.New("native closer did not update the original invocation")
			return
		}
		closeResult <- tx.Commit(work)
	}()
	var closerPID uint32
	select {
	case closerPID = <-closerStarted:
	case err := <-closeResult:
		closeJoined = true
		t.Fatal("closer did not reach shared trigger", err)
	case <-work.Done():
		t.Fatal("closer start timed out")
	}
	for {
		var blocked bool
		if err = repository.pool.QueryRow(work, "SELECT $1::integer=ANY(pg_blocking_pids($2::integer))", openerPID, closerPID).Scan(&blocked); err != nil {
			t.Fatal("observe native root overlap", err)
		}
		if blocked {
			break
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case err := <-closeResult:
			closeJoined = true
			t.Fatal("closer completed without the required root-lock overlap", err)
		case <-work.Done():
			t.Fatal("closer never waited on opener root row")
		}
	}
	unblock()
	select {
	case err := <-openResult:
		openJoined = true
		var waiting *platformservice.KubernetesApprovalRequiredError
		if !errors.As(err, &waiting) {
			t.Fatal("actual overlapping Gate failed", err)
		}
	case <-work.Done():
		t.Fatal("overlapping opener did not complete")
	}
	select {
	case err := <-closeResult:
		closeJoined = true
		if err != nil {
			t.Fatal("overlapping shared trigger failed", err)
		}
	case <-work.Done():
		t.Fatal("overlapping closer did not complete")
	}
	var waiting bool
	if repository.pool.QueryRow(ctx, "SELECT root.state='WAITING_HUMAN' AND EXISTS(SELECT 1 FROM control_plane.owner_gates gate WHERE gate.root_run_id=root.id AND gate.state='OPEN') FROM control_plane.integration_invocations invocation JOIN control_plane.runs execution ON execution.id=invocation.run_id JOIN control_plane.runs root ON root.id=execution.root_run_id WHERE invocation.ref=$1", original.InvocationRef).Scan(&waiting) != nil || !waiting {
		t.Fatal("concurrent sibling OPEN Gate was erased by stale root predicate")
	}
}
