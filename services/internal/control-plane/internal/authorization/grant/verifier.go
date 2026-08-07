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
	grantType           = "mattercodex-application-grant+jws"
	providerReceiptType = "mattercodex-provider-effect-readback-receipt+jws"
	maximumGrantTTL     = 5 * time.Minute
	maximumClockSkew    = 5 * time.Second
	maximumBearerBytes  = 16 << 10
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

type providerReceiptClaims struct {
	ContractVersion          uint32    `json:"contract_version"`
	Issuer                   string    `json:"iss"`
	Purpose                  string    `json:"purpose"`
	WorkloadID               string    `json:"workload_id"`
	CallerSPIFFEID           string    `json:"caller_spiffe_id"`
	FullMethod               string    `json:"full_method"`
	ActorID                  string    `json:"actor_id"`
	OrganizationID           string    `json:"organization_id"`
	ProjectID                string    `json:"project_id"`
	WorkspaceID              string    `json:"workspace_id,omitempty"`
	ProviderTeamRef          string    `json:"provider_team_ref,omitempty"`
	ProviderObjectRef        string    `json:"provider_object_ref,omitempty"`
	Action                   string    `json:"action"`
	Effect                   string    `json:"effect"`
	EffectVersion            uint64    `json:"effect_version"`
	EffectGeneration         uint64    `json:"effect_generation"`
	EffectSHA256             string    `json:"effect_sha256"`
	ReceiptID                string    `json:"jti"`
	ReceiptRevision          uint64    `json:"revision"`
	IssuedAt                 time.Time `json:"issued_at"`
	NotBefore                time.Time `json:"not_before"`
	ExpiresAt                time.Time `json:"expires_at"`
	CredentialBindingID      string    `json:"credential_binding_id,omitempty"`
	CredentialBindingVersion uint64    `json:"credential_binding_version,omitempty"`
	CredentialBindingSHA256  string    `json:"credential_binding_sha256,omitempty"`
	ProviderUsername         string    `json:"provider_username,omitempty"`
	MaskedStatus             string    `json:"masked_status"`
	Provider                 string    `json:"provider,omitempty"`
	MaskedLabel              string    `json:"masked_label,omitempty"`
	Capabilities             []string  `json:"capabilities,omitempty"`
	Eligible                 bool      `json:"eligible"`
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
	expectedType := grantType
	if providerReceiptPurpose(verifier.config.Purpose) {
		expectedType = providerReceiptType
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		verifier.key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  expectedType,
			KeyID: verifier.key.KeyID,
		},
	)
	if err != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	if providerReceiptPurpose(verifier.config.Purpose) {
		return verifier.authenticateProviderReceipt(verified.CanonicalPayload)
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
		(parsed.ProjectID == "" && verifier.config.WorkloadID != "automation-scheduler") ||
		(parsed.ProjectID != "" && value.ValidateID(parsed.ProjectID) != nil) ||
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

func providerReceiptPurpose(purpose string) bool {
	return purpose == "MATTERMOST_PROVIDER_READBACK_RECEIPT" || purpose == "AI_PROVIDER_READBACK_RECEIPT"
}

func (verifier *Verifier) authenticateProviderReceipt(canonicalPayload []byte) (authoritytype.ApplicationIdentity, error) {
	var parsed providerReceiptClaims
	if internalrpcauth.DecodeCanonicalJSON(canonicalPayload, &parsed) != nil {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	now := verifier.now().UTC()
	credentialBound := parsed.CredentialBindingID != "" || parsed.CredentialBindingVersion != 0 || parsed.CredentialBindingSHA256 != ""
	if parsed.ContractVersion != 1 || parsed.Issuer != verifier.config.Issuer || parsed.Purpose != verifier.config.Purpose ||
		parsed.WorkloadID != verifier.config.WorkloadID || parsed.CallerSPIFFEID != verifier.config.CallerSPIFFEID ||
		value.ValidateID(parsed.ActorID) != nil || value.ValidateID(parsed.OrganizationID) != nil || value.ValidateID(parsed.ProjectID) != nil ||
		(parsed.WorkspaceID != "" && value.ValidateID(parsed.WorkspaceID) != nil) || value.ValidateStableKey(parsed.Action) != nil ||
		value.ValidateStableKey(parsed.Effect) != nil || parsed.EffectVersion == 0 || parsed.EffectGeneration == 0 ||
		!validSHA256(parsed.EffectSHA256) || value.ValidateID(parsed.ReceiptID) != nil || parsed.ReceiptRevision == 0 ||
		parsed.NotBefore.Before(parsed.IssuedAt.Add(-maximumClockSkew)) ||
		parsed.NotBefore.After(parsed.IssuedAt.Add(maximumClockSkew)) || parsed.IssuedAt.After(now.Add(maximumClockSkew)) ||
		!parsed.ExpiresAt.After(parsed.NotBefore) ||
		parsed.ExpiresAt.Sub(parsed.IssuedAt) > maximumGrantTTL || now.Before(parsed.NotBefore.Add(-maximumClockSkew)) ||
		!now.Before(parsed.ExpiresAt.Add(maximumClockSkew)) || parsed.FullMethod == "" ||
		(parsed.Provider != "" && value.ValidateStableKey(parsed.Provider) != nil) || len(parsed.MaskedLabel) > 256 ||
		!validStableKeys(parsed.Capabilities) ||
		(credentialBound && (value.ValidateID(parsed.CredentialBindingID) != nil || parsed.CredentialBindingVersion == 0 || !validSHA256(parsed.CredentialBindingSHA256))) {
		return authoritytype.ApplicationIdentity{}, errs.ErrUnauthenticated
	}
	return authoritytype.ApplicationIdentity{
		ProducerID: verifier.config.ProducerID, CredentialPurpose: verifier.config.Purpose,
		CredentialGeneration: parsed.ReceiptRevision, ActorID: parsed.ActorID, OrganizationID: parsed.OrganizationID,
		ProjectID: parsed.ProjectID, SessionJTI: parsed.ReceiptID, SessionRevision: parsed.ReceiptRevision,
		SubjectDigest: digest("PROVIDER_RECEIPT_SUBJECT:" + parsed.ActorID), CredentialDigest: digest(string(canonicalPayload)),
		CallerWorkload: verifier.config.WorkloadID, CallerSPIFFEID: verifier.config.CallerSPIFFEID,
		ProviderReceiptFullMethod: parsed.FullMethod, ProviderReceiptPurpose: parsed.Purpose,
	}, nil
}

func validStableKeys(values []string) bool {
	if len(values) > 64 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, item := range values {
		if value.ValidateStableKey(item) != nil {
			return false
		}
		if _, exists := seen[item]; exists {
			return false
		}
		seen[item] = struct{}{}
	}
	return true
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
