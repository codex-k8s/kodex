package runtime

import "strings"

type SessionDataRetentionMode string

const SessionDataRetentionModeInventoryOnly SessionDataRetentionMode = "inventory-only"

type SessionRetentionReason string

const (
	SessionRetentionReasonContainment   SessionRetentionReason = "containment"
	SessionRetentionReasonActive        SessionRetentionReason = "active"
	SessionRetentionReasonQueued        SessionRetentionReason = "queued"
	SessionRetentionReasonApproval      SessionRetentionReason = "approval"
	SessionRetentionReasonCallback      SessionRetentionReason = "callback"
	SessionRetentionReasonNoArchive     SessionRetentionReason = "no_archive"
	SessionRetentionReasonArchiveFailed SessionRetentionReason = "archive_failed"
	SessionRetentionReasonGrace         SessionRetentionReason = "grace"
	SessionRetentionReasonUnknownDB     SessionRetentionReason = "unknown_db"
	SessionRetentionReasonUnknownS3     SessionRetentionReason = "unknown_s3"
)

type SessionRetentionFacts struct {
	Active        bool
	Queued        bool
	Approval      bool
	Callback      bool
	NoArchive     bool
	ArchiveFailed bool
	Grace         bool
	UnknownDB     bool
	UnknownS3     bool
}

type SessionRetentionDiagnostic struct {
	SessionKey         string
	PVCsInventoried    int
	SecretsInventoried int
	Reasons            []SessionRetentionReason
}

func DiagnoseSessionRetention(sessionKey string, pvcs int, secrets int, facts SessionRetentionFacts) SessionRetentionDiagnostic {
	reasons := []SessionRetentionReason{SessionRetentionReasonContainment}
	for _, item := range []struct {
		applies bool
		reason  SessionRetentionReason
	}{
		{facts.Active, SessionRetentionReasonActive},
		{facts.Queued, SessionRetentionReasonQueued},
		{facts.Approval, SessionRetentionReasonApproval},
		{facts.Callback, SessionRetentionReasonCallback},
		{facts.NoArchive, SessionRetentionReasonNoArchive},
		{facts.ArchiveFailed, SessionRetentionReasonArchiveFailed},
		{facts.Grace, SessionRetentionReasonGrace},
		{facts.UnknownDB, SessionRetentionReasonUnknownDB},
		{facts.UnknownS3, SessionRetentionReasonUnknownS3},
	} {
		if item.applies {
			reasons = append(reasons, item.reason)
		}
	}
	return SessionRetentionDiagnostic{
		SessionKey:         strings.TrimSpace(sessionKey),
		PVCsInventoried:    max(pvcs, 0),
		SecretsInventoried: max(secrets, 0),
		Reasons:            reasons,
	}
}
