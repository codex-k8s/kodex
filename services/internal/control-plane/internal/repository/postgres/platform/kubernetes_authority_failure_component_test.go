package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesAuthorityScanFault installs a failing codec only for one exact native owner query.
// PostgreSQL executes the SELECT successfully; driver decoding fails without aborting its transaction.
// kubernetesAuthorityScanFault устанавливает failing codec только для одного exact native owner query.
// PostgreSQL успешно выполняет SELECT; driver decoding падает без abort транзакции.
type kubernetesAuthorityScanFault struct {
	sql   string
	oid   uint32
	fired atomic.Int32
}

// kubernetesAuthorityScanKey retains the original per-connection codec until native rows close.
// kubernetesAuthorityScanKey сохраняет original per-connection codec до закрытия native rows.
type kubernetesAuthorityScanKey struct{}

// TraceQueryStart changes only the selected decoder, never SQL, arguments or authority rows.
// TraceQueryStart меняет только выбранный decoder, не SQL, arguments либо authority rows.
func (fault *kubernetesAuthorityScanFault) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL != fault.sql || !fault.fired.CompareAndSwap(0, 1) {
		return ctx
	}
	original, ok := conn.TypeMap().TypeForOID(fault.oid)
	if !ok {
		panic("missing native codec")
	}
	conn.TypeMap().RegisterType(&pgtype.Type{Name: original.Name, OID: original.OID, Codec: kubernetesAuthorityFailureCodec{Codec: original.Codec}})
	return context.WithValue(ctx, kubernetesAuthorityScanKey{}, original)
}

// TraceQueryEnd restores the native codec after Scan closes rows, including failed Scan paths.
// TraceQueryEnd восстанавливает native codec после закрытия rows, включая failed Scan.
func (*kubernetesAuthorityScanFault) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, _ pgx.TraceQueryEndData) {
	if original, ok := ctx.Value(kubernetesAuthorityScanKey{}).(*pgtype.Type); ok {
		conn.TypeMap().RegisterType(original)
	}
}

// kubernetesAuthorityFailureCodec preserves real encoding and overrides only the Scan plan.
// kubernetesAuthorityFailureCodec сохраняет real encoding, заменяя только Scan plan.
type kubernetesAuthorityFailureCodec struct{ pgtype.Codec }

// PlanScan returns a controlled driver conversion failure for a successful PostgreSQL row.
// PlanScan возвращает управляемую driver conversion failure для успешной строки PostgreSQL.
func (kubernetesAuthorityFailureCodec) PlanScan(*pgtype.Map, uint32, int16, any) pgtype.ScanPlan {
	return kubernetesAuthorityFailureScan{}
}

// kubernetesAuthorityFailureScan leaves destination fields unset and exposes no row contents.
// kubernetesAuthorityFailureScan оставляет destination fields незаполненными, не раскрывая row contents.
type kubernetesAuthorityFailureScan struct{}

// Scan fails locally without another SQL statement or an aborted PostgreSQL transaction.
// Scan падает локально без дополнительного SQL statement либо abort PostgreSQL transaction.
func (kubernetesAuthorityFailureScan) Scan([]byte, any) error {
	return errors.New("synthetic native owner scan failure")
}

// kubernetesAuthorityFaultRepository owns a new native pool with one exact Scan failure.
// It shares immutable configuration without changing the healthy fixture pool.
// kubernetesAuthorityFaultRepository владеет новым native pool с одной exact Scan failure.
// Разделяет immutable configuration, не меняя healthy pool fixture.
func kubernetesAuthorityFaultRepository(t *testing.T, ctx context.Context, original *Repository, sql string, oid uint32) (*Repository, *kubernetesAuthorityScanFault) {
	t.Helper()
	fault := &kubernetesAuthorityScanFault{sql: sql, oid: oid}
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

// kubernetesAuthorityOwnerSnapshot hashes native owner state and purpose receipts without reading
// target credentials, workspace data or external resources. It includes versions and generations.
// kubernetesAuthorityOwnerSnapshot хэширует native owner state и purpose receipts, не читая
// target credentials, workspace data либо external resources. Включает versions и generations.
func kubernetesAuthorityOwnerSnapshot(t *testing.T, ctx context.Context, fixture kubernetesSetupOwnerFixture) string {
	t.Helper()
	var digest string
	err := fixture.pool.QueryRow(ctx, `SELECT md5(jsonb_build_object(
 'connections',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_connections v WHERE organization_id=$1::uuid),
 'bindings',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_personal_credentials v WHERE organization_id=$1::uuid),
 'identities',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.integration_broker_identity_revisions v WHERE organization_id=$1::uuid),
 'setups',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.kubernetes_identity_setups v WHERE organization_id=$1::uuid),
 'attempts',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM control_plane.kubernetes_identity_upload_attempts v WHERE organization_id=$1::uuid),
 'observations',(SELECT jsonb_agg(to_jsonb(v) ORDER BY attempt_id) FROM control_plane.kubernetes_identity_upload_observations v JOIN control_plane.kubernetes_identity_upload_attempts a ON a.id=v.attempt_id WHERE a.organization_id=$1::uuid),
 'rejections',(SELECT jsonb_agg(to_jsonb(v) ORDER BY attempt_id) FROM control_plane.kubernetes_identity_acknowledgement_rejections v JOIN control_plane.kubernetes_identity_upload_attempts a ON a.id=v.attempt_id WHERE a.organization_id=$1::uuid),
 'material',(SELECT jsonb_agg(to_jsonb(v) ORDER BY identity_revision_id) FROM control_plane.kubernetes_identity_material_revisions v WHERE organization_id=$1::uuid)
 )::text)`, fixture.current.organizationID).Scan(&digest)
	if err != nil {
		t.Fatal("capture private native owner snapshot", err)
	}
	return digest
}

// TestKubernetesAuthorityFailureOwnerComponent checks native Scan and SQL failures against the real
// Complete owner transaction: no cancellation, revocation, material credit or version advancement.
// It preserves OIDC fallback and retries only receipt delivery, never an external storage WRITE.
// TestKubernetesAuthorityFailureOwnerComponent проверяет native Scan и SQL failures в настоящей
// Complete owner transaction: без cancellation, revocation, material credit и version advancement.
// Сохраняет OIDC fallback; повторяет только receipt delivery, не external storage WRITE.
func TestKubernetesAuthorityFailureOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 70*time.Second)
	defer cancel()
	fixture, _, connection, binding := kubernetesPersonalHealthFixture(t, ctx)
	adminConfig, err := pgx.ParseConfig(os.Getenv("KODEX_CONTROL_PLANE_TEST_ADMIN_DSN"))
	application := fixture.pool.Config().ConnConfig
	if err != nil || adminConfig.Database != "postgres" || adminConfig.User != "postgres" ||
		adminConfig.Host != application.Host || adminConfig.Port != application.Port ||
		!strings.HasPrefix(application.Database, "kodex_assistant_test_") {
		t.Fatal("native fault administrator does not match the disposable fixture")
	}
	adminConfig.Database = application.Database
	admin, err := pgx.ConnectConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("connect disposable fault administrator")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer stop()
		_ = admin.Close(cleanup)
	})
	groupRef := "grp_native_fault_group"
	if _, err := fixture.pool.Exec(ctx, `INSERT INTO control_plane.oidc_groups(ref,organization_id,issuer,external_group_digest,display_name,state,last_seen_at,synchronized_at) VALUES($1,$2::uuid,'synthetic-native',repeat('a',64),'Native group','ACTIVE',clock_timestamp(),clock_timestamp())`, groupRef, fixture.current.organizationID); err != nil {
		t.Fatal("seed native OIDC group", err)
	}
	for _, item := range []struct{ name, ref, sql string }{{"subject", fixture.current.actorRef, queryAccessResolveSubject}, {"group", groupRef, queryAccessResolveGroup}} {
		t.Run(item.name, func(t *testing.T) {
			healthy, err := fixture.pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin healthy native access")
			}
			subject, err := fixture.repository.resolveAccessSubject(ctx, healthy, fixture.current.organizationID, item.ref)
			_ = healthy.Rollback(ctx)
			if err != nil || !subject.Active || (item.name == "group" && subject.Kind != "OIDC_GROUP") {
				t.Fatal("healthy native access or OIDC fallback changed", err)
			}
			repository, fault := kubernetesAuthorityFaultRepository(t, ctx, fixture.repository, item.sql, pgtype.BoolOID)
			tx, err := repository.pool.Begin(ctx)
			if err != nil {
				t.Fatal("begin native scan fault")
			}
			_, err = repository.resolveAccessSubject(ctx, tx, fixture.current.organizationID, item.ref)
			if !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
				t.Fatal("native scan failure became owner denial", err)
			}
			var alive int
			if tx.QueryRow(ctx, "SELECT 1").Scan(&alive) != nil || alive != 1 {
				t.Fatal("scan failure aborted PostgreSQL transaction")
			}
			_ = tx.Rollback(ctx)
		})
	}
	owner := fixture.principal
	owner.Permission = "platform.integrations.kubernetes.setup.begin"
	admitted, err := fixture.repository.BeginKubernetesIdentitySetup(ctx, owner, value.Mutation{IdempotencyKey: "kube-native-fault-replacement", ExpectedVersion: &binding.Version}, command.KubernetesIdentitySetupBegin{ConnectionRef: connection.Ref, ExpectedConnectionVersion: connection.Version, ContentSHA256: strings.Repeat("b", 64), ContentBytes: 12})
	if err != nil {
		t.Fatal("prepare native replacement", err)
	}
	permit := kubernetesHealthFenceDigest(string(admitted.Permit))
	clear(admitted.Permit)
	broker := fixture.principal
	broker.CallerWorkload = "secret-broker"
	broker.Permission = "platform.runtime.kubernetes.setup.resolve"
	resolved, err := fixture.repository.ResolveKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadResolve{SetupRef: admitted.View.Ref, PermitSHA256: permit})
	if err != nil {
		t.Fatal("resolve native replacement", err)
	}
	broker.Permission = "platform.runtime.kubernetes.setup.claim"
	claim, err := fixture.repository.ClaimKubernetesIdentityUpload(ctx, broker, command.KubernetesIdentityUploadClaim{SetupRef: admitted.View.Ref, PermitSHA256: permit, SnapshotDigest: resolved.Digest, ContentSHA256: resolved.Provision.ContentSHA256, WorkloadInstance: "native-fault-broker"})
	if err != nil {
		t.Fatal("claim native replacement", err)
	}
	name, err := managedkubernetes.IdentitySecretName(resolved.Provision.IdentityRef, resolved.Provision.Revision)
	if err != nil {
		t.Fatal("derive native material name")
	}
	completion := command.KubernetesIdentityUploadComplete{SetupRef: claim.SetupRef, AttemptRef: claim.AttemptRef, Fence: claim.Fence, Generation: claim.Generation, WorkloadInstance: "native-fault-broker", Identity: &value.ManagedKubernetesIdentityLocator{IdentityRef: resolved.Provision.IdentityRef, TargetRef: resolved.Provision.TargetRef, Revision: resolved.Provision.Revision, Namespace: resolved.Provision.Namespace, Name: name, UID: uuid.NewString(), ResourceVersion: "1", ContentSHA256: resolved.Provision.ContentSHA256}}
	broker.Permission = "platform.runtime.kubernetes.setup.complete"
	for _, item := range []struct {
		name, sql string
		oid       uint32
	}{{"owner scan", queryAccessResolveSubject, pgtype.BoolOID}, {"attachment fact scan", queryBrokerIdentityKubernetesFact, pgtype.TimestamptzOID}} {
		t.Run(item.name, func(t *testing.T) {
			before := kubernetesAuthorityOwnerSnapshot(t, ctx, fixture)
			repository, fault := kubernetesAuthorityFaultRepository(t, ctx, fixture.repository, item.sql, item.oid)
			if _, err := repository.CompleteKubernetesIdentityUpload(ctx, broker, completion); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
				t.Fatal("failed native completion did not remain unavailable", err)
			}
			if kubernetesAuthorityOwnerSnapshot(t, ctx, fixture) != before {
				t.Fatal("native scan failure changed owner state or receipts")
			}
		})
	}
	// The disposable trigger raises an SQL error after material/setup writes and before attachment.
	// Disposable trigger создаёт SQL error после material/setup writes, до attachment.
	if _, err := admin.Exec(ctx, `CREATE FUNCTION control_plane.native_kube_binding_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION USING ERRCODE='58000', MESSAGE='synthetic native binding failure'; END $$; CREATE TRIGGER native_kube_binding_failure BEFORE UPDATE ON control_plane.integration_personal_credentials FOR EACH ROW EXECUTE FUNCTION control_plane.native_kube_binding_failure()`); err != nil {
		t.Fatal("install native write fault", err)
	}
	before := kubernetesAuthorityOwnerSnapshot(t, ctx, fixture)
	if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, completion); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("native attachment SQL error became conflict", err)
	}
	if kubernetesAuthorityOwnerSnapshot(t, ctx, fixture) != before {
		t.Fatal("native SQL failure committed partial owner writes")
	}
	if _, err := admin.Exec(ctx, `DROP TRIGGER native_kube_binding_failure ON control_plane.integration_personal_credentials`); err != nil {
		t.Fatal("remove native write fault", err)
	}
	completed, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, completion)
	if err != nil || completed.State != "COMPLETED" {
		t.Fatal("same owner receipt did not recover", err)
	}
	if _, err := admin.Exec(ctx, `CREATE TRIGGER native_kube_binding_failure BEFORE UPDATE ON control_plane.integration_personal_credentials FOR EACH ROW EXECUTE FUNCTION control_plane.native_kube_binding_failure()`); err != nil {
		t.Fatal("install native quarantine fault", err)
	}
	rejected := completion
	rejected.Identity = nil
	rejected.RejectedAcknowledgement = &value.ManagedKubernetesAcknowledgementRejection{ResponseSHA256: strings.Repeat("c", 64), ObservedUID: completion.Identity.UID}
	before = kubernetesAuthorityOwnerSnapshot(t, ctx, fixture)
	if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, rejected); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("native quarantine SQL error became conflict", err)
	}
	if kubernetesAuthorityOwnerSnapshot(t, ctx, fixture) != before {
		t.Fatal("native quarantine failure committed partial revocation")
	}
	if _, err := admin.Exec(ctx, `DROP TRIGGER native_kube_binding_failure ON control_plane.integration_personal_credentials; DROP FUNCTION control_plane.native_kube_binding_failure()`); err != nil {
		t.Fatal("remove native quarantine fault", err)
	}
	if _, err := fixture.repository.CompleteKubernetesIdentityUpload(ctx, broker, rejected); err != nil {
		t.Fatal("same negative receipt did not recover", err)
	}
}
