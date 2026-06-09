package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config contains bot-service process settings.
type Config struct {
	HTTPAddr             string        `env:"MATTERCODEX_BOT_SERVICE_HTTP_ADDR" envDefault:":8080"`
	MattermostSiteURL    string        `env:"MATTERCODEX_MATTERMOST_SITE_URL"`
	BotServiceSiteURL    string        `env:"MATTERCODEX_BOT_SERVICE_SITE_URL"`
	DefaultTeamName      string        `env:"MATTERCODEX_DEFAULT_TEAM_NAME" envDefault:"agents"`
	DefaultChannels      []string      `env:"MATTERCODEX_DEFAULT_CHANNELS" envDefault:"agents-control:Agents Control,agents-runs:Agents Runs,agent-alerts:Agent Alerts,agents-audit:Agents Audit" envSeparator:","`
	MattermostBotToken   string        `env:"MATTERCODEX_MATTERMOST_BOT_TOKEN"`
	MattermostSlashToken string        `env:"MATTERCODEX_MATTERMOST_SLASH_TOKEN"`
	ReadHeaderTimeout    time.Duration `env:"MATTERCODEX_BOT_SERVICE_READ_HEADER_TIMEOUT" envDefault:"5s"`
	ShutdownTimeout      time.Duration `env:"MATTERCODEX_BOT_SERVICE_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	MaxSlashFormBytes    int64         `env:"MATTERCODEX_BOT_SERVICE_MAX_SLASH_FORM_BYTES" envDefault:"65536"`
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

func (cfg Config) Validate() error {
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

func (cfg Config) SlashTokenConfigured() bool {
	return strings.TrimSpace(cfg.MattermostSlashToken) != ""
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
