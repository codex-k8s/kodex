package query

// ExecutionFileContext содержит только execution pin. Actor и Project
// разрешаются по lease владельцем состояния, а не назначаются RPC caller.
type ExecutionFileContext struct {
	LeaseRef, Fence, CatalogRef, CatalogDigest, Purpose string
	Generation                                          int64
}

type ExecutionFileRef struct {
	EntryRef, ArtifactRef, Digest string
	Revision                      int64
}
