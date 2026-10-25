package entity

import (
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
)

// RepositoryBundleArtifactPin identifies the exact captured CLEAN bytes, not a mutable current artifact or an authorization to download it.
// RepositoryBundleArtifactPin обозначает exact captured CLEAN bytes, не mutable current artifact и не разрешение на download.
type RepositoryBundleArtifactPin = repositorybundle.ArtifactPin

// RepositoryBundlePublication is the safe immutable proposal projection; candidate/result state is owner-controlled and raw source/credentials are excluded.
// RepositoryBundlePublication — безопасная immutable proposal projection; candidate/result state назначает owner, raw source/credentials исключены.
type RepositoryBundlePublication struct {
	Ref, ProjectRef, OriginRunRef, OriginRuntimeRevisionRef  string
	InvocationRef, GateRef, ConnectionRef, GrantRef          string
	RepositoryRef, BaseCommitSHA, ProposalBranch             string
	ApprovalDigest, State, CandidateCommitSHA                string
	CandidateTreeSHA, FailureCode                            string
	ContinuationState, ContinuationCode, ContinuationNodeRef string
	Version, ConnectionVersion, GrantVersion, TotalBytes     int64
	FileCount                                                int32
	Archive, Manifest                                        RepositoryBundleArtifactPin
	CreatedAt, ExpiresAt                                     time.Time
	ConfirmedAt                                              *time.Time
}

// RepositoryBundleLease is the exact durable work tuple; a proposal ref alone never authorizes content reads, Begin or completion.
// RepositoryBundleLease — точный durable work tuple; одного proposal ref недостаточно для content read, Begin либо completion.
type RepositoryBundleLease struct {
	ProposalRef, Claimant, Fence string
	Attempt, Generation          int64
	ExpiresAt                    time.Time
}

// RepositoryBundlePublicationWork contains only the owner-issued execution/recovery snapshot, private credential descriptor and full bounded manifest.
// RepositoryBundlePublicationWork содержит лишь назначенный owner execution/recovery snapshot, private credential descriptor и полный bounded manifest.
type RepositoryBundlePublicationWork struct {
	ResourceScope                            map[string]string
	ResourceScopeDigest                      string
	Lease                                    RepositoryBundleLease
	Proposal                                 RepositoryBundlePublication
	Mode, DefinitionKey, DefinitionVersion   string
	DefinitionDigest, EffectKey, InputDigest string
	DefinitionPackage, Manifest              []byte
	PublicConfiguration                      map[string]any
	Credential                               IntegrationCredentialRevision
	CommitTime, Deadline                     time.Time
	EffectStartedAt                          *time.Time
}

// RepositoryBundlePublicationView binds a bounded manifest page to a full proposal and explicit continuation offset; Complete never means the project was partially imported.
// RepositoryBundlePublicationView связывает bounded manifest page с полной proposal и явным continuation offset; Complete не означает частичный import проекта.
type RepositoryBundlePublicationView struct {
	Proposal           RepositoryBundlePublication
	Files              []repositorybundle.Entry
	Offset, NextOffset int32
	Complete           bool
}
