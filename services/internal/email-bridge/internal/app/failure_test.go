package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type unprintableFailure struct{}

func (unprintableFailure) Error() string { panic("raw error must not be formatted") }

func TestFailureDiagnosticsNeverPrintCause(t *testing.T) {
	const secret = "SENTINEL_PASSWORD_DSN_SQL_MAILBOX_PATH"
	for _, tc := range []struct {
		name  string
		err   error
		class string
	}{
		{"unknown", errors.New(secret), "unknown"},
		{"unprintable", unprintableFailure{}, "unknown"},
		{"path", &os.PathError{Op: secret, Path: secret, Err: os.ErrPermission}, "filesystem"},
		{"database", &pgconn.PgError{Code: "42501", Message: secret, Detail: secret}, "database_permission"},
		{"database-unknown-code", &pgconn.PgError{Code: secret, Message: secret}, "database"},
		{"tls", x509.HostnameError{Host: secret}, "tls_verification"},
		{"rpc", status.Error(codes.PermissionDenied, secret), "rpc_forbidden"},
		{"cancel", context.Canceled, "canceled"},
		{"timeout", context.DeadlineExceeded, "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := failure(stageReadback, tc.err)
			var output bytes.Buffer
			LogFailure(slog.New(slog.NewJSONHandler(&output, nil)), err)
			if strings.Contains(output.String(), secret) || !strings.Contains(output.String(), `"stage":"configuration_owner_readback"`) || !strings.Contains(output.String(), `"error_class":"`+tc.class+`"`) {
				t.Fatal("unsafe or imprecise diagnostic")
			}
			if fmt.Sprintf("%+v", err) != failureMessage {
				t.Fatal("wrapper exposed cause")
			}
		})
	}
	if failure(stageReadback, nil) != nil {
		t.Fatal("success became failure")
	}
	if !errors.Is(failure(stageReadback, context.Canceled), context.Canceled) {
		t.Fatal("cancellation identity lost")
	}
	stage, class := failureFields(failure(failureStage(secret), errors.New(secret)))
	if stage != "unknown" || class != "unknown" {
		t.Fatal("unknown stage escaped registry")
	}
	stage, class = failureFields(errors.New(secret))
	if stage != "unknown" || class != "unknown" {
		t.Fatal("untyped cause exposed")
	}
}

func TestStartupConfigurationAndSecureFileStages(t *testing.T) {
	t.Setenv("EMAIL_BRIDGE_SECRETS_ROOT", "")
	err := Run(t.Context(), t.Context(), "test")
	if stage, _ := failureFields(err); stage != string(stageEnvironment) {
		t.Fatal("environment stage missing")
	}
	_, err = tlsConfig(Config{CertificateFile: filepath.Join(t.TempDir(), "missing")})
	if stage, _ := failureFields(err); stage != string(stageCertificate) {
		t.Fatal("certificate stage missing")
	}
	_, err = databaseDSN(filepath.Join(t.TempDir(), "missing"))
	if stage, _ := failureFields(err); stage != string(stageDSN) {
		t.Fatal("credential read stage missing")
	}
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("postgresql://SENTINEL_PASSWORD@wrong.invalid/foreign"), 0400); err != nil {
		t.Fatal(err)
	}
	_, err = databaseDSN(path)
	if stage, _ := failureFields(err); stage != string(stageDSNValidation) {
		t.Fatal("credential validation stage missing")
	}
}

func TestConfigurationFailureStageAndClosedPublication(t *testing.T) {
	for _, target := range []failureStage{stageConfiguration, stagePins, stageWatermark, stageService, stageReadback} {
		t.Run(string(target), func(t *testing.T) {
			root := t.TempDir()
			if target != stageConfiguration {
				mountConfiguration(t, root, "..fixture", 7)
			}
			var visited []failureStage
			check := func(stage failureStage) error {
				visited = append(visited, stage)
				if stage == target {
					return errors.New("SENTINEL_PRIVATE_METADATA")
				}
				return nil
			}
			r := &configurationRuntime{root: root,
				check:  func(api.Configuration, string) error { return check(stagePins) },
				accept: func(context.Context, api.Configuration, string) error { return check(stageWatermark) },
				build: func(*configuration.Snapshot) *mail.Service {
					if check(stageService) != nil {
						return nil
					}
					return &mail.Service{}
				},
				report: func(context.Context, int64, string) error { return check(stageReadback) },
			}
			err := r.Refresh(t.Context())
			stage, _ := failureFields(failure(stageConfiguration, err))
			if err == nil || stage != string(target) || r.Service() != nil {
				t.Fatal("failed startup published service or lost exact stage")
			}
			if target == stageConfiguration && len(visited) != 0 {
				t.Fatal("effects followed failed load")
			}
			if target != stageConfiguration && visited[len(visited)-1] != target {
				t.Fatal("effects followed failed boundary")
			}
		})
	}
}
