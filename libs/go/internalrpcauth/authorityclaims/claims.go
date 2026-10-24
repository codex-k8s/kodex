// Package authorityclaims defines only authority-proof JSON data shapes, preserving local domain
// types through type parameters; decoding a record does not authenticate or authorize its contents.
// Пакет authorityclaims определяет только JSON формы authority proof, сохраняя локальные доменные
// типы параметрами; декодирование record не аутентифицирует и не авторизует его содержимое.
package authorityclaims

// Provenance records the source/reference/revision/digest JSON form; its values acquire authority
// only after owner and verifier checks.
// Provenance хранит JSON форму source, reference, revision и digest; значения получают полномочия
// лишь после проверок владельца и verifier.
type Provenance struct {
	Source       string `json:"source"`
	Reference    string `json:"reference"`
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// Identity keeps the identity JSON fields while P preserves the consumer-specific nominal
// provenance type; an ID is not a credential.
// Identity сохраняет JSON поля identity, а P сохраняет номинальный provenance тип потребителя; ID
// не является credential.
type Identity[P any] struct {
	ID         string `json:"id"`
	Provenance P      `json:"provenance"`
}

// Workload stores the exact workload/SPIFFE JSON pair without authenticating a transport peer or
// selecting an allowed operation.
// Workload хранит точную JSON пару workload и SPIFFE без аутентификации transport peer либо выбора
// разрешённой операции.
type Workload struct {
	WorkloadID string `json:"workload_id"`
	SPIFFEID   string `json:"spiffe_id"`
}

// Authority keeps actor/tenant and optional project JSON fields while I preserves the local
// identity type; constructing it grants no permission.
// Authority сохраняет JSON поля actor, tenant и необязательного project, а I сохраняет локальный
// тип identity; создание значения не выдаёт permission.
type Authority[I any] struct {
	ActorKind string `json:"actor_kind"`
	Actor     I      `json:"actor"`
	Tenant    I      `json:"tenant"`
	Project   *I     `json:"project,omitempty"`
}

// CredentialAuthentication carries authentication time and optional ACR/AMR metadata; it does not
// verify freshness, and a value copy still shares the AMR slice.
// CredentialAuthentication несёт время аутентификации и необязательные ACR/AMR метаданные;
// свежесть не проверяется, а копия значения сохраняет общий AMR срез.
type CredentialAuthentication struct {
	AuthenticatedAt int64    `json:"auth_time"`
	ACR             string   `json:"acr,omitempty"`
	AMR             []string `json:"amr,omitempty"`
}

// Proof defines the existing authority-proof JSON field layout with local
// workload/authority/authentication types W, A and C; signing, validation and replay state belong
// to callers.
// Proof определяет прежнюю JSON форму authority proof с локальными типами workload, authority и
// аутентификации W, A и C; подпись, проверка и replay состояние принадлежат callers.
type Proof[W, A, C any] struct {
	Version                      int    `json:"v"`
	Issuer                       string `json:"iss"`
	Audience                     string `json:"aud"`
	Caller                       W      `json:"caller"`
	OperationID                  string `json:"operation_id"`
	AuthorizationContextAudience string `json:"authorization_context_audience"`
	Authority                    A      `json:"authority"`
	ProofRevision                uint64 `json:"proof_revision"`
	SignerGeneration             uint64 `json:"signer_generation"`
	CallerCredentialRevision     uint64 `json:"caller_credential_revision"`
	CredentialAuthentication     *C     `json:"credential_authentication,omitempty"`
	JTI                          string `json:"jti"`
	IssuedAt                     int64  `json:"iat"`
	NotBefore                    int64  `json:"nbf"`
	ExpiresAt                    int64  `json:"exp"`
	RequestDigestSHA256          string `json:"request_digest_sha256,omitempty"`
	RequestBindingMode           string `json:"request_binding_mode"`
	AuthorityABIVersion          uint32 `json:"authority_abi_version"`
}
