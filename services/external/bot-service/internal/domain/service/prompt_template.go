package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode"
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
	Profile string
	Role    string
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
		"AgentPlaceholders":       "{{.Agent.Profile}}, {{.Agent.Role}}",
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
			Profile: profileName,
			Role:    profileName,
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
