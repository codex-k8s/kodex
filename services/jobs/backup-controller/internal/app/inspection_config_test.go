package app

import (
	"strings"
	"testing"
)

// TestInspectionCommandsRequireExactReleaseAndOwnerLocators checks the real CLI configuration registry without reading credential files or invoking storage.
// TestInspectionCommandsRequireExactReleaseAndOwnerLocators проверяет настоящий CLI-реестр без чтения credential files либо обращения к хранилищу.
func TestInspectionCommandsRequireExactReleaseAndOwnerLocators(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("BACKUP_CONTROLLER_RELEASE_REVISION", "sha256:"+strings.Repeat("a", 64))
	t.Setenv("BACKUP_CONTROLLER_BACKUP_ID", "20261025T000000Z-0123456789abcdef")
	t.Setenv("BACKUP_CONTROLLER_RESTORE_ID", "qa-restore-test")
	t.Setenv("BACKUP_CONTROLLER_EXPECTED_APPROVAL_ID", "qa-approval-test")
	t.Setenv("BACKUP_CONTROLLER_EXPECTED_TARGET_SHA256", "sha256:"+strings.Repeat("b", 64))
	for _, command := range []string{"list-verified", "inspect-backup", "inspect-restore-drill", "inspect-restore-targets", "inspect-restore-cleanup", "cleanup-restore-drill"} {
		configuration, err := loadConfig(command)
		if err != nil {
			t.Fatalf("registered command rejected: %s: %v", command, err)
		}
		configuration.ReleaseRevision = "sha256:" + strings.Repeat("0", 64)
		if configuration.validate(command) == nil {
			t.Fatal("placeholder release became executable")
		}
	}
	configuration, err := loadConfig("inspect-restore-drill")
	if err != nil {
		t.Fatal(err)
	}
	for _, defect := range []string{"backup", "restore", "approval", "target"} {
		invalid := configuration
		switch defect {
		case "backup":
			invalid.BackupID = "../foreign"
		case "restore":
			invalid.RestoreID = ""
		case "approval":
			invalid.ExpectedApprovalID = ""
		case "target":
			invalid.ExpectedTargetSHA256 = ""
		}
		if invalid.validate("inspect-restore-drill") == nil {
			t.Fatal("missing or unsafe owner locator accepted")
		}
	}
}
