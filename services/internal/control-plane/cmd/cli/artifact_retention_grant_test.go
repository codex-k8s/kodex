package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformpostgres "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

// TestArtifactRetentionGrantCatalogMigration exercises the actual 049 predecessor, denied ART grant,
// atomic scoped upgrade, canonical backup ACL preservation, two live instance grants and replay fences.
// TestArtifactRetentionGrantCatalogMigration проверяет настоящий predecessor049, отказ ART grant,
// атомарный адресный upgrade, сохранение backup ACL, два живых instance grant и ограждения replay.
func TestArtifactRetentionGrantCatalogMigration(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114170000); err != nil {
		t.Fatal("prepare exact grant predecessor", err)
	}
	if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
		t.Fatal("prepare canonical backup authority", err)
	}
	config, err := pgxpool.ParseConfig(observer.Config().ConnString())
	if err != nil {
		t.Fatal("parse disposable runtime configuration")
	}
	config.ConnConfig.User = "control_plane_runtime_g1"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open disposable runtime pool")
	}
	defer pool.Close()
	repository, err := platformpostgres.New(pool, "synthetic", "synthetic", objectstoragetest.New())
	if err != nil {
		t.Fatal("create actual worker owner", err)
	}
	now := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	grant := platformrepo.WorkerGrantInput{WorkloadID: "artifact-retention", InstanceID: uuid.NewString(),
		EnvelopeSHA256: strings.Repeat("a", 64), CredentialGeneration: 1,
		Revision: uint64(now.Unix()), IssuedAt: now, ExpiresAt: now.Add(4 * time.Minute)}
	if err := repository.AcceptWorkerGrant(ctx, grant); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("predecessor must reject ART before registry admission", err)
	}
	// Снимок исключает только два намеренно заменяемых CHECK и единственную новую строку ART.
	snapshot := func() []byte {
		t.Helper()
		var value []byte
		if err := observer.QueryRow(ctx, `SELECT jsonb_build_object(
 'rows',(SELECT jsonb_agg(to_jsonb(r) ORDER BY workload_id) FROM control_plane.trusted_workload_generations r WHERE workload_id <> 'artifact-retention'),
 'floors',(SELECT jsonb_agg(to_jsonb(r) ORDER BY workload_id) FROM control_plane.worker_grant_high_watermarks r WHERE workload_id <> 'artifact-retention'),
 'relations',(SELECT jsonb_agg(jsonb_build_array(oid::text,relowner::text,relacl::text,relrowsecurity,relforcerowsecurity) ORDER BY oid)
 FROM pg_class WHERE oid IN ('control_plane.trusted_workload_generations'::regclass,'control_plane.worker_grant_high_watermarks'::regclass,'control_plane.worker_grant_instance_high_watermarks'::regclass)),
 'policies',(SELECT jsonb_agg(to_jsonb(p) ORDER BY oid) FROM pg_policy p WHERE polrelid IN ('control_plane.trusted_workload_generations'::regclass,'control_plane.worker_grant_high_watermarks'::regclass,'control_plane.worker_grant_instance_high_watermarks'::regclass)),
 'defaults',(SELECT jsonb_agg(to_jsonb(d) ORDER BY oid) FROM pg_default_acl d)
)`).Scan(&value); err != nil {
			t.Fatal("read grant preservation snapshot", err)
		}
		return value
	}
	before := snapshot()
	// Проверяем атомарный отказ неизвестного CHECK, не обходя production predecessor guard.
	if _, err := observer.Exec(ctx, `ALTER TABLE control_plane.trusted_workload_generations DROP CONSTRAINT trusted_workload_generations_workload_id_check;
 ALTER TABLE control_plane.trusted_workload_generations ADD CONSTRAINT trusted_workload_generations_workload_id_check CHECK (workload_id <> 'foreign-workload')`); err != nil {
		t.Fatal("prepare substituted constraint", err)
	}
	if err := applyPostgresMigrations(ctx, database, artifactRetentionGrantCommand); err == nil {
		t.Fatal("substituted predecessor was accepted")
	}
	var version int64
	version, err = goose.GetDBVersionContext(ctx, database)
	if err != nil || version != 20261114170000 {
		t.Fatal("rejected migration changed head", err)
	}
	if _, err := observer.Exec(ctx, `ALTER TABLE control_plane.trusted_workload_generations DROP CONSTRAINT trusted_workload_generations_workload_id_check;
 ALTER TABLE control_plane.trusted_workload_generations ADD CONSTRAINT trusted_workload_generations_workload_id_check CHECK (workload_id IN (
 'automation-scheduler','control-plane','email-bridge','image-admission','image-admission-controller','image-promotion','integration-gateway','interaction-gateway','role-image-builder','runtime-controller','secret-broker','session-archive','stt-tts-service'))`); err != nil {
		t.Fatal("restore exact predecessor constraint", err)
	}
	if err := applyPostgresMigrations(ctx, database, artifactRetentionGrantCommand); err != nil {
		t.Fatal("apply scoped worker registration", err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("registration changed previous rows, ownership, ACL, RLS or backup defaults")
	}
	// Дополнительный exact operator SQL проверяется только в disposable БД;
	// обязательная квалификация выше не зависит от внешнего файла или этого opt-in.
	if path := os.Getenv("KODEX_ARTIFACT_GRANT_OPERATOR_SQL"); path != "" {
		raw, err := os.ReadFile(path)
		digest := sha256.Sum256(raw)
		if err != nil || hex.EncodeToString(digest[:]) != os.Getenv("KODEX_ARTIFACT_GRANT_OPERATOR_SQL_SHA256") {
			t.Fatal("private operator SQL custody rejected")
		}
		var value []byte
		results, err := observer.PgConn().Exec(ctx, string(raw)).ReadAll()
		if err != nil {
			t.Fatal("read exact operator postcondition", err)
		}
		for _, result := range results {
			if result.Err != nil {
				t.Fatal("operator postcondition statement failed", result.Err)
			}
			if len(result.Rows) != 0 {
				if value != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 {
					t.Fatal("operator proof shape rejected")
				}
				value = result.Rows[0][0]
			}
		}
		var proof map[string]any
		if err := json.Unmarshal(value, &proof); err != nil || proof["artifactGrantCatalogMatches"] != true ||
			proof["schemaMatches"] != true || proof["historyMatches"] != true {
			t.Fatalf("operator catalog proof rejected: artifact=%v schema=%v history=%v error=%v", proof["artifactGrantCatalogMatches"], proof["schemaMatches"], proof["historyMatches"], err)
		}
		t.Logf("exact operator catalog proof PASS sql_sha256=%x proof_sha256=%x", digest, sha256.Sum256(value))
	}
	accept := func(input platformrepo.WorkerGrantInput, allowed bool) {
		t.Helper()
		err := repository.AcceptWorkerGrant(ctx, input)
		if allowed && err != nil || !allowed && !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("worker grant admission allowed=%v: %v", allowed, err)
		}
	}
	accept(grant, true)
	accept(grant, true)
	sibling := grant
	sibling.InstanceID = uuid.NewString()
	accept(sibling, true)
	altered := grant
	altered.EnvelopeSHA256 = strings.Repeat("b", 64)
	accept(altered, false)
	advanced := grant
	advanced.CredentialGeneration = 2
	accept(advanced, true)
	accept(sibling, false)
	expired := advanced
	expired.IssuedAt = now.Add(-10 * time.Minute)
	expired.ExpiresAt = now.Add(-6 * time.Minute)
	expired.Revision = uint64(expired.IssuedAt.Unix())
	accept(expired, false)
	unknown := advanced
	unknown.WorkloadID = "foreign-workload"
	accept(unknown, false)
	for _, table := range []string{"trusted_workload_generations", "worker_grant_high_watermarks"} {
		query := "INSERT INTO control_plane." + table + " (workload_id,credential_generation"
		if table == "worker_grant_high_watermarks" {
			query += ",revision,issued_at,expires_at) VALUES ('foreign-workload',1,1,clock_timestamp(),clock_timestamp()+interval '1 minute')"
		} else {
			query += ") VALUES ('foreign-workload',1)"
		}
		_, err := observer.Exec(ctx, query)
		var pgError *pgconn.PgError
		if !errors.As(err, &pgError) || pgError.Code != "23514" {
			t.Fatal("unknown workload escaped closed constraint", err)
		}
	}
	if err := applyPostgresMigrations(ctx, database, artifactRetentionGrantCommand); err != nil {
		t.Fatal("rejoin applied registration", err)
	}
	if generation, err := repository.ResolveServiceCredentialGeneration(ctx, "artifact-retention"); err != nil || generation != 2 {
		t.Fatal("rejoin reset credential floor", err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("runtime or replay changed foreign state or ACL")
	}
	if _, err := observer.Exec(ctx, "UPDATE control_plane.trusted_workload_generations SET enabled=false WHERE workload_id='artifact-retention'"); err != nil {
		t.Fatal("disable exact synthetic registration", err)
	}
	accept(advanced, false)
	if err := applyPostgresMigrations(ctx, database, artifactRetentionGrantCommand); err == nil {
		t.Fatal("rejoin silently enabled revoked registration")
	}
}
