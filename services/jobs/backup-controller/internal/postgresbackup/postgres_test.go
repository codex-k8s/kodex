package postgresbackup

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
)

// TestSupportedServerVersion pins the accepted major releases and fixed six-digit server-version format.
// RU: TestSupportedServerVersion закрепляет допустимые основные версии и шестизначный формат номера сервера.
func TestSupportedServerVersion(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"170010", "180003"} {
		if !supportedServerVersion(value) {
			t.Fatalf("supportedServerVersion(%q) = false", value)
		}
	}
	for _, value := range []string{"160010", "190001", "18", "invalid"} {
		if supportedServerVersion(value) {
			t.Fatalf("supportedServerVersion(%q) = true", value)
		}
	}
}

// TestDatabaseEnvironmentUsesExactTLSNameAndConnectAddress keeps verification identity distinct from routing.
// RU: TestDatabaseEnvironmentUsesExactTLSNameAndConnectAddress отделяет проверяемую идентичность от адреса маршрута.
func TestDatabaseEnvironmentUsesExactTLSNameAndConnectAddress(t *testing.T) {
	t.Parallel()
	database := configspec.Database{Host: "10.0.0.10", Port: 5432, User: "backup", TLSMode: "verify-full",
		TLSServerName: "postgres.example.test", CAFile: "/ca.pem"}
	environment := databaseEnvironment(database, "/pgpass", "control_plane")
	for _, expected := range []string{"PGHOST=postgres.example.test", "PGHOSTADDR=10.0.0.10", "PGSSLMODE=verify-full"} {
		if !slices.Contains(environment, expected) {
			t.Fatalf("database environment does not contain %q: %#v", expected, environment)
		}
	}
}

// TestWritePGPassEscapesFieldsAndUsesOwnerOnlyMode checks separator escaping and private credential-file mode.
// RU: TestWritePGPassEscapesFieldsAndUsesOwnerOnlyMode проверяет экранирование разделителей и закрытый режим файла.
func TestWritePGPassEscapesFieldsAndUsesOwnerOnlyMode(t *testing.T) {
	t.Parallel()
	file, err := writePGPass(t.TempDir(), "postgres.test", 5432, "db", "user", `pa:ss\word`)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %04o", info.Mode().Perm())
	}
	payload, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `pa\:ss\\word`) {
		t.Fatalf("pgpass escaping is invalid: %q", payload)
	}
}
