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

type promptTemplateToolData struct {
	Name    string
	Command string
	Version string
	Purpose string
}

type promptTemplateSecretBindingData struct {
	Name         string
	Kind         string
	Env          string
	File         string
	Availability string
	Purpose      string
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
	Tools       []promptTemplateToolData
	Secrets     []promptTemplateSecretBindingData
	Locale      promptTemplateLocaleData
}

type RolePromptInput struct {
	Project          entity.Project
	Role             entity.AgentRole
	Chat             entity.Chat
	Repositories     []entity.ProjectRepository
	RuntimeVariables []entity.ProjectRuntimeVariable
	UserMessage      string
	Locale           promptTemplateLocaleData
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
	Tools        []promptTemplateToolData
	Secrets      []promptTemplateSecretBindingData
	Locale       promptTemplateLocaleData
}

func renderAgentPromptTemplate(body string, data promptTemplateData) (string, error) {
	data = withPromptTemplateDefaults(data)
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
	rendered, err := RenderRolePromptTemplate(input.Role.PromptTemplate, rolePromptTemplateData(input))
	if err != nil {
		return "", err
	}
	return appendRoleRuntimeContract(rendered, input), nil
}

func BuildRoleContinuationPrompt(input RolePromptInput) (string, error) {
	userMessage := strings.TrimSpace(input.UserMessage)
	if userMessage == "" {
		return "", fmt.Errorf("user message is required")
	}
	var body strings.Builder
	body.WriteString("# User message\n\n")
	body.WriteString(userMessage)
	body.WriteString("\n\n# Continuation context\n\n")
	body.WriteString("- Continue the existing Codex session. Keep the role, project, repository, and operating instructions already established earlier in this session.\n")
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
	appendRoleRuntimeContractMarkdown(&body, input)
	appendRuntimeToolsMarkdown(&body)
	appendSecretBindingsMarkdown(&body, roleSecretBindings(input.RuntimeVariables))
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Response language: ")
		body.WriteString(input.Locale.Language)
		body.WriteString("\n")
	}
	return strings.TrimSpace(body.String()) + "\n", nil
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
	appendRoleRuntimeContractMarkdown(&body, input)
	appendRuntimeToolsMarkdown(&body)
	appendSecretBindingsMarkdown(&body, roleSecretBindings(input.RuntimeVariables))
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Response language: ")
		body.WriteString(input.Locale.Language)
		body.WriteString("\n")
	}
	return strings.TrimSpace(body.String()) + "\n"
}

func appendRoleRuntimeContract(prompt string, input RolePromptInput) string {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(prompt))
	body.WriteString("\n\n")
	appendRoleRuntimeContractMarkdown(&body, input)
	return strings.TrimSpace(body.String()) + "\n"
}

func appendRoleRuntimeContractMarkdown(body *strings.Builder, input RolePromptInput) {
	body.WriteString("# Matter-codex runtime contract\n\n")
	body.WriteString("- GitHub CLI: use `gh` when the role has a GitHub account. Token/user/email are exposed through `GH_TOKEN`, `GITHUB_TOKEN`, `GITHUB_USERNAME`/`GITHUB_USER`, and `GITHUB_EMAIL`. Never print token values.\n")
	body.WriteString("- For GitHub issue, pull request, review, and comment Markdown, write the body to a temporary file or heredoc and pass it with `--body-file`/API file input. Do not inline Markdown with backticks or shell-sensitive text directly inside a shell command string.\n")
	body.WriteString("- Mattermost MCP: use `mattermost_get_thread` to read this thread and `mattermost_search_chat` for small bounded channel searches.\n")
	body.WriteString("- Progress status: use `mattermost_update_turn_status` to update the single status message for this turn. Keep it concise and in the response language")
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString(" (")
		body.WriteString(input.Locale.Language)
		body.WriteString(")")
	}
	body.WriteString(". Update after planning, after meaningful milestones, before a long wait, and when blocked. Do not create routine progress posts with `mattermost_post_thread_update`.\n")
	body.WriteString("- Use `mattermost_post_thread_update` only when you intentionally need an additional message in the thread. Use `mattermost_request_agent` only when the user or role prompt allows delegating to another agent.\n")
	body.WriteString("\n")
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
		Tools:        agentRuntimeTools(),
		Secrets:      roleSecretBindings(input.RuntimeVariables),
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
		"ToolsPlaceholders":       "{{range .Tools}}- {{.Command}} ({{.Name}}): {{.Purpose}}{{end}}",
		"SecretPlaceholders":      "{{range .Secrets}}- {{.Name}} {{.Kind}} env={{.Env}} file={{.File}}: {{.Purpose}}{{end}}",
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
		Tools:   agentRuntimeTools(),
		Secrets: roleSecretBindings(nil),
		Locale:  locale,
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

func withPromptTemplateDefaults(data promptTemplateData) promptTemplateData {
	if len(data.Tools) == 0 {
		data.Tools = agentRuntimeTools()
	}
	if len(data.Secrets) == 0 {
		data.Secrets = roleSecretBindings(nil)
	}
	return data
}

func agentRuntimeTools() []promptTemplateToolData {
	return []promptTemplateToolData{
		{Name: "OpenAI Codex CLI", Command: "codex", Version: "0.141.0", Purpose: "continue or resume Codex agent sessions"},
		{Name: "Node.js runtime", Command: "node", Version: "24.17.x", Purpose: "run Vue, TypeScript, codegen and diagnostic JavaScript tooling"},
		{Name: "npm", Command: "npm", Version: "11.13.x", Purpose: "install and run JavaScript packages and npm scripts"},
		{Name: "pnpm", Command: "pnpm", Version: "11.8.0", Purpose: "install and run workspace-aware JavaScript package scripts"},
		{Name: "Yarn", Command: "yarn", Version: "1.22.22", Purpose: "run projects that still use Yarn classic"},
		{Name: "Git", Command: "git", Version: "distro package", Purpose: "clone repositories, manage branches, commit and push changes"},
		{Name: "GitHub CLI", Command: "gh", Version: "2.95.0", Purpose: "work with GitHub issues, pull requests, reviews, comments, checks and repository metadata"},
		{Name: "Kubernetes CLI", Command: "kubectl", Version: "1.36.2", Purpose: "inspect and, when the role allows it, manage Kubernetes resources"},
		{Name: "Helm", Command: "helm", Version: "4.2.1", Purpose: "inspect and render Kubernetes Helm releases and charts"},
		{Name: "PostgreSQL client", Command: "psql", Version: "18.x distro package", Purpose: "inspect PostgreSQL databases and run SQL diagnostics"},
		{Name: "Redis client", Command: "redis-cli", Version: "8.x distro package", Purpose: "inspect Redis state and run cache diagnostics"},
		{Name: "jq", Command: "jq", Version: "distro package", Purpose: "parse and transform JSON in diagnostics and scripts"},
		{Name: "yq", Command: "yq", Version: "4.53.3", Purpose: "parse and transform YAML manifests and configuration"},
		{Name: "ripgrep", Command: "rg", Version: "distro package", Purpose: "fast repository text search"},
		{Name: "fd", Command: "fd", Version: "distro package", Purpose: "fast file discovery"},
		{Name: "just", Command: "just", Version: "distro package", Purpose: "run justfile project commands"},
		{Name: "netcat", Command: "nc", Version: "distro package", Purpose: "check raw TCP connectivity while debugging services"},
		{Name: "DNS tools", Command: "dig", Version: "distro package", Purpose: "debug DNS records and service discovery"},
		{Name: "Go toolchain", Command: "go", Version: "1.26", Purpose: "build, test and inspect Go modules, including projects pinned to Go 1.25 toolchains"},
		{Name: "goimports", Command: "goimports", Version: "0.46.0", Purpose: "format Go imports"},
		{Name: "gofumpt", Command: "gofumpt", Version: "0.10.0", Purpose: "apply stricter Go formatting when the project asks for it"},
		{Name: "staticcheck", Command: "staticcheck", Version: "0.7.0", Purpose: "run static Go analysis"},
		{Name: "Goose migrations", Command: "goose", Version: "3.27.1", Purpose: "run and inspect PostgreSQL migrations"},
		{Name: "sqlc", Command: "sqlc", Version: "1.31.1", Purpose: "generate typed Go database access from SQL"},
		{Name: "mockgen", Command: "mockgen", Version: "0.6.0", Purpose: "generate Go mocks for tests"},
		{Name: "OpenAPI Go codegen", Command: "oapi-codegen", Version: "2.7.1", Purpose: "generate Go HTTP transport code from OpenAPI specs"},
		{Name: "OpenAPI TypeScript codegen", Command: "openapi-ts", Version: "0.98.2", Purpose: "generate TypeScript clients from OpenAPI specs"},
		{Name: "Vue runtime package", Command: "npm view vue", Version: "3.5.38", Purpose: "Vue runtime package is available through npm for project installs and quick inspection"},
		{Name: "Vue project scaffolder", Command: "create-vue", Version: "3.22.4", Purpose: "scaffold Vue projects when a new frontend package is needed"},
		{Name: "TypeScript compiler", Command: "tsc", Version: "6.0.3", Purpose: "type-check TypeScript projects"},
		{Name: "Vue TypeScript checker", Command: "vue-tsc", Version: "3.3.5", Purpose: "type-check Vue single-file components"},
		{Name: "Vite", Command: "vite", Version: "8.0.16", Purpose: "run and build Vue/Vite applications"},
		{Name: "Vitest", Command: "vitest", Version: "4.1.9", Purpose: "run frontend unit tests"},
		{Name: "ESLint", Command: "eslint", Version: "10.5.0", Purpose: "lint JavaScript and TypeScript code"},
		{Name: "Prettier", Command: "prettier", Version: "3.8.4", Purpose: "format frontend and documentation files"},
		{Name: "AsyncAPI CLI", Command: "asyncapi", Version: "6.0.2", Purpose: "validate AsyncAPI specs and run AsyncAPI generators for event/websocket contracts"},
		{Name: "AsyncAPI generator package", Command: "asyncapi generate", Version: "3.3.0", Purpose: "generate code and documentation from AsyncAPI templates"},
		{Name: "AsyncAPI Modelina", Command: "modelina", Version: "5.10.1", Purpose: "generate TypeScript models for AsyncAPI/WebSocket payloads when templates use Modelina"},
		{Name: "WebSocket client", Command: "wscat", Version: "6.1.0", Purpose: "manually connect to and debug websocket endpoints"},
		{Name: "Buf", Command: "buf", Version: "1.71.0", Purpose: "lint and generate protobuf/gRPC contracts"},
		{Name: "grpcurl", Command: "grpcurl", Version: "1.9.3", Purpose: "inspect and call gRPC services during debugging"},
		{Name: "Protocol Buffers compiler", Command: "protoc", Version: "31.x distro package", Purpose: "generate protobuf and gRPC artifacts"},
		{Name: "Protobuf Go plugin", Command: "protoc-gen-go", Version: "1.36.11", Purpose: "generate Go protobuf types"},
		{Name: "gRPC Go plugin", Command: "protoc-gen-go-grpc", Version: "1.6.2", Purpose: "generate Go gRPC service stubs"},
		{Name: "Go linter", Command: "golangci-lint", Version: "2.12.2", Purpose: "run the main Go lint profile when requested"},
	}
}

func agentSecretBindings() []promptTemplateSecretBindingData {
	return []promptTemplateSecretBindingData{
		{
			Name:         "GitHub account",
			Kind:         "Kubernetes Secret mount plus shell env",
			Env:          "GH_TOKEN, GITHUB_TOKEN, GITHUB_USERNAME, GITHUB_USER, GITHUB_EMAIL, GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME, GIT_COMMITTER_EMAIL, MATTERCODEX_GITHUB_TOKEN_FILE",
			File:         "/var/run/secrets/matter-codex-github/github-token",
			Availability: "only when the role has a GitHub account binding",
			Purpose:      "authenticate git and gh operations as the selected agent GitHub account",
		},
		{
			Name:         "OpenAI Codex account",
			Kind:         "Kubernetes Secret mount copied into CODEX_HOME",
			Env:          "CODEX_HOME",
			File:         "/codex-home/auth.json and /var/run/secrets/matter-codex-codex/auth.json",
			Availability: "required for Codex-backed agent runs",
			Purpose:      "authenticate Codex CLI with the selected OpenAI account",
		},
		{
			Name:         "Kubernetes service account",
			Kind:         "Projected service account token plus in-cluster env",
			Env:          "KUBERNETES_SERVICE_HOST, KUBERNETES_SERVICE_PORT, KUBERNETES_PORT",
			File:         "/var/run/secrets/kubernetes.io/serviceaccount/token, /var/run/secrets/kubernetes.io/serviceaccount/ca.crt, /var/run/secrets/kubernetes.io/serviceaccount/namespace",
			Availability: "developer, reviewer, chat and session agent pods",
			Purpose:      "access the MatterCodex and agent runtime Kubernetes cluster only when the user prompt or repository instructions explicitly allow it",
		},
		{
			Name:         "Mattermost MCP session",
			Kind:         "Codex MCP bearer token",
			Env:          "MATTERCODEX_MCP_TOKEN",
			File:         "/var/run/secrets/matter-codex-session/token",
			Availability: "long-lived chat session pods",
			Purpose:      "let Codex call Mattermost MCP tools for bounded thread reads and progress updates",
		},
	}
}

func roleSecretBindings(runtimeVariables []entity.ProjectRuntimeVariable) []promptTemplateSecretBindingData {
	bindings := agentSecretBindings()
	for _, variable := range runtimeVariables {
		if !variable.Enabled || strings.TrimSpace(variable.Name) == "" {
			continue
		}
		description := strings.TrimSpace(variable.Description)
		if description == "" {
			description = "project-level runtime variable explicitly attached to this role"
		}
		bindings = append(bindings, promptTemplateSecretBindingData{
			Name:         "Project env " + variable.Name,
			Kind:         "Kubernetes Secret env binding",
			Env:          variable.Name,
			Availability: "only for roles explicitly bound to this project variable",
			Purpose:      description,
		})
	}
	return bindings
}

func appendRuntimeToolsMarkdown(body *strings.Builder) {
	body.WriteString("- Available runtime tools:\n")
	for _, tool := range agentRuntimeTools() {
		body.WriteString("  - `")
		body.WriteString(tool.Command)
		body.WriteString("`")
		if strings.TrimSpace(tool.Version) != "" {
			body.WriteString(" ")
			body.WriteString(tool.Version)
		}
		body.WriteString(": ")
		body.WriteString(tool.Purpose)
		body.WriteString("\n")
	}
}

func appendSecretBindingsMarkdown(body *strings.Builder, bindings []promptTemplateSecretBindingData) {
	body.WriteString("- Available credential bindings:\n")
	for _, binding := range bindings {
		body.WriteString("  - ")
		body.WriteString(binding.Name)
		body.WriteString(" (")
		body.WriteString(binding.Availability)
		body.WriteString("): env `")
		body.WriteString(binding.Env)
		body.WriteString("`")
		if strings.TrimSpace(binding.File) != "" {
			body.WriteString("; files `")
			body.WriteString(binding.File)
			body.WriteString("`")
		}
		body.WriteString(". ")
		body.WriteString(binding.Purpose)
		body.WriteString(".\n")
	}
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
