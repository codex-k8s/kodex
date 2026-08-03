// Package integrationgatewayauth задаёт узкий подписанный контракт server-owned
// continuation и result-access capabilities integration-gateway.
package integrationgatewayauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

const (
	ProtectedType       = "mattercodex-integration-continuation-grant+jws"
	PurposeTransition   = "CONTINUATION_TRANSITION"
	PurposeResultAccess = "RESULT_ACCESS"
	maximumFileBytes    = 64 << 10
)

type Claims struct {
	Version                int      `json:"v"`
	Issuer                 string   `json:"iss"`
	Audience               string   `json:"aud"`
	Purpose                string   `json:"purpose"`
	Subject                string   `json:"sub"`
	OrganizationID         string   `json:"organization_id"`
	ProjectID              string   `json:"project_id"`
	WorkloadID             string   `json:"workload_id"`
	CallerSPIFFEID         string   `json:"caller_spiffe_id"`
	SessionID              string   `json:"session_id"`
	TurnID                 string   `json:"turn_id"`
	Attempt                uint32   `json:"attempt"`
	InputSHA256            string   `json:"input_sha256"`
	RuntimeRevisionID      string   `json:"runtime_revision_id"`
	RuntimeRevisionVersion uint64   `json:"runtime_revision_version"`
	RuntimeRevisionSHA256  string   `json:"runtime_revision_sha256"`
	GrantGeneration        uint64   `json:"grant_generation"`
	ContinuationID         string   `json:"continuation_id"`
	ContinuationVersion    uint64   `json:"continuation_version"`
	ContinuationFence      uint64   `json:"continuation_fence"`
	InvocationID           string   `json:"invocation_id"`
	ResultAttemptID        string   `json:"result_attempt_id,omitempty"`
	ResultSHA256           string   `json:"result_sha256,omitempty"`
	AllowedOperationIDs    []string `json:"allowed_operation_ids"`
	SignerGeneration       uint64   `json:"signer_generation"`
	JTI                    string   `json:"jti"`
	IssuedAt               int64    `json:"iat"`
	NotBefore              int64    `json:"nbf"`
	ExpiresAt              int64    `json:"exp"`
}

type Config struct {
	Issuer         string
	Audience       string
	WorkloadID     string
	CallerSPIFFEID string
	Generation     uint64
	MaximumTTL     time.Duration
}

type Signer struct {
	config Config
	key    internalrpcauth.ES256Key
}

type Verifier struct {
	config Config
	key    internalrpcauth.ES256Key
	now    func() time.Time
}

func NewSigner(config Config, privateJWKFile, publicJWKFile string) (*Signer, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	privateRaw, err := read(privateJWKFile)
	if err != nil {
		return nil, errors.New("read continuation private JWK")
	}
	publicRaw, err := read(publicJWKFile)
	if err != nil {
		return nil, errors.New("read continuation public JWK")
	}
	privateKey, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse continuation private JWK")
	}
	publicKey, err := internalrpcauth.ParsePublicJWK(publicRaw)
	if err != nil {
		return nil, errors.New("parse continuation public JWK")
	}
	privateThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(privateKey.PublicOnly())
	if err != nil {
		return nil, err
	}
	publicThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(publicKey)
	if err != nil || privateThumbprint != publicThumbprint || privateKey.KeyID != publicKey.KeyID {
		return nil, errors.New("continuation signer does not match independently delivered public JWK")
	}
	return &Signer{config: config, key: privateKey}, nil
}

func NewVerifier(config Config, publicJWKFile string) (*Verifier, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	raw, err := read(publicJWKFile)
	if err != nil {
		return nil, errors.New("read continuation public JWK")
	}
	key, err := internalrpcauth.ParsePublicJWK(raw)
	if err != nil {
		return nil, errors.New("parse continuation public JWK")
	}
	return &Verifier{config: config, key: key, now: time.Now}, nil
}

func (signer *Signer) Sign(ctx context.Context, claims Claims) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	claims.SignerGeneration = signer.config.Generation
	if err := validateClaims(signer.config, claims, time.Now().UTC()); err != nil {
		return "", err
	}
	return internalrpcauth.SignCanonicalJSON(claims, signer.key,
		internalrpcauth.ProtectedHeaderExpectation{Type: ProtectedType, KeyID: signer.key.KeyID})
}

func (verifier *Verifier) Verify(ctx context.Context, compact string) (Claims, error) {
	if err := ctx.Err(); err != nil {
		return Claims{}, err
	}
	if len(compact) == 0 || len(compact) > 16<<10 || strings.TrimSpace(compact) != compact {
		return Claims{}, errors.New("continuation grant is invalid")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, verifier.key,
		internalrpcauth.ProtectedHeaderExpectation{Type: ProtectedType, KeyID: verifier.key.KeyID})
	if err != nil {
		return Claims{}, errors.New("continuation grant signature is invalid")
	}
	var claims Claims
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
		validateClaims(verifier.config, claims, verifier.now().UTC()) != nil {
		return Claims{}, errors.New("continuation grant claims are invalid")
	}
	return claims, nil
}

func validateConfig(config Config) error {
	if config.Issuer == "" || config.Audience == "" || config.WorkloadID == "" ||
		config.CallerSPIFFEID == "" || config.Generation == 0 ||
		config.MaximumTTL < time.Minute || config.MaximumTTL > 8*24*time.Hour {
		return errors.New("continuation grant configuration is invalid")
	}
	return nil
}

func validateClaims(config Config, claims Claims, now time.Time) error {
	issued := time.Unix(claims.IssuedAt, 0).UTC()
	notBefore := time.Unix(claims.NotBefore, 0).UTC()
	expires := time.Unix(claims.ExpiresAt, 0).UTC()
	if claims.Version != 1 || claims.Issuer != config.Issuer || claims.Audience != config.Audience ||
		claims.WorkloadID != config.WorkloadID || claims.CallerSPIFFEID != config.CallerSPIFFEID ||
		(claims.Purpose != PurposeTransition && claims.Purpose != PurposeResultAccess) ||
		claims.Subject == "" || claims.OrganizationID == "" || claims.ProjectID == "" || claims.SessionID == "" ||
		claims.TurnID == "" || claims.Attempt == 0 || claims.InputSHA256 == "" ||
		claims.RuntimeRevisionID == "" || claims.RuntimeRevisionVersion == 0 || claims.RuntimeRevisionSHA256 == "" ||
		claims.GrantGeneration == 0 || claims.ContinuationID == "" || claims.ContinuationVersion == 0 ||
		claims.ContinuationFence == 0 || claims.InvocationID == "" || claims.SignerGeneration != config.Generation ||
		claims.JTI == "" || len(claims.AllowedOperationIDs) == 0 || len(claims.AllowedOperationIDs) > 8 ||
		hasDuplicates(claims.AllowedOperationIDs) || notBefore.Before(issued.Add(-5*time.Second)) ||
		notBefore.After(issued.Add(5*time.Second)) || issued.After(now.Add(5*time.Second)) ||
		now.Before(notBefore.Add(-5*time.Second)) || !now.Before(expires.Add(5*time.Second)) ||
		!expires.After(notBefore) || expires.Sub(issued) > config.MaximumTTL {
		return errors.New("continuation grant claims are invalid")
	}
	if claims.Purpose == PurposeResultAccess && (claims.ResultAttemptID == "" || claims.ResultSHA256 == "") {
		return errors.New("result access grant binding is invalid")
	}
	return nil
}

func hasDuplicates(values []string) bool {
	seen := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || slices.Contains(seen, value) {
			return true
		}
		seen = append(seen, value)
	}
	return false
}

func read(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("JWK path is not absolute")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumFileBytes || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("JWK file is unsafe")
	}
	return os.ReadFile(path)
}
