package app

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("MATTERCODEX_MATTERMOST_SITE_URL", "")
	t.Setenv("MATTERCODEX_BOT_SERVICE_SITE_URL", "")
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
	if !cfg.StorageMigrations {
		t.Fatal("StorageMigrations = false")
	}
}

func TestConfigValidationRejectsBadTimeout(t *testing.T) {
	cfg := Config{
		HTTPAddr:              ":8080",
		DefaultChannels:       []string{"agents-control:Agents Control"},
		ReadHeaderTimeout:     time.Second,
		ShutdownTimeout:       0,
		MaxSlashFormBytes:     1024,
		MaxGitHubWebhookBytes: 1024,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}
