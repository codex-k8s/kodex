package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

type noOriginResolver struct{}

func (noOriginResolver) Resolve(context.Context, string) (shared.Snapshot, error) {
	return shared.Snapshot{}, errors.New("empty first-run projection must not resolve DNS")
}

type context7OriginResolver struct{}

func (context7OriginResolver) Resolve(_ context.Context, host string) (shared.Snapshot, error) {
	if host != "mcp.context7.com" {
		return shared.Snapshot{}, errors.New("unexpected synthetic projection destination")
	}
	return shared.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func TestIntegrationEgressShippedContext7Component(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated egress PostgreSQL")
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "egress-context7-create"}, Payload: command.ConnectionInput{DefinitionKey: "context7", Name: "Synthetic Context7 egress", PublicConfiguration: map[string]any{"base_url": "https://mcp.context7.com"}}})
	if err != nil || created.Connection == nil {
		t.Fatal("create synthetic shipped connection")
	}
	ref := created.Connection.Ref
	var bindings int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.managed_configuration_bindings WHERE consumer_ref=$1`, ref).Scan(&bindings); err != nil || bindings != 0 {
		t.Fatal("shipped connection unexpectedly has managed binding")
	}
	hosts, err := r.IntegrationEgressHostnames(ctx)
	if err != nil || !slices.Equal(hosts, []string{"mcp.context7.com"}) {
		t.Fatalf("shipped owner origin absent: hosts=%v err=%v", hosts, err)
	}
	document, err := r.PrepareIntegrationEgressProjection(ctx, strings.Repeat("a", 64), context7OriginResolver{})
	if err != nil || document.Validate() != nil || document.SourceDigest != shared.SourceDigest(hosts) || len(document.Destinations) != 1 {
		t.Fatal("shipped owner projection did not preserve exact destination")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET public_configuration='{"base_url":"https://private.invalid"}' WHERE ref=$1`, ref); err != nil {
		t.Fatal("prepare invalid synthetic configuration")
	}
	if hosts, err := r.IntegrationEgressHostnames(ctx); err == nil || len(hosts) != 0 {
		t.Fatal("invalid shipped configuration retained egress origin")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET public_configuration='{"base_url":"https://mcp.context7.com"}' WHERE ref=$1`, ref); err != nil {
		t.Fatal("restore synthetic configuration")
	}
	for _, mutation := range []string{"enabled=false", "enabled=true,lifecycle_state='DELETED'"} {
		// Только локальная disposable fixture; production SQL остаётся отдельным.
		if _, err := pool.Exec(ctx, "UPDATE control_plane.integration_connections SET "+mutation+" WHERE ref=$1", ref); err != nil {
			t.Fatal("prepare inactive synthetic connection")
		}
		if hosts, err := r.IntegrationEgressHostnames(ctx); err != nil || len(hosts) != 0 {
			t.Fatal("inactive connection retained egress origin")
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.integration_connections SET enabled=true,lifecycle_state='ACTIVE' WHERE ref=$1`, ref); err != nil {
		t.Fatal("restore active synthetic connection")
	}
	managed := r.integrationDefinitions["context7"]
	managed.Spec.Name = "Synthetic managed Context7"
	raw, err := json.Marshal(managed)
	if err != nil {
		t.Fatal(err)
	}
	bound := publishAndRebindManagedConfiguration(t, ctx, service, owner, "egress-context7-managed",
		command.CreateIntegrationDefinition, command.ValidateIntegrationDefinition, command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.ManagedConfigurationInput{Name: managed.Spec.Name, ContentFormat: "JSON", Content: string(raw)},
		entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: ref})
	if hosts, err := r.IntegrationEgressHostnames(ctx); err != nil || !slices.Equal(hosts, []string{"mcp.context7.com"}) {
		t.Fatal("exact published managed origin missing")
	}
	// Повреждённый/stale managed owner path не понижается до SHIPPED.
	if _, err := pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET current_revision_id=NULL WHERE ref=$1`, bound.ManagedConfiguration.Ref); err != nil {
		t.Fatal("prepare stale managed binding")
	}
	if hosts, err := r.IntegrationEgressHostnames(ctx); err != nil || len(hosts) != 0 {
		t.Fatal("stale managed binding fell back to shipped egress")
	}
}

func TestIntegrationEgressProjectionComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5.6-sol", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := repository.IntegrationEgressHostnames(ctx)
	if err != nil || len(hosts) != 0 {
		t.Fatalf("first-run origins = %v: %v", hosts, err)
	}
	baseDigest := strings.Repeat("a", 64)
	first, err := repository.PrepareIntegrationEgressProjection(ctx, baseDigest, noOriginResolver{})
	if err != nil || first.Generation != 1 || len(first.Destinations) != 0 || first.Validate() != nil {
		t.Fatalf("first-run document = %#v: %v", first, err)
	}
	second, err := repository.PrepareIntegrationEgressProjection(ctx, baseDigest, noOriginResolver{})
	if err != nil || second.Digest() != first.Digest() || second.Generation != first.Generation {
		t.Fatalf("identical replay advanced generation: %#v: %v", second, err)
	}
	var generation int64
	var digest string
	var storedRaw []byte
	if err := pool.QueryRow(ctx, `SELECT generation,target_digest,document FROM control_plane.integration_egress_projection WHERE singleton=true`).Scan(&generation, &digest, &storedRaw); err != nil || generation != 1 || digest != first.Digest() || len(storedRaw) == 0 {
		t.Fatalf("durable projection mismatch: generation=%d digest=%q err=%v", generation, digest, err)
	}
}
