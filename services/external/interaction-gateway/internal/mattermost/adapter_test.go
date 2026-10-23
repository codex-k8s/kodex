package mattermost

import (
	"errors"
	"fmt"
	"testing"
	"testing/fstest"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	"github.com/mattermost/mattermost/server/public/model"
)

// TestEmptyMattermostCatalogKeepsAdapterConstructible checks that an optional adapter can start with no allowed provider hosts and no remote request.
// RU: TestEmptyMattermostCatalogKeepsAdapterConstructible проверяет создание необязательного адаптера без разрешённых узлов провайдера и без удалённого запроса.
func TestEmptyMattermostCatalogKeepsAdapterConstructible(t *testing.T) {
	t.Parallel()
	adapter, err := New(Config{
		CredentialDirectory: "/var/run/secrets/kodex/integration-connections",
		ProxyURL:            "http://egress-gateway.kodex-system.svc.cluster.local:8080",
		Timeout:             10 * time.Second,
	}, localizerForTest(t))
	if err != nil || adapter == nil {
		t.Fatalf("New() error = %v", err)
	}
}

// TestParseDecisionUsesBoundedCommands covers explicit bilingual decisions, nonempty feedback and rejection of unrelated or empty commands.
// RU: TestParseDecisionUsesBoundedCommands проверяет явные двуязычные решения, непустую обратную связь и отклонение посторонних либо пустых команд.
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

// TestPostedMessageDropsForeignChannelAndBot accepts a synthetic posted event only for the intended channel and a non-bot author.
// RU: TestPostedMessageDropsForeignChannelAndBot принимает синтетическое posted-событие только для заданного канала и автора не-бота.
func TestPostedMessageDropsForeignChannelAndBot(t *testing.T) {
	t.Parallel()
	event := model.NewWebSocketEvent(model.WebsocketEventPosted, "team", "channel", "user", nil, "").SetData(map[string]any{
		"post": fmt.Sprintf(`{"id":%q,"channel_id":"channel","user_id":%q,"message":"work"}`, testPostID, testUserID),
	})
	if post, ok := postedMessage(event, "channel", "bot"); !ok || post.Id != testPostID {
		t.Fatalf("postedMessage() = %#v, %v", post, ok)
	}
	if _, ok := postedMessage(event, "foreign", "bot"); ok {
		t.Fatal("postedMessage() accepted a foreign channel")
	}
	if _, ok := postedMessage(event, "channel", testUserID); ok {
		t.Fatal("postedMessage() accepted a bot post")
	}
}

// TestOutcomeDoesNotExposeProviderError ensures an arbitrary raw provider diagnostic becomes only the stable unavailable code.
// RU: TestOutcomeDoesNotExposeProviderError проверяет превращение произвольной сырой диагностики провайдера только в устойчивый код недоступности.
func TestOutcomeDoesNotExposeProviderError(t *testing.T) {
	t.Parallel()
	success, code := Outcome(errors.New("provider response with secret"))
	if success || code != "INTERACTION_UNAVAILABLE" {
		t.Fatalf("Outcome() = %v, %q", success, code)
	}
}

// localizerForTest constructs an in-memory English READY catalog and fails setup if localizer initialization is invalid.
// RU: localizerForTest создаёт английский каталог READY в памяти и завершает подготовку при ошибке инициализации локализатора.
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
