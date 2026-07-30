package model

// DatabaseCredentialCapability задаёт назначение principal PostgreSQL.
type DatabaseCredentialCapability string

// Поддерживаемые назначения учётных данных PostgreSQL.
const (
	DatabaseCredentialPublisher DatabaseCredentialCapability = "PUBLISHER"
	DatabaseCredentialAttestor  DatabaseCredentialCapability = "READBACK_ATTESTOR"
)

// DatabaseCredentialStatus задаёт состояние поколения учётных данных.
type DatabaseCredentialStatus string

// Допустимые состояния поколения учётных данных.
const (
	DatabaseCredentialCurrent  DatabaseCredentialStatus = "CURRENT"
	DatabaseCredentialNext     DatabaseCredentialStatus = "NEXT"
	DatabaseCredentialPrevious DatabaseCredentialStatus = "PREVIOUS"
	DatabaseCredentialRetired  DatabaseCredentialStatus = "RETIRED"
)

// DatabaseCredentialGeneration связывает principal, поколение и состояние.
type DatabaseCredentialGeneration struct {
	Capability      DatabaseCredentialCapability `json:"capability"`
	Generation      uint64                       `json:"generation"`
	Status          DatabaseCredentialStatus     `json:"status"`
	Principal       string                       `json:"principal"`
	VaultStaticRole string                       `json:"vault_static_role"`
	SourceRevision  uint64                       `json:"source_revision"`
	SourceDigest    string                       `json:"source_digest_sha256"`
}

// DatabaseCredentialRegisteredSet задаёт полный зарегистрированный набор.
type DatabaseCredentialRegisteredSet struct {
	Version        int                            `json:"v"`
	SourceRevision uint64                         `json:"source_revision"`
	SourceDigest   string                         `json:"source_digest_sha256"`
	Generations    []DatabaseCredentialGeneration `json:"generations"`
}
