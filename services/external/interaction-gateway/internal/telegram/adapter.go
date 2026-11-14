package telegram

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/projectedcredential"
	"google.golang.org/protobuf/types/known/structpb"
)

// Config contains only deployment projections and finite budgets; a bot token is never configuration or a constructor argument.
// Config содержит лишь deployment projections и конечные бюджеты; bot token не является настройкой либо аргументом конструктора.
type Config struct {
	CredentialDirectory, ProxyURL, PublicOrigin string
	Timeout                                     time.Duration
}

// BeginOwner admits one exact immutable input through the authoritative CP transaction after fresh provider lookups.
// BeginOwner допускает один точный immutable input через авторитетную CP transaction после свежих provider lookups.
type BeginOwner func(context.Context, *cp.BeginInteractionDeliveryRequest) (*cp.BeginInteractionDeliveryResponse, error)

// Adapter owns only a projected credential reader and fixed transport; all subscriptions, grants and durable effect decisions remain in CP.
// Adapter владеет лишь projected credential reader и fixed transport; все subscriptions, grants и durable effect decisions остаются в CP.
type Adapter struct {
	credentials  *credentialfs.Store
	proxy        *url.URL
	origin       string
	timeout      time.Duration
	newTransport func() http.RoundTripper
}

// New validates local dependencies without contacting Telegram; no connection or empty public origin is not a successful provider verification.
// New проверяет локальные зависимости без обращения к Telegram; отсутствие подключения либо public origin не является успешной проверкой провайдера.
func New(config Config) (*Adapter, error) {
	store, err := credentialfs.New(config.CredentialDirectory)
	proxy, parseErr := url.Parse(config.ProxyURL)
	if err != nil || parseErr != nil || config.Timeout < time.Second || config.Timeout > 2*time.Minute ||
		proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc.cluster.local:8086" || proxy.User != nil ||
		proxy.Path != "" || proxy.RawPath != "" || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" || proxy.Opaque != "" ||
		(config.PublicOrigin != "" && integrationpackage.ValidateTelegramPublicOrigin(config.PublicOrigin) != nil) {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	adapter := &Adapter{credentials: store, proxy: proxy, origin: config.PublicOrigin, timeout: config.Timeout}
	adapter.newTransport = adapter.defaultTransport
	return adapter, nil
}

// defaultTransport preserves exact TLS SNI and the owner egress proxy while disabling reused-connection retries and HTTP tracing middleware for token-bearing request paths.
// defaultTransport сохраняет точные TLS SNI и owner egress proxy, отключая retries повторно используемого соединения и HTTP tracing middleware для путей с token.
func (adapter *Adapter) defaultTransport() http.RoundTripper {
	return &http.Transport{Proxy: http.ProxyURL(adapter.proxy), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "api.telegram.org"},
		DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSHandshakeTimeout: min(adapter.timeout, maximumAPICallTimeout), ResponseHeaderTimeout: min(adapter.timeout, maximumAPICallTimeout)}
}

// claimedSnapshot reconstructs the one canonical owner input from private pins and rejects inconsistent duplicate coordinates before credential reads or network calls.
// claimedSnapshot восстанавливает один канонический owner input из private pins и отклоняет противоречивые дубли координат до чтения реквизитов либо сети.
func claimedSnapshot(claim *cp.InteractionDeliveryClaim) (integrationpackage.TelegramDeliverySnapshot, error) {
	var snapshot integrationpackage.TelegramDeliverySnapshot
	if claim == nil || claim.GetTelegram() == nil || claim.GetTelegram().GetExpiresAt() == nil || claim.GetTelegram().GetExpiresAt().CheckValid() != nil ||
		claim.GetCapabilityKey() != "telegram.notifications" || claim.GetSourceCapabilityKey() != "telegram.notifications" || claim.GetApprovalGateRef() != "" ||
		claim.GetApprovalGateVersion() != 0 || claim.GetBaseUrl() != apiOrigin || claim.GetTeamName() != "" || claim.GetChannelName() != "" ||
		claim.GetExternalTeamRef() != "" || claim.GetExternalChannelRef() != "" || claim.GetExternalRootPostRef() != "" || claim.GetAcceptanceReceiptRef() != "" ||
		claim.GetTemplateData() != nil && len(claim.GetTemplateData().GetFields()) != 0 {
		return snapshot, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	pin := claim.GetTelegram()
	snapshot = integrationpackage.TelegramDeliverySnapshot{SubscriptionRef: pin.GetSubscriptionRef(), SubscriptionVersion: pin.GetSubscriptionVersion(),
		ScopeDigest: pin.GetScopeDigest(), ProjectRef: pin.GetProjectRef(), RunRef: claim.GetRunRef(), GateRef: claim.GetGateRef(), GateVersion: claim.GetGateVersion(),
		EventType: pin.GetEventType(), TemplateKey: pin.GetTemplateKey(), ConnectionVersion: claim.GetConnectionVersion(), SourceRunVersion: pin.GetSourceRunVersion(),
		CredentialRevisionRef: claim.GetCredentialDescriptor().GetRef(), DefinitionDigest: claim.GetDefinitionDigest(), PublicOrigin: pin.GetPublicOrigin(),
		Configuration: pin.GetConfiguration(), ExpiresAt: pin.GetExpiresAt().AsTime().UTC()}
	digest, err := integrationpackage.TelegramDeliveryDigest(snapshot)
	configuration, configErr := integrationpackage.ParseTelegramConfiguration(snapshot.Configuration)
	if err != nil || configErr != nil || digest != pin.GetInputDigest() || configuration.BotID != pin.GetBotId() || configuration.ChatID != pin.GetChatId() ||
		configuration.TopicID != pin.GetTopicId() || claim.GetMessageKey() != snapshot.TemplateKey {
		return snapshot, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	return snapshot, nil
}

// resolvePackage requires the actual bounded private definition to match owner pins and the system-only executable Telegram contract; shipped fallback is forbidden.
// resolvePackage требует совпадения фактического bounded private definition с owner pins и системным Telegram contract; shipped fallback запрещён.
func resolvePackage(raw []byte, key, version, digest string) (integrationpackage.Package, error) {
	if len(raw) == 0 || len(raw) > 256<<10 {
		return integrationpackage.Package{}, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	definition, err := integrationpackage.Parse(raw)
	if err != nil || key != "telegram" || definition.Metadata.Key != key || definition.Metadata.Version != version || definition.Digest != digest ||
		!definition.ExecutableBy(integrationpackage.OwnerInteractionGateway, integrationpackage.RouteInteraction) {
		return definition, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	return definition, nil
}

// client loads only the exact owner descriptor, clears the byte buffer, and returns a short-lived noninstrumented token client whose token is cleared on close.
// client загружает лишь точный owner descriptor, очищает byte buffer и возвращает краткоживущий неинструментированный token client с очисткой token при закрытии.
func (adapter *Adapter) client(ctx context.Context, credential *cp.IntegrationCredentialRevision) (*apiClient, func(), error) {
	raw, err := projectedcredential.Read(ctx, adapter.credentials, credential, adapter.timeout)
	if err != nil {
		return nil, func() {}, &SafeError{Code: errorPreflightUnavailable, NoEffect: true}
	}
	defer clear(raw)
	transport := adapter.newTransport()
	client, err := newAPIClient(string(raw), transport, min(adapter.timeout, maximumAPICallTimeout))
	cleanup := func() {
		if client != nil {
			client.token = ""
		}
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
	return client, cleanup, err
}

// TestConnection performs only current identity/chat/membership reads against exact configuration and credential pins; success never sends a message or approves a subscription.
// TestConnection выполняет лишь текущие identity/chat/membership reads по точным configuration и credential pins; успех не отправляет сообщение и не одобряет подписку.
func (adapter *Adapter) TestConnection(ctx context.Context, claim *cp.IntegrationConnectionTestClaim) (string, error) {
	if claim == nil {
		return "", &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	values := map[string]string{}
	if claim.GetPublicConfiguration() == nil || len(claim.GetPublicConfiguration().GetFields()) > 24 {
		return "", &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	for key, value := range claim.GetPublicConfiguration().GetFields() {
		text, ok := value.GetKind().(*structpb.Value_StringValue)
		if !ok {
			return "", &SafeError{Code: errorConfiguration, NoEffect: true}
		}
		values[key] = text.StringValue
	}
	definition, err := resolvePackage(claim.GetDefinitionPackage(), claim.GetDefinitionKey(), claim.GetDefinitionVersion(), claim.GetDefinitionDigest())
	if err != nil || definition.ValidateConfiguration(values) != nil {
		return "", &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	operation, cancel := context.WithTimeout(ctx, min(adapter.timeout, time.Duration(definition.Spec.HealthCheck.TimeoutSeconds)*time.Second))
	defer cancel()
	ctx = operation
	configuration, err := integrationpackage.ParseTelegramConfiguration(values)
	if err != nil {
		return "", &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	client, cleanup, err := adapter.client(ctx, claim.GetCredentialRevision())
	defer cleanup()
	if err != nil {
		return "", err
	}
	if _, err := client.currentScope(ctx, configuration); err != nil {
		return "", err
	}
	return "TELEGRAM_CONNECTION_VERIFIED", nil
}

// Deliver validates private immutable input, performs fresh provider permission checks, then obtains one committed owner Begin and dispatches exactly one safe template.
// Deliver проверяет private immutable input, выполняет свежие provider permission checks, получает один зафиксированный owner Begin и отправляет ровно один безопасный шаблон.
func (adapter *Adapter) Deliver(ctx context.Context, claim *cp.InteractionDeliveryClaim, begin BeginOwner) (*cp.TelegramDeliveryReceipt, error) {
	snapshot, err := claimedSnapshot(claim)
	if err != nil {
		return nil, err
	}
	lease := claim.GetLease()
	if claim.GetDeliveryRef() == "" || lease == nil || lease.GetRef() == "" || lease.GetFence() == "" || lease.GetGeneration() < 1 || lease.GetExpiresAt() == nil ||
		lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(time.Now().UTC()) {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	if begin == nil || snapshot.PublicOrigin != adapter.origin || !snapshot.ExpiresAt.After(time.Now().UTC()) {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	definition, err := resolvePackage(claim.GetDefinitionPackage(), claim.GetDefinitionKey(), claim.GetDefinitionVersion(), claim.GetDefinitionDigest())
	if err != nil || definition.ValidateConfiguration(snapshot.Configuration) != nil {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	capability, notificationsPresent := definition.Capability("telegram.notifications")
	if !notificationsPresent {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	operation, cancel := context.WithTimeout(ctx, min(adapter.timeout, time.Duration(capability.Execution.TimeoutSeconds)*time.Second))
	defer cancel()
	ctx = operation
	deadline := snapshot.ExpiresAt
	if lease.GetExpiresAt().AsTime().Before(deadline) {
		deadline = lease.GetExpiresAt().AsTime()
	}
	expiring, cancelExpiry := context.WithDeadline(ctx, deadline)
	defer cancelExpiry()
	ctx = expiring
	text, err := integrationpackage.TelegramNotificationText(snapshot, claim.GetLocale())
	if err != nil {
		return nil, &SafeError{Code: errorConfiguration, NoEffect: true}
	}
	configuration, _ := integrationpackage.ParseTelegramConfiguration(snapshot.Configuration)
	client, cleanup, err := adapter.client(ctx, claim.GetCredentialDescriptor())
	defer cleanup()
	if err != nil {
		return nil, err
	}
	proof, err := client.currentScope(ctx, configuration)
	if err != nil {
		return nil, err
	}
	admitted, err := begin(ctx, &cp.BeginInteractionDeliveryRequest{DeliveryRef: claim.GetDeliveryRef(), LeaseRef: lease.GetRef(), Fence: lease.GetFence(), Generation: lease.GetGeneration(), InputDigest: claim.GetTelegram().GetInputDigest()})
	if err != nil || admitted == nil {
		return nil, &SafeError{Code: errorUnknown}
	}
	if admitted.GetDeliveryRef() != claim.GetDeliveryRef() || admitted.GetInputDigest() != claim.GetTelegram().GetInputDigest() || admitted.GetAlreadyStarted() {
		return nil, &SafeError{Code: errorUnknown}
	}
	if !admitted.GetAllowed() {
		return nil, &SafeError{Code: errorAdmissionClosed, NoEffect: true}
	}
	request := map[string]any{"chat_id": configuration.ChatID, "text": text, "protect_content": true, "link_preview_options": map[string]bool{"is_disabled": true}}
	if configuration.TopicID != 0 {
		request["message_thread_id"] = configuration.TopicID
	}
	raw, err := client.call(ctx, operationSend, request)
	if err != nil {
		return nil, err
	}
	messageID, err := verifiedSentMessage(raw, proof, configuration, text)
	if err != nil {
		return nil, err
	}
	return &cp.TelegramDeliveryReceipt{BotId: configuration.BotID, ChatId: configuration.ChatID, TopicId: configuration.TopicID, MessageId: messageID}, nil
}
