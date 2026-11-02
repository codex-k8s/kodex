package platform

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestModelProviderBootstrapComponent proves optional Git-owned registration and restart affinity in disposable PostgreSQL.
// It never contacts a provider or interprets synthetic credential pins as verified secret material.
// TestModelProviderBootstrapComponent проверяет optional Git-owned registration и restart affinity в disposable PostgreSQL.
// Не обращается к provider и не считает synthetic credential pins проверенным secret material.
func TestModelProviderBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("open disposable PostgreSQL")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	configureComponentArtifactScanner(t, repository)
	configureComponentProviderAndRoleImages(t, repository)
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1", AddressCIDRs: []string{"192.0.2.4/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "vendor/model-v1", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	if err := repository.ConfigureModelProvider(config, "vendor/model-v1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatal("gateway bootstrap failed", err)
		}
	}
	var provider, model string
	if err := pool.QueryRow(ctx, `SELECT definition_key FROM control_plane.provider_accounts WHERE stable_key=$1`, "default-"+config.DefinitionKey()).Scan(&provider); err != nil || provider != config.DefinitionKey() {
		t.Fatal("bootstrap credential affinity changed")
	}
	if err := pool.QueryRow(ctx, `SELECT provider,model FROM control_plane.runtime_profiles WHERE stable_key=$1`, repository.defaultRuntimeProfileKey()).Scan(&provider, &model); err != nil || provider != config.DefinitionKey() || model != "vendor/model-v1" {
		t.Fatal("default runtime profile affinity changed")
	}
	var organizationID, agentRef string
	if err := pool.QueryRow(ctx, `SELECT organization_id::text,ref FROM control_plane.agents WHERE system_key='system-assistant'`).Scan(&organizationID, &agentRef); err != nil {
		t.Fatal("system assistant lookup failed")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	agents, readErr := readProviderUsageAgents(ctx, tx, organizationID, []string{agentRef})
	rollbackErr := tx.Rollback(ctx)
	if readErr != nil || rollbackErr != nil || agents[agentRef].Provider != config.DefinitionKey() || agents[agentRef].NativeToolCount != 0 {
		t.Fatal("managed assistant preflight snapshot failed", readErr)
	}
	changed := config
	changed.BaseURL = "https://other.example.invalid/v1"
	if err := repository.ConfigureModelProvider(changed, "vendor/model-v1"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err == nil {
		t.Fatal("existing credential endpoint was overwritten")
	}
	var endpoint string
	if err := pool.QueryRow(ctx, `SELECT capabilities->'modelProvider'->>'base_url' FROM control_plane.provider_definitions WHERE stable_key=$1`, config.DefinitionKey()).Scan(&endpoint); err != nil || endpoint != config.BaseURL {
		t.Fatal("failed reconcile changed endpoint")
	}
}
