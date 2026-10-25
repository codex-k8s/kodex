package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newSyntheticRuntimeComponentRepository creates the fixed synthetic openai-codex/gpt-5 repository
// with in-memory object storage, configures the existing provider descriptor and role-image
// policy, then bootstraps using the exact caller context. Literal digests/registry refs, fresh
// signing-key allocation and original Fatal errors are preserved. The caller owns the isolated
// pool, deadline, seeding, tracer and principals; this helper neither reads credentials nor calls
// an external provider. On success the original caller error remains nil, as it was after all four
// successful steps.
// newSyntheticRuntimeComponentRepository создаёт Repository фиксированного синтетического
// openai-codex/gpt-5 с in-memory object storage, настраивает прежний provider descriptor и
// role-image policy, затем выполняет Bootstrap с точным context вызывающего. Сохранены literal
// digests/registry refs, новое выделение signing-key slice и исходные Fatal errors. Caller владеет
// isolated pool, deadline, seed, tracer и principals; helper не читает credentials и не вызывает
// внешнего provider. При успехе исходная caller error остаётся nil, как после всех четырёх
// успешных шагов.
func newSyntheticRuntimeComponentRepository(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *Repository {
	t.Helper()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err = r.ConfigureRoleImages(RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}); err != nil {
		t.Fatal(err)
	}
	if err = r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	return r
}
