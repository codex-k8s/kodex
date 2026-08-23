// Package integrationgatewayauth задаёт узкий подписанный контракт server-owned
// continuation capability integration-gateway.
package integrationgatewayauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/libs/go/securefile"
)

const (
	ProtectedType     = "mattercodex-integration-continuation-grant+jws"
	PurposeTransition = "CONTINUATION_TRANSITION"
	maximumFileBytes  = 64 << 10
	keyStatusCurrent  = "CURRENT"
	keyStatusPrevious = "PREVIOUS"
	keyStatusNext     = "NEXT"
	keyStatusRetired  = "RETIRED"
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

// PublicKeySet — единственный доставляемый verifier-side canonical JCS snapshot
// ротации. Private JWK намеренно не является частью этого формата.
type PublicKeySet struct {
	Version          int            `json:"version"`
	Revision         uint64         `json:"revision"`
	HighWatermark    uint64         `json:"high_watermark"`
	ServedGeneration uint64         `json:"served_generation"`
	Keys             []PublicKeyRef `json:"keys"`
}

type PublicKeyRef struct {
	Generation  uint64          `json:"generation"`
	Status      string          `json:"status"`
	AcceptUntil int64           `json:"accept_until,omitempty"`
	JWK         json.RawMessage `json:"jwk"`
}

// ServedState связывает readiness и durable verifier high-watermark с exact
// фактически загруженным snapshot без раскрытия key material.
type ServedState struct {
	Revision         uint64
	HighWatermark    uint64
	ServedGeneration uint64
	KeysetSHA256     string
}

type Signer struct {
	config Config
	key    internalrpcauth.ES256Key
}

type Verifier struct {
	config Config
	keys   map[uint64]verificationKey
	state  ServedState
	now    func() time.Time
}

type verificationKey struct {
	key         internalrpcauth.ES256Key
	status      string
	acceptUntil time.Time
}

func NewSigner(config Config, privateJWKFile, publicKeysetFile string) (*Signer, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	privateRaw, err := read(privateJWKFile)
	if err != nil {
		return nil, errors.New("read continuation private JWK")
	}
	keysetRaw, err := read(publicKeysetFile)
	if err != nil {
		return nil, errors.New("read continuation public keyset")
	}
	privateKey, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse continuation private JWK")
	}
	keys, state, err := parsePublicKeySet(config, keysetRaw, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	publicKey, ok := keys[config.Generation]
	if !ok || publicKey.status != keyStatusCurrent || state.ServedGeneration != config.Generation {
		return nil, errors.New("continuation signer generation is not current")
	}
	privateThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(privateKey.PublicOnly())
	if err != nil {
		return nil, err
	}
	publicThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(publicKey.key)
	if err != nil || privateThumbprint != publicThumbprint || privateKey.KeyID != publicKey.key.KeyID {
		return nil, errors.New("continuation signer does not match independently delivered public keyset")
	}
	return &Signer{config: config, key: privateKey}, nil
}

func NewVerifier(config Config, publicKeysetFile string) (*Verifier, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	raw, err := read(publicKeysetFile)
	if err != nil {
		return nil, errors.New("read continuation public keyset")
	}
	keys, state, err := parsePublicKeySet(config, raw, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return &Verifier{config: config, keys: keys, state: state, now: time.Now}, nil
}

func (verifier *Verifier) State() ServedState { return verifier.state }

func (signer *Signer) Sign(ctx context.Context, claims Claims) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	claims.SignerGeneration = signer.config.Generation
	if err := validateClaims(signer.config, claims, time.Now().UTC(), true); err != nil {
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
	now := verifier.now().UTC()
	for generation, candidate := range verifier.keys {
		if candidate.status == keyStatusPrevious && !now.Before(candidate.acceptUntil) {
			continue
		}
		verified, err := internalrpcauth.VerifyCanonicalJSON(compact, candidate.key,
			internalrpcauth.ProtectedHeaderExpectation{Type: ProtectedType, KeyID: candidate.key.KeyID})
		if err != nil {
			continue
		}
		var claims Claims
		if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
			claims.SignerGeneration != generation || validateClaims(verifier.config, claims, now, false) != nil {
			return Claims{}, errors.New("continuation grant claims are invalid")
		}
		return claims, nil
	}
	return Claims{}, errors.New("continuation grant signature is invalid")
}

func validateConfig(config Config) error {
	if config.Issuer == "" || config.Audience == "" || config.WorkloadID == "" ||
		config.CallerSPIFFEID == "" || config.Generation == 0 ||
		config.MaximumTTL < time.Minute || config.MaximumTTL > 8*24*time.Hour {
		return errors.New("continuation grant configuration is invalid")
	}
	return nil
}

func validateClaims(config Config, claims Claims, now time.Time, exactGeneration bool) error {
	issued := time.Unix(claims.IssuedAt, 0).UTC()
	notBefore := time.Unix(claims.NotBefore, 0).UTC()
	expires := time.Unix(claims.ExpiresAt, 0).UTC()
	if claims.Version != 1 || claims.Issuer != config.Issuer || claims.Audience != config.Audience ||
		claims.WorkloadID != config.WorkloadID || claims.CallerSPIFFEID != config.CallerSPIFFEID ||
		claims.Purpose != PurposeTransition ||
		claims.Subject == "" || claims.OrganizationID == "" || claims.ProjectID == "" || claims.SessionID == "" ||
		claims.TurnID == "" || claims.Attempt == 0 || claims.InputSHA256 == "" ||
		claims.RuntimeRevisionID == "" || claims.RuntimeRevisionVersion == 0 || claims.RuntimeRevisionSHA256 == "" ||
		claims.GrantGeneration == 0 || claims.ContinuationID == "" || claims.ContinuationVersion == 0 ||
		claims.ContinuationFence == 0 || claims.InvocationID == "" || claims.SignerGeneration == 0 ||
		claims.JTI == "" || len(claims.AllowedOperationIDs) == 0 || len(claims.AllowedOperationIDs) > 8 ||
		hasDuplicates(claims.AllowedOperationIDs) || notBefore.Before(issued.Add(-5*time.Second)) ||
		notBefore.After(issued.Add(5*time.Second)) || issued.After(now.Add(5*time.Second)) ||
		now.Before(notBefore.Add(-5*time.Second)) || !now.Before(expires.Add(5*time.Second)) ||
		!expires.After(notBefore) || expires.Sub(issued) > config.MaximumTTL {
		return errors.New("continuation grant claims are invalid")
	}
	if exactGeneration && claims.SignerGeneration != config.Generation {
		return errors.New("continuation grant signer generation is invalid")
	}
	return nil
}

func parsePublicKeySet(config Config, raw []byte, now time.Time) (map[uint64]verificationKey, ServedState, error) {
	var document PublicKeySet
	if internalrpcauth.DecodeCanonicalJSON(raw, &document) != nil || document.Version != 1 || document.Revision == 0 || document.HighWatermark == 0 ||
		document.ServedGeneration == 0 || document.HighWatermark != document.ServedGeneration ||
		document.ServedGeneration != config.Generation || len(document.Keys) == 0 || len(document.Keys) > 4 {
		return nil, ServedState{}, errors.New("continuation verifier keyset is invalid")
	}
	keys := make(map[uint64]verificationKey, len(document.Keys))
	seenGenerations := make(map[uint64]struct{}, len(document.Keys))
	keyIDs := make(map[string]struct{}, len(document.Keys))
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || len(reference.JWK) == 0 {
			return nil, ServedState{}, errors.New("continuation verifier keyset is invalid")
		}
		if _, exists := seenGenerations[reference.Generation]; exists {
			return nil, ServedState{}, errors.New("continuation verifier keyset has duplicate generation")
		}
		seenGenerations[reference.Generation] = struct{}{}
		key, err := internalrpcauth.ParsePublicJWK(reference.JWK)
		if err != nil || key.KeyID == "" {
			return nil, ServedState{}, errors.New("parse continuation verifier public JWK")
		}
		if _, exists := keyIDs[key.KeyID]; exists {
			return nil, ServedState{}, errors.New("continuation verifier keyset has duplicate kid")
		}
		keyIDs[key.KeyID] = struct{}{}
		candidate := verificationKey{key: key, status: reference.Status}
		switch reference.Status {
		case keyStatusCurrent:
			if reference.Generation != document.ServedGeneration || reference.AcceptUntil != 0 {
				return nil, ServedState{}, errors.New("continuation verifier CURRENT key is invalid")
			}
			current++
		case keyStatusPrevious:
			candidate.acceptUntil = time.Unix(reference.AcceptUntil, 0).UTC()
			if reference.Generation+1 != document.ServedGeneration || !candidate.acceptUntil.After(now) ||
				candidate.acceptUntil.After(now.Add(config.MaximumTTL)) {
				return nil, ServedState{}, errors.New("continuation verifier PREVIOUS overlap is invalid")
			}
		case keyStatusNext:
			if reference.Generation != document.ServedGeneration+1 || reference.AcceptUntil != 0 {
				return nil, ServedState{}, errors.New("continuation verifier NEXT key is invalid")
			}
		case keyStatusRetired:
			if reference.Generation >= document.ServedGeneration || reference.AcceptUntil != 0 {
				return nil, ServedState{}, errors.New("continuation verifier RETIRED key is invalid")
			}
		default:
			return nil, ServedState{}, errors.New("continuation verifier key status is invalid")
		}
		if reference.Status == keyStatusCurrent || reference.Status == keyStatusPrevious {
			keys[reference.Generation] = candidate
		}
	}
	if current != 1 {
		return nil, ServedState{}, errors.New("continuation verifier keyset must contain one CURRENT key")
	}
	digest := sha256.Sum256(raw)
	return keys, ServedState{
		Revision:      document.Revision,
		HighWatermark: document.HighWatermark, ServedGeneration: document.ServedGeneration,
		KeysetSHA256: hex.EncodeToString(digest[:]),
	}, nil
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
	raw, err := securefile.Read(path, maximumFileBytes)
	if err != nil {
		return nil, errors.New("JWK file is unsafe")
	}
	return raw, nil
}
