// Package mattermostevent выпускает короткоживущее доказательство только из
// подтверждённого REST/WebSocket события и server-owned mapping.
package mattermostevent

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

const (
	credentialType  = "mattercodex-mattermost-event+jws"
	maximumKeyBytes = 64 << 10
)

type Config struct {
	ProducerID      string
	Purpose         string
	Issuer          string
	Audience        string
	PrivateJWKFile  string
	CallbackKeyFile string
	Generation      uint64
	MaximumTTL      time.Duration
}

type Signer struct {
	config      Config
	key         internalrpcauth.ES256Key
	callbackKey []byte
	now         func() time.Time
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

func New(config Config) (*Signer, error) {
	if config.ProducerID != "control-plane.interaction-gateway" || config.Purpose != "MATTERMOST_SIGNED_EVENT" ||
		config.Issuer == "" || config.Audience == "" || config.Generation == 0 ||
		config.MaximumTTL < 30*time.Second || config.MaximumTTL > 5*time.Minute ||
		!filepath.IsAbs(config.PrivateJWKFile) || !filepath.IsAbs(config.CallbackKeyFile) {
		return nil, errors.New("mattermost event signer configuration is invalid")
	}
	privateRaw, err := readSafe(config.PrivateJWKFile, maximumKeyBytes)
	if err != nil {
		return nil, errors.New("read Mattermost event signing key")
	}
	key, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse Mattermost event signing key")
	}
	callbackKey, err := readSafe(config.CallbackKeyFile, 128)
	if err != nil || len(callbackKey) < 32 {
		return nil, errors.New("read Mattermost callback key")
	}
	return &Signer{config: config, key: key, callbackKey: callbackKey, now: time.Now}, nil
}

func (signer *Signer) Sign(event entity.InboundEvent) (string, error) {
	if uuid.Validate(event.ID) != nil || uuid.Validate(event.ActorID) != nil ||
		uuid.Validate(event.OrganizationID) != nil || uuid.Validate(event.ProjectID) != nil ||
		event.Revision == 0 || !validDigest(event.DigestSHA256) ||
		event.TeamID == "" || event.ChannelID == "" || event.UserID == "" {
		return "", errors.New("mattermost event authority is invalid")
	}
	now := signer.now().UTC().Truncate(time.Second)
	payload := claims{
		Version: 1, ProducerID: signer.config.ProducerID, Purpose: signer.config.Purpose,
		Issuer: signer.config.Issuer, Audience: signer.config.Audience,
		Subject: event.ActorID, OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		JTI: event.ID, Revision: event.Revision, WorkloadID: "interaction-gateway",
		CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		EventSHA256:    event.DigestSHA256, TeamID: event.TeamID, ChannelID: event.ChannelID,
		PostID: event.PostID, Generation: signer.config.Generation,
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(signer.config.MaximumTTL).Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(payload, signer.key, internalrpcauth.ProtectedHeaderExpectation{
		Type: credentialType, KeyID: signer.key.KeyID,
	})
	if err != nil {
		return "", errors.New("sign Mattermost event authority")
	}
	return compact, nil
}

func (signer *Signer) CallbackToken(delivery entity.Delivery, actorID string) (string, error) {
	if uuid.Validate(delivery.ID) != nil || uuid.Validate(actorID) != nil ||
		delivery.ChannelID == "" || delivery.OwnerGate == nil {
		return "", errors.New("callback lineage is invalid")
	}
	mac := hmac.New(sha256.New, signer.callbackKey)
	_, _ = mac.Write([]byte(strings.Join([]string{
		delivery.ID, actorID, delivery.ChannelID,
		delivery.OwnerGate.GateID,
		delivery.OwnerGate.ProcessRunID, delivery.SessionID, delivery.TurnID,
	}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (signer *Signer) VerifyCallback(delivery entity.Delivery, actorID, token string) bool {
	expected, err := signer.CallbackToken(delivery, actorID)
	return err == nil && len(token) == len(expected) && hmac.Equal([]byte(token), []byte(expected))
}

func readSafe(path string, maximum int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximum || info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("runtime key file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || int64(len(raw)) > maximum {
		return nil, errors.New("read bounded runtime key")
	}
	return raw, nil
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
