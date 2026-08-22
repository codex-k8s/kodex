package platform

import (
	"context"
	_ "embed"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed sql/bootstrap_component_readback.sql
	bootstrapComponentReadbackQuery string
	//go:embed sql/bootstrap_component_disable_system_assistant.sql
	bootstrapComponentDisableSystemAssistantQuery string
	//go:embed sql/bootstrap_component_delete_system_assistant.sql
	bootstrapComponentDeleteSystemAssistantQuery string
	//go:embed sql/bootstrap_component_replace_core_prompt.sql
	bootstrapComponentReplaceCorePromptQuery string
)

func TestBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("MATTERCODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("MATTERCODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5")
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	assertBootstrapReadback(t, ctx, pool)

	for name, query := range map[string]string{
		"disable system assistant": bootstrapComponentDisableSystemAssistantQuery,
		"delete system assistant":  bootstrapComponentDeleteSystemAssistantQuery,
		"replace core prompt":      bootstrapComponentReplaceCorePromptQuery,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("protected system state was changed")
			}
		})
	}
	assertBootstrapReadback(t, ctx, pool)
}

func assertBootstrapReadback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var organizationCount, ownerContractCount, systemAssistantCount, corePromptCount int
	var assistantRuntimeCount, capabilityCount, integrationDefinitionCount, completedBootstrapCount int
	if err := pool.QueryRow(ctx, bootstrapComponentReadbackQuery).Scan(
		&organizationCount, &ownerContractCount, &systemAssistantCount, &corePromptCount,
		&assistantRuntimeCount, &capabilityCount, &integrationDefinitionCount, &completedBootstrapCount,
	); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if organizationCount != 1 || ownerContractCount != 1 || systemAssistantCount != 1 ||
		corePromptCount != 1 || assistantRuntimeCount != 1 || capabilityCount != 8 ||
		integrationDefinitionCount != 3 || completedBootstrapCount != 1 {
		t.Fatalf("unexpected bootstrap state: organization=%d owner_contract=%d assistant=%d core_prompt=%d runtime=%d capabilities=%d integrations=%d completed=%d",
			organizationCount, ownerContractCount, systemAssistantCount, corePromptCount,
			assistantRuntimeCount, capabilityCount, integrationDefinitionCount, completedBootstrapCount)
	}
}
