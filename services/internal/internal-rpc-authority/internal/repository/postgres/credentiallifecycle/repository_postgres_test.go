package credentiallifecycle

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReconcileCredentialsReadsCanonicalDigest(t *testing.T) {
	dsn := os.Getenv("INTERNAL_RPC_AUTHORITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("disposable PostgreSQL DSN is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse disposable PostgreSQL DSN: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(
			ctx,
			"SET ROLE internal_rpc_authority_database_credential_reconciler",
		)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL pool: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool)
	if err != nil {
		t.Fatalf("create credential lifecycle repository: %v", err)
	}

	const holderID = "20000000-0000-4000-8000-000000000001"
	fencingToken, err := repository.AcquireLease(ctx, holderID, 30*time.Second)
	if err != nil {
		t.Fatalf("acquire credential lifecycle lease: %v", err)
	}
	canonicalDigest := strings.Repeat("a", 64)
	registered := model.DatabaseCredentialRegisteredSet{
		Version:        model.ContractVersion,
		SourceRevision: 1,
		SourceDigest:   strings.Repeat("b", 64),
		Generations: []model.DatabaseCredentialGeneration{{
			Capability:      model.DatabaseCredentialPublisher,
			Generation:      3,
			Status:          model.DatabaseCredentialCurrent,
			Principal:       "ira_publisher_g3",
			VaultStaticRole: "internal-rpc-authority-publisher-g3",
			SourceRevision:  1,
			SourceDigest:    strings.Repeat("b", 64),
		}},
	}
	if registeredDigest(registered) == canonicalDigest {
		t.Fatal("test fixture does not exercise a distinct canonical digest")
	}

	generations, err := repository.ReconcileCredentials(
		ctx,
		holderID,
		fencingToken,
		"20000000-0000-4000-8000-000000000002",
		canonicalDigest,
		registered,
	)
	if err != nil {
		t.Fatalf("reconcile credentials with target canonical digest: %v", err)
	}
	if len(generations) != 1 || generations[0].Principal != "ira_publisher_g3" {
		t.Fatalf("unexpected reconciled generations: %#v", generations)
	}
}
