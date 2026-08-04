package botservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	domainbot "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/botservice"
	"github.com/google/uuid"
)

type Config struct {
	URL, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	Timeout                                                                 time.Duration
}

type Client struct {
	endpoint *url.URL
	http     *http.Client
}

func New(config Config) (*Client, error) {
	endpoint, err := url.Parse(config.URL)
	caRaw, caErr := os.ReadFile(config.CAFile)
	roots := x509.NewCertPool()
	certificate, certificateErr := tls.LoadX509KeyPair(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Hostname() != config.TLSServerName ||
		caErr != nil || !roots.AppendCertsFromPEM(caRaw) || certificateErr != nil ||
		config.Timeout < time.Second || config.Timeout > time.Minute {
		return nil, errors.New("bot-service runtime client configuration is invalid")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13,
		ServerName: config.TLSServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}},
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		ResponseHeaderTimeout: config.Timeout, TLSHandshakeTimeout: 5 * time.Second}
	return &Client{endpoint: endpoint, http: &http.Client{Transport: transport, Timeout: config.Timeout}}, nil
}

func (client *Client) Check(ctx context.Context) error {
	endpoint := *client.endpoint
	endpoint.Path = "/readyz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create bot-service readiness request")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return errors.New("bot-service runtime transport is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("bot-service runtime transport is not ready")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	return nil
}

func (client *Client) EnsureRuntimeMCPBinding(ctx context.Context,
	input domainbot.BindingRequest) (domainbot.Binding, error) {
	body, err := json.Marshal(map[string]string{"control_session_id": input.ControlSessionID,
		"channel_id": input.ChannelID, "root_post_id": input.RootPostID, "bot_stable_key": input.BotStableKey})
	if err != nil {
		return domainbot.Binding{}, errors.New("encode runtime MCP binding request")
	}
	endpoint := *client.endpoint
	endpoint.Path = "/internal/runtime-mcp-bindings"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return domainbot.Binding{}, errors.New("create runtime MCP binding request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return domainbot.Binding{}, errors.New("bot-service runtime MCP binding is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > 8<<10 {
		return domainbot.Binding{}, errors.New("bot-service runtime MCP binding was rejected")
	}
	var wire struct {
		AgentSessionKey           string `json:"agent_session_key"`
		AgentSessionID            int64  `json:"agent_session_id"`
		AgentSessionVersion       uint64 `json:"agent_session_version"`
		AgentSessionBindingSHA256 string `json:"agent_session_binding_sha256"`
		ImmutableSecretRef        string `json:"immutable_secret_ref"`
		ProviderContentVersion    string `json:"provider_content_version"`
		ContentSHA256             string `json:"content_sha256"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, (8<<10)+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		wire.AgentSessionKey != "owner:"+input.ControlSessionID || wire.AgentSessionID <= 0 ||
		wire.AgentSessionVersion == 0 || len(wire.AgentSessionBindingSHA256) != 64 ||
		!strings.HasPrefix(wire.ImmutableSecretRef, "k8s-immutable-secret://") ||
		wire.ProviderContentVersion == "" || len(wire.ContentSHA256) != 64 || uuid.Validate(input.ControlSessionID) != nil {
		return domainbot.Binding{}, errors.New("bot-service runtime MCP binding readback is invalid")
	}
	return domainbot.Binding{AgentSessionKey: wire.AgentSessionKey, AgentSessionID: wire.AgentSessionID,
		AgentSessionVersion: wire.AgentSessionVersion, AgentSessionBindingSHA256: wire.AgentSessionBindingSHA256,
		ImmutableSecretRef: wire.ImmutableSecretRef, ProviderContentVersion: wire.ProviderContentVersion,
		ContentSHA256: wire.ContentSHA256}, nil
}
