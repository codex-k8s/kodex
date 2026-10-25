package app

import "testing"

// TestVerificationInspectionRequiresExactBackupID allows only the registered read command with a canonical backup locator and rejects missing or foreign-shaped input.
// TestVerificationInspectionRequiresExactBackupID допускает зарегистрированную команду чтения только с каноническим адресом копии и отклоняет пустой либо неверный вход.
func TestVerificationInspectionRequiresExactBackupID(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("BACKUP_CONTROLLER_RELEASE_REVISION", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	for _, id := range []string{"", "../foreign", "20261025T233026Z-430002386c6b43d1"} {
		t.Setenv("BACKUP_CONTROLLER_BACKUP_ID", id)
		_, err := loadConfig("inspect-verification")
		if (err == nil) != (id == "20261025T233026Z-430002386c6b43d1") {
			t.Fatal("inspection locator validation changed")
		}
	}
}
