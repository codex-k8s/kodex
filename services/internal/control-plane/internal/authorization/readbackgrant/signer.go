// Package readbackgrant выдаёт узкий credential для readback одной delivery.
package readbackgrant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

const ProtectedType = "mattercodex-interaction-delivery-readback+jws"

type Config struct {
	Issuer, Audience, ProducerID, Purpose, WorkloadID, CallerSPIFFEID string
	Operation, Permission, PrivateJWKFile, PublicKeysetFile           string
	Generation                                                       uint64
	MaximumTTL                                                       time.Duration
}

type Claims struct {
	Version        int    `json:"v"`
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	ProducerID     string `json:"producer_id"`
	Purpose        string `json:"purpose"`
	WorkloadID     string `json:"workload_id"`
	CallerSPIFFEID string `json:"caller_spiffe_id"`
	Operation      string `json:"operation"`
	Permission     string `json:"permission"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	DeliveryID     string `json:"delivery_id"`
	Generation     uint64 `json:"generation"`
	JTI            string `json:"jti"`
	Readiness      bool   `json:"readiness,omitempty"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

type KeyIdentity struct {
	Generation uint64 `json:"generation"`
	Status     string `json:"status"`
	KeyID      string `json:"kid"`
	Thumbprint string `json:"thumbprint_sha256"`
}

type Fence interface {
	AdmitInteractionReadbackKeyset(context.Context, uint64, uint64, uint64, string, []KeyIdentity) error
}

type publicKeyset struct {
	Version          uint64         `json:"version"`
	Revision         uint64         `json:"revision"`
	HighWatermark    uint64         `json:"high_watermark"`
	ServedGeneration uint64         `json:"served_generation"`
	Keys             []publicKeyRef `json:"keys"`
}

type publicKeyRef struct {
	Generation  uint64          `json:"generation"`
	Status      string          `json:"status"`
	AcceptUntil int64           `json:"accept_until,omitempty"`
	JWK         json.RawMessage `json:"jwk"`
}

type State struct {
	Revision, HighWatermark, ServedGeneration uint64
	KeysetSHA256                              string
}

type Signer struct {
	config Config
	key    internalrpcauth.ES256Key
	state  State
	fence  Fence
	identities []KeyIdentity
}

func New(ctx context.Context, config Config, fence Fence) (*Signer, error) {
	if config.Issuer == "" || config.Audience == "" || config.ProducerID != "control-plane.interaction-delivery-readback" ||
		config.Purpose != "INTERACTION_DELIVERY_READBACK_GRANT" || config.WorkloadID != "control-plane" ||
		config.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane" ||
		config.Operation != "interaction.delivery.read" || config.Permission != "interaction.delivery.read" ||
		config.Generation == 0 || config.MaximumTTL < time.Minute || config.MaximumTTL > 15*time.Minute ||
		!filepath.IsAbs(config.PrivateJWKFile) || !filepath.IsAbs(config.PublicKeysetFile) || fence == nil {
		return nil, errors.New("interaction delivery readback signer configuration is invalid")
	}
	privateRaw, err := read(config.PrivateJWKFile)
	if err != nil {
		return nil, errors.New("read interaction delivery readback private JWK")
	}
	keysetRaw, err := read(config.PublicKeysetFile)
	if err != nil {
		return nil, errors.New("read interaction delivery readback public keyset")
	}
	privateKey, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse interaction delivery readback private JWK")
	}
	var document publicKeyset
	if internalrpcauth.DecodeCanonicalJSON(keysetRaw, &document) != nil || document.Version != 1 ||
		document.Revision == 0 || document.HighWatermark != document.ServedGeneration ||
		document.ServedGeneration != config.Generation || len(document.Keys) == 0 || len(document.Keys) > 4 {
		return nil, errors.New("interaction delivery readback public keyset is invalid")
	}
	identities := make([]KeyIdentity, 0, len(document.Keys))
	seenKid, seenThumb := map[string]struct{}{}, map[string]struct{}{}
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || !slices.Contains([]string{"CURRENT", "NEXT", "PREVIOUS", "RETIRED"}, reference.Status) {
			return nil, errors.New("interaction delivery readback key lifecycle is invalid")
		}
		publicKey, parseErr := internalrpcauth.ParsePublicJWK(reference.JWK)
		if parseErr != nil {
			return nil, errors.New("parse interaction delivery readback public key")
		}
		thumb, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(publicKey)
		if thumbErr != nil {
			return nil, thumbErr
		}
		if _, ok := seenKid[publicKey.KeyID]; ok {
			return nil, errors.New("interaction delivery readback keyset has duplicate kid")
		}
		if _, ok := seenThumb[thumb]; ok {
			return nil, errors.New("interaction delivery readback keyset has duplicate public key")
		}
		seenKid[publicKey.KeyID], seenThumb[thumb] = struct{}{}, struct{}{}
		identities = append(identities, KeyIdentity{reference.Generation, reference.Status, publicKey.KeyID, thumb})
		if reference.Status != "CURRENT" {
			continue
		}
		current++
		privateThumb, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(privateKey.PublicOnly())
		if thumbErr != nil || reference.Generation != config.Generation || publicKey.KeyID != privateKey.KeyID || privateThumb != thumb {
			return nil, errors.New("interaction delivery readback signer does not match CURRENT trust")
		}
	}
	if current != 1 {
		return nil, errors.New("interaction delivery readback keyset must contain one CURRENT key")
	}
	digest := sha256.Sum256(keysetRaw)
	state := State{document.Revision, document.HighWatermark, document.ServedGeneration, hex.EncodeToString(digest[:])}
	if err := fence.AdmitInteractionReadbackKeyset(ctx, state.Revision, state.HighWatermark,
		state.ServedGeneration, state.KeysetSHA256, identities); err != nil {
		return nil, err
	}
	return &Signer{config: config, key: privateKey, state: state, fence: fence, identities: slices.Clone(identities)}, nil
}

func (signer *Signer) Sign(ctx context.Context, claims Claims) (string, string, State, error) {
	if err := ctx.Err(); err != nil {
		return "", "", State{}, err
	}
	claims.Version, claims.Issuer, claims.Audience = 1, signer.config.Issuer, signer.config.Audience
	claims.ProducerID, claims.Purpose = signer.config.ProducerID, signer.config.Purpose
	claims.WorkloadID, claims.CallerSPIFFEID = signer.config.WorkloadID, signer.config.CallerSPIFFEID
	claims.Operation, claims.Permission, claims.Generation = signer.config.Operation, signer.config.Permission, signer.config.Generation
	if claims.ExpiresAt <= claims.IssuedAt || time.Duration(claims.ExpiresAt-claims.IssuedAt)*time.Second > signer.config.MaximumTTL {
		return "", "", State{}, errors.New("interaction delivery readback grant TTL is invalid")
	}
	compact, err := internalrpcauth.SignCanonicalJSON(claims, signer.key,
		internalrpcauth.ProtectedHeaderExpectation{Type: ProtectedType, KeyID: signer.key.KeyID})
	if err != nil {
		return "", "", State{}, err
	}
	digest := sha256.Sum256([]byte(compact))
	return compact, hex.EncodeToString(digest[:]), signer.state, nil
}

func (signer *Signer) Check(ctx context.Context) error {
	return signer.fence.AdmitInteractionReadbackKeyset(ctx, signer.state.Revision, signer.state.HighWatermark,
		signer.state.ServedGeneration, signer.state.KeysetSHA256, slices.Clone(signer.identities))
}

func read(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("interaction delivery readback material is unsafe")
	}
	return os.ReadFile(path)
}
