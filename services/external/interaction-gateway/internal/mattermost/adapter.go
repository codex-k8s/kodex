// Package mattermost implements the optional chat adapter without treating external identifiers or post properties as core authority.
// RU: Пакет mattermost реализует необязательный чат-адаптер, не считая внешние идентификаторы или свойства сообщений полномочиями основного домена.
package mattermost

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/gorilla/websocket"
	"github.com/mattermost/mattermost/server/public/model"
)

var (
	// errConfiguration classifies invalid endpoint, connection or contract settings without exposing their contents.
	// RU: errConfiguration обозначает неверные настройки адреса, подключения или контракта без раскрытия их содержимого.
	errConfiguration = errors.New("mattermost interaction configuration is invalid")
	// errCredential reports unavailable or mismatched projected credentials without including secret data.
	// RU: errCredential сообщает о недоступных или несовпавших спроецированных реквизитах без данных секрета.
	errCredential = errors.New("mattermost interaction credential is unavailable")
	// errForbidden maps provider authentication or permission rejection to a closed adapter error.
	// RU: errForbidden отображает отказ провайдера в аутентификации или доступе в закрытую ошибку адаптера.
	errForbidden = errors.New("mattermost interaction operation is forbidden")
	// errRateLimited represents provider throttling without claiming that a dispatched write is safe to repeat.
	// RU: errRateLimited обозначает ограничение частоты провайдера, не утверждая допустимость повтора уже отправленной записи.
	errRateLimited = errors.New("mattermost interaction rate limit exceeded")
	// errUnavailable hides arbitrary provider or transport details behind a stable availability classification.
	// RU: errUnavailable скрывает произвольные данные провайдера или транспорта за устойчивой классификацией недоступности.
	errUnavailable = errors.New("mattermost interaction endpoint is unavailable")
	// errResponse rejects missing, malformed or incorrectly scoped provider results.
	// RU: errResponse отклоняет отсутствующие, неверные или выходящие за область подключения результаты провайдера.
	errResponse = errors.New("mattermost interaction response is invalid")
)

// Config selects projected credential storage, the fixed egress proxy, exact allowed hosts and the adapter timeout.
// RU: Config выбирает хранилище спроецированных реквизитов, фиксированный egress proxy, точные разрешённые узлы и timeout адаптера.
type Config struct {
	CredentialDirectory string
	ProxyURL            string
	AllowedHosts        string
	Timeout             time.Duration
}

// Adapter combines a shipped executable contract with per-operation clients; it does not store one connection's mutable managed revision globally.
// RU: Adapter объединяет поставленный исполняемый контракт с клиентами отдельных операций, не сохраняя изменяемую управляемую ревизию одного подключения глобально.
type Adapter struct {
	credentials  *credentialfs.Store
	proxy        *url.URL
	allowedHosts map[string]struct{}
	timeout      time.Duration
	text         *texti18n.Localizer
	definition   integrationpackage.Package
	newTransport func(*url.URL) http.RoundTripper
}

// Message carries re-read external post identity and optional gate correlation for authoritative acceptance by control-plane.
// RU: Message передаёт повторно прочитанную идентичность внешнего сообщения и необязательную корреляцию gate для авторитетного приёма control-plane.
// Neither its user digest nor gate properties independently grant permission to launch a run or resolve a gate.
// RU: Ни дайджест пользователя, ни свойства gate сами по себе не разрешают запуск или решение gate.
type Message struct {
	EventRef    string
	PostRef     string
	RootPostRef string
	ChannelRef  string
	TeamRef     string
	UserDigest  string
	Text        string
	Decision    controlplanev1.OwnerGateDecision
	GateRef     string
	GateVersion int64
	RunRef      string
}

// MessageHandler accepts one verified adapter message within its operation context; an error is returned to the source listener.
// RU: MessageHandler принимает одно проверенное адаптером сообщение в контексте операции; ошибка возвращается listener источника.
type MessageHandler func(context.Context, Message) error

// DeliveryResult records the observed post, thread, team and channel after an externally successful delivery for exact owner readback.
// RU: DeliveryResult сохраняет наблюдаемые сообщение, ветку, команду и канал после успешной внешней доставки для точной сверки владельцем.
type DeliveryResult struct {
	PostRef    string
	ThreadRef  string
	TeamRef    string
	ChannelRef string
}

// New validates the local credential root, exact proxy, timeout and host allowlist, then loads the shipped Mattermost execution baseline.
// RU: New проверяет локальный корень реквизитов, точный proxy, timeout и разрешённые узлы, затем загружает поставленную исполняемую основу Mattermost.
// An empty host allowlist permits construction but no external origin; this function does not contact the provider.
// RU: Пустой список узлов допускает создание адаптера, но не внешний адрес; функция не обращается к провайдеру.
func New(config Config, text *texti18n.Localizer) (*Adapter, error) {
	store, err := credentialfs.New(config.CredentialDirectory)
	if err != nil || text == nil || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, errConfiguration
	}
	proxy, err := url.Parse(config.ProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host != egressProxyHost || proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" || proxy.Opaque != "" {
		return nil, errConfiguration
	}
	hosts := map[string]struct{}{}
	for _, value := range strings.Split(config.AllowedHosts, ",") {
		host := strings.ToLower(strings.TrimSpace(value))
		if host == "" {
			continue
		}
		if !validHostname(host) {
			return nil, errConfiguration
		}
		hosts[host] = struct{}{}
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return nil, errConfiguration
	}
	adapter := &Adapter{credentials: store, proxy: proxy, allowedHosts: hosts, timeout: config.Timeout, text: text, definition: definitions["mattermost"]}
	adapter.newTransport = adapter.defaultTransport
	return adapter, nil
}

// Deliver validates managed delivery capability, localized content and exact channel/thread before creating one post within the capability budget.
// RU: Deliver проверяет управляемую capability доставки, локализованный текст и точные канал/ветку до создания одного сообщения в бюджете capability.
// Failures before dispatch are marked no-effect; a dispatched error is not automatically safe to retry, and credential bytes are not returned.
// RU: Отказы до отправки помечаются как отсутствие эффекта; ошибка после отправки не делает повтор автоматически безопасным, байты реквизитов не возвращаются.
func (adapter *Adapter) Deliver(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (result DeliveryResult, resultErr error) {
	dispatched := false
	defer func() {
		if resultErr != nil && !dispatched {
			resultErr = &noEffectError{cause: resultErr}
		}
	}()
	if claim == nil || claim.GetMessageKey() == "" {
		return result, errConfiguration
	}
	capability, err := adapter.deliveryCapability(claim)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(capability.Execution.TimeoutSeconds)*time.Second)
	defer cancel()
	gate, err := gateFromClaim(claim)
	if err != nil {
		return result, err
	}
	data := map[string]any{}
	if claim.GetTemplateData() != nil {
		data = claim.GetTemplateData().AsMap()
	}
	if state, ok := data["state"].(string); ok {
		data["state"] = adapter.text.Localize(claim.GetLocale(), "RUN_STATE_"+strings.ToUpper(state), nil)
	}
	message := adapter.text.Localize(claim.GetLocale(), claim.GetMessageKey(), data)
	if message == claim.GetMessageKey() || strings.TrimSpace(message) == "" || len(message) > 16<<10 {
		return result, errResponse
	}
	if claim.GetCapabilityKey() == "mattermost.notifications" || claim.GetCapabilityKey() == "mattermost.result_mirror" {
		raw, err := json.Marshal(map[string]string{"message": message})
		if err != nil {
			return result, errInvocation
		}
		if _, err := capability.ValidateInput(raw); err != nil {
			return result, errInvocation
		}
	}
	client, _, channel, closeClient, err := adapter.client(ctx, claim)
	if err != nil {
		return result, err
	}
	defer closeClient()
	root, err := deliveryRoot(ctx, client, channel, claim)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	dispatched = true
	postRef, threadRef, err := createPost(ctx, client, channel.Id, root, message, gate)
	if err != nil {
		return result, err
	}
	return DeliveryResult{PostRef: postRef, ThreadRef: threadRef, TeamRef: channel.TeamId, ChannelRef: channel.Id}, nil
}

// Listen checks the pinned source and credentials, opens proxied WSS and re-reads each eligible event before forwarding message and gate correlation.
// RU: Listen проверяет закреплённые источник и реквизиты, открывает WSS через proxy и перечитывает каждое допустимое событие до передачи сообщения и корреляции gate.
// Cancellation closes and drains the SDK socket; owner acceptance still decides user permissions and exact gate freshness.
// RU: Отмена закрывает и дренирует SDK сокет; полномочия пользователя и точную свежесть gate по-прежнему определяет приём у владельца.
func (adapter *Adapter) Listen(ctx context.Context, source *controlplanev1.InteractionSource, handler MessageHandler) error {
	if source == nil || handler == nil || !listens(source.GetEnabledCapabilities()) {
		return errConfiguration
	}
	if source.GetConnectionVersion() < 1 || source.GetCredentialRevisionRef() != source.GetCredentialDescriptor().GetRef() || source.GetCredentialRevision() != source.GetCredentialDescriptor().GetRevision() {
		return errConfiguration
	}
	budget, err := adapter.sourceBudget(source)
	if err != nil {
		return err
	}
	startup, cancelStartup := context.WithTimeout(ctx, budget)
	defer cancelStartup()
	client, token, channel, closeClient, err := adapter.client(startup, source)
	if err != nil {
		return err
	}
	defer closeClient()
	me, _, err := client.GetMe(startup, "")
	if err != nil || me == nil || !model.IsValidId(me.Id) {
		return classify(err)
	}
	cancelStartup()
	base, err := adapter.baseURL(source.GetBaseUrl())
	if err != nil {
		return err
	}
	websocketURL := *base
	websocketURL.Scheme = "wss"
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyURL(adapter.proxy),
		HandshakeTimeout: budget,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS13, ServerName: base.Hostname()},
	}
	socket, err := model.NewWebSocketClient4WithDialer(dialer, strings.TrimRight(websocketURL.String(), "/"), token)
	if err != nil {
		return errUnavailable
	}
	socket.Conn.SetReadLimit(maximumWebsocketBytes)
	socket.Listen()
	defer closeSocket(socket)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-socket.PingTimeoutChannel:
			return errUnavailable
		case _, ok := <-socket.ResponseChannel:
			// Канал обязательно дренируется по контракту официального клиента.
			if !ok {
				return errUnavailable
			}
		case event, ok := <-socket.EventChannel:
			if !ok {
				return errUnavailable
			}
			post, ok := postedMessage(event, channel.Id, me.Id)
			if !ok {
				continue
			}
			messageContext, cancelMessage := context.WithTimeout(ctx, budget)
			verified, _, verifyErr := client.GetPost(messageContext, post.Id, "")
			if verifyErr != nil {
				cancelMessage()
				return classify(verifyErr)
			}
			if !sameInboundPost(post, verified, channel.Id, me.Id) {
				cancelMessage()
				continue
			}
			post = verified
			gate, gateErr := readGateContext(messageContext, client, post, channel.Id, me.Id)
			if gateErr != nil {
				cancelMessage()
				return gateErr
			}
			key := "mattermost.inbound"
			if gate != nil {
				key = "mattermost.gate_decisions"
			}
			if !slices.Contains(source.GetEnabledCapabilities(), key) {
				cancelMessage()
				continue
			}
			decision := ParseDecision(post.Message)
			if decision != controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED && gate == nil {
				cancelMessage()
				continue
			}
			message := Message{
				EventRef: post.Id, PostRef: post.Id, RootPostRef: post.RootId,
				ChannelRef: post.ChannelId, TeamRef: channel.TeamId, UserDigest: digest(post.UserId),
				Text: strings.TrimSpace(post.Message), Decision: decision,
			}
			if gate != nil {
				message.GateRef, message.GateVersion, message.RunRef = gate.ref, gate.version, gate.runRef
			}
			if adapter.validateSourceInput(source, key, message.GateRef, decision) != nil {
				cancelMessage()
				continue
			}
			err := handler(messageContext, message)
			cancelMessage()
			if err != nil {
				return err
			}
		}
	}
}

// client checks the allowed origin, reads exact credential content and resolves an authenticated channel, clearing the temporary byte buffer on return.
// RU: client проверяет разрешённый адрес, читает точное содержимое реквизитов и разрешает аутентифицированный канал, очищая временный байтовый буфер при возврате.
func (adapter *Adapter) client(ctx context.Context, source source) (*model.Client4, string, *model.Channel, func(), error) {
	base, err := adapter.baseURL(source.GetBaseUrl())
	if err != nil {
		return nil, "", nil, func() {}, err
	}
	raw, err := adapter.readInvocationCredential(ctx, source.GetCredentialDescriptor())
	if err != nil {
		return nil, "", nil, func() {}, errCredential
	}
	defer clear(raw)
	return adapter.authenticatedClient(ctx, source, base, string(raw))
}

// defaultTransport constructs a proxy-only HTTP transport with TLS 1.3, exact server name and bounded handshake/header waits.
// RU: defaultTransport создаёт HTTP транспорт только через proxy с TLS 1.3, точным именем сервера и ограниченным ожиданием handshake и заголовков.
func (adapter *Adapter) defaultTransport(base *url.URL) http.RoundTripper {
	return &http.Transport{
		Proxy:                 http.ProxyURL(adapter.proxy),
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: base.Hostname()},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   adapter.timeout,
		ResponseHeaderTimeout: adapter.timeout,
	}
}

// authenticatedClient installs a scoped bearer client and resolves the configured channel; cleanup clears AuthToken and closes idle transport connections.
// RU: authenticatedClient создаёт ограниченный bearer-клиент и разрешает заданный канал; cleanup очищает AuthToken и закрывает неактивные соединения транспорта.
// Clearing the SDK field does not erase all immutable string copies; callers must not log or persist the returned token.
// RU: Очистка поля SDK не стирает все неизменяемые копии строки; вызывающие стороны не должны журналировать или сохранять возвращённый токен.
func (adapter *Adapter) authenticatedClient(ctx context.Context, source source, base *url.URL, token string) (*model.Client4, string, *model.Channel, func(), error) {
	transport := adapter.newTransport(base)
	client := model.NewAPIv4Client(strings.TrimRight(base.String(), "/"))
	client.HTTPClient = scopedHTTPClient(base, transport, adapter.timeout)
	client.SetToken(token)
	closeClient := func() {
		client.AuthToken = ""
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
	channel, err := resolveChannel(ctx, client, source)
	if err != nil {
		closeClient()
		return nil, "", nil, func() {}, err
	}
	return client, token, channel, closeClient, nil
}

// source exposes only the connection coordinates and credential descriptor needed to open one scoped Mattermost client.
// RU: source открывает только координаты подключения и descriptor реквизитов для создания одного ограниченного клиента Mattermost.
type source interface {
	// GetBaseUrl returns the candidate origin, which is validated against the adapter's exact host allowlist.
	// RU: GetBaseUrl возвращает предполагаемый адрес, проверяемый по точному списку разрешённых узлов адаптера.
	GetBaseUrl() string
	// GetCredentialDescriptor returns immutable projected-credential pins; their content digest is verified before use.
	// RU: GetCredentialDescriptor возвращает неизменяемые координаты спроецированных реквизитов; их дайджест проверяется до использования.
	GetCredentialDescriptor() *controlplanev1.IntegrationCredentialRevision
	// GetTeamName supplies the configured team name for exact API resolution, not an owner permission.
	// RU: GetTeamName задаёт имя команды для точного разрешения через API, а не полномочие владельца.
	GetTeamName() string
	// GetChannelName supplies the configured channel name, whose returned team and identity are rechecked.
	// RU: GetChannelName задаёт имя канала с последующей сверкой возвращённых команды и идентичности.
	GetChannelName() string
}

// baseURL accepts only an allowlisted DNS HTTPS root without a nonempty port, credentials, query, fragment or escaped path.
// RU: baseURL допускает только корневой HTTPS адрес разрешённого DNS-узла без непустого порта, реквизитов, запроса, фрагмента или экранированного пути.
// It canonicalizes host case but neither resolves DNS nor replaces the egress gateway's network checks.
// RU: Функция канонизирует регистр узла, но не разрешает DNS и не заменяет сетевые проверки egress gateway.
func (adapter *Adapter) baseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" || parsed.RawPath != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Port() != "" {
		return nil, errConfiguration
	}
	host := strings.ToLower(parsed.Hostname())
	if _, ok := adapter.allowedHosts[host]; !ok || !validHostname(host) {
		return nil, errConfiguration
	}
	parsed.Host = host
	parsed.Path = ""
	return parsed, nil
}

// postedMessage parses a bounded posted-event payload and rejects invalid, foreign-channel, deleted, empty or bot-authored posts before authoritative readback.
// RU: postedMessage разбирает ограниченный payload posted-события и отклоняет неверные, чужие, удалённые, пустые или бот-сообщения до авторитетного перечитывания.
func postedMessage(event *model.WebSocketEvent, channelID, botUserID string) (*model.Post, bool) {
	if event == nil || event.EventType() != model.WebsocketEventPosted {
		return nil, false
	}
	raw, ok := event.GetData()["post"].(string)
	if !ok || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, false
	}
	var post model.Post
	if json.Unmarshal([]byte(raw), &post) != nil || !validInboundPost(&post, channelID, botUserID) {
		return nil, false
	}
	return &post, true
}

// ParseDecision recognizes only explicit English/Russian approval commands and nonempty change-request prefixes; it grants no authority itself.
// RU: ParseDecision распознаёт только явные английские/русские команды решения и непустые префиксы запроса изменений, сама не выдавая полномочий.
func ParseDecision(message string) controlplanev1.OwnerGateDecision {
	normalized := strings.ToLower(strings.TrimSpace(message))
	switch normalized {
	case "approve", "одобрить":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_APPROVE
	case "reject", "отклонить":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REJECT
	case "cancel", "отменить":
		return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_CANCEL
	}
	for _, prefix := range []string{"changes:", "изменения:"} {
		if strings.HasPrefix(normalized, prefix) && strings.TrimSpace(strings.TrimPrefix(normalized, prefix)) != "" {
			return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_REQUEST_CHANGES
		}
	}
	return controlplanev1.OwnerGateDecision_OWNER_GATE_DECISION_UNSPECIFIED
}

// listens identifies sources requesting inbound messages or gate-decision subscriptions, not agent-callable operations.
// RU: listens определяет источники входящих сообщений или подписок решений gate, а не вызываемые агентом операции.
func listens(capabilities []string) bool {
	for _, value := range capabilities {
		if value == "mattermost.inbound" || value == "mattermost.gate_decisions" {
			return true
		}
	}
	return false
}

// digest encodes SHA-256 of the exact input bytes as lowercase hexadecimal for correlation rather than authentication.
// RU: digest кодирует SHA-256 точных входных байтов строчным hex для корреляции, а не аутентификации.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// clear overwrites the supplied byte slice in place; it cannot erase strings or copies held elsewhere.
// RU: clear перезаписывает переданный байтовый срез на месте и не может стереть строки или копии в других местах.
func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

// classify maps known SDK status codes to stable adapter sentinels and hides other provider details as unavailable.
// RU: classify отображает известные коды SDK в устойчивые ошибки адаптера, скрывая прочие данные провайдера как недоступность.
func classify(err error) error {
	if err == nil {
		return errResponse
	}
	var appError *model.AppError
	if errors.As(err, &appError) {
		switch appError.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return errForbidden
		case http.StatusTooManyRequests:
			return errRateLimited
		case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
			return errConfiguration
		}
	}
	return errUnavailable
}

// Outcome converts delivery errors into a closed public code set without returning raw provider messages or deciding retry safety.
// RU: Outcome преобразует ошибки доставки в закрытый набор публичных кодов без сырых сообщений провайдера и без решения о безопасности повтора.
func Outcome(err error) (bool, string) {
	if err == nil {
		return true, ""
	}
	switch {
	case errors.Is(err, errConfiguration):
		return false, "INTERACTION_CONFIGURATION_INVALID"
	case errors.Is(err, errCredential):
		return false, "INTERACTION_CREDENTIAL_UNAVAILABLE"
	case errors.Is(err, errForbidden):
		return false, "INTERACTION_FORBIDDEN"
	case errors.Is(err, errRateLimited):
		return false, "INTERACTION_RATE_LIMITED"
	case errors.Is(err, errResponse):
		return false, "INTERACTION_RESPONSE_INVALID"
	default:
		return false, "INTERACTION_UNAVAILABLE"
	}
}
