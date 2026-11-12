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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryWorkspaceClaimContentionLock holds only the fixture's exact session on a separate connection.
// queryWorkspaceClaimContentionLock удерживает только точную сессию fixture отдельным соединением.
//
//go:embed testdata/sql/workspace_claim_contention_lock.sql
var queryWorkspaceClaimContentionLock string

// queryWorkspaceClaimContentionState captures graph, authority and receipt facts before and after the aborted claim.
// queryWorkspaceClaimContentionState фиксирует граф, полномочия и квитанции до и после отменённой транзакции claim.
//
//go:embed testdata/sql/workspace_claim_contention_state.sql
var queryWorkspaceClaimContentionState string

// workspaceClaimContentionTrace signals an actual PostgreSQL NOWAIT refusal without changing the query or its error.
// workspaceClaimContentionTrace сообщает о настоящем отказе PostgreSQL NOWAIT, не меняя запрос или ошибку.
type workspaceClaimContentionTrace struct{ observed chan struct{} }

// TraceQueryStart retains the caller's bounded context and records no SQL or parameters.
// TraceQueryStart сохраняет ограниченный контекст вызывающего и не записывает SQL или параметры.
func (*workspaceClaimContentionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

// TraceQueryEnd unblocks the fixture holder only after the driver reports SQLSTATE 55P03.
// TraceQueryEnd освобождает ожидание fixture только после получения драйвером SQLSTATE 55P03.
func (trace *workspaceClaimContentionTrace) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var databaseError *pgconn.PgError
	if errors.As(data.Err, &databaseError) && databaseError.Code == "55P03" {
		select {
		case trace.observed <- struct{}{}:
		default:
		}
	}
}

// testWorkspaceClaimContention exercises the real owner command while another transaction holds
// its session, then releases that lock after the actual NOWAIT error. The failed claim must leave
// no terminal graph, revision, lease, audit or receipt; the same request can subsequently claim.
// testWorkspaceClaimContention выполняет настоящую owner-команду при занятой другой транзакцией
// сессии и снимает блокировку после фактического NOWAIT. Отказ не создаёт terminal graph,
// revision, lease, audit или receipt; тот же запрос затем успешно получает исполнение.
func testWorkspaceClaimContention(t *testing.T, parent context.Context, repository *Repository,
	service *platformservice.Service, worker value.Principal, workload, runRef string,
) command.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	originalPool := repository.pool
	trace := &workspaceClaimContentionTrace{observed: make(chan struct{}, 1)}
	configuration := originalPool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal("open isolated claim connection", err)
	}
	defer pool.Close()
	repository.pool = pool
	defer func() { repository.pool = originalPool }()
	locker, err := originalPool.Begin(ctx)
	if err != nil {
		t.Fatal("begin session holder", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), time.Second)
		defer stop()
		_ = locker.Rollback(cleanup)
	}()
	var sessionRef string
	if err := locker.QueryRow(ctx, queryWorkspaceClaimContentionLock, runRef).Scan(&sessionRef); err != nil {
		t.Fatal("lock exact fixture session", err)
	}
	request := command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "workspace-owner-contention-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: workload, Limit: 1}}
	var before, after string
	if err := originalPool.QueryRow(ctx, queryWorkspaceClaimContentionState, runRef, request.Mutation.IdempotencyKey).Scan(&before); err != nil {
		t.Fatal("read pre-claim facts", err)
	}
	finished := make(chan error, 1)
	go func() { _, err := service.Execute(ctx, request); finished <- err }()
	observed := false
	select {
	case <-trace.observed:
		observed = true
	case <-ctx.Done():
	}
	cleanup, stop := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	releaseErr := locker.Rollback(cleanup)
	stop()
	if releaseErr != nil {
		cancel()
	}
	claimErr := <-finished
	if releaseErr != nil {
		t.Fatal("release fixture session", releaseErr)
	}
	if !observed || !errors.Is(claimErr, errs.ErrUnavailable) || runtimeCandidateEligibilityFailure(claimErr) {
		t.Fatalf("real session contention became terminal eligibility: observed=%t unavailable=%t terminal=%t",
			observed, errors.Is(claimErr, errs.ErrUnavailable), runtimeCandidateEligibilityFailure(claimErr))
	}
	if err := originalPool.QueryRow(ctx, queryWorkspaceClaimContentionState, runRef, request.Mutation.IdempotencyKey).Scan(&after); err != nil || before != after {
		t.Fatal("aborted contention changed graph, custody or receipt", err)
	}
	result, err := service.Execute(ctx, request)
	if err != nil || len(result.RuntimeItems) != 1 {
		t.Fatal("same claim did not recover after lock release", err)
	}
	return result
}
