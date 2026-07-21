package testsupport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDisposableDatabaseOfflineAdmissionMatrixHermetic(t *testing.T) {
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

func TestBootstrapProofOfflineMatrixHermetic(t *testing.T) {
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
