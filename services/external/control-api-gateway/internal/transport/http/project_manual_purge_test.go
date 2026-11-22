package httptransport

import (
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestManualPurgeRecoveryRejectsAbandonment keeps unresolved irreversible IO read-only and hides
// the manual reason when a forged projection claims missing project or owner resolution authority.
// TestManualPurgeRecoveryRejectsAbandonment сохраняет unresolved irreversible IO read-only и
// отклоняет forged projection с отсутствующим project либо owner resolution authority.
func TestManualPurgeRecoveryRejectsAbandonment(t *testing.T) {
	valid := &cp.ProjectPurgeRecovery{ProjectRef: "prj_disposable", Version: 1, SnapshotDigest: strings.Repeat("a", 64), State: "BLOCKED", UnknownContentCount: 1, ProjectPresent: true, ProjectVersion: 1, ReasonCode: artifactPurgeReconciliationReason}
	if _, ok := projectPurgeRecoveryView(valid.ProjectRef, valid); !ok {
		t.Fatal("manual reconciliation has no safe readback")
	}
	valid.CanResolve = true
	if _, ok := projectPurgeRecoveryView(valid.ProjectRef, valid); ok {
		t.Fatal("manual reconciliation became an abandonment command")
	}
	valid.CanResolve = false
	valid.ProjectPresent = false
	valid.ProjectVersion = 0
	if _, ok := projectPurgeRecoveryView(valid.ProjectRef, valid); ok {
		t.Fatal("manual reconciliation lost the original project eligibility")
	}
}
