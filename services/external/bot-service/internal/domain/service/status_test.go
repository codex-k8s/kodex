package service

import (
	"strings"
	"testing"

	texti18n "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/i18n"
)

func TestStatusText(t *testing.T) {
	localizer, err := texti18n.New(texti18n.DefaultLocale)
	if err != nil {
		t.Fatalf("New localizer error = %v", err)
	}
	svc := NewStatusService(Config{
		Localizer:            localizer,
		ServiceName:          "matter-codex-bot-service",
		ServiceVersion:       "0.1.0",
		MattermostConfigured: true,
		BotTokenConfigured:   true,
		SlashTokenConfigured: true,
		RuntimeConfigured:    true,
		DefaultTeamName:      "agents",
		DefaultChannels:      []string{"agents-control", "agents-runs"},
	})

	text := svc.SlashStatusText()
	for _, want := range []string{
		"matter-codex: online",
		"locale: en",
		"bot token: configured",
		"slash token: configured",
		"kubernetes runtime: configured",
		"default channels: agents-control, agents-runs",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("SlashStatusText() missing %q in %q", want, text)
		}
	}
}
