// Package readbackgrant проверяет application credential для exact delivery
// readback; mTLS peer остаётся отдельным обязательным слоем.
package readbackgrant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

const credentialType = "mattercodex-interaction-delivery-readback+jws"

type Config struct {
	Issuer        string
	Audience      string
	ProducerID    string
	Purpose       string
	Operation     string
	Permission    string
	PublicJWKFile string
	Generation    uint64
	MaximumTTL    time.Duration
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

type Verifier struct {
	config Config
	key    internalrpcauth.ES256Key
	now    func() time.Time
}

func New(config Config) (*Verifier, error) {
	if config.Issuer == "" || config.Audience == "" ||
		config.ProducerID != "control-plane.interaction-delivery-readback" ||
		config.Purpose != "INTERACTION_DELIVERY_READBACK_GRANT" ||
		config.Operation != "interaction.delivery.read" || config.Permission != "interaction.delivery.read" ||
		config.Generation == 0 || config.MaximumTTL < time.Minute || config.MaximumTTL > 15*time.Minute ||
		!filepath.IsAbs(config.PublicJWKFile) {
		return nil, errors.New("interaction delivery readback verifier configuration is invalid")
	}
	info, err := os.Stat(config.PublicJWKFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 64<<10 || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("interaction delivery readback trust file is unsafe")
	}
	raw, err := os.ReadFile(config.PublicJWKFile)
	if err != nil {
		return nil, errors.New("read interaction delivery readback trust")
	}
	key, err := internalrpcauth.ParsePublicJWK(raw)
	if err != nil {
		return nil, errors.New("parse interaction delivery readback trust")
	}
	return &Verifier{config: config, key: key, now: time.Now}, nil
}

func (verifier *Verifier) Verify(ctx context.Context, authorization string) (Claims, error) {
	if err := ctx.Err(); err != nil {
		return Claims{}, err
	}
	if !strings.HasPrefix(authorization, "Bearer ") || len(authorization) > 16<<10 {
		return Claims{}, errors.New("interaction delivery readback credential is missing")
	}
	compact := strings.TrimPrefix(authorization, "Bearer ")
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, verifier.key,
		internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: verifier.key.KeyID})
	if err != nil {
		return Claims{}, errors.New("interaction delivery readback credential is invalid")
	}
	var claims Claims
	if internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims) != nil || verifier.validate(claims) != nil {
		return Claims{}, errors.New("interaction delivery readback claims are invalid")
	}
	return claims, nil
}

func (verifier *Verifier) validate(claims Claims) error {
	now := verifier.now().UTC().Truncate(time.Second)
	issued, notBefore, expires := time.Unix(claims.IssuedAt, 0).UTC(), time.Unix(claims.NotBefore, 0).UTC(), time.Unix(claims.ExpiresAt, 0).UTC()
	if claims.Version != 1 || claims.Issuer != verifier.config.Issuer || claims.Audience != verifier.config.Audience ||
		claims.ProducerID != verifier.config.ProducerID || claims.Purpose != verifier.config.Purpose ||
		claims.WorkloadID != "control-plane" ||
		claims.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane" ||
		claims.Operation != verifier.config.Operation || claims.Permission != verifier.config.Permission ||
		claims.Generation != verifier.config.Generation || uuid.Validate(claims.Subject) != nil ||
		uuid.Validate(claims.OrganizationID) != nil || uuid.Validate(claims.ProjectID) != nil ||
		uuid.Validate(claims.DeliveryID) != nil || uuid.Validate(claims.JTI) != nil ||
		notBefore.Before(issued.Add(-5*time.Second)) || notBefore.After(issued.Add(5*time.Second)) ||
		issued.After(now.Add(5*time.Second)) || now.Before(notBefore.Add(-5*time.Second)) ||
		!now.Before(expires.Add(5*time.Second)) || !expires.After(notBefore) ||
		expires.Sub(issued) > verifier.config.MaximumTTL {
		return errors.New("interaction delivery readback claims are invalid")
	}
	return nil
}

func ReadinessCredential(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("interaction delivery readiness credential path is invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<10 || info.Mode().Perm()&0o037 != 0 {
		return "", errors.New("interaction delivery readiness credential file is unsafe")
	}
	raw, err := os.ReadFile(path)
	value := strings.TrimSpace(string(raw))
	if err != nil || value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("read interaction delivery readiness credential")
	}
	return "Bearer " + value, nil
}
