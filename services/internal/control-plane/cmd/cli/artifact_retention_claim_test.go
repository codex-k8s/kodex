package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
)

// TestArtifactRetentionClaimParameterTypes proves the exact old SQL fails during PostgreSQL parse,
// then describes and executes the shipped int4-typed query under the actual retention runtime role.
// Only empty synthetic metadata is used; the test neither sends DELETE nor creates external objects.
// TestArtifactRetentionClaimParameterTypes доказывает отказ точного старого SQL при разборе PostgreSQL,
// затем проверяет типы и исполняет поставляемый int4-запрос под настоящей retention runtime ролью.
// Используются только пустые synthetic metadata; внешние DELETE и создание объектов не выполняются.
func TestArtifactRetentionClaimParameterTypes(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114170000); err != nil {
		t.Fatal("prepare exact retention claim schema", err)
	}
	config := observer.Config().Copy()
	config.User = "artifact_retention_runtime_g1"
	runtime, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("connect disposable retention principal", err)
	}
	defer func() {
		if err := runtime.Close(ctx); err != nil {
			t.Error("close retention principal", err)
		}
	}()
	var exact, widened bool
	if err := observer.QueryRow(ctx, `SELECT
 to_regprocedure('control_plane.artifact_manual_retention_claim(text,integer,integer)') IS NOT NULL,
 to_regprocedure('control_plane.artifact_manual_retention_claim(text,bigint,integer)') IS NOT NULL`).Scan(&exact, &widened); err != nil || !exact || widened {
		t.Fatal("manual retention overload boundary differs", err)
	}
	raw, err := os.ReadFile("../../../../../services/jobs/artifact-retention/internal/repository/postgres/sql/claim_due.sql")
	if err != nil {
		t.Fatal("read actual retention claim query", err)
	}
	args := pgx.StrictNamedArgs{"claim_owner": "claim-type-fixture", "batch_size": 32, "lease_seconds": int64(30)}
	legacy := strings.ReplaceAll(string(raw), "@batch_size::integer", "@batch_size")
	legacy = strings.ReplaceAll(legacy, "@lease_seconds::integer * interval", "@lease_seconds * interval")
	// Закреплённый hash подтверждает, что RED — полный original aae7 query, а не похожая модель.
	if fmt.Sprintf("%x", sha256.Sum256([]byte(legacy))) != "fd45c8589a61e795cf2bfe67badc522f6739e20e08d936628b160f91fa16d4e9" {
		t.Fatal("legacy query counterfactual is not the pinned original")
	}
	oldSQL, _, err := args.RewriteQuery(ctx, runtime, legacy, nil)
	if err != nil {
		t.Fatal("rewrite exact legacy placeholders", err)
	}
	_, err = runtime.Prepare(ctx, "legacy_retention_claim", oldSQL)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "42883" || !strings.Contains(pgError.Message, "(text, bigint, integer)") {
		t.Fatal("legacy query did not reproduce exact bigint overload failure", err)
	}
	t.Log("original aae7 SQL EXPECTED_RED: 42883, missing signature (text,bigint,integer), parse-only/no effect")
	query, _, err := args.RewriteQuery(ctx, runtime, string(raw), nil)
	if err != nil {
		t.Fatal("rewrite shipped claim placeholders", err)
	}
	description, err := runtime.Prepare(ctx, "typed_retention_claim", query)
	if err != nil || !slices.Equal(description.ParamOIDs, []uint32{23, 25, 23}) {
		t.Fatal("claim parameter types must be int4/text/int4", err)
	}
	for _, sample := range []struct {
		batch int
		lease int64
	}{{0, 1}, {1, 1}, {32, 30}, {100, 600}} {
		rows, err := runtime.Query(ctx, string(raw), pgx.StrictNamedArgs{
			"claim_owner": "claim-type-fixture", "batch_size": sample.batch, "lease_seconds": sample.lease,
		})
		if err != nil {
			t.Fatal("execute bounded runtime claim", err)
		}
		count := 0
		for rows.Next() {
			count++
		}
		rows.Close()
		if rows.Err() != nil || count != 0 {
			t.Fatal("empty synthetic claim changed outcome", rows.Err())
		}
	}
	var changed bool
	if err := observer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM control_plane.artifact_heads
 WHERE retention_claim_owner='claim-type-fixture')`).Scan(&changed); err != nil || changed {
		t.Fatal("parse/read fixture acquired an unrelated artifact", err)
	}
	t.Log("fixed actual runtime query GREEN: int4/text/int4, signature (text,integer,integer), bounded empty claims and no artifacts changed")
}
