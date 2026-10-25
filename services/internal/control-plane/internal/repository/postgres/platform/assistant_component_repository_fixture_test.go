package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newConfiguredAssistantComponentRepository creates one repository adapter
// with the exact synthetic provider credential and image policy shared by
// assistant scope, parallel-lifecycle, warm-wire and organization-environment
// component tests. The caller selects the model; storage is not bootstrapped here so
// an adapter can be reconstructed after a restart.
// RU: newConfiguredAssistantComponentRepository создаёт adapter с точными
// синтетическими credential провайдера и image policy для component-тестов
// scope, параллельного lifecycle, warm-wire и окружения организации. Модель выбирает
// caller; хранилище здесь не инициализируется, чтобы adapter можно было
// заново создать после перезапуска.
func newConfiguredAssistantComponentRepository(t *testing.T, pool *pgxpool.Pool, model string) *Repository {
	t.Helper()
	repository, err := New(pool, "openai-codex", model, objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(namespacedRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
	return repository
}

// newReadyAssistantComponentService bootstraps the shared gpt-5 synthetic repository, prepares observed warm evidence and resolves the same owner/claim/warm-report principals in their original order before reporting the current assistant revision READY. Caller-owned disposable pool/context cleanup stays outside; no real provider, Pod or agent starts here.
// newReadyAssistantComponentService инициализирует общий синтетический репозиторий gpt-5, готовит наблюдаемые warm evidence и разрешает прежних owner/claim/warm-report principals в исходном порядке перед отчётом READY текущей ревизии помощника. Очистка disposable pool/context остаётся у вызывающего; настоящий провайдер, Pod и агент здесь не запускаются.
func newReadyAssistantComponentService(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*Repository, *platformservice.Service, value.Principal, value.Principal, entity.SystemAssistant) {
	t.Helper()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		t.Helper()
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation,
		}, workload)
	}
	owner := principal("control-api-gateway", "platform.assistant.turns.add", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	warmWorker := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY",
	}); err != nil {
		t.Fatal(err)
	}
	return repository, service, owner, worker, assistant
}
