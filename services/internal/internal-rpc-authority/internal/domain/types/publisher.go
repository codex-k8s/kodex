package model

import (
	"encoding/json"
	"sort"
	"time"
)

// DeliveryTarget задаёт один зарегистрированный workload и его пути доставки.
type DeliveryTarget struct {
	TargetID                   string
	WorkloadID                 string
	WorkloadSPIFFEID           string
	Role                       string
	Namespace                  string
	ServiceAccount             string
	WorkloadGeneration         uint64
	StartupReadbackRequired    bool
	CredentialGeneration       uint64
	AuthoritySnapshotSecret    string
	AuthoritySnapshotMountPath string
	AuthPrivateKeySecret       string
	AuthPrivateKeyMountPath    string
	ManifestTrustSecret        string
	ManifestTrustMountPath     string
	ProofTrustSecret           string
	ProofTrustMountPath        string
	ProofPrivateKeySecret      string
	ProofPrivateKeyMountPath   string
	DatabaseLoginPrincipal     string
	DatabaseCredentialSecret   string
	DatabaseDSNMountPath       string
	RestoreCredentialID        string
	RestoreCredentialPath      string
	RestoreCredentialMountPath string
	RestoreACKKeyID            string
	RestoreACKKeyGeneration    uint64
	RestoreACKKeyPath          string
	RestoreACKKeyMountPath     string
	RestoreControllerAddress   string
	RestoreControllerSNI       string
	RestoreControllerCAPath    string
	RestoreControllerAudience  string
	RestoreControllerMethod    string
	RestoreNetworkPolicy       string
	ReadbackID                 string
	ReadbackCredentialPath     string
	ReadbackCredentialMount    string
	ReadbackCredentialID       string
	ReadbackPossessionKeyPath  string
	ReadbackPossessionKeyMount string
	ReadbackPossessionKeyID    string
	ReadbackIntentRevision     uint64
	ReadbackMaterialGeneration uint64
	ReadbackSourceRevision     uint64
	ReadbackServedStateDigest  string
	ReadbackAttestorAddress    string
	ReadbackAttestorSNI        string
	ReadbackAttestorCAPath     string
	ReadbackAttestorAudience   string
	ReadbackChallengeMethod    string
	ReadbackAttestationMethod  string
	ReadbackNetworkPolicy      string
}

// DeliveryTargetRegistry содержит versioned закрытый набор целей доставки.
type DeliveryTargetRegistry struct {
	Version        int
	SourceRevision uint64
	SourceDigest   string
	Targets        map[string]DeliveryTarget
}

// SnapshotReadbackTarget задаёт точную обязательную цель startup readback.
type SnapshotReadbackTarget struct {
	WorkloadID         string
	Role               string
	WorkloadGeneration uint64
}

// StartupReadbackTargets возвращает детерминированный обязательный набор.
func (registry DeliveryTargetRegistry) StartupReadbackTargets() []SnapshotReadbackTarget {
	targets := make([]SnapshotReadbackTarget, 0, len(registry.Targets))
	for _, target := range registry.Targets {
		if !target.StartupReadbackRequired {
			continue
		}
		targets = append(targets, SnapshotReadbackTarget{
			WorkloadID:         target.WorkloadID,
			Role:               target.Role,
			WorkloadGeneration: target.WorkloadGeneration,
		})
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].WorkloadID != targets[right].WorkloadID {
			return targets[left].WorkloadID < targets[right].WorkloadID
		}
		if targets[left].Role != targets[right].Role {
			return targets[left].Role < targets[right].Role
		}
		return targets[left].WorkloadGeneration < targets[right].WorkloadGeneration
	})
	return targets
}

// StartupReadbackTargetCount возвращает число долгоживущих workload,
// подтверждение которых обязательно до первой активации authority snapshot.
func (registry DeliveryTargetRegistry) StartupReadbackTargetCount() int {
	return len(registry.StartupReadbackTargets())
}

// CredentialIssuanceDirective связывает выпуск с restore coordination.
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

// RestoreRoleCredentialClaims задаёт role-bound credential восстановления.
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

// CredentialDeliveryReceiptClaims подтверждает точную доставку credential.
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
	SecretGeneration           uint64 `json:"secret_generation"`
	DeliveryReadbackDigest     string `json:"delivery_readback_digest_sha256"`
	IssuedAt                   int64  `json:"iat"`
	NotBefore                  int64  `json:"nbf"`
	ExpiresAt                  int64  `json:"exp"`
}

// PublishedCredential хранит устойчивый результат идемпотентного выпуска.
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

// PublishedReadbackMaterial содержит credential и possession key проверки.
type PublishedReadbackMaterial struct {
	Intent                     ReadbackIntent
	ReadbackCredentialJWS      string
	PossessionPrivateJWK       string
	CredentialSecretGeneration uint64
	PossessionSecretGeneration uint64
}

// AuthoritySnapshotPublication описывает атомарно опубликованный полный граф.
type AuthoritySnapshotPublication struct {
	IntentID                string
	InputDigestSHA256       string
	SourceRevision          uint64
	SourceDigestSHA256      string
	KeySetRevision          uint64
	PolicyRevision          uint64
	SignerGeneration        uint64
	PredecessorRevision     uint64
	PredecessorDigestSHA256 string
	SnapshotCompactJWS      string
	SnapshotResourceVersion string
	PublishedAt             time.Time
}

// AuthoritySnapshotHistory содержит ограниченную forward-only цепочку.
type AuthoritySnapshotHistory struct {
	Current []RevisionDigest
}
