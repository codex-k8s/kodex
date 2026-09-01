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
