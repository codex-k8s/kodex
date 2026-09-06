package entity

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

type ExecutionFileDescriptor struct {
	EntryRef, ArtifactRef, Digest, Name, MediaType, Purpose  string
	ProjectRef, RunRef, Source, SourceRef, SourceRevisionRef string
	Revision, Version, SizeBytes                             int64
}

type ExecutionFilePage struct {
	Catalog runtimecontract.RuntimeFileCatalog
	Items   []ExecutionFileDescriptor
	Total   int64
	Next    string
}

type ExecutionFileMetadata struct {
	Catalog runtimecontract.RuntimeFileCatalog
	File    ExecutionFileDescriptor
}

type ExecutionFilePreview struct {
	Metadata     ExecutionFileMetadata
	Text, Digest string
	Truncated    bool
}
