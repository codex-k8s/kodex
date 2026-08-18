package resource

import (
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	mattermostProviderReceiptPurpose = "MATTERMOST_PROVIDER_READBACK_RECEIPT"
	aiProviderReceiptPurpose         = "AI_PROVIDER_READBACK_RECEIPT"
)

// mattermostProviderReceiptClaims повторяет точный signed payload
// interaction-gateway. Общий transport-тип дополнительно содержит поля AI
// provider, поэтому его нельзя использовать для digest Mattermost receipt.
type mattermostProviderReceiptClaims struct {
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

func providerReceiptAuthorityDigest(receipt value.ProviderEffectReceipt) (string, error) {
	switch receipt.Purpose {
	case mattermostProviderReceiptPurpose:
		return internalrpcauth.CanonicalJSONSHA256(mattermostProviderReceiptClaims{
			ContractVersion: receipt.ContractVersion, Issuer: receipt.Issuer,
			Audience: receipt.Audience, Purpose: receipt.Purpose, WorkloadID: receipt.WorkloadID,
			CallerSPIFFEID: receipt.CallerSPIFFEID, FullMethod: receipt.FullMethod,
			ActorID: receipt.ActorID, OrganizationID: receipt.OrganizationID, ProjectID: receipt.ProjectID,
			WorkspaceID: receipt.WorkspaceID, ProviderTeamRef: receipt.ProviderTeamRef,
			ProviderObjectRef: receipt.ProviderObjectRef, Action: receipt.Action, Effect: receipt.Effect,
			EffectVersion: receipt.EffectVersion, EffectGeneration: receipt.EffectGeneration,
			EffectSHA256: receipt.EffectSHA256, ReceiptID: receipt.ReceiptID,
			ReceiptRevision: receipt.ReceiptRevision, IssuedAt: receipt.IssuedAt,
			NotBefore: receipt.NotBefore, ExpiresAt: receipt.ExpiresAt,
			CredentialBindingID:      receipt.CredentialBindingID,
			CredentialBindingVersion: receipt.CredentialBindingVersion,
			CredentialBindingSHA256:  receipt.CredentialBindingSHA256,
			ProviderUsername:         receipt.ProviderUsername, MaskedStatus: receipt.MaskedStatus,
			Provider: receipt.Provider, MaskedLabel: receipt.MaskedLabel,
			Capabilities: append([]string(nil), receipt.Capabilities...), Eligible: receipt.Eligible,
			TargetKind: receipt.TargetKind, TargetResourceID: receipt.TargetResourceID,
			TargetStableKey: receipt.TargetStableKey, CommandIntentSHA256: receipt.CommandIntentSHA256,
		})
	case aiProviderReceiptPurpose:
		return internalrpcauth.CanonicalJSONSHA256(receipt)
	default:
		return "", errors.New("provider receipt purpose is unsupported")
	}
}
