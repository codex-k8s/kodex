package entity

import "time"

// PublicRuntimeRevisionIdentity не разделяет тип с приватным worker snapshot.
type PublicRuntimeRevisionIdentity struct {
	Ref, RunRef, SessionRef, TurnRef, RevisionDigest string
	Version                                          int64
	Attempt                                          int32
	CreatedAt                                        time.Time
}

type RuntimeRevisionDiffValue struct {
	Ref      string
	Version  int64
	Digest   string
	Revision string
}

type RuntimeRevisionDiffChange struct {
	Component         string
	Previous, Current *RuntimeRevisionDiffValue
}

type RuntimeRevisionDiff struct {
	Current  PublicRuntimeRevisionIdentity
	Previous *PublicRuntimeRevisionIdentity
	Changes  []RuntimeRevisionDiffChange
}

// RuntimeRevisionPublicProjection содержит только закрытый набор безопасных
// метаданных. Repository не читает JSON рабочего snapshot для этой операции.
type RuntimeRevisionPublicProjection struct {
	Identity PublicRuntimeRevisionIdentity
	Provider, Model, RuntimeProfile, RuntimeConfiguration, ProviderPolicy,
	ConfigOverlay, Environment, EnvironmentBinding, Instruction,
	IntegrationGrants, Image RuntimeRevisionDiffValue
}
