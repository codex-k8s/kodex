package entity

import (
	"encoding/json"
	"time"
)

type ProviderCapability struct {
	Name             string `json:"name"`
	Risk             string `json:"risk"`
	RequiresApproval bool   `json:"requires_approval"`
}

type ProviderDescriptor struct {
	ID                 string               `json:"provider_id"`
	Version            uint64               `json:"version"`
	Digest             string               `json:"digest_sha256"`
	DisplayName        string               `json:"display_name"`
	AuthorizationModes []string             `json:"authorization_modes"`
	Capabilities       []ProviderCapability `json:"capabilities"`
}

// ManagedProviderConnection — целевой owner aggregate Issue #236. Он не
// синхронизируется с legacy bot-service до one-shot cutover #196.
type ManagedProviderConnection struct {
	ID                       string     `json:"connection_id"`
	StableKey                string     `json:"stable_key"`
	ProviderID               string     `json:"provider_id"`
	DisplayName              string     `json:"display_name"`
	Version                  uint64     `json:"version"`
	Generation               uint64     `json:"generation"`
	RevokeGeneration         uint64     `json:"revoke_generation"`
	Status                   string     `json:"status"`
	ActiveCredential         uint64     `json:"active_credential_generation"`
	MaskedLabel              string     `json:"masked_label,omitempty"`
	MaskedAccount            string     `json:"masked_account,omitempty"`
	Capabilities             []string   `json:"capabilities"`
	CapabilityDigest         string     `json:"capability_sha256"`
	CredentialBindingID      string     `json:"credential_binding_id,omitempty"`
	CredentialBindingVersion uint64     `json:"credential_binding_version,omitempty"`
	CredentialBindingDigest  string     `json:"credential_binding_sha256,omitempty"`
	ObservationDigest        string     `json:"observation_sha256"`
	ObservedAt               *time.Time `json:"observed_at,omitempty"`
	ControlPlaneID           string     `json:"control_plane_resource_id,omitempty"`
	ControlPlaneVersion      uint64     `json:"control_plane_version,omitempty"`
	ControlPlaneDigest       string     `json:"control_plane_sha256,omitempty"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type ProviderAuthorization struct {
	ID                    string     `json:"authorization_id"`
	ProviderID            string     `json:"provider_id"`
	ConnectionID          string     `json:"connection_id"`
	Attempt               uint64     `json:"attempt"`
	Version               uint64     `json:"version"`
	Generation            uint64     `json:"generation"`
	State                 string     `json:"state"`
	IntentDigest          string     `json:"intent_sha256"`
	VerificationURL       string     `json:"-"`
	UserCode              string     `json:"-"`
	CodeExpiresAt         *time.Time `json:"code_expires_at,omitempty"`
	ExpiresAt             time.Time  `json:"expires_at"`
	FailureCategory       string     `json:"failure_category,omitempty"`
	EncryptedDeviceResult []byte     `json:"-"`
	LeaseID               string     `json:"-"`
	LeaseGeneration       uint64     `json:"-"`
	LeaseExpiresAt        *time.Time `json:"-"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

type CredentialGeneration struct {
	ConnectionID             string
	Generation               uint64
	AuthorizationID          string
	Status                   string
	SecretRef                string
	SecretVersion            uint64
	SecretContentDigest      string
	CredentialBindingID      string
	CredentialBindingVersion uint64
	CredentialBindingDigest  string
	MaskedAccount            string
	MaskedLabel              string
}

type ProviderPoolMember struct {
	ConnectionID         string `json:"connection_id"`
	ConnectionStableKey  string `json:"connection_stable_key"`
	ConnectionVersion    uint64 `json:"connection_version"`
	ConnectionGeneration uint64 `json:"connection_generation"`
	ObservationDigest    string `json:"observation_sha256"`
	Weight               uint32 `json:"weight"`
	Eligible             bool   `json:"eligible"`
	ControlPlaneID       string `json:"control_plane_resource_id,omitempty"`
	ControlPlaneVersion  uint64 `json:"control_plane_version,omitempty"`
	ControlPlaneDigest   string `json:"control_plane_sha256,omitempty"`
}

type ManagedProviderPool struct {
	ID                  string               `json:"provider_pool_id"`
	StableKey           string               `json:"stable_key"`
	DisplayName         string               `json:"display_name"`
	Policy              string               `json:"policy"`
	Version             uint64               `json:"version"`
	DesiredDigest       string               `json:"desired_sha256"`
	ObservationVersion  uint64               `json:"observation_version"`
	ObservationDigest   string               `json:"observation_sha256"`
	EffectiveDigest     string               `json:"effective_sha256"`
	Status              string               `json:"status"`
	Members             []ProviderPoolMember `json:"members"`
	ControlPlaneID      string               `json:"control_plane_resource_id,omitempty"`
	ControlPlaneVersion uint64               `json:"control_plane_version,omitempty"`
	ControlPlaneDigest  string               `json:"control_plane_sha256,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

type IntegrationConfiguration struct {
	ID                   string    `json:"configuration_id"`
	StableKey            string    `json:"stable_key"`
	Version              uint64    `json:"version"`
	Digest               string    `json:"configuration_sha256"`
	DefinitionID         string    `json:"definition_id"`
	DefinitionVersion    uint64    `json:"definition_version"`
	DefinitionDigest     string    `json:"definition_sha256"`
	ConnectionID         string    `json:"connection_id"`
	ConnectionVersion    uint64    `json:"connection_version"`
	ConnectionGeneration uint64    `json:"connection_generation"`
	Capabilities         []string  `json:"capabilities"`
	CapabilityDigest     string    `json:"capability_sha256"`
	EffectKind           string    `json:"effect_kind"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type IntegrationTestReceipt struct {
	ID                   string
	ConnectionID         string
	ConnectionVersion    uint64
	ConnectionGeneration uint64
	DefinitionID         string
	DefinitionVersion    uint64
	Category             string
	Digest               string
	ExpiresAt            time.Time
	TestedAt             *time.Time
}

// GitSource содержит только server-owned ссылки из закрытого реестра. Значения
// request payload никогда не используются как repository authority.
type GitSource struct {
	RepositoryKey               string
	RefKey                      string
	PathKey                     string
	RepositoryConnectionID      string
	RepositoryConnectionVersion uint64
	RepositoryConnectionDigest  string
	CredentialBindingID         string
	CredentialBindingVersion    uint64
	CredentialBindingDigest     string
}

type GitSourceBinding struct {
	ID                          string     `json:"binding_id"`
	StableKey                   string     `json:"stable_key"`
	Version                     uint64     `json:"version"`
	Status                      string     `json:"status"`
	RepositoryKey               string     `json:"repository_key"`
	RefKey                      string     `json:"ref_key"`
	PathKey                     string     `json:"path_key"`
	RepositoryConnectionID      string     `json:"repository_connection_id"`
	RepositoryConnectionVersion uint64     `json:"repository_connection_version"`
	RepositoryConnectionDigest  string     `json:"repository_connection_sha256"`
	CredentialBindingID         string     `json:"credential_binding_id"`
	CredentialBindingVersion    uint64     `json:"credential_binding_version"`
	CredentialBindingDigest     string     `json:"credential_binding_sha256"`
	TargetKind                  string     `json:"target_kind"`
	TargetStableKey             string     `json:"target_stable_key"`
	FetchedCommit               string     `json:"fetched_commit,omitempty"`
	SourceRevision              uint64     `json:"source_revision,omitempty"`
	SourceDigest                string     `json:"source_sha256,omitempty"`
	FetchedAt                   *time.Time `json:"fetched_at,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

type GitReconciliation struct {
	ID                  string
	BindingID           string
	BindingVersion      uint64
	State               string
	FetchedCommit       string
	SourceRevision      uint64
	SourceDigest        string
	EncryptedSnapshot   []byte
	TargetResourceID    string
	TargetVersion       uint64
	TargetDigest        string
	CommandIntentDigest string
	ReceiptID           string
	ReceiptDigest       string
	FailureCategory     string
	UpdatedAt           time.Time
}

type ManagementEffect struct {
	ID                 string
	Kind               string
	ResourceKind       string
	ResourceID         string
	ResourceVersion    uint64
	ResourceGeneration uint64
	IntentDigest       string
	Status             string
	LeaseID            string
	LeaseFence         uint64
	LeaseExpiresAt     *time.Time
	Attempts           uint32
	Payload            json.RawMessage
}
