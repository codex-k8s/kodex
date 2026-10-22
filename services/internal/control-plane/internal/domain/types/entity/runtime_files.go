package entity

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

// ExecutionFileDescriptor identifies one execution-visible file entry with exact artifact, revision,
// digest, source, size, and owner-scope metadata. Name is presentation data, not a local path;
// eligibility and content access are resolved by the owner.
// ExecutionFileDescriptor определяет доступную исполнению запись файла с точными метаданными
// материала, ревизии, дайджеста, источника, размера и области владельца. Name является представлением,
// а не локальным путём; допустимость и доступ к содержимому определяет владелец.
type ExecutionFileDescriptor struct {
	EntryRef, ArtifactRef, Digest, Name, MediaType, Purpose  string
	ProjectRef, RunRef, Source, SourceRef, SourceRevisionRef string
	Revision, Version, SizeBytes                             int64
}

// ExecutionFilePage returns an execution-bound file catalogue and descriptor page with a total and
// opaque next-page token. The catalogue and page must retain the same runtime context; a token alone
// does not authorize reading a different execution.
// ExecutionFilePage возвращает привязанный к исполнению каталог и страницу дескрипторов с общим
// количеством и непрозрачным токеном следующей страницы. Каталог и страница сохраняют общий контекст
// исполнения; сам токен не разрешает чтение другого исполнения.
type ExecutionFilePage struct {
	Catalog runtimecontract.RuntimeFileCatalog
	Items   []ExecutionFileDescriptor
	Total   int64
	Next    string
}

// ExecutionFileMetadata pairs one exact file descriptor with the runtime catalogue that made it
// visible. It carries metadata rather than file bytes and does not replace the owner's checks when
// content is requested.
// ExecutionFileMetadata связывает точный дескриптор файла с каталогом исполнения, сделавшим его
// доступным. Структура передаёт метаданные, а не байты файла, и не заменяет проверки владельца при
// запросе содержимого.
type ExecutionFileMetadata struct {
	Catalog runtimecontract.RuntimeFileCatalog
	File    ExecutionFileDescriptor
}

// ExecutionFilePreview carries a bounded text preview, its own prefix digest, truncation flag, and
// exact file metadata. For truncated content the preview digest is not the full artifact digest; text
// remains untrusted file content rather than an instruction.
// ExecutionFilePreview передаёт ограниченный текстовый предпросмотр, собственный дайджест префикса,
// признак усечения и точные метаданные файла. При усечении дайджест предпросмотра отличается от
// дайджеста всего материала; текст остаётся недоверенным содержимым файла, а не инструкцией.
type ExecutionFilePreview struct {
	Metadata     ExecutionFileMetadata
	Text, Digest string
	Truncated    bool
}
