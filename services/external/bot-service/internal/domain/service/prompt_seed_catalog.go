package service

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
)

//go:embed prompt_seeds/*.md
var promptSeedFiles embed.FS

const (
	pmDeliveryStatusTemplateKey     = "delivery_status"
	analystAnalysisTaskTemplateKey  = "analysis_task"
	matterCodexAdminTaskTemplateKey = "admin_task"
)

type promptTemplateSeed struct {
	SourceProfile string
	TemplateKey   string
	Role          string
	Description   string
	FileName      string
	RoleNames     []string
	RoleTypes     []string
}

func promptSeedCatalog() []promptTemplateSeed {
	return []promptTemplateSeed{
		{
			SourceProfile: "director",
			TemplateKey:   directorCoordinatePortfolioKey,
			Role:          "director",
			Description:   "Generic top-level project coordinator prompt seed",
			FileName:      "director_coordinate_portfolio.md",
			RoleNames:     []string{"director", "coordinator"},
			RoleTypes:     []string{"director", "coordinator"},
		},
		{
			SourceProfile: "developer",
			TemplateKey:   developerImplementTaskKey,
			Role:          "developer",
			Description:   "Generic developer implementation prompt seed",
			FileName:      "developer_implement_task.md",
			RoleNames:     []string{"developer", "worker", "backend-developer", "frontend-developer", "deployer"},
			RoleTypes:     []string{"worker", "deployer"},
		},
		{
			SourceProfile: "reviewer",
			TemplateKey:   reviewPRTemplateKey,
			Role:          "reviewer",
			Description:   "Generic pull request reviewer prompt seed",
			FileName:      "reviewer_review_pr.md",
			RoleNames:     []string{"reviewer", "technical-reviewer", "technical_reviewer", "lexical-guard", "lexical_guard"},
			RoleTypes:     []string{"reviewer", "lexical_guard"},
		},
		{
			SourceProfile: "manager",
			TemplateKey:   managerCoordinateTaskKey,
			Role:          "manager",
			Description:   "Generic manager coordination prompt seed",
			FileName:      "manager_coordinate_task.md",
			RoleNames:     []string{"manager"},
			RoleTypes:     []string{"manager"},
		},
		{
			SourceProfile: "architect",
			TemplateKey:   architectDocsTaskKey,
			Role:          "architect",
			Description:   "Generic architecture prompt seed",
			FileName:      "architect_architecture_task.md",
			RoleNames:     []string{"architect"},
			RoleTypes:     []string{"architect"},
		},
		{
			SourceProfile: "docs",
			TemplateKey:   docsDocumentationTaskKey,
			Role:          "docs",
			Description:   "Generic documentation prompt seed",
			FileName:      "docs_documentation_task.md",
			RoleNames:     []string{"docs", "writer", "documentation"},
			RoleTypes:     []string{"writer"},
		},
		{
			SourceProfile: "sre",
			TemplateKey:   sreOperationsTaskKey,
			Role:          "sre",
			Description:   "Generic SRE and deployment prompt seed",
			FileName:      "sre_operations_task.md",
			RoleNames:     []string{"sre"},
			RoleTypes:     []string{"sre"},
		},
		{
			SourceProfile: "qa-bot",
			TemplateKey:   qaRegressionTaskKey,
			Role:          "qa-bot",
			Description:   "Generic QA regression prompt seed",
			FileName:      "qa_bot_regression_task.md",
			RoleNames:     []string{"qa-bot", "qa", "tester"},
		},
		{
			SourceProfile: "ui-designer",
			TemplateKey:   uiDesignerTaskTemplateKey,
			Role:          "designer",
			Description:   "Generic UI/UX designer prompt seed",
			FileName:      "ui_designer_task.md",
			RoleNames:     []string{"ui-designer", "ui-ux", "uiux", "designer", "ux", "ux-designer", "product-designer"},
			RoleTypes:     []string{"designer", "ui_ux", "ux"},
		},
		{
			SourceProfile: "improver",
			TemplateKey:   improverFeedbackTaskKey,
			Role:          "improver",
			Description:   "Generic instruction improvement prompt seed",
			FileName:      "improver_feedback_improvement.md",
			RoleNames:     []string{"improver"},
			RoleTypes:     []string{"improver"},
		},
		{
			SourceProfile: "pm-delivery",
			TemplateKey:   pmDeliveryStatusTemplateKey,
			Role:          "pm_delivery",
			Description:   "Generic PM/delivery status prompt seed",
			FileName:      "pm_delivery_status.md",
			RoleNames:     []string{"pm", "delivery", "pm-delivery", "pm_delivery"},
			RoleTypes:     []string{"pm_delivery"},
		},
		{
			SourceProfile: "analyst",
			TemplateKey:   analystAnalysisTaskTemplateKey,
			Role:          "analyst",
			Description:   "Generic analyst prompt seed",
			FileName:      "analyst_analysis_task.md",
			RoleNames:     []string{"analyst"},
			RoleTypes:     []string{"analyst"},
		},
		{
			SourceProfile: "mattercodex-admin",
			TemplateKey:   matterCodexAdminTaskTemplateKey,
			Role:          "sre",
			Description:   "Generic MatterCodex platform admin prompt seed",
			FileName:      "mattercodex_admin_task.md",
			RoleNames:     []string{"mattercodex-admin", "matter-codex-admin"},
		},
	}
}

func promptSeedsForRole(role string) []promptTemplateSeed {
	role = normalizePromptSeedKey(role)
	var seeds []promptTemplateSeed
	for _, seed := range promptSeedCatalog() {
		if normalizePromptSeedKey(seed.Role) == role || normalizePromptSeedKey(seed.SourceProfile) == role || containsNormalized(seed.RoleTypes, role) || containsNormalized(seed.RoleNames, role) {
			seeds = append(seeds, seed)
		}
	}
	return seeds
}

func promptSeedForAgentRole(roleName string, roleType string) (promptTemplateSeed, bool) {
	name := normalizePromptSeedKey(roleName)
	roleType = normalizePromptSeedKey(roleType)
	for _, seed := range promptSeedCatalog() {
		if containsNormalized(seed.RoleNames, name) {
			return seed, true
		}
	}
	for _, seed := range promptSeedCatalog() {
		if containsNormalized(seed.RoleTypes, roleType) {
			return seed, true
		}
	}
	return promptTemplateSeed{}, false
}

func promptSeedForProfileTemplate(profileName string, templateKey string) (promptTemplateSeed, bool) {
	profileName = normalizePromptSeedKey(profileName)
	templateKey = normalizePromptSeedKey(templateKey)
	for _, seed := range promptSeedCatalog() {
		if normalizePromptSeedKey(seed.SourceProfile) == profileName && normalizePromptSeedKey(seed.TemplateKey) == templateKey {
			return seed, true
		}
	}
	return promptTemplateSeed{}, false
}

func promptSeedMarkdownForProfileTemplate(profileName string, templateKey string) (string, error) {
	seed, ok := promptSeedForProfileTemplate(profileName, templateKey)
	if !ok {
		return "", fmt.Errorf("prompt seed %s/%s is not registered", strings.TrimSpace(profileName), strings.TrimSpace(templateKey))
	}
	return promptSeedMarkdown(seed)
}

func promptSeedMarkdown(seed promptTemplateSeed) (string, error) {
	fileName := strings.TrimSpace(seed.FileName)
	if fileName == "" {
		return "", fmt.Errorf("prompt seed file name is empty for %s/%s", seed.SourceProfile, seed.TemplateKey)
	}
	body, err := promptSeedFiles.ReadFile("prompt_seeds/" + fileName)
	if err != nil {
		return "", fmt.Errorf("read prompt seed %s: %w", fileName, err)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", fmt.Errorf("prompt seed %s is empty", fileName)
	}
	return text + "\n", nil
}

func SeedDefaultAgentPromptTemplates(ctx context.Context, store adminrepo.Repository) (int, error) {
	if store == nil {
		return 0, nil
	}
	created := 0
	for _, seed := range promptSeedCatalog() {
		if _, err := store.GetAgentPromptTemplate(ctx, seed.SourceProfile, seed.TemplateKey); err == nil {
			continue
		} else if !errors.Is(err, adminrepo.ErrNotFound) {
			return created, err
		}
		body, err := promptSeedMarkdown(seed)
		if err != nil {
			return created, err
		}
		if _, newTemplate, err := store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
			ProfileName: seed.SourceProfile,
			TemplateKey: seed.TemplateKey,
			Body:        body,
		}); err != nil {
			return created, err
		} else if newTemplate {
			created++
		}
	}
	return created, nil
}

func normalizePromptSeedKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func containsNormalized(values []string, needle string) bool {
	needle = normalizePromptSeedKey(needle)
	for _, value := range values {
		if normalizePromptSeedKey(value) == needle {
			return true
		}
	}
	return false
}
