package restore

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// TestRestoreFenceArgsPreservesNullableWindow checks zero/positive/negative
// Unix timestamps, exact five-field bindings, independent maps and both named
// SQL rewrites. No fence is applied and no PostgreSQL connection is opened.
// TestRestoreFenceArgsPreservesNullableWindow проверяет нулевое, положительное
// и отрицательное время Unix, точные пять привязок, независимые карты и rewrite
// обоих именованных SQL. Ограждение не применяется, подключение к PostgreSQL не открывается.
func TestRestoreFenceArgsPreservesNullableWindow(t *testing.T) {
	t.Parallel()
	for _, timestamp := range []int64{0, 123456789, -1} {
		t.Run(time.Unix(timestamp, 0).UTC().Format(time.RFC3339), func(t *testing.T) {
			state := model.RestoreState{
				DatabaseClusterID: "fixture-cluster", RestoreEpoch: 7,
				Phase: "QUIESCING", EvidenceDigest: "fixture-evidence",
				SafeWindowNotBefore: timestamp,
			}
			args := restoreFenceArgs(state)
			if len(args) != 5 || args["database_cluster_id"] != "fixture-cluster" ||
				args["restore_epoch"] != uint64(7) || args["phase"] != "QUIESCING" ||
				args["evidence_digest_sha256"] != "fixture-evidence" {
				t.Fatal("restore fence binding changed")
			}
			window, ok := args["safe_window_not_before"].(*time.Time)
			if !ok || timestamp == 0 && window != nil || timestamp != 0 &&
				(window == nil || !window.Equal(time.Unix(timestamp, 0)) || window.Location() != time.UTC) {
				t.Fatal("restore safe-window nullability or UTC conversion changed")
			}
			for _, query := range []string{applyFenceSQL, fenceReadinessSQL} {
				if _, _, err := args.RewriteQuery(context.Background(), nil, query, nil); err != nil {
					t.Fatalf("restore fence argument rewrite: %v", err)
				}
			}
			args["database_cluster_id"] = "changed"
			if restoreFenceArgs(state)["database_cluster_id"] != "fixture-cluster" {
				t.Fatal("restore fence argument maps share mutable storage")
			}
		})
	}
}
