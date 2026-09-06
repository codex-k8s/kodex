package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrationFailureClosedClassesNeverExposeCause(t *testing.T) {
	const sentinel = "SENTINEL_DSN_SQL_PASSWORD_PATH"
	for _, test := range []struct {
		name, class string
		err         error
	}{
		{"unknown", "unknown", errors.New(sentinel)},
		{"authentication", "database_authentication", &pgconn.PgError{Code: "28P01", Message: sentinel, Detail: sentinel, InternalQuery: sentinel}},
		{"permission", "database_permission", &pgconn.PgError{Code: "42501", Message: sentinel}},
		{"arbitrary_sqlstate", "database", &pgconn.PgError{Code: sentinel, Message: sentinel}},
		{"filesystem", "filesystem", &os.PathError{Op: sentinel, Path: sentinel, Err: os.ErrNotExist}},
		{"dns", "network", &net.DNSError{Err: sentinel, Name: sentinel}},
		{"timeout", "timeout", fmt.Errorf("%s: %w", sentinel, context.DeadlineExceeded)},
		{"canceled", "canceled", context.Canceled},
		{"hostname", "tls_verification", x509.HostnameError{Certificate: &x509.Certificate{}, Host: sentinel}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := failure(stageDatabaseConnect, test.err)
			var output bytes.Buffer
			reportFailure(&output, err)
			want := migrationFailureMessage + " stage=database_connect error_class=" + test.class + "\n"
			if output.String() != want || strings.Contains(fmt.Sprint(err), sentinel) {
				t.Fatalf("unsafe diagnostic for %s", test.name)
			}
		})
	}
	if failure(stageMigration, nil) != nil {
		t.Fatal("success became failure")
	}
	var output bytes.Buffer
	reportFailure(&output, failure(migrationStage(sentinel), errors.New(sentinel)))
	if output.String() != migrationFailureMessage+" stage=unknown error_class=unknown\n" {
		t.Fatal("unregistered stage escaped")
	}
}

func TestMigrationReadFailureAndMalformedDSNStages(t *testing.T) {
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	os.Args = []string{"cli", "up"}
	file := filepath.Join(t.TempDir(), "dsn")
	t.Setenv("EMAIL_BRIDGE_MIGRATION_DSN_FILE", file)
	assertStage := func(want string) {
		t.Helper()
		var output bytes.Buffer
		reportFailure(&output, run())
		if output.String() != migrationFailureMessage+want+"\n" {
			t.Fatalf("unexpected safe diagnostic: %s", output.String())
		}
	}
	assertStage(" stage=dsn_read error_class=secure_file")
	if err := os.WriteFile(file, []byte("postgresql://SENTINEL_PASSWORD@%invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	assertStage(" stage=dsn_read error_class=secure_file")
	if err := os.Chmod(file, 0440); err != nil {
		t.Fatal(err)
	}
	assertStage(" stage=database_connect error_class=connection_configuration")
	os.Args = []string{"cli", "SENTINEL_ARGUMENT"}
	assertStage(" stage=arguments error_class=configuration")
}
