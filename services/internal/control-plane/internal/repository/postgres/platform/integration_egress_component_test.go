package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
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

// noOriginResolver is the first-run negative DNS fixture: every Resolve call fails, proving that
// an empty owner-origin set never attempts resolution. It carries no resolver configuration or
// network access.
// noOriginResolver является first-run negative DNS fixture: каждый Resolve завершается ошибкой,
// проверяя отсутствие попыток разрешения при пустом owner-origin set. Не содержит resolver
// configuration и не обращается к сети.
type noOriginResolver struct{}

// Resolve returns an empty snapshot with the fixed test failure for any host/context. It performs
// no DNS lookup; successful empty projections must never call it.
// Resolve возвращает пустой snapshot с фиксированной test error для любых host/context. DNS lookup
// не выполняет; успешные пустые projections не должны его вызывать.
func (noOriginResolver) Resolve(context.Context, string) (shared.Snapshot, error) {
	return shared.Snapshot{}, errors.New("empty first-run projection must not resolve DNS")
}

// context7OriginResolver provides one deterministic public address with a short current expiry for
// the exact Context7 test hostname. The fixture models DNS data without consulting an external
// resolver or authorizing wildcard destinations.
// context7OriginResolver предоставляет один детерминированный public address с коротким текущим
// expiry для точного Context7 test hostname. Моделирует DNS data без обращения к внешнему resolver
// или разрешения wildcard destinations.
type context7OriginResolver struct{}

// Resolve accepts only mcp.context7.com and returns the fixed public address plus a one-minute
// expiry; every other hostname is a fixed error. The context is deliberately unused because the
// fixture does no I/O, and the timestamp is not a durable generation or trust proof.
// Resolve принимает только mcp.context7.com и возвращает фиксированный public address с expiry
// через минуту; любой другой hostname даёт фиксированную ошибку. Context намеренно не
// используется, поскольку fixture не делает I/O; timestamp не является durable generation или
// trust proof.
func (context7OriginResolver) Resolve(_ context.Context, host string) (shared.Snapshot, error) {
	if host != "mcp.context7.com" {
		return shared.Snapshot{}, errors.New("unexpected synthetic projection destination")
	}
	return shared.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("8.8.8.8")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

// TestIntegrationEgressShippedContext7Component checks disposable owner egress for a shipped
// Context7 connection without managed binding, exact synthetic DNS projection and
// rejection/removal after invalid configuration, disable or delete. It then binds an exact
// published managed definition and ensures stale managed state never falls back to shipped
// origins. No live DNS, upstream request or Kubernetes egress apply is performed.
// TestIntegrationEgressShippedContext7Component проверяет disposable owner egress shipped Context7
// connection без managed binding, точную synthetic DNS projection и отказ/удаление после invalid
// configuration, disable или delete. Затем привязывает exact published managed definition и
// проверяет отсутствие fallback к shipped origins при stale managed state. Live DNS, upstream
// request или Kubernetes egress apply не выполняется.
func TestIntegrationEgressShippedContext7Component(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated egress PostgreSQL")
	}
	defer pool.Close()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
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

// TestIntegrationEgressProjectionComponent keeps the separate unbootstrapped gpt-5.6-sol first-run
// fixture and proves empty-origin projection never resolves DNS. Repeating the same digest
// preserves generation one and the identical durable document. It uses disposable PostgreSQL and
// synthetic data rather than a live network-policy rollout.
// TestIntegrationEgressProjectionComponent сохраняет отдельную unbootstrapped gpt-5.6-sol
// first-run fixture и проверяет отсутствие DNS resolution для empty-origin projection. Повтор того
// же digest сохраняет generation один и идентичный durable document. Использует disposable
// PostgreSQL и synthetic data вместо live network-policy rollout.
func TestIntegrationEgressProjectionComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
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
