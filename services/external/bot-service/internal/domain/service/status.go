package service

import (
	"strings"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/types/value"
)

type Version = value.ServiceVersion

type Config struct {
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
	return strings.Join([]string{
		"matter-codex: online",
		"service: " + snapshot.ServiceName + " " + string(snapshot.ServiceVersion),
		"mattermost: " + configuredLabel(snapshot.MattermostConfigured),
		"bot token: " + configuredLabel(snapshot.BotTokenConfigured),
		"slash token: " + configuredLabel(snapshot.SlashTokenConfigured),
		"database: " + configuredLabel(snapshot.DatabaseConfigured),
		"storage: " + readyLabel(snapshot.StorageReady),
		"default team: " + snapshot.DefaultTeamName,
		"default channels: " + strings.Join(snapshot.DefaultChannels, ", "),
	}, "\n")
}

func configuredLabel(configured bool) string {
	if configured {
		return "configured"
	}
	return "missing"
}

func readyLabel(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}
