package entity

import (
	"encoding/json"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
)

type EmailMailboxDiagnostic struct {
	Code, Path, Message string
	Line, Column        int32
}
type EmailMailboxPublication struct {
	Ref, Digest, State, ConfigurationRevisionRef, FailureCode string
	Revision                                                  int64
	CreatedAt, ReadyAt                                        time.Time
}
type EmailMailboxConfigurationView struct {
	ConnectionRef, MailboxRef, BoundRevisionRef string
	ConnectionVersion                           int64
	Configuration                               ManagedConfigurationSet
	Revision                                    ManagedConfigurationRevision
	Specification                               EmailMailboxSpecification
	Publication                                 *EmailMailboxPublication
	Diagnostics                                 []EmailMailboxDiagnostic
	NextActions                                 []EmailMailboxActionAvailability
}
type EmailMailboxPage struct {
	Items         []EmailMailboxConfigurationView
	Total         int64
	NextPageToken string
	NextActions   []EmailMailboxActionAvailability
}

type EmailMailboxActionAvailability struct {
	Action, Reason string
	Enabled        bool
}
type EmailMailboxPreview struct {
	Specification *EmailMailboxSpecification
	CanonicalYAML string
	Diagnostics   []EmailMailboxDiagnostic
	Valid         bool
}

// EmailMailboxPublicationWork принадлежит локальному CP reconciler, не public API.
type EmailMailboxPublicationWork struct {
	Ref, Claimant, State string
	ClaimGeneration      int64
	Configuration        api.Configuration
	PolicyDocument       json.RawMessage
	Applied, Callback    bool
	ExpiresAt            time.Time
}
