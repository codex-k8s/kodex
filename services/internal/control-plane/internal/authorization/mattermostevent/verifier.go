// Package mattermostevent проверяет короткоживущую проекцию подтверждённого
// Mattermost события для единственного interaction-gateway workload.
package mattermostevent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	Issuer         string
	Audience       string
	WorkloadID     string
	CallerSPIFFEID string
	PublicJWKFile  string
}

type Verifier struct {
	config Config
	key    internalrpcauth.ES256Key
	now    func() time.Time
}

type claims struct {
	Version        int    `json:"v"`
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

func New(config Config) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" || config.WorkloadID != "interaction-gateway" ||
		config.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway" ||
		!filepath.IsAbs(config.PublicJWKFile) {
		return nil, errors.New("Mattermost event verifier configuration is invalid")
	}
	info, err := os.Stat(config.PublicJWKFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > 64<<10 || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("Mattermost event trust file is unsafe")
	}
	raw, err := os.ReadFile(config.PublicJWKFile)
	if err != nil {
		return nil, errors.New("read Mattermost event trust")
	}
	key, err := internalrpcauth.ParsePublicJWK(raw)
	if err != nil {
		return nil, errors.New("parse Mattermost event trust")
	}
	return &Verifier{config: config, key: key, now: time.Now}, nil
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
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	values := incoming.Get("authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || len(values[0]) > maximumCredential {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	compact := strings.TrimPrefix(values[0], "Bearer ")
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, verifier.key, internalrpcauth.ProtectedHeaderExpectation{
		Type: credentialType, KeyID: verifier.key.KeyID,
	})
	if err != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	var parsed claims
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &parsed) != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	now := verifier.now().UTC().Truncate(time.Second)
	issuedAt := time.Unix(parsed.IssuedAt, 0).UTC()
	notBefore := time.Unix(parsed.NotBefore, 0).UTC()
	expiresAt := time.Unix(parsed.ExpiresAt, 0).UTC()
	if parsed.Version != 1 || parsed.Issuer != verifier.config.Issuer || parsed.Audience != verifier.config.Audience ||
		parsed.WorkloadID != verifier.config.WorkloadID || parsed.CallerSPIFFEID != verifier.config.CallerSPIFFEID ||
		value.ValidateID(parsed.Subject) != nil || value.ValidateID(parsed.OrganizationID) != nil ||
		value.ValidateID(parsed.ProjectID) != nil || value.ValidateID(parsed.JTI) != nil || parsed.Revision == 0 ||
		parsed.Generation == 0 || !validDigest(parsed.EventSHA256) || parsed.TeamID == "" || parsed.ChannelID == "" ||
		notBefore.Before(issuedAt.Add(-maximumClockSkew)) || notBefore.After(issuedAt.Add(maximumClockSkew)) ||
		issuedAt.After(now.Add(maximumClockSkew)) || now.Before(notBefore.Add(-maximumClockSkew)) ||
		!now.Before(expiresAt.Add(maximumClockSkew)) || !expiresAt.After(notBefore) ||
		expiresAt.Sub(issuedAt) > 5*time.Minute {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ActorID: parsed.Subject, OrganizationID: parsed.OrganizationID, ProjectID: parsed.ProjectID,
		SessionJTI: parsed.JTI, SessionRevision: parsed.Revision,
		SubjectDigest: digest("MATTERMOST_ACTOR:" + parsed.Subject), CredentialDigest: parsed.EventSHA256,
		TenantOwner: parsed.TenantOwner, CallerWorkload: parsed.WorkloadID,
		CallerSPIFFEID: parsed.CallerSPIFFEID, BoundGeneration: parsed.Generation,
	}, nil
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
