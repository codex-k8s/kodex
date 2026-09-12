package publisher

import (
	"strings"
	"testing"
)

func TestPromoteSnapshotUsesExactRequiredTargets(t *testing.T) {
	t.Parallel()

	for _, parameter := range []string{
		"@expected_workload_ids",
		"@expected_roles",
		"@expected_generations",
	} {
		if !strings.Contains(promoteSnapshotSQL, parameter) {
			t.Fatalf("publisher promote query omits %s", parameter)
		}
	}
	if strings.Contains(
		promoteSnapshotSQL,
		"authority_snapshot_readbacks\nWHERE source_revision",
	) {
		t.Fatal("publisher promote query still counts every snapshot readback")
	}
}

func TestLoadSnapshotHistoryIncludesCurrentRevisionForRestart(t *testing.T) {
	t.Parallel()

	if !strings.Contains(loadSnapshotHistorySQL, "LIMIT 33") {
		t.Fatal("snapshot history query must retain current revision plus 32 predecessors")
	}
}

func TestLoadSnapshotPredecessorUsesPersistedCrossDomainProvenance(t *testing.T) {
	t.Parallel()
	for _, required := range []string{
		"publisher_load_snapshot_predecessor",
		"@source_revision",
		"@source_digest_sha256",
		"@registry_digest_sha256",
	} {
		if !strings.Contains(loadSnapshotPredecessorSQL, required) {
			t.Fatalf("snapshot predecessor query omits %s", required)
		}
	}
}
