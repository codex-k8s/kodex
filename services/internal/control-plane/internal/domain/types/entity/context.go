package entity

import "time"

// ContextProvenance records the actor, source kind and identity, source revision, digest, and creation
// time of context material. It preserves reported lineage; the owner must verify that lineage rather
// than treating these strings as authenticated authority.
// ContextProvenance фиксирует субъекта, вид и личность источника, ревизию, дайджест и время создания
// материала контекста. Структура сохраняет сообщённое происхождение; владелец проверяет его, а не
// считает строки удостоверенными полномочиями.
type ContextProvenance struct {
	ActorRef, SourceKind, SourceRef, SourceRevision, Digest string
	CreatedAt                                               time.Time
}

// MemoryRecordSpecification supplies a proposed memory title, summary, source run, and retention
// deadline. It is editable content, not a binding to another run or permission to retain data beyond
// the owner's policy.
// MemoryRecordSpecification передаёт предлагаемые название, описание, исходный запуск и срок хранения
// памяти. Это редактируемое содержимое, а не привязка к чужому запуску или разрешение хранить данные
// сверх политики владельца.
type MemoryRecordSpecification struct {
	Title, Summary, SourceRunRef string
	RetentionUntil               time.Time
}

// MemoryRecordRevision describes a memory revision's content, digest, predecessor, provenance,
// retention, and redaction flag. The flag records lifecycle state and does not itself remove text from
// arbitrary copies.
// MemoryRecordRevision описывает содержимое, дайджест, предшественника, происхождение, срок хранения и
// признак сокрытия ревизии памяти. Признак фиксирует состояние жизненного цикла, но сам не удаляет
// текст из произвольных копий.
type MemoryRecordRevision struct {
	Ref, Title, Summary, Digest, ParentRevisionRef string
	Revision                                       int64
	Provenance                                     ContextProvenance
	RetentionUntil                                 time.Time
	Redacted                                       bool
}

// KodexMemoryRecord presents a project-and-agent-scoped memory aggregate with version, lifecycle
// state, optional current revision, and timestamps. Reading the record does not grant permission to
// attach it to a different agent's runtime context.
// KodexMemoryRecord представляет агрегат памяти в области проекта и сотрудника с версией, состоянием,
// необязательной текущей ревизией и временем. Чтение записи не разрешает прикреплять её к контексту
// исполнения другого сотрудника.
type KodexMemoryRecord struct {
	Ref, ProjectRef, AgentRef, State string
	Version                          int64
	CurrentRevision                  *MemoryRecordRevision
	CreatedAt, UpdatedAt             time.Time
}

// SkillBundleFile pins a bundle-relative path to an exact artifact revision, digest, and size. The
// path is untrusted metadata until validated; it must not directly select an arbitrary host filesystem
// destination.
// SkillBundleFile закрепляет относительный путь пакета за точными ревизией материала, дайджестом и
// размером. Путь остаётся недоверенными метаданными до проверки и не должен прямо выбирать
// произвольное место файловой системы узла.
type SkillBundleFile struct {
	Path, ArtifactRef, Digest   string
	ArtifactRevision, SizeBytes int64
}

// SkillBundleSpecification supplies the editable name, description, and exact file descriptors of a
// skill bundle. Publication requires the owner's validation, scan, and review flow rather than merely
// constructing this value.
// SkillBundleSpecification передаёт редактируемые имя, описание и точные дескрипторы файлов пакета
// навыков. Публикация требует проверок, сканирования и рассмотрения владельцем, а не простого создания
// значения.
type SkillBundleSpecification struct {
	Name, Description string
	Files             []SkillBundleFile
}

// SkillBundleRevision records a versioned bundle's file set, content digest, predecessor, provenance,
// scan evidence, review metadata, and diagnostics. Scan state and reviewer strings are stored evidence
// to validate, not self-issued admission or executable instructions.
// SkillBundleRevision фиксирует файлы, дайджест содержимого, предшественника, происхождение,
// свидетельства сканирования, метаданные рассмотрения и диагностику версии пакета. Состояние
// сканирования и строки рецензента являются проверяемыми свидетельствами, а не самостоятельным
// допуском или исполняемыми инструкциями.
type SkillBundleRevision struct {
	Ref, State, Name, Description, Digest, ParentRevisionRef string
	Revision                                                 int64
	Files                                                    []SkillBundleFile
	Provenance                                               ContextProvenance
	ScanState, ScanEngine, ScanDigest, ReviewedBy            string
	ScannedAt, ReviewedAt                                    *time.Time
	Diagnostics                                              []string
}

// SkillBundle presents a scoped versioned skill aggregate with separate current and draft revisions.
// Draft content is not implicitly eligible for runtime attachment merely because the bundle has a
// published current revision.
// SkillBundle представляет пакет навыков в области владельца с версией и отдельными текущей и черновой
// ревизиями. Содержимое черновика не становится допустимым для исполнения только из-за наличия
// опубликованной текущей ревизии пакета.
type SkillBundle struct {
	Ref, ProjectRef, State         string
	Version                        int64
	CurrentRevision, DraftRevision *SkillBundleRevision
	CreatedAt, UpdatedAt           time.Time
}

// AgentContextBinding pins one agent's context attachment to an exact resource revision and digest
// with a binding version. The owner resolves eligibility and concurrency before changing the binding;
// a resource reference alone grants no context access.
// AgentContextBinding закрепляет вложение контекста сотрудника за точными ревизией и дайджестом
// ресурса с версией привязки. До изменения владелец проверяет допустимость и конкуренцию; ссылка на
// ресурс сама не даёт доступа к контексту.
type AgentContextBinding struct {
	Ref, AgentRef, ResourceRef, RevisionRef, Digest string
	Version                                         int64
}
