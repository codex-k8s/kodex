package runtime

import (
	"slices"
	"testing"
)

func TestDiagnoseSessionRetentionNegativeMatrixNeverDropsContainment(t *testing.T) {
	tests := []struct {
		name   string
		facts  SessionRetentionFacts
		reason SessionRetentionReason
	}{
		{name: "active", facts: SessionRetentionFacts{Active: true}, reason: SessionRetentionReasonActive},
		{name: "queued", facts: SessionRetentionFacts{Queued: true}, reason: SessionRetentionReasonQueued},
		{name: "approval", facts: SessionRetentionFacts{Approval: true}, reason: SessionRetentionReasonApproval},
		{name: "callback", facts: SessionRetentionFacts{Callback: true}, reason: SessionRetentionReasonCallback},
		{name: "no_archive", facts: SessionRetentionFacts{NoArchive: true}, reason: SessionRetentionReasonNoArchive},
		{name: "archive_failed", facts: SessionRetentionFacts{ArchiveFailed: true}, reason: SessionRetentionReasonArchiveFailed},
		{name: "grace", facts: SessionRetentionFacts{Grace: true}, reason: SessionRetentionReasonGrace},
		{name: "unknown_db", facts: SessionRetentionFacts{UnknownDB: true}, reason: SessionRetentionReasonUnknownDB},
		{name: "unknown_s3", facts: SessionRetentionFacts{UnknownS3: true}, reason: SessionRetentionReasonUnknownS3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostic := DiagnoseSessionRetention("session-1", 1, 1, test.facts)
			if diagnostic.SessionKey != "session-1" || diagnostic.PVCsInventoried != 1 || diagnostic.SecretsInventoried != 1 {
				t.Fatalf("diagnostic = %#v", diagnostic)
			}
			if len(diagnostic.Reasons) == 0 || diagnostic.Reasons[0] != SessionRetentionReasonContainment {
				t.Fatalf("containment reason is missing: %#v", diagnostic.Reasons)
			}
			if !slices.Contains(diagnostic.Reasons, test.reason) {
				t.Fatalf("diagnostic reason %q is missing: %#v", test.reason, diagnostic.Reasons)
			}
		})
	}

	diagnostic := DiagnoseSessionRetention("session-2", 0, 0, SessionRetentionFacts{})
	if !slices.Equal(diagnostic.Reasons, []SessionRetentionReason{SessionRetentionReasonContainment}) {
		t.Fatalf("empty facts opened containment: %#v", diagnostic.Reasons)
	}
}
