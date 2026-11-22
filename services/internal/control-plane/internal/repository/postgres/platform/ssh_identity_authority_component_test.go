package platform

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sshAuthorityScanFault installs a failing codec only for one exact native owner query.
// PostgreSQL executes the SELECT successfully; driver decoding fails without aborting its transaction.
// sshAuthorityScanFault устанавливает failing codec только для одного exact native owner query.
// PostgreSQL успешно выполняет SELECT; driver decoding падает без abort транзакции.
type sshAuthorityScanFault struct {
	sql          string
	rewrittenSQL string
	oid          uint32
	fired        atomic.Int32
}

// sshAuthorityScanKey retains the original per-connection codec until native rows close.
// sshAuthorityScanKey сохраняет original per-connection codec до закрытия native rows.
type sshAuthorityScanKey struct{}

// TraceQueryStart changes only the selected decoder, never SQL, arguments or authority rows.
// TraceQueryStart меняет только выбранный decoder, не SQL, arguments либо authority rows.
func (fault *sshAuthorityScanFault) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if (data.SQL != fault.sql && data.SQL != fault.rewrittenSQL) || !fault.fired.CompareAndSwap(0, 1) {
		return ctx
	}
	original, ok := conn.TypeMap().TypeForOID(fault.oid)
	if !ok {
		panic("missing native codec")
	}
	conn.TypeMap().RegisterType(&pgtype.Type{Name: original.Name, OID: original.OID, Codec: sshAuthorityFailureCodec{Codec: original.Codec}})
	return context.WithValue(ctx, sshAuthorityScanKey{}, original)
}

// TraceQueryEnd restores the native codec after Scan closes rows, including failed Scan paths.
// TraceQueryEnd восстанавливает native codec после закрытия rows, включая failed Scan.
func (*sshAuthorityScanFault) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, _ pgx.TraceQueryEndData) {
	if original, ok := ctx.Value(sshAuthorityScanKey{}).(*pgtype.Type); ok {
		conn.TypeMap().RegisterType(original)
	}
}

// sshAuthorityFailureCodec preserves real encoding and overrides only the Scan plan.
// sshAuthorityFailureCodec сохраняет real encoding, заменяя только Scan plan.
type sshAuthorityFailureCodec struct{ pgtype.Codec }

// PlanScan returns a controlled driver conversion failure for a successful PostgreSQL row.
// PlanScan возвращает управляемую driver conversion failure для успешной строки PostgreSQL.
func (sshAuthorityFailureCodec) PlanScan(*pgtype.Map, uint32, int16, any) pgtype.ScanPlan {
	return sshAuthorityFailureScan{}
}

// sshAuthorityFailureScan leaves destination fields unset and exposes no row contents.
// sshAuthorityFailureScan оставляет destination fields незаполненными, не раскрывая row contents.
type sshAuthorityFailureScan struct{}

// Scan fails locally without another SQL statement or an aborted PostgreSQL transaction.
// Scan падает локально без дополнительного SQL statement либо abort PostgreSQL transaction.
func (sshAuthorityFailureScan) Scan([]byte, any) error {
	return errors.New("synthetic native owner scan failure")
}

// sshAuthorityFaultRepository owns a new native pool with one exact Scan failure.
// It shares immutable configuration without changing the healthy fixture pool.
// sshAuthorityFaultRepository владеет новым native pool с одной exact Scan failure.
// Разделяет immutable configuration, не меняя healthy pool fixture.
func sshAuthorityFaultRepository(t *testing.T, ctx context.Context, original *Repository, sql string, oid uint32) (*Repository, *sshAuthorityScanFault) {
	t.Helper()
	rewritten, _, rewriteErr := (pgx.NamedArgs{"organization_id": nil, "connection_id": nil, "actor_id": nil, "identity_id": nil}).RewriteQuery(ctx, nil, sql, nil)
	if rewriteErr != nil {
		t.Fatal("prepare exact native query identity", rewriteErr)
	}
	fault := &sshAuthorityScanFault{sql: sql, rewrittenSQL: rewritten, oid: oid}
	config := original.pool.Config()
	config.ConnConfig.Tracer = fault
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("construct native fault pool")
	}
	t.Cleanup(pool.Close)
	copy := *original
	copy.pool = pool
	return &copy, fault
}

// querySSHNativeOwnerSnapshot reads private synthetic state and receipts solely to compare rollback atomicity; raw values never reach test diagnostics.
// querySSHNativeOwnerSnapshot читает private synthetic state и receipts лишь для сравнения rollback atomicity; raw values не входят в diagnostics.
//
//go:embed testdata/sql/ssh_native_owner_snapshot.sql
var querySSHNativeOwnerSnapshot string

// querySSHNativeAttachmentFault creates a disposable58000 failure after setup/material changes but before owner attachment.
// querySSHNativeAttachmentFault создаёт disposable58000 failure после setup/material changes, до owner attachment.
//
//go:embed testdata/sql/ssh_native_attachment_fault.sql
var querySSHNativeAttachmentFault string

// querySSHNativeAttachmentFaultDrop removes only the exact owned disposable fault trigger and function.
// querySSHNativeAttachmentFaultDrop удаляет лишь exact owned disposable fault trigger и function.
//
//go:embed testdata/sql/ssh_native_attachment_fault_drop.sql
var querySSHNativeAttachmentFaultDrop string

// snapshot hashes all owner versions, grants and immutable receipts to detect a partial commit after an infrastructure failure.
// snapshot хэширует owner versions, grants и immutable receipts для обнаружения partial commit после infrastructure failure.
func (f *sshSetupNativeFixture) snapshot(t *testing.T) string {
	t.Helper()
	var result string
	if err := f.pool.QueryRow(f.ctx, querySSHNativeOwnerSnapshot).Scan(&result); err != nil {
		t.Fatal("capture private native owner snapshot", err)
	}
	return result
}

// TestSSHIdentityAuthorityFailureOwnerComponent injects successful-SQL/failed-Scan faults into both validation and attachment, plus a real58000 attachment failure. All preserve state and allow delivery of the same original receipt.
// TestSSHIdentityAuthorityFailureOwnerComponent внедряет successful-SQL/failed-Scan faults в validation и attachment, а также real58000 attachment failure. State сохраняется, доставка того же original receipt остаётся возможной.
func TestSSHIdentityAuthorityFailureOwnerComponent(t *testing.T) {
	for _, outcome := range []string{"KNOWN", "UNKNOWN"} {
		t.Run(outcome, func(t *testing.T) {
			f := newSSHSetupNativeFixture(t)
			setup := f.begin(t, "ssh-native-owner-fault", 0)
			admission, _ := f.claim(t, setup)
			receipt := command.SSHIdentityUploadComplete{Unknown: true}
			if outcome == "KNOWN" {
				locator := f.locator(setup)
				receipt = command.SSHIdentityUploadComplete{Identity: &locator}
			}
			cases := []struct {
				name, query string
				oid         uint32
			}{{"validation scan", queryAccessResolveSubject, pgtype.BoolOID}}
			if outcome == "KNOWN" {
				cases = append(cases, struct {
					name, query string
					oid         uint32
				}{"attachment scan", queryBrokerIdentitySSHFact, pgtype.TimestamptzOID})
			}
			for _, item := range cases {
				t.Run(item.name, func(t *testing.T) {
					before := f.snapshot(t)
					faulty, fault := sshAuthorityFaultRepository(t, f.ctx, f.repository, item.query, item.oid)
					copy := *f
					copy.repository = faulty
					if _, err := copy.complete(admission, receipt); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
						t.Fatalf("infrastructure became terminal owner transition: unavailable=%t fired=%d", errors.Is(err, errs.ErrUnavailable), fault.fired.Load())
					}
					if f.snapshot(t) != before {
						t.Fatal("native Scan failure changed state, versions or receipts")
					}
				})
			}
			if outcome == "KNOWN" {
				adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
				current := f.pool.Config().ConnConfig
				if err != nil || adminConfig.User != "postgres" || adminConfig.Database != "postgres" || adminConfig.Host != current.Host || adminConfig.Port != current.Port || !strings.HasPrefix(current.Database, "kodex_assistant_test_") {
					t.Fatal("fault administrator does not match dedicated fixture")
				}
				adminConfig.Database = current.Database
				admin, err := pgx.ConnectConfig(f.ctx, adminConfig)
				if err != nil {
					t.Fatal("connect disposable fault administrator", err)
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.WithoutCancel(f.ctx), time.Second)
					defer cancel()
					_ = admin.Close(ctx)
				}()
				if _, err := admin.Exec(f.ctx, querySSHNativeAttachmentFault); err != nil {
					t.Fatal("install exact native attachment fault", err)
				}
				before := f.snapshot(t)
				if _, err := f.complete(admission, receipt); !errors.Is(err, errs.ErrUnavailable) {
					t.Fatal("SQL failure became terminal owner transition", err)
				}
				if f.snapshot(t) != before {
					t.Fatal("native SQL failure partially committed material or owner state")
				}
				if _, err := admin.Exec(f.ctx, querySSHNativeAttachmentFaultDrop); err != nil {
					t.Fatal("remove exact native attachment fault", err)
				}
			}
			if _, err := f.complete(admission, receipt); err != nil {
				t.Fatal("same original receipt could not recover after dependency repair", err)
			}
		})
	}
}
