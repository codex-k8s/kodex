package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testProviderBootstrapHistory(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	historical := repository.providerCredential
	// До смены имени старый same-name pin тоже не должен запускать UID/RV recovery.
	var oldest ProviderCredentialConfig
	if err := pool.QueryRow(ctx, `SELECT c.secret_name, c.secret_uid::text, c.secret_resource_version, c.content_sha256
FROM control_plane.provider_credential_revisions c JOIN control_plane.provider_accounts a ON a.id = c.provider_account_id
WHERE a.stable_key = 'default-openai-codex' AND c.revision_number = 1`).Scan(&oldest.SecretName, &oldest.SecretUID, &oldest.SecretResourceVersion, &oldest.ContentSHA256); err != nil {
		t.Fatal("read original bootstrap pin")
	}
	initialState := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(a.current_credential_revision_id, a.version,
 (SELECT count(*) FROM control_plane.provider_credential_revisions),
 (SELECT count(*) FROM control_plane.provider_credential_cleanup_tasks))::text
FROM control_plane.provider_accounts a WHERE a.stable_key = 'default-openai-codex'`).Scan(&snapshot); err != nil {
			t.Fatal("read same-name state")
		}
		return snapshot
	}
	initial := initialState()
	if err := repository.ConfigureProviderCredential(oldest); err != nil {
		t.Fatal("configure original pin")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("restart original same-name pin: %v", err)
		}
		if initialState() != initial {
			t.Fatal("historical same-name pin created rollback revision")
		}
	}
	if err := repository.ConfigureProviderCredential(historical); err != nil {
		t.Fatal("restore repaired pin")
	}
	current := ProviderCredentialConfig{SecretName: "runtime-provider-forward-history", SecretUID: "10000000-0000-4000-8000-000000001151", SecretResourceVersion: "1151", ContentSHA256: strings.Repeat("a", 64)}
	var accountID, organizationID string
	if err := pool.QueryRow(ctx, `SELECT id::text, organization_id::text FROM control_plane.provider_accounts WHERE stable_key = 'default-openai-codex'`).Scan(&accountID, &organizationID); err != nil {
		t.Fatal("read bootstrap account")
	}
	// Fixture публикует отдельную immutable revision, как уже разрешённая owner rotation.
	if _, err := pool.Exec(ctx, `WITH inserted AS (
 INSERT INTO control_plane.provider_credential_revisions
 (ref, organization_id, provider_account_id, revision_number, secret_name, secret_uid, secret_resource_version, content_sha256, observed_at)
 SELECT 'pcr_history_forward_fixture', a.organization_id, a.id, c.revision_number + 1, $2, $3::uuid, $4, $5, clock_timestamp()
 FROM control_plane.provider_accounts a JOIN control_plane.provider_credential_revisions c ON c.id = a.current_credential_revision_id
 WHERE a.id = $1::uuid RETURNING id, provider_account_id
) UPDATE control_plane.provider_accounts a SET current_credential_revision_id = inserted.id, version = a.version + 1, updated_at = clock_timestamp()
FROM inserted WHERE a.id = inserted.provider_account_id`, accountID, current.SecretName, current.SecretUID, current.SecretResourceVersion, current.ContentSHA256); err != nil {
		t.Fatal("publish forward credential fixture")
	}
	// Чужой account содержит exact descriptor, которого нет в истории bootstrap account.
	foreign := ProviderCredentialConfig{SecretName: "runtime-provider-foreign-history", SecretUID: "10000000-0000-4000-8000-000000001152", SecretResourceVersion: "1152", ContentSHA256: strings.Repeat("b", 64)}
	if _, err := pool.Exec(ctx, `WITH account AS (
 INSERT INTO control_plane.provider_accounts (ref, organization_id, definition_key, stable_key, name, state, enabled, created_by)
 SELECT 'pac_history_foreign_fixture', organization_id, definition_key, 'history-foreign-fixture', 'Historical foreign fixture', 'PENDING_AUTHORIZATION', false, created_by
 FROM control_plane.provider_accounts WHERE id = $1::uuid RETURNING id, organization_id
) INSERT INTO control_plane.provider_credential_revisions
 (ref, organization_id, provider_account_id, revision_number, secret_name, secret_uid, secret_resource_version, content_sha256, observed_at)
 SELECT 'pcr_history_foreign_fixture', organization_id, id, 1, $2, $3::uuid, $4, $5, clock_timestamp() FROM account`, accountID, foreign.SecretName, foreign.SecretUID, foreign.SecretResourceVersion, foreign.ContentSHA256); err != nil {
		t.Fatal("create foreign history fixture")
	}
	configure := func(config ProviderCredentialConfig) {
		t.Helper()
		if err := repository.ConfigureProviderCredential(config); err != nil {
			t.Fatal("configure history fixture")
		}
	}
	configure(current)
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap current forward revision: %v", err)
	}
	defer configure(current)
	// Полный metadata state ловит bump version, новые cleanup tasks и revision.
	readback := func() string {
		t.Helper()
		var snapshot string
		if err := pool.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM control_plane.provider_accounts a),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM control_plane.provider_credential_revisions c),
 (SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM control_plane.provider_credential_cleanup_tasks c)
)::text`).Scan(&snapshot); err != nil {
			t.Fatal("read history state")
		}
		return snapshot
	}
	before := readback()
	for attempt := 0; attempt < 2; attempt++ {
		configure(historical)
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("restart with historical bootstrap pin: %v", err)
		}
		if readback() != before {
			t.Fatal("historical bootstrap rolled back or changed current state")
		}
	}
	for name, mutate := range map[string]func(*ProviderCredentialConfig){
		"name":             func(c *ProviderCredentialConfig) { c.SecretName = "unknown-history-name" },
		"uid":              func(c *ProviderCredentialConfig) { c.SecretUID = "10000000-0000-4000-8000-000000001153" },
		"resource_version": func(c *ProviderCredentialConfig) { c.SecretResourceVersion = "unknown-history-rv" },
		"digest":           func(c *ProviderCredentialConfig) { c.ContentSHA256 = strings.Repeat("f", 64) },
		"foreign_account":  func(c *ProviderCredentialConfig) { *c = foreign },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := historical
			mutate(&candidate)
			configure(candidate)
			for attempt := 0; attempt < 2; attempt++ {
				if err := repository.Bootstrap(ctx); err == nil {
					t.Fatal("unknown or partial historical pin accepted")
				}
				if readback() != before {
					t.Fatal("rejected historical pin changed state")
				}
			}
		})
	}
	// Каждый owner predicate проверяется против реально существующего exact pin.
	for _, wrongOrganization := range []bool{false, true} {
		args := pgx.StrictNamedArgs{"organization_id": organizationID, "provider_account_id": accountID,
			"secret_name": historical.SecretName, "secret_uid": historical.SecretUID,
			"secret_resource_version": historical.SecretResourceVersion, "content_sha256": historical.ContentSHA256}
		if wrongOrganization {
			args["organization_id"] = "10000000-0000-4000-8000-000000001154"
		} else {
			args["provider_account_id"] = "10000000-0000-4000-8000-000000001154"
		}
		var matched bool
		if err := pool.QueryRow(ctx, queryProviderCredentialHistoricalBootstrapPin, args).Scan(&matched); err != nil || matched {
			t.Fatal("history query ignored exact owner scope")
		}
	}
}
