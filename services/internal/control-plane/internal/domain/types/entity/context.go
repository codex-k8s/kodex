package entity

import "time"

type ContextProvenance struct {
	ActorRef, SourceKind, SourceRef, SourceRevision, Digest string
	CreatedAt                                               time.Time
}
type MemoryRecordSpecification struct {
	Title, Summary, SourceRunRef string
	RetentionUntil               time.Time
}
type MemoryRecordRevision struct {
	Ref, Title, Summary, Digest, ParentRevisionRef string
	Revision                                       int64
	Provenance                                     ContextProvenance
	RetentionUntil                                 time.Time
	Redacted                                       bool
}
type KodexMemoryRecord struct {
	Ref, ProjectRef, AgentRef, State string
	Version                          int64
	CurrentRevision                  *MemoryRecordRevision
	CreatedAt, UpdatedAt             time.Time
}
type SkillBundleFile struct {
	Path, ArtifactRef, Digest   string
	ArtifactRevision, SizeBytes int64
}
type SkillBundleSpecification struct {
	Name, Description string
	Files             []SkillBundleFile
}
type SkillBundleRevision struct {
	Ref, State, Name, Description, Digest, ParentRevisionRef string
	Revision                                                 int64
	Files                                                    []SkillBundleFile
	Provenance                                               ContextProvenance
	ScanState, ScanEngine, ScanDigest, ReviewedBy            string
	ScannedAt, ReviewedAt                                    *time.Time
	Diagnostics                                              []string
}
type SkillBundle struct {
	Ref, ProjectRef, State         string
	Version                        int64
	CurrentRevision, DraftRevision *SkillBundleRevision
	CreatedAt, UpdatedAt           time.Time
}
type AgentContextBinding struct {
	Ref, AgentRef, ResourceRef, RevisionRef, Digest string
	Version                                         int64
}
