// Package testsupport предоставляет единый безопасный lifecycle одноразовой PostgreSQL для тестов.
package testsupport

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const vectorExtensionLockName = "mattercodex.test.vector-extension.v1"

const (
	disposableMarkerPrefix        = "mattercodex-disposable-v1"
	disposableDatabasePrefix      = "mc_test_"
	disposableMarkerLifetime      = 6 * time.Hour
	disposableMarkerClockSkew     = 5 * time.Minute
	testDatabaseDSNEnvironment    = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_DSN"
	testDatabaseMarkerEnvironment = "MATTERCODEX_BOT_SERVICE_TEST_DATABASE_MARKER"
)

var resourceSequence atomic.Uint64
var disposableMarkers sync.Map

type DisposableDatabase struct {
	DSN      string
	Marker   string
	Database string
}

type disposableMarker struct {
	issuedAt  time.Time
	expiresAt time.Time
	token     string
}

func RequiredDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(testDatabaseDSNEnvironment))
	if dsn == "" {
		if os.Getenv("MATTERCODEX_POSTGRES_TEST_REQUIRED") == "1" {
			t.Fatal("одноразовый PostgreSQL target обязателен в required-режиме")
		}
		t.Skip("одноразовый PostgreSQL target не задан")
	}
	marker, err := markerForDSN(dsn)
	if err != nil {
		t.Fatal("одноразовый PostgreSQL target не имеет допустимой per-run identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ValidateDisposableDatabase(ctx, dsn, marker); err != nil {
		t.Fatal("одноразовый PostgreSQL target не прошёл fail-closed admission")
	}
	return dsn
}

func ValidateDisposableDatabase(ctx context.Context, dsn string, markerValue string) error {
	config, marker, err := validateDisposableIdentityOffline(dsn, markerValue, time.Now().UTC())
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("одноразовый PostgreSQL target недоступен")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("одноразовый PostgreSQL target недоступен")
	}
	defer connection.Release()
	return validateDisposableDatabaseConnection(ctx, connection, config, markerValue, marker)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func validateDisposableDatabaseConnection(ctx context.Context, querier rowQuerier, config *pgxpool.Config, markerValue string, marker disposableMarker) error {
	var currentDatabase string
	var serverAddress string
	var serverPort int
	var databaseComment string
	if err := querier.QueryRow(ctx, `
select current_database(), coalesce(inet_server_addr()::text, ''), coalesce(inet_server_port(), 0),
	coalesce(shobj_description(oid, 'pg_database'), '')
from pg_database
where datname = current_database()
`).Scan(&currentDatabase, &serverAddress, &serverPort, &databaseComment); err != nil {
		return fmt.Errorf("read-only проверка одноразового PostgreSQL target не выполнена")
	}
	if currentDatabase != config.ConnConfig.Database || currentDatabase != disposableDatabaseName(markerValue) || databaseComment != markerValue {
		return fmt.Errorf("одноразовый PostgreSQL target имеет несовпадающую identity")
	}
	if localEndpoint(config.ConnConfig.Host) {
		if serverAddress != "" {
			address := net.ParseIP(strings.TrimSpace(serverAddress))
			if address == nil || !address.IsLoopback() {
				return fmt.Errorf("локальный PostgreSQL target разрешился во внешний endpoint")
			}
		}
	} else if serverPort != int(config.ConnConfig.Port) {
		return fmt.Errorf("PostgreSQL target подключён к несовпадающему endpoint")
	}
	if time.Now().UTC().After(marker.expiresAt) {
		return fmt.Errorf("одноразовый PostgreSQL target просрочен")
	}
	return nil
}

func BootstrapDisposableDatabase(ctx context.Context, bootstrapDSN string) (DisposableDatabase, error) {
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return DisposableDatabase{}, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return DisposableDatabase{}, fmt.Errorf("bootstrap PostgreSQL endpoint недоступен")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return DisposableDatabase{}, fmt.Errorf("bootstrap PostgreSQL endpoint недоступен")
	}
	defer connection.Release()
	if err := verifyBootstrapEndpoint(ctx, connection, config); err != nil {
		return DisposableDatabase{}, err
	}
	markerValue, err := newDisposableMarker(time.Now().UTC())
	if err != nil {
		return DisposableDatabase{}, err
	}
	database := disposableDatabaseName(markerValue)
	identifier := pgx.Identifier{database}.Sanitize()
	if _, err := connection.Exec(ctx, "create database "+identifier+" template template0"); err != nil {
		return DisposableDatabase{}, fmt.Errorf("создание одноразовой PostgreSQL database не выполнено")
	}
	comment := strings.ReplaceAll(markerValue, "'", "''")
	if _, err := connection.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
		return DisposableDatabase{}, fmt.Errorf("маркировка одноразовой PostgreSQL database не выполнена")
	}
	targetDSN, err := deriveDSN(bootstrapDSN, database, "public")
	if err != nil {
		return DisposableDatabase{}, err
	}
	disposableMarkers.Store(database, markerValue)
	if err := ValidateDisposableDatabase(ctx, targetDSN, markerValue); err != nil {
		return DisposableDatabase{}, err
	}
	return DisposableDatabase{DSN: targetDSN, Marker: markerValue, Database: database}, nil
}

func DestroyDisposableDatabase(ctx context.Context, bootstrapDSN string, target DisposableDatabase) error {
	if target.Database == "" || target.Database != disposableDatabaseName(target.Marker) {
		return fmt.Errorf("identity одноразовой PostgreSQL database не подтверждена")
	}
	if err := ValidateDisposableDatabase(ctx, target.DSN, target.Marker); err != nil {
		return fmt.Errorf("удаление PostgreSQL target отклонено повторным admission")
	}
	config, err := validateBootstrapEndpointOffline(bootstrapDSN)
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("bootstrap PostgreSQL endpoint недоступен при cleanup")
	}
	defer pool.Close()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("bootstrap PostgreSQL endpoint недоступен при cleanup")
	}
	defer connection.Release()
	if err := verifyBootstrapEndpoint(ctx, connection, config); err != nil {
		return err
	}
	if err := ValidateDisposableDatabase(ctx, target.DSN, target.Marker); err != nil {
		return fmt.Errorf("удаление PostgreSQL target отклонено финальным admission")
	}
	if _, err := connection.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
		return fmt.Errorf("удаление одноразовой PostgreSQL database не выполнено")
	}
	disposableMarkers.Delete(target.Database)
	return nil
}

func validateDisposableIdentityOffline(dsn string, markerValue string, now time.Time) (*pgxpool.Config, disposableMarker, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return nil, disposableMarker{}, err
	}
	marker, err := parseDisposableMarker(markerValue, now)
	if err != nil {
		return nil, disposableMarker{}, err
	}
	database := strings.TrimSpace(config.ConnConfig.Database)
	if database == "" || database != disposableDatabaseName(markerValue) || canonicalDatabaseName(database) {
		return nil, disposableMarker{}, fmt.Errorf("PostgreSQL target не является одноразовой database")
	}
	if err := admitConfigEndpoints(config); err != nil {
		return nil, disposableMarker{}, err
	}
	if configuredProductionIdentity(config, database) {
		return nil, disposableMarker{}, fmt.Errorf("PostgreSQL target совпадает с настроенной production identity")
	}
	return config, marker, nil
}

func validateBootstrapEndpointOffline(dsn string) (*pgxpool.Config, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return nil, err
	}
	if err := admitConfigEndpoints(config); err != nil {
		return nil, err
	}
	if configuredProductionIdentity(config, config.ConnConfig.Database) {
		return nil, fmt.Errorf("bootstrap PostgreSQL endpoint совпадает с настроенной production identity")
	}
	return config, nil
}

func verifyBootstrapEndpoint(ctx context.Context, querier rowQuerier, config *pgxpool.Config) error {
	var serverAddress string
	var serverPort int
	var databaseComment string
	if err := querier.QueryRow(ctx, `
select coalesce(inet_server_addr()::text, ''), coalesce(inet_server_port(), 0),
	coalesce(shobj_description(oid, 'pg_database'), '')
from pg_database where datname = current_database()
`).Scan(&serverAddress, &serverPort, &databaseComment); err != nil {
		return fmt.Errorf("read-only проверка bootstrap PostgreSQL endpoint не выполнена")
	}
	return verifyBootstrapEndpointSnapshot(config, serverAddress, serverPort, databaseComment)
}

func verifyBootstrapEndpointSnapshot(config *pgxpool.Config, serverAddress string, serverPort int, databaseComment string) error {
	if localEndpoint(config.ConnConfig.Host) {
		if serverAddress != "" {
			address := net.ParseIP(strings.TrimSpace(serverAddress))
			if address == nil || !address.IsLoopback() {
				return fmt.Errorf("локальный bootstrap PostgreSQL разрешился во внешний endpoint")
			}
		}
		return nil
	}
	if serverPort != int(config.ConnConfig.Port) {
		return fmt.Errorf("CI bootstrap PostgreSQL подключён к несовпадающему endpoint")
	}
	expectedMarker := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINT_MARKER"))
	if expectedMarker == "" || databaseComment != expectedMarker {
		return fmt.Errorf("CI bootstrap PostgreSQL endpoint не имеет точного ephemeral marker")
	}
	return nil
}

func parseDSNWithoutDisclosure(dsn string) (*pgxpool.Config, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("PostgreSQL DSN отсутствует")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("PostgreSQL DSN имеет недопустимый URL/keyword синтаксис")
	}
	return config, nil
}

func admitConfigEndpoints(config *pgxpool.Config) error {
	if err := admitEndpoint(config.ConnConfig.Host, config.ConnConfig.Port); err != nil {
		return err
	}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(fallback.Host), strings.TrimSpace(config.ConnConfig.Host)) || fallback.Port != config.ConnConfig.Port {
			return fmt.Errorf("PostgreSQL DSN с альтернативным fallback endpoint запрещён")
		}
	}
	return nil
}

func admitEndpoint(host string, port uint16) error {
	host = strings.TrimSpace(host)
	if localEndpoint(host) {
		return nil
	}
	endpoint := net.JoinHostPort(host, strconv.Itoa(int(port)))
	for _, allowed := range strings.Split(os.Getenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS"), ",") {
		if strings.EqualFold(strings.TrimSpace(allowed), endpoint) && host != "" {
			return nil
		}
	}
	return fmt.Errorf("PostgreSQL endpoint не входит в fail-closed ephemeral allowlist")
}

func localEndpoint(host string) bool {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "/") {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func configuredProductionIdentity(config *pgxpool.Config, database string) bool {
	database = strings.TrimSpace(database)
	hosts := []string{strings.TrimSpace(config.ConnConfig.Host)}
	for _, fallback := range config.ConnConfig.Fallbacks {
		if fallback != nil {
			hosts = append(hosts, strings.TrimSpace(fallback.Host))
		}
	}
	if configuredHost := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_HOST")); configuredHost != "" && containsFold(hosts, configuredHost) {
		return true
	}
	if configuredDatabase := strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_DB")); configuredDatabase != "" && strings.EqualFold(configuredDatabase, database) {
		return true
	}
	for _, key := range []string{"MATTERCODEX_DATABASE_DSN", "MATTERCODEX_MIGRATIONS_DATABASE_DSN"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		config, err := pgxpool.ParseConfig(value)
		if err != nil {
			return true
		}
		productionHosts := []string{strings.TrimSpace(config.ConnConfig.Host)}
		for _, fallback := range config.ConnConfig.Fallbacks {
			if fallback != nil {
				productionHosts = append(productionHosts, strings.TrimSpace(fallback.Host))
			}
		}
		if intersectsFold(hosts, productionHosts) || strings.EqualFold(strings.TrimSpace(config.ConnConfig.Database), database) {
			return true
		}
	}
	return false
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if sameEndpointHost(value, expected) {
			return true
		}
	}
	return false
}

func sameEndpointHost(left string, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(left, right) || localEndpoint(left) && localEndpoint(right)
}

func intersectsFold(left []string, right []string) bool {
	for _, value := range left {
		if value != "" && containsFold(right, value) {
			return true
		}
	}
	return false
}

func canonicalDatabaseName(database string) bool {
	database = strings.ToLower(strings.TrimSpace(database))
	return database == "" || database == "postgres" || database == "mattermost" ||
		database == "mattercodex" || database == "matter_codex" || strings.HasPrefix(database, "template")
}

func newDisposableMarker(now time.Time) (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("генерация identity одноразовой PostgreSQL database не выполнена")
	}
	return strings.Join([]string{
		disposableMarkerPrefix,
		strconv.FormatInt(now.Unix(), 10),
		strconv.FormatInt(now.Add(disposableMarkerLifetime).Unix(), 10),
		hex.EncodeToString(token),
	}, ":"), nil
}

func parseDisposableMarker(value string, now time.Time) (disposableMarker, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != disposableMarkerPrefix || len(parts[3]) != 64 {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый формат")
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый формат")
	}
	issuedUnix, issuedErr := strconv.ParseInt(parts[1], 10, 64)
	expiresUnix, expiresErr := strconv.ParseInt(parts[2], 10, 64)
	if issuedErr != nil || expiresErr != nil {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker имеет недопустимый срок")
	}
	marker := disposableMarker{issuedAt: time.Unix(issuedUnix, 0).UTC(), expiresAt: time.Unix(expiresUnix, 0).UTC(), token: parts[3]}
	if marker.issuedAt.After(now.Add(disposableMarkerClockSkew)) || !marker.expiresAt.After(now) || marker.expiresAt.After(marker.issuedAt.Add(disposableMarkerLifetime+disposableMarkerClockSkew)) {
		return disposableMarker{}, fmt.Errorf("per-run PostgreSQL marker просрочен или ещё не действителен")
	}
	return marker, nil
}

func disposableDatabaseName(markerValue string) string {
	digest := sha256.Sum256([]byte(markerValue))
	return disposableDatabasePrefix + hex.EncodeToString(digest[:12])
}

func markerForDSN(dsn string) (string, error) {
	config, err := parseDSNWithoutDisclosure(dsn)
	if err != nil {
		return "", err
	}
	database := strings.TrimSpace(config.ConnConfig.Database)
	if marker, ok := disposableMarkers.Load(database); ok {
		return marker.(string), nil
	}
	marker := strings.TrimSpace(os.Getenv(testDatabaseMarkerEnvironment))
	if marker == "" {
		return "", fmt.Errorf("per-run PostgreSQL marker отсутствует")
	}
	return marker, nil
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
	markerValue, err := markerForDSN(baseDSN)
	if err != nil {
		t.Fatal("создание schema отклонено: disposable marker отсутствует")
	}
	config, marker, err := validateDisposableIdentityOffline(baseDSN, markerValue, time.Now().UTC())
	if err != nil {
		t.Fatal("создание schema отклонено offline admission")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("создание подключения к PostgreSQL не выполнено")
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal("подключение для создания schema не получено")
	}
	if err := validateDisposableDatabaseConnection(ctx, connection, config, markerValue, marker); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("CREATE SCHEMA отклонён повторным admission одноразовой database")
	}
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := connection.Exec(ctx, "create schema "+identifier); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание изолированной схемы PostgreSQL не выполнено")
	}
	connection.Release()
	pool.Close()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		marker, markerErr := markerForDSN(baseDSN)
		if markerErr != nil || ValidateDisposableDatabase(cleanupCtx, baseDSN, marker) != nil {
			t.Error("очистка schema отклонена повторным admission одноразовой database")
			return
		}
		cleanupConfig, cleanupMarker, cleanupErr := validateDisposableIdentityOffline(baseDSN, marker, time.Now().UTC())
		if cleanupErr != nil {
			t.Error("очистка schema отклонена offline admission")
			return
		}
		cleanupPool, cleanupErr := pgxpool.NewWithConfig(cleanupCtx, cleanupConfig)
		if cleanupErr != nil {
			t.Error("подключение для очистки PostgreSQL не выполнено")
			return
		}
		defer cleanupPool.Close()
		cleanupConnection, cleanupErr := cleanupPool.Acquire(cleanupCtx)
		if cleanupErr != nil {
			t.Error("подключение для очистки schema не получено")
			return
		}
		defer cleanupConnection.Release()
		if validateDisposableDatabaseConnection(cleanupCtx, cleanupConnection, cleanupConfig, marker, cleanupMarker) != nil {
			t.Error("DROP SCHEMA отклонён повторным admission одноразовой database")
			return
		}
		if _, cleanupErr := cleanupConnection.Exec(cleanupCtx, "drop schema "+identifier+" cascade"); cleanupErr != nil {
			t.Error("очистка схемы PostgreSQL не выполнена")
		}
	})
	return dsnWithDatabaseAndSearchPath(t, baseDSN, "", schema+",public")
}

func EnsureVectorExtension(ctx context.Context, dsn string) error {
	marker, err := markerForDSN(dsn)
	if err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён admission одноразовой PostgreSQL database")
	}
	config, parsedMarker, err := validateDisposableIdentityOffline(dsn, marker, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён admission одноразовой PostgreSQL database")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("создание подключения для extension не выполнено")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("начало транзакции extension: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := validateDisposableDatabaseConnection(ctx, tx, config, marker, parsedMarker); err != nil {
		return fmt.Errorf("CREATE EXTENSION отклонён повторным admission одноразовой PostgreSQL database")
	}
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
	markerValue, err := newDisposableMarker(time.Now().UTC())
	if err != nil {
		t.Fatal("генерация identity чистой PostgreSQL database не выполнена")
	}
	database := disposableDatabaseName(markerValue)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	baseMarkerValue, err := markerForDSN(baseDSN)
	if err != nil {
		t.Fatal("parent PostgreSQL database не имеет disposable identity")
	}
	baseConfig, baseMarker, err := validateDisposableIdentityOffline(baseDSN, baseMarkerValue, time.Now().UTC())
	if err != nil {
		t.Fatal("parent PostgreSQL database не прошла offline admission")
	}
	pool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal("подключение для создания тестовой database не выполнено")
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal("подключение для создания тестовой database не получено")
	}
	if err := validateDisposableDatabaseConnection(ctx, connection, baseConfig, baseMarkerValue, baseMarker); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание database отклонено повторным admission parent database")
	}
	identifier := pgx.Identifier{database}.Sanitize()
	if _, err := connection.Exec(ctx, "create database "+identifier+" template template0"); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("создание чистой одноразовой database не выполнено")
	}
	comment := strings.ReplaceAll(markerValue, "'", "''")
	if _, err := connection.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
		connection.Release()
		pool.Close()
		t.Fatal("маркировка чистой одноразовой PostgreSQL database не выполнена")
	}
	connection.Release()
	pool.Close()
	targetDSN := dsnWithDatabaseAndSearchPath(t, baseDSN, database, "public")
	disposableMarkers.Store(database, markerValue)
	if err := ValidateDisposableDatabase(ctx, targetDSN, markerValue); err != nil {
		t.Fatal("чистая одноразовая PostgreSQL database не прошла admission")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if ValidateDisposableDatabase(cleanupCtx, targetDSN, markerValue) != nil {
			t.Error("удаление чистой PostgreSQL database отклонено повторным admission")
			return
		}
		parentMarker, markerErr := markerForDSN(baseDSN)
		if markerErr != nil || ValidateDisposableDatabase(cleanupCtx, baseDSN, parentMarker) != nil {
			t.Error("cleanup parent PostgreSQL database не прошла admission")
			return
		}
		cleanupConfig, cleanupMarker, cleanupErr := validateDisposableIdentityOffline(baseDSN, parentMarker, time.Now().UTC())
		if cleanupErr != nil {
			t.Error("cleanup parent PostgreSQL database не прошла offline admission")
			return
		}
		cleanupPool, cleanupErr := pgxpool.NewWithConfig(cleanupCtx, cleanupConfig)
		if cleanupErr != nil {
			t.Error("подключение для удаления тестовой database не выполнено")
			return
		}
		defer cleanupPool.Close()
		cleanupConnection, cleanupErr := cleanupPool.Acquire(cleanupCtx)
		if cleanupErr != nil {
			t.Error("cleanup connection одноразовой database не получено")
			return
		}
		defer cleanupConnection.Release()
		if validateDisposableDatabaseConnection(cleanupCtx, cleanupConnection, cleanupConfig, parentMarker, cleanupMarker) != nil {
			t.Error("DROP DATABASE отклонён повторным admission parent database")
			return
		}
		if _, cleanupErr := cleanupConnection.Exec(cleanupCtx, "drop database "+identifier+" with (force)"); cleanupErr != nil {
			t.Error("удаление одноразовой database не выполнено")
		}
		disposableMarkers.Delete(database)
	})
	return targetDSN
}

func dsnWithDatabaseAndSearchPath(t *testing.T, dsn string, database string, searchPath string) string {
	t.Helper()
	result, err := deriveDSN(dsn, database, searchPath)
	if err != nil {
		t.Fatal("не удалось получить DSN одноразовой PostgreSQL database")
	}
	return result
}

func deriveDSN(dsn string, database string, searchPath string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		if database != "" {
			parsed.Path = "/" + database
		}
		query := parsed.Query()
		query.Set("search_path", searchPath)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	if strings.ContainsAny(database+searchPath, " '\"=\\") {
		return "", fmt.Errorf("небезопасный идентификатор одноразовой PostgreSQL")
	}
	result := strings.TrimSpace(dsn)
	if database != "" {
		result += " dbname=" + database
	}
	return result + " search_path=" + searchPath, nil
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
