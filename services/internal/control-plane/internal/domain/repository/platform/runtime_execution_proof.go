package platform

import "time"

// RuntimeExecutionProof содержит только разрешённое владельцем состояние
// активного исполнения. Worker не назначает actor или provenance.
type RuntimeExecutionProof struct {
	ActorKind      string
	RevisionID     string
	Generation     uint64
	RevisionDigest string
	ExpiresAt      time.Time
}
