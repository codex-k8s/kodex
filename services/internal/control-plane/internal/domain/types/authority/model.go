// Package authority содержит нейтральные запросы между адаптером OIDC и
// сервисом доказательств полномочий.
package authority

import "time"

// ApplicationIdentity — результат точных mTLS- и OIDC-проверок, а не поле запроса.
type ApplicationIdentity struct {
	ActorID                     string
	OrganizationID              string
	ProjectID                   string
	SessionJTI                  string
	SessionRevision             uint64
	SubjectDigest               string
	CredentialDigest            string
	TenantOwner                 bool
	CallerWorkload              string
	CallerSPIFFEID              string
	BoundSessionID              string
	BoundTurnID                 string
	BoundAttempt                uint32
	BoundInputSHA256            string
	BoundGeneration             uint64
	BoundRuntimeRevisionID      string
	BoundRuntimeRevisionVersion uint64
	BoundRuntimeRevisionSHA256  string
	BoundContinuationID         string
	BoundContinuationVersion    uint64
	BoundContinuationFence      uint64
	BoundInvocationID           string
	AllowedOperationIDs         []string
}

type Provenance struct {
	Source       string `json:"source"`
	Reference    string `json:"reference"`
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

type Identity struct {
	ID         string     `json:"id"`
	Provenance Provenance `json:"provenance"`
}

type Workload struct {
	WorkloadID string `json:"workload_id"`
	SPIFFEID   string `json:"spiffe_id"`
}

type Authority struct {
	ActorKind string    `json:"actor_kind"`
	Actor     Identity  `json:"actor"`
	Tenant    Identity  `json:"tenant"`
	Project   *Identity `json:"project,omitempty"`
}

// ProofClaims соответствует authority-proof.schema.json v1.
type ProofClaims struct {
	Version                      int       `json:"v"`
	Issuer                       string    `json:"iss"`
	Audience                     string    `json:"aud"`
	Caller                       Workload  `json:"caller"`
	OperationID                  string    `json:"operation_id"`
	AuthorizationContextAudience string    `json:"authorization_context_audience"`
	Authority                    Authority `json:"authority"`
	ProofRevision                uint64    `json:"proof_revision"`
	SignerGeneration             uint64    `json:"signer_generation"`
	JTI                          string    `json:"jti"`
	IssuedAt                     int64     `json:"iat"`
	NotBefore                    int64     `json:"nbf"`
	ExpiresAt                    int64     `json:"exp"`
}

type Proof struct {
	CompactJWS       string    `json:"compactJws"`
	ExpiresAt        time.Time `json:"expiresAt"`
	ProofRevision    uint64    `json:"proofRevision"`
	ProofDigest      string    `json:"proofDigestSha256"`
	PolicyRevision   uint64    `json:"policyRevision"`
	SignerGeneration uint64    `json:"signerGeneration"`
}
