package testsupport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	assertConsumedReservedLedger := func(target DisposableDatabase) {
		t.Helper()
		var state string
		var databaseOID int64
		var consumed bool
		if err := pool.QueryRow(ctx, `
select target_state, coalesce(target_database_oid::bigint, 0), consumed_at is not null
from public.mattercodex_test_bootstrap_proofs
where target_database = $1
`, target.Database).Scan(&state, &databaseOID, &consumed); err != nil {
			t.Fatalf("read consumed reserved ledger: %v", err)
		}
		if state != bootstrapTargetStateReserved || databaseOID != 0 || !consumed {
			t.Fatalf("ledger state=%q oid=%d consumed=%t", state, databaseOID, consumed)
		}
	}
	assertConsumedCreatedLedger := func(target DisposableDatabase, expectedOID int64) {
		t.Helper()
		var state string
		var databaseOID int64
		var consumed bool
		if err := pool.QueryRow(ctx, `
select target_state, coalesce(target_database_oid::bigint, 0), consumed_at is not null
from public.mattercodex_test_bootstrap_proofs
where target_database = $1
`, target.Database).Scan(&state, &databaseOID, &consumed); err != nil {
			t.Fatalf("read consumed created ledger: %v", err)
		}
		if state != bootstrapTargetStateCreated || databaseOID != expectedOID || !consumed {
			t.Fatalf("ledger state=%q oid=%d consumed=%t", state, databaseOID, consumed)
		}
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

	t.Run("post-create fault matrix", func(t *testing.T) {
		cases := []struct {
			name         string
			point        bootstrapLifecycleHookPoint
			cancelCause  bool
			bootstrapDSN string
			preserved    bool
		}{
			{name: "неоднозначный CREATE", point: bootstrapHookAfterCreateExec, preserved: true},
			{name: "после зафиксированного CREATE", point: bootstrapHookAfterCreateIdentified, preserved: true},
			{name: "COMMENT не применён", point: bootstrapHookBeforeComment, preserved: true},
			{name: "COMMENT применён с неоднозначным ответом", point: bootstrapHookAfterCommentExec, preserved: true},
			{name: "ошибка deriveDSN", point: bootstrapHookBeforeDeriveDSN},
			{name: "ошибка перед финальной Validate", point: bootstrapHookBeforeFinalValidate},
			{name: "ошибка после финальной Validate", point: bootstrapHookAfterFinalValidate},
			{name: "отмена caller во время финальной Validate", point: bootstrapHookBeforeFinalValidate, cancelCause: true},
			{name: "отмена caller после CREATE", point: bootstrapHookAfterCreateIdentified, cancelCause: true, preserved: true},
			{name: "loopback external-style endpoint", point: bootstrapHookBeforeDeriveDSN, bootstrapDSN: harness.LoopbackBootstrapDSN},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				bootstrapDSN := testCase.bootstrapDSN
				if bootstrapDSN == "" {
					bootstrapDSN = harness.BootstrapDSN
				}
				proof, err := provisionGeneratedBootstrapProof(
					ctx, bootstrapDSN, harness.dataDirectory, harness.socketDirectory,
				)
				if err != nil {
					t.Fatalf("provision fault proof: %v", err)
				}
				before := databaseCount()
				var cancelCase context.CancelFunc
				var dropCalls atomic.Int32
				options := bootstrapLifecycleOptions{
					cleanupTimeout: 10 * time.Second,
					attemptTimeout: 3 * time.Second,
					retryDelay:     10 * time.Millisecond,
					hook: func(_ context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
						if point == bootstrapHookBeforeDrop {
							dropCalls.Add(1)
						}
						if point != testCase.point {
							return nil
						}
						if testCase.cancelCause {
							cancelCase()
							return nil
						}
						return fmt.Errorf("синтетический отказ lifecycle")
					},
				}
				caseCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
				caseCtx, cancelCase = context.WithTimeout(caseCtx, 30*time.Second)
				target, bootstrapErr := BootstrapDisposableDatabase(caseCtx, bootstrapDSN, proof)
				cancelCase()
				if bootstrapErr == nil {
					t.Fatal("синтетический post-CREATE отказ не возвращён")
				}
				if target.Database == "" || target.Marker == "" {
					t.Fatal("post-CREATE ошибка скрыла exact target handle")
				}
				if !strings.Contains(bootstrapErr.Error(), "компенсирующ") {
					t.Fatalf("ошибка не содержит итог компенсирующей очистки: %v", bootstrapErr)
				}
				if strings.Contains(bootstrapErr.Error(), target.Marker) || strings.Contains(bootstrapErr.Error(), proof) {
					t.Fatal("ошибка раскрыла marker или proof")
				}
				after := databaseCount()
				if testCase.preserved {
					if after != before+1 || dropCalls.Load() != 0 {
						t.Fatalf("pre-marker отказ не сохранил target без DROP: before=%d after=%d drops=%d", before, after, dropCalls.Load())
					}
					snapshot, snapshotErr := readBootstrapTargetSnapshot(ctx, pool, target.Database)
					if snapshotErr != nil || !snapshot.exists {
						t.Fatalf("pre-marker target не сохранён: %v", snapshotErr)
					}
					if testCase.point == bootstrapHookAfterCreateExec {
						assertConsumedReservedLedger(target)
					} else {
						assertConsumedCreatedLedger(target, snapshot.databaseOID)
					}
					if _, cleanupErr := pool.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); cleanupErr != nil {
						t.Fatalf("controller cleanup preserved target: %v", cleanupErr)
					}
				} else if after != before || dropCalls.Load() == 0 {
					t.Fatalf("post-marker generated cleanup: before=%d after=%d drops=%d", before, after, dropCalls.Load())
				}
				if _, reuseErr := BootstrapDisposableDatabase(ctx, bootstrapDSN, proof); reuseErr == nil {
					t.Fatal("proof повторно принят после компенсирующей очистки")
				}
			})
		}
	})

	t.Run("transient cleanup reconnect retry", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision retry proof: %v", err)
		}
		before := databaseCount()
		var cleanupAttempts atomic.Int32
		options := bootstrapLifecycleOptions{
			cleanupTimeout: 10 * time.Second,
			attemptTimeout: 3 * time.Second,
			retryDelay:     10 * time.Millisecond,
			attempts:       4,
			hook: func(_ context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
				switch point {
				case bootstrapHookBeforeDeriveDSN:
					return fmt.Errorf("синтетический отказ после exact marker")
				case bootstrapHookBeforeCleanupAttempt:
					if cleanupAttempts.Add(1) <= 2 {
						return fmt.Errorf("синтетическая потеря cleanup connection")
					}
				}
				return nil
			},
		}
		retryCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
		if _, err := BootstrapDisposableDatabase(retryCtx, harness.BootstrapDSN, proof); err == nil {
			t.Fatal("bootstrap без COMMENT неожиданно успешен")
		} else if strings.Contains(err.Error(), "не подтверждена") {
			t.Fatalf("cleanup не восстановился после transient отказов: %v", err)
		}
		if cleanupAttempts.Load() != 3 {
			t.Fatalf("cleanup attempts=%d, want 3", cleanupAttempts.Load())
		}
		if after := databaseCount(); after != before {
			t.Fatalf("retry cleanup оставил database: before=%d after=%d", before, after)
		}
	})

	t.Run("ambiguous drop reconciliation", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision DROP proof: %v", err)
		}
		target, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof)
		if err != nil {
			t.Fatalf("bootstrap DROP target: %v", err)
		}
		var ambiguous atomic.Bool
		options := bootstrapLifecycleOptions{
			cleanupTimeout: 10 * time.Second,
			attemptTimeout: 3 * time.Second,
			retryDelay:     10 * time.Millisecond,
			hook: func(_ context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
				if point == bootstrapHookAfterDropExec && ambiguous.CompareAndSwap(false, true) {
					return fmt.Errorf("синтетическая потеря ответа DROP")
				}
				return nil
			},
		}
		destroyCtx := context.WithValue(ctx, bootstrapLifecycleOptionsContextKey{}, options)
		if err := DestroyDisposableDatabase(destroyCtx, harness.BootstrapDSN, target); err != nil {
			t.Fatalf("ambiguous DROP не reconciled: %v", err)
		}
		if !ambiguous.Load() {
			t.Fatal("fault после DROP не был вызван")
		}
	})

	t.Run("fail-closed identity matrix", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision identity proof: %v", err)
		}
		target, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof)
		if err != nil {
			t.Fatalf("bootstrap identity target: %v", err)
		}
		marker, err := parseDisposableMarker(target.Marker, time.Now().UTC())
		if err != nil {
			t.Fatalf("parse target marker: %v", err)
		}
		original, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !original.exists {
			t.Fatalf("read original target: %v", err)
		}
		assertDeniedAndPreserved := func(label string, bootstrapDSN string, candidate DisposableDatabase) {
			t.Helper()
			destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer destroyCancel()
			destroyErr := DestroyDisposableDatabase(destroyCtx, bootstrapDSN, candidate)
			if destroyErr == nil {
				t.Fatalf("%s: недоказанный DROP разрешён", label)
			}
			if strings.Contains(destroyErr.Error(), target.Marker) || strings.Contains(destroyErr.Error(), proof) {
				t.Fatalf("%s: ошибка раскрыла marker или proof", label)
			}
			actual, snapshotErr := readBootstrapTargetSnapshot(ctx, pool, target.Database)
			if snapshotErr != nil || !actual.exists || actual.databaseOID != original.databaseOID {
				t.Fatalf("%s: original target удалён или заменён: exists=%t same_oid=%t err=%v",
					label, actual.exists, actual.databaseOID == original.databaseOID, snapshotErr)
			}
		}

		wrongName := target
		wrongName.Database = disposableDatabasePrefix + strings.Repeat("f", 24)
		assertDeniedAndPreserved("wrong name", harness.BootstrapDSN, wrongName)
		wrongMarker := target
		wrongMarker.Marker = target.Marker + "00"
		assertDeniedAndPreserved("wrong proof marker", harness.BootstrapDSN, wrongMarker)
		wrongTargetDSN := target
		wrongTargetDSN.DSN = harness.BootstrapDSN
		assertDeniedAndPreserved("wrong target DSN", harness.BootstrapDSN, wrongTargetDSN)
		assertDeniedAndPreserved("wrong maintenance database", target.DSN, target)

		identifier := pgx.Identifier{target.Database}.Sanitize()
		comment := strings.ReplaceAll(target.Marker, "'", "''")
		if _, err := pool.Exec(ctx, "comment on database "+identifier+" is null"); err != nil {
			t.Fatalf("remove marker for negative test: %v", err)
		}
		assertDeniedAndPreserved("missing applied marker", harness.BootstrapDSN, target)
		if _, err := pool.Exec(ctx, "comment on database "+identifier+" is 'foreign-marker'"); err != nil {
			t.Fatalf("set foreign marker: %v", err)
		}
		assertDeniedAndPreserved("foreign marker", harness.BootstrapDSN, target)
		if _, err := pool.Exec(ctx, "comment on database "+identifier+" is '"+comment+"'"); err != nil {
			t.Fatalf("restore marker: %v", err)
		}

		foreignOwner := "mc_test_foreign_owner"
		foreignOwnerIdentifier := pgx.Identifier{foreignOwner}.Sanitize()
		if _, err := pool.Exec(ctx, "create role "+foreignOwnerIdentifier+" nologin"); err != nil {
			t.Fatalf("create foreign owner: %v", err)
		}
		if _, err := pool.Exec(ctx, "alter database "+identifier+" owner to "+foreignOwnerIdentifier); err != nil {
			t.Fatalf("replace target owner: %v", err)
		}
		assertDeniedAndPreserved("wrong owner", harness.BootstrapDSN, target)
		if _, err := pool.Exec(ctx, "alter database "+identifier+" owner to "+pgx.Identifier{generatedPostgresOwner}.Sanitize()); err != nil {
			t.Fatalf("restore target owner: %v", err)
		}
		if _, err := pool.Exec(ctx, "drop role "+foreignOwnerIdentifier); err != nil {
			t.Fatalf("drop foreign owner: %v", err)
		}

		table := pgx.Identifier{bootstrapProofTable}.Sanitize()
		trigger := pgx.Identifier{bootstrapProofGuardTrigger}.Sanitize()
		if _, err := pool.Exec(ctx, "update public."+table+" set purpose='foreign-purpose' where run_id=$1", marker.runID); err == nil {
			t.Fatal("immutable creation ledger разрешил изменение consumed proof")
		}
		mutateLedger := func(query string, arguments ...any) error {
			if _, err := pool.Exec(ctx, "alter table public."+table+" disable trigger "+trigger); err != nil {
				return err
			}
			_, mutationErr := pool.Exec(ctx, query, arguments...)
			_, enableErr := pool.Exec(ctx, "alter table public."+table+" enable trigger "+trigger)
			if mutationErr != nil {
				return mutationErr
			}
			return enableErr
		}
		if err := mutateLedger("update public."+table+" set target_database_oid=$2::oid where run_id=$1", marker.runID, original.databaseOID+1); err != nil {
			t.Fatalf("mutate ledger OID: %v", err)
		}
		assertDeniedAndPreserved("wrong ledger OID", harness.BootstrapDSN, target)
		if err := mutateLedger("update public."+table+" set target_database_oid=$2::oid where run_id=$1", marker.runID, original.databaseOID); err != nil {
			t.Fatalf("restore ledger OID: %v", err)
		}
		if err := mutateLedger("update public."+table+" set target_state=$2, target_dropped_at=clock_timestamp() where run_id=$1", marker.runID, bootstrapTargetStateDropped); err != nil {
			t.Fatalf("mutate ledger state: %v", err)
		}
		assertDeniedAndPreserved("wrong registry state", harness.BootstrapDSN, target)
		if err := mutateLedger("update public."+table+" set target_state=$2, target_dropped_at=null where run_id=$1", marker.runID, bootstrapTargetStateMarked); err != nil {
			t.Fatalf("restore ledger state: %v", err)
		}
		if err := mutateLedger("update public."+table+" set server_fingerprint=$2 where run_id=$1", marker.runID, strings.Repeat("a", 64)); err != nil {
			t.Fatalf("mutate ledger server: %v", err)
		}
		assertDeniedAndPreserved("wrong registry server", harness.BootstrapDSN, target)
		if err := mutateLedger("update public."+table+" set server_fingerprint=$2 where run_id=$1", marker.runID, marker.serverFingerprint); err != nil {
			t.Fatalf("restore ledger server: %v", err)
		}
		if err := mutateLedger("update public."+table+" set purpose='foreign-purpose' where run_id=$1", marker.runID); err != nil {
			t.Fatalf("mutate ledger purpose: %v", err)
		}
		assertDeniedAndPreserved("wrong consumed proof purpose", harness.BootstrapDSN, target)
		if err := mutateLedger("update public."+table+" set purpose=$2 where run_id=$1", marker.runID, bootstrapProofPurpose); err != nil {
			t.Fatalf("restore ledger purpose: %v", err)
		}
		if err := mutateLedger("update public."+table+" set target_database=$2 where run_id=$1", marker.runID, disposableDatabasePrefix+strings.Repeat("e", 24)); err != nil {
			t.Fatalf("mutate ledger target: %v", err)
		}
		assertDeniedAndPreserved("wrong registry target", harness.BootstrapDSN, target)
		if err := mutateLedger("update public."+table+" set target_database=$2 where run_id=$1", marker.runID, target.Database); err != nil {
			t.Fatalf("restore ledger target: %v", err)
		}
		if err := mutateLedger("update public."+table+" set target_marker_sha256=decode(repeat('00', 32), 'hex') where run_id=$1", marker.runID); err != nil {
			t.Fatalf("mutate ledger marker hash: %v", err)
		}
		assertDeniedAndPreserved("wrong registry marker hash", harness.BootstrapDSN, target)
		markerDigest := sha256.Sum256([]byte(target.Marker))
		if err := mutateLedger("update public."+table+" set target_marker_sha256=$2 where run_id=$1", marker.runID, markerDigest[:]); err != nil {
			t.Fatalf("restore ledger marker hash: %v", err)
		}

		if err := DestroyDisposableDatabase(ctx, harness.BootstrapDSN, target); err != nil {
			t.Fatalf("destroy target after identity matrix: %v", err)
		}
	})

	t.Run("bounded cleanup deadline", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision deadline proof: %v", err)
		}
		options := bootstrapLifecycleOptions{
			cleanupTimeout: 120 * time.Millisecond,
			attemptTimeout: 40 * time.Millisecond,
			retryDelay:     5 * time.Millisecond,
			attempts:       8,
			hook: func(hookCtx context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
				if point == bootstrapHookBeforeComment {
					return fmt.Errorf("синтетический отказ до COMMENT")
				}
				if point == bootstrapHookBeforeCleanupAttempt {
					<-hookCtx.Done()
					return hookCtx.Err()
				}
				return nil
			},
		}
		deadlineCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
		started := time.Now()
		target, bootstrapErr := BootstrapDisposableDatabase(deadlineCtx, harness.BootstrapDSN, proof)
		if bootstrapErr == nil || !strings.Contains(bootstrapErr.Error(), "не подтверждена") {
			t.Fatalf("deadline cleanup не вернул явную итоговую ошибку: %v", bootstrapErr)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("deadline cleanup не ограничен: %s", elapsed)
		}
		snapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !snapshot.exists {
			t.Fatalf("deadline cleanup не сохранил pre-marker target: %v", err)
		}
		assertConsumedCreatedLedger(target, snapshot.databaseOID)
		if _, err := pool.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
			t.Fatalf("controller cleanup deadline target: %v", err)
		}
	})

	t.Run("definite create collisions preserve foreign database", func(t *testing.T) {
		cases := []struct {
			name             string
			expectedSQLState string
			testDatabaseOID  int64
		}{
			{name: "42P04 name collision", expectedSQLState: "42P04", testDatabaseOID: 2_000_000_001},
			{name: "23505 unique collision", expectedSQLState: "23505", testDatabaseOID: 2_000_000_002},
		}
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
				if err != nil {
					t.Fatalf("provision collision proof: %v", err)
				}
				before := databaseCount()
				foreignDatabase := ""
				var dropCalls atomic.Int32
				var createDatabaseError error
				if testCase.expectedSQLState == "23505" {
					connection, acquireErr := pool.Acquire(ctx)
					if acquireErr != nil {
						t.Fatalf("acquire real 23505 connection: %v", acquireErr)
					}
					table := pgx.Identifier{uniqueName("mc_unique_collision", 48)}.Sanitize()
					if _, err := connection.Exec(ctx, "create temporary table "+table+"(id integer primary key)"); err != nil {
						connection.Release()
						t.Fatalf("prepare real 23505 constraint: %v", err)
					}
					_, createDatabaseError = connection.Exec(ctx, "insert into "+table+" values (1), (1)")
					connection.Release()
					var postgresErr *pgconn.PgError
					if !errors.As(createDatabaseError, &postgresErr) || postgresErr.Code != "23505" {
						t.Fatalf("real PostgreSQL не вернул 23505: %v", createDatabaseError)
					}
				}
				options := bootstrapLifecycleOptions{
					createDatabaseOIDForTest:   testCase.testDatabaseOID,
					createDatabaseErrorForTest: createDatabaseError,
					cleanupTimeout:             10 * time.Second,
					attemptTimeout:             3 * time.Second,
					retryDelay:                 10 * time.Millisecond,
					hook: func(hookCtx context.Context, point bootstrapLifecycleHookPoint, input bootstrapLifecycleHookInput) error {
						if point == bootstrapHookBeforeDrop {
							dropCalls.Add(1)
						}
						if point != bootstrapHookBeforeCreateExec {
							return nil
						}
						foreignDatabase = input.target.Database
						identifier := pgx.Identifier{foreignDatabase}.Sanitize()
						owner := pgx.Identifier{generatedPostgresOwner}.Sanitize()
						_, createErr := pool.Exec(
							hookCtx,
							"create database "+identifier+" with template template0 owner "+owner+" oid "+fmt.Sprint(input.databaseOID),
						)
						return createErr
					},
				}
				collisionCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
				target, bootstrapErr := BootstrapDisposableDatabase(collisionCtx, harness.BootstrapDSN, proof)
				var collisionErr bootstrapCreateCollisionError
				if !errors.As(bootstrapErr, &collisionErr) || collisionErr.sqlState != testCase.expectedSQLState ||
					!strings.Contains(bootstrapErr.Error(), "объекта-сироты") {
					t.Fatalf("однозначная коллизия не диагностирована: %v", bootstrapErr)
				}
				foreignSnapshot, err := readBootstrapTargetSnapshot(ctx, pool, foreignDatabase)
				if err != nil || !foreignSnapshot.exists || foreignSnapshot.comment != "" || foreignSnapshot.databaseOID != testCase.testDatabaseOID {
					t.Fatalf("foreign collision database не сохранена: snapshot=%#v error=%v", foreignSnapshot, err)
				}
				targetSnapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
				if err != nil || !targetSnapshot.exists || dropCalls.Load() != 0 {
					t.Fatalf("collision target state unexpected: snapshot=%#v drops=%d error=%v", targetSnapshot, dropCalls.Load(), err)
				}
				assertConsumedReservedLedger(target)
				if _, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof); err == nil {
					t.Fatal("proof definite collision повторно принят")
				}
				if _, err := pool.Exec(ctx, "drop database "+pgx.Identifier{foreignDatabase}.Sanitize()+" with (force)"); err != nil {
					t.Fatalf("controller cleanup collision database: %v", err)
				}
				if after := databaseCount(); after != before {
					t.Fatalf("collision test cleanup изменил baseline: before=%d after=%d", before, after)
				}
			})
		}
	})

	t.Run("post-create pre-ledger replacement survives", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision pre-ledger replacement proof: %v", err)
		}
		before := databaseCount()
		var originalOID int64
		var dropCalls atomic.Int32
		options := bootstrapLifecycleOptions{
			cleanupTimeout: 10 * time.Second,
			attemptTimeout: 3 * time.Second,
			retryDelay:     10 * time.Millisecond,
			hook: func(hookCtx context.Context, point bootstrapLifecycleHookPoint, input bootstrapLifecycleHookInput) error {
				if point == bootstrapHookBeforeDrop {
					dropCalls.Add(1)
				}
				if point != bootstrapHookAfterCreateExec {
					return nil
				}
				original, readErr := readBootstrapTargetSnapshot(hookCtx, pool, input.target.Database)
				if readErr != nil || !original.exists {
					return fmt.Errorf("read original pre-ledger target: %w", readErr)
				}
				originalOID = original.databaseOID
				identifier := pgx.Identifier{input.target.Database}.Sanitize()
				if _, dropErr := pool.Exec(hookCtx, "drop database "+identifier+" with (force)"); dropErr != nil {
					return dropErr
				}
				owner := pgx.Identifier{generatedPostgresOwner}.Sanitize()
				if _, createErr := pool.Exec(
					hookCtx,
					"create database "+identifier+" with template template0 owner "+owner+" oid "+fmt.Sprint(originalOID),
				); createErr != nil {
					return createErr
				}
				return fmt.Errorf("синтетическая pre-ledger подмена target")
			},
		}
		replacementCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
		target, bootstrapErr := BootstrapDisposableDatabase(replacementCtx, harness.BootstrapDSN, proof)
		if bootstrapErr == nil || !strings.Contains(bootstrapErr.Error(), "коллиз") || !strings.Contains(bootstrapErr.Error(), "не подтверждена") {
			t.Fatalf("pre-ledger replacement не диагностирован: %v", bootstrapErr)
		}
		snapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !snapshot.exists || snapshot.databaseOID != originalOID || snapshot.comment != "" || dropCalls.Load() != 0 {
			t.Fatalf("pre-ledger replacement не сохранён: snapshot=%#v original=%d error=%v", snapshot, originalOID, err)
		}
		assertConsumedReservedLedger(target)
		if _, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof); err == nil {
			t.Fatal("proof pre-ledger replacement повторно принят")
		}
		if _, err := pool.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
			t.Fatalf("test cleanup pre-ledger replacement: %v", err)
		}
		if after := databaseCount(); after != before {
			t.Fatalf("pre-ledger replacement cleanup изменил baseline: before=%d after=%d", before, after)
		}
	})

	t.Run("adversarial replacement survives", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision replacement proof: %v", err)
		}
		before := databaseCount()
		var originalOID int64
		var dropCalls atomic.Int32
		options := bootstrapLifecycleOptions{
			cleanupTimeout: 10 * time.Second,
			attemptTimeout: 3 * time.Second,
			retryDelay:     10 * time.Millisecond,
			hook: func(hookCtx context.Context, point bootstrapLifecycleHookPoint, input bootstrapLifecycleHookInput) error {
				if point == bootstrapHookBeforeDrop {
					dropCalls.Add(1)
				}
				if point != bootstrapHookAfterCreateIdentified {
					return nil
				}
				originalOID = input.databaseOID
				identifier := pgx.Identifier{input.target.Database}.Sanitize()
				if _, err := pool.Exec(hookCtx, "drop database "+identifier+" with (force)"); err != nil {
					return err
				}
				owner := pgx.Identifier{generatedPostgresOwner}.Sanitize()
				if _, err := pool.Exec(
					hookCtx,
					"create database "+identifier+" with template template0 owner "+owner+" oid "+fmt.Sprint(originalOID),
				); err != nil {
					return err
				}
				return fmt.Errorf("синтетическая подмена target")
			},
		}
		replacementCtx := context.WithValue(context.Background(), bootstrapLifecycleOptionsContextKey{}, options)
		target, bootstrapErr := BootstrapDisposableDatabase(replacementCtx, harness.BootstrapDSN, proof)
		if bootstrapErr == nil || !strings.Contains(bootstrapErr.Error(), "не подтверждена") {
			t.Fatalf("replacement не вызвал fail-closed cleanup: %v", bootstrapErr)
		}
		snapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !snapshot.exists || snapshot.databaseOID != originalOID || snapshot.comment != "" || dropCalls.Load() != 0 {
			t.Fatalf("replacement не сохранён: exists=%t oid=%d original=%d err=%v", snapshot.exists, snapshot.databaseOID, originalOID, err)
		}
		assertConsumedCreatedLedger(target, originalOID)
		if _, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof); err == nil {
			t.Fatal("proof replacement-сценария повторно принят")
		}
		if _, err := pool.Exec(ctx, "drop database "+pgx.Identifier{target.Database}.Sanitize()+" with (force)"); err != nil {
			t.Fatalf("test cleanup replacement: %v", err)
		}
		if after := databaseCount(); after != before {
			t.Fatalf("replacement test cleanup изменил baseline: before=%d after=%d", before, after)
		}
	})

	t.Run("external endpoint hands cleanup to owner without DROP", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision external-style proof: %v", err)
		}
		authorityValue, loaded := generatedPostgresAuthorities.LoadAndDelete(harness.authorityKey)
		if !loaded {
			t.Fatal("generated authority отсутствует до external-style проверки")
		}
		authorityRestored := false
		defer func() {
			if !authorityRestored {
				generatedPostgresAuthorities.Store(harness.authorityKey, authorityValue)
			}
		}()
		target, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof)
		if err != nil {
			t.Fatalf("bootstrap external-style target: %v", err)
		}
		var dropCalls atomic.Int32
		destroyOptions := bootstrapLifecycleOptions{
			hook: func(_ context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
				if point == bootstrapHookBeforeDrop {
					dropCalls.Add(1)
				}
				return nil
			},
		}
		destroyCtx := context.WithValue(ctx, bootstrapLifecycleOptionsContextKey{}, destroyOptions)
		destroyErr := DestroyDisposableDatabase(destroyCtx, harness.BootstrapDSN, target)
		var handoffErr ExternalCleanupRequiredError
		if !errors.As(destroyErr, &handoffErr) || handoffErr.Database != target.Database || dropCalls.Load() != 0 {
			t.Fatalf("external cleanup handoff не выполнен безопасно: error=%v drops=%d", destroyErr, dropCalls.Load())
		}
		snapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !snapshot.exists || snapshot.comment != target.Marker {
			t.Fatalf("external target не сохранён для владельца endpoint: snapshot=%#v error=%v", snapshot, err)
		}
		generatedPostgresAuthorities.Store(harness.authorityKey, authorityValue)
		authorityRestored = true
		if err := DestroyDisposableDatabase(ctx, harness.BootstrapDSN, target); err != nil {
			t.Fatalf("generated owner cleanup external-style target: %v", err)
		}
	})

	t.Run("external compensation hands marked target to owner without DROP", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision external compensation proof: %v", err)
		}
		authorityValue, loaded := generatedPostgresAuthorities.LoadAndDelete(harness.authorityKey)
		if !loaded {
			t.Fatal("generated authority отсутствует до external compensation проверки")
		}
		authorityRestored := false
		defer func() {
			if !authorityRestored {
				generatedPostgresAuthorities.Store(harness.authorityKey, authorityValue)
			}
		}()
		var dropCalls atomic.Int32
		options := bootstrapLifecycleOptions{
			hook: func(_ context.Context, point bootstrapLifecycleHookPoint, _ bootstrapLifecycleHookInput) error {
				if point == bootstrapHookBeforeDrop {
					dropCalls.Add(1)
				}
				if point == bootstrapHookBeforeDeriveDSN {
					return fmt.Errorf("синтетический отказ external lifecycle после exact marker")
				}
				return nil
			},
		}
		bootstrapCtx := context.WithValue(ctx, bootstrapLifecycleOptionsContextKey{}, options)
		target, bootstrapErr := BootstrapDisposableDatabase(bootstrapCtx, harness.BootstrapDSN, proof)
		var handoffErr ExternalCleanupRequiredError
		if !errors.As(bootstrapErr, &handoffErr) || handoffErr.Database != target.Database || dropCalls.Load() != 0 {
			t.Fatalf("external compensation не передала cleanup владельцу: error=%v drops=%d", bootstrapErr, dropCalls.Load())
		}
		snapshot, err := readBootstrapTargetSnapshot(ctx, pool, target.Database)
		if err != nil || !snapshot.exists || snapshot.comment != target.Marker {
			t.Fatalf("external compensation скрыла сохранённый target: snapshot=%#v error=%v", snapshot, err)
		}
		if _, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof); err == nil {
			t.Fatal("external compensation proof повторно принят")
		}
		generatedPostgresAuthorities.Store(harness.authorityKey, authorityValue)
		authorityRestored = true
		if err := DestroyDisposableDatabase(ctx, harness.BootstrapDSN, target); err != nil {
			t.Fatalf("generated owner cleanup compensated target: %v", err)
		}
	})

	t.Run("concurrent cleanup", func(t *testing.T) {
		proof, err := provisionGeneratedBootstrapProof(ctx, harness.BootstrapDSN, harness.dataDirectory, harness.socketDirectory)
		if err != nil {
			t.Fatalf("provision concurrent cleanup proof: %v", err)
		}
		target, err := BootstrapDisposableDatabase(ctx, harness.BootstrapDSN, proof)
		if err != nil {
			t.Fatalf("bootstrap concurrent cleanup target: %v", err)
		}
		start := make(chan struct{})
		errorsChannel := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				errorsChannel <- DestroyDisposableDatabase(ctx, harness.BootstrapDSN, target)
			}()
		}
		close(start)
		for range 2 {
			if err := <-errorsChannel; err != nil {
				t.Fatalf("concurrent cleanup: %v", err)
			}
		}
	})
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
