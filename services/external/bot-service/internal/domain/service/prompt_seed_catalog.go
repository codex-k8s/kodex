package service

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	adminrepo "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/repository/admin"
)

//go:embed prompt_seeds/*.md prompt_seeds/history/v1/*.md prompt_seeds/history/v2/*.md prompt_seeds/history/v3/*.md
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
	Version       int
	PreviousFiles []string
}

func promptSeedCatalog() []promptTemplateSeed {
	seeds := []promptTemplateSeed{
		{
			SourceProfile: "director",
			TemplateKey:   directorCoordinatePortfolioKey,
			Role:          "director",
			Description:   "Generic top-level project coordinator prompt seed",
			FileName:      "director_coordinate_portfolio.md",
			RoleNames:     []string{"director", "coordinator"},
			RoleTypes:     []string{"director", "coordinator"},
			Version:       3,
			PreviousFiles: []string{"history/v1/director_coordinate_portfolio.md", "history/v2/director_coordinate_portfolio.md"},
		},
		{
			SourceProfile: "developer",
			TemplateKey:   developerImplementTaskKey,
			Role:          "developer",
			Description:   "Generic developer implementation prompt seed",
			FileName:      "developer_implement_task.md",
			RoleNames:     []string{"developer", "worker", "backend-developer", "frontend-developer", "deployer"},
			RoleTypes:     []string{"worker", "deployer"},
			Version:       3,
			PreviousFiles: []string{"history/v1/developer_implement_task.md", "history/v2/developer_implement_task.md"},
		},
		{
			SourceProfile: "reviewer",
			TemplateKey:   reviewPRTemplateKey,
			Role:          "reviewer",
			Description:   "Generic pull request reviewer prompt seed",
			FileName:      "reviewer_review_pr.md",
			RoleNames:     []string{"reviewer", "technical-reviewer", "technical_reviewer", "security", "security-reviewer", "security_reviewer", "lexical-guard", "lexical_guard"},
			RoleTypes:     []string{"reviewer", "security", "security_reviewer", "lexical_guard"},
			Version:       4,
			PreviousFiles: []string{"history/v1/reviewer_review_pr.md", "history/v2/reviewer_review_pr.md", "history/v3/reviewer_review_pr.md"},
		},
		{
			SourceProfile: "manager",
			TemplateKey:   managerCoordinateTaskKey,
			Role:          "manager",
			Description:   "Generic manager coordination prompt seed",
			FileName:      "manager_coordinate_task.md",
			RoleNames:     []string{"manager"},
			RoleTypes:     []string{"manager"},
			Version:       4,
			PreviousFiles: []string{"history/v1/manager_coordinate_task.md", "history/v2/manager_coordinate_task.md", "history/v3/manager_coordinate_task.md"},
		},
		{
			SourceProfile: "architect",
			TemplateKey:   architectDocsTaskKey,
			Role:          "architect",
			Description:   "Generic architecture prompt seed",
			FileName:      "architect_architecture_task.md",
			RoleNames:     []string{"architect"},
			RoleTypes:     []string{"architect"},
			Version:       2,
			PreviousFiles: []string{"history/v1/architect_architecture_task.md"},
		},
		{
			SourceProfile: "docs",
			TemplateKey:   docsDocumentationTaskKey,
			Role:          "docs",
			Description:   "Generic documentation prompt seed",
			FileName:      "docs_documentation_task.md",
			RoleNames:     []string{"docs", "writer", "documentation"},
			RoleTypes:     []string{"writer"},
			Version:       2,
			PreviousFiles: []string{"history/v1/docs_documentation_task.md"},
		},
		{
			SourceProfile: "sre",
			TemplateKey:   sreOperationsTaskKey,
			Role:          "sre",
			Description:   "Generic SRE and deployment prompt seed",
			FileName:      "sre_operations_task.md",
			RoleNames:     []string{"sre"},
			RoleTypes:     []string{"sre"},
			Version:       2,
			PreviousFiles: []string{"history/v1/sre_operations_task.md"},
		},
		{
			SourceProfile: "qa-bot",
			TemplateKey:   qaRegressionTaskKey,
			Role:          "qa-bot",
			Description:   "Generic QA regression prompt seed",
			FileName:      "qa_bot_regression_task.md",
			RoleNames:     []string{"qa-bot", "qa", "tester"},
			Version:       2,
			PreviousFiles: []string{"history/v1/qa_bot_regression_task.md"},
		},
		{
			SourceProfile: "ui-designer",
			TemplateKey:   uiDesignerTaskTemplateKey,
			Role:          "designer",
			Description:   "Generic UI/UX designer prompt seed",
			FileName:      "ui_designer_task.md",
			RoleNames:     []string{"ui-designer", "ui-ux", "uiux", "designer", "ux", "ux-designer", "product-designer"},
			RoleTypes:     []string{"designer", "ui_ux", "ux"},
			Version:       2,
			PreviousFiles: []string{"history/v1/ui_designer_task.md"},
		},
		{
			SourceProfile: "improver",
			TemplateKey:   improverFeedbackTaskKey,
			Role:          "improver",
			Description:   "Generic instruction improvement prompt seed",
			FileName:      "improver_feedback_improvement.md",
			RoleNames:     []string{"improver"},
			RoleTypes:     []string{"improver"},
			Version:       3,
			PreviousFiles: []string{"history/v1/improver_feedback_improvement.md", "history/v2/improver_feedback_improvement.md"},
		},
		{
			SourceProfile: "pm-delivery",
			TemplateKey:   pmDeliveryStatusTemplateKey,
			Role:          "pm_delivery",
			Description:   "Generic PM/delivery status prompt seed",
			FileName:      "pm_delivery_status.md",
			RoleNames:     []string{"pm", "delivery", "pm-delivery", "pm_delivery"},
			RoleTypes:     []string{"pm_delivery"},
			Version:       2,
			PreviousFiles: []string{"history/v1/pm_delivery_status.md"},
		},
		{
			SourceProfile: "analyst",
			TemplateKey:   analystAnalysisTaskTemplateKey,
			Role:          "analyst",
			Description:   "Generic analyst prompt seed",
			FileName:      "analyst_analysis_task.md",
			RoleNames:     []string{"analyst"},
			RoleTypes:     []string{"analyst"},
			Version:       2,
			PreviousFiles: []string{"history/v1/analyst_analysis_task.md"},
		},
		{
			SourceProfile: "mattercodex-admin",
			TemplateKey:   matterCodexAdminTaskTemplateKey,
			Role:          "sre",
			Description:   "Generic MatterCodex platform admin prompt seed",
			FileName:      "mattercodex_admin_task.md",
			RoleNames:     []string{"mattercodex-admin", "matter-codex-admin"},
			Version:       2,
			PreviousFiles: []string{"history/v1/mattercodex_admin_task.md"},
		},
	}
	for index := range seeds {
		if seeds[index].Version == 0 {
			seeds[index].Version = 1
		}
	}
	return seeds
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
	changed := 0
	for _, seed := range promptSeedCatalog() {
		seedChanged, err := seedDefaultAgentPromptTemplate(ctx, store, seed)
		if err != nil {
			return changed, err
		}
		if seedChanged {
			changed++
		}
	}
	return changed, nil
}

func seedDefaultAgentPromptTemplate(ctx context.Context, store adminrepo.Repository, seed promptTemplateSeed) (bool, error) {
	if seed.Version <= 0 {
		return false, fmt.Errorf("prompt seed %s/%s has invalid version", seed.SourceProfile, seed.TemplateKey)
	}
	body, err := promptSeedMarkdown(seed)
	if err != nil {
		return false, err
	}
	previousBodies, err := promptSeedPreviousBodies(seed)
	if err != nil {
		return false, err
	}
	changed := false
	_, err = store.GetAgentPromptTemplate(ctx, seed.SourceProfile, seed.TemplateKey)
	if errors.Is(err, adminrepo.ErrNotFound) {
		_, created, upsertErr := store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
			ProfileName: seed.SourceProfile,
			TemplateKey: seed.TemplateKey,
			Body:        body,
		})
		if upsertErr != nil {
			return false, upsertErr
		}
		changed = created
	}
	if err != nil && !errors.Is(err, adminrepo.ErrNotFound) {
		return false, err
	}
	upgrader, ok := store.(adminrepo.AgentPromptSeedUpgradeRepository)
	if !ok {
		return changed, nil
	}
	for _, previousBody := range previousBodies {
		result, upgradeErr := upgrader.UpgradeUnmodifiedAgentPromptSeed(ctx, adminrepo.UpgradeAgentPromptSeedInput{
			ProfileName:  seed.SourceProfile,
			TemplateKey:  seed.TemplateKey,
			PreviousBody: previousBody,
			Body:         body,
			RoleNames:    seed.RoleNames,
			RoleTypes:    seed.RoleTypes,
		})
		if upgradeErr != nil {
			return false, upgradeErr
		}
		changed = changed || result.TemplatesUpdated > 0 || result.RolesUpdated > 0
	}
	return changed, nil
}

func promptSeedPreviousBodies(seed promptTemplateSeed) ([]string, error) {
	bodies := make([]string, 0, len(seed.PreviousFiles))
	for _, fileName := range seed.PreviousFiles {
		fileName = strings.TrimSpace(fileName)
		if fileName == "" {
			return nil, fmt.Errorf("prompt seed %s/%s has empty previous file", seed.SourceProfile, seed.TemplateKey)
		}
		body, err := promptSeedFiles.ReadFile("prompt_seeds/" + fileName)
		if err != nil {
			return nil, fmt.Errorf("read previous prompt seed %s: %w", fileName, err)
		}
		text := strings.TrimSpace(string(body))
		if text == "" {
			return nil, fmt.Errorf("previous prompt seed %s is empty", fileName)
		}
		bodies = append(bodies, text+"\n")
	}
	return bodies, nil
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
