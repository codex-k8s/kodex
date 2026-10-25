package platform

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRoleImageAdmissionMaintenanceComponent allocates an isolated migrated database for issue1797
// under a ninety-second context, configures synthetic image policy/keys and bootstraps before
// running stale-policy maintenance. The isolation helper skips without its disposable runtime DSN;
// no production TTL or registry is used.
// TestRoleImageAdmissionMaintenanceComponent выделяет изолированную мигрированную БД для issue1797
// с контекстом на девяносто секунд, настраивает синтетические image policy/keys и выполняет
// bootstrap перед stale-policy maintenance. Isolation helper пропускает тест без disposable
// runtime DSN; production TTL и registry не используются.
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
	if err := repository.ConfigureRoleImages(syntheticRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	testRoleImageAdmissionPolicyRotation(t, ctx, repository)
}
