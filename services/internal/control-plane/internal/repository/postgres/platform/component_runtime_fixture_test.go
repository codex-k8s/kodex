package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	serviceplatform "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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

// resolveSystemGrantComponentPrincipals resolves the synthetic SYSTEM-grant owner and then runtime
// claim worker using the exact existing actor/tenant/workload/operation inputs. Owner operation is
// fixed to system-assistant integration-grant changes, distinct from project-assistant and
// run-launch fixtures. It preserves resolvedTestPrincipal owner-claim behavior and returns the
// principals without creating a service, lease or external credential.
// resolveSystemGrantComponentPrincipals разрешает synthetic SYSTEM-grant owner, затем runtime
// claim worker с точными прежними actor/tenant/workload/operation inputs. Owner operation
// фиксирована на system-assistant integration-grant changes и отличается от project-assistant и
// run-launch fixtures. Сохраняет owner-claim поведение resolvedTestPrincipal и возвращает
// principals без создания service, lease или внешней credential.
func resolveSystemGrantComponentPrincipals(t *testing.T, ctx context.Context, r *Repository) (value.Principal, value.Principal) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.system-assistant.integration-grants.change"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	return owner, worker
}

// newProjectAssistantComponentFixture creates the existing synthetic repository, seeds its
// observed catalog, resolves the fixed project-assistant owner then runtime claim worker and
// constructs the domain service in that order. It preserves proof inputs, Fatal errors and the
// successful nil caller error; no SYSTEM-grant or ordinary launch owner is substituted. The
// caller owns its pool, Close, deadline and tracer and runs all commands/assertions itself.
// newProjectAssistantComponentFixture создаёт прежний synthetic repository, наполняет observed
// catalog, разрешает fixed project-assistant owner, затем runtime claim worker и создаёт domain
// service в таком порядке. Сохраняет proof inputs, Fatal errors и успешную nil caller error;
// SYSTEM-grant или ordinary launch owner не подставляется. Caller владеет pool, Close, deadline
// и tracer и самостоятельно выполняет все commands/assertions.
func newProjectAssistantComponentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*Repository, *serviceplatform.Service, value.Principal, value.Principal) {
	t.Helper()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	service, err := serviceplatform.New(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, service, owner, worker
}
