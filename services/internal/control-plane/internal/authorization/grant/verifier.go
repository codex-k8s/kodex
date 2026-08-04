// Package grant проверяет независимо доставленные разрешения рабочих нагрузок
// для сервиса выдачи доказательств полномочий.
package grant

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
	grantType          = "mattercodex-application-grant+jws"
	maximumGrantTTL    = 5 * time.Minute
	maximumClockSkew   = 5 * time.Second
	maximumBearerBytes = 16 << 10
)

type Config struct {
	ProducerID     string
	Purpose        string
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
	SessionID      string `json:"session_id,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	Attempt        uint32 `json:"attempt,omitempty"`
	InputSHA256    string `json:"input_sha256,omitempty"`
	Generation     uint64 `json:"generation,omitempty"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

func New(config Config) (*Verifier, error) {
	if config.ProducerID == "" || config.Purpose == "" || config.Issuer == "" || config.Audience == "" ||
		value.ValidateStableKey(config.WorkloadID) != nil ||
		config.CallerSPIFFEID == "" ||
		!filepath.IsAbs(config.PublicJWKFile) {
		return nil, errors.New("application grant verifier configuration is invalid")
	}
	info, err := os.Stat(config.PublicJWKFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > 64<<10 || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("application grant trust file is unsafe")
	}
	raw, err := os.ReadFile(config.PublicJWKFile)
	if err != nil {
		return nil, errors.New("read application grant trust")
	}
	key, err := internalrpcauth.ParsePublicJWK(raw)
	if err != nil {
		return nil, errors.New("parse application grant trust")
	}
	return &Verifier{config: config, key: key, now: time.Now}, nil
}

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
		certificate.URIs[0].String() != verifier.config.CallerSPIFFEID {
		return errs.ErrPermissionDenied
	}
	return nil
}

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
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") ||
		len(values[0]) <= len("Bearer ") || len(values[0]) > maximumBearerBytes {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	compact := strings.TrimPrefix(values[0], "Bearer ")
	if strings.TrimSpace(compact) != compact {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		verifier.key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  grantType,
			KeyID: verifier.key.KeyID,
		},
	)
	if err != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	var parsed claims
	if internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&parsed,
	) != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	now := verifier.now().UTC().Truncate(time.Second)
	issuedAt := time.Unix(parsed.IssuedAt, 0).UTC()
	notBefore := time.Unix(parsed.NotBefore, 0).UTC()
	expiresAt := time.Unix(parsed.ExpiresAt, 0).UTC()
	if parsed.Version != 1 ||
		parsed.Issuer != verifier.config.Issuer ||
		parsed.Audience != verifier.config.Audience ||
		parsed.WorkloadID != verifier.config.WorkloadID ||
		parsed.CallerSPIFFEID != verifier.config.CallerSPIFFEID ||
		value.ValidateID(parsed.Subject) != nil ||
		value.ValidateID(parsed.OrganizationID) != nil ||
		value.ValidateID(parsed.ProjectID) != nil ||
		value.ValidateID(parsed.JTI) != nil ||
		parsed.Revision == 0 ||
		notBefore.Before(issuedAt.Add(-maximumClockSkew)) ||
		notBefore.After(issuedAt.Add(maximumClockSkew)) ||
		issuedAt.After(now.Add(maximumClockSkew)) ||
		now.Before(notBefore.Add(-maximumClockSkew)) ||
		!now.Before(expiresAt.Add(maximumClockSkew)) ||
		!expiresAt.After(notBefore) ||
		expiresAt.Sub(issuedAt) > maximumGrantTTL {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	if (verifier.config.WorkloadID == "agent-runner" ||
		verifier.config.WorkloadID == "runtime-controller" ||
		verifier.config.WorkloadID == "integration-gateway" ||
		verifier.config.WorkloadID == "runtime-restore-verifier" ||
		verifier.config.WorkloadID == "runtime-cleanup-authorizer") &&
		(value.ValidateID(parsed.SessionID) != nil ||
			value.ValidateID(parsed.TurnID) != nil ||
			parsed.Attempt == 0 || parsed.Attempt > 100 ||
			!validSHA256(parsed.InputSHA256) ||
			parsed.Generation == 0) {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ProducerID:           verifier.config.ProducerID,
		CredentialPurpose:    verifier.config.Purpose,
		CredentialGeneration: parsed.Revision,
		ActorID:              parsed.Subject,
		OrganizationID:       parsed.OrganizationID,
		ProjectID:            parsed.ProjectID,
		SessionJTI:           parsed.JTI,
		SessionRevision:      parsed.Revision,
		SubjectDigest:        digest("WORKLOAD_SUBJECT:" + parsed.Subject),
		CredentialDigest:     digest(compact),
		TenantOwner:          parsed.TenantOwner,
		CallerWorkload:       verifier.config.WorkloadID,
		CallerSPIFFEID:       verifier.config.CallerSPIFFEID,
		BoundSessionID:       parsed.SessionID,
		BoundTurnID:          parsed.TurnID,
		BoundAttempt:         parsed.Attempt,
		BoundInputSHA256:     parsed.InputSHA256,
		BoundGeneration:      parsed.Generation,
	}, nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validSHA256(value string) bool {
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
