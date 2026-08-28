package configspec

import "testing"

func TestFingerprintTargetsIsStableAndExcludesPasswords(t *testing.T) {
	t.Parallel()
	targets := RestoreTargets{SchemaVersion: 1, Databases: []RestoreDatabase{
		{Name: "authority", Host: "postgres.test", Port: 5432, AdminDatabase: "postgres", Database: "restore_authority", User: "restore", Password: "first", TLSMode: "verify-full", TLSServerName: "postgres.test", CAFile: "/ca.pem"},
		{Name: "control-plane", Host: "postgres.test", Port: 5432, AdminDatabase: "postgres", Database: "restore_control", User: "restore", Password: "second", TLSMode: "verify-full", TLSServerName: "postgres.test", CAFile: "/ca.pem"},
	}, ObjectStore: S3{Name: "restore", Endpoint: "https://s3.test", Region: "test", Bucket: "restore", AccessKeyID: "key", SecretAccessKey: "secret"}}
	first, err := FingerprintTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	targets.Databases[0], targets.Databases[1] = targets.Databases[1], targets.Databases[0]
	targets.Databases[0].Password = "changed"
	targets.ObjectStore.SecretAccessKey = "changed"
	second, err := FingerprintTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("fingerprint changed for order or credentials: %q != %q", first, second)
	}
	targets.Databases[0].Database = "another_target"
	third, err := FingerprintTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("fingerprint did not bind target database")
	}
}

func TestProductionRejectsPlaintextStorageAndDatabase(t *testing.T) {
	t.Parallel()
	store := S3{Name: "backup", Endpoint: "http://seaweedfs-s3.kodex-system.svc.cluster.local:8333",
		Region: "local", Bucket: "backup", AccessKeyID: "key", SecretAccessKey: "secret", AllowInsecureLocal: true}
	if err := store.Validate("production"); err == nil {
		t.Fatal("production accepted plaintext S3")
	}
	database := Database{Name: "control-plane", Host: "postgres.kodex-system.svc.cluster.local", Port: 5432,
		Database: "control_plane", User: "backup", Password: "secret", TLSMode: "disable", SchemaKind: "goose"}
	if err := database.Validate("production"); err == nil {
		t.Fatal("production accepted plaintext PostgreSQL")
	}
}
