package model

import (
	"encoding/json"
	"time"
)

// ReadbackIntent закрепляет ожидаемое обслуживаемое состояние за workload.
type ReadbackIntent struct {
	IntentID                string
	Kind                    string
	IntentRevision          uint64
	IntentDigestSHA256      string
	WorkloadID              string
	WorkloadSPIFFEID        string
	Role                    string
	WorkloadGeneration      uint64
	CredentialGeneration    uint64
	MaterialGeneration      uint64
	PossessionKeyID         string
	PossessionKeyGeneration uint64
	PossessionPublicJWK     json.RawMessage
	PossessionKeyThumbprint string
	SourceRevision          uint64
	ServedStateDigestSHA256 string
	Status                  string
	ExpiresAt               time.Time
}

// ReadbackCredentialClaims разрешает конкретную независимую проверку.
type ReadbackCredentialClaims struct {
	Version                  int             `json:"v"`
	Issuer                   string          `json:"iss"`
	Audience                 string          `json:"aud"`
	Subject                  string          `json:"sub"`
	JTI                      string          `json:"jti"`
	Purpose                  string          `json:"purpose"`
	IntentID                 string          `json:"intent_id"`
	IntentKind               string          `json:"intent_kind"`
	IntentRevision           uint64          `json:"intent_revision"`
	IntentDigestSHA256       string          `json:"intent_digest_sha256"`
	WorkloadID               string          `json:"workload_id"`
	WorkloadSPIFFEID         string          `json:"workload_spiffe_id"`
	Role                     string          `json:"role"`
	WorkloadGeneration       uint64          `json:"workload_generation"`
	CredentialGeneration     uint64          `json:"credential_generation"`
	MaterialGeneration       uint64          `json:"material_generation"`
	PossessionKeyID          string          `json:"possession_key_kid"`
	PossessionKeyGeneration  uint64          `json:"possession_key_generation"`
	PossessionPublicJWK      json.RawMessage `json:"possession_public_jwk"`
	PossessionKeyThumbprint  string          `json:"possession_key_thumbprint_sha256"`
	SignerSourceRevision     uint64          `json:"credential_signer_source_revision"`
	SignerSourceDigestSHA256 string          `json:"credential_signer_source_digest_sha256"`
	SignerKeySetRevision     uint64          `json:"credential_signer_key_set_revision"`
	SignerGeneration         uint64          `json:"credential_signer_generation"`
	IssuedAt                 int64           `json:"iat"`
	NotBefore                int64           `json:"nbf"`
	ExpiresAt                int64           `json:"exp"`
}

// ReadbackAttestationClaims доказывает владение ключом и обслуживаемое состояние.
type ReadbackAttestationClaims struct {
	Version                  int    `json:"v"`
	Issuer                   string `json:"iss"`
	Audience                 string `json:"aud"`
	Subject                  string `json:"sub"`
	JTI                      string `json:"jti"`
	Purpose                  string `json:"purpose"`
	IntentID                 string `json:"intent_id"`
	IntentKind               string `json:"intent_kind"`
	IntentRevision           uint64 `json:"intent_revision"`
	WorkloadID               string `json:"workload_id"`
	WorkloadSPIFFEID         string `json:"workload_spiffe_id"`
	Role                     string `json:"role"`
	WorkloadGeneration       uint64 `json:"workload_generation"`
	CredentialGeneration     uint64 `json:"credential_generation"`
	ReadbackCredentialJTI    string `json:"readback_credential_jti"`
	ReadbackCredentialDigest string `json:"readback_credential_digest_sha256"`
	PossessionKeyID          string `json:"possession_key_kid"`
	PossessionKeyGeneration  uint64 `json:"possession_key_generation"`
	PossessionKeyThumbprint  string `json:"possession_key_thumbprint_sha256"`
	SourceRevision           uint64 `json:"source_revision"`
	ServedStateDigestSHA256  string `json:"served_state_digest_sha256"`
	ChallengeID              string `json:"challenge_id"`
	ChallengeJTI             string `json:"challenge_jti"`
	ChallengeNonce           string `json:"challenge_nonce"`
	ChallengeDigestSHA256    string `json:"challenge_digest_sha256"`
	IssuedAt                 int64  `json:"iat"`
	NotBefore                int64  `json:"nbf"`
	ExpiresAt                int64  `json:"exp"`
}

// ReadbackChallenge содержит созданный сервером одноразовый запрос.
type ReadbackChallenge struct {
	ChallengeID              string
	ChallengeJTI             string
	Nonce                    string
	DigestSHA256             string
	Intent                   ReadbackIntent
	ReadbackCredentialJTI    string
	ReadbackCredentialDigest string
	IdempotencyKey           string
	SemanticRequestDigest    string
	IssuedAt                 time.Time
	ExpiresAt                time.Time
	ConsumedAt               *time.Time
}

// ReadbackReceipt неизменяемо фиксирует принятую независимую проверку.
type ReadbackReceipt struct {
	ReceiptID             string
	ChallengeID           string
	EvidenceJTI           string
	EvidenceDigestSHA256  string
	SemanticRequestDigest string
	VerifierGeneration    uint64
	AcceptedAt            time.Time
	ExpiresAt             time.Time
	Intent                ReadbackIntent
}
