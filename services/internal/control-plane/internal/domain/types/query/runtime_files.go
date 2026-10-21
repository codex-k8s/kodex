package query

// ExecutionFileContext contains only execution pins; the state owner resolves
// actor and project through the lease rather than trusting RPC caller fields.
// ExecutionFileContext содержит только привязки исполнения; владелец состояния
// определяет actor и проект по аренде, не доверяя полям вызывающего RPC.
type ExecutionFileContext struct {
	LeaseRef, Fence, CatalogRef, CatalogDigest, Purpose string
	Generation                                          int64
}

// ExecutionFileRef identifies a file entry and artifact at a pinned digest and
// revision for an execution-scoped read.
// ExecutionFileRef определяет запись файла и артефакт с закреплёнными
// дайджестом и ревизией для чтения в области исполнения.
type ExecutionFileRef struct {
	EntryRef, ArtifactRef, Digest string
	Revision                      int64
}
