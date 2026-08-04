package oidc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

const maximumBearerBytes = 2300

type Config struct {
	Issuer        string
	Audience      string
	TLSServerName string
	CAFile        string
	Timeout       time.Duration
}

type Principal struct {
	Subject         string
	SessionID       string
	SessionRevision uint64
	ExpiresAt       time.Time
}

type Verifier struct {
	verifier  *coreoidc.IDTokenVerifier
	transport *http.Transport
}

type exactTransport struct {
	next *http.Transport
	host string
	port string
}

func (transport exactTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" ||
		!strings.EqualFold(request.URL.Hostname(), transport.host) || request.URL.Port() != transport.port ||
		request.URL.User != nil || request.URL.Fragment != "" {
		return nil, errors.New("OIDC endpoint is not permitted")
	}
	return transport.next.RoundTrip(request)
}

type claims struct {
	SessionID       string `json:"sid"`
	SessionRevision uint64 `json:"session_revision"`
	TokenID         string `json:"jti"`
}

func New(ctx context.Context, config Config) (*Verifier, error) {
	if ctx == nil || config.Issuer == "" || config.Audience == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) || config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("OIDC verifier configuration is invalid")
	}
	issuerURL, err := url.Parse(config.Issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() == "" || issuerURL.User != nil ||
		issuerURL.RawQuery != "" || issuerURL.Fragment != "" ||
		!strings.EqualFold(issuerURL.Hostname(), config.TLSServerName) {
		return nil, errors.New("OIDC issuer is not permitted")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read OIDC CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse OIDC CA")
	}
	base := &http.Transport{
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    roots,
		},
		DisableCompression: true,
	}
	client := &http.Client{
		Timeout:   config.Timeout,
		Transport: exactTransport{next: base, host: issuerURL.Hostname(), port: issuerURL.Port()},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OIDC redirects are forbidden")
		},
	}
	providerContext := coreoidc.ClientContext(ctx, client)
	provider, err := coreoidc.NewProvider(providerContext, config.Issuer)
	if err != nil {
		base.CloseIdleConnections()
		return nil, errors.New("initialize OIDC provider")
	}
	return &Verifier{
		verifier:  provider.VerifierContext(providerContext, &coreoidc.Config{ClientID: config.Audience}),
		transport: base,
	}, nil
}

func (verifier *Verifier) VerifyAuthorization(ctx context.Context, authorization string) (Principal, string, error) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return Principal{}, "", errors.New("OIDC bearer is invalid")
	}
	principal, err := verifier.VerifyToken(ctx, parts[1])
	return principal, parts[1], err
}

func (verifier *Verifier) VerifyToken(ctx context.Context, raw string) (Principal, error) {
	if verifier == nil || raw == "" || len(raw) > maximumBearerBytes || strings.TrimSpace(raw) != raw {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	token, err := verifier.verifier.Verify(ctx, raw)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Principal{}, err
	}
	if err != nil || uuid.Validate(token.Subject) != nil || token.Expiry.IsZero() {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	var values claims
	if token.Claims(&values) != nil || uuid.Validate(values.SessionID) != nil ||
		uuid.Validate(values.TokenID) != nil || values.SessionRevision == 0 {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	return Principal{
		Subject: token.Subject, SessionID: values.SessionID,
		SessionRevision: values.SessionRevision, ExpiresAt: token.Expiry,
	}, nil
}

func (verifier *Verifier) Close() {
	if verifier != nil && verifier.transport != nil {
		verifier.transport.CloseIdleConnections()
	}
}
