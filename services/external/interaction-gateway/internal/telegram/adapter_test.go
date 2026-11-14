package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/projectedcredential"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// telegramAdapterFixture builds exact synthetic owner/package/credential pins and a private projected token; its transport never reaches an external network.
// telegramAdapterFixture создаёт точные synthetic owner/package/credential pins и private projected token; transport не обращается к внешней сети.
func telegramAdapterFixture(t *testing.T) (*Adapter, *cp.InteractionDeliveryClaim) {
	t.Helper()
	return telegramAdapterBudgetFixture(t, time.Second)
}

// telegramAdapterBudgetFixture keeps the production constructor budget while preparing exact synthetic owner and credential pins without external I/O.
// telegramAdapterBudgetFixture сохраняет production constructor budget, подготавливая точные synthetic owner и credential pins без внешнего I/O.
func telegramAdapterBudgetFixture(t *testing.T, timeout time.Duration) (*Adapter, *cp.InteractionDeliveryClaim) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "fixture-token"), []byte(fixtureToken), 0o400); err != nil {
		t.Fatal("prepare synthetic credential")
	}
	adapter, err := New(Config{CredentialDirectory: root, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8086", PublicOrigin: "https://kodex.example.test", Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	definition := definitions["telegram"]
	raw, _ := json.Marshal(definition)
	credentialDigest := sha256.Sum256([]byte(fixtureToken))
	credential := &cp.IntegrationCredentialRevision{Ref: "icr_fixture_owned", Revision: 1, SecretRef: projectedcredential.SecretPrefix + "fixture-token",
		SecretUid: "11111111-1111-4111-8111-111111111111", SecretResourceVersion: "1", ContentSha256: hex.EncodeToString(credentialDigest[:])}
	configuration := map[string]string{"base_url": apiOrigin, "bot_id": "123456", "chat_id": "-1001234", "topic": "77"}
	snapshot := integrationpackage.TelegramDeliverySnapshot{SubscriptionRef: "tgs_fixture_owned", SubscriptionVersion: 1, ScopeDigest: strings.Repeat("a", 64),
		ProjectRef: "project_fixture", RunRef: "run_fixture_owned", SourceRunVersion: 1, EventType: "FINISHED", TemplateKey: "TELEGRAM_FINISHED_V1", ConnectionVersion: 1,
		CredentialRevisionRef: credential.Ref, DefinitionDigest: definition.Digest, PublicOrigin: adapter.origin, Configuration: configuration, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	digest, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	if err != nil {
		t.Fatal("prepare immutable fixture input")
	}
	claim := &cp.InteractionDeliveryClaim{DeliveryRef: "delivery_fixture", ConnectionRef: "connection_fixture", ConnectionVersion: 1,
		DefinitionKey: "telegram", DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest, DefinitionPackage: raw,
		CredentialDescriptor: credential, BaseUrl: apiOrigin, CapabilityKey: "telegram.notifications", SourceCapabilityKey: "telegram.notifications",
		MessageKey: snapshot.TemplateKey, Locale: "ru", RunRef: snapshot.RunRef,
		Telegram: &cp.TelegramDeliveryPin{SubscriptionRef: snapshot.SubscriptionRef, SubscriptionVersion: 1, ScopeDigest: snapshot.ScopeDigest,
			BotId: 123456, ChatId: -1001234, TopicId: 77, EventType: snapshot.EventType, TemplateKey: snapshot.TemplateKey, InputDigest: digest, SourceRunVersion: 1,
			ExpiresAt: timestamppb.New(snapshot.ExpiresAt), ProjectRef: snapshot.ProjectRef, PublicOrigin: snapshot.PublicOrigin, Configuration: configuration},
		Lease: &cp.WorkLease{Ref: "lease_fixture", Fence: "fence_fixture", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}
	return adapter, claim
}

// telegramProviderFixture responds only to current read methods and one controlled send, preserving exact coordinates and the server-rendered text in its response.
// telegramProviderFixture отвечает лишь на текущие read methods и одну контролируемую отправку, сохраняя точные координаты и server-rendered text в ответе.
func telegramProviderFixture(t *testing.T, calls *[]string, sendStatus int) http.RoundTripper {
	t.Helper()
	return fixtureTransport(func(request *http.Request) (*http.Response, error) {
		method := strings.TrimPrefix(request.URL.Path, "/bot"+fixtureToken+"/")
		*calls = append(*calls, method)
		result := map[string]any{}
		switch method {
		case "getMe":
			result = map[string]any{"id": 123456, "is_bot": true}
		case "getChat":
			result = map[string]any{"id": -1001234, "type": "supergroup", "is_forum": true, "permissions": map[string]bool{"can_send_messages": true}}
		case "getChatMember":
			result = map[string]any{"status": "administrator", "user": map[string]any{"id": 123456, "is_bot": true}}
		case "sendMessage":
			raw, _ := io.ReadAll(request.Body)
			var input map[string]any
			if json.Unmarshal(raw, &input) != nil || len(input) != 5 || input["chat_id"] != float64(-1001234) || input["message_thread_id"] != float64(77) || input["protect_content"] != true {
				t.Fatal("send escaped exact safe scope")
			}
			preview, ok := input["link_preview_options"].(map[string]any)
			if !ok || len(preview) != 1 || preview["is_disabled"] != true || input["text"] != "Запуск завершён.\nhttps://kodex.example.test/projects/project_fixture/runs/run_fixture_owned" {
				t.Fatal("unsafe server template")
			}
			if sendStatus != 200 {
				return &http.Response{StatusCode: sendStatus, Body: io.NopCloser(strings.NewReader(`{"ok":false,"error_code":429,"parameters":{"retry_after":30}}`))}, nil
			}
			result = map[string]any{"message_id": 123, "message_thread_id": 77, "chat": map[string]any{"id": -1001234}, "from": map[string]any{"id": 123456, "is_bot": true}, "text": input["text"]}
		default:
			t.Fatal("unexpected synthetic provider operation")
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "result": result})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})
}

// TestTelegramDeliveryRequiresFreshProviderAndCommittedBegin proves exact execution order, safe text and no second send after a replayed Begin.
// TestTelegramDeliveryRequiresFreshProviderAndCommittedBegin доказывает точный порядок исполнения, безопасный текст и отсутствие повторной отправки после повторного Begin.
func TestTelegramDeliveryRequiresFreshProviderAndCommittedBegin(t *testing.T) {
	adapter, claim := telegramAdapterFixture(t)
	calls := []string{}
	adapter.newTransport = func() http.RoundTripper { return telegramProviderFixture(t, &calls, 200) }
	begin := func(ctx context.Context, request *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
		calls = append(calls, "BEGIN")
		if request.GetDeliveryRef() != claim.GetDeliveryRef() || request.GetLeaseRef() != claim.GetLease().GetRef() || request.GetInputDigest() != claim.GetTelegram().GetInputDigest() {
			t.Fatal("Begin lost exact owner pins")
		}
		return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.GetDeliveryRef(), InputDigest: claim.GetTelegram().GetInputDigest(), Allowed: true}, nil
	}
	receipt, err := adapter.Deliver(t.Context(), claim, begin)
	if err != nil || receipt.GetBotId() != 123456 || receipt.GetChatId() != -1001234 || receipt.GetTopicId() != 77 || receipt.GetMessageId() != 123 {
		t.Fatalf("exact delivery did not preserve receipt: %v; operations=%v", err, calls)
	}
	if !slices.Equal(calls, []string{"getMe", "getChat", "getChatMember", "BEGIN", "sendMessage"}) {
		t.Fatal("send preceded fresh provider or committed Begin")
	}
	_, err = adapter.Deliver(t.Context(), claim, func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
		return &cp.BeginInteractionDeliveryResponse{DeliveryRef: claim.GetDeliveryRef(), InputDigest: claim.GetTelegram().GetInputDigest(), AlreadyStarted: true}, nil
	})
	if err == nil || ConfirmedNoEffect(err) || len(calls) != 8 {
		t.Fatal("replayed Begin authorized another send")
	}
}

// TestTelegramDeliveryRejectsMalformedPinsBeforeProvider checks stale credentials, expired leases and mismatched private metadata without a network call or Begin.
// TestTelegramDeliveryRejectsMalformedPinsBeforeProvider проверяет stale credentials, expired leases и подмену private metadata без сети либо Begin.
func TestTelegramDeliveryRejectsMalformedPinsBeforeProvider(t *testing.T) {
	adapter, original := telegramAdapterFixture(t)
	for _, change := range []struct {
		name   string
		mutate func(*cp.InteractionDeliveryClaim)
	}{
		{name: "foreign_chat", mutate: func(c *cp.InteractionDeliveryClaim) { c.Telegram.ChatId-- }},
		{name: "changed_template", mutate: func(c *cp.InteractionDeliveryClaim) { c.Telegram.TemplateKey = "ARBITRARY_TEXT" }},
		{name: "expired_lease", mutate: func(c *cp.InteractionDeliveryClaim) {
			c.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		}},
		{name: "missing_package", mutate: func(c *cp.InteractionDeliveryClaim) { c.DefinitionPackage = nil }},
		{name: "credential_digest", mutate: func(c *cp.InteractionDeliveryClaim) { c.CredentialDescriptor.ContentSha256 = strings.Repeat("0", 64) }},
		{name: "other_origin", mutate: func(c *cp.InteractionDeliveryClaim) { c.Telegram.PublicOrigin = "https://foreign.example.test" }},
	} {
		t.Run(change.name, func(t *testing.T) {
			calls := 0
			adapter.newTransport = func() http.RoundTripper {
				calls++
				return fixtureTransport(func(*http.Request) (*http.Response, error) { t.Fatal("invalid pins reached provider"); return nil, nil })
			}
			claim := proto.Clone(original).(*cp.InteractionDeliveryClaim)
			change.mutate(claim)
			_, err := adapter.Deliver(t.Context(), claim, func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
				t.Fatal("invalid pins reached Begin")
				return nil, nil
			})
			if err == nil || !ConfirmedNoEffect(err) || calls != 0 {
				t.Fatal("malformed pins were not closed before provider")
			}
		})
	}
}

// TestTelegramHealthOnlyRevisionCannotBeginOrReadCredential proves a valid owner-managed narrowing cannot be used as notification authority even when its private snapshot pins are internally consistent.
// TestTelegramHealthOnlyRevisionCannotBeginOrReadCredential доказывает, что допустимое owner-managed сужение не выдаёт права уведомления даже при внутренне согласованных private snapshot pins.
func TestTelegramHealthOnlyRevisionCannotBeginOrReadCredential(t *testing.T) {
	adapter, claim := telegramAdapterFixture(t)
	definition, err := integrationpackage.Parse(claim.DefinitionPackage)
	if err != nil {
		t.Fatal(err)
	}
	definition.Metadata.Origin = integrationpackage.OriginUI
	definition.Metadata.Version = "2.0.0"
	health, exists := definition.Capability("telegram.connection.read")
	if !exists {
		t.Fatal("health operation missing")
	}
	definition.Spec.Capabilities = []integrationpackage.Capability{health}
	raw, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	definition, err = integrationpackage.Parse(raw)
	if err != nil {
		t.Fatal("health-only revision rejected", err)
	}
	claim.DefinitionPackage = raw
	claim.DefinitionVersion = definition.Metadata.Version
	claim.DefinitionDigest = definition.Digest
	snapshot, err := claimedSnapshot(claim)
	// Rebind only this synthetic source digest to the valid narrowed definition;
	// production consumers receive these immutable pins from the owner ledger.
	// Перепривязывается только synthetic source digest к допустимому сужению;
	// production consumers получают immutable pins из owner ledger.
	if err == nil {
		t.Fatal("changed package unexpectedly retained the old immutable input digest")
	}
	snapshot.DefinitionDigest = definition.Digest
	digest, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	claim.Telegram.InputDigest = digest
	calls := 0
	adapter.newTransport = func() http.RoundTripper { calls++; return telegramProviderFixture(t, new([]string), 200) }
	begin := func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error) {
		calls++
		return &cp.BeginInteractionDeliveryResponse{Allowed: true}, nil
	}
	if receipt, err := adapter.Deliver(t.Context(), claim, begin); err == nil || receipt != nil {
		t.Fatal("health-only package admitted a notification")
	}
	if calls != 0 {
		t.Fatal("health-only package reached credential transport or owner Begin")
	}
}
