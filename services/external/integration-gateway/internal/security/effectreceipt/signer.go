// Package effectreceipt подписывает закрытые provider/Git receipts Issue #236.
package effectreceipt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

const (
	providerType    = "mattercodex-provider-effect-readback-receipt+jws"
	gitType         = "mattercodex-git-reconciliation-receipt+jws"
	maximumKeyBytes = 64 << 10
	workloadID      = "integration-gateway"
	callerSPIFFEID  = "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
)

type (
	Config struct {
		ProviderIssuer, ProviderPrivateJWKFile, GitIssuer, GitPrivateJWKFile string
		MaximumTTL                                                           time.Duration
	}
	Signer struct {
		config              Config
		providerKey, gitKey internalrpcauth.ES256Key
		now                 func() time.Time
	}
	Credential[T any] struct {
		CompactJWS string
		Receipt    T
	}
)

type (
	ProviderReceipt struct {
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
		ProviderObjectRef        string    `json:"provider_object_ref,omitempty"`
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
	GitReceipt struct {
		ContractVersion     uint32    `json:"contract_version"`
		Issuer              string    `json:"iss"`
		Audience            string    `json:"aud"`
		Purpose             string    `json:"purpose"`
		WorkloadID          string    `json:"workload_id"`
		CallerSPIFFEID      string    `json:"caller_spiffe_id"`
		FullMethod          string    `json:"full_method"`
		ActorID             string    `json:"actor_id"`
		OrganizationID      string    `json:"organization_id"`
		ProjectID           string    `json:"project_id"`
		TargetKind          string    `json:"target_kind"`
		TargetResourceID    string    `json:"target_resource_id,omitempty"`
		TargetStableKey     string    `json:"target_stable_key"`
		SourceRef           string    `json:"source_ref"`
		SourceRevision      uint64    `json:"source_revision"`
		SourceSHA256        string    `json:"source_sha256"`
		CommandIntentSHA256 string    `json:"command_intent_sha256"`
		ReceiptID           string    `json:"jti"`
		ReceiptRevision     uint64    `json:"revision"`
		IssuedAt            time.Time `json:"issued_at"`
		NotBefore           time.Time `json:"not_before"`
		ExpiresAt           time.Time `json:"expires_at"`
	}
)

func New(config Config) (*Signer, error) {
	if !strings.HasPrefix(config.ProviderIssuer, "https://") || !strings.HasPrefix(config.GitIssuer, "https://") || !filepath.IsAbs(config.ProviderPrivateJWKFile) || !filepath.IsAbs(config.GitPrivateJWKFile) || config.MaximumTTL < 30*time.Second || config.MaximumTTL > 5*time.Minute {
		return nil, errors.New("effect receipt signer configuration is invalid")
	}
	provider, err := readKey(config.ProviderPrivateJWKFile)
	if err != nil {
		return nil, err
	}
	git, err := readKey(config.GitPrivateJWKFile)
	if err != nil {
		return nil, err
	}
	return &Signer{config: config, providerKey: provider, gitKey: git, now: time.Now}, nil
}

func readKey(path string) (internalrpcauth.ES256Key, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumKeyBytes || info.Mode().Perm()&0o037 != 0 {
		return internalrpcauth.ES256Key{}, errors.New("effect receipt signing key is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return internalrpcauth.ES256Key{}, errors.New("read effect receipt signing key")
	}
	key, err := internalrpcauth.ParsePrivateJWK(raw)
	if err != nil {
		return internalrpcauth.ES256Key{}, errors.New("parse effect receipt signing key")
	}
	return key, nil
}

func (signer *Signer) SignProvider(input ProviderReceipt) (Credential[ProviderReceipt], error) {
	now := signer.now().UTC().Truncate(time.Microsecond)
	input.ContractVersion = 1
	input.Issuer = signer.config.ProviderIssuer
	input.Audience = "urn:mattercodex:provider-readback:ai"
	input.Purpose = "AI_PROVIDER_READBACK_RECEIPT"
	input.WorkloadID = workloadID
	input.CallerSPIFFEID = callerSPIFFEID
	input.IssuedAt, input.NotBefore, input.ExpiresAt = now, now, now.Add(signer.config.MaximumTTL)
	if !validProvider(input) {
		return Credential[ProviderReceipt]{}, errors.New("provider effect receipt input is invalid")
	}
	compact, err := internalrpcauth.SignCanonicalJSON(input, signer.providerKey, internalrpcauth.ProtectedHeaderExpectation{Type: providerType, KeyID: signer.providerKey.KeyID})
	if err != nil {
		return Credential[ProviderReceipt]{}, errors.New("sign provider effect receipt")
	}
	return Credential[ProviderReceipt]{compact, input}, nil
}

func (signer *Signer) SignGit(input GitReceipt) (Credential[GitReceipt], error) {
	now := signer.now().UTC().Truncate(time.Microsecond)
	input.ContractVersion = 1
	input.Issuer = signer.config.GitIssuer
	input.Audience = "urn:mattercodex:git-reconciliation"
	input.Purpose = "GIT_RECONCILIATION_RECEIPT"
	input.WorkloadID = workloadID
	input.CallerSPIFFEID = callerSPIFFEID
	input.IssuedAt, input.NotBefore, input.ExpiresAt = now, now, now.Add(signer.config.MaximumTTL)
	if !validGit(input) {
		return Credential[GitReceipt]{}, errors.New("Git reconciliation receipt input is invalid")
	}
	compact, err := internalrpcauth.SignCanonicalJSON(input, signer.gitKey, internalrpcauth.ProtectedHeaderExpectation{Type: gitType, KeyID: signer.gitKey.KeyID})
	if err != nil {
		return Credential[GitReceipt]{}, errors.New("sign Git reconciliation receipt")
	}
	return Credential[GitReceipt]{compact, input}, nil
}

func validProvider(value ProviderReceipt) bool {
	credentialBound := value.CredentialBindingID != "" || value.CredentialBindingVersion != 0 || value.CredentialBindingSHA256 != ""
	providerRefValid := value.TargetKind != "provider_connection_reference" || value.ProviderObjectRef != ""
	return strings.HasPrefix(value.FullMethod, "/controlplane.v1.ControlPlaneService/") && uuid.Validate(value.ActorID) == nil && uuid.Validate(value.OrganizationID) == nil && uuid.Validate(value.ProjectID) == nil && value.Action != "" && value.Effect != "" && value.EffectVersion > 0 && value.EffectGeneration > 0 && validDigest(value.EffectSHA256) && uuid.Validate(value.ReceiptID) == nil && value.ReceiptRevision > 0 && value.MaskedStatus != "" && value.Provider != "" && value.MaskedLabel != "" && (value.TargetKind == "provider_connection_reference" || value.TargetKind == "provider_pool") && (value.TargetResourceID == "" || uuid.Validate(value.TargetResourceID) == nil) && value.TargetStableKey != "" && validDigest(value.CommandIntentSHA256) && providerRefValid && (!credentialBound || uuid.Validate(value.CredentialBindingID) == nil && value.CredentialBindingVersion > 0 && validDigest(value.CredentialBindingSHA256))
}

func validGit(value GitReceipt) bool {
	return strings.HasPrefix(value.FullMethod, "/controlplane.v1.ControlPlaneService/ReconcileGit") && uuid.Validate(value.ActorID) == nil && uuid.Validate(value.OrganizationID) == nil && uuid.Validate(value.ProjectID) == nil && value.TargetKind != "" && (value.TargetResourceID == "" || uuid.Validate(value.TargetResourceID) == nil) && value.TargetStableKey != "" && len(value.SourceRef) >= 8 && len(value.SourceRef) <= 2048 && value.SourceRevision > 0 && validDigest(value.SourceSHA256) && validDigest(value.CommandIntentSHA256) && uuid.Validate(value.ReceiptID) == nil && value.ReceiptRevision > 0
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
