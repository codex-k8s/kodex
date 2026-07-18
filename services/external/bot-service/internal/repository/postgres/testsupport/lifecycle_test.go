package testsupport

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDisposableDatabaseOfflineAdmissionMatrix(t *testing.T) {
	now := time.Now().UTC()
	marker, err := newDisposableMarker(now)
	if err != nil {
		t.Fatal("не удалось подготовить синтетический marker")
	}
	database := disposableDatabaseName(marker)
	urlDSN := fmt.Sprintf("postgres://synthetic-user:synthetic-password@127.0.0.1:5432/%s", database)
	keywordDSN := fmt.Sprintf("host=/var/run/postgresql user=synthetic-user password=synthetic-password dbname=%s", database)
	assertAllowed := func(label string, dsn string) {
		t.Helper()
		if _, _, err := validateDisposableIdentityOffline(dsn, marker, now); err != nil {
			t.Fatalf("%s отклонён: %v", label, err)
		}
	}
	assertDenied := func(label string, dsn string, candidateMarker string) {
		t.Helper()
		_, _, err := validateDisposableIdentityOffline(dsn, candidateMarker, now)
		if err == nil {
			t.Fatalf("%s разрешён", label)
		}
		for _, secret := range []string{"synthetic-user", "synthetic-password", "attacker.invalid", "203.0.113.10", marker} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("%s раскрыл чувствительную часть target", label)
			}
		}
	}

	t.Setenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS", "")
	t.Setenv("MATTERCODEX_POSTGRES_HOST", "")
	t.Setenv("MATTERCODEX_POSTGRES_DB", "")
	t.Setenv("MATTERCODEX_DATABASE_DSN", "")
	t.Setenv("MATTERCODEX_MIGRATIONS_DATABASE_DSN", "")
	assertAllowed("URL loopback", urlDSN)
	assertAllowed("keyword Unix socket", keywordDSN)

	assertDenied("canonical mattermost", "postgres://synthetic-user:synthetic-password@127.0.0.1:5432/mattermost", marker)
	assertDenied("canonical postgres", "postgres://synthetic-user:synthetic-password@127.0.0.1:5432/postgres", marker)
	assertDenied("canonical template", "postgres://synthetic-user:synthetic-password@127.0.0.1:5432/template1", marker)
	assertDenied("external hostname", fmt.Sprintf("postgres://synthetic-user:synthetic-password@attacker.invalid:5432/%s", database), marker)
	assertDenied("external IP", fmt.Sprintf("postgres://synthetic-user:synthetic-password@203.0.113.10:5432/%s", database), marker)
	assertDenied("external fallback", fmt.Sprintf("host=127.0.0.1,attacker.invalid port=5432,5432 user=synthetic-user password=synthetic-password dbname=%s", database), marker)
	assertDenied("missing marker", urlDSN, "")
	assertDenied("mismatched marker", urlDSN, disposableMarkerPrefix+":1:2:"+strings.Repeat("a", 64))
	staleMarker := strings.Join([]string{disposableMarkerPrefix, fmt.Sprint(now.Add(-8 * time.Hour).Unix()), fmt.Sprint(now.Add(-2 * time.Hour).Unix()), strings.Repeat("b", 64)}, ":")
	assertDenied("stale marker", urlDSN, staleMarker)
	reusedMarker, err := newDisposableMarker(now.Add(-time.Minute))
	if err != nil {
		t.Fatal("не удалось подготовить reused marker")
	}
	reusedDSN := fmt.Sprintf("postgres://synthetic-user:synthetic-password@127.0.0.1:5432/%s", disposableDatabaseName(reusedMarker))
	assertDenied("reused run identity", reusedDSN, marker)
	assertDenied("malformed URL", "postgres://synthetic-user:%zz@127.0.0.1/database", marker)
	assertDenied("malformed keyword", "host='unterminated dbname=synthetic", marker)

	t.Setenv("MATTERCODEX_POSTGRES_HOST", "127.0.0.1")
	assertDenied("configured production host", urlDSN, marker)
	t.Setenv("MATTERCODEX_POSTGRES_HOST", "localhost")
	assertDenied("configured production loopback alias", urlDSN, marker)
	t.Setenv("MATTERCODEX_POSTGRES_HOST", "")
	t.Setenv("MATTERCODEX_POSTGRES_DB", database)
	assertDenied("configured production database", urlDSN, marker)
	t.Setenv("MATTERCODEX_POSTGRES_DB", "")
	t.Setenv("MATTERCODEX_DATABASE_DSN", fmt.Sprintf("postgres://production-user:production-password@127.0.0.1:6432/%s", database))
	assertDenied("configured production DSN identity", urlDSN, marker)
	t.Setenv("MATTERCODEX_DATABASE_DSN", "")
	t.Setenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS", "ci-postgres:5432")
	ciDSN := fmt.Sprintf("host=ci-postgres user=synthetic-user password=synthetic-password dbname=%s", database)
	assertAllowed("explicit ephemeral CI endpoint", ciDSN)
}

func TestEphemeralCIBootstrapRequiresExactEndpointMarker(t *testing.T) {
	config, err := pgxpool.ParseConfig("host=ci-postgres port=5432 dbname=bootstrap user=synthetic-user")
	if err != nil {
		t.Fatal("не удалось подготовить synthetic CI config")
	}
	t.Setenv("MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINT_MARKER", "synthetic-ci-run-marker")
	if err := verifyBootstrapEndpointSnapshot(config, "192.0.2.10", 5432, "synthetic-ci-run-marker"); err != nil {
		t.Fatalf("явно разрешённый ephemeral CI bootstrap отклонён: %v", err)
	}
	for name, proof := range map[string]struct {
		port    int
		comment string
	}{
		"missing marker":    {port: 5432},
		"mismatched marker": {port: 5432, comment: "foreign-marker"},
		"remapped endpoint": {port: 6432, comment: "synthetic-ci-run-marker"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := verifyBootstrapEndpointSnapshot(config, "192.0.2.10", proof.port, proof.comment); err == nil {
				t.Fatal("CI bootstrap без точного endpoint proof разрешён")
			}
		})
	}
}

func TestVectorExtensionLifecycleSerializesCleanDatabase(t *testing.T) {
	dsn := FreshDatabaseDSN(t, "vector_race")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("подключение к чистой database: %v", err)
	}
	var extensionExists bool
	if err := pool.QueryRow(ctx, `select exists(select 1 from pg_extension where extname = 'vector')`).Scan(&extensionExists); err != nil {
		pool.Close()
		t.Fatalf("исходная проверка extension: %v", err)
	}
	pool.Close()
	if extensionExists {
		t.Fatal("новая database неожиданно содержит extension vector")
	}

	for iteration := 0; iteration < 3; iteration++ {
		start := make(chan struct{})
		errors := make(chan error, 12)
		var wait sync.WaitGroup
		for range 12 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				errors <- EnsureVectorExtension(ctx, dsn)
			}()
		}
		close(start)
		wait.Wait()
		close(errors)
		for err := range errors {
			if err != nil {
				t.Fatalf("iteration %d concurrent extension setup: %v", iteration+1, err)
			}
		}
	}

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("повторное подключение: %v", err)
	}
	defer pool.Close()
	var extensionCount int
	var typeAvailable bool
	if err := pool.QueryRow(ctx, `select count(*), to_regtype('public.vector') is not null from pg_extension where extname = 'vector'`).Scan(&extensionCount, &typeAvailable); err != nil {
		t.Fatalf("итоговая проверка extension: %v", err)
	}
	if extensionCount != 1 || !typeAvailable {
		t.Fatalf("extension count=%d type_available=%t", extensionCount, typeAvailable)
	}
}
