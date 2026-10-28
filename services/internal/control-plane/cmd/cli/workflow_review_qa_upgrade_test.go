package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// TestWorkflowReviewQAEvidenceForwardUpgrade runs shipped pre/post queries against an actual
// empty disposable013 database, applies014 forward, and proves a repeated up preserves its proof.
// It never rewrites a proof database name or supplies fabricated business rows to pass admission.
// TestWorkflowReviewQAEvidenceForwardUpgrade выполняет поставляемые pre/post queries на настоящей
// пустой disposable013, применяет014 и проверяет сохранение proof при повторном up. Имя БД в proof
// не подменяется; готовые business rows ради прохождения допуска не вставляются.
func TestWorkflowReviewQAEvidenceForwardUpgrade(t *testing.T) {
	dsn := os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DSN")
	if dsn == "" {
		t.Skip("dedicated disposable review QA upgrade database is required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil || config.Host != "127.0.0.1" || config.Port < 1024 ||
		config.Database != "control_plane" || config.User != "control_plane_migrator" ||
		config.Password != "" || config.TLSConfig != nil || len(config.Fallbacks) != 0 ||
		os.Getenv("KODEX_WORKFLOW_REVIEW_QA_UPGRADE_DISPOSABLE") != "dedicated-loopback-container" {
		t.Fatal("invalid disposable review QA upgrade configuration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
	defer cancel()
	database := stdlib.OpenDB(*config)
	database.SetMaxOpenConns(1)
	defer database.Close()
	var bootstrapValid bool
	if err := database.QueryRowContext(ctx, schedulerUpgradeMetadataFixture).Scan(&bootstrapValid); err != nil || !bootstrapValid {
		t.Fatal("review QA upgrade requires empty canonical bootstrap metadata")
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal("review QA upgrade dialect configuration failed")
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261026001300); err != nil {
		t.Fatal("review QA predecessor migration failed", err)
	}
	// The observer matches the operator's local postgres identity only in this own trust fixture.
	// Observer повторяет локальную postgres identity оператора только в своей trust fixture.
	observerConfig := config.Copy()
	observerConfig.User = "postgres"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	if err != nil {
		t.Fatal("connect disposable review QA observer")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = observer.Close(cleanup)
	}()
	readWorkflowReviewQAUpgradeProof(t, ctx, observer, "precondition")
	var firstPost []byte
	for attempt := 0; attempt < 2; attempt++ {
		if err := goose.UpToContext(ctx, database, "migrations", 20261026001400); err != nil {
			t.Fatal("review QA forward migration failed", err)
		}
		post := readWorkflowReviewQAUpgradeProof(t, ctx, observer, "postcondition")
		if attempt == 0 {
			firstPost = post
		} else if !bytes.Equal(firstPost, post) {
			t.Fatal("review QA proof changed after no-op upgrade")
		}
	}
}

// readWorkflowReviewQAUpgradeProof executes the entire immutable READ ONLY statement and passes
// its single unmodified JSON row to the same exported verifier used by the release operator.
// readWorkflowReviewQAUpgradeProof выполняет целый immutable READ ONLY statement и передаёт
// единственную неизменённую JSON row тому же exported verifier, который использует release operator.
func readWorkflowReviewQAUpgradeProof(t *testing.T, ctx context.Context, observer *pgx.Conn, phase string) []byte {
	t.Helper()
	if phase != "precondition" && phase != "postcondition" {
		t.Fatal("invalid review QA proof phase")
	}
	release, err := filepath.Abs("../../../../../tools/release")
	if err != nil {
		t.Fatal("resolve shipped review QA proof directory")
	}
	source, err := os.ReadFile(filepath.Join(release, "workflow-review-qa-evidence-migration-"+phase+".sql"))
	if err != nil {
		t.Fatal("read shipped review QA upgrade proof")
	}
	results, err := observer.PgConn().Exec(ctx, string(source)).ReadAll()
	if err != nil {
		t.Fatal("execute shipped review QA upgrade proof")
	}
	var proof []byte
	for _, result := range results {
		if result.Err != nil {
			t.Fatal("review QA upgrade proof statement failed", result.Err)
		}
		if len(result.Rows) == 0 {
			continue
		}
		if proof != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || len(result.Rows[0][0]) > 1<<20 {
			t.Fatal("review QA upgrade proof shape is invalid")
		}
		proof = result.Rows[0][0]
	}
	if !json.Valid(proof) {
		t.Fatal("review QA upgrade proof JSON is invalid")
	}
	verifier := "validWorkflowReviewQAEvidenceMigrationPostcondition"
	if phase == "precondition" {
		verifier = "validWorkflowReviewQAEvidenceMigrationPrecondition"
	}
	check := exec.CommandContext(ctx, "node", "--input-type=module", "-e", `
import {pathToFileURL} from 'node:url';
const module = await import(pathToFileURL(process.argv[1]));
let raw = ''; for await (const chunk of process.stdin) raw += chunk;
const proof = JSON.parse(raw);
if (!module[process.argv[2]](proof)) process.exit(1);
if (process.argv[2].endsWith('Precondition')) {
  const fields = ['activeRuntimeRuns','activeRunNodes','activeSessionTurns','claimedRuntimeLeases',
    'openBuilds','pendingAdmissions','pendingPromotions','activeArchiveTasks','activeModelCatalogTasks','activeRenewalTasks','oldClaimSessions'];
  if (!fields.every(key => proof[key] === 0)) process.exit(2);
}
`, filepath.Join(release, "control-plane-immutable-migration.mjs"), verifier)
	check.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	check.Stdin = bytes.NewReader(proof)
	if err := check.Run(); err != nil {
		var fields map[string]any
		_ = json.Unmarshal(proof, &fields)
		var failed []string
		for key, value := range fields {
			if flag, boolean := value.(bool); boolean && !flag {
				failed = append(failed, key)
			}
		}
		slices.Sort(failed)
		t.Fatalf("shipped review QA %s verifier rejected actual database proof; false predicates: %v", phase, failed)
	}
	t.Logf("review QA %s proof passed: sha256=%x", phase, sha256.Sum256(proof))
	return proof
}
