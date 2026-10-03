package platform

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Только disposable owner: файл связывает настоящий snapshot, transport и controller.
func TestAssistantWarmRuntimeWireComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1",
		ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
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
