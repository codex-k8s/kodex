package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestProjectPurgeRecoveryRejectsForgedStagingReason prevents a read-only KUBE blocker from being
// advertised as a generic owner abandonment command, a removed project or a receipt without objects.
// TestProjectPurgeRecoveryRejectsForgedStagingReason запрещает выдавать read-only KUBE blocker за
// generic owner abandonment command, удалённый проект либо receipt без незавершённых объектов.
func TestProjectPurgeRecoveryRejectsForgedStagingReason(t *testing.T) {
	for _, test := range []struct {
		name, state, reason    string
		count                  int32
		present, resolve, want bool
	}{
		{"manual", "BLOCKED", errs.ArtifactPurgeReconciliationReason, 1, true, false, true},
		{"manual-abandon", "WAITING_OWNER", errs.ArtifactPurgeReconciliationReason, 1, true, true, false},
		{"blocked", "BLOCKED", managedkubernetes.ProjectStagingReconciliationReason, 1, true, false, true},
		{"unknown reason", "BLOCKED", "KUBERNETES_ANYTHING", 1, true, false, false},
		{"resolve", "WAITING_OWNER", managedkubernetes.ProjectStagingReconciliationReason, 1, true, true, false},
		{"absent", "BLOCKED", managedkubernetes.ProjectStagingReconciliationReason, 1, false, false, false},
		{"empty", "BLOCKED", managedkubernetes.ProjectStagingReconciliationReason, 0, true, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			version := int64(0)
			if test.present {
				version = 1
			}
			raw, err := json.Marshal(map[string]any{"projectRef": "prj_disposable", "version": 1, "snapshotDigest": strings.Repeat("a", 64), "state": test.state, "reasonCode": test.reason, "unknownContentCount": test.count, "projectPresent": test.present, "projectVersion": version, "canResolve": test.resolve})
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeProjectPurgeRecovery(raw)
			if (err == nil) != test.want {
				t.Fatal("forged blocker boundary", err)
			}
		})
	}
}
