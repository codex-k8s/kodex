package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newRoleImageAdmissionComponentFixture prepares the existing terminal/expiry fixtures in their
// original order: repository with synthetic object storage and model defaults, native role-image
// configuration, bootstrap, gateway-bound recipe owner proof, resolved principal and service.
// Each failure remains fatal. On success the caller's already nil pool error stays unchanged.
// The caller retains its ninety-second context, isolated database and deferred pool Close;
// B1/B2 commands, claims, terminal/expiry callbacks and every independent assertion stay there.
// No image-admission/controller grant or real provider/registry connection is introduced.
// newRoleImageAdmissionComponentFixture готовит прежние terminal/expiry fixtures в исходном
// порядке: repository с synthetic object storage и model defaults, native role-image config,
// bootstrap, gateway-bound proof владельца recipes, resolved principal и service. Каждый отказ
// остаётся Fatal; при успехе уже nil pool error вызывающего не меняется. Caller сохраняет свой
// context на девяносто секунд, isolated database и defer pool.Close; B1/B2 commands, claims,
// terminal/expiry callbacks и все независимые проверки остаются там. Image-admission/controller
// grant либо соединение с настоящим provider/registry здесь не создаются.
func newRoleImageAdmissionComponentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*Repository, *platformservice.Service, value.Principal, value.Principal) {
	t.Helper()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(syntheticRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.role-images.recipes.manage"}, "control-api-gateway")
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, s, owner, resolved
}
