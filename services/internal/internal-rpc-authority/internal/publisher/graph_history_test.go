package publisher

import (
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestSnapshotHistoryForBuildKeepsStableBoundaryWindow(t *testing.T) {
	t.Parallel()

	beforePersist := revisionHistory(1, 32)
	initial, err := snapshotHistoryForBuild(beforePersist, 33, "", false)
	if err != nil {
		t.Fatalf("initial history: %v", err)
	}
	afterPersist := revisionHistory(1, 33)
	restarted, err := snapshotHistoryForBuild(
		afterPersist,
		33,
		fmt.Sprintf("%064x", 33),
		true,
	)
	if err != nil {
		t.Fatalf("restart history: %v", err)
	}
	assertRevisionWindow(t, initial, 1, 32)
	assertRevisionWindow(t, restarted, 1, 32)
}

func TestSnapshotHistoryForBuildTrimsNewRevisionToLatestWindow(t *testing.T) {
	t.Parallel()

	history, err := snapshotHistoryForBuild(revisionHistory(1, 33), 34, "", false)
	if err != nil {
		t.Fatalf("new revision history: %v", err)
	}
	assertRevisionWindow(t, history, 2, 33)
}

func TestSnapshotHistoryForBuildRejectsMissingPersistedRevision(t *testing.T) {
	t.Parallel()

	if _, err := snapshotHistoryForBuild(
		revisionHistory(1, 32),
		33,
		fmt.Sprintf("%064x", 33),
		true,
	); err == nil {
		t.Fatal("missing persisted revision was accepted")
	}
}

func revisionHistory(first, last uint64) []model.RevisionDigest {
	result := make([]model.RevisionDigest, 0, last-first+1)
	for revision := first; revision <= last; revision++ {
		result = append(result, model.RevisionDigest{
			Revision:     revision,
			DigestSHA256: fmt.Sprintf("%064x", revision),
		})
	}
	return result
}

func assertRevisionWindow(
	t *testing.T,
	history []model.RevisionDigest,
	first uint64,
	last uint64,
) {
	t.Helper()
	if len(history) != int(last-first+1) {
		t.Fatalf("history length = %d, want %d", len(history), last-first+1)
	}
	if history[0].Revision != first || history[len(history)-1].Revision != last {
		t.Fatalf(
			"history revisions = %d..%d, want %d..%d",
			history[0].Revision,
			history[len(history)-1].Revision,
			first,
			last,
		)
	}
}
