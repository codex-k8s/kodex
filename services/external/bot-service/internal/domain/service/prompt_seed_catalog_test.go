package service

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestPromptSeedCatalogMarkdownRenders(t *testing.T) {
	locale := promptTemplateLocaleData{Code: texti18n.RussianLocale, Language: "Russian"}
	for _, seed := range promptSeedCatalog() {
		t.Run(seed.SourceProfile+"/"+seed.TemplateKey, func(t *testing.T) {
			body, err := promptSeedMarkdown(seed)
			if err != nil {
				t.Fatalf("promptSeedMarkdown() error = %v", err)
			}
			if !strings.Contains(body, "{{.Locale.Language}}") || !strings.Contains(body, "mattermost_request_agent") {
				t.Fatalf("seed is missing locale/MCP contract:\n%s", body)
			}
			if _, err := renderAgentPromptTemplate(body, samplePromptTemplateData(seed.SourceProfile, seed.TemplateKey, locale)); err != nil {
				t.Fatalf("renderAgentPromptTemplate() error = %v", err)
			}
			if _, err := RenderRolePromptTemplate(body, SampleRolePromptData(seed.SourceProfile, seed.Role, locale)); err != nil {
				t.Fatalf("RenderRolePromptTemplate() error = %v", err)
			}
		})
	}
}

func TestSeedDefaultAgentPromptTemplatesDoesNotOverwriteExistingTemplates(t *testing.T) {
	store := &fakeAdminStore{
		promptTemplates: map[string]entity.AgentPromptTemplate{
			promptTemplateMapKey("developer", developerImplementTaskKey): {
				ProfileName: "developer",
				TemplateKey: developerImplementTaskKey,
				Body:        "custom owner-edited template",
			},
		},
	}

	created, err := SeedDefaultAgentPromptTemplates(context.Background(), store)
	if err != nil {
		t.Fatalf("SeedDefaultAgentPromptTemplates() error = %v", err)
	}
	if created == 0 {
		t.Fatalf("created = %d, want default templates to be inserted", created)
	}
	item, err := store.GetAgentPromptTemplate(context.Background(), "developer", developerImplementTaskKey)
	if err != nil {
		t.Fatalf("GetAgentPromptTemplate() error = %v", err)
	}
	if item.Body != "custom owner-edited template" {
		t.Fatalf("existing template was overwritten: %q", item.Body)
	}
	if _, err := store.GetAgentPromptTemplate(context.Background(), "qa-bot", qaRegressionTaskKey); err != nil {
		t.Fatalf("qa-bot seed was not inserted: %v", err)
	}
}
