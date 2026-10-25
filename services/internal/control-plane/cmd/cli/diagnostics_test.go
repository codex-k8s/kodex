package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql/driver"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPostgresFailureClassesAndRedaction exercises typed permanent-error precedence and exact output,
// including driver cause loss, nested/joined secrets and errors whose Error methods cannot safely run.
// TestPostgresFailureClassesAndRedaction проверяет приоритет типизированных постоянных ошибок и точный вывод,
// включая потерю причины драйвером, вложенные/joined секреты и ошибки с небезопасными методами Error.
func TestPostgresFailureClassesAndRedaction(t *testing.T) {
	t.Parallel()
	const sentinel = "SENTINEL_PRIVATE_DSN_HOST_PASSWORD_SQL"
	private := errors.New(sentinel)
	for _, scenario := range []struct {
		name, class string
		cause       error
	}{
		{"unknown", "unknown", private},
		{"canceled", "canceled", errors.Join(private, context.Canceled)},
		{"deadline", "timeout", errors.Join(private, context.DeadlineExceeded)},
		{"auth", "database_authentication", &pgconn.PgError{Code: "28P01", Message: sentinel, Detail: sentinel}},
		{"permission", "database_permission", &pgconn.PgError{Code: "42501", Message: sentinel}},
		{"database-missing", "database_missing", &pgconn.PgError{Code: "3D000", Message: sentinel}},
		{"database-busy", "database_unavailable", &pgconn.PgError{Code: "53300", Message: sentinel}},
		{"database-starting", "database_unavailable", &pgconn.PgError{Code: "57P03", Message: sentinel}},
		{"unknown-sqlstate", "database", &pgconn.PgError{Code: sentinel, Message: sentinel}},
		{"tls-ca", "tls_verification", x509.UnknownAuthorityError{}},
		{"tls-host", "tls_verification", x509.HostnameError{Host: sentinel}},
		{"tls-invalid", "tls_verification", x509.CertificateInvalidError{Detail: sentinel}},
		{"tls-verification", "tls_verification", &tls.CertificateVerificationError{Err: private}},
		{"parse", "connection_configuration", &pgconn.ParseConfigError{}},
		{"file", "filesystem", &os.PathError{Op: sentinel, Path: sentinel, Err: private}},
		{"refused", "connection_refused", &net.OpError{Op: sentinel, Net: sentinel, Err: syscall.ECONNREFUSED}},
		{"dns-not-found", "dns_not_found", &net.DNSError{Name: sentinel, Err: sentinel, IsNotFound: true}},
		{"bad-connection", "bad_connection", errors.Join(private, driver.ErrBadConn)},
		{"joined-auth-network", "database_authentication", errors.Join(syscall.ECONNRESET, &pgconn.PgError{Code: "28P01", Message: sentinel})},
		{"joined-tls-network", "tls_verification", errors.Join(syscall.ECONNRESET, x509.UnknownAuthorityError{})},
		{"joined-config-network", "connection_configuration", errors.Join(syscall.ECONNRESET, &pgconn.ParseConfigError{})},
		{"joined-auth-deadline", "database_authentication", errors.Join(context.DeadlineExceeded, &pgconn.PgError{Code: "28P01", Message: sentinel})},
		{"connect-without-cause", "unknown", &pgconn.ConnectError{Config: &pgconn.Config{User: sentinel, Database: sentinel}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			failure := cliFailure(stageDatabaseConnect, scenario.cause)
			if !errors.Is(failure, scenario.cause) {
				t.Fatal("private typed cause lost before classification")
			}
			var output bytes.Buffer
			reportCLIFailure(&output, failure)
			if output.String() != "control-plane CLI failed stage=database_connect error_class="+scenario.class+"\n" || failure.Error() != cliFailureMessage {
				t.Fatal("unexpected class or unsafe diagnostic output")
			}
		})
	}
	for _, cause := range []error{private, cliFailure(cliStage(sentinel), private), errors.Join(private, cliFailure(stageMigration, private))} {
		var output bytes.Buffer
		reportCLIFailure(&output, cause)
		want := "control-plane CLI failed stage=unknown error_class=unknown\n"
		if errors.Is(cause, private) {
			var failure *commandFailure
			if errors.As(cause, &failure) && failure.stage == stageMigration {
				want = "control-plane CLI failed stage=migration error_class=unknown\n"
			}
		}
		if output.String() != want {
			t.Fatal("unknown or joined stage escaped closed reporting")
		}
	}
}
