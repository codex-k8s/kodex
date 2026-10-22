package platform

import "time"

// RuntimeExecutionProof carries owner-authorized active-execution metadata: actor kind, revision ID/digest, generation and expiry. It contains neither a worker-selected actor identity nor a signature and must originate from the authoritative owner path.
// RuntimeExecutionProof содержит разрешённые владельцем метаданные активного исполнения: вид субъекта, идентификатор и дайджест ревизии, поколение и срок. Структура не содержит выбранной исполнителем идентичности субъекта или подписи и должна происходить из авторитетного пути владельца.
type RuntimeExecutionProof struct {
	ActorKind      string
	RevisionID     string
	Generation     uint64
	RevisionDigest string
	ExpiresAt      time.Time
}
