package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// postgresProbeFixture is a synthetic connector/connection with no network or SQL execution path.
// It records lifecycle calls and supplies a context-aware ping while Prepare/Begin fail the test.
// postgresProbeFixture является синтетическим connector/connection без сети и выполнения SQL.
// Учитывает lifecycle calls и задаёт context-aware ping; Prepare/Begin приводят к провалу теста.
type postgresProbeFixture struct {
	t                       *testing.T
	ping                    func(context.Context) error
	connects, pings, closes int
}

// Connect returns the same synthetic connection without inspecting or exposing DSN bytes.
// Connect возвращает то же синтетическое соединение без проверки или раскрытия байтов DSN.
func (fixture *postgresProbeFixture) Connect(context.Context) (driver.Conn, error) {
	fixture.connects++
	return fixture, nil
}

// Driver returns the fixture's closed driver implementation required by sql.OpenDB.
// Driver возвращает закрытую реализацию fixture driver, требуемую sql.OpenDB.
func (fixture *postgresProbeFixture) Driver() driver.Driver { return fixture }

// Open rejects the legacy DSN-based driver path so no hidden connection fallback is possible.
// Open отвергает старый DSN-based путь driver, исключая скрытое запасное подключение.
func (fixture *postgresProbeFixture) Open(string) (driver.Conn, error) {
	fixture.t.Fatal("unexpected legacy driver open")
	return nil, errors.New("unexpected driver open")
}

// Prepare fails if connection-only diagnostics attempts any SQL statement, including Goose setup.
// Prepare завершает тест при попытке connection-only диагностики выполнить SQL, включая подготовку Goose.
func (fixture *postgresProbeFixture) Prepare(string) (driver.Stmt, error) {
	fixture.t.Fatal("connection check attempted SQL")
	return nil, errors.New("unexpected SQL")
}

// Begin fails if connection-only diagnostics opens a transaction.
// Begin завершает тест при открытии транзакции connection-only диагностикой.
func (fixture *postgresProbeFixture) Begin() (driver.Tx, error) {
	fixture.t.Fatal("connection check attempted transaction")
	return nil, errors.New("unexpected transaction")
}

// Close records handle cleanup without contacting a database.
// Close учитывает очистку handle без обращения к базе данных.
func (fixture *postgresProbeFixture) Close() error { fixture.closes++; return nil }

// Ping records each actual database/sql ping and delegates to the synthetic context callback.
// Ping учитывает каждый фактический database/sql ping и вызывает синтетический context callback.
func (fixture *postgresProbeFixture) Ping(ctx context.Context) error {
	fixture.pings++
	return fixture.ping(ctx)
}

// postgresProbeDependencies configures an explicit synthetic DSN and returns counted owner callbacks.
// Caller tests assert Goose cardinality independently from SQL rejection, with no live credentials.
// postgresProbeDependencies настраивает явный синтетический DSN и возвращает учитываемые owner callbacks.
// Тесты отдельно проверяют число вызовов Goose и запрет SQL, не используя живые реквизиты.
func postgresProbeDependencies(t *testing.T, fixture *postgresProbeFixture, migrate func(context.Context, *sql.DB, string) error) postgresCLIDependencies {
	t.Helper()
	dsn := "postgres://fixture:synthetic@127.0.0.1:65432/fixture?sslmode=disable&connect_timeout=99"
	path := filepath.Join(t.TempDir(), "synthetic-dsn")
	if err := os.WriteFile(path, []byte(dsn), 0600); err != nil {
		t.Fatal("write synthetic DSN")
	}
	t.Setenv("CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE", path)
	t.Setenv("PGSERVICE", "")
	return postgresCLIDependencies{
		open: func(config *pgx.ConnConfig) *sql.DB {
			if config.Host != "127.0.0.1" || config.Port != 65432 || config.ConnectTimeout != 99*time.Second {
				t.Fatal("synthetic parse configuration changed")
			}
			// Expose only Connector: the shared fixture's Conn.Close must not also become a connector Close.
			// Открываем только Connector: Conn.Close общей fixture не должен одновременно закрывать connector.
			return sql.OpenDB(struct{ driver.Connector }{fixture})
		},
		migrate: migrate,
	}
}

// TestPostgresCheckOnlyProbesOnce verifies ParseConfig/OpenDB/PingContext, ten-second deadline,
// exact stdout, cleanup and zero Goose/SQL calls, without waiting ten seconds or using PostgreSQL.
// TestPostgresCheckOnlyProbesOnce проверяет ParseConfig/OpenDB/PingContext, десятисекундный deadline,
// точный stdout, cleanup и отсутствие Goose/SQL без ожидания десяти секунд или PostgreSQL.
func TestPostgresCheckOnlyProbesOnce(t *testing.T) {
	if postgresPingBudget != 10*time.Second {
		t.Fatal("unexpected initial ping budget")
	}
	fixture := &postgresProbeFixture{t: t, ping: func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second || time.Until(deadline) < 9*time.Second {
			t.Fatal("initial ping deadline not bounded by ten seconds")
		}
		return nil
	}}
	dependencies := postgresProbeDependencies(t, fixture, func(context.Context, *sql.DB, string) error {
		t.Fatal("check invoked Goose")
		return nil
	})
	var output bytes.Buffer
	if err := runPostgres(t.Context(), "check", &output, dependencies); err != nil || output.String() != "PostgreSQL connection check completed\n" || fixture.connects != 1 || fixture.pings != 1 || fixture.closes != 1 {
		t.Fatalf("connection-only lifecycle changed: class=%s connects=%d pings=%d closes=%d output_bytes=%d", postgresFailureClass(err), fixture.connects, fixture.pings, fixture.closes, output.Len())
	}
}

// TestPostgresCommandsPreserveCallerAndNeverRetryMigration gives all commands one bounded ping;
// up/status retain the original context and execute the migration callback once, even on transient SQL error.
// TestPostgresCommandsPreserveCallerAndNeverRetryMigration даёт всем командам один ограниченный ping;
// up/status сохраняют исходный context и однократно выполняют callback даже при временной ошибке SQL.
func TestPostgresCommandsPreserveCallerAndNeverRetryMigration(t *testing.T) {
	for _, command := range []string{"up", "status"} {
		t.Run(command, func(t *testing.T) {
			fixture := &postgresProbeFixture{t: t, ping: func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("unbounded migration initial ping")
				}
				return nil
			}}
			calls := 0
			ctx := t.Context()
			dependencies := postgresProbeDependencies(t, fixture, func(received context.Context, _ *sql.DB, selected string) error {
				calls++
				if received != ctx || received.Err() != nil || selected != command {
					t.Fatal("migration caller context or command changed")
				}
				return syscall.ECONNRESET
			})
			var output bytes.Buffer
			err := runPostgres(ctx, command, &output, dependencies)
			if !errors.Is(err, syscall.ECONNRESET) || calls != 1 || fixture.pings != 1 || fixture.closes != 1 || output.Len() != 0 {
				t.Fatalf("migration lifecycle changed: class=%s calls=%d pings=%d closes=%d output_bytes=%d", postgresFailureClass(err), calls, fixture.pings, fixture.closes, output.Len())
			}
		})
	}
}

// TestPostgresCancellationAndDeadlinePreventSuccess covers cancellation before/open/during/after ping
// for every command; shorter caller budgets prevail and neither success output nor Goose may follow.
// TestPostgresCancellationAndDeadlinePreventSuccess проверяет отмену до/open/во время/после ping
// для каждой команды; меньший caller budget приоритетен, успешный вывод и Goose запрещены.
func TestPostgresCancellationAndDeadlinePreventSuccess(t *testing.T) {
	for _, command := range []string{"check", "up", "status"} {
		for _, phase := range []string{"before", "open", "during", "after"} {
			t.Run(command+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				defer cancel()
				deadline, _ := ctx.Deadline()
				fixture := &postgresProbeFixture{t: t, ping: func(received context.Context) error {
					actual, ok := received.Deadline()
					if !ok || actual != deadline {
						t.Fatal("caller deadline was extended")
					}
					if phase == "after" {
						cancel()
						return nil
					}
					<-received.Done()
					return received.Err()
				}}
				dependencies := postgresProbeDependencies(t, fixture, func(context.Context, *sql.DB, string) error {
					t.Fatal("Goose ran after cancellation")
					return nil
				})
				open := dependencies.open
				dependencies.open = func(config *pgx.ConnConfig) *sql.DB {
					if phase == "open" {
						cancel()
					}
					return open(config)
				}
				if phase == "before" {
					cancel()
					dependencies.open = func(*pgx.ConnConfig) *sql.DB { t.Fatal("open after cancellation"); return nil }
				}
				var output bytes.Buffer
				err := runPostgres(ctx, command, &output, dependencies)
				want := context.Canceled
				if phase == "during" {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) || output.Len() != 0 {
					t.Fatal("caller cancellation/deadline produced success")
				}
			})
		}
	}
}

// TestPostgresConnectFailureStopsEveryCommand checks a single refused probe cannot reach Goose,
// publish check success or retry application work; only closed typed diagnostic labels are emitted.
// TestPostgresConnectFailureStopsEveryCommand проверяет, что единственный refused ping не вызывает Goose,
// успешный вывод check или повтор работы; выдаются только закрытые типизированные диагностические метки.
func TestPostgresConnectFailureStopsEveryCommand(t *testing.T) {
	for _, command := range []string{"check", "up", "status"} {
		t.Run(command, func(t *testing.T) {
			fixture := &postgresProbeFixture{t: t, ping: func(context.Context) error { return syscall.ECONNREFUSED }}
			dependencies := postgresProbeDependencies(t, fixture, func(context.Context, *sql.DB, string) error {
				t.Fatal("Goose ran after failed connection")
				return nil
			})
			var output, diagnostic bytes.Buffer
			err := runPostgres(t.Context(), command, &output, dependencies)
			reportCLIFailure(&diagnostic, err)
			if !errors.Is(err, syscall.ECONNREFUSED) || fixture.pings != 1 || fixture.closes != 1 || output.Len() != 0 || diagnostic.String() != "control-plane CLI failed stage=database_connect error_class=connection_refused\n" {
				t.Fatalf("failed connection lifecycle changed: class=%s pings=%d closes=%d output_bytes=%d", postgresFailureClass(err), fixture.pings, fixture.closes, output.Len())
			}
		})
	}
}

// TestPostgresRejectsInputBeforeDatabaseOpen covers argument, missing/empty file and malformed DSN
// failures through the public run dispatch; sentinel text and credential paths never reach diagnostics.
// TestPostgresRejectsInputBeforeDatabaseOpen проверяет args, missing/empty file и malformed DSN
// через публичный run; sentinel text и пути реквизитов не попадают в диагностику.
func TestPostgresRejectsInputBeforeDatabaseOpen(t *testing.T) {
	const sentinel = "SENTINEL_PRIVATE_DSN"
	for _, scenario := range []struct {
		name, input, class string
	}{
		{"missing", "", "configuration"},
		{"empty", " ", "configuration"},
		{"parse", "host='" + sentinel, "connection_configuration"},
		{"read", "missing-file", "filesystem"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), sentinel)
			if scenario.name != "read" && scenario.name != "missing" {
				if err := os.WriteFile(path, []byte(scenario.input), 0600); err != nil {
					t.Fatal("write fixture")
				}
			}
			if scenario.name == "missing" {
				path = ""
			}
			t.Setenv("CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE", path)
			err := run(t.Context(), []string{"check"})
			var output bytes.Buffer
			reportCLIFailure(&output, err)
			if err == nil || strings.Contains(output.String(), sentinel) || !strings.Contains(output.String(), "error_class="+scenario.class+"\n") {
				t.Fatal("input failure escaped redacted closed classification")
			}
		})
	}
	for _, args := range [][]string{nil, {"check", sentinel}, {sentinel}} {
		var output bytes.Buffer
		reportCLIFailure(&output, run(t.Context(), args))
		if output.String() != "control-plane CLI failed stage=arguments error_class=configuration\n" {
			t.Fatal("invalid command escaped closed arguments boundary")
		}
	}
}
