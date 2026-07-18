package testsupport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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

func TestBootstrapProofOfflineMatrix(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	nonce := strings.Repeat("ab", 32)
	nonceBytes, _ := hex.DecodeString(nonce)
	nonceHash := sha256.Sum256(nonceBytes)
	valid := bootstrapProof{
		Version: bootstrapProofVersion, Nonce: nonce, NonceSHA256: hex.EncodeToString(nonceHash[:]),
		IssuedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(bootstrapProofLifetime).Format(time.RFC3339Nano),
		EndpointFingerprint: strings.Repeat("c", 64), ServerFingerprint: strings.Repeat("d", 64),
		MaintenanceDatabase: "bootstrap", Purpose: bootstrapProofPurpose,
		RunID: strings.Repeat("e", 32), State: bootstrapProofState,
	}
	encode := func(proof bootstrapProof) string {
		t.Helper()
		value, err := json.Marshal(proof)
		if err != nil {
			t.Fatal("сериализация synthetic proof")
		}
		return string(value)
	}
	if _, err := parseBootstrapProof(encode(valid), now); err != nil {
		t.Fatalf("допустимый one-shot proof отклонён: %v", err)
	}
	cases := map[string]func(*bootstrapProof){
		"short nonce":      func(proof *bootstrapProof) { proof.Nonce = "aa" },
		"wrong nonce hash": func(proof *bootstrapProof) { proof.NonceSHA256 = strings.Repeat("f", 64) },
		"expired": func(proof *bootstrapProof) {
			proof.IssuedAt = now.Add(-20 * time.Minute).Format(time.RFC3339Nano)
			proof.ExpiresAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
		},
		"future": func(proof *bootstrapProof) {
			proof.IssuedAt = now.Add(time.Minute).Format(time.RFC3339Nano)
			proof.ExpiresAt = now.Add(2 * time.Minute).Format(time.RFC3339Nano)
		},
		"excessive ttl": func(proof *bootstrapProof) {
			proof.ExpiresAt = now.Add(bootstrapProofLifetime + time.Second).Format(time.RFC3339Nano)
		},
		"wrong endpoint":   func(proof *bootstrapProof) { proof.EndpointFingerprint = "short" },
		"wrong server":     func(proof *bootstrapProof) { proof.ServerFingerprint = "short" },
		"wrong database":   func(proof *bootstrapProof) { proof.MaintenanceDatabase = "" },
		"wrong purpose":    func(proof *bootstrapProof) { proof.Purpose = "foreign-purpose" },
		"wrong state":      func(proof *bootstrapProof) { proof.State = "consumed" },
		"malformed run id": func(proof *bootstrapProof) { proof.RunID = "short" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if _, err := parseBootstrapProof(encode(candidate), now); err == nil {
				t.Fatal("недопустимый bootstrap proof принят")
			}
		})
	}
	if _, err := parseBootstrapProof("{}", now); err == nil {
		t.Fatal("пустой bootstrap proof принят")
	}
}

func TestGeneratedPostgresHarnessOneShotBootstrapProof(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MATTERCODEX_POSTGRES_TEST_BINDIR")) == "" {
		t.Skip("server binaries generated PostgreSQL harness не заданы")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	harness, err := StartGeneratedPostgresHarness(ctx)
	if err != nil {
		t.Fatalf("start generated PostgreSQL harness: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := harness.Close(cleanupCtx); err != nil {
			t.Errorf("close generated PostgreSQL harness: %v", err)
		}
	}()
	pool, err := pgxpool.New(ctx, harness.BootstrapDSN)
	if err != nil {
		t.Fatalf("connect generated PostgreSQL harness: %v", err)
	}
	defer pool.Close()
	databaseCount := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from pg_database`).Scan(&count); err != nil {
			t.Fatalf("count generated PostgreSQL databases: %v", err)
		}
		return count
	}
	assertNoDatabaseEffect := func(label string, action func() error) {
		t.Helper()
		before := databaseCount()
		if err := action(); err == nil {
			t.Fatalf("%s был принят", label)
		}
		if after := databaseCount(); after != before {
			t.Fatalf("%s изменил число databases: before=%d after=%d", label, before, after)
		}
	}
	assertNoDatabaseEffect("локальный live endpoint без proof", func() error {
		_, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, "")
		return err
	})

	var proof bootstrapProof
	if err := json.Unmarshal([]byte(harness.BootstrapProof), &proof); err != nil {
		t.Fatalf("decode generated proof: %v", err)
	}
	encodeProof := func(candidate bootstrapProof) string {
		t.Helper()
		value, err := json.Marshal(candidate)
		if err != nil {
			t.Fatalf("encode generated proof candidate: %v", err)
		}
		return string(value)
	}
	for label, mutate := range map[string]func(*bootstrapProof){
		"endpoint mismatch": func(candidate *bootstrapProof) { candidate.EndpointFingerprint = strings.Repeat("a", 64) },
		"server mismatch":   func(candidate *bootstrapProof) { candidate.ServerFingerprint = strings.Repeat("b", 64) },
		"database mismatch": func(candidate *bootstrapProof) { candidate.MaintenanceDatabase = "template1" },
		"expired proof": func(candidate *bootstrapProof) {
			candidate.IssuedAt = time.Now().UTC().Add(-2 * bootstrapProofLifetime).Format(time.RFC3339Nano)
			candidate.ExpiresAt = time.Now().UTC().Add(-bootstrapProofLifetime).Format(time.RFC3339Nano)
		},
		"future proof": func(candidate *bootstrapProof) {
			candidate.IssuedAt = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
			candidate.ExpiresAt = time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
		},
		"short nonce":       func(candidate *bootstrapProof) { candidate.Nonce = "aa" },
		"malformed purpose": func(candidate *bootstrapProof) { candidate.Purpose = "foreign-purpose" },
	} {
		candidate := proof
		mutate(&candidate)
		assertNoDatabaseEffect(label, func() error {
			_, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, encodeProof(candidate))
			return err
		})
	}
	assertNoDatabaseEffect("malformed proof", func() error {
		_, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, "{")
		return err
	})
	t.Setenv("MATTERCODEX_POSTGRES_DB", "postgres")
	assertNoDatabaseEffect("configured production identity", func() error {
		_, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, harness.BootstrapProof)
		return err
	})
	t.Setenv("MATTERCODEX_POSTGRES_DB", "")
	target, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, harness.BootstrapProof)
	if err != nil {
		t.Fatalf("generated one-shot proof rejected: %v", err)
	}
	assertNoDatabaseEffect("reused proof", func() error {
		_, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, harness.BootstrapProof)
		return err
	})
	if err := DestroyDisposableDatabase(ctx, harness.BootstrapDSN, target); err != nil {
		t.Fatalf("destroy generated proof target: %v", err)
	}

	concurrentProof, err := provisionGeneratedBootstrapProof(
		ctx,
		harness.BootstrapDSN,
		harness.dataDirectory,
		harness.socketDirectory,
	)
	if err != nil {
		t.Fatalf("provision concurrent generated proof: %v", err)
	}
	start := make(chan struct{})
	results := make(chan struct {
		target DisposableDatabase
		err    error
	}, 2)
	for range 2 {
		go func() {
			<-start
			created, createErr := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, concurrentProof)
			results <- struct {
				target DisposableDatabase
				err    error
			}{target: created, err: createErr}
		}()
	}
	close(start)
	winners := make([]DisposableDatabase, 0, 1)
	for range 2 {
		result := <-results
		if result.err == nil {
			winners = append(winners, result.target)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("concurrent proof winners=%d, want 1", len(winners))
	}
	if err := DestroyDisposableDatabase(ctx, harness.BootstrapDSN, winners[0]); err != nil {
		t.Fatalf("destroy concurrent proof target: %v", err)
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
