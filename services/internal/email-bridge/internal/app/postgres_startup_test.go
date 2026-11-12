package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestPostgresStartupAdmission proves initial refusal recovery, non-retried schema checks and cancellation before any further startup work.
// TestPostgresStartupAdmission доказывает восстановление начального подключения, неповторяемость проверки схемы и отмену до дальнейшего startup.
func TestPostgresStartupAdmission(t *testing.T) {
	for _, scenario := range []string{"recover", "permission", "rejected", "readiness-refused", "cancel-before", "cancel-ping", "cancel-ready", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			budget := 3 * time.Second
			if scenario == "budget" {
				budget = 20 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			if scenario == "cancel-before" {
				cancel()
			}
			pings, reads := 0, 0
			err := verifyPostgresStartup(ctx, func(context.Context) error {
				pings++
				switch scenario {
				case "recover":
					if pings == 1 {
						return syscall.ECONNREFUSED
					}
				case "permission":
					return &pgconn.PgError{Code: "42501", Message: "SYNTHETIC_PRIVATE_SQL"}
				case "cancel-ping":
					cancel()
				case "budget":
					return syscall.ECONNREFUSED
				}
				return nil
			}, func(context.Context) error {
				reads++
				switch scenario {
				case "rejected":
					return errs.Unavailable
				case "readiness-refused":
					return errs.WrapUnavailable(syscall.ECONNREFUSED)
				case "cancel-ready":
					cancel()
				}
				return nil
			})
			if scenario == "recover" {
				if err != nil || pings != 2 || reads != 1 {
					t.Fatal("initial connectivity did not recover exactly once")
				}
				return
			}
			if err == nil || pings > 1 || reads > 1 {
				t.Fatal("failed startup admitted or repeated authority assertion")
			}
			if scenario == "cancel-before" && (pings != 0 || reads != 0) {
				t.Fatal("canceled caller performed I/O")
			}
			stage, class := failureFields(err)
			switch scenario {
			case "rejected", "readiness-refused", "cancel-ready":
				if stage != string(stageDatabaseReady) || reads != 1 {
					t.Fatal("schema assertion phase lost")
				}
			default:
				if stage != string(stageDatabasePing) || reads != 0 {
					t.Fatal("failed connectivity reached schema assertion")
				}
			}
			if scenario == "rejected" && class != "readiness_rejected" {
				t.Fatal("negative schema assertion conflated with dependency failure")
			}
			if scenario == "budget" && (!errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, syscall.ECONNREFUSED) || class != "connection_refused") {
				t.Fatal("original budget or refusal lost")
			}
		})
	}
}

// TestDatabaseFailureDiagnostics preserves typed causes behind unavailable while forbidding raw formatting and unsafe retry precedence.
// TestDatabaseFailureDiagnostics сохраняет типизированную причину за unavailable, запрещая сырое форматирование и небезопасный приоритет retry.
func TestDatabaseFailureDiagnostics(t *testing.T) {
	const private = "SYNTHETIC_PRIVATE_SQL_DSN_CERT"
	for _, tc := range []struct {
		name, class string
		cause       error
	}{
		{"permission", "database_permission", &pgconn.PgError{Code: "42501", Message: private}},
		{"permission-cancel-refusal", "database_permission", errors.Join(&pgconn.PgError{Code: "42501", Message: private}, context.Canceled, syscall.ECONNREFUSED)},
		{"tls-refusal", "tls_verification", errors.Join(x509.HostnameError{Host: private}, syscall.ECONNREFUSED)},
		{"parse-refusal", "connection_configuration", errors.Join(&pgconn.ParseConfigError{ConnString: private}, syscall.ECONNREFUSED)},
		{"unknown", "unknown", unprintableFailure{}},
		{"cancel", "canceled", context.Canceled},
		{"timeout", "timeout", context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := errs.WrapUnavailable(tc.cause)
			if !errors.Is(wrapped, errs.Unavailable) || fmt.Sprintf("%+v", wrapped) != "UNAVAILABLE" || retryPostgresConnectivity(wrapped) {
				t.Fatal("domain identity, safe formatting or retry policy changed")
			}
			var output bytes.Buffer
			LogFailure(slog.New(slog.NewJSONHandler(&output, nil)), failure(stageDatabaseReady, wrapped))
			if strings.Contains(output.String(), private) || !strings.Contains(output.String(), `"error_class":"`+tc.class+`"`) {
				t.Fatal("unsafe or imprecise readiness diagnostic")
			}
		})
	}
	if retryPostgresConnectivity(errors.Join(context.Canceled, syscall.ECONNREFUSED)) || errs.WrapUnavailable(nil) != errs.Unavailable {
		t.Fatal("cancellation or bare assertion policy changed")
	}
}
