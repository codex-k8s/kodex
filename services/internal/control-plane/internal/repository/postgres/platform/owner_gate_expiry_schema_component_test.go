package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// ownerGateExpiryWrongGrouping changes only a disposable schema to a subtly incompatible CHECK.
// ownerGateExpiryWrongGrouping меняет только одноразовую схему на несовместимую группировку CHECK.
//
//go:embed testdata/sql/owner_gate_expiry_wrong_grouping.sql
var ownerGateExpiryWrongGrouping string

// ownerGateExpiryPartialTrigger simulates a schema drift that leaves direct expiry updates unguarded.
// ownerGateExpiryPartialTrigger моделирует изменение схемы, оставляющее прямой UPDATE срока без защиты.
//
//go:embed testdata/sql/owner_gate_expiry_partial_trigger.sql
var ownerGateExpiryPartialTrigger string

// TestOwnerGateExpiryMigrationProofComponent executes the shipped read-only schema proof against
// valid and deliberately drifted disposable databases. No staging or business state is modified.
// TestOwnerGateExpiryMigrationProofComponent исполняет поставляемый read-only запрос на правильной
// и намеренно изменённых одноразовых схемах. Staging и бизнес-состояние не изменяются.
func TestOwnerGateExpiryMigrationProofComponent(t *testing.T) {
	for _, fixture := range []struct{ name, mutation, rejectedField string }{
		{"canonical", "", ""},
		{"wrong-grouping", ownerGateExpiryWrongGrouping, "expiryConstraint"},
		{"partial-trigger", ownerGateExpiryPartialTrigger, "immutableGuard"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			// This helper validates loopback/admin identity and owns exact database cleanup.
			// Этот helper проверяет loopback/admin identity и владеет удалением точной БД.
			dsn := isolatedAssistantComponentDSN(t)
			runtime, err := pgx.ParseConfig(dsn)
			if err != nil || !strings.HasPrefix(runtime.Database, "kodex_assistant_test_") {
				t.Fatal("invalid disposable expiry schema database")
			}
			admin, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
			if err != nil || admin.Host != runtime.Host || admin.Port != runtime.Port || admin.Database != "postgres" || admin.User != "postgres" {
				t.Fatal("invalid disposable expiry schema administrator")
			}
			admin.Database = runtime.Database
			conn, err := pgx.ConnectConfig(ctx, admin)
			if err != nil {
				t.Fatal("connect disposable expiry schema administrator")
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
				defer stop()
				_ = conn.Close(cleanup)
			}()
			if fixture.mutation != "" {
				if _, err := conn.Exec(ctx, fixture.mutation); err != nil {
					t.Fatal("apply disposable expiry schema drift")
				}
			}
			source, err := os.ReadFile("../../../../../../../tools/release/owner-gate-expiry-migration-postcondition.sql")
			if err != nil {
				t.Fatal("read shipped expiry schema proof")
			}
			results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
			if err != nil {
				t.Fatal("execute read-only expiry schema proof")
			}
			var proof map[string]any
			for _, result := range results {
				if result.Err != nil {
					t.Fatal("expiry schema proof statement failed")
				}
				if len(result.Rows) == 1 && len(result.Rows[0]) == 1 {
					if err := json.Unmarshal(result.Rows[0][0], &proof); err != nil {
						t.Fatal("decode expiry schema proof")
					}
				}
			}
			if proof["readOnly"] != "on" || proof["database"] != runtime.Database || proof["gooseVersion"] != float64(20261026001100) || proof["tableOwner"] != "control_plane_owner" {
				t.Fatal("expiry schema proof identity mismatch")
			}
			for _, field := range []string{"expiryColumn", "stateConstraint", "resolutionConstraint", "expiryConstraint", "dueIndex", "immutableGuard", "runtimeExecute", "publicExecuteRevoked"} {
				if proof[field] != (field != fixture.rejectedField) {
					t.Errorf("expiry schema proof field %s: got %v", field, proof[field])
				}
			}
		})
	}
}
