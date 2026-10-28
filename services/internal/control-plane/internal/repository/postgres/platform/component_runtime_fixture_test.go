package platform

import (
	"context"
	"strings"
	"testing"

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
	configureSyntheticRuntimeComponentRepository(t, r)
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

// newRunComponentFixture prepares the existing synthetic repository, the caller-selected warm or
// ordinary catalog, exact run-launch principals and the domain service in their original order.
// The caller owns pool, deadline, errors and all scenario assertions; no provider is contacted.
// newRunComponentFixture готовит прежний synthetic repository, выбранный вызывающим warm или
// обычный каталог, точные principals запуска и domain service в исходном порядке. Вызывающий
// владеет pool, сроком, обработкой ошибок и всеми проверками сценария; provider не вызывается.
func newRunComponentFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, warm bool) (*Repository, *serviceplatform.Service, value.Principal, value.Principal, error) {
	t.Helper()
	r := newSyntheticRuntimeComponentRepository(t, ctx, pool)
	if warm {
		prepareObservedWarmFixture(t, ctx, r)
	} else {
		seedObservedCatalogFixture(t, ctx, r)
	}
	owner, worker := resolveRunLaunchComponentPrincipals(t, ctx, r)
	service, err := serviceplatform.New(r)
	return r, service, owner, worker, err
}

// configureSyntheticRuntimeComponentRepository applies the fixed synthetic provider descriptor and
// role-image policy to an existing test repository in that order, allocating a fresh signing-key
// slice each time. Both original guarded errors remain fatal. It does not create storage, read
// credentials, bootstrap PostgreSQL, resolve principals or own the caller lifecycle.
// configureSyntheticRuntimeComponentRepository применяет фиксированный синтетический дескриптор
// провайдера и политику образов ролей к существующему тестовому репозиторию в этом порядке, каждый
// раз выделяя новый массив ключа подписи. Обе исходные ошибки приводят к Fatal. Не создаёт
// хранилище, не читает учётные данные, не инициализирует PostgreSQL, не разрешает участников и не
// управляет жизненным циклом вызывающего.
func configureSyntheticRuntimeComponentRepository(t *testing.T, r *Repository) {
	t.Helper()
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureRoleImages(syntheticRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
}

// resolveRunLaunchComponentPrincipals resolves the fixed synthetic run-launch owner followed by
// the runtime claim worker, preserving both proof inputs and the shared repository/context. It
// does not resolve SYSTEM-grant or assistant-turn owners, construct a service, change permissions
// or create a lease; those operations remain explicit in each component scenario.
// resolveRunLaunchComponentPrincipals разрешает фиксированного тестового владельца запуска, затем
// исполнителя захвата runtime, сохраняя оба proof inputs и общие repository/context. Не подменяет
// владельцев SYSTEM-grant или assistant-turn, не создаёт сервис, не меняет permissions и не
// создаёт lease: эти операции остаются явными в каждом компонентном сценарии.
func resolveRunLaunchComponentPrincipals(t *testing.T, ctx context.Context, r *Repository) (value.Principal, value.Principal) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch"}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, r, port.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim"}, "runtime-controller")
	return owner, worker
}

// newNamespacedRoleImageComponentRepository creates the fixed synthetic openai-codex/gpt-5
// adapter with fresh in-memory object storage, applies only the namespaced role-image policy,
// then bootstraps with the exact caller context. It preserves the three original Fatal error
// paths and does not configure a provider credential. Pool ownership, tracer, deadline,
// principals and scenario assertions stay with the caller; no provider or cluster is contacted.
// newNamespacedRoleImageComponentRepository создаёт фиксированный тестовый adapter
// openai-codex/gpt-5 с новым in-memory object storage, применяет только namespaced role-image
// policy и выполняет Bootstrap с точным context вызывающего. Сохраняет три исходных Fatal
// error paths и не настраивает provider credential. Pool, tracer, deadline, principals и
// проверки сценария остаются у вызывающего; обращения к provider или кластеру отсутствуют.
func newNamespacedRoleImageComponentRepository(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *Repository {
	t.Helper()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(namespacedRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	return repository
}

// newTracedOrganizationComponentPool opens an isolated pool with the existing organization
// query tracer and the exact caller context. Parsing and connection errors keep their original
// Fatal messages. The caller closes the returned pool before registered database cleanup;
// deadline, repository configuration and assertions are not selected here.
// newTracedOrganizationComponentPool открывает изолированный pool с прежним organization
// query tracer и точным context вызывающего. Ошибки разбора и подключения сохраняют исходные
// Fatal messages. Вызывающий закрывает pool до зарегистрированной очистки БД; helper не
// выбирает deadline, конфигурацию repository и проверки сценария.
func newTracedOrganizationComponentPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated fixture configuration")
	}
	config.ConnConfig.Tracer = componentQueryRejectionTracer{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// resolvedComponentScope resolves the caller's synthetic principal through the repository and
// then resolves its owner scope, preserving both original fatal checks and call order. It runs
// synchronously in the test goroutine and returns only the authoritative scope; SQL, transactions,
// side-effect counters and lifecycle assertions remain in the caller. It grants no new permission.
// resolvedComponentScope разрешает переданного тестового участника через репозиторий, затем его
// область владельца, сохраняя оба исходных Fatal и порядок вызовов. Выполняется синхронно в
// goroutine теста и возвращает только авторитетную область; SQL, транзакции, счётчики эффектов и
// проверки жизненного цикла остаются у вызывающего. Новые полномочия не выдаются.
func resolvedComponentScope(t *testing.T, ctx context.Context, repository *Repository, principal value.Principal) scope {
	t.Helper()
	resolved, err := repository.ResolvePrincipal(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	current, err := repository.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	return current
}

// newProjectOwnerComponentService resolves the fixed synthetic project-create owner and constructs
// the platform service with the same repository and context, in that order. It preserves the
// exact proof inputs and fatal constructor error. The caller retains project creation, resource
// setup, pool lifetime, deadlines and every independent scenario assertion; no runtime is started.
// newProjectOwnerComponentService разрешает фиксированного тестового владельца создания проекта,
// затем создаёт платформенный сервис с теми же репозиторием и контекстом. Сохраняет точные proof
// inputs и Fatal при ошибке конструктора. Создание проекта, подготовка ресурсов, срок, время жизни
// пула и независимые проверки сценария остаются у вызывающего; runtime не запускается.
func newProjectOwnerComponentService(t *testing.T, ctx context.Context, repository *Repository) (*serviceplatform.Service, value.Principal) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	service, err := serviceplatform.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	return service, owner
}

// readAssistantRunScopeEffects reads the original five Run/receipt/audit/turn/outbox fixture
// counters with one query and the exact caller organization. It preserves the fixed array order
// and fatal scan error; the caller controls when snapshots are taken and compares durable effects.
// It does not resolve authority, mutate data, start a transaction or contact an external runtime.
// readAssistantRunScopeEffects читает прежние пять тестовых счётчиков Run/receipt/audit/turn/outbox
// одним запросом для точной организации вызывающего. Сохраняет порядок массива и Fatal при ошибке
// Scan; вызывающий выбирает момент снимка и сравнивает устойчивые эффекты. Не разрешает authority,
// не меняет данные, не открывает транзакцию и не обращается к внешнему runtime.
func readAssistantRunScopeEffects(t *testing.T, ctx context.Context, pool *pgxpool.Pool, organizationID string) [5]int {
	t.Helper()
	var result [5]int
	if err := pool.QueryRow(ctx, queryAssistantRunScopeEffects, organizationID).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
		t.Fatal(err)
	}
	return result
}

// configureSyntheticArtifactComponent configures the test artifact scanner, fixed synthetic
// provider and role-image fixture, then bootstraps in that order. The caller owns the repository,
// object store, pool, deadline and subsequent assertions; no external provider is contacted.
// configureSyntheticArtifactComponent настраивает тестовый scanner файлов, фиксированный synthetic
// provider и role-image fixture, затем запускает bootstrap в этом порядке. Repository, object store,
// pool, deadline и последующие проверки принадлежат caller; внешнего provider helper не вызывает.
func configureSyntheticArtifactComponent(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	configureComponentArtifactScanner(t, repository)
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(syntheticRoleImageComponentConfig()); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
}
