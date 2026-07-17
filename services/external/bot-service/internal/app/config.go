package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

// Config contains bot-service process settings.
type Config struct {
	HTTPAddr                        string        `env:"MATTERCODEX_BOT_SERVICE_HTTP_ADDR" envDefault:":8080"`
	MattermostSiteURL               string        `env:"MATTERCODEX_MATTERMOST_SITE_URL"`
	MattermostInternalURL           string        `env:"MATTERCODEX_MATTERMOST_INTERNAL_URL"`
	BotServiceSiteURL               string        `env:"MATTERCODEX_BOT_SERVICE_SITE_URL"`
	BotServiceInternalURL           string        `env:"MATTERCODEX_BOT_SERVICE_INTERNAL_URL"`
	Locale                          string        `env:"MATTERCODEX_LOCALE" envDefault:"en"`
	DefaultTeamName                 string        `env:"MATTERCODEX_DEFAULT_TEAM_NAME" envDefault:"agents"`
	DefaultChannels                 []string      `env:"MATTERCODEX_DEFAULT_CHANNELS" envDefault:"agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit" envSeparator:","`
	OwnerMattermostUsername         string        `env:"MATTERCODEX_OWNER_MATTERMOST_USERNAME"`
	MattermostBotToken              string        `env:"MATTERCODEX_MATTERMOST_BOT_TOKEN"`
	MattermostAdminToken            string        `env:"MATTERCODEX_MATTERMOST_ADMIN_TOKEN"`
	MattermostSlashToken            string        `env:"MATTERCODEX_MATTERMOST_SLASH_TOKEN"`
	GitHubToken                     string        `env:"MATTERCODEX_GITHUB_TOKEN"`
	GitHubWebhookSecret             string        `env:"MATTERCODEX_GITHUB_WEBHOOK_SECRET"`
	GitHubSecretName                string        `env:"MATTERCODEX_GITHUB_SECRET" envDefault:"matter-codex-github"`
	DatabaseDSN                     string        `env:"MATTERCODEX_DATABASE_DSN"`
	RuntimeEnabled                  bool          `env:"MATTERCODEX_RUNTIME_ENABLED" envDefault:"true"`
	RuntimeNamespace                string        `env:"MATTERCODEX_RUNTIME_NAMESPACE"`
	RuntimeKubeconfigPath           string        `env:"MATTERCODEX_RUNTIME_KUBECONFIG_PATH"`
	RuntimeSmokeImage               string        `env:"MATTERCODEX_RUNTIME_SMOKE_IMAGE" envDefault:"busybox:1.36"`
	AgentRunnerImage                string        `env:"MATTERCODEX_AGENT_RUNNER_IMAGE" envDefault:"matter-codex-agent-runner:dev"`
	CodexPackage                    string        `env:"MATTERCODEX_CODEX_PACKAGE" envDefault:"@openai/codex@0.144.1"`
	RuntimeWorkspaceSize            string        `env:"MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE" envDefault:"1Gi"`
	RuntimeJobTTLSeconds            int32         `env:"MATTERCODEX_RUNTIME_JOB_TTL_SECONDS" envDefault:"86400"`
	RuntimeRetentionEnabled         bool          `env:"MATTERCODEX_RUNTIME_RETENTION_ENABLED" envDefault:"true"`
	RuntimeRetentionInterval        time.Duration `env:"MATTERCODEX_RUNTIME_RETENTION_INTERVAL" envDefault:"30m"`
	RuntimeRetentionOlderThan       time.Duration `env:"MATTERCODEX_RUNTIME_RETENTION_OLDER_THAN" envDefault:"24h"`
	RuntimeSessionRepairEnabled     bool          `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_ENABLED" envDefault:"true"`
	RuntimeSessionRepairInterval    time.Duration `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_INTERVAL" envDefault:"30s"`
	RuntimeSessionRepairBatch       int           `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_BATCH" envDefault:"20"`
	AuthCheckJobTTLSeconds          int32         `env:"MATTERCODEX_CODEX_AUTH_CHECK_JOB_TTL_SECONDS" envDefault:"300"`
	RuntimeLogTailLines             int64         `env:"MATTERCODEX_RUNTIME_LOG_TAIL_LINES" envDefault:"40"`
	AgentServiceAccount             string        `env:"MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT" envDefault:"matter-codex-agent-runner"`
	AgentClusterAdminServiceAccount string        `env:"MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT" envDefault:"matter-codex-agent-runner-cluster-admin"`
	CodexAuthSecretName             string        `env:"MATTERCODEX_CODEX_AUTH_SECRET" envDefault:"matter-codex-codex-auth"`
	StorageMigrations               bool          `env:"MATTERCODEX_STORAGE_MIGRATIONS_ENABLED" envDefault:"true"`
	ReadHeaderTimeout               time.Duration `env:"MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ShutdownTimeout                 time.Duration `env:"MATTERCODEX_BOT_SERVICE_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	MaxSlashFormBytes               int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_SLASH_FORM_BYTES" envDefault:"65536"`
	MaxGitHubWebhookBytes           int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES" envDefault:"262144"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load bot-service config: %w", err)
	}
	return cfg, nil
}

func (cfg *Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_HTTP_ADDR is required")
	}
	if cfg.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT is invalid")
	}
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_SHUTDOWN_TIMEOUT is invalid")
	}
	if cfg.MaxSlashFormBytes <= 0 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_MAX_SLASH_FORM_BYTES is invalid")
	}
	if cfg.MaxGitHubWebhookBytes <= 0 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES is invalid")
	}
	if cfg.RuntimeJobTTLSeconds <= 0 {
		return fmt.Errorf("MATTERCODEX_RUNTIME_JOB_TTL_SECONDS is invalid")
	}
	if cfg.RuntimeRetentionEnabled {
		if cfg.RuntimeRetentionInterval <= 0 {
			return fmt.Errorf("MATTERCODEX_RUNTIME_RETENTION_INTERVAL is invalid")
		}
		if cfg.RuntimeRetentionOlderThan <= 0 {
			return fmt.Errorf("MATTERCODEX_RUNTIME_RETENTION_OLDER_THAN is invalid")
		}
	}
	if cfg.RuntimeSessionRepairEnabled {
		if cfg.RuntimeSessionRepairInterval <= 0 {
			return fmt.Errorf("MATTERCODEX_RUNTIME_SESSION_REPAIR_INTERVAL is invalid")
		}
		if cfg.RuntimeSessionRepairBatch <= 0 {
			return fmt.Errorf("MATTERCODEX_RUNTIME_SESSION_REPAIR_BATCH is invalid")
		}
	}
	if cfg.AuthCheckJobTTLSeconds <= 0 {
		return fmt.Errorf("MATTERCODEX_CODEX_AUTH_CHECK_JOB_TTL_SECONDS is invalid")
	}
	if cfg.RuntimeLogTailLines <= 0 {
		return fmt.Errorf("MATTERCODEX_RUNTIME_LOG_TAIL_LINES is invalid")
	}
	if strings.TrimSpace(cfg.RuntimeSmokeImage) == "" {
		return fmt.Errorf("MATTERCODEX_RUNTIME_SMOKE_IMAGE is required")
	}
	if strings.TrimSpace(cfg.AgentRunnerImage) == "" {
		return fmt.Errorf("MATTERCODEX_AGENT_RUNNER_IMAGE is required")
	}
	if strings.TrimSpace(cfg.CodexPackage) == "" {
		return fmt.Errorf("MATTERCODEX_CODEX_PACKAGE is required")
	}
	if strings.TrimSpace(cfg.RuntimeWorkspaceSize) == "" {
		return fmt.Errorf("MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE is required")
	}
	if strings.TrimSpace(cfg.AgentServiceAccount) == "" {
		return fmt.Errorf("MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT is required")
	}
	if strings.TrimSpace(cfg.AgentClusterAdminServiceAccount) == "" {
		return fmt.Errorf("MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT is required")
	}
	if strings.TrimSpace(cfg.CodexAuthSecretName) == "" {
		return fmt.Errorf("MATTERCODEX_CODEX_AUTH_SECRET is required")
	}
	if strings.TrimSpace(cfg.GitHubSecretName) == "" {
		return fmt.Errorf("MATTERCODEX_GITHUB_SECRET is required")
	}
	locale, ok := texti18n.ResolveLocale(cfg.Locale)
	if !ok {
		return fmt.Errorf("MATTERCODEX_LOCALE must be one of: %s", strings.Join(texti18n.SupportedLocales(), ", "))
	}
	cfg.Locale = locale
	if len(cfg.DefaultChannels) == 0 {
		return fmt.Errorf("MATTERCODEX_DEFAULT_CHANNELS is required")
	}
	for _, channel := range cfg.DefaultChannels {
		if strings.TrimSpace(channelName(channel)) == "" {
			return fmt.Errorf("MATTERCODEX_DEFAULT_CHANNELS contains empty channel name")
		}
	}
	return nil
}

func (cfg Config) BotTokenConfigured() bool {
	return strings.TrimSpace(cfg.MattermostBotToken) != ""
}

func (cfg Config) MattermostAPIURL() string {
	if internalURL := strings.TrimSpace(cfg.MattermostInternalURL); internalURL != "" {
		return internalURL
	}
	return strings.TrimSpace(cfg.MattermostSiteURL)
}

func (cfg Config) SlashTokenConfigured() bool {
	return strings.TrimSpace(cfg.MattermostSlashToken) != ""
}

func (cfg Config) GitHubTokenConfigured() bool {
	return strings.TrimSpace(cfg.GitHubToken) != ""
}

func (cfg Config) GitHubWebhookConfigured() bool {
	return strings.TrimSpace(cfg.GitHubWebhookSecret) != ""
}

func (cfg Config) DatabaseConfigured() bool {
	return strings.TrimSpace(cfg.DatabaseDSN) != ""
}

func (cfg Config) ChannelNames() []string {
	channels := make([]string, 0, len(cfg.DefaultChannels))
	for _, channel := range cfg.DefaultChannels {
		channels = append(channels, channelName(channel))
	}
	return channels
}

func channelName(value string) string {
	name, _, _ := strings.Cut(value, ":")
	return strings.TrimSpace(name)
}
