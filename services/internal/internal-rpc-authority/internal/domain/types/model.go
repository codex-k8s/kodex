package model

import "time"

const (
	ContractVersion   = 1
	ReplayModeOneTime = "ONE_TIME"
)

type Workload struct {
	WorkloadID string `json:"workload_id"`
	SPIFFEID   string `json:"spiffe_id"`
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

type Authority struct {
	ActorKind string    `json:"actor_kind"`
	Actor     Identity  `json:"actor"`
	Tenant    Identity  `json:"tenant"`
	Project   *Identity `json:"project,omitempty"`
}

type AuthorityProof struct {
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

type AuthorizationClaims struct {
	Version            int       `json:"v"`
	Issuer             string    `json:"iss"`
	Audience           string    `json:"aud"`
	Subject            string    `json:"sub"`
	Caller             Workload  `json:"caller"`
	Target             Workload  `json:"target"`
	FullMethod         string    `json:"rpc"`
	OperationID        string    `json:"operation_id"`
	Authority          Authority `json:"authority"`
	Permission         string    `json:"permission"`
	JTI                string    `json:"jti"`
	IssuedAt           int64     `json:"iat"`
	NotBefore          int64     `json:"nbf"`
	ExpiresAt          int64     `json:"exp"`
	ReplayMode         string    `json:"replay_mode"`
	SourceRevision     uint64    `json:"source_revision"`
	SourceDigestSHA256 string    `json:"source_digest_sha256"`
	KeySetRevision     uint64    `json:"key_set_revision"`
	PolicyRevision     uint64    `json:"policy_revision"`
	SignerGeneration   uint64    `json:"signer_generation"`
}

func (claims AuthorizationClaims) IssuedTime() time.Time {
	return time.Unix(claims.IssuedAt, 0)
}

func (claims AuthorizationClaims) NotBeforeTime() time.Time {
	return time.Unix(claims.NotBefore, 0)
}

func (claims AuthorizationClaims) ExpiryTime() time.Time {
	return time.Unix(claims.ExpiresAt, 0)
}

type OperationBinding struct {
	OperationID            string   `yaml:"operation_id"`
	CallerWorkloadID       string   `yaml:"caller_workload_id"`
	CallerSPIFFEID         string   `yaml:"caller_spiffe_id"`
	Issuer                 string   `yaml:"issuer"`
	TargetWorkloadID       string   `yaml:"target_workload_id"`
	TargetSPIFFEID         string   `yaml:"target_spiffe_id"`
	Audience               string   `yaml:"audience"`
	FullMethod             string   `yaml:"full_method"`
	Permission             string   `yaml:"permission"`
	AuthorityProofIssuer   string   `yaml:"authority_proof_issuer"`
	AuthorityProofAudience string   `yaml:"authority_proof_audience"`
	AuthoritySources       []string `yaml:"authority_sources"`
	ProjectRequired        bool     `yaml:"project_required"`
	TokenTTLSeconds        int64    `yaml:"token_ttl_seconds"`
}

type PolicySnapshot struct {
	Version                 int                `yaml:"version"`
	TrustDomain             string             `yaml:"trust_domain"`
	DefaultDecision         string             `yaml:"default_decision"`
	TokenTTLSeconds         int64              `yaml:"token_ttl_seconds"`
	AllowedClockSkewSeconds int64              `yaml:"allowed_clock_skew_seconds"`
	MaxCompactJWSBytes      int                `yaml:"max_compact_jws_bytes"`
	Issuer                  string             `yaml:"issuer"`
	SignerKeyID             string             `yaml:"signer_key_id"`
	SourceRevision          uint64             `yaml:"source_revision"`
	SourceDigestSHA256      string             `yaml:"source_digest_sha256"`
	PredecessorRevision     uint64             `yaml:"predecessor_revision"`
	PredecessorDigestSHA256 string             `yaml:"predecessor_digest_sha256"`
	KeySetRevision          uint64             `yaml:"key_set_revision"`
	PolicyRevision          uint64             `yaml:"policy_revision"`
	SignerGeneration        uint64             `yaml:"signer_generation"`
	History                 []RevisionDigest   `yaml:"history"`
	AuthorityProofProducers []string           `yaml:"authority_proof_producers"`
	OperationBindings       []OperationBinding `yaml:"operation_bindings"`
}

type RevisionDigest struct {
	Revision     uint64 `yaml:"revision"`
	DigestSHA256 string `yaml:"digest_sha256"`
}

type SnapshotState struct {
	SourceRevision          uint64
	SourceDigestSHA256      string
	PredecessorRevision     uint64
	PredecessorDigestSHA256 string
	KeySetRevision          uint64
	PolicyRevision          uint64
	SignerGeneration        uint64
	History                 []RevisionDigest
}
