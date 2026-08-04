// Package oidc реализует прикладную границу точных mTLS- и OIDC-проверок
// сервиса выдачи доказательств полномочий.
package oidc

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	coreosoidc "github.com/coreos/go-oidc/v3/oidc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

const (
	maximumBearerBytes = 16 << 10
	maximumTokenTTL    = 15 * time.Minute
)

// Config фиксирует издателя, аудиторию, TLS и единственного транспортного
// вызывающего.
type Config struct {
	ProducerID           string
	Purpose              string
	Issuer               string
	Audience             string
	TLSServerName        string
	CAFile               string
	ExpectedCallerSPIFFE string
	ExpectedWorkload     string
	ClockSkew            time.Duration
	HTTPTimeout          time.Duration
}

// Verifier владеет кэшем поставщика и JWKS с точным транспортом TLS.
type Verifier struct {
	config    Config
	verifier  *coreosoidc.IDTokenVerifier
	transport *http.Transport
	now       func() time.Time
}

type claims struct {
	JTI             string `json:"jti"`
	SessionID       string `json:"sid"`
	OrganizationID  string `json:"organization_id"`
	ProjectID       string `json:"project_id"`
	SessionRevision uint64 `json:"session_revision"`
	TenantOwner     bool   `json:"mattercodex_owner"`
	NotBefore       int64  `json:"nbf"`
}

// New выполняет обнаружение OIDC через закреплённые CA и SNI до открытия
// прослушивателя.
func New(ctx context.Context, config Config) (*Verifier, error) {
	issuer, err := url.Parse(config.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" ||
		issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		config.ProducerID == "" || config.Purpose == "" || config.Audience == "" || config.TLSServerName == "" ||
		net.ParseIP(config.TLSServerName) != nil ||
		config.ExpectedCallerSPIFFE == "" ||
		config.ExpectedWorkload == "" ||
		config.ClockSkew < 0 || config.ClockSkew > 30*time.Second ||
		config.HTTPTimeout < time.Second || config.HTTPTimeout > 10*time.Second {
		return nil, errors.New("OIDC verifier configuration is invalid")
	}
	if !filepath.IsAbs(config.CAFile) {
		return nil, errors.New("OIDC trust bundle path is invalid")
	}
	info, err := os.Stat(config.CAFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > 1<<20 || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("OIDC trust bundle is unavailable")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read OIDC trust bundle")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("OIDC trust bundle is invalid")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   config.HTTPTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    roots,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       time.Minute,
		TLSHandshakeTimeout:   config.HTTPTimeout,
		ResponseHeaderTimeout: config.HTTPTimeout,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   config.HTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("OIDC redirect is forbidden")
		},
	}
	provider, err := coreosoidc.NewProvider(
		coreosoidc.ClientContext(ctx, client),
		config.Issuer,
	)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return &Verifier{
		config: config,
		verifier: provider.VerifierContext(
			coreosoidc.ClientContext(ctx, client),
			&coreosoidc.Config{
				ClientID:             config.Audience,
				SupportedSigningAlgs: []string{coreosoidc.RS256},
			},
		),
		transport: transport,
		now:       time.Now,
	}, nil
}

// Close завершает простаивающий транспорт обнаружения OIDC и JWKS.
func (verifier *Verifier) Close() {
	if verifier != nil && verifier.transport != nil {
		verifier.transport.CloseIdleConnections()
	}
}

// VerifyPeer проверяет единственную точную SPIFFE-идентичность вызывающего.
func (verifier *Verifier) VerifyPeer(ctx context.Context) error {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return errs.ErrUnauthenticated
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.PeerCertificates) != 1 {
		return errs.ErrUnauthenticated
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	if len(certificate.URIs) != 1 ||
		certificate.URIs[0].String() != verifier.config.ExpectedCallerSPIFFE {
		return errs.ErrPermissionDenied
	}
	return nil
}

// Authenticate получает Bearer только из метаданных и возвращает проверенную
// идентичность.
func (verifier *Verifier) Authenticate(
	ctx context.Context,
) (authoritytype.ApplicationIdentity, error) {
	if err := verifier.VerifyPeer(ctx); err != nil {
		return authoritytype.ApplicationIdentity{}, err
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	values := incoming.Get("authorization")
	if len(values) != 1 ||
		!strings.HasPrefix(values[0], "Bearer ") ||
		len(values[0]) <= len("Bearer ") ||
		len(values[0]) > maximumBearerBytes {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	if strings.TrimSpace(raw) != raw {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	token, err := verifier.verifier.Verify(ctx, raw)
	if err != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	now := verifier.now().UTC()
	if token.Issuer != verifier.config.Issuer ||
		len(token.Audience) != 1 ||
		token.Audience[0] != verifier.config.Audience ||
		token.IssuedAt.IsZero() ||
		token.IssuedAt.After(now.Add(verifier.config.ClockSkew)) ||
		!token.Expiry.After(now.Add(-verifier.config.ClockSkew)) ||
		token.Expiry.Sub(token.IssuedAt) <= 0 ||
		token.Expiry.Sub(token.IssuedAt) > maximumTokenTTL {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	var custom claims
	if err := token.Claims(&custom); err != nil ||
		custom.NotBefore <= 0 ||
		time.Unix(custom.NotBefore, 0).After(now.Add(verifier.config.ClockSkew)) ||
		time.Unix(custom.NotBefore, 0).Before(
			token.IssuedAt.Add(-verifier.config.ClockSkew),
		) ||
		token.Expiry.Before(time.Unix(custom.NotBefore, 0)) ||
		value.ValidateID(token.Subject) != nil ||
		value.ValidateID(custom.JTI) != nil ||
		value.ValidateID(custom.SessionID) != nil ||
		value.ValidateID(custom.OrganizationID) != nil ||
		(custom.ProjectID != "" && value.ValidateID(custom.ProjectID) != nil) ||
		custom.SessionRevision == 0 {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ProducerID:           verifier.config.ProducerID,
		CredentialPurpose:    verifier.config.Purpose,
		CredentialGeneration: custom.SessionRevision,
		ActorID:              token.Subject,
		OrganizationID:       custom.OrganizationID,
		ProjectID:            custom.ProjectID,
		SessionJTI:           custom.JTI,
		SessionID:            custom.SessionID,
		SessionRevision:      custom.SessionRevision,
		SubjectDigest:        digest("OIDC_SUBJECT:" + token.Subject),
		CredentialDigest:     digest(raw),
		TenantOwner:          custom.TenantOwner,
		CallerWorkload:       verifier.config.ExpectedWorkload,
		CallerSPIFFEID:       verifier.config.ExpectedCallerSPIFFE,
	}, nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
