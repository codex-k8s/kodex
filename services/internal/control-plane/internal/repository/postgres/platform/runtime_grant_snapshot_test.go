package platform

import (
	fixture "github.com/codex-k8s/kodex/services/internal/control-plane/testdata/runtime-snapshot"
	"testing"
)

func TestRuntimeGrantSnapshotOwnerDigestMatchesWireFixture(t *testing.T) {
	values := fixture.Snapshot(t)
	want := stringMap(values, "revisionDigest")
	got, err := runtimeRevisionDigestFromSnapshot(values)
	if err != nil || got != want {
		t.Fatalf("owner snapshot digest does not match complete wire fixture: err=%v", err)
	}
	for _, key := range []string{"definitionVersion", "definitionDigest", "operation", "inputSchema", "inputSchemaSha256"} {
		t.Run(key, func(t *testing.T) {
			changed := fixture.Snapshot(t)
			changed["integrationGrants"].([]map[string]string)[0][key] += "changed"
			digest, err := runtimeRevisionDigestFromSnapshot(changed)
			if err == nil && digest == want {
				t.Fatal("grant pin was omitted from owner digest")
			}
		})
	}
}

// Проверяет настоящий ClaimExecution из disposable PG, включая оба grants.
func testClaimedIntegrationGrantDigestPins(t *testing.T, snapshot map[string]any) {
	t.Helper()
	expected := stringMap(snapshot, "revisionDigest")
	got, err := runtimeRevisionDigestFromSnapshot(snapshot)
	if err != nil || got != expected {
		t.Fatal("claimed owner digest differs from its snapshot")
	}
	grants, ok := snapshot["integrationGrants"].([]map[string]string)
	if !ok || len(grants) == 0 {
		t.Fatal("claimed integration grants missing")
	}
	for _, grant := range grants {
		for _, key := range []string{"definitionVersion", "definitionDigest", "operation", "inputSchema", "inputSchemaSha256"} {
			previous := grant[key]
			if previous == "" {
				t.Fatalf("claimed grant pin missing: %s", key)
			}
			grant[key] = "changed"
			changed, err := runtimeRevisionDigestFromSnapshot(snapshot)
			grant[key] = previous
			if err == nil && changed == expected {
				t.Fatalf("claimed grant pin not bound: %s", key)
			}
		}
	}
}
