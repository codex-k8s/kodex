package platform

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAssistantWarmRuntimeWireComponent binds the actual owner snapshot,
// transport projection and warm controller in a disposable database while
// asserting immutable image and instruction pins without a live agent.
// RU: TestAssistantWarmRuntimeWireComponent связывает настоящий owner
// snapshot, transport-проекцию и warm controller в disposable БД, проверяя
// неизменные image/instruction pins без живого агента.
func TestAssistantWarmRuntimeWireComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.reconcile",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, snapshot, _, err := service.ReconcileWarmRuntime(ctx, worker, "warm-wire-synthetic-worker")
	if err != nil {
		t.Fatal(err)
	}
	image, ok := snapshot["environmentImage"].(runtimecontract.RuntimeEnvironmentImage)
	promptSnapshot, promptOK := snapshot["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	if !promptOK {
		t.Fatal("owner warm snapshot lost immutable instruction input")
	}
	assertPlatformInstructions(t, promptSnapshot)
	generation, validGeneration := snapshot["roleImageRecipeGeneration"].(int64)
	if !ok || image.ArtifactRef == "" || image.RecipeRef == "" || image.RecipeGeneration < 1 ||
		stringMap(snapshot, "roleImageArtifactRef") != image.ArtifactRef || stringMap(snapshot, "roleImageRecipeRef") != image.RecipeRef ||
		!validGeneration || generation != image.RecipeGeneration {
		t.Fatal("owner warm snapshot lost canonical admitted image pins")
	}
	digest := stringMap(snapshot, "revisionDigest")
	actual, err := runtimeRevisionDigestFromSnapshot(snapshot)
	if err != nil || actual != digest || digest == "" || assistant.DesiredRuntimeRevision != stringMap(snapshot, "runtimeRevisionRef") {
		t.Fatal("owner warm revision does not match its immutable digest")
	}
	for _, key := range []string{"roleImageArtifactRef", "roleImageRecipeRef", "roleImageRecipeGeneration"} {
		if snapshot[key] == nil {
			t.Fatalf("owner transport image pin missing: %s", key)
		}
	}
	// isolatedAssistantComponentDSN уже закрыл любые shared/live DSN до экспорта.
	if output := os.Getenv("KODEX_TEST_WARM_OWNER_SNAPSHOT_PATH"); output != "" {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("create synthetic owner snapshot output failed")
		}
		_, writeErr := file.Write(raw)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal("write synthetic owner snapshot output failed")
		}
	}
}
