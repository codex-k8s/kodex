// Package providerreceipt выпускает exact Mattermost provider readback receipt.
package providerreceipt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
	"github.com/google/uuid"
)

const (
	credentialType  = "mattercodex-provider-effect-readback-receipt+jws"
	maximumKeyBytes = 64 << 10
	Purpose         = "MATTERMOST_PROVIDER_READBACK_RECEIPT"
	Audience        = "urn:mattercodex:provider-readback:mattermost"
	WorkloadID      = "interaction-gateway"
	CallerSPIFFEID  = "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway"
)

type Config struct {
	Issuer         string
	PrivateJWKFile string
	MaximumTTL     time.Duration
}

type Signer struct {
	config Config
	key    internalrpcauth.ES256Key
	now    func() time.Time
}

type claims struct {
	ContractVersion          uint32    `json:"contract_version"`
	Issuer                   string    `json:"iss"`
	Audience                 string    `json:"aud"`
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
	TargetKind               string    `json:"target_kind"`
	TargetResourceID         string    `json:"target_resource_id,omitempty"`
	TargetStableKey          string    `json:"target_stable_key"`
	CommandIntentSHA256      string    `json:"command_intent_sha256"`
}

func New(config Config) (*Signer, error) {
	if config.Issuer == "" || !strings.HasPrefix(config.Issuer, "https://") ||
		!filepath.IsAbs(config.PrivateJWKFile) || config.MaximumTTL < 30*time.Second ||
		config.MaximumTTL > 5*time.Minute {
		return nil, errors.New("provider receipt signer configuration is invalid")
	}
	info, err := os.Stat(config.PrivateJWKFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumKeyBytes ||
		info.Mode().Perm()&0o037 != 0 {
		return nil, errors.New("provider receipt signing key is unsafe")
	}
	raw, err := os.ReadFile(config.PrivateJWKFile)
	if err != nil || int64(len(raw)) > maximumKeyBytes {
		return nil, errors.New("read provider receipt signing key")
	}
	key, err := internalrpcauth.ParsePrivateJWK(raw)
	if err != nil {
		return nil, errors.New("parse provider receipt signing key")
	}
	return &Signer{config: config, key: key, now: time.Now}, nil
}

func (signer *Signer) Sign(input domaincontrol.ProviderEffectReceipt) (domaincontrol.ProviderCredential, error) {
	now := signer.now().UTC().Truncate(time.Microsecond)
	input.ContractVersion = 1
	input.Issuer = signer.config.Issuer
	input.Audience = Audience
	input.Purpose = Purpose
	input.WorkloadID = WorkloadID
	input.CallerSPIFFEID = CallerSPIFFEID
	input.IssuedAt, input.NotBefore, input.ExpiresAt = now, now, now.Add(signer.config.MaximumTTL)
	if !validReceipt(input) {
		return domaincontrol.ProviderCredential{}, errors.New("provider receipt input is invalid")
	}
	payload := claims{
		ContractVersion: input.ContractVersion, Issuer: input.Issuer, Audience: input.Audience, Purpose: input.Purpose,
		WorkloadID: input.WorkloadID, CallerSPIFFEID: input.CallerSPIFFEID, FullMethod: input.FullMethod,
		ActorID: input.ActorID, OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		WorkspaceID: input.WorkspaceID, ProviderTeamRef: input.ProviderTeamRef,
		ProviderObjectRef: input.ProviderObjectRef, Action: input.Action, Effect: input.Effect,
		EffectVersion: input.EffectVersion, EffectGeneration: input.EffectGeneration,
		EffectSHA256: input.EffectSHA256, ReceiptID: input.ReceiptID,
		ReceiptRevision: input.ReceiptRevision, IssuedAt: input.IssuedAt, NotBefore: input.NotBefore,
		ExpiresAt:                input.ExpiresAt,
		CredentialBindingID:      input.CredentialBindingID,
		CredentialBindingVersion: input.CredentialBindingVersion,
		CredentialBindingSHA256:  input.CredentialBindingSHA256,
		ProviderUsername:         input.ProviderUsername, MaskedStatus: input.MaskedStatus,
		Provider: input.Provider, MaskedLabel: input.MaskedLabel,
		Capabilities: append([]string(nil), input.Capabilities...), Eligible: input.Eligible,
		TargetKind: input.TargetKind, TargetResourceID: input.TargetResourceID,
		TargetStableKey: input.TargetStableKey, CommandIntentSHA256: input.CommandIntentSHA256,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(payload, signer.key, internalrpcauth.ProtectedHeaderExpectation{
		Type: credentialType, KeyID: signer.key.KeyID,
	})
	if err != nil {
		return domaincontrol.ProviderCredential{}, errors.New("sign provider effect receipt")
	}
	return domaincontrol.ProviderCredential{CompactJWS: compact, Receipt: input}, nil
}

func validReceipt(input domaincontrol.ProviderEffectReceipt) bool {
	targetValid := input.TargetKind == "workspace_mattermost_mapping" &&
		input.TargetStableKey == "workspace-"+strings.ReplaceAll(input.WorkspaceID, "-", "")
	if input.TargetKind == "agent_bot_identity" {
		targetValid = validAgentReceipt(input)
	}
	return uuid.Validate(input.ActorID) == nil && uuid.Validate(input.OrganizationID) == nil &&
		uuid.Validate(input.ProjectID) == nil && uuid.Validate(input.WorkspaceID) == nil &&
		input.ProjectID == input.WorkspaceID && input.Audience == Audience && strings.HasPrefix(input.FullMethod, "/controlplane.v1.ControlPlaneService/") &&
		input.Action != "" && input.Effect != "" && input.EffectVersion > 0 && input.EffectGeneration > 0 &&
		validDigest(input.EffectSHA256) && uuid.Validate(input.ReceiptID) == nil && input.ReceiptRevision > 0 &&
		input.MaskedStatus != "" && len(input.MaskedStatus) <= 64 && targetValid &&
		(input.TargetResourceID == "" || uuid.Validate(input.TargetResourceID) == nil) &&
		validDigest(input.CommandIntentSHA256)
}

func validAgentReceipt(input domaincontrol.ProviderEffectReceipt) bool {
	if uuid.Validate(input.TargetResourceID) != nil || input.TargetStableKey == "" ||
		len(input.TargetStableKey) > 128 || strings.ContainsAny(input.TargetStableKey, "\x00\r\n") ||
		input.ProviderObjectRef == "" || input.ProviderTeamRef == "" || input.ProviderUsername == "" ||
		input.ReceiptRevision != input.EffectGeneration {
		return false
	}
	if input.FullMethod == "/controlplane.v1.ControlPlaneService/GetAgent" {
		return input.Action == "read" && input.Effect == "agent_bot_identity_source_readback" &&
			input.ProviderObjectRef == input.ProviderTeamRef && input.ProviderUsername == "source-readback" &&
			input.TargetStableKey == "agent-source-"+strings.ReplaceAll(input.TargetResourceID, "-", "") &&
			input.CredentialBindingID == "" && input.CredentialBindingVersion == 0 &&
			input.CredentialBindingSHA256 == "" && len(input.Capabilities) == 0
	}
	if input.FullMethod != "/controlplane.v1.ControlPlaneService/ManageAgentMattermostBotIdentity" ||
		(input.Action != "bind" && input.Action != "rebind" && input.Action != "revoke") ||
		input.Effect != "agent_bot_identity" || uuid.Validate(input.ProviderObjectRef) != nil ||
		uuid.Validate(input.CredentialBindingID) != nil || input.CredentialBindingVersion == 0 ||
		!validDigest(input.CredentialBindingSHA256) || input.Provider != "mattermost" ||
		input.MaskedLabel == "" || len(input.MaskedLabel) > 64 || len(input.Capabilities) != 2 {
		return false
	}
	return input.Capabilities[0] == "mattermost.post" && input.Capabilities[1] == "mattermost.readback"
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
