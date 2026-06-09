package service

import (
	"strings"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

type Version = value.ServiceVersion

type Config struct {
	Localizer            *texti18n.Localizer
	ServiceName          string
	ServiceVersion       Version
	MattermostConfigured bool
	BotTokenConfigured   bool
	SlashTokenConfigured bool
	DatabaseConfigured   bool
	StorageReady         bool
	DefaultTeamName      string
	DefaultChannels      []string
}

type StatusService struct {
	cfg Config
}

func NewStatusService(cfg Config) *StatusService {
	return &StatusService{cfg: cfg}
}

func (svc *StatusService) Snapshot() value.StatusSnapshot {
	return value.StatusSnapshot{
		Status:               "ok",
		ServiceName:          svc.cfg.ServiceName,
		ServiceVersion:       svc.cfg.ServiceVersion,
		MattermostConfigured: svc.cfg.MattermostConfigured,
		BotTokenConfigured:   svc.cfg.BotTokenConfigured,
		SlashTokenConfigured: svc.cfg.SlashTokenConfigured,
		DatabaseConfigured:   svc.cfg.DatabaseConfigured,
		StorageReady:         svc.cfg.StorageReady,
		DefaultTeamName:      svc.cfg.DefaultTeamName,
		DefaultChannels:      append([]string(nil), svc.cfg.DefaultChannels...),
	}
}

func (svc *StatusService) SlashStatusText() string {
	snapshot := svc.Snapshot()
	return svc.cfg.Localizer.T("status.text", map[string]any{
		"ServiceName":     snapshot.ServiceName,
		"ServiceVersion":  string(snapshot.ServiceVersion),
		"Locale":          svc.cfg.Localizer.Locale(),
		"Mattermost":      configuredLabel(svc.cfg.Localizer, snapshot.MattermostConfigured),
		"BotToken":        configuredLabel(svc.cfg.Localizer, snapshot.BotTokenConfigured),
		"SlashToken":      configuredLabel(svc.cfg.Localizer, snapshot.SlashTokenConfigured),
		"Database":        configuredLabel(svc.cfg.Localizer, snapshot.DatabaseConfigured),
		"Storage":         readyLabel(svc.cfg.Localizer, snapshot.StorageReady),
		"DefaultTeamName": snapshot.DefaultTeamName,
		"DefaultChannels": strings.Join(snapshot.DefaultChannels, ", "),
	})
}

func configuredLabel(localizer *texti18n.Localizer, configured bool) string {
	if configured {
		return localizer.T("label.configured", nil)
	}
	return localizer.T("label.missing", nil)
}

func readyLabel(localizer *texti18n.Localizer, ready bool) string {
	if ready {
		return localizer.T("label.ready", nil)
	}
	return localizer.T("label.not_ready", nil)
}
