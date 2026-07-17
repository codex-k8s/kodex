package app

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("MATTERCODEX_MATTERMOST_SITE_URL", "")
	t.Setenv("MATTERCODEX_BOT_SERVICE_SITE_URL", "")
	t.Setenv("MATTERCODEX_LOCALE", "")
	t.Setenv("MATTERCODEX_MATTERMOST_BOT_TOKEN", "")
	t.Setenv("MATTERCODEX_MATTERMOST_SLASH_TOKEN", "")
	t.Setenv("MATTERCODEX_GITHUB_TOKEN", "")
	t.Setenv("MATTERCODEX_GITHUB_WEBHOOK_SECRET", "")
	t.Setenv("MATTERCODEX_DATABASE_DSN", "")

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
