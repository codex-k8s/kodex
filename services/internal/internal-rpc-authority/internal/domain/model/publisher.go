package model

import (
	"encoding/json"
	"time"
)

type DeliveryTarget struct {
	TargetID              string
	WorkloadID            string
	WorkloadSPIFFEID      string
	Role                  string
	WorkloadGeneration    uint64
	CredentialGeneration  uint64
	RestoreCredentialPath string
	RestoreACKKeyPath     string
}

type DeliveryTargetRegistry struct {
	Version        int
	SourceRevision uint64
	SourceDigest   string
	Targets        map[string]DeliveryTarget
}

type CredentialIssuanceDirective struct {
	Version                int    `json:"v"`
	Issuer                 string `json:"iss"`
	Audience               string `json:"aud"`
	Subject                string `json:"sub"`
	JTI                    string `json:"jti"`
	RestoreID              string `json:"restore_id"`
	RestoreEpoch           uint64 `json:"restore_epoch"`
	CoordinationRevision   uint64 `json:"coordination_revision"`
	TargetRegistryRevision uint64 `json:"target_registry_revision"`
	TargetRegistryDigest   string `json:"target_registry_digest_sha256"`
	DeliveryTargetID       string `json:"delivery_target_id"`
	WorkloadID             string `json:"workload_id"`
	WorkloadSPIFFEID       string `json:"workload_spiffe_id"`
	Role                   string `json:"role"`
	WorkloadGeneration     uint64 `json:"workload_generation"`
	CredentialGeneration   uint64 `json:"credential_generation"`
	ACKKeyGeneration       uint64 `json:"ack_key_generation"`
	IssuedAt               int64  `json:"iat"`
	NotBefore              int64  `json:"nbf"`
	ExpiresAt              int64  `json:"exp"`
}

type RestoreRoleCredentialClaims struct {
	Version                  int             `json:"v"`
	Issuer                   string          `json:"iss"`
	Audience                 string          `json:"aud"`
	Subject                  string          `json:"sub"`
	JTI                      string          `json:"jti"`
	WorkloadID               string          `json:"workload_id"`
	WorkloadSPIFFEID         string          `json:"workload_spiffe_id"`
	Role                     string          `json:"role"`
	WorkloadGeneration       uint64          `json:"workload_generation"`
	CredentialGeneration     uint64          `json:"credential_generation"`
	RestoreID                string          `json:"restore_id"`
	RestoreEpoch             uint64          `json:"restore_epoch"`
	CoordinationRevision     uint64          `json:"coordination_revision"`
	SignerSourceRevision     uint64          `json:"credential_signer_source_revision"`
	SignerSourceDigestSHA256 string          `json:"credential_signer_source_digest_sha256"`
	SignerKeySetRevision     uint64          `json:"credential_signer_key_set_revision"`
	SignerGeneration         uint64          `json:"credential_signer_generation"`
	SignerKeyID              string          `json:"credential_signer_kid"`
	ACKKeyID                 string          `json:"ack_key_kid"`
	ACKKeyGeneration         uint64          `json:"ack_key_generation"`
	ACKPublicJWK             json.RawMessage `json:"ack_public_jwk"`
	ACKKeyThumbprintSHA256   string          `json:"ack_key_thumbprint_sha256"`
	IssuedAt                 int64           `json:"iat"`
	NotBefore                int64           `json:"nbf"`
	ExpiresAt                int64           `json:"exp"`
}

type CredentialDeliveryReceiptClaims struct {
	Version                    int    `json:"v"`
	Issuer                     string `json:"iss"`
	Audience                   string `json:"aud"`
	Subject                    string `json:"sub"`
	JTI                        string `json:"jti"`
	IssuanceDirectiveJTI       string `json:"issuance_directive_jti"`
	RestoreID                  string `json:"restore_id"`
	RestoreEpoch               uint64 `json:"restore_epoch"`
	CoordinationRevision       uint64 `json:"coordination_revision"`
	DeliveryTargetID           string `json:"delivery_target_id"`
	WorkloadID                 string `json:"workload_id"`
	Role                       string `json:"role"`
	WorkloadGeneration         uint64 `json:"workload_generation"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	ACKKeyID                   string `json:"ack_key_kid"`
	ACKKeyGeneration           uint64 `json:"ack_key_generation"`
	ACKKeyThumbprintSHA256     string `json:"ack_key_thumbprint_sha256"`
	VaultMetadataVersion       uint64 `json:"vault_metadata_version"`
	DeliveryReadbackDigest     string `json:"delivery_readback_digest_sha256"`
	IssuedAt                   int64  `json:"iat"`
	NotBefore                  int64  `json:"nbf"`
	ExpiresAt                  int64  `json:"exp"`
}

type PublishedCredential struct {
	IdempotencyKey       string
	DirectiveJTI         string
	DirectiveDigest      string
	DeliveryReceiptJWS   string
	RoleCredentialDigest string
	CredentialGeneration uint64
	ACKKeyGeneration     uint64
	AcceptedAt           time.Time
}
