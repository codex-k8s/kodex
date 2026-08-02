package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	providerport "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/provider"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

const maximumResponseBytes = 1 << 20

type Config struct {
	ProxyURL              string
	ProxyServerName       string
	CAFile                string
	ClientCertificateFile string
	ClientPrivateKeyFile  string
	MaximumConnections    int
}

type Client struct {
	http     *http.Client
	proxyURL *url.URL
}

func (client *Client) Check(ctx context.Context) error {
	endpoint, err := endpointURL(client.proxyURL.String(), "/readyz")
	if err != nil {
		return errors.New("provider egress readiness endpoint is invalid")
	}
	checkContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(checkContext, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create provider egress readiness request")
	}
	response, err := client.http.Do(request)
	if err != nil {
		return errors.New("provider egress is not ready")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("provider egress readiness was rejected")
	}
	return nil
}

func New(config Config) (*Client, error) {
	proxyURL, err := url.Parse(config.ProxyURL)
	if err != nil || proxyURL.Scheme != "https" || proxyURL.Hostname() == "" || proxyURL.User != nil ||
		config.ProxyServerName == "" || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.ClientCertificateFile) || !filepath.IsAbs(config.ClientPrivateKeyFile) ||
		config.MaximumConnections < 1 || config.MaximumConnections > 256 {
		return nil, errors.New("provider egress configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read provider egress CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse provider egress CA")
	}
	certificate, err := loadClientCertificate(config.ClientCertificateFile, config.ClientPrivateKeyFile)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: config.ProxyServerName,
			RootCAs: roots, Certificates: []tls.Certificate{certificate}},
		MaxConnsPerHost:       config.MaximumConnections,
		MaxIdleConnsPerHost:   config.MaximumConnections,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &Client{http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("provider redirects are forbidden")
	}}, proxyURL: proxyURL}, nil
}

func loadClientCertificate(certificatePath, keyPath string) (tls.Certificate, error) {
	for _, path := range []string{certificatePath, keyPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 || info.Mode().Perm()&0o007 != 0 {
			return tls.Certificate{}, errors.New("provider egress client certificate is unsafe")
		}
	}
	certificatePEM, err := os.ReadFile(certificatePath)
	if err != nil {
		return tls.Certificate{}, errors.New("read provider egress client certificate")
	}
	privateKeyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, errors.New("read provider egress client private key")
	}
	certificate, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return tls.Certificate{}, errors.New("parse provider egress client certificate")
	}
	return certificate, nil
}

func (client *Client) Execute(ctx context.Context, connection entity.Connection, tool entity.Tool, arguments json.RawMessage, credentials map[string]string, idempotencyKey string) (providerport.Result, error) {
	endpoint, err := endpointURL(client.proxyURL.String(), tool.HTTP.Path)
	if err != nil {
		return providerport.Result{Status: enum.InvocationFailed}, err
	}
	request, err := http.NewRequestWithContext(ctx, tool.HTTP.Method, endpoint.String(), bytes.NewReader(arguments))
	if err != nil {
		return providerport.Result{Status: enum.InvocationFailed}, errors.New("create provider request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-MatterCodex-Target", connection.EndpointRef)
	if tool.Idempotency == enum.IdempotencyProviderHeader {
		request.Header.Set(tool.HTTP.IdempotencyHeader, idempotencyKey)
	}
	for header, purpose := range tool.HTTP.CredentialHeaders {
		value, ok := credentials[purpose]
		if !ok || value == "" {
			return providerport.Result{Status: enum.InvocationFailed}, errors.New("provider credential is unavailable")
		}
		request.Header.Set(header, value)
	}
	requestContext, cancel := context.WithTimeout(ctx, tool.HTTP.Timeout)
	defer cancel()
	request = request.WithContext(requestContext)
	response, err := client.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return providerport.Result{Status: enum.InvocationUnknown}, errors.New("provider request deadline exceeded")
		}
		return providerport.Result{Status: enum.InvocationUnknown}, errors.New("provider request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil || len(body) > maximumResponseBytes {
		return providerport.Result{Status: enum.InvocationUnknown}, errors.New("provider response is invalid")
	}
	status := enum.InvocationSucceeded
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status = enum.InvocationFailed
		body = []byte(`{"status":"provider_rejected","code":` + strconv.Itoa(response.StatusCode) + `}`)
	}
	if !json.Valid(body) {
		body = []byte(`{"status":"provider_response_not_json"}`)
		if status == enum.InvocationSucceeded {
			status = enum.InvocationFailed
		}
	}
	receipt := response.Header.Get("X-Request-Id")
	if len(receipt) > 256 || strings.ContainsAny(receipt, "\r\n") {
		receipt = ""
	} else if receipt != "" {
		digest := sha256.Sum256([]byte(receipt))
		receipt = hex.EncodeToString(digest[:])
	}
	return providerport.Result{Status: status, Payload: body, ProviderReceipt: receipt}, nil
}

func (client *Client) Validate(ctx context.Context, connection entity.Connection, credentials map[string]string) enum.ValidationCode {
	payload, err := json.Marshal(struct {
		Credentials map[string]string `json:"credentials"`
	}{Credentials: credentials})
	if err != nil || len(payload) == 0 || len(payload) > 64<<10 {
		return enum.ValidationProtocolError
	}
	endpoint, err := endpointURL(client.proxyURL.String(), "/validate")
	if err != nil {
		return enum.ValidationProtocolError
	}
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return enum.ValidationProtocolError
	}
	request.Header.Set("X-MatterCodex-Target", connection.EndpointRef)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return enum.ValidationTimeout
		}
		return enum.ValidationEndpointUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return enum.ValidationUnauthorized
	case http.StatusForbidden:
		return enum.ValidationForbidden
	default:
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return enum.ValidationOK
		}
		if response.StatusCode >= 400 && response.StatusCode < 500 {
			return enum.ValidationProtocolError
		}
		return enum.ValidationEndpointUnavailable
	}
}

func endpointURL(base, adapterPath string) (*url.URL, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("provider endpoint is invalid")
	}
	parsed.Path = adapterPath
	return parsed, nil
}
