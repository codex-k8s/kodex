// Package readbackgrant проверяет выдаваемый control-plane application
// credential exact delivery readback. mTLS остаётся независимым слоем.
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
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

const credentialType = "mattercodex-interaction-delivery-readback+jws"

type Config struct {
	Issuer, Audience, ProducerID, Purpose, Operation, Permission string
	PublicKeysetFile                                             string
	Generation                                                   uint64
	MaximumTTL                                                   time.Duration
}

type Claims struct {
	Version          int    `json:"v"`
	Issuer           string `json:"iss"`
	Audience         string `json:"aud"`
	Subject          string `json:"sub"`
	ProducerID       string `json:"producer_id"`
	Purpose          string `json:"purpose"`
	WorkloadID       string `json:"workload_id"`
	CallerSPIFFEID   string `json:"caller_spiffe_id"`
	Operation        string `json:"operation"`
	Permission       string `json:"permission"`
	OrganizationID   string `json:"organization_id"`
	ProjectID        string `json:"project_id"`
	DeliveryID       string `json:"delivery_id"`
	Generation       uint64 `json:"generation"`
	JTI              string `json:"jti"`
	Readiness        bool   `json:"readiness,omitempty"`
	IssuedAt         int64  `json:"iat"`
	NotBefore        int64  `json:"nbf"`
	ExpiresAt        int64  `json:"exp"`
	CredentialSHA256 string `json:"-"`
}

type KeyIdentity struct {
	Generation uint64 `json:"generation"`
	Status     string `json:"status"`
	KeyID      string `json:"kid"`
	Thumbprint string `json:"thumbprint_sha256"`
}

type KeysetFence interface {
	AdmitDeliveryReadbackKeyset(context.Context, uint64, uint64, uint64, string, []KeyIdentity) error
}

type publicKeyset struct {
	Version, Revision, HighWatermark, ServedGeneration uint64         `json:"-"`
	Keys                                               []publicKeyRef `json:"keys"`
}

func (value *publicKeyset) UnmarshalJSON(raw []byte) error {
	type wire struct {
		Version          uint64         `json:"version"`
		Revision         uint64         `json:"revision"`
		HighWatermark    uint64         `json:"high_watermark"`
		ServedGeneration uint64         `json:"served_generation"`
		Keys             []publicKeyRef `json:"keys"`
	}
	var decoded wire
	if internalrpcauth.DecodeCanonicalJSON(raw, &decoded) != nil {
		return errors.New("decode delivery readback keyset")
	}
	value.Version, value.Revision = decoded.Version, decoded.Revision
	value.HighWatermark, value.ServedGeneration, value.Keys = decoded.HighWatermark, decoded.ServedGeneration, decoded.Keys
	return nil
}

type publicKeyRef struct {
	Generation  uint64          `json:"generation"`
	Status      string          `json:"status"`
	AcceptUntil int64           `json:"accept_until,omitempty"`
	JWK         json.RawMessage `json:"jwk"`
}

type verificationKey struct {
	key         internalrpcauth.ES256Key
	status      string
	acceptUntil time.Time
}

type servedState struct {
	revision, highWatermark, generation uint64
	digest                              string
}

type Verifier struct {
	config Config
	fence  KeysetFence
	mu     sync.RWMutex
	keys   map[uint64]verificationKey
	state  servedState
	now    func() time.Time
}

func New(ctx context.Context, config Config, fence KeysetFence) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" || config.ProducerID != "control-plane.interaction-delivery-readback" ||
		config.Purpose != "INTERACTION_DELIVERY_READBACK_GRANT" || config.Operation != "interaction.delivery.read" ||
		config.Permission != "interaction.delivery.read" || config.Generation == 0 ||
		config.MaximumTTL < time.Minute || config.MaximumTTL > 15*time.Minute ||
		!filepath.IsAbs(config.PublicKeysetFile) || fence == nil {
		return nil, errors.New("interaction delivery readback verifier configuration is invalid")
	}
	result := &Verifier{config: config, fence: fence, now: time.Now}
	if err := result.refresh(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func (verifier *Verifier) Verify(ctx context.Context, authorization string) (Claims, error) {
	if err := verifier.refresh(ctx); err != nil {
		return Claims{}, errors.New("interaction delivery readback trust is unavailable")
	}
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) > 16<<10 {
		return Claims{}, errors.New("interaction delivery readback credential is missing")
	}
	compact := strings.TrimPrefix(authorization, "Bearer ")
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	now := verifier.now().UTC()
	for generation, candidate := range verifier.keys {
		if candidate.status == "PREVIOUS" && !now.Before(candidate.acceptUntil) {
			continue
		}
		verified, err := internalrpcauth.VerifyCanonicalJSON(compact, candidate.key,
			internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: candidate.key.KeyID})
		if err != nil {
			continue
		}
		var claims Claims
		if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil ||
			claims.Generation != generation || verifier.validate(claims) != nil {
			return Claims{}, errors.New("interaction delivery readback claims are invalid")
		}
		digest := sha256.Sum256([]byte(compact))
		claims.CredentialSHA256 = hex.EncodeToString(digest[:])
		return claims, nil
	}
	return Claims{}, errors.New("interaction delivery readback credential is invalid")
}

func (verifier *Verifier) refresh(ctx context.Context) error {
	raw, err := readKeyset(verifier.config.PublicKeysetFile)
	if err != nil {
		return err
	}
	keys, state, identities, err := parseKeyset(verifier.config, raw, verifier.now().UTC())
	if err != nil {
		return err
	}
	if err := verifier.fence.AdmitDeliveryReadbackKeyset(ctx, state.revision, state.highWatermark,
		state.generation, state.digest, identities); err != nil {
		return err
	}
	verifier.mu.Lock()
	verifier.keys, verifier.state = keys, state
	verifier.mu.Unlock()
	return nil
}

func parseKeyset(config Config, raw []byte, now time.Time) (map[uint64]verificationKey, servedState, []KeyIdentity, error) {
	var document publicKeyset
	if json.Unmarshal(raw, &document) != nil || document.Version != 1 || document.Revision == 0 ||
		document.HighWatermark == 0 || document.ServedGeneration != config.Generation ||
		document.HighWatermark != document.ServedGeneration || len(document.Keys) == 0 || len(document.Keys) > 4 {
		return nil, servedState{}, nil, errors.New("interaction delivery readback keyset is invalid")
	}
	keys := make(map[uint64]verificationKey)
	identities := make([]KeyIdentity, 0, len(document.Keys))
	seenKid, seenThumb := map[string]struct{}{}, map[string]struct{}{}
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || !slices.Contains([]string{"CURRENT", "NEXT", "PREVIOUS", "RETIRED"}, reference.Status) {
			return nil, servedState{}, nil, errors.New("interaction delivery readback key lifecycle is invalid")
		}
		key, err := internalrpcauth.ParsePublicJWK(reference.JWK)
		if err != nil || key.KeyID == "" {
			return nil, servedState{}, nil, errors.New("parse interaction delivery readback public JWK")
		}
		thumb, err := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if err != nil {
			return nil, servedState{}, nil, err
		}
		if _, ok := seenKid[key.KeyID]; ok {
			return nil, servedState{}, nil, errors.New("interaction delivery readback keyset has duplicate kid")
		}
		if _, ok := seenThumb[thumb]; ok {
			return nil, servedState{}, nil, errors.New("interaction delivery readback keyset has duplicate public key")
		}
		if _, ok := keys[reference.Generation]; ok {
			return nil, servedState{}, nil, errors.New("interaction delivery readback keyset has duplicate generation")
		}
		seenKid[key.KeyID], seenThumb[thumb] = struct{}{}, struct{}{}
		identities = append(identities, KeyIdentity{reference.Generation, reference.Status, key.KeyID, thumb})
		switch reference.Status {
		case "CURRENT":
			if reference.Generation != document.ServedGeneration {
				return nil, servedState{}, nil, errors.New("interaction delivery readback CURRENT key is invalid")
			}
			current++
			keys[reference.Generation] = verificationKey{key: key, status: reference.Status}
		case "PREVIOUS":
			if reference.AcceptUntil <= now.Unix() || reference.Generation >= document.ServedGeneration {
				return nil, servedState{}, nil, errors.New("interaction delivery readback PREVIOUS overlap is invalid")
			}
			keys[reference.Generation] = verificationKey{key: key, status: reference.Status,
				acceptUntil: time.Unix(reference.AcceptUntil, 0).UTC()}
		case "NEXT":
			if reference.Generation <= document.ServedGeneration || reference.Generation > document.HighWatermark+1 {
				return nil, servedState{}, nil, errors.New("interaction delivery readback NEXT key is invalid")
			}
		case "RETIRED":
			if reference.Generation >= document.ServedGeneration {
				return nil, servedState{}, nil, errors.New("interaction delivery readback RETIRED key is invalid")
			}
		}
	}
	if current != 1 {
		return nil, servedState{}, nil, errors.New("interaction delivery readback keyset must contain one CURRENT key")
	}
	sum := sha256.Sum256(raw)
	return keys, servedState{document.Revision, document.HighWatermark, document.ServedGeneration,
		hex.EncodeToString(sum[:])}, identities, nil
}

func (verifier *Verifier) validate(claims Claims) error {
	now := verifier.now().UTC().Truncate(time.Second)
	issued, notBefore, expires := time.Unix(claims.IssuedAt, 0).UTC(), time.Unix(claims.NotBefore, 0).UTC(), time.Unix(claims.ExpiresAt, 0).UTC()
	if claims.Version != 1 || claims.Issuer != verifier.config.Issuer || claims.Audience != verifier.config.Audience ||
		claims.ProducerID != verifier.config.ProducerID || claims.Purpose != verifier.config.Purpose ||
		claims.WorkloadID != "control-plane" || claims.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane" ||
		claims.Operation != verifier.config.Operation || claims.Permission != verifier.config.Permission ||
		uuid.Validate(claims.Subject) != nil || uuid.Validate(claims.OrganizationID) != nil ||
		uuid.Validate(claims.ProjectID) != nil || uuid.Validate(claims.DeliveryID) != nil || uuid.Validate(claims.JTI) != nil ||
		notBefore.Before(issued.Add(-5*time.Second)) || notBefore.After(issued.Add(5*time.Second)) ||
		issued.After(now.Add(5*time.Second)) || now.Before(notBefore.Add(-5*time.Second)) ||
		!now.Before(expires.Add(5*time.Second)) || !expires.After(notBefore) || expires.Sub(issued) > verifier.config.MaximumTTL {
		return errors.New("interaction delivery readback claims are invalid")
	}
	return nil
}

func readKeyset(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("interaction delivery readback trust file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read interaction delivery readback trust")
	}
	return raw, nil
}
