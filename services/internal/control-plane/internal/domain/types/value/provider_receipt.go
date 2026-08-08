package value

import (
	"errors"
	"strings"
	"time"
)

// ProviderEffectReceipt — canonical payload проверенного provider JWS. Это не
// provider payload и не secret: только refs, версии, digest и masked status.
type ProviderEffectReceipt struct {
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
	SecretRef                string    `json:"secret_ref,omitempty"`
	SecretVersion            uint64    `json:"secret_version,omitempty"`
	SecretContentSHA256      string    `json:"secret_content_sha256,omitempty"`
	MaskedAccount            string    `json:"masked_account,omitempty"`
	ObservedUsage            uint64    `json:"observed_usage,omitempty"`
	ObservedLimit            uint64    `json:"observed_limit,omitempty"`
	ObservationRevision      uint64    `json:"observation_revision,omitempty"`
	ObservedAt               time.Time `json:"observed_at,omitempty"`
	WindowDurationSeconds    uint64    `json:"window_duration_seconds,omitempty"`
	ResetsAt                 time.Time `json:"resets_at,omitempty"`
	ObservationExpiresAt     time.Time `json:"observation_expires_at,omitempty"`
	ObservationSHA256        string    `json:"observation_sha256,omitempty"`
}

func (receipt ProviderEffectReceipt) Validate(now time.Time) error {
	if receipt.ContractVersion != 1 || receipt.Issuer == "" || receipt.Audience == "" || receipt.Purpose == "" ||
		ValidateStableKey(receipt.WorkloadID) != nil || receipt.CallerSPIFFEID == "" ||
		!strings.HasPrefix(receipt.FullMethod, "/controlplane.v1.ControlPlaneService/") ||
		ValidateID(receipt.ActorID) != nil || ValidateID(receipt.OrganizationID) != nil ||
		ValidateID(receipt.ProjectID) != nil || (receipt.WorkspaceID != "" && ValidateID(receipt.WorkspaceID) != nil) ||
		ValidateStableKey(receipt.Action) != nil || ValidateStableKey(receipt.Effect) != nil ||
		receipt.EffectVersion == 0 || receipt.EffectGeneration == 0 || !validDigestValue(receipt.EffectSHA256) ||
		ValidateID(receipt.ReceiptID) != nil || receipt.ReceiptRevision == 0 ||
		receipt.IssuedAt.IsZero() || receipt.NotBefore.IsZero() || receipt.ExpiresAt.IsZero() ||
		receipt.NotBefore.Before(receipt.IssuedAt.Add(-5*time.Second)) ||
		receipt.NotBefore.After(receipt.IssuedAt.Add(5*time.Second)) || receipt.IssuedAt.After(now.Add(5*time.Second)) ||
		!receipt.ExpiresAt.After(receipt.NotBefore) || receipt.ExpiresAt.Sub(receipt.IssuedAt) > 5*time.Minute ||
		now.Before(receipt.NotBefore.Add(-5*time.Second)) || !now.Before(receipt.ExpiresAt.Add(5*time.Second)) ||
		len(receipt.MaskedStatus) == 0 || len(receipt.MaskedStatus) > 64 {
		return errors.New("provider effect receipt is invalid")
	}
	if receipt.Provider != "" && ValidateStableKey(receipt.Provider) != nil || len(receipt.MaskedLabel) > 256 || len(receipt.Capabilities) > 64 {
		return errors.New("provider receipt metadata is invalid")
	}
	if ValidateStableKey(receipt.TargetKind) != nil ||
		(receipt.TargetResourceID != "" && ValidateID(receipt.TargetResourceID) != nil) ||
		ValidateStableKey(receipt.TargetStableKey) != nil || !validDigestValue(receipt.CommandIntentSHA256) {
		return errors.New("provider receipt target binding is invalid")
	}
	seen := make(map[string]struct{}, len(receipt.Capabilities))
	for _, capability := range receipt.Capabilities {
		if ValidateStableKey(capability) != nil {
			return errors.New("provider receipt capability is invalid")
		}
		if _, exists := seen[capability]; exists {
			return errors.New("provider receipt capability is duplicated")
		}
		seen[capability] = struct{}{}
	}
	credentialBound := receipt.CredentialBindingID != "" || receipt.CredentialBindingVersion != 0 || receipt.CredentialBindingSHA256 != ""
	if credentialBound && (ValidateID(receipt.CredentialBindingID) != nil || receipt.CredentialBindingVersion == 0 ||
		!validDigestValue(receipt.CredentialBindingSHA256)) {
		return errors.New("provider credential receipt binding is invalid")
	}
	if receipt.TargetKind == "provider_connection_reference" && receipt.Action != "archive" &&
		(receipt.SecretRef == "" || receipt.SecretVersion == 0 || !validDigestValue(receipt.SecretContentSHA256) ||
			receipt.MaskedAccount == "" || receipt.ObservedLimit == 0 || receipt.ObservedUsage > receipt.ObservedLimit ||
			receipt.ObservationRevision == 0 || receipt.ObservedAt.IsZero() || receipt.WindowDurationSeconds == 0 ||
			receipt.ResetsAt.IsZero() || !receipt.ObservationExpiresAt.After(receipt.ObservedAt) || !validDigestValue(receipt.ObservationSHA256)) {
		return errors.New("provider credential materialization receipt is invalid")
	}
	return nil
}

func validDigestValue(value string) bool {
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
