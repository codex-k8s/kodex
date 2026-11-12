package app

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestPostgresStartupDiagnosticPreservesTypedCauses checks closed labels and safe formatting, including an unprintable cause.
// TestPostgresStartupDiagnosticPreservesTypedCauses проверяет закрытые метки и безопасный вывод, включая непечатаемую причину.
func TestPostgresStartupDiagnosticPreservesTypedCauses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		class string
	}{
		{"refused", &net.OpError{Op: diagnosticSentinel, Err: syscall.ECONNREFUSED}, "connection_refused"},
		{"dns", &net.DNSError{Name: diagnosticSentinel, IsNotFound: true}, "dns_not_found"},
		{"tls", x509.UnknownAuthorityError{}, "tls_verification"},
		{"authentication", &pgconn.PgError{Code: "28P01", Message: diagnosticSentinel}, "database_authentication"},
		{"permission", &pgconn.PgError{Code: "42501", Message: diagnosticSentinel}, "database_permission"},
		{"capacity", &pgconn.PgError{Code: "53300", Message: diagnosticSentinel}, "database_unavailable"},
		{"deadline", &net.OpError{Err: context.DeadlineExceeded}, "deadline"},
		{"canceled", context.Canceled, "canceled"},
		{"unknown", unprintableDiagnosticError{}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := &postgresStartupError{step: postgresPingPool, cause: tc.cause}
			if !errors.Is(wrapped, tc.cause) || wrapped.Error() != "PostgreSQL startup failed" {
				t.Fatal("private cause or safe text changed")
			}
			got := SafeRuntimeFailure(ModeIssuer, &runtimeStageError{stage: stagePostgres, cause: wrapped})
			if !strings.HasSuffix(got, " postgres_step=PING_POOL postgres_cause="+tc.class) || strings.Contains(got, diagnosticSentinel) {
				t.Fatalf("unsafe or unclassified diagnostic: %s", got)
			}
		})
	}
	step, class, ok := postgresStartupDiagnostic(&postgresStartupError{step: postgresStartupStep(diagnosticSentinel), cause: unprintableDiagnosticError{}})
	if !ok || step != diagnosticUnknown || class != "unknown" {
		t.Fatal("unregistered diagnostic was reflected")
	}
}

// TestCapabilityPostgresRealRefusalRetainsCause exercises pgx parsing and Ping against a closed loopback port with a synthetic private DSN.
// TestCapabilityPostgresRealRefusalRetainsCause проверяет настоящий разбор pgx и Ping на закрытом loopback порту с синтетическим закрытым DSN.
func TestCapabilityPostgresRealRefusalRetainsCause(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	serverTLS, _ := syntheticMutualTLS(t)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverTLS.Certificates[0].Certificate[0]}), 0400); err != nil {
		t.Fatal(err)
	}
	dsnFile := filepath.Join(dir, "dsn")
	dsn := fmt.Sprintf("host=127.0.0.1 port=%d user=synthetic password=%s dbname=synthetic sslmode=verify-full sslrootcert=%s", port, diagnosticSentinel, ca)
	if err := os.WriteFile(dsnFile, []byte(dsn), 0400); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	pool, err := openCapabilityPostgres(ctx, postgresConnectionConfig{DSNFile: dsnFile, TLSServerName: "127.0.0.1", ExpectedUser: "synthetic", Capability: "internal_rpc_authority_issuer", MaxConnections: 1})
	if pool != nil {
		pool.Close()
		t.Fatal("rejected pool escaped")
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatal("actual driver connection refusal lost")
	}
	got := SafeRuntimeFailure(ModeIssuer, &runtimeStageError{stage: stagePostgres, cause: err})
	if !strings.HasSuffix(got, "postgres_step=PING_POOL postgres_cause=connection_refused") || strings.Contains(got, diagnosticSentinel) || strings.Contains(got, dir) {
		t.Fatal("actual driver diagnostic was unsafe or unclassified")
	}
}

// TestCapabilityPostgresRejectedConfigurationStaysPrivate exercises malformed and absent DSNs before any network connection.
// TestCapabilityPostgresRejectedConfigurationStaysPrivate проверяет неверный и отсутствующий DSN до сетевого подключения.
func TestCapabilityPostgresRejectedConfigurationStaysPrivate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	config := postgresConnectionConfig{DSNFile: path, TLSServerName: "example.invalid", ExpectedUser: "synthetic", Capability: "internal_rpc_authority_issuer", MaxConnections: 1}
	for _, tc := range []struct {
		write       bool
		step, class string
	}{{false, "READ_DSN", "unknown"}, {true, "PARSE_DSN", "configuration"}} {
		if tc.write {
			if err := os.WriteFile(path, []byte("postgres://synthetic:"+diagnosticSentinel+"@%invalid"), 0400); err != nil {
				t.Fatal(err)
			}
		}
		pool, err := openCapabilityPostgres(t.Context(), config)
		if pool != nil || err == nil {
			t.Fatal("invalid configuration accepted")
		}
		step, class, ok := postgresStartupDiagnostic(err)
		if !ok || step != tc.step || class != tc.class || strings.Contains(err.Error(), diagnosticSentinel) {
			t.Fatal("configuration classification changed or leaked")
		}
	}
}

// TestAuthorityStartupRetryIsRestricted excludes credentials, permission, TLS, parse, DNS, reset and text-shaped refusals even in joined chains.
// TestAuthorityStartupRetryIsRestricted исключает credentials, permission, TLS, parse, DNS, reset и текстовые отказы, включая joined цепочки.
func TestAuthorityStartupRetryIsRestricted(t *testing.T) {
	if !retryPostgresStartup(&net.OpError{Err: syscall.ECONNREFUSED}) {
		t.Fatal("typed refusal rejected")
	}
	for _, cause := range []error{&pgconn.PgError{Code: "28P01"}, &pgconn.PgError{Code: "42501"}, x509.UnknownAuthorityError{}, &pgconn.ParseConfigError{}, &net.DNSError{IsTemporary: true}, syscall.ECONNRESET, syscall.ETIMEDOUT, context.Canceled, context.DeadlineExceeded, errors.New("connection_refused ECONNREFUSED")} {
		if retryPostgresStartup(cause) {
			t.Fatal("permanent failure permits retry")
		}
	}
	for _, cause := range []error{&pgconn.PgError{Code: "28P01"}, &pgconn.PgError{Code: "42501"}, x509.UnknownAuthorityError{}, &pgconn.ParseConfigError{}, context.Canceled} {
		if retryPostgresStartup(errors.Join(syscall.ECONNREFUSED, cause)) {
			t.Fatal("refusal conceals authoritative rejection")
		}
	}
}

// TestAuthorityStartupBudgetKeepsRefusal checks that exhausted waiting retains the typed initial failure and an independent context outcome without printing private causes.
// TestAuthorityStartupBudgetKeepsRefusal проверяет сохранение первичного typed отказа и отдельного context outcome без печати закрытых причин.
func TestAuthorityStartupBudgetKeepsRefusal(t *testing.T) {
	err := &postgresStartupError{step: postgresPingPool, cause: syscall.ECONNREFUSED, budget: context.DeadlineExceeded}
	if !errors.Is(err, syscall.ECONNREFUSED) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cause or wait budget lost")
	}
	text := SafeRuntimeFailure(ModeIssuer, &runtimeStageError{stage: stagePostgres, cause: err})
	if !strings.Contains(text, "context_state=DEADLINE") || !strings.HasSuffix(text, "postgres_step=PING_POOL postgres_cause=connection_refused") {
		t.Fatal("refusal hidden by wait budget")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if pool, err := openCapabilityPostgres(ctx, postgresConnectionConfig{}); pool != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled startup read config or opened pool")
	}
}
