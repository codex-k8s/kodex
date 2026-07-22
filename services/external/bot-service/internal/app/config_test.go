package app

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestFoundationDSNBuilderSupportsReservedPassword(t *testing.T) {
	password := "synthetic/with+reserved=value"
	command := exec.Command(
		"bash", "-c",
		`. "$1"; mattercodex_postgres_dsn "$2" "$3" "$4" "$5"`,
		"bash",
		filepath.Join(testRepositoryRoot(t), "scripts/lib/env.sh"),
		"runtime-user", password, "mattermost-postgres.mattermost.svc.cluster.local", "mattercodex",
	)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("foundation DSN builder error = %v", err)
	}
	config, err := pgx.ParseConfig(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("pgx.ParseConfig(foundation DSN) error = %v", err)
	}
	if config.User != "runtime-user" || config.Password != password || config.Database != "mattercodex" {
		t.Fatal("foundation DSN builder изменил parser-visible credentials")
	}
}

func TestConfigDefaults(t *testing.T) {
	t.Setenv("MATTERCODEX_MATTERMOST_SITE_URL", "")
	t.Setenv("MATTERCODEX_BOT_SERVICE_SITE_URL", "")
	t.Setenv("MATTERCODEX_LOCALE", "")
	t.Setenv("MATTERCODEX_MATTERMOST_BOT_TOKEN", "")
	t.Setenv("MATTERCODEX_MATTERMOST_SLASH_TOKEN", "")
	t.Setenv("MATTERCODEX_GITHUB_TOKEN", "")
	t.Setenv("MATTERCODEX_GITHUB_WEBHOOK_SECRET", "")
	t.Setenv("MATTERCODEX_DATABASE_DSN", "")
	t.Setenv("MATTERCODEX_MIGRATIONS_DATABASE_DSN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.DefaultTeamName != "agents" {
		t.Fatalf("DefaultTeamName = %q", cfg.DefaultTeamName)
	}
	if cfg.Locale != "en" {
		t.Fatalf("Locale = %q", cfg.Locale)
	}
	if len(cfg.ChannelNames()) != 4 {
		t.Fatalf("ChannelNames() len = %d", len(cfg.ChannelNames()))
	}
	if cfg.BotTokenConfigured() {
		t.Fatal("BotTokenConfigured() = true")
	}
	if cfg.SlashTokenConfigured() {
		t.Fatal("SlashTokenConfigured() = true")
	}
	if cfg.GitHubTokenConfigured() {
		t.Fatal("GitHubTokenConfigured() = true")
	}
	if cfg.GitHubWebhookConfigured() {
		t.Fatal("GitHubWebhookConfigured() = true")
	}
	if cfg.DatabaseConfigured() {
		t.Fatal("DatabaseConfigured() = true")
	}
	if !cfg.RuntimeEnabled {
		t.Fatal("RuntimeEnabled = false")
	}
	if cfg.RuntimeSmokeImage != "busybox:1.36" {
		t.Fatalf("RuntimeSmokeImage = %q", cfg.RuntimeSmokeImage)
	}
	if cfg.AgentRunnerImage != "matter-codex-agent-runner:dev" {
		t.Fatalf("AgentRunnerImage = %q", cfg.AgentRunnerImage)
	}
	if cfg.AgentClusterAdminServiceAccount != "matter-codex-agent-runner-cluster-admin" {
		t.Fatalf("AgentClusterAdminServiceAccount = %q", cfg.AgentClusterAdminServiceAccount)
	}
	if cfg.CodexPackage != "@openai/codex@0.144.1" {
		t.Fatalf("CodexPackage = %q", cfg.CodexPackage)
	}
	if cfg.RuntimeWorkspaceSize != "1Gi" {
		t.Fatalf("RuntimeWorkspaceSize = %q", cfg.RuntimeWorkspaceSize)
	}
	if cfg.AgentSessionCPURequest != "500m" || cfg.AgentSessionMemoryRequest != "1Gi" || cfg.AgentSessionMemoryLimit != "64Gi" || cfg.AgentUtilityMemoryLimit != "4Gi" || cfg.AgentDevShmSizeLimit != "8Gi" {
		t.Fatalf("agent resource defaults = cpu-request:%q memory-request:%q session-limit:%q utility-limit:%q dev-shm:%q", cfg.AgentSessionCPURequest, cfg.AgentSessionMemoryRequest, cfg.AgentSessionMemoryLimit, cfg.AgentUtilityMemoryLimit, cfg.AgentDevShmSizeLimit)
	}
	if !cfg.RuntimeRetentionEnabled {
		t.Fatal("RuntimeRetentionEnabled = false")
	}
	if cfg.RuntimeRetentionInterval != 30*time.Minute {
		t.Fatalf("RuntimeRetentionInterval = %s", cfg.RuntimeRetentionInterval)
	}
	if cfg.RuntimeRetentionOlderThan != 24*time.Hour {
		t.Fatalf("RuntimeRetentionOlderThan = %s", cfg.RuntimeRetentionOlderThan)
	}
	if cfg.AuthCheckJobTTLSeconds != 300 {
		t.Fatalf("AuthCheckJobTTLSeconds = %d", cfg.AuthCheckJobTTLSeconds)
	}
	if cfg.CodexAuthSecretName != "matter-codex-codex-auth" {
		t.Fatalf("CodexAuthSecretName = %q", cfg.CodexAuthSecretName)
	}
	if !cfg.StorageMigrations {
		t.Fatal("StorageMigrations = false")
	}
	if !cfg.InteractionCleanupEnabled || cfg.InteractionCleanupInterval != 30*time.Minute || cfg.InteractionCleanupRetention != 168*time.Hour || cfg.InteractionCleanupBatch != 500 {
		t.Fatalf("interaction cleanup defaults = enabled:%t interval:%s retention:%s batch:%d", cfg.InteractionCleanupEnabled, cfg.InteractionCleanupInterval, cfg.InteractionCleanupRetention, cfg.InteractionCleanupBatch)
	}
	if cfg.ReadHeaderTimeout != 5*time.Second || cfg.ReadTimeout != 10*time.Second || cfg.IdleTimeout != time.Minute || cfg.MaxHeaderBytes != 1024*1024 || cfg.MaxMCPRequestBodyBytes != 1024*1024 {
		t.Fatalf("HTTP boundary defaults = header:%s read:%s idle:%s max_header:%d max_mcp:%d", cfg.ReadHeaderTimeout, cfg.ReadTimeout, cfg.IdleTimeout, cfg.MaxHeaderBytes, cfg.MaxMCPRequestBodyBytes)
	}
}

func TestConfigRejectsShortControlCenterReadToken(t *testing.T) {
	t.Setenv("MATTERCODEX_CONTROL_CENTER_READ_TOKEN", "synthetic-short-token")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "MATTERCODEX_CONTROL_CENTER_READ_TOKEN") {
		t.Fatalf("короткий Control Center token принят: %v", err)
	}
}

func TestHTTPServerUsesBoundedReadAndConnectionSettings(t *testing.T) {
	cfg := Config{
		HTTPAddr:          ":9090",
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       7 * time.Second,
		IdleTimeout:       45 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
	server := newHTTPServer(cfg, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if server.Addr != cfg.HTTPAddr || server.ReadHeaderTimeout != cfg.ReadHeaderTimeout || server.ReadTimeout != cfg.ReadTimeout || server.IdleTimeout != cfg.IdleTimeout || server.MaxHeaderBytes != cfg.MaxHeaderBytes {
		t.Fatalf("server bounds = addr:%s header:%s read:%s idle:%s max_header:%d", server.Addr, server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout, server.MaxHeaderBytes)
	}
}

func TestConfigValidationDatabaseRoleBoundary(t *testing.T) {
	tests := []struct {
		name       string
		runtimeDSN string
		ownerDSN   string
		wantError  bool
	}{
		{
			name:       "separate roles on one endpoint",
			runtimeDSN: "postgres://runtime:runtime-password@database:5432/mattercodex?sslmode=disable",
			ownerDSN:   "postgres://owner:owner-password@database:5432/mattercodex?sslmode=disable",
		},
		{
			name:       "missing migration credentials",
			runtimeDSN: "postgres://runtime:runtime-password@database:5432/mattercodex?sslmode=disable",
			wantError:  true,
		},
		{
			name:       "same role",
			runtimeDSN: "postgres://runtime:runtime-password@database:5432/mattercodex?sslmode=disable",
			ownerDSN:   "postgres://runtime:owner-password@database:5432/mattercodex?sslmode=disable",
			wantError:  true,
		},
		{
			name:       "different endpoint",
			runtimeDSN: "postgres://runtime:runtime-password@database:5432/mattercodex?sslmode=disable",
			ownerDSN:   "postgres://owner:owner-password@other-database:5432/mattercodex?sslmode=disable",
			wantError:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MATTERCODEX_DATABASE_DSN", test.runtimeDSN)
			t.Setenv("MATTERCODEX_MIGRATIONS_DATABASE_DSN", test.ownerDSN)
			_, err := LoadConfig()
			if (err != nil) != test.wantError {
				t.Fatalf("LoadConfig() error = %v, wantError=%t", err, test.wantError)
			}
		})
	}
}

func TestConfigValidationRejectsBadTimeout(t *testing.T) {
	cfg := Config{
		HTTPAddr:                        ":8080",
		Locale:                          "en",
		DefaultChannels:                 []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:               time.Second,
		ShutdownTimeout:                 0,
		MaxSlashFormBytes:               1024,
		MaxGitHubWebhookBytes:           1024,
		RuntimeSmokeImage:               "busybox:1.36",
		AgentRunnerImage:                "matter-codex-agent-runner:dev",
		CodexPackage:                    "@openai/codex@0.144.1",
		RuntimeWorkspaceSize:            "1Gi",
		RuntimeJobTTLSeconds:            86400,
		AuthCheckJobTTLSeconds:          300,
		RuntimeLogTailLines:             40,
		AgentServiceAccount:             "matter-codex-agent-runner",
		AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName:             "matter-codex-codex-auth",
		GitHubSecretName:                "matter-codex-github",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestConfigValidationRejectsBadRuntimeRetention(t *testing.T) {
	cfg := Config{
		HTTPAddr:                        ":8080",
		Locale:                          "en",
		DefaultChannels:                 []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:               time.Second,
		ShutdownTimeout:                 time.Second,
		MaxSlashFormBytes:               1024,
		MaxGitHubWebhookBytes:           1024,
		RuntimeSmokeImage:               "busybox:1.36",
		AgentRunnerImage:                "matter-codex-agent-runner:dev",
		CodexPackage:                    "@openai/codex@0.144.1",
		RuntimeWorkspaceSize:            "1Gi",
		RuntimeJobTTLSeconds:            86400,
		RuntimeRetentionEnabled:         true,
		RuntimeRetentionInterval:        0,
		RuntimeRetentionOlderThan:       24 * time.Hour,
		AuthCheckJobTTLSeconds:          300,
		RuntimeLogTailLines:             40,
		AgentServiceAccount:             "matter-codex-agent-runner",
		AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName:             "matter-codex-codex-auth",
		GitHubSecretName:                "matter-codex-github",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestConfigValidationNormalizesLocale(t *testing.T) {
	cfg := Config{
		HTTPAddr:                        ":8080",
		Locale:                          "ru-RU",
		DefaultChannels:                 []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:               time.Second,
		ShutdownTimeout:                 time.Second,
		MaxSlashFormBytes:               1024,
		MaxGitHubWebhookBytes:           1024,
		RuntimeSmokeImage:               "busybox:1.36",
		AgentRunnerImage:                "matter-codex-agent-runner:dev",
		CodexPackage:                    "@openai/codex@0.144.1",
		RuntimeWorkspaceSize:            "1Gi",
		RuntimeJobTTLSeconds:            86400,
		AuthCheckJobTTLSeconds:          300,
		RuntimeLogTailLines:             40,
		AgentServiceAccount:             "matter-codex-agent-runner",
		AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName:             "matter-codex-codex-auth",
		GitHubSecretName:                "matter-codex-github",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.Locale != "ru" {
		t.Fatalf("Locale = %q", cfg.Locale)
	}
}

func TestConfigValidationRejectsUnsupportedLocale(t *testing.T) {
	cfg := Config{
		HTTPAddr:                        ":8080",
		Locale:                          "fr",
		DefaultChannels:                 []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:               time.Second,
		ShutdownTimeout:                 time.Second,
		MaxSlashFormBytes:               1024,
		MaxGitHubWebhookBytes:           1024,
		RuntimeSmokeImage:               "busybox:1.36",
		AgentRunnerImage:                "matter-codex-agent-runner:dev",
		CodexPackage:                    "@openai/codex@0.144.1",
		RuntimeWorkspaceSize:            "1Gi",
		RuntimeJobTTLSeconds:            86400,
		AuthCheckJobTTLSeconds:          300,
		RuntimeLogTailLines:             40,
		AgentServiceAccount:             "matter-codex-agent-runner",
		AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName:             "matter-codex-codex-auth",
		GitHubSecretName:                "matter-codex-github",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestMattermostAPIURLPrefersInternalURL(t *testing.T) {
	cfg := Config{
		MattermostSiteURL:     "https://mattermost.example.com",
		MattermostInternalURL: "http://mattermost.mattermost.svc.cluster.local:8065",
	}
	if got := cfg.MattermostAPIURL(); got != "http://mattermost.mattermost.svc.cluster.local:8065" {
		t.Fatalf("MattermostAPIURL() = %q", got)
	}
}

func TestMattermostAPIURLFallsBackToSiteURL(t *testing.T) {
	cfg := Config{
		MattermostSiteURL: "https://mattermost.example.com",
	}
	if got := cfg.MattermostAPIURL(); got != "https://mattermost.example.com" {
		t.Fatalf("MattermostAPIURL() = %q", got)
	}
}

func TestAgentsActionURLUsesClusterBoundary(t *testing.T) {
	cfg := Config{
		BotServiceSiteURL:     "https://matter-codex.example.com",
		BotServiceInternalURL: "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080",
	}
	got := agentsActionURL(cfg)
	want := "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080/mattermost/actions/agents"
	if got != want {
		t.Fatalf("agentsActionURL() = %q, want %q", got, want)
	}
}

func TestAgentsDialogURLUsesClusterBoundary(t *testing.T) {
	cfg := Config{
		BotServiceSiteURL:     "https://matter-codex.example.com",
		BotServiceInternalURL: "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080",
	}
	got := agentsDialogURL(cfg)
	want := "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080/mattermost/dialogs/agents"
	if got != want {
		t.Fatalf("agentsDialogURL() = %q, want %q", got, want)
	}
}

func TestInteractiveURLsNeverFallBackToPublicOrigin(t *testing.T) {
	cfg := Config{BotServiceSiteURL: "https://matter-codex.example.com"}
	if got := agentsActionURL(cfg); got != "" {
		t.Fatalf("agentsActionURL() = %q", got)
	}
	if got := agentsDialogURL(cfg); got != "" {
		t.Fatalf("agentsDialogURL() = %q", got)
	}
	if got := botServiceRuntimeURL(cfg); got != "" {
		t.Fatalf("botServiceRuntimeURL() = %q", got)
	}
}

func TestInteractiveSurfaceRequiresValidInternalServiceOrigin(t *testing.T) {
	valid := Config{
		MattermostSlashToken:  "configured",
		BotServiceInternalURL: "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080",
		HTTPAddr:              ":8080", Locale: "en", DefaultChannels: []string{"agents-control:Agents Control"},
		ReadHeaderTimeout: time.Second, ShutdownTimeout: time.Second, MaxSlashFormBytes: 1024, MaxGitHubWebhookBytes: 1024,
		RuntimeSmokeImage: "busybox:1.36", AgentRunnerImage: "matter-codex-agent-runner:dev", CodexPackage: "@openai/codex@0.144.1",
		RuntimeWorkspaceSize: "1Gi", RuntimeJobTTLSeconds: 86400, AuthCheckJobTTLSeconds: 300, RuntimeLogTailLines: 40,
		AgentServiceAccount: "matter-codex-agent-runner", AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName: "matter-codex-codex-auth", GitHubSecretName: "matter-codex-github",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid internal origin: %v", err)
	}
	if agentsActionURL(valid) == "" || agentsDialogURL(valid) == "" {
		t.Fatal("interactive callback URL is empty")
	}

	invalidOrigins := []string{
		"",
		"https://public.example.com:443",
		"http://127.0.0.1:8080",
		"http://service.svc:8080",
		"http://.svc:8080",
		"ftp://matter-codex-bot-service.mattermost.svc:8080",
		"http://user@matter-codex-bot-service.mattermost.svc:8080",
		"http://matter-codex-bot-service.mattermost.svc",
		"http://matter-codex-bot-service.mattermost.svc:8080/callback",
		"http://matter-codex-bot-service.mattermost.svc:8080?query=1",
		"http://matter-codex-bot-service.mattermost.svc:8080#fragment",
	}
	for _, origin := range invalidOrigins {
		t.Run(origin, func(t *testing.T) {
			cfg := valid
			cfg.BotServiceInternalURL = origin
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() accepted origin %q", origin)
			}
		})
	}
}

func TestInteractionCapabilityCleanupValidation(t *testing.T) {
	base := Config{
		HTTPAddr: ":8080", Locale: "en", DefaultChannels: []string{"agents-control:Agents Control"},
		ReadHeaderTimeout: time.Second, ShutdownTimeout: time.Second, MaxSlashFormBytes: 1024, MaxGitHubWebhookBytes: 1024,
		RuntimeSmokeImage: "busybox:1.36", AgentRunnerImage: "matter-codex-agent-runner:dev", CodexPackage: "@openai/codex@0.144.1",
		RuntimeWorkspaceSize: "1Gi", RuntimeJobTTLSeconds: 86400, AuthCheckJobTTLSeconds: 300, RuntimeLogTailLines: 40,
		AgentServiceAccount: "matter-codex-agent-runner", AgentClusterAdminServiceAccount: "matter-codex-agent-runner-cluster-admin",
		CodexAuthSecretName: "matter-codex-codex-auth", GitHubSecretName: "matter-codex-github",
		InteractionCleanupEnabled: true, InteractionCleanupInterval: time.Minute, InteractionCleanupRetention: time.Hour, InteractionCleanupBatch: 100,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid cleanup config: %v", err)
	}
	for _, mutate := range []func(*Config){
		func(cfg *Config) { cfg.InteractionCleanupInterval = 0 },
		func(cfg *Config) { cfg.InteractionCleanupRetention = 0 },
		func(cfg *Config) { cfg.InteractionCleanupBatch = 0 },
		func(cfg *Config) { cfg.InteractionCleanupBatch = 10001 },
	} {
		cfg := base
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted invalid cleanup config")
		}
	}
}
