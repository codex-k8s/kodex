package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestStartupFailurePreservesTypedCauseAndRedactsOutput verifies the real probe-to-process diagnostic
// path with private typed causes, permanent-error precedence and one cleanup before any caller continuation.
// TestStartupFailurePreservesTypedCauseAndRedactsOutput проверяет реальный путь probe→process diagnostic
// с приватными типизированными причинами, приоритетом постоянного отказа и cleanup до продолжения вызывающего.
func TestStartupFailurePreservesTypedCauseAndRedactsOutput(t *testing.T) {
	const sentinel = "SENTINEL_PRIVATE_DSN_HOST_USER_PASSWORD_QUERY"
	for _, scenario := range []struct {
		name, class string
		cause       error
	}{
		{"refused", "connection_refused", &net.OpError{Op: sentinel, Net: sentinel, Err: syscall.ECONNREFUSED}},
		{"dns", "dns_not_found", &net.DNSError{Name: sentinel, Err: sentinel, IsNotFound: true}},
		{"auth", "database_authentication", &pgconn.PgError{Code: "28P01", Message: sentinel, Detail: sentinel}},
		{"unknown-sqlstate", "database", &pgconn.PgError{Code: sentinel, Message: sentinel}},
		{"tls", "tls_verification", errors.Join(syscall.ECONNRESET, &tls.CertificateVerificationError{Err: x509.HostnameError{Host: sentinel}})},
		{"configuration", "connection_configuration", errors.Join(syscall.ECONNRESET, &pgconn.ParseConfigError{})},
		{"deadline", "timeout", errors.Join(errors.New(sentinel), context.DeadlineExceeded)},
		{"unknown", "unknown", errors.New(sentinel)},
		{"connect-without-cause", "unknown", &pgconn.ConnectError{Config: &pgconn.Config{User: sentinel, Database: sentinel}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pool := &startupPoolFixture{ping: func(context.Context) error { return scenario.cause }}
			ctx := t.Context()
			waitSuffix := ""
			if scenario.name == "refused" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 30*time.Millisecond)
				defer cancel()
				waitSuffix = " wait_status=deadline"
			}
			err := verifyPostgresStartup(ctx, pool)
			if !errors.Is(err, scenario.cause) || pool.pings != 1 || pool.closes != 1 {
				t.Fatal("startup lost typed cause, retried or skipped close")
			}
			var output bytes.Buffer
			ReportFailure(&output, err)
			if output.String() != "control-plane failed stage=\"verify PostgreSQL connection\" error_class="+scenario.class+waitSuffix+"\n" || err.Error() != "verify PostgreSQL connection" {
				t.Fatal("private startup error escaped fixed process fields")
			}
		})
	}
}

// TestStartupFixedStageRegistryAndUnknownFallback preserves useful preexisting fixed messages while
// refusing forged labels, untyped errors and nested private strings without parsing Error text.
// TestStartupFixedStageRegistryAndUnknownFallback сохраняет полезные прежние фиксированные сообщения,
// отклоняя подставные метки, нетипизированные ошибки и вложенные приватные строки без разбора Error text.
func TestStartupFixedStageRegistryAndUnknownFallback(t *testing.T) {
	const sentinel = "SENTINEL_PRIVATE_STAGE_ERROR"
	for _, scenario := range []struct {
		stage          startupStage
		message, class string
	}{
		{stageConstructPlatformRepository, "construct platform repository", "unknown"},
		{stageVerifyOutbox, "verify outbox", "unknown"},
		{stageControlPlaneListenAddressIsInvalid, "control-plane listen address is invalid", "configuration"},
		{stageParseControlPlaneEnvironment, "parse control-plane environment", "configuration"},
		{stageParsePostgreSQLCA, "parse PostgreSQL CA", "tls_configuration"},
		{stageLoadPostgreSQLConfiguration, "load PostgreSQL configuration", "filesystem"},
		{startupStage(sentinel), "unknown", "unknown"},
	} {
		err := startupFailure(scenario.stage, errors.New(sentinel))
		var output bytes.Buffer
		ReportFailure(&output, err)
		if output.String() != "control-plane failed stage=\""+scenario.message+"\" error_class="+scenario.class+"\n" || err.Error() != scenario.message {
			t.Fatal("fixed stage was hidden or private value escaped registry")
		}
	}
	var output bytes.Buffer
	ReportFailure(&output, errors.New(sentinel))
	if output.String() != "control-plane failed stage=\"unknown\" error_class=unknown\n" {
		t.Fatal("unclassified error exposed arbitrary text")
	}
}

// TestBootstrapStartupReporterKeepsDriverCausePrivate covers the later bootstrap phase,
// independently of initial Ping retry, without exposing database payload in process output.
// TestBootstrapStartupReporterKeepsDriverCausePrivate проверяет поздний bootstrap отдельно
// от повторов initial Ping, не раскрывая database payload в process output.
func TestBootstrapStartupReporterKeepsDriverCausePrivate(t *testing.T) {
	for _, test := range []struct {
		name, class string
		cause       error
	}{
		{"serialization", "database", &pgconn.PgError{Code: "40001", Message: "PRIVATE_BOOTSTRAP_SQL", ConstraintName: "PRIVATE_CONSTRAINT"}},
		{"deadlock", "database", &pgconn.PgError{Code: "40P01", Detail: "PRIVATE_BOOTSTRAP_SQL"}},
		{"permission", "database_permission", &pgconn.PgError{Code: "42501", Message: "PRIVATE_BOOTSTRAP_PASSWORD"}},
		{"deadline", "timeout", context.DeadlineExceeded},
		{"cancel", "canceled", context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := startupFailure(stageBootstrapPlatform, test.cause)
			var output bytes.Buffer
			ReportFailure(&output, failure)
			if !errors.Is(failure, test.cause) || failure.Error() != "bootstrap platform" || output.String() != "control-plane failed stage=\"bootstrap platform\" error_class="+test.class+"\n" {
				t.Fatal("bootstrap process diagnostic lost cause or disclosed private payload")
			}
		})
	}
}
