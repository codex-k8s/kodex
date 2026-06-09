package service

import (
	"strings"
	"testing"
)

func TestStatusText(t *testing.T) {
	svc := NewStatusService(Config{
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control", "agents-runs"},
	})

	text := svc.SlashStatusText()
	for _, want := range []string{
		"matter-codex: online",
		"bot token: configured",
		"slash token: configured",
		"default channels: agents-control, agents-runs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("SlashStatusText() missing %q in %q", want, text)
		}
	}
}
