// Package testsupport предоставляет единый безопасный lifecycle одноразовой PostgreSQL для тестов.
package testsupport

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const vectorExtensionLockName = "mattercodex.test.vector-extension.v1"

var resourceSequence atomic.Uint64

func RequiredDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"))
	if dsn == "" {
		if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
			t.Fatal("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN обязателен в required-режиме PostgreSQL-тестов")
		}
		t.Skip("MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN не задан")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("разбор PostgreSQL DSN: %v", err)
	}
	database := strings.ToLower(strings.TrimSpace(config.ConnConfig.Database))
	if database == "" || database == "postgres" || strings.HasPrefix(database, "template") || database == "mattercodex" || database == "matter_codex" {
		t.Fatalf("PostgreSQL-тестам нужна отдельная одноразовая database, получена %q", database)
	}
	return dsn
}

func IsolatedSchemaDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := RequiredDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := EnsureVectorExtension(ctx, baseDSN); err != nil {
		t.Fatalf("подготовка database-global extension vector: %v", err)
	}
	schema := uniqueName("mc_"+label, 48)
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
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	return dsnWithDatabaseAndSearchPath(t, baseDSN, "", schema+",public")
}

func EnsureVectorExtension(ctx context.Context, dsn string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("создание подключения: %w", err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции extension: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, vectorExtensionLockName); err != nil {
		return fmt.Errorf("database-global advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `create extension if not exists vector with schema public`); err != nil {
		return fmt.Errorf("CREATE EXTENSION vector: %w", err)
	}
	var extensionCount int
	var typeAvailable bool
	if err := tx.QueryRow(ctx, `
select count(*), to_regtype('public.vector') is not null
from pg_extension extension_row
join pg_namespace namespace_row on namespace_row.oid = extension_row.extnamespace
where extension_row.extname = 'vector' and namespace_row.nspname = 'public'
`).Scan(&extensionCount, &typeAvailable); err != nil {
		return fmt.Errorf("проверка extension vector: %w", err)
	}
	if extensionCount != 1 || !typeAvailable {
		return fmt.Errorf("extension vector должна быть единственной в public: count=%d type_available=%t", extensionCount, typeAvailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("фиксация extension vector: %w", err)
	}
	return nil
}

func FreshDatabaseDSN(t *testing.T, label string) string {
	t.Helper()
	baseDSN := RequiredDSN(t)
	database := uniqueName("mc_db_"+label, 60)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("подключение для создания тестовой database: %v", err)
	}
	identifier := pgx.Identifier{database}.Sanitize()
	if _, err := pool.Exec(ctx, "create database "+identifier+" template template0"); err != nil {
		pool.Close()
		t.Fatalf("создание чистой одноразовой database: %v", err)
	}
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupPool, cleanupErr := pgxpool.New(cleanupCtx, baseDSN)
		if cleanupErr != nil {
			t.Errorf("подключение для удаления тестовой database: %v", cleanupErr)
			return
		}
		defer cleanupPool.Close()
		if _, cleanupErr := cleanupPool.Exec(cleanupCtx, "drop database "+identifier+" with (force)"); cleanupErr != nil {
			t.Errorf("удаление одноразовой database: %v", cleanupErr)
		}
	})
	return dsnWithDatabaseAndSearchPath(t, baseDSN, database, "public")
}

func dsnWithDatabaseAndSearchPath(t *testing.T, dsn string, database string, searchPath string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		if database != "" {
			parsed.Path = "/" + database
		}
		query := parsed.Query()
		query.Set("search_path", searchPath)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if strings.ContainsAny(database+searchPath, " '\"=\\") {
		t.Fatal("небезопасный идентификатор одноразовой PostgreSQL")
	}
	result := strings.TrimSpace(dsn)
	if database != "" {
		result += " dbname=" + database
	}
	return result + " search_path=" + searchPath
}

func uniqueName(prefix string, maximumLength int) string {
	suffix := fmt.Sprintf("_%x_%x", time.Now().UTC().UnixNano(), resourceSequence.Add(1))
	prefix = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' {
			return character
		}
		return '_'
	}, strings.ToLower(prefix))
	if len(prefix)+len(suffix) > maximumLength {
		prefix = prefix[:maximumLength-len(suffix)]
	}
	return prefix + suffix
}
