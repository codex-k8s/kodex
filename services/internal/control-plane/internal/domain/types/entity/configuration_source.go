package entity

import "time"

const (
	ConfigurationSourceQueued      = "QUEUED"
	ConfigurationSourceClaimed     = "CLAIMED"
	ConfigurationSourceReady       = "READY"
	ConfigurationSourceBlocked     = "SYNC_BLOCKED"
	ConfigurationSourceDetached    = "DETACHED"
	ConfigurationSourceUnavailable = "UNAVAILABLE"
	ConfigurationSourceCredential  = "CREDENTIAL_REJECTED"
	ConfigurationSourceAccess      = "ACCESS_DENIED"
	ConfigurationSourceNotFound    = "NOT_FOUND"
	ConfigurationSourceDiverged    = "DIVERGED"
	ConfigurationSourceContent     = "CONTENT_INVALID"
	ConfigurationSourceResponse    = "RESPONSE_INVALID"
)

// ManagedConfigurationGitSource — публичное состояние без private descriptor.
type ManagedConfigurationGitSource struct {
	Ref, ConnectionRef, ProviderKey, RepositoryRef, RefName, Path string
	State, AcceptedCommitSHA, AcceptedContentSHA256               string
	AcceptedRevisionRef, FailureCode                              string
	Version, Generation                                           int64
	SyncedAt                                                      *time.Time
}

type ManagedConfigurationSourceLease struct {
	WorkRef, Claimant, Fence                   string
	SourceGeneration, Attempt, ClaimGeneration int64
	ExpiresAt                                  time.Time
}

// ManagedConfigurationSourceWork выдаётся только exact integration-gateway.
type ManagedConfigurationSourceWork struct {
	Lease                                                          ManagedConfigurationSourceLease
	SourceRef, ConfigurationRef, Kind, ConnectionRef               string
	DefinitionKey, DefinitionVersion, DefinitionDigest             string
	RepositoryRef, RefName, Path, PreviousCommitSHA, ContentFormat string
	ConnectionVersion                                              int64
	MaximumContentBytes                                            int32
	Deadline                                                       time.Time
	PublicConfiguration                                            map[string]any
	CredentialRevision                                             IntegrationCredentialRevision
	DefinitionPackage                                              []byte
}
