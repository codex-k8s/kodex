package service

import (
	"context"
	"crypto/sha256"
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
	SourceProfile  string
	TemplateKey    string
	Role           string
	Description    string
	FileName       string
	RoleNames      []string
	RoleTypes      []string
	Version        int
	PreviousSHA256 []string
}

func promptSeedCatalog() []promptTemplateSeed {
	seeds := []promptTemplateSeed{
		{
			SourceProfile:  "director",
			TemplateKey:    directorCoordinatePortfolioKey,
			Role:           "director",
			Description:    "Generic top-level project coordinator prompt seed",
			FileName:       "director_coordinate_portfolio.md",
			RoleNames:      []string{"director", "coordinator"},
			RoleTypes:      []string{"director", "coordinator"},
			Version:        2,
			PreviousSHA256: []string{"9757920bae68b4a14732e530d5dfb7f5d8afd6a0fdbe0377035d26e4a945e791"},
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
			SourceProfile:  "reviewer",
			TemplateKey:    reviewPRTemplateKey,
			Role:           "reviewer",
			Description:    "Generic pull request reviewer prompt seed",
			FileName:       "reviewer_review_pr.md",
			RoleNames:      []string{"reviewer", "technical-reviewer", "technical_reviewer", "security", "security-reviewer", "security_reviewer", "lexical-guard", "lexical_guard"},
			RoleTypes:      []string{"reviewer", "security", "security_reviewer", "lexical_guard"},
			Version:        2,
			PreviousSHA256: []string{"54260e1c6e1edec6b01e2f0d6cfeae51bef874811ce2e79ca67d8e597aa97d45"},
		},
		{
			SourceProfile:  "manager",
			TemplateKey:    managerCoordinateTaskKey,
			Role:           "manager",
			Description:    "Generic manager coordination prompt seed",
			FileName:       "manager_coordinate_task.md",
			RoleNames:      []string{"manager"},
			RoleTypes:      []string{"manager"},
			Version:        2,
			PreviousSHA256: []string{"be3491a2b6b9822557bc20ec6e2d27e5c425eb0b0667aed9e877386e7e30cfa2"},
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
			SourceProfile:  "improver",
			TemplateKey:    improverFeedbackTaskKey,
			Role:           "improver",
			Description:    "Generic instruction improvement prompt seed",
			FileName:       "improver_feedback_improvement.md",
			RoleNames:      []string{"improver"},
			RoleTypes:      []string{"improver"},
			Version:        2,
			PreviousSHA256: []string{"3cd624912902adc423658192bd41a9c0a0b2630cadb061644f0c9aa9bf5e07dd"},
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
	existing, err := store.GetAgentPromptTemplate(ctx, seed.SourceProfile, seed.TemplateKey)
	if errors.Is(err, adminrepo.ErrNotFound) {
		_, created, upsertErr := store.UpsertAgentPromptTemplate(ctx, adminrepo.UpsertAgentPromptTemplateInput{
			ProfileName: seed.SourceProfile,
			TemplateKey: seed.TemplateKey,
			Body:        body,
		})
		return created, upsertErr
	}
	if err != nil {
		return false, err
	}
	if existing.Body == body || !matchesPreviousPromptSeedBody(existing.Body, seed.PreviousSHA256) {
		return false, nil
	}
	upgrader, ok := store.(adminrepo.AgentPromptSeedUpgradeRepository)
	if !ok {
		return false, nil
	}
	result, err := upgrader.UpgradeUnmodifiedAgentPromptSeed(ctx, adminrepo.UpgradeAgentPromptSeedInput{
		ProfileName:  seed.SourceProfile,
		TemplateKey:  seed.TemplateKey,
		PreviousBody: existing.Body,
		Body:         body,
		RoleNames:    seed.RoleNames,
		RoleTypes:    seed.RoleTypes,
	})
	if err != nil {
		return false, err
	}
	return result.TemplatesUpdated > 0 || result.RolesUpdated > 0, nil
}

func matchesPreviousPromptSeedBody(body string, digests []string) bool {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
	for _, candidate := range digests {
		if strings.EqualFold(strings.TrimSpace(candidate), digest) {
			return true
		}
	}
	return false
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
