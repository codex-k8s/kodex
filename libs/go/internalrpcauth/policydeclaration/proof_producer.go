// Package policydeclaration defines unverified machine-policy field layouts
// without cryptographic, transport, persistence or provider dependencies.
// Пакет policydeclaration задаёт непроверенные формы полей машинной политики
// без зависимостей от криптографии, транспорта, хранения или провайдеров.
package policydeclaration

// ProofProducer defines the existing machine-policy JSON field layout
// for one authority-proof producer: caller/owner identities, resolver transport,
// credential/trust references, lifetime/retry inputs and server-resolved scope.
// This is unverified data, not a credential or permission grant. Consumers retain
// their own named types and validate provenance, allowed values and eligibility;
// copying this value does not deep-copy its slices or make it immutable.
// ProofProducer задаёт прежнюю форму полей JSON машинной политики
// одного производителя доказательств: личности сторон, транспорт resolver,
// ссылки на учётные данные и доверие, сроки, повторы и разрешаемую сервером область.
// Это непроверенные данные, не учётное полномочие и не выдача права. Потребители
// сохраняют собственные именованные типы и проверяют происхождение, значения
// и допустимость; копия значения не копирует срезы и не делает его неизменяемым.
type ProofProducer struct {
	ProducerID                         string   `json:"producer_id"`
	CallerWorkloadID                   string   `json:"caller_workload_id"`
	CallerSPIFFEID                     string   `json:"caller_spiffe_id"`
	OwnerWorkloadID                    string   `json:"owner_workload_id"`
	OwnerSPIFFEID                      string   `json:"owner_spiffe_id"`
	FullMethod                         string   `json:"full_method"`
	TLSServerName                      string   `json:"tls_server_name"`
	TransportTrustBundleID             string   `json:"transport_trust_bundle_id"`
	ApplicationCredential              string   `json:"application_credential"`
	ApplicationCredentialMetadata      string   `json:"application_credential_metadata"`
	ApplicationCredentialIssuer        string   `json:"application_credential_issuer"`
	ApplicationCredentialAudience      string   `json:"application_credential_audience"`
	ApplicationCredentialTrustBundleID string   `json:"application_credential_trust_bundle_id"`
	AuthorityProofIssuer               string   `json:"authority_proof_issuer"`
	AuthorityProofAudience             string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID        string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds        int64    `json:"authority_proof_max_age_seconds"`
	DeadlineMilliseconds               int64    `json:"deadline_milliseconds"`
	MaxAttempts                        int      `json:"max_attempts"`
	RetryableGRPCCodes                 []string `json:"retryable_grpc_codes"`
	IdempotencyScope                   string   `json:"idempotency_scope"`
	AuthoritySources                   []string `json:"authority_sources"`
	AllowedOperationIDs                []string `json:"allowed_operation_ids"`
	ServerResolvedFields               []string `json:"server_resolved_fields"`
}
