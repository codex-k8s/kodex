package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// querySTTProviderFixture creates a separate synthetic speech credential; it cannot reuse the agent gateway identity.
// querySTTProviderFixture создаёт отдельный тестовый ключ речи без повторного использования identity модельного шлюза.
//
//go:embed testdata/sql/stt_provider_fixture.sql
var querySTTProviderFixture string

// querySTTProviderFixtureActivate publishes the newly inserted immutable fixture revision after its transaction is visible.
// querySTTProviderFixtureActivate публикует новую неизменяемую тестовую ревизию после видимости её транзакции.
//
//go:embed testdata/sql/stt_provider_fixture_activate.sql
var querySTTProviderFixtureActivate string

// createSTTProviderFixture installs metadata only for the fixed OpenAI speech adapter without contacting it or loading real credentials.
// createSTTProviderFixture создаёт лишь метаданные фиксированного адаптера речи OpenAI без сетевых вызовов и настоящих ключей.
func createSTTProviderFixture(t *testing.T, ctx context.Context, repository *Repository, organizationID, actorID string) string {
	t.Helper()
	config := *repository.defaultModelProvider()
	config.ProviderID = "openai"
	config.BaseURL = "https://api.openai.com/v1"
	config.API = runtimecontract.ModelAPIResponses
	config.RouteKind = "DIRECT"
	config.RouteVersion = ""
	if !config.SupportsOpenAITranscription() {
		t.Fatal("invalid speech provider fixture")
	}
	capabilities, err := json.Marshal(map[string]any{"modelProvider": config, "managedBy": "git", "runtimeEngine": "opencode"})
	if err != nil {
		t.Fatal("encode speech provider fixture")
	}
	var ref, credential string
	if err := repository.pool.QueryRow(ctx, querySTTProviderFixture, organizationID, actorID, capabilities).Scan(&ref, &credential); err != nil {
		t.Fatal("create speech provider fixture", err)
	}
	if _, err := repository.pool.Exec(ctx, querySTTProviderFixtureActivate, ref, credential); err != nil {
		t.Fatal("activate speech provider fixture", err)
	}
	return ref
}
