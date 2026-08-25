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
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	"github.com/gorilla/websocket"
	"github.com/mattermost/mattermost/server/public/model"
)

const tokenFile = "token"

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
}

type Message struct {
	EventRef    string
	PostRef     string
	RootPostRef string
	ChannelRef  string
	UserDigest  string
	Text        string
	Decision    controlplanev1.OwnerGateDecision
}

type MessageHandler func(context.Context, Message) (string, error)

func New(config Config, text *texti18n.Localizer) (*Adapter, error) {
	store, err := credentialfs.New(config.CredentialDirectory)
	if err != nil || text == nil || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, errConfiguration
	}
	proxy, err := url.Parse(config.ProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host == "" || proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" {
		return nil, errConfiguration
	}
	hosts := map[string]struct{}{}
	for _, value := range strings.Split(config.AllowedHosts, ",") {
		host := strings.ToLower(strings.TrimSpace(value))
		if host == "" {
			continue
		}
		if net.ParseIP(host) != nil || strings.ContainsAny(host, "*/:@ ") {
			return nil, errConfiguration
		}
		hosts[host] = struct{}{}
	}
	return &Adapter{credentials: store, proxy: proxy, allowedHosts: hosts, timeout: config.Timeout, text: text}, nil
}

func (adapter *Adapter) Deliver(ctx context.Context, claim *controlplanev1.InteractionDeliveryClaim) (string, string, error) {
	if claim == nil || claim.GetMessageKey() == "" {
		return "", "", errConfiguration
	}
	client, _, channel, closeClient, err := adapter.client(ctx, claim)
	if err != nil {
		return "", "", err
	}
	defer closeClient()
	data := map[string]any{}
	if claim.GetTemplateData() != nil {
		data = claim.GetTemplateData().AsMap()
	}
	if state, ok := data["state"].(string); ok {
		data["state"] = adapter.text.Localize(claim.GetLocale(), "RUN_STATE_"+strings.ToUpper(state), nil)
	}
	message := adapter.text.Localize(claim.GetLocale(), claim.GetMessageKey(), data)
	if message == claim.GetMessageKey() || strings.TrimSpace(message) == "" || len(message) > 16<<10 {
		return "", "", errResponse
	}
	post, _, err := client.CreatePost(ctx, &model.Post{ChannelId: channel.Id, Message: message})
	if err != nil {
		return "", "", classify(err)
	}
	if post == nil || post.Id == "" {
		return "", "", errResponse
	}
	return post.Id, post.Id, nil
}

func (adapter *Adapter) Listen(ctx context.Context, source *controlplanev1.InteractionSource, handler MessageHandler) error {
	if source == nil || handler == nil || !listens(source.GetEnabledCapabilities()) {
		return errConfiguration
	}
	client, token, channel, closeClient, err := adapter.client(ctx, source)
	if err != nil {
		return err
	}
	defer closeClient()
	me, _, err := client.GetMe(ctx, "")
	if err != nil || me == nil || me.Id == "" {
		return classify(err)
	}
	base, err := adapter.baseURL(source.GetBaseUrl())
	if err != nil {
		return err
	}
	websocketURL := *base
	websocketURL.Scheme = "wss"
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyURL(adapter.proxy),
		HandshakeTimeout: adapter.timeout,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS13, ServerName: base.Hostname()},
	}
	socket, err := model.NewWebSocketClient4WithDialer(dialer, strings.TrimRight(websocketURL.String(), "/"), token)
	if err != nil {
		return errUnavailable
	}
	defer socket.Close()
	go socket.Listen()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-socket.PingTimeoutChannel:
			return errUnavailable
		case <-socket.ResponseChannel:
			// Канал обязательно дренируется по контракту официального клиента.
		case event, ok := <-socket.EventChannel:
			if !ok {
				return errUnavailable
			}
			post, ok := postedMessage(event, channel.Id, me.Id)
			if !ok {
				continue
			}
			messageKey, handleErr := handler(ctx, Message{
				EventRef: post.Id, PostRef: post.Id, RootPostRef: post.RootId,
				ChannelRef: post.ChannelId, UserDigest: digest(post.UserId),
				Text: strings.TrimSpace(post.Message), Decision: ParseDecision(post.Message),
			})
			if handleErr != nil {
				return handleErr
			}
			if messageKey == "" {
				continue
			}
			response := adapter.text.Localize(source.GetLocale(), messageKey, nil)
			if response == messageKey || strings.TrimSpace(response) == "" {
				return errResponse
			}
			root := post.RootId
			if root == "" {
				root = post.Id
			}
			if _, _, err := client.CreatePost(ctx, &model.Post{ChannelId: channel.Id, RootId: root, Message: response}); err != nil {
				return classify(err)
			}
		}
	}
}

func (adapter *Adapter) client(ctx context.Context, source source) (*model.Client4, string, *model.Channel, func(), error) {
	base, err := adapter.baseURL(source.GetBaseUrl())
	if err != nil {
		return nil, "", nil, func() {}, err
	}
	raw, err := adapter.credentials.Read(source.GetCredentialMaterializationRef(), tokenFile)
	if err != nil {
		return nil, "", nil, func() {}, errCredential
	}
	defer clear(raw)
	token := strings.TrimSpace(string(raw))
	if token == "" || len(token) > 16<<10 || strings.ContainsAny(token, "\r\n") {
		return nil, "", nil, func() {}, errCredential
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(adapter.proxy),
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: base.Hostname()},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   adapter.timeout,
		ResponseHeaderTimeout: adapter.timeout,
	}
	client := model.NewAPIv4Client(strings.TrimRight(base.String(), "/"))
	client.HTTPClient = &http.Client{Transport: transport, Timeout: adapter.timeout}
	client.SetToken(token)
	closeClient := func() {
		client.AuthToken = ""
		transport.CloseIdleConnections()
	}
	team, _, err := client.GetTeamByName(ctx, source.GetTeamName(), "")
	if err != nil || team == nil || team.Id == "" {
		closeClient()
		return nil, "", nil, func() {}, classify(err)
	}
	channel, _, err := client.GetChannelByName(ctx, source.GetChannelName(), team.Id, "")
	if err != nil || channel == nil || channel.Id == "" {
		closeClient()
		return nil, "", nil, func() {}, classify(err)
	}
	return client, token, channel, closeClient, nil
}

type source interface {
	GetBaseUrl() string
	GetCredentialMaterializationRef() string
	GetTeamName() string
	GetChannelName() string
}

func (adapter *Adapter) baseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Port() != "" {
		return nil, errConfiguration
	}
	host := strings.ToLower(parsed.Hostname())
	if _, ok := adapter.allowedHosts[host]; !ok {
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
	if json.Unmarshal([]byte(raw), &post) != nil || post.Id == "" || post.ChannelId != channelID || post.UserId == "" || post.UserId == botUserID || post.DeleteAt != 0 {
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
