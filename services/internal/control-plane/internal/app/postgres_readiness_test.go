package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/postgresdiagnostic"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestPostgresReadinessTransientRefusalPreservesOneOwnerBudget recovers after two typed refusals,
// preserving the original deadline and pool ownership without reaching any application SQL path.
// TestPostgresReadinessTransientRefusalPreservesOneOwnerBudget восстанавливается после двух typed refusals,
// сохраняя исходный deadline и владение pool без входа в прикладной SQL.
func TestPostgresReadinessTransientRefusalPreservesOneOwnerBudget(t *testing.T) {
	if postgresStartupRetryDelay != time.Second {
		t.Fatal("unexpected refusal retry interval")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	calls := 0
	pool := &startupPoolFixture{ping: func(probe context.Context) error {
		calls++
		actual, ok := probe.Deadline()
		if !ok || actual != deadline || probe.Err() != nil {
			t.Fatal("startup wait reset or expired the caller budget")
		}
		if calls < 3 {
			return &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
		}
		return nil
	}}
	if err := verifyPostgresStartup(ctx, pool); err != nil || pool.pings != 3 || pool.closes != 0 || ctx.Err() != nil {
		t.Fatal("transient refusal did not preserve successful pool ownership")
	}
}

// TestPostgresReadinessExhaustionAndPauseCancellation preserves both primary refusal and owner budget
// while preventing a second probe after cancellation/deadline during the one-second pause.
// TestPostgresReadinessExhaustionAndPauseCancellation сохраняет первичный отказ и owner budget,
// запрещая второй ping после отмены/срока во время секундной паузы.
func TestPostgresReadinessExhaustionAndPauseCancellation(t *testing.T) {
	for _, outcome := range []string{"deadline", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
			defer cancel()
			var timer *time.Timer
			pool := &startupPoolFixture{ping: func(context.Context) error {
				if outcome == "canceled" {
					timer = time.AfterFunc(5*time.Millisecond, cancel)
				}
				return &net.OpError{Op: "SENTINEL_PRIVATE_HOST", Net: "tcp", Err: syscall.ECONNREFUSED}
			}}
			err := verifyPostgresStartup(ctx, pool)
			if timer != nil {
				timer.Stop()
			}
			budget := context.DeadlineExceeded
			if outcome == "canceled" {
				budget = context.Canceled
			}
			if !errors.Is(err, budget) || !errors.Is(err, syscall.ECONNREFUSED) || pool.pings != 1 || pool.closes != 1 {
				t.Fatal("wait exhaustion lost refusal/budget, retried or failed cleanup")
			}
			var output bytes.Buffer
			ReportFailure(&output, err)
			if output.String() != "control-plane failed stage=\"verify PostgreSQL connection\" error_class=connection_refused wait_status="+outcome+"\n" {
				t.Fatal("wait diagnostic hid primary refusal or leaked private details")
			}
		})
	}
}

// TestPostgresReadinessCancellationInsideLaterProbe keeps the first refusal when a later probe is
// canceled, exposes cancellation through errors.Is and closes once without attempting a third probe.
// TestPostgresReadinessCancellationInsideLaterProbe сохраняет первый отказ при отмене следующего ping,
// раскрывает отмену через errors.Is и закрывает pool один раз без третьей попытки.
func TestPostgresReadinessCancellationInsideLaterProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	calls := 0
	pool := &startupPoolFixture{ping: func(probe context.Context) error {
		calls++
		if calls == 1 {
			return syscall.ECONNREFUSED
		}
		cancel()
		<-probe.Done()
		return probe.Err()
	}}
	err := verifyPostgresStartup(ctx, pool)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, syscall.ECONNREFUSED) || pool.pings != 2 || pool.closes != 1 {
		t.Fatal("later probe cancellation lost primary cause or cleanup")
	}
	var output bytes.Buffer
	ReportFailure(&output, err)
	if output.String() != "control-plane failed stage=\"verify PostgreSQL connection\" error_class=connection_refused wait_status=canceled\n" {
		t.Fatal("later probe cancellation hid bounded wait outcome")
	}
}

// TestPostgresReadinessRejectsEveryOtherClassOnce disallows retries for authentication, TLS,
// configuration, DNS, reset, timeout, cancellation and arbitrary refusal-looking text, including joins.
// TestPostgresReadinessRejectsEveryOtherClassOnce запрещает повторы auth, TLS, configuration,
// DNS, reset, timeout, cancellation и произвольного текста с отказом, включая joined ошибки.
func TestPostgresReadinessRejectsEveryOtherClassOnce(t *testing.T) {
	for _, cause := range []error{
		&pgconn.PgError{Code: "28P01"}, &pgconn.PgError{Code: "42501"}, x509.UnknownAuthorityError{}, &pgconn.ParseConfigError{},
		&net.DNSError{IsTemporary: true}, syscall.ECONNRESET, syscall.ETIMEDOUT, context.Canceled, context.DeadlineExceeded,
		errors.New("connection_refused ECONNREFUSED SENTINEL_PRIVATE_ERROR"),
		errors.Join(syscall.ECONNREFUSED, &pgconn.PgError{Code: "28P01"}),
		errors.Join(syscall.ECONNREFUSED, x509.UnknownAuthorityError{}),
		errors.Join(syscall.ECONNREFUSED, &pgconn.ParseConfigError{}),
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		pool := &startupPoolFixture{ping: func(context.Context) error { return cause }}
		err := verifyPostgresStartup(ctx, pool)
		cancel()
		if !errors.Is(err, cause) || pool.pings != 1 || pool.closes != 1 {
			t.Fatal("non-refused cause was retried or lost cleanup")
		}
	}
}

// TestPostgresReadinessLaterPermanentErrorWins stops after a refusal followed by authentication failure;
// the previous transient state cannot conceal or retry the new authoritative rejection.
// TestPostgresReadinessLaterPermanentErrorWins останавливается после refusal и следующего auth failure;
// прежний временный отказ не может скрыть либо повторить новое авторитетное отклонение.
func TestPostgresReadinessLaterPermanentErrorWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	calls := 0
	denied := &pgconn.PgError{Code: "28P01", Message: "SENTINEL_PRIVATE_CREDENTIAL"}
	pool := &startupPoolFixture{ping: func(context.Context) error {
		calls++
		if calls == 1 {
			return syscall.ECONNREFUSED
		}
		return denied
	}}
	err := verifyPostgresStartup(ctx, pool)
	if !errors.Is(err, denied) || postgresdiagnostic.Class(err) != "database_authentication" || pool.pings != 2 || pool.closes != 1 {
		t.Fatal("permanent rejection was hidden by previous transient refusal")
	}
}

// TestPostgresReadinessOwnerBarrierInRun guards the actual composition root: PostgreSQL admission
// and its immediate error return must precede storage, bootstrap, broker and worker effects.
// TestPostgresReadinessOwnerBarrierInRun защищает настоящий composition root: допуск PostgreSQL
// и немедленный return при отказе должны предшествовать storage, bootstrap, broker и worker effects.
func TestPostgresReadinessOwnerBarrierInRun(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "app.go", nil, 0)
	if err != nil {
		t.Fatal("parse startup composition root")
	}
	var run *ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "Run" {
			run = function
		}
	}
	if run == nil {
		t.Fatal("missing Run lifecycle")
	}
	var admission token.Pos
	guarded := false
	ownerBudget := false
	for index, statement := range run.Body.List {
		assignment, ok := statement.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			continue
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
			if owner, ok := selector.X.(*ast.Ident); ok && owner.Name == "context" && selector.Sel.Name == "WithTimeout" && len(call.Args) == 2 {
				if budget, ok := call.Args[1].(*ast.SelectorExpr); ok {
					if config, ok := budget.X.(*ast.Ident); ok && config.Name == "config" && budget.Sel.Name == "StartupTimeout" {
						ownerBudget = true
					}
				}
			}
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || name.Name != "openPostgres" {
			continue
		}
		admission = call.Pos()
		if index+1 >= len(run.Body.List) {
			t.Fatal("missing PostgreSQL admission guard")
		}
		guard, ok := run.Body.List[index+1].(*ast.IfStmt)
		if !ok {
			continue
		}
		condition, ok := guard.Cond.(*ast.BinaryExpr)
		if !ok || condition.Op != token.NEQ {
			continue
		}
		left, leftOK := condition.X.(*ast.Ident)
		right, rightOK := condition.Y.(*ast.Ident)
		if !leftOK || !rightOK || left.Name != "err" || right.Name != "nil" || len(guard.Body.List) != 1 {
			continue
		}
		returned, ok := guard.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returned.Results) != 1 {
			continue
		}
		result, ok := returned.Results[0].(*ast.Ident)
		guarded = ok && result.Name == "err"
	}
	if admission == token.NoPos || !guarded || !ownerBudget {
		t.Fatal("startup admission can leak a failed pool into later effects")
	}
	effects := 0
	ast.Inspect(run.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		key := owner.Name + "." + selector.Sel.Name
		if key == "s3store.New" || key == "service.Bootstrap" || key == "natsjetstream.New" || key == "serviceruntime.StartWorkers" {
			effects++
			if call.Pos() < admission {
				t.Fatal("application effect precedes PostgreSQL readiness")
			}
		}
		return true
	})
	if effects != 5 {
		t.Fatal("startup effect inventory changed; review the readiness barrier")
	}
}

// TestPostgresReadinessUnknownBudgetRemainsClosed prevents a forged private budget error from
// becoming a false deadline/cancel observation or leaking its text through the process reporter.
// TestPostgresReadinessUnknownBudgetRemainsClosed запрещает подставной private budget ошибке
// выдавать ложный deadline/cancel либо раскрывать свой текст через process reporter.
func TestPostgresReadinessUnknownBudgetRemainsClosed(t *testing.T) {
	err := &startupError{stage: stageVerifyPostgreSQLConnection, cause: syscall.ECONNREFUSED, budget: errors.New("SENTINEL_PRIVATE_BUDGET")}
	var output bytes.Buffer
	ReportFailure(&output, err)
	if output.String() != "control-plane failed stage=\"verify PostgreSQL connection\" error_class=connection_refused wait_status=unknown\n" {
		t.Fatal("unknown budget escaped closed observation")
	}
}
