package platform

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// standardOIDCComponentPolicy contains only synthetic deployment intent and no credentials, token claims or real organization values.
// standardOIDCComponentPolicy содержит только synthetic deployment intent без credentials, token claims и реальных organization values.
func standardOIDCComponentPolicy() value.OIDCIdentityPolicy {
	return value.OIDCIdentityPolicy{Schema: "kodex.oidc-identity-policy/v1", Revision: 1, Issuer: "https://identity.example.invalid/realm/synthetic",
		BootstrapSubjects: []string{"20000000-0000-4000-8000-000000000001"}, SessionLifetimeSeconds: 3600}
}

// standardOIDCComponentRepository configures the ordinary synthetic provider/image dependencies and the explicit identity policy before bootstrap.
// standardOIDCComponentRepository настраивает обычные synthetic provider/image dependencies и явную identity policy до bootstrap.
func standardOIDCComponentRepository(t *testing.T, pool *pgxpool.Pool, policy *value.OIDCIdentityPolicy) *Repository {
	t.Helper()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureSyntheticRuntimeComponentRepository(t, repository)
	if policy != nil {
		if err := repository.ConfigureStandardOIDCIdentity(*policy); err != nil {
			t.Fatal("configure synthetic identity policy")
		}
	}
	return repository
}

// TestStandardOIDCPolicyComponent proves native forward migration, replica rejoin, immutable policy history and durable rollback/downgrade denial.
// It verifies that publishing policy alone never chooses the first human owner or allocates an authorized session.
// TestStandardOIDCPolicyComponent проверяет native forward migration, replica rejoin, immutable history и durable запрет rollback/downgrade.
// Публикация policy сама не назначает первого human owner и не выделяет авторизованную session.
func TestStandardOIDCPolicyComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated standard identity PostgreSQL")
	}
	defer pool.Close()
	policy := standardOIDCComponentPolicy()
	repository := standardOIDCComponentRepository(t, pool, &policy)
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap explicit standard identity policy: %v", err)
	}
	if err := repository.Ready(ctx); err != nil {
		t.Fatal("configured identity policy is not ready")
	}
	var state string
	var sessions int
	if pool.QueryRow(ctx, `SELECT state FROM control_plane.owner_claim_contracts WHERE stable_key='installation-owner'`).Scan(&state) != nil || state != "PENDING_CLAIM" ||
		pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.oidc_identity_sessions`).Scan(&sessions) != nil || sessions != 0 {
		t.Fatal("policy bootstrap assigned human ownership or session authority")
	}
	replica := standardOIDCComponentRepository(t, pool, &policy)
	if replica.Bootstrap(ctx) != nil || replica.Ready(ctx) != nil {
		t.Fatal("same policy failed durable replica rejoin")
	}
	changed := policy
	changed.SessionLifetimeSeconds = 1800
	conflicting := standardOIDCComponentRepository(t, pool, &changed)
	if conflicting.Bootstrap(ctx) == nil || conflicting.Ready(ctx) == nil {
		t.Fatal("changed content reused the accepted policy revision")
	}
	changed.Revision = 2
	newer := standardOIDCComponentRepository(t, pool, &changed)
	if newer.Bootstrap(ctx) != nil || newer.Ready(ctx) != nil {
		t.Fatal("valid policy advancement failed")
	}
	if replica.Ready(ctx) == nil || replica.Bootstrap(ctx) == nil {
		t.Fatal("old replica ignored policy high-watermark")
	}
	legacy := standardOIDCComponentRepository(t, pool, nil)
	if legacy.Bootstrap(ctx) == nil || legacy.Ready(ctx) == nil {
		t.Fatal("removing configuration silently restored legacy authority")
	}
	for _, mutation := range []string{
		`UPDATE control_plane.oidc_identity_policy_current SET revision=1 WHERE singleton`,
		`UPDATE control_plane.oidc_identity_policies SET digest=repeat('b',64) WHERE revision=2`,
		`DELETE FROM control_plane.oidc_identity_policy_current`,
		`DELETE FROM control_plane.oidc_identity_policies`,
	} {
		if _, err := pool.Exec(ctx, mutation); err == nil {
			t.Fatal("runtime SQL rewrote durable policy authority")
		}
	}
	var revision uint64
	if pool.QueryRow(ctx, `SELECT revision FROM control_plane.oidc_identity_policy_current WHERE singleton`).Scan(&revision) != nil || revision != 2 {
		t.Fatal("policy history changed after failed rollback attempts")
	}
}
