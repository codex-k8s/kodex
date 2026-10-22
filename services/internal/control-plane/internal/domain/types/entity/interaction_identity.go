package entity

// InteractionIdentity presents a versioned connection-and-channel-scoped mapping from an external user
// digest to a platform subject. The owner validates the mapping and connection version; a matching
// external identifier alone does not authenticate a platform actor.
// InteractionIdentity представляет версионированную привязку дайджеста внешнего пользователя к
// субъекту платформы в области подключения и канала. Владелец проверяет привязку и версию подключения;
// совпадение внешнего идентификатора само не удостоверяет субъекта платформы.
type InteractionIdentity struct {
	Ref, ConnectionRef, ExternalTeamRef, ExternalChannelRef, ExternalUserDigest, SubjectRef, State string
	Version, ConnectionVersion                                                                     int64
}
