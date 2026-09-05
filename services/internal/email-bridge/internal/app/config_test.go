package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseTransport(t *testing.T) {
	const valid = "postgresql://email_bridge_runtime@" + "email-bridge-postgresql.kodex-system.svc.cluster.local:5432/email_bridge?sslmode=verify-full&sslrootcert=/var/run/email/tls/ca.crt"
	for _, suffix := range []string{"", "&host=foreign.example.test", "&sslmode=disable", "&user=postgres", "&service=override", "&sslrootcert=/other", "&hostaddr=127.0.0.1"} {
		path := filepath.Join(t.TempDir(), "dsn")
		if err := os.WriteFile(path, []byte(valid+suffix), 0440); err != nil {
			t.Fatal(err)
		}
		_, err := databaseDSN(path)
		if (err == nil) != (suffix == "") {
			t.Fatal("database transport validation mismatch")
		}
	}
}
