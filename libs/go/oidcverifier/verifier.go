package oidcverifier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/oidcidentity"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

const (
	maximumBearerBytes = 2300
	ownerScope         = "mattercodex.owner"
	ownerRealmRole     = "mattercodex-owner"
)

type Config struct {
	Issuer         string
	Audience       string
	JWKSURL        string
	ConnectAddress string
	TLSServerName  string
	CAFile         string
	Timeout        time.Duration
}

type Principal struct {
	Subject         string
	OrganizationID  string
	SessionID       string
	SessionRevision uint64
	ExpiresAt       time.Time
}

type Verifier struct {
	verifier  *coreoidc.IDTokenVerifier
	keys      *boundedKeySet
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
	OrganizationID  string `json:"organization_id"`
	SessionRevision uint64 `json:"session_revision"`
	TokenID         string `json:"jti"`
	Scope           string `json:"scope"`
	RealmAccess     struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

func New(ctx context.Context, config Config) (*Verifier, error) {
	if ctx == nil || config.Issuer == "" || config.Audience == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) || config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("OIDC verifier configuration is invalid")
	}
	issuerURL, err := url.Parse(config.Issuer)
	jwksURL, jwksErr := url.Parse(config.JWKSURL)
	connectHost, connectPort, connectErr := net.SplitHostPort(config.ConnectAddress)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() == "" || issuerURL.User != nil ||
		issuerURL.RawQuery != "" || issuerURL.Fragment != "" ||
		jwksErr != nil || jwksURL.Scheme != "https" || jwksURL.Hostname() != issuerURL.Hostname() ||
		jwksURL.User != nil || jwksURL.RawQuery != "" || jwksURL.Fragment != "" || jwksURL.Path == "" ||
		!strings.EqualFold(issuerURL.Hostname(), config.TLSServerName) || connectErr != nil ||
		connectHost == "" || net.ParseIP(connectHost) != nil || connectPort != "443" {
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
	dialer := &net.Dialer{Timeout: config.Timeout, KeepAlive: 30 * time.Second}
	issuerAddress := net.JoinHostPort(issuerURL.Hostname(), "443")
	base := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if !strings.HasPrefix(network, "tcp") || !strings.EqualFold(address, issuerAddress) {
				return nil, errors.New("OIDC endpoint is not permitted")
			}
			return dialer.DialContext(ctx, network, config.ConnectAddress)
		},
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
	keys := newBoundedKeySet(client, config.JWKSURL)
	if err := keys.Refresh(ctx); err != nil {
		base.CloseIdleConnections()
		return nil, errors.New("initialize OIDC signing keys")
	}
	return &Verifier{
		verifier:  coreoidc.NewVerifier(config.Issuer, keys, &coreoidc.Config{ClientID: config.Audience, SupportedSigningAlgs: []string{"RS256"}}),
		keys:      keys,
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
	if err != nil || token.Expiry.IsZero() {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	if deadline, degraded := verifier.keys.DegradedDeadline(); degraded && token.Expiry.After(deadline) {
		return Principal{}, errors.New("OIDC bearer exceeds the signing-key grace window")
	}
	var values claims
	if token.Claims(&values) != nil || uuid.Validate(values.OrganizationID) != nil ||
		values.SessionRevision == 0 || !containsWord(values.Scope, ownerScope) ||
		!contains(values.RealmAccess.Roles, ownerRealmRole) {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	subject, subjectErr := oidcidentity.Subject(token.Issuer, token.Subject)
	sessionID, sessionErr := oidcidentity.SessionID(token.Issuer, values.SessionID)
	if subjectErr != nil || sessionErr != nil {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	if _, tokenIDErr := oidcidentity.TokenID(token.Issuer, values.TokenID); tokenIDErr != nil {
		return Principal{}, errors.New("OIDC session claims are invalid")
	}
	return Principal{
		Subject: subject, OrganizationID: values.OrganizationID, SessionID: sessionID,
		SessionRevision: values.SessionRevision, ExpiresAt: token.Expiry,
	}, nil
}

// Refresh обновляет JWKS независимо от request/readiness path. При кратком
// сетевом отказе verifier использует bounded last-known-good, а повреждение или
// конфликт key material закрывают авторизацию немедленно.
func (verifier *Verifier) Refresh(ctx context.Context) error {
	if verifier == nil || verifier.keys == nil {
		return errors.New("OIDC verifier is unavailable")
	}
	return verifier.keys.Refresh(ctx)
}

func containsWord(words, expected string) bool {
	return contains(strings.Fields(words), expected)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func (verifier *Verifier) Close() {
	if verifier != nil && verifier.transport != nil {
		verifier.transport.CloseIdleConnections()
	}
}
