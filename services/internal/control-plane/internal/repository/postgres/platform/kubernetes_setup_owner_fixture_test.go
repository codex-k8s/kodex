package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kubernetesSetupOwnerFixture owns an isolated native schema with synthetic owner metadata only.
// Its existing TOKEN Definition is a tenant FK and deliberately does not qualify Kubernetes execution.
// kubernetesSetupOwnerFixture владеет isolated native schema только с synthetic owner metadata.
// Existing TOKEN Definition служит tenant FK и намеренно не квалифицирует Kubernetes execution.
type kubernetesSetupOwnerFixture struct {
	pool                 *pgxpool.Pool
	repository           *Repository
	candidate            value.KubernetesOperatorRegistry
	principal, transport value.Principal
	current              scope
	begin                command.KubernetesIdentitySetupBegin
}

// newKubernetesSetupOwnerFixture creates the same actual scope/package/operator boundary for owner tests.
// Pool cleanup belongs to the test, and neither network account material nor a live cluster is used.
// newKubernetesSetupOwnerFixture создаёт единую actual scope/package/operator boundary для owner tests.
// Pool cleanup принадлежит test; network account material и live cluster не используются.
func newKubernetesSetupOwnerFixture(t *testing.T, ctx context.Context) kubernetesSetupOwnerFixture {
	t.Helper()
	fixture := kubernetesSetupOwnerFixture{pool: newIsolatedAssistantComponentPool(t, ctx), candidate: kubernetesOperatorComponentFixture(t)}
	t.Cleanup(fixture.pool.Close)
	fixture.repository = newSyntheticRuntimeComponentRepository(t, ctx, fixture.pool, func(repository *Repository) {
		if repository.ConfigureKubernetesOperator(fixture.candidate) != nil {
			t.Fatal("configure synthetic ledger operator")
		}
	})
	if fixture.repository.RegisterKubernetesOperator(ctx) != nil {
		t.Fatal("register synthetic ledger operator")
	}
	fixture.transport = resolvedTestPrincipal(t, ctx, fixture.repository, port.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	var err error
	fixture.principal, err = fixture.repository.ResolvePrincipal(ctx, fixture.transport)
	if err != nil {
		t.Fatal("resolve synthetic ledger principal")
	}
	fixture.current, err = fixture.repository.resolveScope(ctx, fixture.principal)
	if err != nil {
		t.Fatal("resolve synthetic ledger tenant")
	}
	fixture.begin = command.KubernetesIdentitySetupBegin{ConnectionRef: "conn_setup_ledger", ContentSHA256: strings.Repeat("f", 64), ExpectedConnectionVersion: 1, ContentBytes: 12}
	return fixture
}

// seedKubernetesSetupLedgerTx creates one original immutable intent inside a caller-owned test transaction.
// Explicit synthetic IDs and microsecond UTC time keep native row decoding bound to its original digest.
// seedKubernetesSetupLedgerTx создаёт original immutable intent в caller-owned test transaction.
// Явные synthetic IDs и microsecond UTC time сохраняют связь native row decoding с original digest.
func seedKubernetesSetupLedgerTx(t *testing.T, ctx context.Context, tx pgx.Tx, fixture kubernetesSetupOwnerFixture, age int) (string, kubernetesSetupImmutableInput, [32]byte) {
	t.Helper()
	createdAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Duration(age) * time.Second)
	configurationHash := sha256.Sum256([]byte("{}"))
	configurationDigest := hex.EncodeToString(configurationHash[:])
	definitionDigest := fixture.repository.integrationDefinitions["gitlab"].Digest
	connectionID, identityID := uuid.NewString(), uuid.NewString()
	original := kubernetesSetupImmutableInput{SetupRef: "ksu_setup_ledger_fixture", OrganizationID: fixture.current.organizationID,
		ActorID: fixture.current.actorID, ConnectionID: connectionID, IdentityID: identityID, IdentityRef: "bki_setup_ledger",
		TargetRef: fixture.candidate.Targets[0].Target.Ref, DefinitionDigest: definitionDigest, ConfigurationDigest: configurationDigest,
		ContentDigest: fixture.begin.ContentSHA256, IdentityRevision: 1, RegistryRevision: 1, TargetRevision: 1,
		ConnectionVersion: 1, BindingVersion: 1, BindingGeneration: 1, Policy: entity.IntegrationCredentialInitiatorRequired,
		ContentBytes: fixture.begin.ContentBytes, ExpiresAt: createdAt.Add(90 * time.Second)}
	snapshotBytes, err := json.Marshal(original)
	if err != nil {
		t.Fatal("encode immutable synthetic setup snapshot")
	}
	snapshotHash := sha256.Sum256(snapshotBytes)
	intent, err := json.Marshal(struct {
		Input          command.KubernetesIdentitySetupBegin
		BindingVersion int64
	}{fixture.begin, 1})
	if err != nil {
		t.Fatal("encode original synthetic Begin intent")
	}
	intentHash := sha256.Sum256(intent)
	var setupID string
	if tx.QueryRow(ctx, queryKubernetesSetupLedgerFixture, pgx.StrictNamedArgs{
		"organization_id": fixture.current.organizationID, "actor_id": fixture.current.actorID,
		"target_ref": original.TargetRef, "connection_id": connectionID, "identity_id": identityID, "created_at": createdAt,
		"intent_digest": hex.EncodeToString(intentHash[:]), "definition_digest": definitionDigest,
		"configuration_digest": configurationDigest, "snapshot_digest": hex.EncodeToString(snapshotHash[:]),
	}).Scan(&setupID) != nil {
		t.Fatal("create synthetic setup ledger rows")
	}
	return setupID, original, snapshotHash
}
