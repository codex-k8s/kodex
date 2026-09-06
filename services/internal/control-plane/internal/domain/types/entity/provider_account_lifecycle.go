package entity

import "time"

type ProviderAccountBlockerCount struct {
	Kind  string
	Total int64
}

type ProviderAccountDeletion struct {
	Ref, State, SafeReason  string
	Version, PendingCleanup int64
	Blockers                []ProviderAccountBlockerCount
	RequestedAt             time.Time
	CompletedAt             *time.Time
}

type ProviderAccountVerification struct {
	Ref, State, Scope, SafeReason      string
	AccountVersion, CredentialRevision int64
	RequestedAt                        time.Time
	CompletedAt                        *time.Time
}

type ProviderAccountBlocker struct {
	Kind, Ref, Name, ProjectRef string
	Version                     int64
	CanCancel                   bool
}

type ProviderAccountBlockerPage struct {
	Items                                                     []ProviderAccountBlocker
	Total, HiddenCount, AccountVersion, DeletionIntentVersion int64
	NextPageToken, ContextDigest                              string
}

type ProviderQueuedWorkCancellationResult struct {
	RunRef, Outcome string
}

type ProviderQueuedWorkCancellation struct {
	Account ProviderAccount
	Results []ProviderQueuedWorkCancellationResult
}
