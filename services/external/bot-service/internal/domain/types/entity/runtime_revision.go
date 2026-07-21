package entity

import "time"

// RuntimeRevision хранит неизменяемый безопасный манифест фактической конфигурации pod.
type RuntimeRevision struct {
	ID                    int64
	Digest                string
	Manifest              string
	AccountAlias          string
	AuthorizationRevision string
	CreatedAt             time.Time
}

// AgentSessionRuntimeRevisionState связывает сессию с желаемой и применённой ревизиями.
type AgentSessionRuntimeRevisionState struct {
	SessionID                int64
	SessionKey               string
	DesiredRuntimeRevisionID int64
	AppliedRuntimeRevisionID int64
	AppliedPodUID            string
	ReconcileLeaseToken      string
	ReconcileLeaseExpiresAt  time.Time
}

// RuntimeSecretBindingRevision — монотонная server-owned ревизия безопасно наблюдаемой Secret-привязки.
type RuntimeSecretBindingRevision struct {
	BindingKey      string
	SecretName      string
	SecretKey       string
	IntegritySHA256 string
	Revision        int64
	UpdatedAt       time.Time
}

// AgentSessionArchive — подтверждённая неизменяемая версия bounded архива Codex.
type AgentSessionArchive struct {
	ID                int64
	SessionID         int64
	TurnID            int64
	Version           int64
	CodexSessionID    string
	PayloadGzipBase64 string
	SHA256            string
	SizeBytes         int64
	CreatedAt         time.Time
}

// AgentSessionCompletion — атомарный результат terminal turn и подтверждённого архива.
type AgentSessionCompletion struct {
	Turn             AgentSessionTurn
	Session          AgentSession
	Archive          AgentSessionArchive
	AlreadyCompleted bool
}
