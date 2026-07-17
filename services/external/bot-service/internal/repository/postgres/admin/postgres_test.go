package admin_test

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var postgresSchemaSequence atomic.Uint64

func postgresTestDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	if dsn != "" {
		return dsn
	}
	if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
		t.Fatal("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN обязателен в required-режиме PostgreSQL-тестов")
	}
	t.Skip("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN не задан")
	return ""
}

func isolatedPostgresTestDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := postgresTestDSN(t)
	schema := "mc_" + label + "_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36) + "_" + strconv.FormatUint(postgresSchemaSequence.Add(1), 36)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("создание подключения к PostgreSQL: %v", err)
	}
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := pool.Exec(ctx, "create schema "+identifier); err != nil {
		pool.Close()
		t.Fatalf("создание изолированной схемы PostgreSQL: %v", err)
	}
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		cleanupPool, cleanupErr := pgxpool.New(cleanupCtx, baseDSN)
		if cleanupErr != nil {
			t.Errorf("подключение для очистки PostgreSQL: %v", cleanupErr)
			return
		}
		defer cleanupPool.Close()
		if _, cleanupErr := cleanupPool.Exec(cleanupCtx, "drop schema "+identifier+" cascade"); cleanupErr != nil {
			t.Errorf("очистка схемы PostgreSQL: %v", cleanupErr)
		}
	})
	return dsnWithSearchPath(t, baseDSN, schema)
}

func dsnWithSearchPath(t *testing.T, dsn string, schema string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if strings.ContainsAny(schema, " '=") {
		t.Fatal("небезопасное имя тестовой схемы")
	}
	return strings.TrimSpace(dsn) + " search_path=" + schema
}
