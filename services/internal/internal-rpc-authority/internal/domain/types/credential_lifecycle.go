package model

import "time"

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

// DatabaseCredentialRotationPhase задаёт устойчивый шаг one-shot ротации.
type DatabaseCredentialRotationPhase string

// Замкнутые состояния ротации учётных данных.
const (
	DatabaseCredentialRotationCreated    DatabaseCredentialRotationPhase = "CREATED"
	DatabaseCredentialRotationPreRotated DatabaseCredentialRotationPhase = "PRE_ROTATE_CHECKPOINTED"
	DatabaseCredentialRotationStaged     DatabaseCredentialRotationPhase = "NEXT_STAGED"
	DatabaseCredentialRotationReadBack   DatabaseCredentialRotationPhase = "NEXT_READ_BACK"
	DatabaseCredentialRotationPromoted   DatabaseCredentialRotationPhase = "PROMOTED"
	DatabaseCredentialRotationRolledOut  DatabaseCredentialRotationPhase = "CURRENT_ROLLED_OUT"
	DatabaseCredentialRotationCompleted  DatabaseCredentialRotationPhase = "COMPLETED"
)

// DatabaseCredentialRotationIntent хранит durable progress и только
// односторонние digest фактически прочитанных static credentials.
type DatabaseCredentialRotationIntent struct {
	RequestID             string
	CanonicalDigestSHA256 string
	Phase                 DatabaseCredentialRotationPhase
	PreRotationDigests    map[string]string
	StagedDigests         map[string]string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// DatabaseCredentialSessionReadback подтверждает фактическую сессию consumer.
type DatabaseCredentialSessionReadback struct {
	Capability             DatabaseCredentialCapability
	Generation             uint64
	Status                 DatabaseCredentialStatus
	Principal              string
	CredentialDigestSHA256 string
	PodUID                 string
	ObservedAt             time.Time
}
