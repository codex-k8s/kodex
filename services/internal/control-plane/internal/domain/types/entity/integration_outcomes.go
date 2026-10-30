package entity

// ExecutionIntegrationOutcomes contains one UTF-8 byte page of a digest-pinned
// snapshot. Digests commit content and never grant access or external WRITE authority.
// ExecutionIntegrationOutcomes содержит UTF-8 страницу закреплённого снимка;
// digests связывают содержимое и не предоставляют доступ либо external WRITE authority.
type ExecutionIntegrationOutcomes struct {
	SourceInvocationRef, SourceSHA256, PageSHA256, Text string
	SizeBytes, OffsetBytes, NextOffsetBytes             int64
	OutcomesCount                                       int32
	EOF                                                 bool
}
