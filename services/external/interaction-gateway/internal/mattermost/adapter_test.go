package mattermost

import (
	"errors"
	"testing"
	"testing/fstest"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	texti18n "github.com/codex-k8s/matter-codex/libs/go/i18n"
	"github.com/mattermost/mattermost/server/public/model"
)

func TestEmptyMattermostCatalogKeepsAdapterConstructible(t *testing.T) {
	t.Parallel()
	adapter, err := New(Config{
		CredentialDirectory: "/var/run/secrets/mattercodex/integration-connections",
		ProxyURL:            "http://egress-gateway.mattercodex-system.svc.cluster.local:8080",
		Timeout:             10 * time.Second,
	}, localizerForTest(t))
	if err != nil || adapter == nil {
		t.Fatalf("New() error = %v", err)
	}
}

func TestParseDecisionUsesBoundedCommands(t *testing.T) {
	t.Parallel()
	tests := map[string]controlplanev1.OwnerGateDecision{
		"approve":               controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE,
		" ОДОБРИТЬ ":            controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE,
		"reject":                controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REJECT,
		"отменить":              controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_CANCEL,
		"changes: add a source": controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES,
		"изменения: уточнить":   controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES,
		"changes:":              controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED,
		"run arbitrary command": controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED,
	}
	for input, expected := range tests {
		if actual := ParseDecision(input); actual != expected {
			t.Errorf("ParseDecision(%q) = %s, expected %s", input, actual, expected)
		}
	}
}

func TestPostedMessageDropsForeignChannelAndBot(t *testing.T) {
	t.Parallel()
	event := model.NewWebSocketEvent(model.WebsocketEventPosted, "team", "channel", "user", nil, "").SetData(map[string]any{
		"post": `{"id":"post","channel_id":"channel","user_id":"user","message":"work"}`,
	})
	if post, ok := postedMessage(event, "channel", "bot"); !ok || post.Id != "post" {
		t.Fatalf("postedMessage() = %#v, %v", post, ok)
	}
	if _, ok := postedMessage(event, "foreign", "bot"); ok {
		t.Fatal("postedMessage() accepted a foreign channel")
	}
	if _, ok := postedMessage(event, "channel", "user"); ok {
		t.Fatal("postedMessage() accepted a bot post")
	}
}

func TestOutcomeDoesNotExposeProviderError(t *testing.T) {
	t.Parallel()
	success, code := Outcome(errors.New("provider response with secret"))
	if success || code != "INTERACTION_UNAVAILABLE" {
		t.Fatalf("Outcome() = %v, %q", success, code)
	}
}

func localizerForTest(t *testing.T) *texti18n.Localizer {
	t.Helper()
	localizer, err := texti18n.New(texti18n.Config{
		Locale: texti18n.DefaultLocale, MessageFS: fstest.MapFS{
			"messages.en.yaml": {Data: []byte("READY:\n  other: Ready\n")},
		}, MessageFiles: []string{"messages.en.yaml"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return localizer
}
