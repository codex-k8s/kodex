package internalrpcauth

// PlatformWorkerGrantClaims defines the common JSON field layout of a platform
// worker's signed credential, not a verified identity or authorization result.
// Local issuer and verifier types retain their distinct names; signature,
// version/instance, time, producer and durable acceptance checks remain theirs.
// InstanceID is omitted only when empty; other zero fields remain on the wire.
// Subject and organization fields do not by themselves establish root authority.
// PlatformWorkerGrantClaims задаёт общую форму JSON подписанных учётных данных
// платформенной нагрузки, не проверенную личность или результат авторизации.
// Локальные типы выпуска и проверки сохраняют разные имена; подпись, версия,
// экземпляр, сроки, producer и устойчивое принятие проверяются на прежних местах.
// Только пустой InstanceID опускается; остальные нулевые поля остаются в JSON.
// Поля субъекта и организации сами по себе не задают корневые полномочия.
type PlatformWorkerGrantClaims struct {
	Version              int    `json:"v"`
	InstanceID           string `json:"instance_id,omitempty"`
	Issuer               string `json:"iss"`
	Audience             string `json:"aud"`
	Subject              string `json:"sub"`
	CallerSPIFFEID       string `json:"caller_spiffe_id"`
	WorkloadID           string `json:"workload_id"`
	OrganizationID       string `json:"organization_id"`
	ProjectID            string `json:"project_id"`
	TenantOwner          bool   `json:"tenant_owner"`
	Revision             uint64 `json:"revision"`
	CredentialGeneration uint64 `json:"credential_generation"`
	AuthorityABIVersion  uint32 `json:"authority_abi_version"`
	JTI                  string `json:"jti"`
	IssuedAt             int64  `json:"iat"`
	NotBefore            int64  `json:"nbf"`
	ExpiresAt            int64  `json:"exp"`
}
