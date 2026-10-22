// Package platform coordinates repository-owned authority, lifecycle, idempotency and optimistic-concurrency operations for the web-first control plane.
// Package platform координирует принадлежащие репозиторию операции полномочий, жизненного цикла, идемпотентности и оптимистической конкуренции web-first control-plane.
package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// Service coordinates the platform repository and optional credential-materialization ports. Public operations resolve principals and apply their own input/command guards; persistence, resource authorization and durable lifecycle effects remain behind the repository port.
// Service координирует репозиторий платформы и необязательные порты материализации реквизитов. Публичные операции разрешают субъектов и применяют свои проверки входа и команд; хранение, авторизация ресурсов и устойчивые эффекты жизненного цикла остаются за портом репозитория.
type Service struct {
	repository                     repository.Repository
	credentialMaterializer         CredentialMaterializer
	emailCredentialMaterializer    CredentialMaterializer
	providerCredentialMaterializer ProviderCredentialMaterializer
}

// promptFullMaterializationMaximumAuthenticationAge limits interactive authentication freshness for includeFull prompt previews to five minutes, independently of the permission check.
// promptFullMaterializationMaximumAuthenticationAge ограничивает свежесть интерактивной аутентификации для предпросмотра промпта с includeFull пятью минутами независимо от проверки полномочия.
const promptFullMaterializationMaximumAuthenticationAge = 5 * time.Minute

// Option configures a Service during construction; New ignores nil options but otherwise applies them in caller order without validating optional adapters itself.
// Option настраивает Service при создании; New игнорирует nil, но остальные параметры применяет в порядке вызывающего, самостоятельно не проверяя необязательные адаптеры.
type Option func(*Service)

// WithCredentialMaterializer installs the runtime-credential materialization port without invoking it or changing repository readiness.
// WithCredentialMaterializer назначает порт материализации runtime-реквизитов без его вызова или изменения готовности репозитория.
func WithCredentialMaterializer(materializer CredentialMaterializer) Option {
	return func(service *Service) { service.credentialMaterializer = materializer }
}

// WithEmailCredentialMaterializer installs the distinct email-credential materialization port; it neither authorizes a mailbox operation nor starts materialization.
// WithEmailCredentialMaterializer назначает отдельный порт материализации почтовых реквизитов, не разрешая операцию ящика и не начиная материализацию.
func WithEmailCredentialMaterializer(materializer CredentialMaterializer) Option {
	return func(service *Service) { service.emailCredentialMaterializer = materializer }
}

// New requires a repository, applies nonnil options in order and returns an unstarted service. Optional adapters may remain absent; no bootstrap, dependency I/O or background work begins here.
// New требует репозиторий, по порядку применяет ненулевые параметры и возвращает незапущенный сервис. Необязательные адаптеры могут отсутствовать; bootstrap, I/O зависимостей и фоновая работа здесь не начинаются.
func New(repo repository.Repository, options ...Option) (*Service, error) {
	if repo == nil {
		return nil, errors.New("platform repository is required")
	}
	service := &Service{repository: repo}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

// Bootstrap delegates installation-state initialization to the repository using the caller's context; it does not resolve an interactive principal or configure optional materializers.
// Bootstrap передаёт инициализацию состояния установки репозиторию с контекстом вызывающего, не разрешая интерактивного субъекта и не настраивая необязательные материализаторы.
func (service *Service) Bootstrap(ctx context.Context) error {
	return service.repository.Bootstrap(ctx)
}

// Ready checks only repository-owned control-plane state. Downstream materializer readiness is intentionally separate to avoid a startup cycle between control-plane and secret-broker.
// Ready проверяет только принадлежащее репозиторию состояние control-plane. Готовность downstream-материализатора намеренно отделена во избежание стартового цикла между control-plane и secret-broker.
func (service *Service) Ready(ctx context.Context) error {
	// Общая readiness подтверждает только owned state control-plane. Downstream
	// materializer имеет отдельную exact-path readiness: иначе control-plane
	// исчезает из Service endpoints до старта secret-broker, а secret-broker не
	// может проверить принадлежащий control-plane runtime-secret work path.
	return service.repository.Ready(ctx)
}

// ResolveProofAuthority forwards proof-principal bindings to the authoritative repository resolver; it does not derive authority from a request field locally.
// ResolveProofAuthority передаёт привязки субъекта доказательства авторитетному разрешению в репозитории, не выводя полномочия локально из поля запроса.
func (service *Service) ResolveProofAuthority(ctx context.Context, input repository.ProofPrincipalInput) (repository.ProofAuthority, error) {
	return service.repository.ResolveProofAuthority(ctx, input)
}

// ResolveServiceCredentialGeneration reads the owner's durable generation floor for a workload through the repository; a routine RPC or Pod replacement does not create or advance that floor here.
// ResolveServiceCredentialGeneration читает устойчивый нижний предел поколения workload у владельца через репозиторий; обычный RPC или замена Pod не создают и не повышают его здесь.
func (service *Service) ResolveServiceCredentialGeneration(ctx context.Context, workload string) (uint64, error) {
	return service.repository.ResolveServiceCredentialGeneration(ctx, workload)
}

// AcceptWorkerGrant delegates validation and acceptance of the supplied worker grant to its repository owner without minting or modifying a grant in this wrapper.
// AcceptWorkerGrant передаёт проверку и принятие полномочия исполнителя владельцу в репозитории, не создавая и не изменяя полномочие в этой обёртке.
func (service *Service) AcceptWorkerGrant(ctx context.Context, input repository.WorkerGrantInput) error {
	return service.repository.AcceptWorkerGrant(ctx, input)
}

// NextAuthorityProofRevision requests the next authority-proof revision from repository-owned state rather than keeping a process-local counter.
// NextAuthorityProofRevision запрашивает следующую ревизию доказательства полномочий из состояния владельца в репозитории вместо счётчика процесса.
func (service *Service) NextAuthorityProofRevision(ctx context.Context) (uint64, error) {
	return service.repository.NextAuthorityProofRevision(ctx)
}

// principal rejects malformed principal values as ErrUnauthorized, then resolves current authoritative identity through the repository. Resolution errors pass through; this shared step is not a substitute for operation-specific resource authorization.
// principal отклоняет некорректные значения субъекта как ErrUnauthorized, затем разрешает актуальную авторитетную идентичность через репозиторий. Ошибки разрешения сохраняются; общий шаг не заменяет авторизацию ресурса конкретной операции.
func (service *Service) principal(ctx context.Context, principal value.Principal) (value.Principal, error) {
	if err := principal.Validate(); err != nil {
		return value.Principal{}, errs.ErrUnauthorized
	}
	resolved, err := service.repository.ResolvePrincipal(ctx, principal)
	if err != nil {
		return value.Principal{}, err
	}
	return resolved, nil
}

// GetBootstrapState resolves the principal before reading installation/bootstrap progress; the repository owns visibility of the returned setup state.
// GetBootstrapState разрешает субъекта перед чтением прогресса установки и bootstrap; видимость возвращаемого состояния настройки определяет репозиторий.
func (service *Service) GetBootstrapState(ctx context.Context, p value.Principal) (repository.BootstrapState, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.BootstrapState{}, err
	}
	return service.repository.GetBootstrapState(ctx, p)
}

// GetPlatformEventCursor resolves the principal and returns the repository's event cursor and revision for that boundary, not a locally synthesized realtime position.
// GetPlatformEventCursor разрешает субъекта и возвращает курсор событий и ревизию репозитория для этой границы, а не локально придуманный realtime-указатель.
func (service *Service) GetPlatformEventCursor(ctx context.Context, p value.Principal) (string, int64, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return "", 0, err
	}
	return service.repository.GetPlatformEventCursor(ctx, p)
}

// GetOverview resolves the principal and delegates the requested project overview; a project reference does not itself grant visibility.
// GetOverview разрешает субъекта и передаёт чтение обзора выбранного проекта; ссылка проекта сама по себе не даёт видимости.
func (service *Service) GetOverview(ctx context.Context, p value.Principal, projectRef string) (repository.Overview, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Overview{}, err
	}
	return service.repository.GetOverview(ctx, p, projectRef)
}

// ListCapabilities resolves the principal before reading integration-capability metadata from the repository; listing is not a capability grant.
// ListCapabilities разрешает субъекта перед чтением метаданных возможностей интеграций из репозитория; список не выдаёт полномочия на их использование.
func (service *Service) ListCapabilities(ctx context.Context, p value.Principal) ([]entity.IntegrationCapability, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListCapabilities(ctx, p)
}

// ListRuntimes resolves the principal and reads available runtime selections without choosing or starting an execution.
// ListRuntimes разрешает субъекта и читает доступные варианты runtime без выбора или запуска исполнения.
func (service *Service) ListRuntimes(ctx context.Context, p value.Principal) ([]entity.RuntimeSelection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListRuntimes(ctx, p)
}

// Search resolves the principal, trims the query and enforces two through two hundred Unicode runes. A zero page size inherits filter.Limit before repository search returns items, total and cursor.
// Search разрешает субъекта, обрезает внешние пробелы запроса и требует от двух до двухсот Unicode-символов. Нулевой размер страницы получает filter.Limit перед поиском в репозитории, возвращающим элементы, общее число и курсор.
func (service *Service) Search(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.SearchResult, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query, err = normalizePlatformSearchQuery(filter.Query)
	if err != nil {
		return nil, 0, "", err
	}
	if filter.Page.Size == 0 {
		filter.Page.Size = filter.Limit
	}
	return service.repository.Search(ctx, p, filter)
}

// ListVFSNodes resolves the principal, trims search text and defaults an empty path to /projects. It requires an absolute-looking path without any double-dot substring and at most one thousand bytes before repository listing; it does not traverse a host filesystem.
// ListVFSNodes разрешает субъекта, обрезает внешние пробелы поиска и заменяет пустой путь на /projects. До чтения репозитория требуется путь с начальным слешем, без любой подстроки из двух точек и длиной не более тысячи байт; файловая система хоста не обходится.
func (service *Service) ListVFSNodes(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.ResourceRef == "" {
		filter.ResourceRef = "/projects"
	}
	if !strings.HasPrefix(filter.ResourceRef, "/") || strings.Contains(filter.ResourceRef, "..") || len(filter.ResourceRef) > 1000 {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListVFSNodes(ctx, p, filter)
}

// SearchVFS resolves the principal and requires a trimmed query of two through two hundred Unicode runes before repository VFS search. Unlike Search, it does not copy filter.Limit into Page.Size.
// SearchVFS разрешает субъекта и требует очищенный от внешних пробелов запрос от двух до двухсот Unicode-символов перед поиском VFS в репозитории. В отличие от Search, filter.Limit в Page.Size не переносится.
func (service *Service) SearchVFS(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.VFSNode, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query, err = normalizePlatformSearchQuery(filter.Query)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.SearchVFS(ctx, p, filter)
}

// ListProjects resolves the principal and forwards the project filter, returning repository items, continuation cursor and action metadata without applying a separate local project filter.
// ListProjects разрешает субъекта и передаёт фильтр проектов, возвращая элементы, курсор продолжения и метаданные действий репозитория без отдельной локальной фильтрации проектов.
func (service *Service) ListProjects(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Project, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListProjects(ctx, p, filter)
}

// ListTrashedProjects resolves the principal before paging the repository's project trash; reading trash does not restore or purge projects.
// ListTrashedProjects разрешает субъекта перед постраничным чтением корзины проектов в репозитории; чтение не восстанавливает и не удаляет проекты окончательно.
func (service *Service) ListTrashedProjects(ctx context.Context, p value.Principal, page query.Page) ([]entity.Project, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListTrashedProjects(ctx, p, page)
}

// GetProject resolves the principal and delegates the exact project reference to repository authorization and lookup, returning its error unchanged.
// GetProject разрешает субъекта и передаёт точную ссылку проекта авторизации и поиску репозитория, сохраняя его ошибку.
func (service *Service) GetProject(ctx context.Context, p value.Principal, ref string) (entity.Project, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Project{}, err
	}
	return service.repository.GetProject(ctx, p, ref)
}

// ListPlatformMemberships resolves the principal and pages platform-level memberships, separately from project membership queries.
// ListPlatformMemberships разрешает субъекта и читает страницы членства уровня платформы отдельно от запросов членства в проекте.
func (service *Service) ListPlatformMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMemberships(ctx, p, filter)
}

// ListPlatformMembershipCandidates resolves the principal before reading candidate users for platform membership; candidate visibility does not add a membership.
// ListPlatformMembershipCandidates разрешает субъекта перед чтением пользователей-кандидатов на членство платформы; видимость кандидата не добавляет членство.
func (service *Service) ListPlatformMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPlatformMembershipCandidates(ctx, p, filter)
}

// ListMemberships resolves the principal and forwards the project-membership filter to the repository, returning memberships and the continuation cursor.
// ListMemberships разрешает субъекта и передаёт фильтр проектного членства репозиторию, возвращая записи и курсор продолжения.
func (service *Service) ListMemberships(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Membership, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMemberships(ctx, p, filter)
}

// ListMembershipCandidates resolves the principal and reads candidate users for the requested membership boundary, without granting project access.
// ListMembershipCandidates разрешает субъекта и читает пользователей-кандидатов в запрошенной границе членства, не предоставляя доступ к проекту.
func (service *Service) ListMembershipCandidates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.User, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListMembershipCandidates(ctx, p, filter)
}

// ListAgents resolves the principal before paging agent metadata through the repository; it neither enables agents nor loads their execution state locally.
// ListAgents разрешает субъекта перед чтением страниц метаданных сотрудников через репозиторий, не включая сотрудников и не загружая локально состояние их исполнения.
func (service *Service) ListAgents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAgents(ctx, p, filter)
}

// GetAgent resolves the principal and reads one agent through the repository's resource boundary rather than treating its reference as authority.
// GetAgent разрешает субъекта и читает сотрудника через ресурсную границу репозитория, не считая ссылку полномочием.
func (service *Service) GetAgent(ctx context.Context, p value.Principal, ref string) (entity.Agent, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Agent{}, err
	}
	return service.repository.GetAgent(ctx, p, ref)
}

// GetAgentRuntimeConfiguration resolves the principal and returns the repository's current agent-runtime configuration view, not a newly published runtime revision.
// GetAgentRuntimeConfiguration разрешает субъекта и возвращает текущее представление runtime-конфигурации сотрудника из репозитория, а не новую опубликованную ревизию runtime.
func (service *Service) GetAgentRuntimeConfiguration(ctx context.Context, p value.Principal, ref string) (entity.AgentRuntimeConfigurationView, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	return service.repository.GetAgentRuntimeConfiguration(ctx, p, ref)
}

// ListAgentRuntimeConfigurations resolves the principal before paging agent-runtime configurations with the supplied filter and returning the repository cursor.
// ListAgentRuntimeConfigurations разрешает субъекта перед чтением страниц runtime-конфигураций сотрудников с переданным фильтром и возвращает курсор репозитория.
func (service *Service) ListAgentRuntimeConfigurations(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AgentRuntimeConfiguration, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAgentRuntimeConfigurations(ctx, p, filter)
}

// ListRuntimeEnvironments resolves the principal and pages environment-set metadata; returned environments are not implicitly bound to an agent.
// ListRuntimeEnvironments разрешает субъекта и читает страницы метаданных наборов окружений; возвращённые окружения не привязываются к сотруднику неявно.
func (service *Service) ListRuntimeEnvironments(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentSet, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironments(ctx, p, filter)
}

// GetRuntimeEnvironment resolves the principal and delegates lookup of one runtime environment to its repository owner.
// GetRuntimeEnvironment разрешает субъекта и передаёт чтение одного runtime-окружения владельцу в репозитории.
func (service *Service) GetRuntimeEnvironment(ctx context.Context, p value.Principal, ref string) (entity.RuntimeEnvironmentSet, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, err
	}
	return service.repository.GetRuntimeEnvironment(ctx, p, ref)
}

// GetRuntimeEnvironmentReadiness resolves the principal and reads the repository's environment-readiness result; it does not start a runtime or probe dependencies itself.
// GetRuntimeEnvironmentReadiness разрешает субъекта и читает результат готовности окружения в репозитории, не запуская runtime и не проверяя зависимости самостоятельно.
func (service *Service) GetRuntimeEnvironmentReadiness(ctx context.Context, p value.Principal, ref string) (entity.RuntimeEnvironmentReadiness, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeEnvironmentReadiness{}, err
	}
	return service.repository.GetRuntimeEnvironmentReadiness(ctx, p, ref)
}

// ListRuntimeEnvironmentAgents resolves the principal before paging agents associated with the requested environment filter; association visibility does not change their bindings.
// ListRuntimeEnvironmentAgents разрешает субъекта перед чтением страниц сотрудников, связанных с фильтром окружения; видимость связи не изменяет привязки.
func (service *Service) ListRuntimeEnvironmentAgents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Agent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironmentAgents(ctx, p, filter)
}

// ListRuntimeEnvironmentVersions resolves the principal and pages stored environment revisions without selecting or rolling back a version.
// ListRuntimeEnvironmentVersions разрешает субъекта и читает страницы сохранённых ревизий окружения без выбора или отката версии.
func (service *Service) ListRuntimeEnvironmentVersions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeEnvironmentVersions(ctx, p, filter)
}

// ListRuntimeSecrets resolves the principal and pages runtime-secret metadata through the repository; this method does not materialize plaintext values.
// ListRuntimeSecrets разрешает субъекта и читает страницы метаданных runtime-секретов через репозиторий; открытые значения этим методом не материализуются.
func (service *Service) ListRuntimeSecrets(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RuntimeSecret, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecrets(ctx, p, filter)
}

// GetRuntimeSecret resolves the principal and reads a secret's metadata record, separately from the credential-projection and materialization operations.
// GetRuntimeSecret разрешает субъекта и читает запись метаданных секрета отдельно от операций проекции реквизита и материализации.
func (service *Service) GetRuntimeSecret(ctx context.Context, p value.Principal, ref string) (entity.RuntimeSecret, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	return service.repository.GetRuntimeSecret(ctx, p, ref)
}

// PrepareRuntimeSecretOperation fixes scope to PROJECT, resolves the principal, assigns the kind-derived mutation operation and hashes the exact metadata/version intent before delegating preparation. No secret bytes are included in that intent shape.
// PrepareRuntimeSecretOperation фиксирует область PROJECT, разрешает субъекта, назначает операцию изменения по виду и хеширует точное намерение метаданных и версии перед подготовкой в репозитории. Байты секрета в форму намерения не входят.
func (service *Service) PrepareRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretPrepareInput) (repository.RuntimeSecretPrepareResult, error) {
	input.ScopeKind = "PROJECT"
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretPrepareResult{}, err
	}
	input.Mutation.Operation = "runtime-secret." + strings.ToLower(input.Kind)
	input.Mutation.IntentDigest = digest(struct {
		Kind, ProjectRef, SecretRef, Name, Description, ValueType, ExpectedContentSHA256 string
		ExpectedVersion                                                                  *int64
	}{input.Kind, input.ProjectRef, input.SecretRef, input.Name, input.Description, input.ValueType, input.ExpectedContentSHA256, input.Mutation.ExpectedVersion})
	return service.repository.PrepareRuntimeSecretOperation(ctx, p, input)
}

// ListRuntimeSecretRecoveryWork resolves the principal before reading paginated secret-recovery work; listing does not itself consume an operation grant.
// ListRuntimeSecretRecoveryWork разрешает субъекта перед чтением страниц работы восстановления секретов; чтение само по себе не потребляет полномочие операции.
func (service *Service) ListRuntimeSecretRecoveryWork(ctx context.Context, p value.Principal, page repository.RuntimeSecretRecoveryPage) ([]entity.RuntimeSecretRecoveryWork, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRuntimeSecretRecoveryWork(ctx, p, page)
}

// ConsumeRuntimeSecretOperation resolves the principal and delegates consumption of the exact operation input to the repository, which owns grant and operation state.
// ConsumeRuntimeSecretOperation разрешает субъекта и передаёт потребление точного входа операции репозиторию, владеющему состоянием полномочия и операции.
func (service *Service) ConsumeRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretConsumeInput) (entity.RuntimeSecretOperation, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecretOperation{}, err
	}
	return service.repository.ConsumeRuntimeSecretOperation(ctx, p, input)
}

// CompleteRuntimeSecretOperation resolves the principal and submits completion evidence to the repository, returning the resulting secret metadata rather than performing materialization here.
// CompleteRuntimeSecretOperation разрешает субъекта и передаёт доказательства завершения репозиторию, возвращая метаданные итогового секрета, а не выполняя здесь материализацию.
func (service *Service) CompleteRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretCompleteInput) (entity.RuntimeSecret, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeSecret{}, err
	}
	return service.repository.CompleteRuntimeSecretOperation(ctx, p, input)
}

// FailRuntimeSecretOperation resolves the principal and records the operation failure through the repository; the returned recovery outcome must not be confused with successful secret publication.
// FailRuntimeSecretOperation разрешает субъекта и фиксирует ошибку операции через репозиторий; возвращённый исход восстановления не следует смешивать с успешной публикацией секрета.
func (service *Service) FailRuntimeSecretOperation(ctx context.Context, p value.Principal, input repository.RuntimeSecretFailInput) (repository.RuntimeSecretFailureResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretFailureResult{}, err
	}
	return service.repository.FailRuntimeSecretOperation(ctx, p, input)
}

// RecoverRuntimeSecretMaterialization resolves the principal and delegates owner reconciliation of the supplied materialization recovery input, not a blind external write retry.
// RecoverRuntimeSecretMaterialization разрешает субъекта и передаёт владельцу сопоставление входа восстановления материализации, а не слепой повтор внешней записи.
func (service *Service) RecoverRuntimeSecretMaterialization(ctx context.Context, p value.Principal, input repository.RuntimeSecretRecoveryInput) (repository.RuntimeSecretRecoveryResult, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeSecretRecoveryResult{}, err
	}
	return service.repository.RecoverRuntimeSecretMaterialization(ctx, p, input)
}

// ResolveRuntimeCredentialProjection resolves the principal before requesting the repository's runtime credential projection; the wrapper does not read a Kubernetes Secret itself.
// ResolveRuntimeCredentialProjection разрешает субъекта перед запросом runtime-проекции реквизита у репозитория; сама обёртка Kubernetes Secret не читает.
func (service *Service) ResolveRuntimeCredentialProjection(ctx context.Context, p value.Principal, input repository.RuntimeCredentialProjectionInput) (repository.RuntimeCredentialProjection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.RuntimeCredentialProjection{}, err
	}
	return service.repository.ResolveRuntimeCredentialProjection(ctx, p, input)
}

// ValidateRuntimeCredentialProjection resolves the principal and returns the repository's validity decision and error for the exact projection input, without issuing a replacement grant.
// ValidateRuntimeCredentialProjection разрешает субъекта и возвращает решение репозитория о допустимости точного входа проекции и ошибку без выдачи заменяющего полномочия.
func (service *Service) ValidateRuntimeCredentialProjection(ctx context.Context, p value.Principal, input repository.RuntimeCredentialProjectionInput) (bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return false, err
	}
	return service.repository.ValidateRuntimeCredentialProjection(ctx, p, input)
}

// ResolveTranscriptionCredentialProjection resolves the principal and delegates the distinct speech-transcription credential-projection path to the repository.
// ResolveTranscriptionCredentialProjection разрешает субъекта и передаёт отдельный путь проекции реквизита транскрибации речи репозиторию.
func (service *Service) ResolveTranscriptionCredentialProjection(ctx context.Context, p value.Principal, input repository.TranscriptionCredentialProjectionInput) (repository.TranscriptionCredentialProjection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.TranscriptionCredentialProjection{}, err
	}
	return service.repository.ResolveTranscriptionCredentialProjection(ctx, p, input)
}

// ListTemplateVariables resolves the principal, trims the query and reads template-variable metadata with total and cursor; it does not materialize a template.
// ListTemplateVariables разрешает субъекта, обрезает внешние пробелы запроса и читает метаданные переменных шаблона с общим числом и курсором, не материализуя шаблон.
func (service *Service) ListTemplateVariables(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.TemplateVariable, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return service.repository.ListTemplateVariables(ctx, p, filter)
}

// ListPromptContextVariables resolves the principal and reads the context-aware variable catalog from the repository rather than using only the synthetic default catalog.
// ListPromptContextVariables разрешает субъекта и читает учитывающий контекст каталог переменных из репозитория вместо использования только синтетического каталога по умолчанию.
func (service *Service) ListPromptContextVariables(ctx context.Context, p value.Principal, filter query.Filter) (entity.PromptVariableCatalog, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.PromptVariableCatalog{}, err
	}
	return service.repository.ListPromptContextVariables(ctx, p, filter)
}

// ValidatePromptTemplateWithContext uses syntax/catalog validation when every context selector is empty; otherwise it previews without includeFull. ErrInvalid accompanied by diagnostics becomes a successful diagnostic result, while other errors remain errors.
// ValidatePromptTemplateWithContext использует проверку синтаксиса и каталога при пустых селекторах контекста; иначе выполняет предпросмотр без includeFull. ErrInvalid с диагностикой превращается в успешный диагностический результат, а остальные ошибки сохраняются.
func (service *Service) ValidatePromptTemplateWithContext(ctx context.Context, p value.Principal, text, kind, ref string, input query.PromptPreviewContext, expected string) (promptservice.Materialization, error) {
	if kind == "" && ref == "" && expected == "" && input.AgentRef == "" && input.WorkflowRevisionRef == "" && input.WorkflowStageKey == "" && input.AttachmentSetRef == "" && input.Task == "" && input.ExpectedAgentVersion == 0 && input.ExpectedWorkflowVersion == 0 && len(input.Input) == 0 {
		diagnostics, err := service.ValidatePromptTemplate(ctx, p, text)
		return promptservice.Materialization{Complete: len(diagnostics) == 0, Diagnostics: diagnostics}, err
	}
	result, err := service.PreviewPromptTemplateWithContext(ctx, p, text, kind, ref, false, input, expected)
	if errors.Is(err, errs.ErrInvalid) && len(result.Diagnostics) != 0 {
		return result, nil
	}
	return result, err
}

// ListProviderDefinitions resolves the principal and pages provider-definition metadata with the repository filter; reading definitions does not authorize an account.
// ListProviderDefinitions разрешает субъекта и читает страницы метаданных определений провайдеров с фильтром репозитория; чтение определений не авторизует аккаунт.
func (service *Service) ListProviderDefinitions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ProviderDefinition, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListProviderDefinitions(ctx, p, filter)
}

// ValidatePromptTemplate requires successful principal resolution, then validates the template against the static prompt catalog without reading a target snapshot or invoking a model.
// ValidatePromptTemplate требует успешного разрешения субъекта, затем проверяет шаблон по статическому каталогу промпта без чтения снимка цели или вызова модели.
func (service *Service) ValidatePromptTemplate(ctx context.Context, p value.Principal, template string) ([]promptservice.Diagnostic, error) {
	if _, err := service.principal(ctx, p); err != nil {
		return nil, err
	}
	return promptservice.Validate(template, promptservice.Catalog()), nil
}

// PreviewPromptTemplate delegates to the context-aware preview with an empty preview context and no expected context digest, retaining the caller's target and includeFull choice.
// PreviewPromptTemplate передаёт выполнение контекстному предпросмотру с пустым контекстом и без ожидаемого дайджеста, сохраняя цель и выбор includeFull вызывающего.
func (service *Service) PreviewPromptTemplate(ctx context.Context, p value.Principal, template, targetKind, targetRef string, includeFull bool) (promptservice.Materialization, error) {
	return service.PreviewPromptTemplateWithContext(ctx, p, template, targetKind, targetRef, includeFull, query.PromptPreviewContext{}, "")
}

// PreviewPromptTemplateWithContext resolves the principal, validates target/context shape and optional digest pin, loads or synthesizes the snapshot and materializes the chosen template. includeFull additionally requires exactly one allowed prompt.full.view decision and fresh interactive authentication; this method does not itself implement output redaction or call a model.
// PreviewPromptTemplateWithContext разрешает субъекта, проверяет форму цели и контекста и необязательную привязку дайджеста, загружает либо создаёт снимок и материализует выбранный шаблон. includeFull дополнительно требует ровно одного разрешающего решения prompt.full.view и свежей интерактивной аутентификации; самостоятельного скрытия чувствительных значений результата или вызова модели здесь нет.
func (service *Service) PreviewPromptTemplateWithContext(ctx context.Context, p value.Principal, template, targetKind, targetRef string, includeFull bool, previewContext query.PromptPreviewContext, expectedContextDigest string) (promptservice.Materialization, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return promptservice.Materialization{}, err
	}
	if targetKind == "" {
		targetKind = "SYNTHETIC"
	}
	if expectedContextDigest != "" {
		decoded, decodeErr := hex.DecodeString(expectedContextDigest)
		if decodeErr != nil || len(decoded) != sha256.Size || strings.ToLower(expectedContextDigest) != expectedContextDigest {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
	}
	if targetKind == "SYNTHETIC" || targetKind == "RUN" || targetKind == "SESSION" {
		if previewContext.AgentRef != "" || previewContext.WorkflowRevisionRef != "" || previewContext.WorkflowStageKey != "" ||
			previewContext.ExpectedAgentVersion != 0 || previewContext.ExpectedWorkflowVersion != 0 ||
			len(previewContext.Input) != 0 || previewContext.AttachmentSetRef != "" || previewContext.Task != "" {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
	}
	var snapshot entity.PromptMaterializationSnapshot
	authorizationTarget := organizationTargetForPreview(p.AuthorityTenant)
	switch targetKind {
	case "SYNTHETIC":
		if targetRef != "" || strings.TrimSpace(template) == "" {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
		snapshot = syntheticPromptSnapshot(template)
	case "RUN", "SESSION":
		if strings.TrimSpace(targetRef) == "" {
			return promptservice.Materialization{}, errs.ErrInvalid
		}
		snapshot, err = service.repository.GetPromptMaterializationSnapshot(ctx, p, targetKind, targetRef)
		if err != nil {
			return promptservice.Materialization{}, err
		}
		authorizationTarget = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: snapshot.ProjectRef,
			ResourceKind: targetKind, ResourceRef: targetRef}
	case promptservice.TargetAgent, promptservice.TargetWorkflowStage, promptservice.TargetSessionContinuation:
		snapshot, err = service.repository.GetPromptPreviewContextSnapshot(ctx, p, targetKind, targetRef, previewContext)
		if err != nil {
			return promptservice.Materialization{}, err
		}
		resourceKind := targetKind
		if targetKind == promptservice.TargetWorkflowStage {
			resourceKind = "WORKFLOW"
		}
		if targetKind == promptservice.TargetSessionContinuation {
			resourceKind = "SESSION"
		}
		authorizationTarget = entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: snapshot.ProjectRef, ResourceKind: resourceKind, ResourceRef: targetRef}
	default:
		return promptservice.Materialization{}, errs.ErrInvalid
	}
	if expectedContextDigest != "" && expectedContextDigest != snapshot.ContextPin.Digest {
		return promptservice.Materialization{}, errs.ErrVersionMismatch
	}
	if strings.TrimSpace(template) == "" {
		template = snapshot.TemplateContent
	} else {
		digest := sha256.Sum256([]byte(template))
		snapshot.TemplateDigest = hex.EncodeToString(digest[:])
		snapshot.TemplateRef = "preview_" + snapshot.TemplateDigest[:24]
		snapshot.TemplateContent = template
	}
	materialized, err := promptservice.Materialize(template, promptservice.FromSnapshot(snapshot))
	materialized.ContextPin = snapshot.ContextPin
	if err != nil {
		return materialized, errs.ErrInvalid
	}
	if includeFull {
		access, accessErr := service.repository.QueryEffectiveAccess(ctx, p, "", authorizationTarget,
			[]string{"prompt.full.view"}, time.Now().UTC())
		if accessErr != nil || len(access.Decisions) != 1 || !access.Decisions[0].Allowed {
			return promptservice.Materialization{}, errs.ErrForbidden
		}
		if !p.InteractiveAuthenticationIsFresh(time.Now().UTC(), promptFullMaterializationMaximumAuthenticationAge) {
			return promptservice.Materialization{}, errs.ErrFreshAuthenticationRequired
		}
	}
	return materialized, nil
}

// syntheticPromptSnapshot builds deterministic preview metadata from the template's SHA-256 and static synthetic user/organization/project/agent variables. It grants no user or agent capabilities and does not read a real target.
// syntheticPromptSnapshot создаёт детерминированные метаданные предпросмотра из SHA-256 шаблона и статических синтетических переменных пользователя, организации, проекта и сотрудника. Полномочия пользователю или сотруднику не выдаются, реальная цель не читается.
func syntheticPromptSnapshot(template string) entity.PromptMaterializationSnapshot {
	digest := sha256.Sum256([]byte(template))
	digestHex := hex.EncodeToString(digest[:])
	variables := promptservice.Catalog()
	variables["user.ref"], variables["user.name"] = "usr_preview0001", "Пользователь"
	variables["organization.ref"], variables["organization.name"] = "org_preview0001", "Организация"
	variables["project.ref"], variables["project.name"] = "prj_preview0001", "Проект"
	variables["agent.ref"], variables["agent.name"] = "agt_preview0001", "Агент"
	return entity.PromptMaterializationSnapshot{
		ServiceTemplateRevision: promptservice.ServiceTemplateRevision, Locale: "en",
		TargetKind: promptservice.TargetAgent, TargetRef: "agt_preview0001", ProjectRef: "prj_preview0001",
		TemplateRef: "preview_" + digestHex[:24], TemplateDigest: digestHex, TemplateContent: template,
		Variables: variables, UserCapabilities: []string{}, AgentCapabilities: []string{},
	}
}

// organizationTargetForPreview constructs an organization access-scope value from the supplied reference; it does not validate membership or authorize that scope.
// organizationTargetForPreview создаёт значение организационной области доступа из переданной ссылки, не проверяя членство и не разрешая доступ к области.
func organizationTargetForPreview(ref string) entity.AccessScope {
	return entity.AccessScope{Kind: "ORGANIZATION", ResourceKind: "ORGANIZATION", ResourceRef: ref}
}

// ListModelCapabilities reuses ListModelCatalog and flattens its models, total and cursor into separate typed return values while preserving the catalog error; it does not perform a second read.
// ListModelCapabilities использует ListModelCatalog и раскладывает модели, общее число и курсор в отдельные типизированные результаты с сохранением ошибки каталога; второго чтения нет.
func (service *Service) ListModelCapabilities(ctx context.Context, p value.Principal, definitionKey, accountRef string, filter query.Filter) ([]entity.ModelCapability, int64, string, error) {
	result, err := service.ListModelCatalog(ctx, p, definitionKey, accountRef, filter)
	return result.Models, result.Total, result.NextPageToken, err
}

// ListModelCatalog resolves the principal, trims query, provider-definition key and account reference, then reads the repository model catalog and its provenance metadata.
// ListModelCatalog разрешает субъекта, обрезает внешние пробелы запроса, ключа определения провайдера и ссылки аккаунта, затем читает каталог моделей и метаданные его происхождения из репозитория.
func (service *Service) ListModelCatalog(ctx context.Context, p value.Principal, definitionKey, accountRef string, filter query.Filter) (entity.ModelCatalog, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ModelCatalog{}, err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	return service.repository.ListModelCapabilities(ctx, p, strings.TrimSpace(definitionKey), strings.TrimSpace(accountRef), filter)
}

// ListManagedConfigurations resolves the principal, trims and bounds the query to two hundred bytes and accepts only the closed managed-configuration categories before repository paging.
// ListManagedConfigurations разрешает субъекта, обрезает внешние пробелы запроса и ограничивает его двумястами байтами, принимая только закрытый набор категорий управляемых конфигураций перед постраничным чтением.
func (service *Service) ListManagedConfigurations(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ManagedConfigurationSet, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.Query) > 200 {
		return nil, 0, "", errs.ErrInvalid
	}
	switch filter.Category {
	case "", "PROMPT_TEMPLATE", "ROLE_IMAGE", "INTEGRATION_DEFINITION", "SYSTEM_STT":
	default:
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListManagedConfigurations(ctx, p, filter)
}

// ListManagedConfigurationHistory resolves the principal, rejects a blank configuration reference and delegates its trimmed value plus pagination to return the set and revision history.
// ListManagedConfigurationHistory разрешает субъекта, отклоняет пустую ссылку конфигурации и передаёт её очищенное значение с пагинацией для получения набора и истории ревизий.
func (service *Service) ListManagedConfigurationHistory(ctx context.Context, p value.Principal, ref string, page query.Page) (entity.ManagedConfigurationSet, []entity.ManagedConfigurationRevision, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationSet{}, nil, 0, "", err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.ManagedConfigurationSet{}, nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListManagedConfigurationHistory(ctx, p, strings.TrimSpace(ref), page)
}

// GetRoleImageImpactPlan resolves the principal and rejects a whitespace-only plan reference, then forwards the original reference, search and page without additional normalization.
// GetRoleImageImpactPlan разрешает субъекта и отклоняет ссылку плана из одних пробелов, затем передаёт исходную ссылку, поиск и страницу без дополнительной нормализации.
func (service *Service) GetRoleImageImpactPlan(ctx context.Context, p value.Principal, ref, search string, page query.Page) (entity.RoleImageImpactPage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RoleImageImpactPage{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.RoleImageImpactPage{}, errs.ErrInvalid
	}
	return service.repository.GetRoleImageImpactPlan(ctx, p, ref, search, page)
}

// GetManagedConfigurationImpact resolves the principal and requires nonblank configuration and revision references, trimming both before the repository impact read.
// GetManagedConfigurationImpact разрешает субъекта и требует непустые ссылки конфигурации и ревизии, обрезая внешние пробелы обеих перед чтением влияния в репозитории.
func (service *Service) GetManagedConfigurationImpact(ctx context.Context, p value.Principal, ref, revisionRef string, filter query.Filter) (entity.ManagedConfigurationImpact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationImpact{}, err
	}
	if strings.TrimSpace(ref) == "" || strings.TrimSpace(revisionRef) == "" {
		return entity.ManagedConfigurationImpact{}, errs.ErrInvalid
	}
	return service.repository.GetManagedConfigurationImpact(ctx, p, strings.TrimSpace(ref), strings.TrimSpace(revisionRef), filter)
}

// GetEffectiveManagedConfiguration resolves and trims the inputs, permitting only ROLE_IMAGE/RUNTIME_ENVIRONMENT or INTEGRATION_DEFINITION/INTEGRATION_CONNECTION with a nonempty consumer reference. The repository resolves the effective binding.
// GetEffectiveManagedConfiguration разрешает субъекта и очищает входы, допуская только ROLE_IMAGE/RUNTIME_ENVIRONMENT либо INTEGRATION_DEFINITION/INTEGRATION_CONNECTION с непустой ссылкой потребителя. Действующую привязку разрешает репозиторий.
func (service *Service) GetEffectiveManagedConfiguration(ctx context.Context, p value.Principal, kind, consumerKind, consumerRef string) (entity.ManagedConfigurationBindingSnapshot, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationBindingSnapshot{}, err
	}
	kind, consumerKind, consumerRef = strings.TrimSpace(kind), strings.TrimSpace(consumerKind), strings.TrimSpace(consumerRef)
	allowed := kind == "ROLE_IMAGE" && consumerKind == "RUNTIME_ENVIRONMENT" ||
		kind == "INTEGRATION_DEFINITION" && consumerKind == "INTEGRATION_CONNECTION"
	if !allowed || consumerRef == "" {
		return entity.ManagedConfigurationBindingSnapshot{}, errs.ErrInvalid
	}
	return service.repository.GetEffectiveManagedConfiguration(ctx, p, kind, consumerKind, consumerRef)
}

// GetSystemSTTConfiguration resolves the principal and reads system speech-transcription configuration metadata through its repository boundary; no audio is recorded or sent.
// GetSystemSTTConfiguration разрешает субъекта и читает метаданные системной конфигурации транскрибации через границу репозитория; аудио не записывается и не отправляется.
func (service *Service) GetSystemSTTConfiguration(ctx context.Context, p value.Principal) (entity.SystemSTTConfiguration, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemSTTConfiguration{}, err
	}
	return service.repository.GetSystemSTTConfiguration(ctx, p)
}

// ListProviderAccounts resolves the principal and pages account metadata, cursor and action metadata; reading an account is separate from credential materialization and authorization refresh.
// ListProviderAccounts разрешает субъекта и читает страницы метаданных аккаунтов, курсор и метаданные действий; чтение аккаунта отделено от материализации реквизита и обновления авторизации.
func (service *Service) ListProviderAccounts(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ProviderAccount, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListProviderAccounts(ctx, p, filter)
}

// GetProviderAccount resolves the principal and rejects a blank reference but passes the original string to repository lookup, rather than silently trimming identity.
// GetProviderAccount разрешает субъекта и отклоняет пустую ссылку, но передаёт исходную строку поиску репозитория без тихого изменения идентичности обрезанием пробелов.
func (service *Service) GetProviderAccount(ctx context.Context, p value.Principal, ref string) (entity.ProviderAccount, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.ProviderAccount{}, errs.ErrInvalid
	}
	return service.repository.GetProviderAccount(ctx, p, ref)
}

// ListProviderAccountBlockers resolves the principal and reads the owner-provided blockers for the supplied account query; it does not cancel or remove blocked work.
// ListProviderAccountBlockers разрешает субъекта и читает блокирующие зависимости владельца по запросу аккаунта, не отменяя и не удаляя зависимую работу.
func (service *Service) ListProviderAccountBlockers(ctx context.Context, p value.Principal, input query.ProviderAccountBlockers) (entity.ProviderAccountBlockerPage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProviderAccountBlockerPage{}, err
	}
	return service.repository.ListProviderAccountBlockers(ctx, p, input)
}

// GetProviderAccountWithUsage resolves the principal and passes the caller-supplied usage context to repository account projection; the wrapper does not fetch provider usage remotely.
// GetProviderAccountWithUsage разрешает субъекта и передаёт контекст использования проекции аккаунта в репозитории; сама обёртка удалённо использование провайдера не запрашивает.
func (service *Service) GetProviderAccountWithUsage(ctx context.Context, p value.Principal, ref string, usage *entity.ProviderAccountUsageContext) (entity.ProviderAccount, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	return service.repository.GetProviderAccountWithUsage(ctx, p, ref, usage)
}

// ListRoleImageRecipeRevisions resolves the principal before paging recipe revisions through the repository, without admitting or promoting any returned image.
// ListRoleImageRecipeRevisions разрешает субъекта перед чтением страниц ревизий рецепта через репозиторий без допуска или продвижения возвращённых образов.
func (service *Service) ListRoleImageRecipeRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RoleImageRecipeRevision, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListRoleImageRecipeRevisions(ctx, p, filter)
}

// ListAgentInstructionVersions resolves the principal and reads the agent first, then pages its already published instruction versions locally; unpublished instructions are not added to that slice.
// ListAgentInstructionVersions разрешает субъекта и сначала читает сотрудника, затем локально разбивает на страницы уже опубликованные версии его инструкций; неопубликованные инструкции в срез не добавляются.
func (service *Service) ListAgentInstructionVersions(ctx context.Context, p value.Principal, ref string, page query.Page) ([]entity.InstructionVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	agent, err := service.repository.GetAgent(ctx, p, ref)
	if err != nil {
		return nil, "", err
	}
	return pagePublishedInstructionVersions(agent.PublishedInstructionVersions, page)
}

// pagePublishedInstructionVersions pages an existing version-ordered slice without sorting it, using a positive int32 cursor as an exclusive version bound. Size defaults to fifty and caps at one hundred; returned items copy the slice and next cursor is the last emitted version when more remain.
// pagePublishedInstructionVersions разбивает существующий упорядоченный по версиям срез без сортировки, используя положительный int32-курсор как исключающую границу версии. Размер по умолчанию пятьдесят, максимум сто; элементы копируются, а при наличии продолжения курсором становится последняя выданная версия.
func pagePublishedInstructionVersions(versions []entity.InstructionVersion, page query.Page) ([]entity.InstructionVersion, string, error) {
	limit := int(page.Size)
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cursor := int64(0)
	if page.Token != "" {
		parsed, err := strconv.ParseInt(page.Token, 10, 32)
		if err != nil || parsed < 1 {
			return nil, "", errs.ErrInvalid
		}
		cursor = parsed
	}
	start := 0
	if cursor > 0 {
		for start < len(versions) && int64(versions[start].VersionNumber) >= cursor {
			start++
		}
	}
	end := min(start+limit, len(versions))
	items := append([]entity.InstructionVersion(nil), versions[start:end]...)
	next := ""
	if end < len(versions) && len(items) > 0 {
		next = strconv.FormatInt(int64(items[len(items)-1].VersionNumber), 10)
	}
	return items, next, nil
}

// ListWorkflows resolves the principal before repository paging of workflow metadata; listing does not publish a draft or start a run.
// ListWorkflows разрешает субъекта перед чтением страниц метаданных процессов в репозитории; чтение не публикует черновик и не запускает исполнение.
func (service *Service) ListWorkflows(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Workflow, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListWorkflows(ctx, p, filter)
}

// GetWorkflow resolves the principal and reads one workflow through repository ownership checks, preserving the returned lookup error.
// GetWorkflow разрешает субъекта и читает процесс через проверки владения репозитория с сохранением ошибки поиска.
func (service *Service) GetWorkflow(ctx context.Context, p value.Principal, ref string) (entity.Workflow, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Workflow{}, err
	}
	return service.repository.GetWorkflow(ctx, p, ref)
}

// ListRuns resolves the principal, reads run items/total/cursor and substitutes the request context error only when repository reading also failed. Successful results are not replaced merely because the context later closed.
// ListRuns разрешает субъекта, читает запуски, общее число и курсор и подставляет ошибку контекста запроса только при ошибке чтения репозитория. Успех не заменяется лишь из-за последующего закрытия контекста.
func (service *Service) ListRuns(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Run, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	items, total, next, err := service.repository.ListRuns(ctx, p, filter)
	return items, total, next, preserveRequestContext(ctx, err)
}

// GetRun resolves the principal and reads the run, preserving request cancellation or deadline over a concurrent repository failure through preserveRequestContext.
// GetRun разрешает субъекта и читает запуск, сохраняя отмену или срок запроса вместо одновременной ошибки репозитория через preserveRequestContext.
func (service *Service) GetRun(ctx context.Context, p value.Principal, ref string) (entity.Run, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, err
	}
	item, err := service.repository.GetRun(ctx, p, ref)
	return item, preserveRequestContext(ctx, err)
}

// preserveRequestContext returns ctx.Err only when both an operation error and a context error exist; otherwise it preserves the operation result, including nil.
// preserveRequestContext возвращает ctx.Err только при одновременном наличии ошибки операции и контекста; иначе сохраняет результат операции, включая nil.
func preserveRequestContext(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// GetRunGraph resolves the principal and returns the repository run and graph together; this wrapper does not recompute graph state.
// GetRunGraph разрешает субъекта и возвращает запуск и граф из репозитория вместе; состояние графа обёртка не пересчитывает.
func (service *Service) GetRunGraph(ctx context.Context, p value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	return service.repository.GetRunGraph(ctx, p, ref)
}

// GetRunGraphSnapshot resolves the principal and delegates the distinct graph-snapshot read path, retaining the repository's run/graph consistency contract.
// GetRunGraphSnapshot разрешает субъекта и передаёт отдельный путь чтения снимка графа, сохраняя контракт согласованности запуска и графа репозитория.
func (service *Service) GetRunGraphSnapshot(ctx context.Context, p value.Principal, ref string) (entity.Run, entity.RunGraph, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	return service.repository.GetRunGraphSnapshot(ctx, p, ref)
}

// ListRunEvents resolves the principal and returns repository events, total and continuation flag under the given filter; no event is synthesized or published.
// ListRunEvents разрешает субъекта и возвращает события репозитория, общее число и признак продолжения по фильтру; события не придумываются и не публикуются.
func (service *Service) ListRunEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.RunEvent, int64, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, false, err
	}
	return service.repository.ListRunEvents(ctx, p, filter)
}

// ListOwnerGates resolves the principal before reading decision gates with total and cursor; the read cannot approve a gate.
// ListOwnerGates разрешает субъекта перед чтением шлюзов решения с общим числом и курсором; чтение не подтверждает решение.
func (service *Service) ListOwnerGates(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.OwnerGate, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListOwnerGates(ctx, p, filter)
}

// GetOwnerGate resolves the principal and reads one owner gate through the repository; its reference is not a decision or approval token.
// GetOwnerGate разрешает субъекта и читает шлюз владельца через репозиторий; ссылка не является решением или токеном подтверждения.
func (service *Service) GetOwnerGate(ctx context.Context, p value.Principal, ref string) (entity.OwnerGate, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.OwnerGate{}, err
	}
	return service.repository.GetOwnerGate(ctx, p, ref)
}

// ListArtifacts resolves the principal and pages artifact metadata with total and cursor, leaving resource eligibility to the repository.
// ListArtifacts разрешает субъекта и читает страницы метаданных файлов с общим числом и курсором, оставляя допустимость ресурсов репозиторию.
func (service *Service) ListArtifacts(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Artifact, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListArtifacts(ctx, p, filter)
}

// GetArtifact resolves the principal and reads an artifact metadata record, separately from granting a download or preview.
// GetArtifact разрешает субъекта и читает запись метаданных файла отдельно от разрешения скачивания или предпросмотра.
func (service *Service) GetArtifact(ctx context.Context, p value.Principal, ref string) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	return service.repository.GetArtifact(ctx, p, ref)
}

// ListArtifactRevisions resolves the principal and pages revisions belonging to the supplied artifact reference, returning total and cursor from the repository.
// ListArtifactRevisions разрешает субъекта и читает страницы ревизий переданного файла, возвращая общее число и курсор репозитория.
func (service *Service) ListArtifactRevisions(ctx context.Context, p value.Principal, ref string, page query.Page) ([]entity.ArtifactRevision, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListArtifactRevisions(ctx, p, ref, page)
}

// GetArtifactRevision resolves the principal and rejects an empty revision reference before repository lookup under the artifact reference.
// GetArtifactRevision разрешает субъекта и отклоняет пустую ссылку ревизии перед поиском в репозитории в границе файла.
func (service *Service) GetArtifactRevision(ctx context.Context, p value.Principal, ref, revisionRef string) (entity.ArtifactRevision, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ArtifactRevision{}, err
	}
	if revisionRef == "" {
		return entity.ArtifactRevision{}, errs.ErrInvalid
	}
	return service.repository.GetArtifactRevision(ctx, p, ref, revisionRef)
}

// DownloadArtifactRevision resolves the principal and requires a revision reference plus the exact DOWNLOAD or PREVIEW purpose before requesting the repository download descriptor.
// DownloadArtifactRevision разрешает субъекта и требует ссылку ревизии и точную цель DOWNLOAD либо PREVIEW перед запросом дескриптора скачивания в репозитории.
func (service *Service) DownloadArtifactRevision(ctx context.Context, p value.Principal, ref, revisionRef, purpose string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if revisionRef == "" || purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return repository.ArtifactDownload{}, errs.ErrInvalid
	}
	return service.repository.DownloadArtifactRevision(ctx, p, ref, revisionRef, purpose)
}

// GetArtifactImpact resolves the principal, trims artifact reference and action and permits only DELETE or PURGE for a nonempty reference before reading impact.
// GetArtifactImpact разрешает субъекта, обрезает внешние пробелы ссылки файла и действия и допускает только DELETE либо PURGE для непустой ссылки перед чтением влияния.
func (service *Service) GetArtifactImpact(ctx context.Context, p value.Principal, ref, action string) (entity.ArtifactImpact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ArtifactImpact{}, err
	}
	ref = strings.TrimSpace(ref)
	action = strings.TrimSpace(action)
	if ref == "" || action != "DELETE" && action != "PURGE" {
		return entity.ArtifactImpact{}, errs.ErrInvalid
	}
	return service.repository.GetArtifactImpact(ctx, p, ref, action)
}

// GetAttachmentSet resolves the principal and rejects a blank set reference, negative or over-one-hundred page size, or cursor longer than two hundred fifty-six bytes before repository lookup.
// GetAttachmentSet разрешает субъекта и до чтения репозитория отклоняет пустую ссылку набора, отрицательный или превышающий сто размер страницы и курсор длиннее двухсот пятидесяти шести байт.
func (service *Service) GetAttachmentSet(ctx context.Context, p value.Principal, ref string, page query.Page) (entity.AttachmentSet, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AttachmentSet{}, "", err
	}
	if strings.TrimSpace(ref) == "" || page.Size < 0 || page.Size > 100 || len(page.Token) > 256 {
		return entity.AttachmentSet{}, "", errs.ErrInvalid
	}
	return service.repository.GetAttachmentSet(ctx, p, ref, page)
}

// UploadArtifact resolves the principal, bounds and hashes the seekable upload, checks any supplied digest, obtains artifactpolicy scan/preview metadata and rewinds before persistence. It assigns a content-bound mutation intent and validates it; it does not itself claim a ClamAV or semantic-model scan.
// UploadArtifact разрешает субъекта, ограничивает и хеширует загрузку с перемещаемой позицией, проверяет переданный дайджест, получает метаданные scan/preview от artifactpolicy и возвращает позицию к началу перед сохранением. Назначается связанное с содержимым намерение изменения с проверкой; собственное сканирование ClamAV или смысловой моделью не заявляется.
func (service *Service) UploadArtifact(ctx context.Context, p value.Principal, mutation value.Mutation, input repository.ArtifactUpload) (entity.Artifact, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Artifact{}, err
	}
	if input.SizeBytes < 0 || input.SizeBytes > repository.MaximumArtifactBytes || input.Reader == nil ||
		(strings.TrimSpace(input.ProjectRef) == "" && strings.TrimSpace(input.RunRef) != "") {
		return entity.Artifact{}, errs.ErrInvalid
	}
	actualDigest, err := hashUploadContent(input.Reader, input.SizeBytes, repository.MaximumArtifactBytes, input.Digest)
	if err != nil {
		return entity.Artifact{}, err
	}
	input.Digest = actualDigest
	verdict, err := artifactpolicy.InspectReader(input.FileName, input.MediaType, input.Reader, input.SizeBytes)
	if err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	input.MediaType = verdict.MediaType
	input.ScanState = verdict.ScanState
	input.PreviewState = verdict.PreviewState
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Artifact{}, errs.ErrUnavailable
	}
	mutation.Operation = "artifact.upload"
	mutation.IntentDigest = digest(struct {
		ProjectRef, RunRef, FileName, MediaType, Digest string
		SizeBytes                                       int64
	}{input.ProjectRef, input.RunRef, input.FileName, input.MediaType, input.Digest, input.SizeBytes})
	if err := mutation.Validate(); err != nil {
		return entity.Artifact{}, errs.ErrInvalid
	}
	return service.repository.UploadArtifact(ctx, p, mutation, input)
}

// UploadAgentAvatar resolves the principal and requires project/agent/version plus a one-byte through five-MiB seekable payload. After exact length/digest verification, policy must report CLEAN/AVAILABLE and JPEG, PNG or WebP; the reader is rewound and the version-bound mutation validated before persistence.
// UploadAgentAvatar разрешает субъекта и требует проект, сотрудника и версию и загрузку с перемещаемой позицией размером от байта до пяти МиБ. После точной проверки длины и дайджеста политика должна вернуть CLEAN/AVAILABLE и JPEG, PNG либо WebP; позиция возвращается к началу и связанное с версией изменение проверяется перед сохранением.
func (service *Service) UploadAgentAvatar(ctx context.Context, p value.Principal, mutation value.Mutation, input repository.AgentAvatarUpload) (entity.Agent, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Agent{}, err
	}
	if strings.TrimSpace(input.ProjectRef) == "" || strings.TrimSpace(input.AgentRef) == "" ||
		input.ExpectedVersion < 1 || input.SizeBytes < 1 || input.SizeBytes > 5<<20 || input.Reader == nil {
		return entity.Agent{}, errs.ErrInvalid
	}
	actualDigest, err := hashUploadContent(input.Reader, input.SizeBytes, 5<<20, input.Digest)
	if err != nil {
		return entity.Agent{}, err
	}
	input.Digest = actualDigest
	verdict, err := artifactpolicy.InspectReader(input.FileName, input.MediaType, input.Reader, input.SizeBytes)
	if err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	if verdict.ScanState != "CLEAN" || verdict.PreviewState != "AVAILABLE" ||
		!containsString([]string{"image/jpeg", "image/png", "image/webp"}, verdict.MediaType) {
		return entity.Agent{}, errs.ErrInvalid
	}
	input.MediaType, input.ScanState, input.PreviewState = verdict.MediaType, verdict.ScanState, verdict.PreviewState
	if _, err := input.Reader.Seek(0, io.SeekStart); err != nil {
		return entity.Agent{}, errs.ErrUnavailable
	}
	mutation.Operation = "agent.avatar.upload"
	mutation.IntentDigest = digest(struct {
		ProjectRef, AgentRef, FileName, MediaType, Digest string
		SizeBytes, ExpectedVersion                        int64
	}{input.ProjectRef, input.AgentRef, input.FileName, input.MediaType, input.Digest, input.SizeBytes, input.ExpectedVersion})
	if err := mutation.Validate(); err != nil {
		return entity.Agent{}, errs.ErrInvalid
	}
	return service.repository.UploadAgentAvatar(ctx, p, mutation, input)
}

// PurgeArtifact resolves the principal and requires trimmed artifact/impact references, expected version and a nonblank idempotency key. It assigns the purge intent digest then delegates; this wrapper does not call the general Mutation.Validate method.
// PurgeArtifact разрешает субъекта и требует очищенные ссылки файла и влияния, ожидаемую версию и непустой ключ идемпотентности. Затем назначает дайджест намерения удаления и передаёт выполнение; общий Mutation.Validate эта обёртка не вызывает.
func (service *Service) PurgeArtifact(ctx context.Context, p value.Principal, mutation value.Mutation, artifactRef, impactDigest string) (string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return "", err
	}
	artifactRef = strings.TrimSpace(artifactRef)
	impactDigest = strings.TrimSpace(impactDigest)
	if artifactRef == "" || impactDigest == "" || mutation.ExpectedVersion == nil || strings.TrimSpace(mutation.IdempotencyKey) == "" {
		return "", errs.ErrInvalid
	}
	mutation.Operation = "artifact.purge"
	mutation.IntentDigest = digest(struct {
		ArtifactRef, ImpactDigest string
		ExpectedVersion           int64
	}{ArtifactRef: artifactRef, ImpactDigest: impactDigest, ExpectedVersion: *mutation.ExpectedVersion})
	return service.repository.PurgeArtifact(ctx, p, mutation, artifactRef, impactDigest)
}

// DownloadArtifact resolves the principal and permits only DOWNLOAD or PREVIEW before requesting the repository descriptor for the current artifact.
// DownloadArtifact разрешает субъекта и допускает только DOWNLOAD либо PREVIEW перед запросом дескриптора текущего файла у репозитория.
func (service *Service) DownloadArtifact(ctx context.Context, p value.Principal, ref, purpose string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if purpose != "DOWNLOAD" && purpose != "PREVIEW" {
		return repository.ArtifactDownload{}, errs.ErrInvalid
	}
	return service.repository.DownloadArtifact(ctx, p, ref, purpose)
}

// ReadExecutionArtifact uses the shared execution-artifact guard with the exact read permission; it does not substitute the streaming-transfer permission.
// ReadExecutionArtifact использует общую проверку файла исполнения с точным полномочием чтения без подмены полномочием потоковой передачи.
func (service *Service) ReadExecutionArtifact(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, artifactRef string) (repository.ArtifactDownload, error) {
	return service.readExecutionArtifact(ctx, p, leaseRef, fence, generation, artifactRef, "platform.runtime.execution.artifact.read")
}

// SearchAssistantResources requires resolved runtime-controller authority, the exact assistant-resource search permission and nonblank lease/fence with positive generation. It checks the trimmed query's minimum and the original query's one-hundred-sixty-rune maximum, then delegates with trimmed text.
// SearchAssistantResources требует разрешённого субъекта runtime-controller, точного полномочия поиска ресурсов помощника и непустых lease/fence с положительным поколением. Проверяет минимум очищенного запроса и максимум сто шестьдесят символов исходного, затем передаёт очищенный текст.
func (service *Service) SearchAssistantResources(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, search string) ([]entity.SearchResult, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, false, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.assistant.resources.search" ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 ||
		len([]rune(strings.TrimSpace(search))) < 2 || len([]rune(search)) > 160 {
		return nil, false, errs.ErrForbidden
	}
	return service.repository.SearchAssistantResources(ctx, p, leaseRef, fence, generation, strings.TrimSpace(search))
}

// ListAssistantIntegrationDefinitions requires the resolved runtime-controller search authority and exact lease/fence/generation, then bounds trimmed search to eighty runes and offset to zero through ten thousand. Repository eligibility still applies to returned definitions.
// ListAssistantIntegrationDefinitions требует разрешённого полномочия поиска runtime-controller и точных lease/fence/generation, затем ограничивает очищенный поиск восемьюдесятью символами, а смещение диапазоном от нуля до десяти тысяч. Допустимость возвращённых определений по-прежнему проверяет репозиторий.
func (service *Service) ListAssistantIntegrationDefinitions(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, search string, offset int32) ([]entity.AssistantIntegrationDefinition, int32, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.assistant.resources.search" ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 ||
		len([]rune(strings.TrimSpace(search))) > 80 || offset < 0 || offset > 10000 {
		return nil, 0, errs.ErrForbidden
	}
	return service.repository.ListAssistantIntegrationDefinitions(ctx, p, leaseRef, fence, generation, strings.TrimSpace(search), offset)
}

// ListAssistantConfigurationCatalog requires resolved runtime-controller search authority and lease bindings, then validates the closed catalog kind, account/profile selectors, query/offset bounds and paired entity selectors. Workflow/agent/current-configuration reads have exact empty-query/zero-offset rules before owner-scoped repository lookup.
// ListAssistantConfigurationCatalog требует разрешённого полномочия поиска runtime-controller и привязок аренды, затем проверяет закрытый вид каталога, селекторы аккаунта и профиля, пределы запроса и смещения и парные селекторы сущности. Перед чтением репозитория в границе владельца для конфигураций процесса, сотрудника и текущей конфигурации действуют точные правила пустого запроса и нулевого смещения.
func (service *Service) ListAssistantConfigurationCatalog(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, input entity.AssistantConfigurationCatalogRequest) (entity.AssistantConfigurationCatalogResponse, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AssistantConfigurationCatalogResponse{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.assistant.resources.search" ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrForbidden
	}
	validKind := false
	switch input.Kind {
	case "ASSISTANTS", "RUNTIME_PROFILES", "PROVIDER_ACCOUNTS", "MODELS", "ROLE_IMAGE_RECIPES", "IMAGE_ARTIFACTS", "ROLE_ENVIRONMENTS", "CURRENT_CONFIGURATION", "PROJECT_INTEGRATION_GRANTS", "RECIPIENT_INTEGRATION_GRANTS", "WORKFLOW_CONFIGURATION", "AGENT_CONFIGURATION", "AGENT_RUNTIME_CONFIGURATION", "WORKFLOW_METADATA":
		validKind = true
	}
	if !validKind || len(input.AssistantRef) < 8 || len(input.AssistantRef) > 128 || len([]rune(input.Query)) > 80 || input.Offset < 0 || input.Offset > 10000 ||
		(input.Kind != "MODELS" && input.AccountRef != "") || (input.Kind == "MODELS" && (len(input.AccountRef) < 8 || len(input.AccountRef) > 128)) ||
		(input.Kind != "MODELS" && input.Kind != "PROVIDER_ACCOUNTS" && input.RuntimeProfileRef != "") || len(input.RuntimeProfileRef) > 128 {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrInvalid
	}
	input.Query = strings.TrimSpace(input.Query)
	if (input.EntityKind == "") != (input.EntityRef == "") || input.EntityKind != "" &&
		(input.Kind != "RECIPIENT_INTEGRATION_GRANTS" && input.Kind != "WORKFLOW_CONFIGURATION" && input.Kind != "WORKFLOW_METADATA" && input.Kind != "AGENT_CONFIGURATION" && input.Kind != "AGENT_RUNTIME_CONFIGURATION" ||
			input.EntityKind != "AGENT" && input.EntityKind != "WORKFLOW" || !validAssistantCatalogEntityRef(input.EntityRef)) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrInvalid
	}
	if (input.Kind == "WORKFLOW_CONFIGURATION" || input.Kind == "WORKFLOW_METADATA") && (input.EntityKind != "WORKFLOW" || input.Query != "" || input.Offset != 0) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrInvalid
	}
	if (input.Kind == "AGENT_CONFIGURATION" || input.Kind == "AGENT_RUNTIME_CONFIGURATION") && (input.EntityKind != "AGENT" || input.Query != "" || input.Offset != 0) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrInvalid
	}
	if input.Kind == "CURRENT_CONFIGURATION" && (input.Query != "" || input.Offset != 0) {
		return entity.AssistantConfigurationCatalogResponse{}, errs.ErrInvalid
	}
	return service.repository.ListAssistantConfigurationCatalog(ctx, p, leaseRef, fence, generation, input)
}

// validAssistantCatalogEntityRef accepts only eight through one hundred twenty-eight bytes with no leading/trailing whitespace or NUL/CR/LF/tab/space characters. It checks shape, not resource prefix, existence or owner eligibility.
// validAssistantCatalogEntityRef принимает только от восьми до ста двадцати восьми байт без внешних пробелов и символов NUL/CR/LF/tab/space. Проверяется форма, а не префикс ресурса, существование или допустимость владельца.
func validAssistantCatalogEntityRef(ref string) bool {
	return len(ref) >= 8 && len(ref) <= 128 && strings.TrimSpace(ref) == ref && !strings.ContainsAny(ref, "\x00\r\n\t ")
}

// OpenExecutionArtifactTransfer uses the shared execution-artifact guard with the exact stream permission; opening this repository path is distinct from ordinary artifact download.
// OpenExecutionArtifactTransfer использует общую проверку файла исполнения с точным полномочием stream; этот путь репозитория отличается от обычного скачивания файла.
func (service *Service) OpenExecutionArtifactTransfer(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, artifactRef string) (repository.ArtifactDownload, error) {
	return service.readExecutionArtifact(ctx, p, leaseRef, fence, generation, artifactRef, "platform.runtime.execution.artifact.stream")
}

// readExecutionArtifact resolves the principal, requires runtime-controller and the supplied exact permission, and rejects blank lease/fence/artifact or nonpositive generation before repository artifact reading. It does not replace repository lease and artifact-eligibility checks.
// readExecutionArtifact разрешает субъекта, требует runtime-controller и переданное точное полномочие и отклоняет пустые lease/fence/artifact или неположительное поколение перед чтением файла в репозитории. Проверки аренды и допустимости файла репозитория этим не заменяются.
func (service *Service) readExecutionArtifact(ctx context.Context, p value.Principal, leaseRef, fence string, generation int64, artifactRef, permission string) (repository.ArtifactDownload, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.ArtifactDownload{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != permission ||
		strings.TrimSpace(leaseRef) == "" || strings.TrimSpace(fence) == "" || generation < 1 || strings.TrimSpace(artifactRef) == "" {
		return repository.ArtifactDownload{}, errs.ErrForbidden
	}
	return service.repository.ReadExecutionArtifact(ctx, p, leaseRef, fence, generation, artifactRef)
}

// ListSchedules resolves the principal and pages schedule metadata with the supplied filter without claiming due occurrences.
// ListSchedules разрешает субъекта и читает страницы метаданных автоматизаций с переданным фильтром без захвата наступивших запусков.
func (service *Service) ListSchedules(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.Schedule, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListSchedules(ctx, p, filter)
}

// GetSchedule resolves the principal and rejects a whitespace-only reference before forwarding its original value to repository lookup.
// GetSchedule разрешает субъекта и отклоняет ссылку из одних пробелов перед передачей исходного значения поиску репозитория.
func (service *Service) GetSchedule(ctx context.Context, p value.Principal, ref string) (entity.Schedule, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.Schedule{}, err
	}
	if strings.TrimSpace(ref) == "" {
		return entity.Schedule{}, errs.ErrInvalid
	}
	return service.repository.GetSchedule(ctx, p, ref)
}

// ListScheduleRevisions resolves the principal before paging stored schedule revisions; no revision is activated or rolled back.
// ListScheduleRevisions разрешает субъекта перед чтением страниц сохранённых ревизий автоматизации без активации или отката версии.
func (service *Service) ListScheduleRevisions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ScheduleRevision, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListScheduleRevisions(ctx, p, filter)
}

// ListScheduleRuns resolves the principal and pages occurrence/run metadata according to the repository filter, without materializing new runs.
// ListScheduleRuns разрешает субъекта и читает страницы метаданных срабатываний и запусков по фильтру репозитория без материализации новых запусков.
func (service *Service) ListScheduleRuns(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.ScheduleRunOccurrence, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListScheduleRuns(ctx, p, filter)
}

// ListIntegrationDefinitions resolves the principal and reads definition metadata, cursor and action metadata; it does not open an integration connection.
// ListIntegrationDefinitions разрешает субъекта и читает метаданные определений, курсор и метаданные действий без открытия соединения интеграции.
func (service *Service) ListIntegrationDefinitions(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationDefinition, string, []string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", nil, err
	}
	return service.repository.ListIntegrationDefinitions(ctx, p, filter)
}

// ListIntegrationConnections resolves the principal before paging connection metadata, separately from credential setup or a connection test.
// ListIntegrationConnections разрешает субъекта перед чтением страниц метаданных соединений отдельно от настройки реквизитов или проверки соединения.
func (service *Service) ListIntegrationConnections(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.IntegrationConnection, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListIntegrationConnections(ctx, p, filter)
}

// GetIntegrationConnection resolves the principal and reads one connection through repository ownership checks without invoking its remote capability.
// GetIntegrationConnection разрешает субъекта и читает соединение через проверки владения репозитория без вызова удалённой возможности.
func (service *Service) GetIntegrationConnection(ctx context.Context, p value.Principal, ref string) (entity.IntegrationConnection, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.IntegrationConnection{}, err
	}
	return service.repository.GetIntegrationConnection(ctx, p, ref)
}

// GetSystemAssistant resolves the principal and reads the system-assistant view from the repository; it does not start a conversation or recover a runtime.
// GetSystemAssistant разрешает субъекта и читает представление системного помощника из репозитория без начала диалога или восстановления runtime.
func (service *Service) GetSystemAssistant(ctx context.Context, p value.Principal) (entity.SystemAssistant, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	return service.repository.GetSystemAssistant(ctx, p)
}

// GetProjectAssistant resolves the principal and requires a nonblank project reference before repository lookup of that project's assistant profile.
// GetProjectAssistant разрешает субъекта и требует непустую ссылку проекта перед чтением профиля его помощника в репозитории.
func (service *Service) GetProjectAssistant(ctx context.Context, p value.Principal, projectRef string) (entity.ProjectAssistantProfile, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProjectAssistantProfile{}, err
	}
	if strings.TrimSpace(projectRef) == "" {
		return entity.ProjectAssistantProfile{}, errs.ErrInvalid
	}
	return service.repository.GetProjectAssistant(ctx, p, projectRef)
}

// ListAssistantConversations resolves the principal and pages conversations under the supplied assistant scope/filter; history and drafts are not combined locally.
// ListAssistantConversations разрешает субъекта и читает страницы диалогов по переданным области и фильтру помощника без локального объединения истории и черновиков.
func (service *Service) ListAssistantConversations(ctx context.Context, p value.Principal, filter query.AssistantConversationFilter) ([]entity.AssistantConversation, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAssistantConversations(ctx, p, filter)
}

// GetAdministration resolves the principal and reads the repository administration view; resolution alone is not an administrator-role grant.
// GetAdministration разрешает субъекта и читает административное представление репозитория; само разрешение субъекта не выдаёт роль администратора.
func (service *Service) GetAdministration(ctx context.Context, p value.Principal) (repository.Administration, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return repository.Administration{}, err
	}
	return service.repository.GetAdministration(ctx, p)
}

// ListAuditEvents resolves the principal before paging audit records with the given filter, preserving the repository's visibility and cursor boundary.
// ListAuditEvents разрешает субъекта перед чтением страниц записей аудита с фильтром, сохраняя границу видимости и курсора репозитория.
func (service *Service) ListAuditEvents(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.AuditEvent, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAuditEvents(ctx, p, filter)
}

// ListPermissionRegistry resolves the principal and reads permission definitions, not the subject's effective grants.
// ListPermissionRegistry разрешает субъекта и читает определения полномочий, а не действующие назначения субъекта.
func (service *Service) ListPermissionRegistry(ctx context.Context, p value.Principal) ([]entity.PermissionDefinition, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListPermissionRegistry(ctx, p)
}

// ListAccessSubjects resolves the principal and forwards the subject filter and kind to repository paging without assigning membership or bindings.
// ListAccessSubjects разрешает субъекта и передаёт фильтр и вид субъекта постраничному чтению репозитория без назначения членства или привязок.
func (service *Service) ListAccessSubjects(ctx context.Context, p value.Principal, filter query.Filter, kind string) ([]entity.AccessSubject, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessSubjects(ctx, p, filter, kind)
}

// ListOIDCGroups resolves the principal and reads repository OIDC-group metadata rather than querying an identity provider directly.
// ListOIDCGroups разрешает субъекта и читает метаданные OIDC-групп репозитория вместо прямого запроса провайдера идентичности.
func (service *Service) ListOIDCGroups(ctx context.Context, p value.Principal, filter query.Filter) ([]entity.OIDCGroup, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListOIDCGroups(ctx, p, filter)
}

// ListAccessRoles resolves the principal and pages access-role metadata with an explicit includeArchived choice; archived-role visibility does not reactivate a role.
// ListAccessRoles разрешает субъекта и читает страницы ролей доступа с явным includeArchived; видимость архивной роли не активирует её снова.
func (service *Service) ListAccessRoles(ctx context.Context, p value.Principal, filter query.Filter, includeArchived bool) ([]entity.AccessRole, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessRoles(ctx, p, filter, includeArchived)
}

// ListAccessRoleVersions resolves the principal and returns the role plus a page of stored versions and cursor; reading a version does not publish it.
// ListAccessRoleVersions разрешает субъекта и возвращает роль, страницу сохранённых версий и курсор; чтение версии её не публикует.
func (service *Service) ListAccessRoleVersions(ctx context.Context, p value.Principal, roleRef string, page query.Page) (entity.AccessRole, []entity.AccessRoleVersion, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AccessRole{}, nil, "", err
	}
	return service.repository.ListAccessRoleVersions(ctx, p, roleRef, page)
}

// ListAccessBindings resolves the principal and pages the repository's access bindings with the typed filter; it does not change grant state.
// ListAccessBindings разрешает субъекта и читает страницы привязок доступа репозитория с типизированным фильтром без изменения состояния полномочий.
func (service *Service) ListAccessBindings(ctx context.Context, p value.Principal, filter query.AccessBindingFilter) ([]entity.AccessBinding, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListAccessBindings(ctx, p, filter)
}

// QueryEffectiveAccess resolves the caller principal and delegates evaluation for the requested subject, target, permission keys and time. The wrapper does not accept those query fields as caller authority.
// QueryEffectiveAccess разрешает вызывающего субъекта и передаёт оценку запрошенных субъекта, цели, ключей полномочий и времени. Поля запроса не принимаются обёрткой за полномочия вызывающего.
func (service *Service) QueryEffectiveAccess(ctx context.Context, p value.Principal, subjectRef string, target entity.AccessScope, permissionKeys []string, evaluatedAt time.Time) (entity.EffectiveAccess, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EffectiveAccess{}, err
	}
	return service.repository.QueryEffectiveAccess(ctx, p, subjectRef, target, permissionKeys, evaluatedAt)
}

// SimulateAccess resolves the principal and forwards the typed simulation input to the repository without applying simulated bindings as real grants.
// SimulateAccess разрешает субъекта и передаёт типизированный вход симуляции репозиторию, не применяя моделируемые привязки как реальные полномочия.
func (service *Service) SimulateAccess(ctx context.Context, p value.Principal, input command.AccessSimulationInput) (entity.AccessSimulation, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.AccessSimulation{}, err
	}
	return service.repository.SimulateAccess(ctx, p, input)
}

// Execute rejects a workflow-child launch from the wrong workload/permission before principal resolution, then replaces the command principal with its authoritative resolution and enters executeResolved. Payload identifiers are not resolved as the caller's identity here.
// Execute отклоняет дочерний запуск процесса с неверными workload или полномочием до разрешения субъекта, затем заменяет субъекта команды его авторитетным разрешением и вызывает executeResolved. Идентификаторы payload здесь не разрешаются как идентичность вызывающего.
func (service *Service) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	if input.Kind == command.LaunchWorkflowExecution && (input.Principal.CallerWorkload != "runtime-controller" || input.Principal.Permission != "platform.runtime.execution.workflow.launch") {
		return command.Result{}, errs.ErrForbidden
	}
	principal, err := service.principal(ctx, input.Principal)
	if err != nil {
		return command.Result{}, err
	}
	input.Principal = principal
	return service.executeResolved(ctx, input)
}

// executeResolved requires a known kind and nonnil payload, checks assistant-turn content, assigns mutation operation and a bounded credential-specific intent shape, then validates mutation. It protects the system assistant and exact scheduler operations before repository execution; the closed kind list is not a replacement for command-specific owner authorization.
// executeResolved требует известный вид и ненулевой payload, проверяет содержимое хода помощника, назначает операцию изменения и специальную ограниченную форму намерения для реквизитов, затем проверяет изменение. До исполнения в репозитории защищает системного помощника и точные операции планировщика; закрытый список видов не заменяет авторизацию конкретной команды владельцем.
func (service *Service) executeResolved(ctx context.Context, input command.Command) (command.Result, error) {
	if !knownCommand(input.Kind) || input.Payload == nil {
		return command.Result{}, errs.ErrInvalid
	}
	if input.Kind == command.AddAssistantTurn {
		payload, ok := input.Payload.(command.AssistantTurnInput)
		if !ok || !runtimecontract.ValidAssistantTurnContent(payload.Content) {
			return command.Result{}, errs.ErrInvalid
		}
	}
	input.Mutation.Operation = "controlplane." + strings.ToLower(string(input.Kind))
	intentPayload := input.Payload
	if input.Kind == command.ConfigureEmailCredential {
		payload, ok := input.Payload.(command.EmailCredentialInput)
		if !ok {
			return command.Result{}, errs.ErrInvalid
		}
		intentPayload = struct {
			ConnectionRef, Name, Kind, Digest string
			Generation                        int64
		}{
			payload.ConnectionRef, payload.Credential.Name, payload.Credential.Kind, payload.Credential.ContentSHA256, payload.Credential.Generation,
		}
	}
	if input.Kind == command.ConfigureConnectionCredential {
		payload, ok := input.Payload.(command.ConnectionInput)
		if !ok || payload.CredentialRevision == nil {
			return command.Result{}, errs.ErrInvalid
		}
		intentPayload = struct {
			Ref, MaterializationRef, ContentSHA256 string
		}{payload.Ref, payload.MaterializationRef, payload.CredentialRevision.ContentSHA256}
	}
	input.Mutation.IntentDigest = digest(struct {
		Kind    command.Kind
		Payload any
	}{input.Kind, intentPayload})
	if err := input.Mutation.Validate(); err != nil {
		return command.Result{}, errs.ErrInvalid
	}
	if input.Kind == command.SetAgentEnabled {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" && !payload.Enabled {
			return command.Result{}, errs.ErrProtected
		}
	}
	if input.Kind == command.MaterializeOccurrence &&
		(input.Principal.CallerWorkload != "automation-scheduler" || input.Principal.Permission != "platform.runtime.schedules.materialize") {
		return command.Result{}, errs.ErrForbidden
	}
	if input.Kind == command.FailScheduleOccurrence &&
		(input.Principal.CallerWorkload != "automation-scheduler" || input.Principal.Permission != "platform.runtime.schedules.fail") {
		return command.Result{}, errs.ErrForbidden
	}
	if input.Kind == command.ArchiveAgent {
		payload, ok := input.Payload.(command.AgentInput)
		if !ok || payload.Ref == "system-assistant" {
			return command.Result{}, errs.ErrProtected
		}
	}
	return service.repository.Execute(ctx, input)
}

// ReconcileWarmRuntime resolves the principal and rejects a blank instance before owner-side warm reconciliation. It forwards the original instance and does not report runtime readiness itself.
// ReconcileWarmRuntime разрешает субъекта и отклоняет пустой экземпляр перед сверкой тёплого runtime на стороне владельца. Передаётся исходный экземпляр, готовность runtime самостоятельно не сообщается.
func (service *Service) ReconcileWarmRuntime(ctx context.Context, p value.Principal, instance string) (entity.SystemAssistant, map[string]any, bool, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	if strings.TrimSpace(instance) == "" {
		return entity.SystemAssistant{}, nil, false, errs.ErrInvalid
	}
	return service.repository.ReconcileWarmRuntime(ctx, p, instance)
}

// ReportWarmRuntime resolves the principal and requires runtime-controller with the exact warm-report permission before delegating the report; workload claims in report payload are not sufficient authority.
// ReportWarmRuntime разрешает субъекта и требует runtime-controller с точным полномочием warm-report перед передачей отчёта; заявлений workload в payload отчёта недостаточно для полномочий.
func (service *Service) ReportWarmRuntime(ctx context.Context, p value.Principal, input command.WarmRuntimeInput) (entity.SystemAssistant, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	if p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.warm.report" {
		return entity.SystemAssistant{}, errs.ErrForbidden
	}
	return service.repository.ReportWarmRuntime(ctx, p, input)
}

// ClaimSessionArchiveTasks resolves the principal and requires the exact session-archive claim authority, nonblank instance and batch size one through sixteen. Failed authority or bounds return ErrForbidden before repository claiming.
// ClaimSessionArchiveTasks разрешает субъекта и требует точные полномочия захвата session-archive, непустой экземпляр и размер пачки от одного до шестнадцати. Нарушение полномочий или пределов даёт ErrForbidden до захвата в репозитории.
func (service *Service) ClaimSessionArchiveTasks(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "session-archive" || p.Permission != "platform.session-archive.tasks.claim" ||
		strings.TrimSpace(instance) == "" || limit < 1 || limit > 16 {
		return nil, errs.ErrForbidden
	}
	return service.repository.ClaimSessionArchiveTasks(ctx, p, instance, limit)
}

// RenewSessionArchiveTask resolves the principal and requires session-archive with the exact renew permission before the repository checks and renews the task lease.
// RenewSessionArchiveTask разрешает субъекта и требует session-archive с точным полномочием продления до проверки и продления аренды задания репозиторием.
func (service *Service) RenewSessionArchiveTask(ctx context.Context, p value.Principal, input command.SessionArchiveTaskInput) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "session-archive" || p.Permission != "platform.session-archive.tasks.renew" {
		return nil, errs.ErrForbidden
	}
	return service.repository.RenewSessionArchiveTask(ctx, p, input)
}

// ClaimDueSchedules resolves the principal and enforces automation-scheduler claim authority, then requires a nonblank instance and one-through-one-hundred-twenty-eight limit. Authority and input failures retain distinct Forbidden and Invalid classes.
// ClaimDueSchedules разрешает субъекта и проверяет полномочие захвата automation-scheduler, затем требует непустой экземпляр и лимит от одного до ста двадцати восьми. Ошибки полномочий и ввода сохраняют разные классы Forbidden и Invalid.
func (service *Service) ClaimDueSchedules(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "automation-scheduler" || p.Permission != "platform.runtime.schedules.claim" {
		return nil, errs.ErrForbidden
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 128 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimDueSchedules(ctx, p, instance, limit)
}

// RenewScheduleOccurrence resolves the principal and requires automation-scheduler with the exact renewal permission before repository occurrence-lease renewal.
// RenewScheduleOccurrence разрешает субъекта и требует automation-scheduler с точным полномочием продления перед продлением аренды срабатывания в репозитории.
func (service *Service) RenewScheduleOccurrence(ctx context.Context, p value.Principal, input command.OccurrenceInput) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if p.CallerWorkload != "automation-scheduler" || p.Permission != "platform.runtime.schedules.renew" {
		return nil, errs.ErrForbidden
	}
	return service.repository.RenewScheduleOccurrence(ctx, p, input)
}

// PreviewSchedule resolves the principal and requires the gateway's schedule-preview permission, defaulting a zero start time to current UTC. It normalizes and previews the schedule locally, mapping either validation error to ErrInvalid without creating an occurrence.
// PreviewSchedule разрешает субъекта и требует полномочие предпросмотра автоматизации шлюза, заменяя нулевое начальное время текущим UTC. Локально нормализует и просматривает расписание, преобразуя ошибки обоих этапов в ErrInvalid без создания срабатывания.
func (service *Service) PreviewSchedule(ctx context.Context, p value.Principal, spec scheduleservice.Spec, after time.Time, limit int32) (scheduleservice.Normalized, []time.Time, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return scheduleservice.Normalized{}, nil, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.query.schedules.preview" {
		return scheduleservice.Normalized{}, nil, errs.ErrForbidden
	}
	if after.IsZero() {
		after = time.Now().UTC()
	}
	normalized, err := scheduleservice.Normalize(spec, after)
	if err != nil {
		return scheduleservice.Normalized{}, nil, errs.ErrInvalid
	}
	occurrences, err := scheduleservice.Preview(normalized.Spec, after, int(limit))
	if err != nil {
		return scheduleservice.Normalized{}, nil, errs.ErrInvalid
	}
	return normalized, occurrences, nil
}

// ClaimIntegrationConnectionTests resolves the principal and requires a nonblank instance and batch size one through thirty-two before repository claim; this wrapper does not itself run a remote connection test.
// ClaimIntegrationConnectionTests разрешает субъекта и требует непустой экземпляр и размер пачки от одного до тридцати двух перед захватом в репозитории; сама обёртка удалённую проверку соединения не запускает.
func (service *Service) ClaimIntegrationConnectionTests(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationConnectionTests(ctx, p, instance, limit)
}

// ResolveIntegrationInvocation resolves the principal and requires nonblank run, node, connection, capability and idempotency references, then forwards both metadata and bounded input to the repository. Presence checks do not prove ownership or provenance.
// ResolveIntegrationInvocation разрешает субъекта и требует непустые ссылки запуска, узла, соединения, возможности и идемпотентности, затем передаёт метаданные и ограниченный ввод репозиторию. Проверки наличия не доказывают владение или происхождение.
func (service *Service) ResolveIntegrationInvocation(ctx context.Context, p value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"run_ref", "node_ref", "connection_ref", "capability_key", "idempotency_key"} {
		if strings.TrimSpace(input[key]) == "" {
			return nil, errs.ErrInvalid
		}
	}
	return service.repository.ResolveIntegrationInvocation(ctx, p, input, boundedInput)
}

// ClaimIntegrationInvocations resolves the principal and validates instance plus one-through-thirty-two batch size before repository claiming; it does not perform the external integration effect.
// ClaimIntegrationInvocations разрешает субъекта и проверяет экземпляр и размер пачки от одного до тридцати двух перед захватом в репозитории; внешний эффект интеграции не выполняется.
func (service *Service) ClaimIntegrationInvocations(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimIntegrationInvocations(ctx, p, instance, limit)
}

// GetIntegrationInvocation resolves the principal and requires a nonblank invocation reference before reading its original reference through the repository boundary.
// GetIntegrationInvocation разрешает субъекта и требует непустую ссылку вызова перед чтением по исходному значению через границу репозитория.
func (service *Service) GetIntegrationInvocation(ctx context.Context, p value.Principal, ref string) (map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ref) == "" {
		return nil, errs.ErrInvalid
	}
	return service.repository.GetIntegrationInvocation(ctx, p, ref)
}

// ListInteractionSources resolves the principal and reads the repository's interaction-source metadata without polling external channels in this wrapper.
// ListInteractionSources разрешает субъекта и читает метаданные источников взаимодействия репозитория без опроса внешних каналов в обёртке.
func (service *Service) ListInteractionSources(ctx context.Context, p value.Principal) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	return service.repository.ListInteractionSources(ctx, p)
}

// ClaimInteractionDeliveries resolves the principal and validates nonblank instance and one-through-thirty-two batch size before repository delivery claim; claiming does not itself complete a delivery.
// ClaimInteractionDeliveries разрешает субъекта и проверяет непустой экземпляр и размер пачки от одного до тридцати двух перед захватом доставки в репозитории; сам захват доставку не завершает.
func (service *Service) ClaimInteractionDeliveries(ctx context.Context, p value.Principal, instance string, limit int32) ([]map[string]any, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(instance) == "" || limit < 1 || limit > 32 {
		return nil, errs.ErrInvalid
	}
	return service.repository.ClaimInteractionDeliveries(ctx, p, instance, limit)
}

// knownCommand recognizes only the explicit supported command kinds across several closed groups; unknown values return false. Membership does not validate payload shape, caller authority or lifecycle preconditions, which are enforced by subsequent command paths.
// knownCommand распознаёт только явно поддерживаемые виды команд в нескольких закрытых группах; неизвестные значения дают false. Вхождение не проверяет форму payload, полномочия вызывающего или предусловия жизненного цикла — это проверяют последующие пути команд.
func knownCommand(kind command.Kind) bool {
	switch kind {
	case command.PrepareRoleImageGitWriteBack, command.PrepareIntegrationDefinitionGitWriteBack, command.ApproveManagedConfigurationGitWriteBack, command.RejectManagedConfigurationGitWriteBack, command.CancelManagedConfigurationGitWriteBack:
		return true
	}
	if kind == command.ConfigureRoleImageGitSource || kind == command.ConfigureIntegrationDefinitionGitSource || kind == command.RefreshRoleImageGitSource || kind == command.RefreshIntegrationDefinitionGitSource {
		return true
	}
	switch kind {
	case command.ReconcileEmailEffect, command.ReportEmailEffect:
		return true
	case command.BindAgentMemoryRecord, command.UnbindAgentMemoryRecord, command.BindAgentSkillBundle, command.UnbindAgentSkillBundle:
		return true
	case command.CreateSkillBundleDraft, command.SaveSkillBundleDraft, command.ValidateSkillBundleDraft:
		return true
	case command.ReviewSkillBundleDraft, command.PublishSkillBundleDraft, command.DiscardSkillBundleDraft, command.ArchiveSkillBundle, command.RestoreSkillBundle, command.PurgeSkillBundle:
		return true
	case command.CreateMemoryRecord, command.ReviseMemoryRecord, command.ArchiveMemoryRecord, command.RestoreMemoryRecord, command.PurgeMemoryRecord:
		return true
	case command.CreateRuntimeEnvironmentDraft, command.CreateOrganizationRuntimeEnvironmentDraft, command.SaveRuntimeEnvironmentDraft, command.ValidateRuntimeEnvironmentDraft,
		command.PrepareEnvironmentDraftImpact, command.PublishRuntimeEnvironmentDraft, command.DiscardRuntimeEnvironmentDraft, command.RebindRuntimeEnvironment, command.RebindRuntimeSecret, command.BindInteractionIdentity, command.RevokeInteractionIdentity:
		return true
	case command.CompleteOnboarding, command.CreateProject, command.CreateProjectFile, command.CreateProjectFileRevision, command.UpdateProject, command.TrashProject, command.RestoreProject, command.PurgeProject, command.ResolveProjectPurgeRecovery,
		command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership,
		command.AddMembership, command.ChangeMembership, command.RemoveMembership,
		command.CreateAgent, command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent,
		command.CreateAssistantRoleImageRecipe, command.UpdateAssistantRoleImageRecipe,
		command.CreateSystemAssistantRoleImageRecipe, command.UpdateSystemAssistantRoleImageRecipe,
		command.PublishAssistantRuntimeConfig,
		command.SetAgentAvatar, command.RemoveAgentAvatar,
		command.PrepareInstructionsImpact, command.PreparePromptTemplateImpact,
		command.CreateInstructions, command.ValidateInstructions, command.PublishInstructions,
		command.RollbackInstructions, command.PublishAgentRuntimeConfig,
		command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft, command.PublishConfigOverlayDraft,
		command.RollbackConfigOverlay, command.CreateRuntimeEnvironment, command.PublishRuntimeEnvironment,
		command.RollbackRuntimeEnvironment, command.SetRuntimeEnvironmentEnabled, command.DeleteRuntimeEnvironment,
		command.BindAgentRuntimeEnvironment,
		command.ChangeAgentCapability, command.ChangeAgentGrant,
		command.CreateWorkflow, command.UpdateWorkflow,
		command.ValidateWorkflow, command.PublishWorkflow, command.ArchiveWorkflow,
		command.LaunchRun, command.AddSessionTurn, command.CancelRun, command.RetryRun,
		command.ResolveOwnerGate, command.ChangeArtifactBinding, command.DeleteArtifact,
		command.RestoreArtifact, command.CreateAttachmentSetDraft, command.AddAttachmentSetItems,
		command.RemoveAttachmentSetItems, command.FinalizeAttachmentSet, command.CreateSchedule,
		command.UpdateSchedule, command.SetScheduleEnabled, command.ArchiveSchedule, command.DeleteSchedule,
		command.CreateProviderAccount, command.StartProviderDeviceAuth, command.AuthorizeProviderAPIKey,
		command.RefreshProviderAuthorization, command.VerifyProviderAuthorization, command.CancelProviderAccountQueuedWork, command.RevokeProviderAccount, command.DeleteProviderAccount, command.SetProviderAccountEnabled, command.SetProviderAccountConcurrency,
		command.CreateConnection, command.UpdateConnection, command.DeleteConnection,
		command.ConfigureConnectionCredential, command.ConfigureEmailCredential,
		command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant, command.ChangeSystemAssistantIntegrationGrant, command.ChangeProjectAssistantIntegrationGrant,
		command.CreateProjectAssistant, command.CreateAssistantConversation, command.UpdateAssistantConversation, command.ArchiveAssistantConversation, command.RestoreAssistantConversation, command.PurgeAssistantConversation, command.MoveAssistantConversationToProject, command.AddAssistantTurn, command.CancelAssistantTurn,
		command.UpdateAssistantPlan, command.ValidateAssistantPlan, command.ApplyAssistantPlan, command.RejectAssistantPlan,
		command.UpdateAssistantInstructions, command.RecoverAssistant, command.ClaimExecution,
		command.RenewExecution, command.ReportExecutionProgress, command.CommitProviderCredentialRefresh,
		command.CompleteExecution,
		command.DelegateExecution, command.LaunchWorkflowExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall,
		command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask,
		command.MaterializeOccurrence, command.FailScheduleOccurrence, command.CompleteConnectionTest,
		command.CompleteIntegrationInvocation, command.CompleteInteractionDelivery,
		command.AcceptInteractionMessage, command.CreateAccessRole,
		command.CreateAccessRoleVersion, command.ArchiveAccessRole,
		command.CreateAccessBinding, command.ChangeAccessBinding, command.RevokeAccessBinding,
		command.CreatePromptTemplateDraft, command.ValidatePromptTemplateDraft,
		command.PublishPromptTemplateDraft, command.RebindPromptTemplate,
		command.SavePromptTemplateDraft,
		command.DiscardPromptTemplateDraft,
		command.SaveRoleImageRevisionDraft,
		command.DiscardRoleImageRevisionDraft,
		command.SaveIntegrationDefinitionDraft,
		command.DiscardIntegrationDefinitionDraft,
		command.SaveSystemSTTConfigurationDraft,
		command.DiscardSystemSTTConfigurationDraft,
		command.CreateRoleImageRevisionDraft, command.ValidateRoleImageRevision,
		command.PublishRoleImageRevision, command.RebindRoleImage, command.PrepareRoleImageImpactPlan,
		command.CreateIntegrationDefinition, command.ValidateIntegrationDefinition,
		command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.CreateSystemSTTDraft, command.ValidateSystemSTTDraft,
		command.PublishSystemSTTDraft, command.RebindSystemSTT,
		command.DetachGitManagedConfiguration, command.CopyGitManagedConfiguration,
		command.CopyRoleImageConfiguration, command.CopyIntegrationDefinitionConfiguration, command.ArchiveRoleImageConfiguration, command.ArchiveIntegrationDefinitionConfiguration,
		command.CreateEmailMailboxDraft, command.SaveEmailMailboxDraft, command.ValidateEmailMailboxDraft,
		command.PublishEmailMailboxDraft, command.DiscardEmailMailboxDraft,
		command.BindEmailMailboxConfiguration, command.UnbindEmailMailboxConfiguration:
		return true
	default:
		return false
	}
}

// digest hashes ordinary JSON bytes with SHA-256 and lowercase hexadecimal encoding. If JSON marshaling fails it hashes a type-only unsupported marker instead; this helper is neither canonical JCS serialization nor proof of unique intent for unsupported values.
// digest хеширует обычные JSON-байты через SHA-256 с шестнадцатеричным выводом нижнего регистра. При ошибке JSON хеширует маркер неподдерживаемого типа без значения; это не каноническая JCS-сериализация и не доказательство уникального намерения неподдерживаемых значений.
func digest(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		encoded = []byte(fmt.Sprintf("unsupported:%T", value))
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// GetProviderFinancialOverview resolves the principal before reading the repository's provider financial overview; it does not fetch billing remotely or decide launch eligibility locally.
// GetProviderFinancialOverview разрешает субъекта перед чтением финансового обзора провайдеров из репозитория без удалённого запроса биллинга или локального решения о допустимости запуска.
func (service *Service) GetProviderFinancialOverview(ctx context.Context, p value.Principal) (entity.ProviderFinancialOverview, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.ProviderFinancialOverview{}, err
	}
	return service.repository.GetProviderFinancialOverview(ctx, p)
}
