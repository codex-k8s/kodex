// Package mattermost реализует необязательный interaction adapter без
// переноса полномочий Mattermost identifiers в core-домен.
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
	errConfiguration = errors.New("mattermost interaction configuration is invalid")
	errCredential    = errors.New("mattermost interaction credential is unavailable")
	errForbidden     = errors.New("mattermost interaction operation is forbidden")
	errRateLimited   = errors.New("mattermost interaction rate limit exceeded")
	errUnavailable   = errors.New("mattermost interaction endpoint is unavailable")
	errResponse      = errors.New("mattermost interaction response is invalid")
)

type Config struct {
	CredentialDirectory string
	ProxyURL            string
	AllowedHosts        string
	Timeout             time.Duration
}

type Adapter struct {
	credentials  *credentialfs.Store
	proxy        *url.URL
	allowedHosts map[string]struct{}
	timeout      time.Duration
	text         *texti18n.Localizer
	definition   integrationpackage.Package
	newTransport func(*url.URL) http.RoundTripper
}

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

type MessageHandler func(context.Context, Message) error

type DeliveryResult struct {
	PostRef    string
	ThreadRef  string
	TeamRef    string
	ChannelRef string
}

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

type source interface {
	GetBaseUrl() string
	GetCredentialDescriptor() *controlplanev1.IntegrationCredentialRevision
	GetTeamName() string
	GetChannelName() string
}

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

func listens(capabilities []string) bool {
	for _, value := range capabilities {
		if value == "mattermost.inbound" || value == "mattermost.gate_decisions" {
			return true
		}
	}
	return false
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

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
