package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing/natsjetstream"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "control-plane CLI failed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 2 && args[0] == "broker" && args[1] == "bootstrap" {
		return bootstrapBroker(ctx)
	}
	if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
		return errors.New("usage: cli <up|status|broker bootstrap>")
	}
	dsnFile := strings.TrimSpace(os.Getenv("CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE"))
	if dsnFile == "" {
		return errors.New("CONTROL_PLANE_POSTGRES_ADMIN_DSN_FILE is required")
	}
	raw, err := os.ReadFile(dsnFile)
	if err != nil {
		return errors.New("read PostgreSQL DSN file")
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return errors.New("PostgreSQL DSN file is empty")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("parse PostgreSQL DSN")
	}
	database := stdlib.OpenDB(*config)
	defer database.Close()
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("configure goose: %w", err)
	}
	if err := database.PingContext(ctx); err != nil {
		return errors.New("connect PostgreSQL")
	}
	if args[0] == "status" {
		return goose.StatusContext(ctx, database, "migrations")
	}
	return goose.UpContext(ctx, database, "migrations")
}

func bootstrapBroker(ctx context.Context) error {
	replicas, err := strconv.Atoi(strings.TrimSpace(os.Getenv("CONTROL_PLANE_NATS_REPLICAS")))
	if err != nil {
		return errors.New("CONTROL_PLANE_NATS_REPLICAS is invalid")
	}
	maximumBytes, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("CONTROL_PLANE_NATS_MAX_BYTES")), 10, 64)
	if err != nil {
		return errors.New("CONTROL_PLANE_NATS_MAX_BYTES is invalid")
	}
	publisher, err := natsjetstream.New(natsjetstream.Config{
		URL: os.Getenv("CONTROL_PLANE_NATS_URL"), TLSServerName: os.Getenv("CONTROL_PLANE_NATS_TLS_SERVER_NAME"),
		CAFile: os.Getenv("CONTROL_PLANE_NATS_CA_FILE"), CertificateFile: os.Getenv("CONTROL_PLANE_NATS_CERTIFICATE_FILE"),
		PrivateKeyFile: os.Getenv("CONTROL_PLANE_NATS_PRIVATE_KEY_FILE"), CredentialsFile: os.Getenv("CONTROL_PLANE_NATS_CREDENTIALS_FILE"),
		Stream: "CONTROL_PLANE", Subjects: []string{"control_plane.run.*.*.events", "control_plane.platform.*.events"},
		Replicas: replicas, MaxMessageBytes: 64 << 10, MaxMessages: 10_000_000, MaxBytes: maximumBytes,
		MaxPerSubject: 1_000_000, MaxAge: 30 * 24 * time.Hour, DuplicateWindow: 2 * time.Minute, ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("construct NATS publisher: %w", err)
	}
	defer publisher.Close()
	if err := publisher.EnsureStream(ctx); err != nil {
		return fmt.Errorf("bootstrap NATS stream: %w", err)
	}
	return nil
}
