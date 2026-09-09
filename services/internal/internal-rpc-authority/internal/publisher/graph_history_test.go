package publisher

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

type rotationSecretDelivery struct {
	material domainrepository.SecretMaterial
	writes   int
}

func (delivery *rotationSecretDelivery) ReadVersioned(context.Context, string) (domainrepository.SecretMaterial, bool, error) {
	return delivery.material, true, nil
}

func (delivery *rotationSecretDelivery) CreateVersioned(context.Context, string, map[string]string) (domainrepository.SecretMaterial, error) {
	delivery.writes++
	return domainrepository.SecretMaterial{}, nil
}

func (delivery *rotationSecretDelivery) WriteVersionedCAS(context.Context, string, uint64, map[string]string) (domainrepository.SecretMaterial, error) {
	delivery.writes++
	return domainrepository.SecretMaterial{}, nil
}

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

func TestKeyRotationRejectsAValidButUnknownPredecessorDigestBeforeCAS(t *testing.T) {
	t.Parallel()
	current, err := internalrpcauth.GenerateES256Key("rotation-current")
	if err != nil {
		t.Fatal("generate current key")
	}
	next, err := internalrpcauth.GenerateES256Key("rotation-next")
	if err != nil {
		t.Fatal("generate next key")
	}
	predecessorDigest := strings.Repeat("a", 64)
	graph := &Graph{config: GraphConfig{Registry: model.DeliveryTargetRegistry{
		SourceRevision: 2,
		SourceDigest:   strings.Repeat("b", 64),
	}}}
	data, err := graph.keySetData(current, next, nil, 1, 2, 0)
	if err != nil {
		t.Fatal("encode previous key set")
	}
	data["source_revision"] = "1"
	data["source_digest_sha256"] = strings.Repeat("c", 64)
	delivery := &rotationSecretDelivery{material: domainrepository.SecretMaterial{
		Version: 1,
		Digest:  strings.Repeat("d", 64),
		Data:    data,
	}}
	graph.config.Secrets = delivery
	if _, err := graph.ensureKeySet(t.Context(), "secret", "rotation", 1, predecessorDigest); err == nil {
		t.Fatal("unknown predecessor digest was accepted")
	}
	if delivery.writes != 0 {
		t.Fatal("unknown predecessor reached external CAS")
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
