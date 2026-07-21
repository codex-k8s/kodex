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
			rendered, err := renderAgentPromptTemplate(body, samplePromptTemplateData(seed.SourceProfile, seed.TemplateKey, locale))
			if err != nil {
				t.Fatalf("renderAgentPromptTemplate() error = %v", err)
			}
			for _, forbidden := range []string{"GitHub-аккаунт: primary", "GitHub-аккаунт: agent", "Ожидаемый аутентифицированный GitHub login"} {
				if strings.Contains(rendered, forbidden) {
					t.Fatalf("active seed rendered platform-owned GitHub identity %q:\n%s", forbidden, rendered)
				}
			}
			if _, err := RenderRolePromptTemplate(body, SampleRolePromptData(seed.SourceProfile, seed.Role, locale)); err != nil {
				t.Fatalf("RenderRolePromptTemplate() error = %v", err)
			}
		})
	}
}

func TestLegacyCustomPromptCannotRenderPlatformGitHubIdentity(t *testing.T) {
	data := rolePromptTemplateData(RolePromptInput{
		Project:       entity.Project{ID: 1, Name: "Project", Slug: "project"},
		Role:          entity.AgentRole{ID: 1, Name: "developer", RoleType: "worker", GitHubAccountName: "platform-owner"},
		GitHubAccount: entity.GitHubAccount{Name: "platform-owner", Username: "owner-login"},
		Locale:        promptTemplateLocaleData{Code: texti18n.RussianLocale, Language: "Russian"},
	})
	rendered, err := RenderRolePromptTemplate(`{{if .GitHub.Account}}account={{.GitHub.Account}}{{end}}{{if .GitHub.Username}} login={{.GitHub.Username}}{{end}} token={{.GitHub.TokenEnv}}`, data)
	if err != nil {
		t.Fatalf("renderAgentPromptTemplate() error = %v", err)
	}
	if strings.Contains(rendered, "platform-owner") || strings.Contains(rendered, "owner-login") || !strings.Contains(rendered, "GH_TOKEN") {
		t.Fatalf("legacy prompt exposed platform identity or lost environment contract: %q", rendered)
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
		"prototype-accelerated",
		"один reviewer pass и один security pass",
		"post-merge reviewer и security",
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
	for _, expected := range []string{"mvp-follow-up", "до шести независимых manager-волн", "merged PR без label `improved`"} {
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
	for _, expected := range []string{"merged PR за период без label `improved`", "добавь ему `improved`", "очередным ежедневным batch"} {
		if !strings.Contains(improverBody, expected) {
			t.Fatalf("improver seed missing daily batch contract %q:\n%s", expected, improverBody)
		}
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

func TestSeedDefaultAgentPromptTemplateUpgradesOnlyUnmodifiedSeedCopies(t *testing.T) {
	seed, ok := promptSeedForProfileTemplate("manager", managerCoordinateTaskKey)
	if !ok {
		t.Fatal("manager prompt seed is missing")
	}
	previousBodies, err := promptSeedPreviousBodies(seed)
	if err != nil {
		t.Fatalf("promptSeedPreviousBodies() error = %v", err)
	}
	if len(previousBodies) != 2 {
		t.Fatalf("previous bodies = %d, want 2", len(previousBodies))
	}
	want, err := promptSeedMarkdown(seed)
	if err != nil {
		t.Fatalf("promptSeedMarkdown() error = %v", err)
	}
	previousBody := previousBodies[len(previousBodies)-1]
	for _, profileState := range []string{"missing", "current", "custom"} {
		t.Run(profileState, func(t *testing.T) {
			store := &fakeAdminStore{
				promptTemplates: map[string]entity.AgentPromptTemplate{},
				agentRoles: map[int64]entity.AgentRole{
					1: {ID: 1, ProjectID: 1, Name: "manager", RoleType: "manager", PromptTemplate: previousBody},
					2: {ID: 2, ProjectID: 2, Name: "manager", RoleType: "manager", PromptTemplate: "owner customized role prompt"},
				},
			}
			key := promptTemplateMapKey(seed.SourceProfile, seed.TemplateKey)
			switch profileState {
			case "current":
				store.promptTemplates[key] = entity.AgentPromptTemplate{ProfileName: seed.SourceProfile, TemplateKey: seed.TemplateKey, Body: want}
			case "custom":
				store.promptTemplates[key] = entity.AgentPromptTemplate{ProfileName: seed.SourceProfile, TemplateKey: seed.TemplateKey, Body: "owner customized profile template"}
			}

			changed, err := seedDefaultAgentPromptTemplate(context.Background(), store, seed)
			if err != nil {
				t.Fatalf("seedDefaultAgentPromptTemplate() error = %v", err)
			}
			if !changed {
				t.Fatal("seedDefaultAgentPromptTemplate() did not report an upgrade")
			}
			wantProfile := want
			if profileState == "custom" {
				wantProfile = "owner customized profile template"
			}
			if got := store.promptTemplates[key].Body; got != wantProfile {
				t.Fatalf("profile template = %q, want %q", got, wantProfile)
			}
			if got := store.agentRoles[1].PromptTemplate; got != want {
				t.Fatalf("unmodified role prompt was not upgraded")
			}
			if got := store.agentRoles[2].PromptTemplate; got != "owner customized role prompt" {
				t.Fatalf("custom role prompt was overwritten: %q", got)
			}
		})
	}
}
