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
			if !strings.Contains(body, "{{.Locale.Language}}") || !strings.Contains(body, "MatterCodex") || !strings.Contains(body, "MCP") {
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

func TestManagerPromptSeedDocumentsDynamicCrossChatRouting(t *testing.T) {
	seed, ok := promptSeedForProfileTemplate("manager", managerCoordinateTaskKey)
	if !ok {
		t.Fatal("manager prompt seed is missing")
	}
	body, err := promptSeedMarkdown(seed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown() error = %v", err)
	}
	for _, expected := range []string{
		"mattermost_list_chats(target_agent)",
		"mattermost_get_chat(chat)",
		"mattermost_start_agent_thread(target_chat, target_agent, title, message, work_item_key)",
		"Конфигурация проекта в MatterCodex является источником истины",
		"Не угадывай и не зашивай имя чата",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("manager seed missing cross-chat routing contract %q:\n%s", expected, body)
		}
	}
}

func TestManagedReviewTriagePromptContract(t *testing.T) {
	managerSeed, ok := promptSeedForProfileTemplate("manager", managerCoordinateTaskKey)
	if !ok {
		t.Fatal("manager prompt seed is missing")
	}
	managerBody, err := promptSeedMarkdown(managerSeed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown(manager) error = %v", err)
	}
	for _, expected := range []string{
		"Merge blocker",
		"MVP follow-up",
		"Informational",
		"GitHub GraphQL `reviewThreads`",
		"labels `mvp-follow-up`",
		"ограниченную неблокирующую волну",
	} {
		if !strings.Contains(managerBody, expected) {
			t.Fatalf("manager seed missing review triage contract %q:\n%s", expected, managerBody)
		}
	}

	reviewSeed, ok := promptSeedForProfileTemplate("reviewer", reviewPRTemplateKey)
	if !ok {
		t.Fatal("review prompt seed is missing")
	}
	reviewBody, err := promptSeedMarkdown(reviewSeed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown(reviewer) error = %v", err)
	}
	for _, expected := range []string{
		"Merge blocker",
		"MVP follow-up",
		"Informational",
		"labels `mvp-follow-up`",
		"URL Issue",
		"почему замечание не является blocker",
		"GitHub GraphQL `reviewThreads`",
	} {
		if !strings.Contains(reviewBody, expected) {
			t.Fatalf("review seed missing review triage contract %q:\n%s", expected, reviewBody)
		}
	}
	issueURLIndex := strings.Index(reviewBody, "URL Issue")
	reasonIndex := strings.Index(reviewBody, "почему замечание не является blocker")
	resolveIndex := strings.Index(reviewBody, "только после этого разреши thread")
	if issueURLIndex < 0 || reasonIndex < 0 || resolveIndex < 0 || issueURLIndex > resolveIndex || reasonIndex > resolveIndex {
		t.Fatalf("review seed must require Issue URL and non-blocker reason before resolving the thread:\n%s", reviewBody)
	}

	securitySeed, ok := promptSeedForAgentRole("security-reviewer", "security")
	if !ok || securitySeed.SourceProfile != "reviewer" || securitySeed.TemplateKey != reviewPRTemplateKey {
		t.Fatalf("security review role does not resolve to shared review triage seed: %#v, ok=%v", securitySeed, ok)
	}

	directorSeed, ok := promptSeedForProfileTemplate("director", directorCoordinatePortfolioKey)
	if !ok {
		t.Fatal("director prompt seed is missing")
	}
	directorBody, err := promptSeedMarkdown(directorSeed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown(director) error = %v", err)
	}
	for _, expected := range []string{"label `mvp-follow-up`", "неблокирующую волну", "не запускает новый `improver` рекурсивно"} {
		if !strings.Contains(directorBody, expected) {
			t.Fatalf("director seed missing follow-up contract %q:\n%s", expected, directorBody)
		}
	}

	improverSeed, ok := promptSeedForProfileTemplate("improver", improverFeedbackTaskKey)
	if !ok {
		t.Fatal("improver prompt seed is missing")
	}
	improverBody, err := promptSeedMarkdown(improverSeed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown(improver) error = %v", err)
	}
	if !strings.Contains(improverBody, "слияние результата текущего цикла `improver` основанием для рекурсивного запуска") {
		t.Fatalf("improver seed missing terminal-cycle contract:\n%s", improverBody)
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
