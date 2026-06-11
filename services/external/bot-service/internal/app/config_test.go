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
	if cfg.CodexPackage != "@openai/codex@0.138.0" {
		t.Fatalf("CodexPackage = %q", cfg.CodexPackage)
	}
	if cfg.RuntimeWorkspaceSize != "1Gi" {
		t.Fatalf("RuntimeWorkspaceSize = %q", cfg.RuntimeWorkspaceSize)
	}
	if cfg.CodexAuthSecretName != "matter-codex-codex-auth" {
		t.Fatalf("CodexAuthSecretName = %q", cfg.CodexAuthSecretName)
	}
	if !cfg.StorageMigrations {
		t.Fatal("StorageMigrations = false")
	}
}

func TestConfigValidationRejectsBadTimeout(t *testing.T) {
	cfg := Config{
		HTTPAddr:              ":8080",
		Locale:                "en",
		DefaultChannels:       []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:     time.Second,
		ShutdownTimeout:       0,
		MaxSlashFormBytes:     1024,
		MaxGitHubWebhookBytes: 1024,
		RuntimeSmokeImage:     "busybox:1.36",
		AgentRunnerImage:      "matter-codex-agent-runner:dev",
		CodexPackage:          "@openai/codex@0.138.0",
		RuntimeWorkspaceSize:  "1Gi",
		RuntimeJobTTLSeconds:  86400,
		RuntimeLogTailLines:   40,
		AgentServiceAccount:   "matter-codex-agent-runner",
		CodexAuthSecretName:   "matter-codex-codex-auth",
		GitHubSecretName:      "matter-codex-github",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestConfigValidationNormalizesLocale(t *testing.T) {
	cfg := Config{
		HTTPAddr:              ":8080",
		Locale:                "ru-RU",
		DefaultChannels:       []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:     time.Second,
		ShutdownTimeout:       time.Second,
		MaxSlashFormBytes:     1024,
		MaxGitHubWebhookBytes: 1024,
		RuntimeSmokeImage:     "busybox:1.36",
		AgentRunnerImage:      "matter-codex-agent-runner:dev",
		CodexPackage:          "@openai/codex@0.138.0",
		RuntimeWorkspaceSize:  "1Gi",
		RuntimeJobTTLSeconds:  86400,
		RuntimeLogTailLines:   40,
		AgentServiceAccount:   "matter-codex-agent-runner",
		CodexAuthSecretName:   "matter-codex-codex-auth",
		GitHubSecretName:      "matter-codex-github",
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
		HTTPAddr:              ":8080",
		Locale:                "fr",
		DefaultChannels:       []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:     time.Second,
		ShutdownTimeout:       time.Second,
		MaxSlashFormBytes:     1024,
		MaxGitHubWebhookBytes: 1024,
		RuntimeSmokeImage:     "busybox:1.36",
		AgentRunnerImage:      "matter-codex-agent-runner:dev",
		CodexPackage:          "@openai/codex@0.138.0",
		RuntimeWorkspaceSize:  "1Gi",
		RuntimeJobTTLSeconds:  86400,
		RuntimeLogTailLines:   40,
		AgentServiceAccount:   "matter-codex-agent-runner",
		CodexAuthSecretName:   "matter-codex-codex-auth",
		GitHubSecretName:      "matter-codex-github",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestFlowActionURLPrefersInternalURL(t *testing.T) {
	cfg := Config{
		BotServiceSiteURL:     "https://matter-codex.example.com",
		BotServiceInternalURL: "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080",
	}
	got := flowActionURL(cfg)
	want := "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080/mattermost/actions/flow"
	if got != want {
		t.Fatalf("flowActionURL() = %q, want %q", got, want)
	}
}

func TestAgentsActionURLPrefersSiteURL(t *testing.T) {
	cfg := Config{
		BotServiceSiteURL:     "https://matter-codex.example.com",
		BotServiceInternalURL: "http://matter-codex-bot-service.mattermost.svc.cluster.local:8080",
	}
	got := agentsActionURL(cfg)
	want := "https://matter-codex.example.com/mattermost/actions/agents"
	if got != want {
		t.Fatalf("agentsActionURL() = %q, want %q", got, want)
	}
}
