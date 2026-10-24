package model

import (
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclaims"
	"time"
)

// Contract and request-binding literals identify supported authority envelopes;
// size limits are enforced separately and are not defined by this group.
// Литералы контракта и привязки запроса обозначают поддерживаемые конверты полномочий;
// ограничения размера проверяются отдельно и этой группой не задаются.
const (
	// ContractVersion identifies the signed authority document version that producers and consumers must
	// match before interpretation; the constant selects no decoder fallback.
	// ContractVersion обозначает версию signed authority документа, которую producer и consumer сверяют до
	// интерпретации; константа не выбирает decoder fallback.
	ContractVersion = 1
	// AuthorityABIVersion pins the authorization ABI understood by policy, proofs and contexts independently
	// of the outer document version.
	// AuthorityABIVersion закрепляет authorization ABI, понимаемый policy, proofs и contexts, независимо от
	// внешней версии документа.
	AuthorityABIVersion = 2
	// ReplayModeOneTime selects durable single-use context admission; the string itself does not consume a
	// JTI or establish replay protection.
	// ReplayModeOneTime выбирает durable одноразовый context admission; сама строка не потребляет JTI и не
	// устанавливает replay protection.
	ReplayModeOneTime = "ONE_TIME"
	// RequestBindingUnary names the exact unary Proto request-digest profile, distinct from stream-session
	// authorization where no complete request exists at opening.
	// RequestBindingUnary обозначает профиль exact unary Proto request digest отдельно от stream-session
	// авторизации без полного request при открытии.
	RequestBindingUnary = "UNARY_PROTO_SHA256"
	// RequestBindingStream binds a streaming session rather than claiming that a single initial Proto
	// message represents every later stream action.
	// RequestBindingStream связывает streaming session, не утверждая, что одно начальное Proto сообщение
	// представляет все последующие stream действия.
	RequestBindingStream = "STREAM_SESSION"
	// ProfileBindingRequired marks a policy dimension that must participate in the request profile; it is a
	// closed policy value, not caller-selected authority.
	// ProfileBindingRequired помечает policy измерение, обязательное в request profile; это закрытое policy
	// значение, не выбранные caller полномочия.
	ProfileBindingRequired = "REQUIRED"
	// ProfileBindingForbidden marks a policy dimension forbidden for that request profile so absent
	// semantics cannot be supplied implicitly by a caller.
	// ProfileBindingForbidden помечает policy измерение как запрещённое для request profile, исключая
	// неявное добавление отсутствующей семантики caller.
	ProfileBindingForbidden = "FORBIDDEN"
)

// Workload pairs an enrolled workload ID with its exact SPIFFE identity in signed authority data;
// constructing this value alone does not authenticate a transport peer.
// Workload связывает зарегистрированный workload ID с точной SPIFFE identity в signed authority данных;
// само создание значения не аутентифицирует transport peer.
type Workload authorityclaims.Workload

// Provenance pins the owner's source kind/reference, monotonic revision and content digest for a
// resolved identity; consumers must validate that source rather than trusting these fields as
// self-asserted authority.
// Provenance закрепляет source kind/reference владельца, monotonic revision и content digest разрешённой
// identity; consumer проверяет источник, не доверяя полям как самозаявленным полномочиям.
type Provenance authorityclaims.Provenance

// Identity carries a resolved domain identifier together with its authority provenance, keeping identity
// lookup separate from the identifier string in an untrusted request.
// Identity несёт разрешённый domain идентификатор с authority происхождением, отделяя identity lookup от
// строки идентификатора в недоверенном request.
type Identity authorityclaims.Identity[Provenance]

// Authority records the actor kind, resolved actor/tenant and optional project boundary. Operation
// permission belongs to the surrounding binding/claims, not an extra permission granted by this
// structure.
// Authority фиксирует actor kind, разрешённые actor/tenant и optional project границу. Operation
// permission принадлежит окружающим binding/claims, не дополнительному разрешению этой структуры.
type Authority authorityclaims.Authority[Identity]

// CredentialAuthentication preserves signed authentication time and optional ACR/AMR provenance; copying
// the metadata does not reauthenticate a user or elevate policy eligibility.
// CredentialAuthentication сохраняет signed время аутентификации и optional ACR/AMR происхождение;
// копирование metadata не аутентифицирует пользователя заново и не повышает policy допустимость.
type CredentialAuthentication authorityclaims.CredentialAuthentication

// AuthorityProof is the owner's short-lived signed input for a caller/operation, binding resolved
// authority, credential/proof generations and exact request profile with JTI and time limits; parsing
// the payload does not verify its signature or freshness.
// AuthorityProof задаёт краткоживущий signed вход владельца для caller/operation, связывая разрешённые
// полномочия, credential/proof поколения и exact request profile с JTI и сроками; разбор payload не
// проверяет подпись и свежесть.
type AuthorityProof authorityclaims.Proof[Workload, Authority, CredentialAuthentication]

// AuthorizationClaims represents the signed internal RPC context with caller/target/method/permission,
// provenance, source/key/policy pins and optional continuation lineage. It is a data model, not a
// replacement for signature, peer, expiry and durable replay verification.
// AuthorizationClaims представляет signed internal RPC context с caller/target/method/permission,
// происхождением, source/key/policy привязками и optional continuation lineage. Это модель данных, не
// замена signature, peer, expiry и durable replay проверкам.
type AuthorizationClaims struct {
	Version                  int                       `json:"v"`
	Issuer                   string                    `json:"iss"`
	Audience                 string                    `json:"aud"`
	Subject                  string                    `json:"sub"`
	Caller                   Workload                  `json:"caller"`
	Target                   Workload                  `json:"target"`
	FullMethod               string                    `json:"rpc"`
	OperationID              string                    `json:"operation_id"`
	Authority                Authority                 `json:"authority"`
	Permission               string                    `json:"permission"`
	JTI                      string                    `json:"jti"`
	IssuedAt                 int64                     `json:"iat"`
	NotBefore                int64                     `json:"nbf"`
	ExpiresAt                int64                     `json:"exp"`
	ReplayMode               string                    `json:"replay_mode"`
	SourceRevision           uint64                    `json:"source_revision"`
	SourceDigestSHA256       string                    `json:"source_digest_sha256"`
	KeySetRevision           uint64                    `json:"key_set_revision"`
	PolicyRevision           uint64                    `json:"policy_revision"`
	SignerGeneration         uint64                    `json:"signer_generation"`
	CallerCredentialRevision uint64                    `json:"caller_credential_revision"`
	CredentialAuthentication *CredentialAuthentication `json:"credential_authentication,omitempty"`
	RequestDigestSHA256      string                    `json:"request_digest_sha256,omitempty"`
	RequestBindingMode       string                    `json:"request_binding_mode"`
	AuthorityABIVersion      uint32                    `json:"authority_abi_version"`
	Continuation             *ContinuationLineage      `json:"continuation,omitempty"`
}

// ContinuationLineage pins one child to its root and already accepted parent, including
// request/correlation and source provenance; callers cannot create valid delegation merely by filling
// these fields.
// ContinuationLineage связывает child с root и уже принятым parent, включая request/correlation и source
// происхождение; заполнение полей само не создаёт корректное делегирование.
type ContinuationLineage struct {
	RootJTI                string `json:"root_jti"`
	RootOperationID        string `json:"root_operation_id"`
	RootFullMethod         string `json:"root_full_method"`
	RootSourceRevision     uint64 `json:"root_source_revision"`
	RootSourceDigestSHA256 string `json:"root_source_digest_sha256"`
	ParentJTI              string `json:"parent_jti"`
	ParentOperationID      string `json:"parent_operation_id"`
	ParentFullMethod       string `json:"parent_full_method"`
	RequestID              string `json:"request_id"`
	CorrelationID          string `json:"correlation_id"`
}

// RequestProfile names the binding mode and applicability of resource/version/attempt/idempotency
// dimensions in machine policy, not runtime values supplied by a request.
// RequestProfile задаёт binding mode и применимость resource/version/attempt/idempotency измерений в
// machine policy, не runtime значения из request.
type RequestProfile struct {
	Mode        string `yaml:"mode"`
	Resource    string `yaml:"resource"`
	Version     string `yaml:"version"`
	Attempt     string `yaml:"attempt"`
	Idempotency string `yaml:"idempotency"`
}

// ContinuationProfile restricts a child operation to one exact parent operation and full method; it does
// not enable arbitrary transitive delegation.
// ContinuationProfile ограничивает child operation одной точной parent operation и full method, не
// включая произвольное транзитивное делегирование.
type ContinuationProfile struct {
	ParentOperationID string `yaml:"parent_operation_id"`
	ParentFullMethod  string `yaml:"parent_full_method"`
}

// IssuedTime converts issued-at Unix seconds into a time.Time using time.Unix; it adds no validity check
// or explicit UTC location conversion.
// IssuedTime преобразует issued-at Unix секунды в time.Time через time.Unix без проверки корректности и
// явного преобразования location в UTC.
func (claims AuthorizationClaims) IssuedTime() time.Time {
	return time.Unix(claims.IssuedAt, 0)
}

// NotBeforeTime converts the signed not-before seconds with time.Unix without deciding whether the
// current clock is inside the permitted validity window.
// NotBeforeTime преобразует signed not-before секунды через time.Unix без решения о попадании текущего
// времени в допустимое окно действия.
func (claims AuthorizationClaims) NotBeforeTime() time.Time {
	return time.Unix(claims.NotBefore, 0)
}

// ExpiryTime converts expiry seconds to a time.Time without extending the lifetime, checking freshness
// or explicitly replacing the location with UTC.
// ExpiryTime преобразует expiry секунды в time.Time без продления срока, проверки свежести и явной
// замены location на UTC.
func (claims AuthorizationClaims) ExpiryTime() time.Time {
	return time.Unix(claims.ExpiresAt, 0)
}

// OperationBinding joins one allowed caller/target/method/operation/permission to proof issuers,
// authority sources, project requirement, TTL and exact request/continuation profiles; it is
// policy-owned rather than assembled from caller payload.
// OperationBinding связывает разрешённые caller/target/method/operation/permission с proof issuers,
// authority sources, project requirement, TTL и exact request/continuation profiles; binding принадлежит
// policy, не собирается из caller payload.
type OperationBinding struct {
	OperationID            string               `yaml:"operation_id"`
	CallerWorkloadID       string               `yaml:"caller_workload_id"`
	CallerSPIFFEID         string               `yaml:"caller_spiffe_id"`
	Issuer                 string               `yaml:"issuer"`
	TargetWorkloadID       string               `yaml:"target_workload_id"`
	TargetSPIFFEID         string               `yaml:"target_spiffe_id"`
	Audience               string               `yaml:"audience"`
	FullMethod             string               `yaml:"full_method"`
	Permission             string               `yaml:"permission"`
	AuthorityProofIssuer   string               `yaml:"authority_proof_issuer"`
	AuthorityProofAudience string               `yaml:"authority_proof_audience"`
	AuthoritySources       []string             `yaml:"authority_sources"`
	ProjectRequired        bool                 `yaml:"project_required"`
	TokenTTLSeconds        int64                `yaml:"token_ttl_seconds"`
	RequestProfile         RequestProfile       `yaml:"request_profile"`
	Continuation           *ContinuationProfile `yaml:"continuation,omitempty"`
}

// PolicySnapshot carries the versioned closed machine policy, trust/signing metadata, predecessor
// history and allowed operation bindings. Loaders must verify its signed provenance and consistency
// before it becomes served authority state.
// PolicySnapshot несёт версионированную закрытую machine policy, trust/signing metadata, predecessor
// историю и допустимые operation bindings. До превращения в served authority state loader проверяет
// signed происхождение и согласованность.
type PolicySnapshot struct {
	Version                 int                `yaml:"version"`
	AuthorityABIVersion     uint32             `yaml:"authority_abi_version"`
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

// RevisionDigest pairs a policy/history revision with its canonical digest; it does not contain a
// receipt or prove the revision is active.
// RevisionDigest связывает policy/history ревизию с canonical digest без receipt и доказательства
// активности ревизии.
type RevisionDigest struct {
	Revision     uint64 `yaml:"revision"`
	DigestSHA256 string `yaml:"digest_sha256"`
}

// SnapshotState summarizes the domain's served source/key/policy/signer pins and predecessor history
// without the storage-side attestation receipt identity carried by repository.SnapshotState.
// SnapshotState описывает доменные served source/key/policy/signer привязки и predecessor историю без
// storage-side attestation receipt identity из repository.SnapshotState.
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
