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
	developerImplementTaskKey = "implement_task"
	reviewPRTemplateKey       = "review_pr"
	managerCoordinateTaskKey  = "coordinate_task"
	architectDocsTaskKey      = "architecture_task"
	docsDocumentationTaskKey  = "documentation_task"
	sreOperationsTaskKey      = "operations_task"
	qaRegressionTaskKey       = "regression_task"
	improverFeedbackTaskKey   = "feedback_improvement"
	uiDesignerTaskTemplateKey = "ui_design_task"
)

type promptTemplateRunData struct {
	ID      string
	Profile string
	Role    string
	Locale  string
}

type promptTemplateProjectData struct {
	Name        string
	Slug        string
	Description string
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
	Project     promptTemplateProjectData
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
	Title      string
	Body       string
	BaseBranch string
	HeadBranch string
}

type rolePromptData struct {
	Project      rolePromptProjectData
	Role         rolePromptRoleData
	Agent        promptTemplateAgentData
	Chat         rolePromptChatData
	Run          promptTemplateRunData
	Repository   promptTemplateRepositoryData
	Repositories []rolePromptRepositoryData
	Task         rolePromptTaskData
	PullRequest  promptTemplatePullRequestData
	GitHub       promptTemplateGitHubData
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
	body.WriteString("# Сообщение пользователя\n\n")
	body.WriteString(userMessage)
	body.WriteString("\n\n# Контекст продолжения\n\n")
	body.WriteString("- Продолжай существующую сессию Codex. Сохрани роль, проект, репозиторий и рабочие инструкции, которые уже были установлены ранее в этой сессии.\n")
	if strings.TrimSpace(input.Project.Name) != "" {
		body.WriteString("- Проект: ")
		body.WriteString(input.Project.Name)
		if strings.TrimSpace(input.Project.Slug) != "" {
			body.WriteString(" (")
			body.WriteString(input.Project.Slug)
			body.WriteString(")")
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.Name) != "" {
		body.WriteString("- Чат: ")
		body.WriteString(input.Chat.Name)
		if strings.TrimSpace(input.Chat.ChatType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Chat.ChatType)
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Role.Name) != "" {
		body.WriteString("- Роль: ")
		body.WriteString(input.Role.Name)
		if strings.TrimSpace(input.Role.RoleType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Role.RoleType)
		}
		body.WriteString("\n")
	}
	if len(input.Repositories) > 0 {
		body.WriteString("- Репозитории:\n")
		for _, repo := range input.Repositories {
			body.WriteString("  - ")
			body.WriteString(repo.Provider)
			body.WriteString(":")
			body.WriteString(repo.FullName())
			if strings.TrimSpace(repo.DefaultBranch) != "" {
				body.WriteString(" ветка ")
				body.WriteString(repo.DefaultBranch)
			}
			body.WriteString("\n")
		}
	}
	appendRoleRuntimeContractMarkdown(&body, input)
	appendRuntimeToolsMarkdown(&body)
	appendSecretBindingsMarkdown(&body, roleSecretBindings(input.RuntimeVariables))
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Язык ответа: ")
		body.WriteString(input.Locale.Language)
		body.WriteString("\n")
	}
	return strings.TrimSpace(body.String()) + "\n", nil
}

func buildRawRolePrompt(input RolePromptInput, userMessage string) string {
	var body strings.Builder
	body.WriteString("# Инструкция пользователя\n\n")
	body.WriteString(userMessage)
	body.WriteString("\n\n# Контекст Matter-codex\n\n")
	if strings.TrimSpace(input.Project.Name) != "" {
		body.WriteString("- Проект: ")
		body.WriteString(input.Project.Name)
		if strings.TrimSpace(input.Project.Slug) != "" {
			body.WriteString(" (")
			body.WriteString(input.Project.Slug)
			body.WriteString(")")
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.Name) != "" {
		body.WriteString("- Чат: ")
		body.WriteString(input.Chat.Name)
		if strings.TrimSpace(input.Chat.ChatType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Chat.ChatType)
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Role.Name) != "" {
		body.WriteString("- Роль: ")
		body.WriteString(input.Role.Name)
		if strings.TrimSpace(input.Role.RoleType) != "" {
			body.WriteString(" / ")
			body.WriteString(input.Role.RoleType)
		}
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Role.Description) != "" {
		body.WriteString("- Описание роли: ")
		body.WriteString(input.Role.Description)
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.RootGitHubIssue) != "" {
		body.WriteString("- Корневая задача GitHub: ")
		body.WriteString(input.Chat.RootGitHubIssue)
		body.WriteString("\n")
	}
	if strings.TrimSpace(input.Chat.WorkPolicy) != "" {
		body.WriteString("- Рабочая политика: ")
		body.WriteString(input.Chat.WorkPolicy)
		body.WriteString("\n")
	}
	if len(input.Repositories) > 0 {
		body.WriteString("- Репозитории:\n")
		for _, repo := range input.Repositories {
			body.WriteString("  - ")
			body.WriteString(repo.Provider)
			body.WriteString(":")
			body.WriteString(repo.FullName())
			if strings.TrimSpace(repo.DefaultBranch) != "" {
				body.WriteString(" ветка ")
				body.WriteString(repo.DefaultBranch)
			}
			body.WriteString("\n")
		}
	}
	appendRoleRuntimeContractMarkdown(&body, input)
	appendRuntimeToolsMarkdown(&body)
	appendSecretBindingsMarkdown(&body, roleSecretBindings(input.RuntimeVariables))
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Язык ответа: ")
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
	body.WriteString("# Контракт среды выполнения Matter-codex\n\n")
	body.WriteString("- GitHub CLI: используй `gh`, если у роли есть GitHub-аккаунт. Токен, пользователь и email доступны через `GH_TOKEN`, `GITHUB_TOKEN`, `GITHUB_USERNAME`/`GITHUB_USER` и `GITHUB_EMAIL`. Никогда не печатай значения токенов.\n")
	body.WriteString("- Для Markdown-текста задач GitHub, пул-реквестов, ревью и комментариев записывай текст во временный файл или heredoc и передавай через `--body-file`/файловый ввод API. Не встраивай Markdown с обратными кавычками или shell-чувствительным текстом напрямую в одну командную строку shell.\n")
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString("- Язык: все пользовательские ответы в Mattermost, заголовки и описания задач GitHub, заголовки и описания пул-реквестов, комментарии к задачам и пул-реквестам, тексты ревью, строчные замечания ревью, комментарии в коде, документацию и резюме поставки пиши на ")
		body.WriteString(input.Locale.Language)
		body.WriteString(". Идентификаторы кода, пути к файлам, имена переменных среды, команды, имена API и цитаты из исходников оставляй как есть. Если `AGENTS.md` отсутствует или не задает язык, эта локаль среды выполнения является обязательной.\n")
	}
	body.WriteString("- Mattermost MCP: используй `mattermost_get_thread`, чтобы читать текущий тред, и `mattermost_search_chat` для небольшого ограниченного поиска по каналу.\n")
	body.WriteString("- Статус прогресса: используй `mattermost_update_turn_status` для коротких обновлений прогресса, которые не запускают агентов. Matter-codex держит карточку статуса со стартом, лимитами и кнопкой остановки отдельно и обновляет ее сам. Текст прогресса пиши на языке ответа")
	if strings.TrimSpace(input.Locale.Language) != "" {
		body.WriteString(" (")
		body.WriteString(input.Locale.Language)
		body.WriteString(")")
	}
	body.WriteString(". Обновляй статус после планирования, после значимых этапов, перед долгим ожиданием и при блокере. Не создавай обычные сообщения о прогрессе через `mattermost_post_thread_update`.\n")
	body.WriteString("- Используй `mattermost_post_thread_update` только когда тебе намеренно нужно отдельное сообщение в треде.\n")
	body.WriteString("- Делегирование агентам: запускай другого агента только через `mattermost_request_agent`. Обычные упоминания username в Mattermost-сообщениях от агентов никого не запускают; сообщения агентских ботов игнорируются маршрутизацией чата. Платформа ставит ход работы в существующую тредовую сессию целевого агента; если целевой агент занят, ход работы ждет завершения текущего хода и сохранения сессии. Если несколько агентов запросят того же занятого целевого агента в этом треде, Matter-codex объединит их промпты в один следующий ход работы с явным указанием инициаторов.\n")
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
	repository := promptTemplateRepositoryData{}
	baseBranch := "main"
	if len(input.Repositories) > 0 {
		repo := input.Repositories[0]
		repository = promptTemplateRepositoryData{
			Provider: repo.Provider,
			Owner:    repo.Owner,
			Name:     repo.Name,
			FullName: repo.FullName(),
		}
		baseBranch = defaultString(repo.DefaultBranch, "main")
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
		Agent: promptTemplateAgentData{
			Profile:          input.Role.Name,
			Role:             input.Role.RoleType,
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
		Run: promptTemplateRunData{
			ID:      "chat-session",
			Profile: input.Role.Name,
			Role:    input.Role.RoleType,
			Locale:  locale.Code,
		},
		Repository:   repository,
		Repositories: repositories,
		Task: rolePromptTaskData{
			Title:      "Chat task",
			Body:       strings.TrimSpace(input.UserMessage),
			BaseBranch: baseBranch,
			HeadBranch: "matter-codex-chat-session",
		},
		PullRequest: promptTemplatePullRequestData{},
		GitHub:      promptGitHubData(input.Role.GitHubAccountName),
		Tools:       agentRuntimeTools(),
		Secrets:     roleSecretBindings(input.RuntimeVariables),
		Locale:      locale,
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
		"ProjectPlaceholders":     "{{.Project.Name}}, {{.Project.Slug}}, {{.Project.Description}}",
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
		Project: promptTemplateProjectData{
			Name:        "Sample Project",
			Slug:        "sample-project",
			Description: "Sample project context.",
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
	if templateKey == developerImplementTaskKey {
		data.Run.Role = "developer"
		data.Agent.Role = "developer"
		data.GitHub.Account = "agent"
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
		{Name: "OpenAI Codex CLI", Command: "codex", Version: "0.144.1", Purpose: "продолжать или возобновлять сессии Codex-агентов"},
		{Name: "Node.js runtime", Command: "node", Version: "24.17.x", Purpose: "запускать Vue, TypeScript, кодогенерацию и диагностические JavaScript-инструменты"},
		{Name: "npm", Command: "npm", Version: "11.13.x", Purpose: "устанавливать и запускать JavaScript-пакеты и npm-скрипты"},
		{Name: "pnpm", Command: "pnpm", Version: "11.8.0", Purpose: "устанавливать и запускать JavaScript-скрипты с учетом workspace"},
		{Name: "Yarn", Command: "yarn", Version: "1.22.22", Purpose: "запускать проекты, которые еще используют Yarn classic"},
		{Name: "Git", Command: "git", Version: "distro package", Purpose: "клонировать репозитории, управлять ветками, коммитить и пушить изменения"},
		{Name: "GitHub CLI", Command: "gh", Version: "2.95.0", Purpose: "работать с задачами GitHub, пул-реквестами, ревью, комментариями, проверками и метаданными репозитория"},
		{Name: "Kubernetes CLI", Command: "kubectl", Version: "1.36.2", Purpose: "инспектировать и, если роль разрешает, управлять ресурсами Kubernetes"},
		{Name: "Helm", Command: "helm", Version: "4.2.1", Purpose: "инспектировать и рендерить Kubernetes Helm-релизы и чарты"},
		{Name: "PostgreSQL client", Command: "psql", Version: "distro package", Purpose: "инспектировать базы PostgreSQL и выполнять SQL-диагностику"},
		{Name: "Redis client", Command: "redis-cli", Version: "distro package", Purpose: "инспектировать состояние Redis и выполнять диагностику кэша"},
		{Name: "jq", Command: "jq", Version: "distro package", Purpose: "разбирать и преобразовывать JSON в диагностике и скриптах"},
		{Name: "yq", Command: "yq", Version: "4.53.3", Purpose: "разбирать и преобразовывать YAML-манифесты и конфигурацию"},
		{Name: "ripgrep", Command: "rg", Version: "distro package", Purpose: "быстро искать текст по репозиторию"},
		{Name: "fd", Command: "fd", Version: "distro package", Purpose: "быстро искать файлы"},
		{Name: "just", Command: "just", Version: "1.55.1", Purpose: "запускать проектные команды из justfile"},
		{Name: "netcat", Command: "nc", Version: "distro package", Purpose: "проверять raw TCP connectivity при отладке сервисов"},
		{Name: "DNS tools", Command: "dig", Version: "distro package", Purpose: "отлаживать DNS-записи и service discovery"},
		{Name: "Go toolchain", Command: "go", Version: "1.26", Purpose: "собирать, тестировать и инспектировать Go-модули, включая проекты, закрепленные на Go 1.25"},
		{Name: "goimports", Command: "goimports", Version: "0.46.0", Purpose: "форматировать Go-импорты"},
		{Name: "gofumpt", Command: "gofumpt", Version: "0.10.0", Purpose: "применять более строгое Go-форматирование, когда проект этого требует"},
		{Name: "staticcheck", Command: "staticcheck", Version: "0.7.0", Purpose: "запускать статический Go-анализ"},
		{Name: "Goose migrations", Command: "goose", Version: "3.27.1", Purpose: "запускать и инспектировать PostgreSQL-миграции"},
		{Name: "sqlc", Command: "sqlc", Version: "1.31.1", Purpose: "генерировать типизированный Go-доступ к базе из SQL"},
		{Name: "mockgen", Command: "mockgen", Version: "0.6.0", Purpose: "генерировать Go-моки для тестов"},
		{Name: "OpenAPI Go codegen", Command: "oapi-codegen", Version: "2.7.1", Purpose: "генерировать Go HTTP transport code из OpenAPI-спецификаций"},
		{Name: "OpenAPI TypeScript codegen", Command: "openapi-ts", Version: "0.98.2", Purpose: "генерировать TypeScript-клиенты из OpenAPI-спецификаций"},
		{Name: "Vue runtime package", Command: "npm view vue", Version: "3.5.38", Purpose: "проверять Vue runtime package через npm для установки в проект и быстрой инспекции"},
		{Name: "Vue project scaffolder", Command: "create-vue", Version: "3.22.4", Purpose: "создавать каркас Vue-проекта, если нужен новый frontend-пакет"},
		{Name: "TypeScript compiler", Command: "tsc", Version: "6.0.3", Purpose: "проверять типы в TypeScript-проектах"},
		{Name: "Vue TypeScript checker", Command: "vue-tsc", Version: "3.3.5", Purpose: "проверять типы Vue single-file components"},
		{Name: "Vite", Command: "vite", Version: "8.0.16", Purpose: "запускать и собирать Vue/Vite-приложения"},
		{Name: "Vitest", Command: "vitest", Version: "4.1.9", Purpose: "запускать frontend unit tests"},
		{Name: "ESLint", Command: "eslint", Version: "10.5.0", Purpose: "линтить JavaScript и TypeScript-код"},
		{Name: "Prettier", Command: "prettier", Version: "3.8.4", Purpose: "форматировать frontend-файлы и документацию"},
		{Name: "AsyncAPI CLI", Command: "asyncapi", Version: "6.0.2", Purpose: "валидировать AsyncAPI-спецификации и запускать генераторы для event/websocket-контрактов"},
		{Name: "AsyncAPI generator package", Command: "asyncapi generate", Version: "3.3.0", Purpose: "генерировать код и документацию из AsyncAPI-шаблонов"},
		{Name: "AsyncAPI Modelina", Command: "modelina", Version: "5.10.1", Purpose: "генерировать TypeScript-модели для AsyncAPI/WebSocket payloads, когда шаблоны используют Modelina"},
		{Name: "WebSocket client", Command: "wscat", Version: "6.1.0", Purpose: "вручную подключаться к websocket endpoints и отлаживать их"},
		{Name: "Chromium browser", Command: "chromium", Version: "distro package", Purpose: "диагностировать browser runtime и проверять версию системного browser binary; для smoke/e2e используй Playwright CLI/API"},
		{Name: "Playwright", Command: "playwright", Version: "1.61.1", Purpose: "запускать браузерные e2e/smoke-проверки, делать скриншоты и собирать UI-диагностику"},
		{Name: "Playwright MCP", Command: "playwright-mcp", Version: "0.0.77", Purpose: "поднимать MCP-сервер браузерной автоматизации для ролей, которым он явно включен в Codex config.toml"},
		{Name: "wait-on", Command: "wait-on", Version: "9.0.10", Purpose: "дожидаться локального dev-server перед browser smoke/e2e и снятием скриншотов"},
		{Name: "Buf", Command: "buf", Version: "1.71.0", Purpose: "линтить и генерировать protobuf/gRPC-контракты"},
		{Name: "grpcurl", Command: "grpcurl", Version: "1.9.3", Purpose: "инспектировать и вызывать gRPC-сервисы при отладке"},
		{Name: "Protocol Buffers compiler", Command: "protoc", Version: "distro package", Purpose: "генерировать protobuf и gRPC-артефакты"},
		{Name: "Protobuf Go plugin", Command: "protoc-gen-go", Version: "1.36.11", Purpose: "генерировать Go protobuf types"},
		{Name: "gRPC Go plugin", Command: "protoc-gen-go-grpc", Version: "1.6.2", Purpose: "генерировать Go gRPC service stubs"},
		{Name: "Go linter", Command: "golangci-lint", Version: "2.12.2", Purpose: "запускать основной Go lint profile по запросу"},
	}
}

func agentSecretBindings() []promptTemplateSecretBindingData {
	return []promptTemplateSecretBindingData{
		{
			Name:         "GitHub-аккаунт",
			Kind:         "Kubernetes Secret mount и переменные shell",
			Env:          "GH_TOKEN, GITHUB_TOKEN, GITHUB_USERNAME, GITHUB_USER, GITHUB_EMAIL, GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME, GIT_COMMITTER_EMAIL, MATTERCODEX_GITHUB_TOKEN_FILE",
			File:         "/var/run/secrets/matter-codex-github/github-token",
			Availability: "только если у роли привязан GitHub-аккаунт",
			Purpose:      "аутентифицировать операции git и gh от имени выбранного GitHub-аккаунта агента",
		},
		{
			Name:         "OpenAI Codex-аккаунт",
			Kind:         "Kubernetes Secret mount, копируемый в CODEX_HOME",
			Env:          "CODEX_HOME",
			File:         "/codex-home/auth.json and /var/run/secrets/matter-codex-codex/auth.json",
			Availability: "обязателен для запусков агентов через Codex",
			Purpose:      "аутентифицировать Codex CLI выбранным OpenAI-аккаунтом",
		},
		{
			Name:         "Kubernetes service account",
			Kind:         "Projected service account token и внутрикластерные переменные среды",
			Env:          "KUBERNETES_SERVICE_HOST, KUBERNETES_SERVICE_PORT, KUBERNETES_PORT",
			File:         "/var/run/secrets/kubernetes.io/serviceaccount/token, /var/run/secrets/kubernetes.io/serviceaccount/ca.crt, /var/run/secrets/kubernetes.io/serviceaccount/namespace",
			Availability: "поды developer, reviewer, чата и агентских сессий",
			Purpose:      "доступ к Kubernetes-кластеру MatterCodex и среды выполнения агентов только когда промпт пользователя или инструкции репозитория явно это разрешают",
		},
		{
			Name:         "Mattermost MCP-сессия",
			Kind:         "Codex MCP bearer token",
			Env:          "MATTERCODEX_MCP_TOKEN",
			File:         "/var/run/secrets/matter-codex-session/token",
			Availability: "долгоживущие поды чатовых сессий",
			Purpose:      "позволить Codex MCP-клиенту вызывать Mattermost MCP-инструменты для ограниченного чтения треда и обновлений прогресса; переменная не доступна shell-командам",
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
			description = "проектная переменная среды выполнения, явно назначенная этой роли"
		}
		bindings = append(bindings, promptTemplateSecretBindingData{
			Name:         "Проектная переменная " + variable.Name,
			Kind:         "привязка переменной среды из Kubernetes Secret",
			Env:          variable.Name,
			Availability: "только для ролей, которым явно назначена эта проектная переменная",
			Purpose:      description,
		})
	}
	return bindings
}

func appendRuntimeToolsMarkdown(body *strings.Builder) {
	body.WriteString("- Доступные инструменты среды выполнения:\n")
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
	body.WriteString("- Доступные привязки учетных данных:\n")
	for _, binding := range bindings {
		body.WriteString("  - ")
		body.WriteString(binding.Name)
		body.WriteString(" (")
		body.WriteString(binding.Availability)
		body.WriteString("): переменные `")
		body.WriteString(binding.Env)
		body.WriteString("`")
		if strings.TrimSpace(binding.File) != "" {
			body.WriteString("; файлы `")
			body.WriteString(binding.File)
			body.WriteString("`")
		}
		body.WriteString(". Назначение: ")
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
