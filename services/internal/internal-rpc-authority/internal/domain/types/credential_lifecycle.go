package model

type DatabaseCredentialCapability string

const (
	DatabaseCredentialPublisher DatabaseCredentialCapability = "PUBLISHER"
	DatabaseCredentialAttestor  DatabaseCredentialCapability = "READBACK_ATTESTOR"
)

type DatabaseCredentialStatus string

const (
	DatabaseCredentialCurrent  DatabaseCredentialStatus = "CURRENT"
	DatabaseCredentialNext     DatabaseCredentialStatus = "NEXT"
	DatabaseCredentialPrevious DatabaseCredentialStatus = "PREVIOUS"
	DatabaseCredentialRetired  DatabaseCredentialStatus = "RETIRED"
)

type DatabaseCredentialGeneration struct {
	Capability      DatabaseCredentialCapability `json:"capability"`
	Generation      uint64                       `json:"generation"`
	Status          DatabaseCredentialStatus     `json:"status"`
	Principal       string                       `json:"principal"`
	VaultStaticRole string                       `json:"vault_static_role"`
	SourceRevision  uint64                       `json:"source_revision"`
	SourceDigest    string                       `json:"source_digest_sha256"`
}

type DatabaseCredentialRegisteredSet struct {
	Version        int                            `json:"v"`
	SourceRevision uint64                         `json:"source_revision"`
	SourceDigest   string                         `json:"source_digest_sha256"`
	Generations    []DatabaseCredentialGeneration `json:"generations"`
}
