// Package mattermostevent проверяет короткоживущую проекцию подтверждённого
// Mattermost события для единственного interaction-gateway workload.
package mattermostevent

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
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

const (
	credentialType    = "mattercodex-mattermost-event+jws"
	maximumClockSkew  = 5 * time.Second
	maximumCredential = 16 << 10
)

type Config struct {
	ProducerID       string
	Purpose          string
	Issuer           string
	Audience         string
	WorkloadID       string
	CallerSPIFFEID   string
	PublicKeysetFile string
	MaximumTTL       time.Duration
}

type Verifier struct {
	config    Config
	fence     KeysetFence
	refreshMu sync.Mutex
	mu        sync.RWMutex
	keys      map[uint64]verificationKey
	state     servedState
	retired   []int64
	active    []int64
	now       func() time.Time
}

type verificationKey struct {
	key         internalrpcauth.ES256Key
	acceptUntil time.Time
}

type servedState struct {
	revision         uint64
	highWatermark    uint64
	servedGeneration uint64
	digest           string
}

type publicKeySet struct {
	Version          int            `json:"version"`
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

// KeysetFence хранит verifier-owned high-watermark независимо от pod lifecycle.
type KeysetFence interface {
	AdmitMattermostEventKeyset(context.Context, string, uint64, uint64, uint64, string, []int64, []int64) error
}

type claims struct {
	Version        int    `json:"v"`
	ProducerID     string `json:"producer_id"`
	Purpose        string `json:"purpose"`
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	JTI            string `json:"jti"`
	Revision       uint64 `json:"revision"`
	TenantOwner    bool   `json:"tenant_owner"`
	WorkloadID     string `json:"workload_id"`
	CallerSPIFFEID string `json:"caller_spiffe_id"`
	EventSHA256    string `json:"event_sha256"`
	TeamID         string `json:"team_id"`
	ChannelID      string `json:"channel_id"`
	PostID         string `json:"post_id,omitempty"`
	Generation     uint64 `json:"generation"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

func New(ctx context.Context, config Config, fence KeysetFence) (*Verifier, error) {
	if config.ProducerID == "" || config.Purpose != "MATTERMOST_SIGNED_EVENT" ||
		config.Issuer == "" || config.Audience == "" || config.WorkloadID != "interaction-gateway" ||
		config.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway" ||
		!filepath.IsAbs(config.PublicKeysetFile) || config.MaximumTTL < 30*time.Second ||
		config.MaximumTTL > 5*time.Minute || fence == nil {
		return nil, errors.New("mattermost event verifier configuration is invalid")
	}
	raw, err := readPublicKeysetFile(config.PublicKeysetFile)
	if err != nil {
		return nil, err
	}
	keys, state, retired, active, err := parsePublicKeyset(config, raw, time.Now().UTC())
	if err != nil {
		return nil, errors.New("parse Mattermost event trust")
	}
	if err := fence.AdmitMattermostEventKeyset(ctx, config.ProducerID, state.revision,
		state.highWatermark, state.servedGeneration, state.digest, retired, active); err != nil {
		return nil, err
	}
	return &Verifier{config: config, fence: fence, keys: keys, state: state,
		retired: slices.Clone(retired), active: slices.Clone(active), now: time.Now}, nil
}

func (verifier *Verifier) VerifyPeer(ctx context.Context) error {
	peerValue, ok := peer.FromContext(ctx)
	if !ok {
		return errs.ErrUnauthenticated
	}
	tlsInfo, ok := peerValue.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) != 1 ||
		len(tlsInfo.State.PeerCertificates[0].URIs) != 1 {
		return errs.ErrUnauthenticated
	}
	if tlsInfo.State.PeerCertificates[0].URIs[0].String() != verifier.config.CallerSPIFFEID {
		return errs.ErrPermissionDenied
	}
	return nil
}

func (verifier *Verifier) Authenticate(ctx context.Context) (authoritytype.ApplicationIdentity, error) {
	if err := verifier.VerifyPeer(ctx); err != nil {
		return authoritytype.ApplicationIdentity{}, err
	}
	if err := verifier.refresh(ctx); err != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	values := incoming.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) > maximumCredential {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	compact := strings.TrimPrefix(values[0], "Bearer ")
	parsed, ok := verifier.verifyCredential(compact)
	if !ok {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	now := verifier.now().UTC().Truncate(time.Second)
	issuedAt := time.Unix(parsed.IssuedAt, 0).UTC()
	notBefore := time.Unix(parsed.NotBefore, 0).UTC()
	expiresAt := time.Unix(parsed.ExpiresAt, 0).UTC()
	if parsed.Version != 1 || parsed.ProducerID != verifier.config.ProducerID || parsed.Purpose != verifier.config.Purpose ||
		parsed.Issuer != verifier.config.Issuer || parsed.Audience != verifier.config.Audience ||
		parsed.WorkloadID != verifier.config.WorkloadID || parsed.CallerSPIFFEID != verifier.config.CallerSPIFFEID ||
		value.ValidateID(parsed.Subject) != nil || value.ValidateID(parsed.OrganizationID) != nil ||
		value.ValidateID(parsed.ProjectID) != nil || value.ValidateID(parsed.JTI) != nil || parsed.Revision == 0 ||
		parsed.Generation == 0 || !validDigest(parsed.EventSHA256) || parsed.TeamID == "" || parsed.ChannelID == "" ||
		notBefore.Before(issuedAt.Add(-maximumClockSkew)) || notBefore.After(issuedAt.Add(maximumClockSkew)) ||
		issuedAt.After(now.Add(maximumClockSkew)) || now.Before(notBefore.Add(-maximumClockSkew)) ||
		!now.Before(expiresAt.Add(maximumClockSkew)) || !expiresAt.After(notBefore) ||
		expiresAt.Sub(issuedAt) > verifier.config.MaximumTTL {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ProducerID: verifier.config.ProducerID, CredentialPurpose: verifier.config.Purpose,
		CredentialGeneration: parsed.Generation,
		ActorID:              parsed.Subject, OrganizationID: parsed.OrganizationID, ProjectID: parsed.ProjectID,
		SessionJTI: parsed.JTI, SessionRevision: parsed.Revision,
		SubjectDigest: digest("MATTERMOST_ACTOR:" + parsed.Subject), CredentialDigest: parsed.EventSHA256,
		TenantOwner: parsed.TenantOwner, CallerWorkload: parsed.WorkloadID,
		CallerSPIFFEID: parsed.CallerSPIFFEID,
	}, nil
}

func (verifier *Verifier) verifyCredential(compact string) (claims, bool) {
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	now := verifier.now().UTC()
	for generation, candidate := range verifier.keys {
		if !candidate.acceptUntil.IsZero() && !now.Before(candidate.acceptUntil) {
			continue
		}
		verified, err := internalrpcauth.VerifyCanonicalJSON(compact, candidate.key,
			internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: candidate.key.KeyID})
		if err != nil {
			continue
		}
		var parsed claims
		if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &parsed) != nil || parsed.Generation != generation {
			return claims{}, false
		}
		return parsed, true
	}
	return claims{}, false
}

func (verifier *Verifier) refresh(ctx context.Context) error {
	verifier.refreshMu.Lock()
	defer verifier.refreshMu.Unlock()
	raw, err := readPublicKeysetFile(verifier.config.PublicKeysetFile)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	verifier.mu.RLock()
	unchanged := digest == verifier.state.digest
	currentState := verifier.state
	currentRetired, currentActive := slices.Clone(verifier.retired), slices.Clone(verifier.active)
	verifier.mu.RUnlock()
	if unchanged {
		return verifier.fence.AdmitMattermostEventKeyset(ctx, verifier.config.ProducerID, currentState.revision,
			currentState.highWatermark, currentState.servedGeneration, currentState.digest, currentRetired, currentActive)
	}
	keys, state, retired, active, err := parsePublicKeyset(verifier.config, raw, verifier.now().UTC())
	if err != nil {
		return err
	}
	if err := verifier.fence.AdmitMattermostEventKeyset(ctx, verifier.config.ProducerID, state.revision,
		state.highWatermark, state.servedGeneration, state.digest, retired, active); err != nil {
		return err
	}
	verifier.mu.Lock()
	verifier.keys, verifier.state = keys, state
	verifier.retired, verifier.active = slices.Clone(retired), slices.Clone(active)
	verifier.mu.Unlock()
	return nil
}

func readPublicKeysetFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > 64<<10 || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("mattermost event trust file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) != info.Size() {
		return nil, errors.New("read Mattermost event trust")
	}
	return raw, nil
}

func parsePublicKeyset(config Config, raw []byte, now time.Time) (
	map[uint64]verificationKey, servedState, []int64, []int64, error,
) {
	var document publicKeySet
	if internalrpcauth.DecodeCanonicalJSON(raw, &document) != nil || document.Version != 1 ||
		document.Revision == 0 || document.HighWatermark == 0 || document.ServedGeneration == 0 ||
		document.HighWatermark != document.ServedGeneration || len(document.Keys) == 0 || len(document.Keys) > 4 {
		return nil, servedState{}, nil, nil, errors.New("mattermost event verifier keyset is invalid")
	}
	keys := make(map[uint64]verificationKey, 2)
	seenGenerations := make(map[uint64]struct{}, len(document.Keys))
	seenKeyIDs := make(map[string]struct{}, len(document.Keys))
	retired := make([]int64, 0, len(document.Keys))
	active := make([]int64, 0, len(document.Keys))
	current := 0
	for _, reference := range document.Keys {
		if reference.Generation == 0 || len(reference.JWK) == 0 {
			return nil, servedState{}, nil, nil, errors.New("mattermost event verifier keyset entry is invalid")
		}
		if _, exists := seenGenerations[reference.Generation]; exists {
			return nil, servedState{}, nil, nil, errors.New("mattermost event verifier keyset has duplicate generation")
		}
		seenGenerations[reference.Generation] = struct{}{}
		key, err := internalrpcauth.ParsePublicJWK(reference.JWK)
		if err != nil || key.KeyID == "" {
			return nil, servedState{}, nil, nil, errors.New("parse Mattermost event verifier public JWK")
		}
		if _, exists := seenKeyIDs[key.KeyID]; exists {
			return nil, servedState{}, nil, nil, errors.New("mattermost event verifier keyset has duplicate kid")
		}
		seenKeyIDs[key.KeyID] = struct{}{}
		switch reference.Status {
		case "CURRENT":
			if reference.Generation != document.ServedGeneration || reference.AcceptUntil != 0 {
				return nil, servedState{}, nil, nil, errors.New("mattermost event verifier CURRENT key is invalid")
			}
			active = append(active, int64(reference.Generation))
			keys[reference.Generation] = verificationKey{key: key}
			current++
		case "PREVIOUS":
			acceptUntil := time.Unix(reference.AcceptUntil, 0).UTC()
			if reference.Generation+1 != document.ServedGeneration || reference.AcceptUntil <= 0 ||
				!now.Before(acceptUntil) || acceptUntil.After(now.Add(config.MaximumTTL)) {
				return nil, servedState{}, nil, nil, errors.New("mattermost event verifier PREVIOUS overlap is invalid")
			}
			active = append(active, int64(reference.Generation))
			keys[reference.Generation] = verificationKey{key: key, acceptUntil: acceptUntil}
		case "NEXT":
			if reference.Generation != document.ServedGeneration+1 || reference.AcceptUntil != 0 {
				return nil, servedState{}, nil, nil, errors.New("mattermost event verifier NEXT key is invalid")
			}
			active = append(active, int64(reference.Generation))
		case "RETIRED":
			if reference.Generation >= document.ServedGeneration || reference.AcceptUntil != 0 {
				return nil, servedState{}, nil, nil, errors.New("mattermost event verifier RETIRED key is invalid")
			}
			retired = append(retired, int64(reference.Generation))
		default:
			return nil, servedState{}, nil, nil, errors.New("mattermost event verifier key status is invalid")
		}
	}
	if current != 1 {
		return nil, servedState{}, nil, nil, errors.New("mattermost event verifier keyset must contain one CURRENT key")
	}
	slices.Sort(retired)
	slices.Sort(active)
	sum := sha256.Sum256(raw)
	return keys, servedState{revision: document.Revision, highWatermark: document.HighWatermark,
		servedGeneration: document.ServedGeneration, digest: hex.EncodeToString(sum[:])}, retired, active, nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}
