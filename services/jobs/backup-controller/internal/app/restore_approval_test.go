package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
)

// TestRestoreApprovalExpirySeparatesReadbackFromMutations exercises the real protected-file decoder and closed command dispatch: expired historical inspection works, while cleanup/restore remain denied and malformed pins fail before storage.
// TestRestoreApprovalExpirySeparatesReadbackFromMutations проверяет настоящий protected-file decoder и закрытый dispatch: истёкшая историческая запись доступна inspection, cleanup/restore запрещены, повреждённые pins отклоняются до хранилища.
func TestRestoreApprovalExpirySeparatesReadbackFromMutations(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	base := configspec.RestoreApproval{SchemaVersion: 1, ApprovalID: "qa-approval-test", RestoreID: "qa-restore-test",
		BackupID: "20261025T000000Z-0123456789abcdef", TargetSetSHA256: "sha256:" + strings.Repeat("a", 64), ExpiresAt: now.Add(-time.Hour)}
	for _, defect := range []string{"", "fresh", "future", "schema", "approval", "restore", "backup", "target", "missing-expiry"} {
		t.Run(defect, func(t *testing.T) {
			value := base
			switch defect {
			case "fresh":
				value.ExpiresAt = now.Add(time.Hour)
			case "future":
				value.ExpiresAt = now.Add(48 * time.Hour)
			case "schema":
				value.SchemaVersion = 0
			case "approval":
				value.ApprovalID = ""
			case "restore":
				value.RestoreID = "../foreign"
			case "backup":
				value.BackupID = "../foreign"
			case "target":
				value.TargetSetSHA256 = ""
			case "missing-expiry":
				value.ExpiresAt = time.Time{}
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "approval.json")
			if err := os.WriteFile(file, body, 0400); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"inspect-restore-targets", "inspect-restore-cleanup"} {
				decoded, err := loadRestoreTargetApproval(command, file, now)
				if (err == nil) != (defect == "" || defect == "fresh" || defect == "future") {
					t.Fatalf("historical %s acceptance mismatch: %v", command, err)
				}
				if err == nil && decoded != value {
					t.Fatal("historical pins were rewritten")
				}
			}
			if _, err := loadRestoreTargetApproval("cleanup-restore-drill", file, now); (err == nil) != (defect == "fresh") {
				t.Fatal("mutation freshness boundary is invalid")
			}
			if _, err := configspec.LoadRestoreApproval(file, now); (err == nil) != (defect == "fresh") {
				t.Fatal("restore freshness boundary is invalid")
			}
			if _, err := loadRestoreTargetApproval("unregistered-command", file, now); err == nil {
				t.Fatal("unknown command acquired historical decoder")
			}
		})
	}
}
