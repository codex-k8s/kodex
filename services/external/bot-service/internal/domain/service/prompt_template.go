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

type promptTemplateData struct {
	Run         promptTemplateRunData
	Agent       promptTemplateAgentData
	Repository  promptTemplateRepositoryData
	Task        promptTemplateTaskData
	PullRequest promptTemplatePullRequestData
	GitHub      promptTemplateGitHubData
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

func promptTemplateReferenceMarkdown() string {
	return strings.TrimSpace(`Доступные placeholders:

- {{.Run.ID}}, {{.Run.Profile}}, {{.Run.Role}}, {{.Run.Locale}}
- {{.Agent.Profile}}, {{.Agent.Role}}
- {{.Repository.Provider}}, {{.Repository.Owner}}, {{.Repository.Name}}, {{.Repository.FullName}}
- {{.Task.Title}}, {{.Task.Body}}, {{.Task.BaseBranch}}, {{.Task.HeadBranch}}
- {{.PullRequest.Number}}, {{.PullRequest.URL}}, {{.PullRequest.Title}}, {{.PullRequest.BaseBranch}}, {{.PullRequest.HeadBranch}}
- {{.GitHub.Account}}, {{.GitHub.TokenEnv}}, {{.GitHub.UsernameEnv}}, {{.GitHub.EmailEnv}}

Доступные функции:

- {{default "fallback" .Task.Title}}
- {{lower .Repository.FullName}}
- {{upper .Run.Role}}
- {{trim .Task.Body}}
- {{join ", " .SomeStringSlice}}
- {{now}}

Бот ждет Markdown-шаблон на Go text/template. Перед сохранением шаблон парсится и тестово рендерится на sample context.`)
}

func samplePromptTemplateData(profileName string, templateKey string, locale string) promptTemplateData {
	data := promptTemplateData{
		Run: promptTemplateRunData{
			ID:      "sample-run",
			Profile: profileName,
			Role:    profileName,
			Locale:  locale,
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
	}
	if templateKey == reviewPRTemplateKey {
		data.Run.Role = "reviewer"
		data.Agent.Role = "reviewer"
		data.GitHub.Account = "primary"
	}
	if templateKey == developerSmokeTemplateKey {
		data.GitHub.Account = "agent"
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
