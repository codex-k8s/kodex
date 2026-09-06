package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionRetryClosedClassification(t *testing.T) {
	const secret = "SENTINEL_DSN_HOST_PASSWORD"
	for _, tc := range []struct {
		name, class string
		cause       error
		retry       bool
	}{
		{"refused", "connection_refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"reset", "connection_reset", &os.SyscallError{Syscall: secret, Err: syscall.ECONNRESET}, true},
		{"host", "host_unreachable", syscall.EHOSTUNREACH, true},
		{"route", "network_unreachable", syscall.ENETUNREACH, true},
		{"network-timeout", "network_timeout", syscall.ETIMEDOUT, true},
		{"dns-temporary", "dns_temporary", &net.DNSError{Err: secret, Name: secret, IsTemporary: true}, true},
		{"dns-timeout", "dns_timeout", &net.DNSError{Err: secret, Name: secret, IsTimeout: true}, true},
		{"dns-nxdomain", "dns_not_found", &net.DNSError{Err: secret, Name: secret, IsNotFound: true}, false},
		{"dns-unknown", "dns", &net.DNSError{Err: secret, Name: secret}, false},
		{"auth", "database_authentication", &pgconn.PgError{Code: "28P01", Message: secret}, false},
		{"permission", "database_permission", &pgconn.PgError{Code: "42501", Message: secret}, false},
		{"config", "connection_configuration", &pgconn.ParseConfigError{}, false},
		{"tls", "tls_verification", &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}, false},
		{"joined-tls-network", "tls_verification", errors.Join(x509.UnknownAuthorityError{}, syscall.ECONNRESET), false},
		{"unknown", "unknown", errors.New(secret), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if retryableConnection(tc.cause) != tc.retry || failureClass(tc.cause) != tc.class {
				t.Fatal("unexpected closed class or retry decision")
			}
			var out bytes.Buffer
			reportFailure(&out, failure(stageDatabaseConnect, tc.cause))
			if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), "error_class="+tc.class) {
				t.Fatal("unsafe network diagnostic")
			}
		})
	}
}

func TestConnectionTransientThenSuccessRunsMigrationOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	pings, applies := 0, 0
	err := withReadyDatabase(ctx, func(received context.Context) error {
		pings++
		if received != ctx {
			t.Fatal("startup budget replaced")
		}
		if pings < 3 {
			return syscall.ECONNREFUSED
		}
		return nil
	}, func(received context.Context) error {
		applies++
		d, _ := received.Deadline()
		if d != deadline || pings != 3 {
			t.Fatal("migration started early or budget reset")
		}
		return nil
	}, time.Millisecond)
	if err != nil || pings != 3 || applies != 1 {
		t.Fatal("transient recovery did not invoke migration once")
	}
}

func TestConnectionExhaustionAndCancellationNeverRunMigration(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[canceled], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			pings := 0
			err := withReadyDatabase(ctx, func(context.Context) error {
				pings++
				if canceled {
					cancel()
				}
				return syscall.ECONNREFUSED
			}, func(context.Context) error { t.Fatal("migration ran without connection"); return nil }, time.Millisecond)
			var f *migrationFailure
			var wait *connectionWaitFailure
			if !errors.As(err, &f) || !errors.As(f.cause, &wait) || pings < 1 {
				t.Fatal("missing bounded wait failure")
			}
			want := "deadline"
			if canceled {
				want = "canceled"
			}
			var out bytes.Buffer
			reportFailure(&out, err)
			if !strings.Contains(out.String(), "error_class=connection_refused wait_status="+want) {
				t.Fatal("budget masked original network failure")
			}
		})
	}
}

func TestPermanentConnectionAndMigrationErrorsAreNotRetried(t *testing.T) {
	for _, cause := range []error{&pgconn.PgError{Code: "28P01"}, &pgconn.PgError{Code: "42501"}, x509.UnknownAuthorityError{}, &pgconn.ParseConfigError{}, &net.DNSError{IsNotFound: true}} {
		pings := 0
		err := withReadyDatabase(t.Context(), func(context.Context) error { pings++; return cause }, func(context.Context) error { t.Fatal("permanent failure entered goose"); return nil }, time.Millisecond)
		if err == nil || pings != 1 {
			t.Fatal("permanent connect failure retried")
		}
	}
	pings, applies := 0, 0
	sqlFailure := failure(stageMigration, syscall.ECONNRESET)
	err := withReadyDatabase(t.Context(), func(context.Context) error { pings++; return nil }, func(context.Context) error { applies++; return sqlFailure }, time.Millisecond)
	if err != sqlFailure || pings != 1 || applies != 1 {
		t.Fatal("migration SQL retried after network error")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := withReadyDatabase(ctx, func(context.Context) error { t.Fatal("ping after cancellation"); return nil }, func(context.Context) error { t.Fatal("goose after cancellation"); return nil }, time.Millisecond); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestDeadlineDuringPingPreservesPreviousNetworkCause(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	calls := 0
	err := withReadyDatabase(ctx, func(ctx context.Context) error {
		calls++
		if calls == 1 {
			return syscall.ECONNRESET
		}
		<-ctx.Done()
		return ctx.Err()
	}, func(context.Context) error { t.Fatal("migration after deadline"); return nil }, time.Millisecond)
	var out bytes.Buffer
	reportFailure(&out, err)
	if !strings.Contains(out.String(), "error_class=connection_reset wait_status=deadline") {
		t.Fatal("ping deadline masked prior network cause")
	}
}
