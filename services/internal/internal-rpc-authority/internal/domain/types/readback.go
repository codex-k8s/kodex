package model

import (
	"encoding/json"
	"time"
)

// ReadbackIntent pins expected workload/role/material/source state and public possession-key identity to
// one durable revision, digest, status and expiry; it is resolved by the owning store before a challenge
// is issued.
// ReadbackIntent закрепляет ожидаемые workload/role/material/source состояние и public possession-key
// identity за durable revision, digest, status и expiry; до challenge intent разрешается owning store.
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

// ReadbackCredentialClaims binds signed permission for one exact intent/workload/generation and
// possession key to signer-source provenance and lifetime; payload possession is not equivalent to
// successful credential verification.
// ReadbackCredentialClaims связывает signed разрешение одного exact intent/workload/generation и
// possession key с signer-source происхождением и сроком; наличие payload не равно успешной credential
// проверке.
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

// ReadbackAttestationClaims binds proof of possession and served state to the exact credential, intent,
// workload and one-time challenge nonce/digest; verifier checks these relationships before producing a
// receipt.
// ReadbackAttestationClaims связывает proof of possession и served state с exact credential, intent,
// workload и одноразовыми challenge nonce/digest; verifier проверяет связи до выдачи receipt.
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

// ReadbackChallenge records a server-created challenge with the resolved intent, credential binding,
// semantic idempotency and time limits plus optional consumption time; it is not a reusable generic
// nonce.
// ReadbackChallenge фиксирует созданный сервером challenge с разрешённым intent, credential привязкой,
// semantic idempotency, сроками и optional временем потребления; это не повторно используемый общий
// nonce.
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

// ReadbackReceipt records accepted challenge/evidence identity, semantic binding, verifier generation
// and receipt lifetime with the exact intent snapshot; callers cannot manufacture acceptance by setting
// these fields.
// ReadbackReceipt фиксирует принятые challenge/evidence identity, semantic привязку, verifier generation
// и срок receipt с exact intent snapshot; заполнение полей не создаёт принятие.
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

// ReadbackTrustState pins the served root-to-manifest-to-credential trust chain and forward-only
// predecessor digest with observation time; cryptographic loading and durable activation enforce those
// pins.
// ReadbackTrustState закрепляет served root-to-manifest-to-credential trust цепочку и forward-only
// predecessor digest со временем наблюдения; привязки обеспечиваются cryptographic загрузкой и durable
// активацией.
type ReadbackTrustState struct {
	RootID                       string
	RootFingerprintSHA256        string
	ManifestBundleRevision       uint64
	ManifestBundleDigestSHA256   string
	TrustSourceRevision          uint64
	TrustSetDigestSHA256         string
	TrustKeySetRevision          uint64
	SignerGeneration             uint64
	PredecessorStateDigestSHA256 string
	ServedStateDigestSHA256      string
	ServedAt                     time.Time
}
