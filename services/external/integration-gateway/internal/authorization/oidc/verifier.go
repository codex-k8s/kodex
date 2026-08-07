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

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
)

type Config struct {
	Issuer        string
	Audience      string
	TLSServerName string
	CAFile        string
	Timeout       time.Duration
}

type Principal struct {
	Scope       domainrepo.Scope
	Permissions []string
}

type Verifier struct {
	verifier *oidc.IDTokenVerifier
}

type exactTransport struct {
	next http.RoundTripper
	host string
}

func (transport exactTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" ||
		!strings.EqualFold(request.URL.Hostname(), transport.host) {
		return nil, errors.New("OIDC endpoint is not permitted")
	}
	return transport.next.RoundTrip(request)
}

type claims struct {
	OrganizationID string   `json:"organization_id"`
	ProjectID      string   `json:"project_id"`
	Permissions    []string `json:"permissions"`
}

func New(ctx context.Context, config Config) (*Verifier, error) {
	if ctx == nil || config.Issuer == "" || config.Audience == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) || config.Timeout < time.Second || config.Timeout > 10*time.Second {
		return nil, errors.New("OIDC verifier configuration is invalid")
	}
	issuerURL, err := url.Parse(config.Issuer)
	if err != nil || issuerURL.Scheme != "https" || issuerURL.Hostname() == "" || issuerURL.User != nil ||
		issuerURL.RawQuery != "" || issuerURL.Fragment != "" || !strings.EqualFold(issuerURL.Hostname(), config.TLSServerName) {
		return nil, errors.New("OIDC issuer is not permitted")
	}
	raw, err := os.ReadFile(config.CAFile)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errors.New("read OIDC CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse OIDC CA")
	}
	baseTransport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName, RootCAs: roots,
	}}
	httpClient := &http.Client{
		Timeout:   config.Timeout,
		Transport: exactTransport{next: baseTransport, host: issuerURL.Hostname()},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OIDC redirects are forbidden")
		},
	}
	providerContext := oidc.ClientContext(ctx, httpClient)
	provider, err := oidc.NewProvider(providerContext, config.Issuer)
	if err != nil {
		return nil, errors.New("initialize OIDC provider")
	}
	return &Verifier{verifier: provider.VerifierContext(providerContext, &oidc.Config{ClientID: config.Audience})}, nil
}

func (verifier *Verifier) Verify(ctx context.Context, authorization string) (Principal, error) {
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 16<<10 {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	token, err := verifier.verifier.Verify(ctx, parts[1])
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Principal{}, err
	}
	if err != nil || uuid.Validate(token.Subject) != nil {
		return Principal{}, errors.New("OIDC bearer is invalid")
	}
	var values claims
	if token.Claims(&values) != nil || uuid.Validate(values.OrganizationID) != nil || uuid.Validate(values.ProjectID) != nil ||
		len(values.Permissions) == 0 || len(values.Permissions) > 256 {
		return Principal{}, errors.New("OIDC claims are invalid")
	}
	return Principal{
		Scope:       domainrepo.Scope{TenantID: values.OrganizationID, ProjectID: values.ProjectID, ActorID: token.Subject},
		Permissions: values.Permissions,
	}, nil
}
