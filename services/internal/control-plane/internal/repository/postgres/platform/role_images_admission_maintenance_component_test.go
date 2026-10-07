package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Issue #1797: пустая очередь current-policy не скрывает stale owner maintenance.
func TestRoleImageAdmissionMaintenanceComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated admission maintenance fixture")
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute,
		AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging",
		PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64),
		LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	testRoleImageAdmissionPolicyRotation(t, ctx, repository)
}
