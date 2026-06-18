package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/entity"
)

const (
	developerSmokeTemplateKey = "developer_smoke"
	developerImplementTaskKey = "implement_task"
	developerFixReviewKey     = "fix_review"
	reviewPRTemplateKey       = "review_pr"
)

type promptTemplateRunData struct {
	ID      string
	Profile string
	Role    string
	Locale  string
}

type promptTemplateAgentData struct {
	Profile          string
	Role             string
	KubernetesAccess string
	SandboxMode      string
	ConfigOverlay    string
}

type promptTemplateRepositoryData struct {
	Provider string
	Owner    string
	Name     string
	FullName string
}

type promptTemplateTaskData struct {
	Title      string
	Body       string
	BaseBranch string
	HeadBranch string
}

type promptTemplatePullRequestData struct {
	Number     int
	URL        string
	Title      string
	BaseBranch string
	HeadBranch string
}

type promptTemplateGitHubData struct {
	Account     string
	TokenEnv    string
	UsernameEnv string
	EmailEnv    string
}

type promptTemplateLocaleData struct {
	Code     string
	Language string
}

type promptTemplateData struct {
	Run         promptTemplateRunData
	Agent       promptTemplateAgentData
	Repository  promptTemplateRepositoryData
	Task        promptTemplateTaskData
	PullRequest promptTemplatePullRequestData
	GitHub      promptTemplateGitHubData
	Locale      promptTemplateLocaleData
}

type RolePromptInput struct {
	Project      entity.Project
	Role         entity.AgentRole
	Chat         entity.Chat
	Repositories []entity.ProjectRepository
	UserMessage  string
	Locale       promptTemplateLocaleData
}

type rolePromptProjectData struct {
	ID          int64
	Name        string
	Slug        string
	Description string
}

type rolePromptRoleData struct {
	ID               int64
	Name             string
	Type             string
	Description      string
	KubernetesAccess string
	SandboxMode      string
	ConfigOverlay    string
}

type rolePromptChatData struct {
	ID              int64
	Name            string
	Type            string
	RootGitHubIssue string
	WorkPolicy      string
}

type rolePromptRepositoryData struct {
	Provider      string
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
}

type rolePromptTaskData struct {
	Body string
}

type rolePromptData struct {
	Project      rolePromptProjectData
	Role         rolePromptRoleData
	Chat         rolePromptChatData
	Repositories []rolePromptRepositoryData
	Task         rolePromptTaskData
	Locale       promptTemplateLocaleData
}

func renderAgentPromptTemplate(body string, data promptTemplateData) (string, error) {
	tpl, err := template.New("agent-prompt").
		Option("missingkey=error").
		Funcs(promptTemplateFuncMap()).
		Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("render prompt template: %w", err)
	}
	text := strings.TrimSpace(rendered.String())
	if text == "" {
		return "", fmt.Errorf("render prompt template: rendered prompt is empty")
	}
	return text + "\n", nil
}

func BuildRolePrompt(input RolePromptInput) (string, error) {
	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage == "" {
		return "", fmt.Errorf("user message is required")
	}
	if strings.TrimSpace(input.Role.PromptTemplate) == "" {
		return buildRawRolePrompt(input, userMessage), nil
	}
	return RenderRolePromptTemplate(input.Role.PromptTemplate, rolePromptTemplateData(input))
}

func buildRawRolePrompt(input RolePromptInput, userMessage string) string {
	var body strings.Builder
	body.WriteString("# User instruction\n\n")
	body.WriteString(userMessage)
	body.WriteString("\n\n# Matter-codex context\n\n")
	if strings.TrimSpace(input.Project.Name) != "" {
		body.WriteString("- Project: ")
		body.WriteString(input.Project.Name)
		if strings.TrimSpace(input.Project.Slug) != "" {
			body.WriteString(" (")
			body.WriteString(input.Project.Slug)
			body.WriteString(")")
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.Name) != "" {
		body.WriteString("- Chat: ")
		body.WriteString(input.Chat.Name)
		if strings.TrimSpace(input.Chat.ChatType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Chat.ChatType)
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Role.Name) != "" {
		body.WriteString("- Role: ")
		body.WriteString(input.Role.Name)
		if strings.TrimSpace(input.Role.RoleType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Role.RoleType)
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Role.Description) != "" {
		body.WriteString("- Role description: ")
		body.WriteString(input.Role.Description)
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.RootGitHubIssue) != "" {
		body.WriteString("- Root GitHub issue: ")
		body.WriteString(input.Chat.RootGitHubIssue)
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.WorkPolicy) != "" {
		body.WriteString("- Work policy: ")
		body.WriteString(input.Chat.WorkPolicy)
		body.WriteString("\n")
	}
	if len(input.Repositories) > 0 {
		body.WriteString("- Repositories:\n")
		for _, repo := range input.Repositories {
			body.WriteString("  - ")
			body.WriteString(repo.Provider)
			body.WriteString(":")
			body.WriteString(repo.FullName())
			if strings.TrimSpace(repo.DefaultBranch) != "" {
				body.WriteString(" branch ")
				body.WriteString(repo.DefaultBranch)
			}
			body.WriteString("\n")
		}
	}
	body.WriteString("- GitHub CLI: use `gh` when the role has a GitHub account. Token/user/email are exposed through GH_TOKEN, GITHUB_TOKEN, GITHUB_USERNAME/GITHUB_USER and GITHUB_EMAIL.\n")
	body.WriteString("- Mattermost MCP: use `mattermost_get_thread` to read this thread, `mattermost_search_chat` for small bounded channel searches, `mattermost_post_thread_update` for progress updates, and `mattermost_request_agent` only when the user or role prompt allows delegating to another agent.\n")
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Response language: ")
		body.WriteString(input.Locale.Language)
		body.WriteString("\n")
	}
	return strings.TrimSpace(body.String()) + "\n"
}

func RenderRolePromptTemplate(body string, data rolePromptData) (string, error) {
	tpl, err := template.New("agent-role-prompt").
		Option("missingkey=error").
		Funcs(promptTemplateFuncMap()).
		Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse role prompt template: %w", err)
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("render role prompt template: %w", err)
	}
	text := strings.TrimSpace(rendered.String())
	if text == "" {
		return "", fmt.Errorf("render role prompt template: rendered prompt is empty")
	}
	return text + "\n", nil
}

func rolePromptTemplateData(input RolePromptInput) rolePromptData {
	locale := input.Locale
	if strings.TrimSpace(locale.Code) == "" {
		locale.Code = "en"
	}
	if strings.TrimSpace(locale.Language) == "" {
		locale.Language = "English"
	}
	repositories := make([]rolePromptRepositoryData, 0, len(input.Repositories))
	for _, repo := range input.Repositories {
		repositories = append(repositories, rolePromptRepositoryData{
			Provider:      repo.Provider,
			Owner:         repo.Owner,
			Name:          repo.Name,
			FullName:      repo.FullName(),
			DefaultBranch: repo.DefaultBranch,
		})
	}
	return rolePromptData{
		Project: rolePromptProjectData{
			ID:          input.Project.ID,
			Name:        input.Project.Name,
			Slug:        input.Project.Slug,
			Description: input.Project.Description,
		},
		Role: rolePromptRoleData{
			ID:               input.Role.ID,
			Name:             input.Role.Name,
			Type:             input.Role.RoleType,
			Description:      input.Role.Description,
			KubernetesAccess: input.Role.KubernetesAccess,
			SandboxMode:      input.Role.SandboxMode,
			ConfigOverlay:    input.Role.ConfigOverlay,
		},
		Chat: rolePromptChatData{
			ID:              input.Chat.ID,
			Name:            input.Chat.Name,
			Type:            input.Chat.ChatType,
			RootGitHubIssue: input.Chat.RootGitHubIssue,
			WorkPolicy:      input.Chat.WorkPolicy,
		},
		Repositories: repositories,
		Task:         rolePromptTaskData{Body: strings.TrimSpace(input.UserMessage)},
		Locale:       locale,
	}
}

func SampleRolePromptData(roleName string, roleType string, locale promptTemplateLocaleData) rolePromptData {
	return rolePromptTemplateData(RolePromptInput{
		Project: entity.Project{ID: 1, Name: "Sample Project", Slug: "sample-project", Description: "Sample project context."},
		Role: entity.AgentRole{
			ID:               1,
			Name:             defaultString(roleName, "worker"),
			RoleType:         defaultString(roleType, "worker"),
			Description:      "Sample role",
			KubernetesAccess: "read-only",
			SandboxMode:      "danger-full-access",
		},
		Chat: entity.Chat{ID: 1, Name: "Sample chat", ChatType: "single_custom", RootGitHubIssue: "https://github.com/org/repo/issues/1"},
		Repositories: []entity.ProjectRepository{{
			Provider:      "github",
			Owner:         "codex-k8s",
			Name:          "matter-codex",
			DefaultBranch: "main",
		}},
		UserMessage: "Update a safe documentation file.",
		Locale:      locale,
	})
}

func (svc *SlashCommandService) renderStoredPromptTemplate(ctx context.Context, profileName string, templateKey string, data promptTemplateData) (string, error) {
	if !svc.cfg.StorageReady || svc.cfg.Store == nil {
		return "", fmt.Errorf("storage is not ready")
	}
	item, err := svc.cfg.Store.GetAgentPromptTemplate(ctx, profileName, templateKey)
	if err != nil {
		return "", err
	}
	return renderAgentPromptTemplate(item.Body, data)
}

func promptTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"default": func(fallback string, value string) string {
			if strings.TrimSpace(value) == "" {
				return fallback
			}
			return value
		},
		"join": func(separator string, values []string) string {
			return strings.Join(values, separator)
		},
		"lower": strings.ToLower,
		"now": func() string {
			return time.Now().UTC().Format(time.RFC3339)
		},
		"trim":  strings.TrimSpace,
		"upper": strings.ToUpper,
	}
}

func promptTemplateReferenceData() map[string]any {
	return map[string]any{
		"RunPlaceholders":         "{{.Run.ID}}, {{.Run.Profile}}, {{.Run.Role}}, {{.Run.Locale}}",
		"AgentPlaceholders":       "{{.Agent.Profile}}, {{.Agent.Role}}, {{.Agent.KubernetesAccess}}, {{.Agent.SandboxMode}}, {{.Agent.ConfigOverlay}}",
		"RepositoryPlaceholders":  "{{.Repository.Provider}}, {{.Repository.Owner}}, {{.Repository.Name}}, {{.Repository.FullName}}",
		"TaskPlaceholders":        "{{.Task.Title}}, {{.Task.Body}}, {{.Task.BaseBranch}}, {{.Task.HeadBranch}}",
		"PullRequestPlaceholders": "{{.PullRequest.Number}}, {{.PullRequest.URL}}, {{.PullRequest.Title}}, {{.PullRequest.BaseBranch}}, {{.PullRequest.HeadBranch}}",
		"GitHubPlaceholders":      "{{.GitHub.Account}}, {{.GitHub.TokenEnv}}, {{.GitHub.UsernameEnv}}, {{.GitHub.EmailEnv}}",
		"LocalePlaceholders":      "{{.Locale.Code}}, {{.Locale.Language}}",
		"DefaultFuncExample":      `{{default "fallback" .Task.Title}}`,
		"LowerFuncExample":        "{{lower .Repository.FullName}}",
		"UpperFuncExample":        "{{upper .Run.Role}}",
		"TrimFuncExample":         "{{trim .Task.Body}}",
		"JoinFuncExample":         `{{join ", " .SomeStringSlice}}`,
		"NowFuncExample":          "{{now}}",
	}
}

func samplePromptTemplateData(profileName string, templateKey string, locale promptTemplateLocaleData) promptTemplateData {
	if strings.TrimSpace(locale.Code) == "" {
		locale.Code = "en"
	}
	if strings.TrimSpace(locale.Language) == "" {
		locale.Language = "English"
	}
	data := promptTemplateData{
		Run: promptTemplateRunData{
			ID:      "sample-run",
			Profile: profileName,
			Role:    profileName,
			Locale:  locale.Code,
		},
		Agent: promptTemplateAgentData{
			Profile:          profileName,
			Role:             profileName,
			KubernetesAccess: "read-only",
			SandboxMode:      "danger-full-access",
		},
		Repository: promptTemplateRepositoryData{
			Provider: "github",
			Owner:    "codex-k8s",
			Name:     "matter-codex",
			FullName: "codex-k8s/matter-codex",
		},
		Task: promptTemplateTaskData{
			Title:      "Sample task",
			Body:       "Update a safe documentation file.",
			BaseBranch: "main",
			HeadBranch: "matter-codex-dev-sample-run",
		},
		PullRequest: promptTemplatePullRequestData{
			Number:     9,
			URL:        "https://github.com/codex-k8s/matter-codex/pull/9",
			Title:      "Sample pull request",
			BaseBranch: "main",
			HeadBranch: "feature/sample",
		},
		GitHub: promptTemplateGitHubData{
			Account:     "primary",
			TokenEnv:    "GH_TOKEN / GITHUB_TOKEN",
			UsernameEnv: "GITHUB_USERNAME / GITHUB_USER",
			EmailEnv:    "GITHUB_EMAIL",
		},
		Locale: locale,
	}
	if templateKey == reviewPRTemplateKey {
		data.Run.Role = "reviewer"
		data.Agent.Role = "reviewer"
		data.GitHub.Account = "primary"
	}
	if templateKey == developerSmokeTemplateKey || templateKey == developerImplementTaskKey || templateKey == developerFixReviewKey {
		data.Run.Role = "developer"
		data.Agent.Role = "developer"
		data.GitHub.Account = "agent"
	}
	if templateKey == developerFixReviewKey {
		data.PullRequest.Number = 10
		data.PullRequest.URL = "https://github.com/codex-k8s/matter-codex/pull/10"
		data.Task.HeadBranch = "matter-codex-flow-sample-run"
	}
	return data
}

func consumeToken(raw string) (string, string) {
	raw = strings.TrimLeftFunc(raw, unicode.IsSpace)
	if raw == "" {
		return "", ""
	}
	for index, char := range raw {
		if unicode.IsSpace(char) {
			return raw[:index], strings.TrimLeftFunc(raw[index:], unicode.IsSpace)
		}
	}
	return raw, ""
}
