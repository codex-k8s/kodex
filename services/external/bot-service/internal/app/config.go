package app

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
	"github.com/jackc/pgx/v5"
)

var kubernetesDNSLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Config contains bot-service process settings.
type Config struct {
	HTTPAddr                            string        `env:"MATTERCODEX_BOT_SERVICE_HTTP_ADDR" envDefault:":8080"`
	MattermostSiteURL                   string        `env:"MATTERCODEX_MATTERMOST_SITE_URL"`
	MattermostInternalURL               string        `env:"MATTERCODEX_MATTERMOST_INTERNAL_URL"`
	BotServiceSiteURL                   string        `env:"MATTERCODEX_BOT_SERVICE_SITE_URL"`
	BotServiceInternalURL               string        `env:"MATTERCODEX_BOT_SERVICE_INTERNAL_URL"`
	Locale                              string        `env:"MATTERCODEX_LOCALE" envDefault:"en"`
	DefaultTeamName                     string        `env:"MATTERCODEX_DEFAULT_TEAM_NAME" envDefault:"agents"`
	DefaultChannels                     []string      `env:"MATTERCODEX_DEFAULT_CHANNELS" envDefault:"agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit" envSeparator:","`
	OwnerMattermostUsername             string        `env:"MATTERCODEX_OWNER_MATTERMOST_USERNAME"`
	MattermostBotToken                  string        `env:"MATTERCODEX_MATTERMOST_BOT_TOKEN"`
	MattermostAdminToken                string        `env:"MATTERCODEX_MATTERMOST_ADMIN_TOKEN"`
	MattermostSlashToken                string        `env:"MATTERCODEX_MATTERMOST_SLASH_TOKEN"`
	GitHubToken                         string        `env:"MATTERCODEX_GITHUB_TOKEN"`
	GitHubWebhookSecret                 string        `env:"MATTERCODEX_GITHUB_WEBHOOK_SECRET"`
	GitHubSecretName                    string        `env:"MATTERCODEX_GITHUB_SECRET" envDefault:"matter-codex-github"`
	DatabaseDSN                         string        `env:"MATTERCODEX_DATABASE_DSN"`
	MigrationsDatabaseDSN               string        `env:"MATTERCODEX_MIGRATIONS_DATABASE_DSN"`
	RuntimeEnabled                      bool          `env:"MATTERCODEX_RUNTIME_ENABLED" envDefault:"true"`
	RuntimeNamespace                    string        `env:"MATTERCODEX_RUNTIME_NAMESPACE"`
	RuntimeKubeconfigPath               string        `env:"MATTERCODEX_RUNTIME_KUBECONFIG_PATH"`
	RuntimeSmokeImage                   string        `env:"MATTERCODEX_RUNTIME_SMOKE_IMAGE" envDefault:"busybox:1.36"`
	AgentRunnerImage                    string        `env:"MATTERCODEX_AGENT_RUNNER_IMAGE" envDefault:"matter-codex-agent-runner:dev"`
	CodexPackage                        string        `env:"MATTERCODEX_CODEX_PACKAGE" envDefault:"@openai/codex@0.144.1"`
	RuntimeWorkspaceSize                string        `env:"MATTERCODEX_RUNTIME_WORKSPACE_STORAGE_SIZE" envDefault:"1Gi"`
	AgentSessionMemoryLimit             string        `env:"MATTERCODEX_AGENT_SESSION_MEMORY_LIMIT" envDefault:"64Gi"`
	AgentUtilityMemoryLimit             string        `env:"MATTERCODEX_AGENT_UTILITY_MEMORY_LIMIT" envDefault:"4Gi"`
	AgentDevShmSizeLimit                string        `env:"MATTERCODEX_AGENT_DEV_SHM_SIZE_LIMIT" envDefault:"8Gi"`
	RuntimeJobTTLSeconds                int32         `env:"MATTERCODEX_RUNTIME_JOB_TTL_SECONDS" envDefault:"86400"`
	RuntimeRetentionEnabled             bool          `env:"MATTERCODEX_RUNTIME_RETENTION_ENABLED" envDefault:"true"`
	RuntimeRetentionInterval            time.Duration `env:"MATTERCODEX_RUNTIME_RETENTION_INTERVAL" envDefault:"30m"`
	RuntimeRetentionOlderThan           time.Duration `env:"MATTERCODEX_RUNTIME_RETENTION_OLDER_THAN" envDefault:"24h"`
	RuntimeSessionRepairEnabled         bool          `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_ENABLED" envDefault:"true"`
	RuntimeSessionRepairInterval        time.Duration `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_INTERVAL" envDefault:"30s"`
	RuntimeSessionRepairBatch           int           `env:"MATTERCODEX_RUNTIME_SESSION_REPAIR_BATCH" envDefault:"20"`
	AuthCheckJobTTLSeconds              int32         `env:"MATTERCODEX_CODEX_AUTH_CHECK_JOB_TTL_SECONDS" envDefault:"300"`
	RuntimeLogTailLines                 int64         `env:"MATTERCODEX_RUNTIME_LOG_TAIL_LINES" envDefault:"40"`
	AgentServiceAccount                 string        `env:"MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT" envDefault:"matter-codex-agent-runner"`
	AgentClusterAdminServiceAccount     string        `env:"MATTERCODEX_AGENT_RUNNER_CLUSTER_ADMIN_SERVICE_ACCOUNT" envDefault:"matter-codex-agent-runner-cluster-admin"`
	CodexAuthSecretName                 string        `env:"MATTERCODEX_CODEX_AUTH_SECRET" envDefault:"matter-codex-codex-auth"`
	StorageMigrations                   bool          `env:"MATTERCODEX_STORAGE_MIGRATIONS_ENABLED" envDefault:"true"`
	ReadHeaderTimeout                   time.Duration `env:"MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout                         time.Duration `env:"MATTERCODEX_BOT_SERVICE_READ_TIMEOUT" envDefault:"10s"`
	IdleTimeout                         time.Duration `env:"MATTERCODEX_BOT_SERVICE_IDLE_TIMEOUT" envDefault:"60s"`
	MaxHeaderBytes                      int           `env:"MATTERCODEX_BOT_SERVICE_MAX_HEADER_BYTES" envDefault:"1048576"`
	ShutdownTimeout                     time.Duration `env:"MATTERCODEX_BOT_SERVICE_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	MaxSlashFormBytes                   int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_SLASH_FORM_BYTES" envDefault:"65536"`
	MaxGitHubWebhookBytes               int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_GITHUB_WEBHOOK_BYTES" envDefault:"262144"`
	MaxMCPRequestBodyBytes              int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES" envDefault:"1048576"`
	InteractionCleanupEnabled           bool          `env:"MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_ENABLED" envDefault:"true"`
	InteractionCleanupInterval          time.Duration `env:"MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_INTERVAL" envDefault:"30m"`
	InteractionCleanupRetention         time.Duration `env:"MATTERCODEX_INTERACTION_CAPABILITY_RETENTION" envDefault:"168h"`
	InteractionCleanupBatch             int           `env:"MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_BATCH" envDefault:"500"`
	MattermostHTTPTimeout               time.Duration `env:"MATTERCODEX_MATTERMOST_HTTP_TIMEOUT" envDefault:"5s"`
	MattermostHTTPDialTimeout           time.Duration `env:"MATTERCODEX_MATTERMOST_HTTP_DIAL_TIMEOUT" envDefault:"2s"`
	MattermostHTTPTLSHandshakeTimeout   time.Duration `env:"MATTERCODEX_MATTERMOST_HTTP_TLS_HANDSHAKE_TIMEOUT" envDefault:"2s"`
	MattermostHTTPResponseHeaderTimeout time.Duration `env:"MATTERCODEX_MATTERMOST_HTTP_RESPONSE_HEADER_TIMEOUT" envDefault:"3s"`
	MattermostHTTPIdleConnTimeout       time.Duration `env:"MATTERCODEX_MATTERMOST_HTTP_IDLE_CONN_TIMEOUT" envDefault:"30s"`
	CallbackMaxBytes                    int           `env:"MATTERCODEX_CALLBACK_MAX_BYTES" envDefault:"131072"`
	CallbackMaxChunks                   int           `env:"MATTERCODEX_CALLBACK_MAX_CHUNKS" envDefault:"8"`
	CallbackMaxChunkBytes               int           `env:"MATTERCODEX_CALLBACK_MAX_CHUNK_BYTES" envDefault:"49152"`
	CallbackPublishConcurrency          int           `env:"MATTERCODEX_CALLBACK_PUBLISH_CONCURRENCY" envDefault:"4"`
	CallbackPublishDeadline             time.Duration `env:"MATTERCODEX_CALLBACK_PUBLISH_DEADLINE" envDefault:"5s"`
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
	cfg.applyPublishDefaults()
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_HTTP_ADDR is required")
	}
	if cfg.ReadHeaderTimeout <= 0 || cfg.ReadHeaderTimeout > 30*time.Second {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT is invalid")
	}
	if cfg.ReadTimeout < cfg.ReadHeaderTimeout || cfg.ReadTimeout > time.Minute {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_READ_TIMEOUT is invalid")
	}
	if cfg.IdleTimeout <= 0 || cfg.IdleTimeout > 5*time.Minute {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_IDLE_TIMEOUT is invalid")
	}
	if cfg.MaxHeaderBytes < 1024 || cfg.MaxHeaderBytes > 1024*1024 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_MAX_HEADER_BYTES is invalid")
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
	if cfg.MaxMCPRequestBodyBytes <= 0 || cfg.MaxMCPRequestBodyBytes > 2*1024*1024 {
		return fmt.Errorf("MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES is invalid")
	}
	if cfg.MattermostHTTPTimeout <= 0 || cfg.MattermostHTTPTimeout > 15*time.Second {
		return fmt.Errorf("MATTERCODEX_MATTERMOST_HTTP_TIMEOUT is invalid")
	}
	if cfg.MattermostHTTPDialTimeout <= 0 || cfg.MattermostHTTPDialTimeout > cfg.MattermostHTTPTimeout {
		return fmt.Errorf("MATTERCODEX_MATTERMOST_HTTP_DIAL_TIMEOUT is invalid")
	}
	if cfg.MattermostHTTPTLSHandshakeTimeout <= 0 || cfg.MattermostHTTPTLSHandshakeTimeout > cfg.MattermostHTTPTimeout {
		return fmt.Errorf("MATTERCODEX_MATTERMOST_HTTP_TLS_HANDSHAKE_TIMEOUT is invalid")
	}
	if cfg.MattermostHTTPResponseHeaderTimeout <= 0 || cfg.MattermostHTTPResponseHeaderTimeout > cfg.MattermostHTTPTimeout {
		return fmt.Errorf("MATTERCODEX_MATTERMOST_HTTP_RESPONSE_HEADER_TIMEOUT is invalid")
	}
	if cfg.MattermostHTTPIdleConnTimeout <= 0 || cfg.MattermostHTTPIdleConnTimeout > 5*time.Minute {
		return fmt.Errorf("MATTERCODEX_MATTERMOST_HTTP_IDLE_CONN_TIMEOUT is invalid")
	}
	if cfg.CallbackMaxBytes <= 0 || cfg.CallbackMaxBytes > 256*1024 {
		return fmt.Errorf("MATTERCODEX_CALLBACK_MAX_BYTES is invalid")
	}
	if cfg.MaxMCPRequestBodyBytes < int64(cfg.CallbackMaxBytes*6+16*1024) {
		return fmt.Errorf("MCP request body and callback byte limits are inconsistent")
	}
	if cfg.CallbackMaxChunks < 2 || cfg.CallbackMaxChunks > 16 {
		return fmt.Errorf("MATTERCODEX_CALLBACK_MAX_CHUNKS is invalid")
	}
	if cfg.CallbackMaxChunkBytes <= 256 || cfg.CallbackMaxChunkBytes > 64*1024 {
		return fmt.Errorf("MATTERCODEX_CALLBACK_MAX_CHUNK_BYTES is invalid")
	}
	if cfg.CallbackMaxBytes > (cfg.CallbackMaxChunks-1)*(cfg.CallbackMaxChunkBytes-256) {
		return fmt.Errorf("callback byte and chunk limits are inconsistent")
	}
	if cfg.CallbackPublishConcurrency <= 0 || cfg.CallbackPublishConcurrency > 32 {
		return fmt.Errorf("MATTERCODEX_CALLBACK_PUBLISH_CONCURRENCY is invalid")
	}
	if cfg.CallbackPublishDeadline <= 0 || cfg.CallbackPublishDeadline > 15*time.Second {
		return fmt.Errorf("MATTERCODEX_CALLBACK_PUBLISH_DEADLINE is invalid")
	}
	if cfg.InteractiveSurfaceEnabled() {
		if err := validateInternalServiceOrigin(cfg.BotServiceInternalURL); err != nil {
			return fmt.Errorf("MATTERCODEX_BOT_SERVICE_INTERNAL_URL is invalid: %w", err)
		}
	}
	if cfg.InteractionCleanupEnabled {
		if cfg.InteractionCleanupInterval <= 0 {
			return fmt.Errorf("MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_INTERVAL is invalid")
		}
		if cfg.InteractionCleanupRetention <= 0 {
			return fmt.Errorf("MATTERCODEX_INTERACTION_CAPABILITY_RETENTION is invalid")
		}
		if cfg.InteractionCleanupBatch <= 0 || cfg.InteractionCleanupBatch > 10000 {
			return fmt.Errorf("MATTERCODEX_INTERACTION_CAPABILITY_CLEANUP_BATCH is invalid")
		}
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
	if strings.TrimSpace(cfg.DatabaseDSN) != "" && cfg.StorageMigrations && strings.TrimSpace(cfg.MigrationsDatabaseDSN) == "" {
		return fmt.Errorf("MATTERCODEX_MIGRATIONS_DATABASE_DSN is required when storage migrations are enabled")
	}
	if strings.TrimSpace(cfg.DatabaseDSN) != "" {
		runtimeConfig, err := pgx.ParseConfig(cfg.DatabaseDSN)
		if err != nil || strings.TrimSpace(runtimeConfig.User) == "" || runtimeConfig.Password == "" {
			return fmt.Errorf("MATTERCODEX_DATABASE_DSN must contain explicit runtime credentials")
		}
		if strings.TrimSpace(cfg.MigrationsDatabaseDSN) != "" {
			migrationConfig, err := pgx.ParseConfig(cfg.MigrationsDatabaseDSN)
			if err != nil || strings.TrimSpace(migrationConfig.User) == "" {
				return fmt.Errorf("MATTERCODEX_MIGRATIONS_DATABASE_DSN must contain explicit migration credentials")
			}
			if runtimeConfig.User == migrationConfig.User {
				return fmt.Errorf("runtime and migrations database roles must be different")
			}
			if runtimeConfig.Host != migrationConfig.Host || runtimeConfig.Port != migrationConfig.Port || runtimeConfig.Database != migrationConfig.Database {
				return fmt.Errorf("runtime and migrations database credentials must use the same endpoint and database")
			}
		}
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

func (cfg *Config) applyPublishDefaults() {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = time.Minute
	}
	if cfg.MaxHeaderBytes == 0 {
		cfg.MaxHeaderBytes = 1024 * 1024
	}
	if cfg.MaxMCPRequestBodyBytes == 0 {
		cfg.MaxMCPRequestBodyBytes = 1024 * 1024
	}
	if cfg.MattermostHTTPTimeout == 0 {
		cfg.MattermostHTTPTimeout = 5 * time.Second
	}
	if cfg.MattermostHTTPDialTimeout == 0 {
		cfg.MattermostHTTPDialTimeout = 2 * time.Second
	}
	if cfg.MattermostHTTPTLSHandshakeTimeout == 0 {
		cfg.MattermostHTTPTLSHandshakeTimeout = 2 * time.Second
	}
	if cfg.MattermostHTTPResponseHeaderTimeout == 0 {
		cfg.MattermostHTTPResponseHeaderTimeout = 3 * time.Second
	}
	if cfg.MattermostHTTPIdleConnTimeout == 0 {
		cfg.MattermostHTTPIdleConnTimeout = 30 * time.Second
	}
	if cfg.CallbackMaxBytes == 0 {
		cfg.CallbackMaxBytes = 128 * 1024
	}
	if cfg.CallbackMaxChunks == 0 {
		cfg.CallbackMaxChunks = 8
	}
	if cfg.CallbackMaxChunkBytes == 0 {
		cfg.CallbackMaxChunkBytes = 48 * 1024
	}
	if cfg.CallbackPublishConcurrency == 0 {
		cfg.CallbackPublishConcurrency = 4
	}
	if cfg.CallbackPublishDeadline == 0 {
		cfg.CallbackPublishDeadline = 5 * time.Second
	}
}

func (cfg Config) InteractiveSurfaceEnabled() bool {
	return cfg.BotTokenConfigured() || cfg.SlashTokenConfigured()
}

func validateInternalServiceOrigin(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("scheme, host and port are required")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("userinfo, query, fragment and path are forbidden")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if net.ParseIP(hostname) != nil || !validKubernetesServiceDNSName(hostname) {
		return fmt.Errorf("host must be a Kubernetes Service DNS name")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("an explicit valid port is required")
	}
	return nil
}

func validKubernetesServiceDNSName(hostname string) bool {
	labels := strings.Split(hostname, ".")
	validSuffix := len(labels) >= 3 && labels[len(labels)-1] == "svc"
	if len(labels) >= 5 && strings.Join(labels[len(labels)-3:], ".") == "svc.cluster.local" {
		validSuffix = true
	}
	if !validSuffix {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || !kubernetesDNSLabelPattern.MatchString(label) {
			return false
		}
	}
	return len(hostname) <= 253
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
