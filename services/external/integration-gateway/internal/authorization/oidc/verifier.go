package oidc

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
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
	verifier      *oidc.IDTokenVerifier
	readinessFile string
	digest        string
	generation    uint64
	issuer        string
	audience      string
	strictTime    bool
}

type FileConfig struct {
	Issuer, Audience, File, ExpectedSHA256 string
	ExpectedGeneration                     uint64
}

type providerSnapshot struct {
	SchemaVersion uint64          `json:"schema_version"`
	Generation    uint64          `json:"generation"`
	Issuer        string          `json:"issuer"`
	Audience      string          `json:"audience"`
	Algorithms    []string        `json:"algorithms"`
	JWKS          json.RawMessage `json:"jwks"`
	DigestSHA256  string          `json:"digest_sha256"`
}

type snapshotDigestInput struct {
	SchemaVersion uint64          `json:"schema_version"`
	Generation    uint64          `json:"generation"`
	Issuer        string          `json:"issuer"`
	Audience      string          `json:"audience"`
	Algorithms    []string        `json:"algorithms"`
	JWKS          json.RawMessage `json:"jwks"`
}

type staticKeySet struct{ keys jose.JSONWebKeySet }

func (set staticKeySet) VerifySignature(_ context.Context, raw string) ([]byte, error) {
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return nil, errors.New("OIDC signature envelope is invalid")
	}
	header := signed.Signatures[0].Header
	if header.Algorithm != string(jose.RS256) || header.KeyID == "" {
		return nil, errors.New("OIDC signing key is not registered")
	}
	keys := set.keys.Key(header.KeyID)
	if len(keys) != 1 || keys[0].Algorithm != string(jose.RS256) || keys[0].Use != "sig" {
		return nil, errors.New("OIDC signing key is not registered")
	}
	payload, err := signed.Verify(keys[0].Key)
	if err != nil {
		return nil, errors.New("OIDC signature is invalid")
	}
	return payload, nil
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

// NewFile использует только owner-materialized immutable provider snapshot и
// не выполняет discovery/JWKS network requests.
func NewFile(config FileConfig) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" || !filepath.IsAbs(config.File) ||
		len(config.ExpectedSHA256) != 64 || config.ExpectedGeneration == 0 {
		return nil, errors.New("OIDC file verifier configuration is invalid")
	}
	snapshot, keys, err := loadProviderSnapshot(config)
	if err != nil {
		return nil, err
	}
	return &Verifier{
		verifier: oidc.NewVerifier(snapshot.Issuer, staticKeySet{keys: keys}, &oidc.Config{
			ClientID: config.Audience, SupportedSigningAlgs: []string{string(jose.RS256)},
		}),
		readinessFile: config.File, digest: snapshot.DigestSHA256, generation: snapshot.Generation,
		issuer: snapshot.Issuer, audience: snapshot.Audience, strictTime: true,
	}, nil
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
	now := time.Now().UTC()
	if verifier.strictTime && (token.IssuedAt.IsZero() || token.IssuedAt.After(now.Add(time.Minute)) || !token.Expiry.After(now) ||
		token.Expiry.Sub(token.IssuedAt) <= 0 || token.Expiry.Sub(token.IssuedAt) > 24*time.Hour) {
		return Principal{}, errors.New("OIDC bearer time claims are invalid")
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

func (verifier *Verifier) Check(context.Context) error {
	if verifier == nil || verifier.verifier == nil {
		return errors.New("OIDC verifier is unavailable")
	}
	if verifier.readinessFile == "" {
		return nil
	}
	_, _, err := loadProviderSnapshot(FileConfig{
		Issuer: verifier.issuer, Audience: verifier.audience, File: verifier.readinessFile,
		ExpectedSHA256: verifier.digest, ExpectedGeneration: verifier.generation,
	})
	return err
}

func loadProviderSnapshot(config FileConfig) (providerSnapshot, jose.JSONWebKeySet, error) {
	info, err := os.Stat(config.File)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider snapshot file is unsafe")
	}
	raw, err := os.ReadFile(config.File)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("read OIDC provider snapshot")
	}
	var snapshot providerSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		snapshot.SchemaVersion != 1 || snapshot.Generation != config.ExpectedGeneration ||
		(config.Issuer != "" && snapshot.Issuer != config.Issuer) ||
		(config.Audience != "" && snapshot.Audience != config.Audience) ||
		len(snapshot.Algorithms) != 1 || snapshot.Algorithms[0] != string(jose.RS256) || len(snapshot.DigestSHA256) != 64 {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider snapshot binding is invalid")
	}
	var keys jose.JSONWebKeySet
	if json.Unmarshal(snapshot.JWKS, &keys) != nil || len(keys.Keys) == 0 || len(keys.Keys) > 16 {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider JWKS is invalid")
	}
	seen := make(map[string]struct{}, len(keys.Keys))
	for _, key := range keys.Keys {
		_, rsaKey := key.Key.(*rsa.PublicKey)
		if !rsaKey || !key.Valid() || !key.IsPublic() || key.KeyID == "" || key.Algorithm != string(jose.RS256) ||
			key.Use != "sig" || key.CertificatesURL != nil {
			return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider JWK is not permitted")
		}
		if _, duplicate := seen[key.KeyID]; duplicate {
			return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider JWK kid is duplicated")
		}
		seen[key.KeyID] = struct{}{}
	}
	canonicalJWKS, err := json.Marshal(keys)
	if err != nil {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("canonicalize OIDC provider JWKS")
	}
	digestRaw, err := json.Marshal(snapshotDigestInput{
		SchemaVersion: snapshot.SchemaVersion, Generation: snapshot.Generation, Issuer: snapshot.Issuer,
		Audience: snapshot.Audience, Algorithms: snapshot.Algorithms, JWKS: canonicalJWKS,
	})
	if err != nil {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("digest OIDC provider snapshot")
	}
	digest := sha256.Sum256(digestRaw)
	actualDigest := hex.EncodeToString(digest[:])
	if actualDigest != snapshot.DigestSHA256 || actualDigest != config.ExpectedSHA256 {
		return providerSnapshot{}, jose.JSONWebKeySet{}, errors.New("OIDC provider snapshot digest or generation rollback rejected")
	}
	return snapshot, keys, nil
}
