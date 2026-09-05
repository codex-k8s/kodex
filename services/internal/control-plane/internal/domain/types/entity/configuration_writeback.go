package entity

import "time"

const (
	WriteBackWaiting     = "WAITING_APPROVAL"
	WriteBackQueued      = "QUEUED"
	WriteBackClaimed     = "CLAIMED"
	WriteBackStarted     = "EFFECT_STARTED"
	WriteBackSucceeded   = "SUCCEEDED"
	WriteBackRejected    = "REJECTED"
	WriteBackCancelled   = "CANCELLED"
	WriteBackExpired     = "EXPIRED"
	WriteBackFailed      = "FAILED"
	WriteBackUnknown     = "UNKNOWN_OUTCOME"
	WriteBackBranch      = "BRANCH"
	WriteBackPullRequest = "PULL_REQUEST"
)

type ConfigurationWriteBackAction struct {
	Action, Reason string
	Enabled        bool
}

// ConfigurationWriteBack не содержит private package или credential locator.
type ConfigurationWriteBack struct {
	Ref, ConfigurationRef, Kind, SourceRef, ConnectionRef                  string
	Version, ConfigurationVersion, SourceVersion, ConnectionVersion        int64
	RepositoryRef, SourceRefName, Path, BaseCommitSHA, BaseContentSHA256   string
	ProposedContentSHA256, ContentFormat, ProposalBranch, ApprovalDigest   string
	State, FailureCode, CandidateCommitSHA, PullRequestRef, PullRequestURL string
	CreatedAt, ExpiresAt                                                   time.Time
	ApprovedAt, CompletedAt, BranchConfirmedAt, PullRequestConfirmedAt     *time.Time
	NextActions                                                            []ConfigurationWriteBackAction
}

type ConfigurationWriteBackView struct {
	Proposal                     ConfigurationWriteBack
	BaseContent, ProposedContent string
}

type ConfigurationWriteBackLease struct {
	ProposalRef, Claimant, Fence string
	Attempt, ClaimGeneration     int64
	ExpiresAt                    time.Time
}

// ConfigurationWriteBackWork выдаётся только exact integration-gateway.
type ConfigurationWriteBackWork struct {
	Lease                                                            ConfigurationWriteBackLease
	Proposal                                                         ConfigurationWriteBack
	Mode, Effect                                                     string
	DefinitionKey, DefinitionVersion, DefinitionDigest               string
	DefinitionPackage, ProposedContent                               []byte
	PublicConfiguration                                              map[string]any
	CredentialRevision                                               IntegrationCredentialRevision
	EffectMarker, CommitMessage, CommitAuthorName, CommitAuthorEmail string
	CommitTime                                                       time.Time
	CandidateTreeSHA, CandidateBlobSHA                               string
	EffectStartedAt                                                  *time.Time
	Deadline                                                         time.Time
}
