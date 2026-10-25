// Package workload материализует immutable RuntimeRevision в execution-scoped Kubernetes Pod.
package workload

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	// managedLabel marks resources included in controller-managed runtime selection; the label alone is not proof of owner authority.
	// managedLabel отмечает ресурсы, включаемые в выборку управляемого runtime; сама метка не доказывает полномочия владельца.
	managedLabel = "runtime.kodex.dev/managed"
	// modeLabel separates turn, warm and warm-turn resource selection and readback within the runtime namespace.
	// modeLabel разделяет выборку и сверку ресурсов turn, warm и warm-turn внутри пространства runtime.
	modeLabel = "runtime.kodex.dev/mode"
	// revisionAnnotation records the immutable RuntimeRevision digest compared during resource reuse and Pod observation.
	// revisionAnnotation хранит хеш неизменяемой RuntimeRevision для сравнения при повторном использовании ресурсов и наблюдении Pod.
	revisionAnnotation = "runtime.kodex.dev/revision-digest"
	// configAnnotation pins the runtime configuration digest in projection and ticket metadata rather than carrying its mutable configuration source.
	// configAnnotation закрепляет хеш runtime-конфигурации в метаданных проекции и билета, не перенося изменяемый источник конфигурации.
	configAnnotation = "runtime.kodex.dev/config-digest"
	// environmentAnnotation binds projection and ticket metadata to the selected runtime environment digest.
	// environmentAnnotation связывает метаданные проекции и билета с хешем выбранного окружения runtime.
	environmentAnnotation = "runtime.kodex.dev/environment-digest"
	// warmCompatibilityAnnotation stores the compatibility digest used to decide whether the existing warm Pod can serve the selected input.
	// warmCompatibilityAnnotation хранит хеш совместимости для решения о пригодности существующего warm Pod выбранному входу.
	warmCompatibilityAnnotation = "runtime.kodex.dev/warm-compatibility-digest"
	// controllerAnnotation records the controller workload identity used by resource readback and stale-resource classification, not a bearer credential.
	// controllerAnnotation записывает идентификатор workload контроллера для сверки ресурсов и определения устаревших объектов, а не bearer-учётные данные.
	controllerAnnotation = "runtime.kodex.dev/controller-pod-uid"
	// podAnnotation associates tickets and projections with their runtime Pod name for callback lookup and cleanup.
	// podAnnotation связывает билеты и проекции с именем runtime Pod для поиска callback и очистки.
	podAnnotation = "runtime.kodex.dev/pod-name"
	// leaseAnnotation records the execution lease reference in turn metadata; lease validity is still checked by the authoritative execution path.
	// leaseAnnotation записывает ссылку аренды исполнения в метаданных хода; действительность аренды по-прежнему проверяет авторитетный путь исполнения.
	leaseAnnotation = "runtime.kodex.dev/lease-ref"
	// resourcesAnnotation pins the normalized resource-policy digest in execution metadata and immutable readback.
	// resourcesAnnotation закрепляет хеш нормализованной политики ресурсов в метаданных исполнения и проверке неизменяемости.
	resourcesAnnotation = "runtime.kodex.dev/resources-digest"
	// volumesAnnotation pins the normalized volume-policy digest alongside the execution revision.
	// volumesAnnotation закрепляет хеш нормализованной политики томов рядом с ревизией исполнения.
	volumesAnnotation = "runtime.kodex.dev/volumes-digest"
	// networkAnnotation records the normalized network-policy digest used to bind execution resources to their selected egress policy.
	// networkAnnotation записывает хеш нормализованной сетевой политики для привязки ресурсов исполнения к выбранному правилу исходящих соединений.
	networkAnnotation = "runtime.kodex.dev/network-digest"
	// rbacProfileAnnotation binds metadata to the requested Kubernetes access-profile digest, separately from the effective access digest.
	// rbacProfileAnnotation связывает метаданные с хешем запрошенного профиля доступа Kubernetes отдельно от хеша фактического доступа.
	rbacProfileAnnotation = "runtime.kodex.dev/rbac-profile-digest"
	// effectiveRBACAnnotation records the resolved Kubernetes-access digest checked when execution resources are reused.
	// effectiveRBACAnnotation записывает хеш разрешённого фактического доступа Kubernetes для сверки повторно используемых ресурсов исполнения.
	effectiveRBACAnnotation = "runtime.kodex.dev/effective-rbac-digest"
	// workspacePolicyAnnotation binds runtime projections and tickets to the exact workspace path/limit policy digest.
	// workspacePolicyAnnotation связывает проекции и билеты runtime с точным хешем политики путей и лимитов workspace.
	workspacePolicyAnnotation = "runtime.kodex.dev/workspace-policy-digest"
	// executionBindingAnnotation pins the execution callback binding digest in runtime projection metadata.
	// executionBindingAnnotation закрепляет хеш привязки callback исполнения в метаданных проекции runtime.
	executionBindingAnnotation = "runtime.kodex.dev/execution-binding-digest"
	// mcpBindingAnnotation pins the managed-MCP binding digest independently of the execution callback digest.
	// mcpBindingAnnotation закрепляет хеш привязки управляемого MCP отдельно от хеша callback исполнения.
	mcpBindingAnnotation = "runtime.kodex.dev/mcp-binding-digest"
	// organizationHashAnnotation stores a shortened organization hash for scope comparisons without putting the raw reference in this metadata field.
	// organizationHashAnnotation хранит сокращённый хеш организации для сравнения области без исходной ссылки в этом поле метаданных.
	organizationHashAnnotation = "runtime.kodex.dev/organization-hash"
	// projectHashAnnotation stores the shortened project binding used by projection readback and assistant-session PVC promotion.
	// projectHashAnnotation хранит сокращённую привязку проекта для сверки проекции и перевода PVC сессии помощника.
	projectHashAnnotation = "runtime.kodex.dev/project-hash"
	// sessionHashAnnotation carries the shortened session identity used to match projection and retained volume metadata.
	// sessionHashAnnotation содержит сокращённый идентификатор сессии для сопоставления метаданных проекции и сохраняемого тома.
	sessionHashAnnotation = "runtime.kodex.dev/session-hash"
	// turnHashAnnotation records the shortened turn identity in immutable runtime projection metadata.
	// turnHashAnnotation записывает сокращённый идентификатор хода в метаданных неизменяемой проекции runtime.
	turnHashAnnotation = "runtime.kodex.dev/turn-hash"
	// attemptAnnotation stores the decimal execution attempt in projection and ticket metadata for exact readback.
	// attemptAnnotation хранит десятичный номер попытки исполнения в метаданных проекции и билета для точной сверки.
	attemptAnnotation = "runtime.kodex.dev/attempt"
	// providerSecretNameAnnotation pins the Kubernetes provider-authentication Secret name in a ticket without copying it into the role-readable runtime input.
	// providerSecretNameAnnotation закрепляет имя Kubernetes Secret авторизации провайдера в билете, не копируя его во вход runtime, читаемый ролью.
	providerSecretNameAnnotation = "runtime.kodex.dev/provider-secret-name"
	// providerSecretUIDAnnotation pins the provider Secret UID so a replacement with the same name cannot satisfy credential readback.
	// providerSecretUIDAnnotation закрепляет UID Secret провайдера, чтобы замена с тем же именем не прошла сверку учётных данных.
	providerSecretUIDAnnotation = "runtime.kodex.dev/provider-secret-uid"
	// providerSecretResourceVersionAnnotation records the exact provider Secret resource version required by the credential reader.
	// providerSecretResourceVersionAnnotation записывает точную resourceVersion Secret провайдера, требуемую читателем учётных данных.
	providerSecretResourceVersionAnnotation = "runtime.kodex.dev/provider-secret-resource-version"
	// providerCredentialRefAnnotation links the ticket to the selected provider credential revision reference.
	// providerCredentialRefAnnotation связывает билет со ссылкой выбранной ревизии учётных данных провайдера.
	providerCredentialRefAnnotation = "runtime.kodex.dev/provider-credential-ref"
	// providerCredentialDigestAnnotation stores the expected provider-authentication content digest on tickets and refreshed Secret metadata.
	// providerCredentialDigestAnnotation хранит ожидаемый хеш содержимого авторизации провайдера в билетах и метаданных обновлённого Secret.
	providerCredentialDigestAnnotation = "runtime.kodex.dev/provider-credential-digest"
	// credentialProjectionNameAnnotation identifies the broker-owned credential projection mounted by a turn Pod.
	// credentialProjectionNameAnnotation указывает принадлежащую брокеру проекцию учётных данных, подключаемую Pod хода.
	credentialProjectionNameAnnotation = "runtime.kodex.dev/credential-projection-name"
	// credentialProjectionUIDAnnotation pins the broker-owned projected Secret UID in the turn Pod metadata.
	// credentialProjectionUIDAnnotation закрепляет UID проецируемого Secret брокера в метаданных Pod хода.
	credentialProjectionUIDAnnotation = "runtime.kodex.dev/credential-projection-uid"
	// credentialProjectionVersionAnnotation pins the broker projection resource version, distinct from the original provider Secret version.
	// credentialProjectionVersionAnnotation закрепляет resourceVersion проекции брокера отдельно от версии исходного Secret провайдера.
	credentialProjectionVersionAnnotation = "runtime.kodex.dev/credential-projection-resource-version"
	// credentialProjectionDigestAnnotation records the exact projected credential content digest on the turn Pod.
	// credentialProjectionDigestAnnotation записывает точный хеш проецируемых учётных данных в метаданных Pod хода.
	credentialProjectionDigestAnnotation = "runtime.kodex.dev/credential-projection-digest"
	// providerAccountRefAnnotation binds ticket or refreshed-Secret metadata to the selected provider account reference, not its private credentials.
	// providerAccountRefAnnotation связывает билет либо метаданные обновлённого Secret со ссылкой выбранного аккаунта провайдера, а не его секретными реквизитами.
	providerAccountRefAnnotation = "runtime.kodex.dev/provider-account-ref"
	// previousCredentialRefAnnotation records the predecessor credential revision for exact refresh lineage readback.
	// previousCredentialRefAnnotation записывает предшествующую ревизию учётных данных для точной сверки происхождения обновления.
	previousCredentialRefAnnotation = "runtime.kodex.dev/previous-provider-credential-ref"
	// previousCredentialDigestAnnotation records the predecessor authentication digest independently of the newly materialized credential digest.
	// previousCredentialDigestAnnotation записывает хеш предшествующей авторизации отдельно от хеша вновь материализованных учётных данных.
	previousCredentialDigestAnnotation = "runtime.kodex.dev/previous-provider-credential-digest"
	// executionHashLabel selects per-execution Pod, service-account and policy resources by the shortened lease hash; it is not a lease grant.
	// executionHashLabel выбирает Pod, service-account и политики исполнения по сокращённому хешу аренды; разрешением аренды не является.
	executionHashLabel = "runtime.kodex.dev/execution-hash"
	// inputKey names the validated runtime-input document stored in immutable ticket/projection data and mounted for the runner.
	// inputKey задаёт имя проверенного документа runtime-входа в данных неизменяемого билета и проекции, подключаемого runner.
	inputKey = "runtime.json"
	// ticketKey names the private callback token entry in an execution ticket, mounted separately from the role-readable projection.
	// ticketKey задаёт имя закрытой записи токена callback в билете исполнения, подключаемой отдельно от читаемой ролью проекции.
	ticketKey = "token"
	// workspacePolicyKey names the serialized workspace access/limit policy in the read-only runtime projection.
	// workspacePolicyKey задаёт имя сериализованной политики доступа и лимитов workspace в проекции runtime только для чтения.
	workspacePolicyKey = "workspace-policy.json"
	// inputManifestKey names the combined canonical attachment catalog, not the attachment bytes themselves.
	// inputManifestKey задаёт имя общего канонического каталога вложений, а не самих байтов вложений.
	inputManifestKey = "inputs.json"
	// resultManifestKey names the identity-bound outbox root and writable-limit projection for result publication.
	// resultManifestKey задаёт имя связанной с идентичностью проекции корня outbox и лимитов записи для публикации результатов.
	resultManifestKey = "results.json"
	// skillManifestKey names the context-digest-bound skill metadata projection for the selected runtime revision.
	// skillManifestKey задаёт имя проекции метаданных навыков выбранной ревизии runtime с привязкой к хешу контекста.
	skillManifestKey = "skills.json"
	// memoryManifestKey names the context-digest-bound memory projection, keeping its revision identity alongside the selected memories.
	// memoryManifestKey задаёт имя проекции памяти с привязкой к хешу контекста и идентичностью ревизии рядом с выбранными записями.
	memoryManifestKey = "memories.json"
	// mcpManifestKey names the identity/binding/capability/grant projection for managed MCP rather than issuing new integration grants.
	// mcpManifestKey задаёт имя проекции идентичности, привязки, возможностей и грантов управляемого MCP, не выдавая новых интеграционных грантов.
	mcpManifestKey = "mcp.json"
	// callbackManifestKey names the execution-binding projection containing callback URL and TLS server name, without the callback token.
	// callbackManifestKey задаёт имя проекции привязки исполнения с URL callback и именем TLS-сервера без токена callback.
	callbackManifestKey = "callback.json"
	// providerDigestKey names the provider-authentication digest file mounted beside the separately projected private authentication file.
	// providerDigestKey задаёт имя файла хеша авторизации провайдера, подключаемого рядом с отдельно проецируемым закрытым файлом авторизации.
	providerDigestKey = "provider-auth.sha256"
	// maximumKubernetesProjectionBytes limits the sum of data key/value bytes before a ConfigMap or ticket is created; it is not a full serialized Kubernetes-object size measurement.
	// maximumKubernetesProjectionBytes ограничивает сумму байтов ключей и значений данных до создания ConfigMap или билета; полным измерением сериализованного Kubernetes-объекта не является.
	maximumKubernetesProjectionBytes = 900 << 10
	// assistantSessionDrainPollInterval bounds polling frequency while waiting for an exact terminating Pod UID to disappear before PVC promotion.
	// assistantSessionDrainPollInterval задаёт частоту проверки исчезновения точного UID завершаемого Pod перед переводом PVC.
	assistantSessionDrainPollInterval = 100 * time.Millisecond
	// assistantSessionDrainTimeout limits the wait for a terminating session consumer; expiration rejects PVC promotion rather than assuming the Pod stopped.
	// assistantSessionDrainTimeout ограничивает ожидание завершаемого потребителя сессии; истечение срока запрещает перевод PVC вместо предположения об остановке Pod.
	assistantSessionDrainTimeout = 5 * time.Second
)

// ErrProviderCredentialRefreshRejected distinguishes rejected credential lineage/readback from ordinary Kubernetes availability errors; callers must not treat it as permission to retry or switch revisions.
// ErrProviderCredentialRefreshRejected отделяет отклонённое происхождение либо сверку учётных данных от обычных ошибок доступности Kubernetes; права повторить операцию или переключить ревизию не даёт.
var ErrProviderCredentialRefreshRejected = errors.New("provider credential refresh is rejected")

// Config supplies controller/runtime namespaces and identity, image/ABI pins, storage, callback and egress settings; private signing material stays controller-side rather than entering the role input.
// Config задаёт пространства и идентичность контроллера и runtime, привязки образа и ABI, хранилище, callback и исходящие соединения; закрытый материал подписи остаётся у контроллера, не во входе роли.
type Config struct {
	RPCProfile                                                                         string
	Environment, ControlNamespace, RuntimeNamespace, ControllerPodUID, ControllerPodIP string
	CallbackTLSServerName, CallbackClientCASecret, CallbackClientTLSSecret             string
	StorageClass, SessionPVCSize, RunnerServiceAccount                                 string
	ProviderHTTPSProxy                                                                 string
	RuntimeEgressSigningKey                                                            []byte
	RuntimeEgressCASecret                                                              string
	ProviderAppArmorProfile                                                            string
	PromotedRoleImageRepository, DefaultRoleImageReference                             string
	RoleRuntimeContractSHA256                                                          string
	RoleRuntimeContractRevision                                                        uint64
}

// ProviderSecretBinding pins a provider Secret's name, UID, resource version and content digest inside the trusted controller; it is not serialized into role-readable runtime.json.
// ProviderSecretBinding закрепляет имя, UID, resourceVersion и хеш содержимого Secret провайдера внутри доверенного контроллера; во входной runtime.json, читаемый ролью, не сериализуется.
type ProviderSecretBinding struct {
	Name, UID, ResourceVersion, ContentSHA256 string
}

// CredentialProjection contains exact broker-owned Secret metadata and key mappings without its values; the turn path validates that binding before constructing mounts.
// CredentialProjection содержит точные метаданные Secret брокера и соответствия ключей без значений; путь хода проверяет эту привязку до создания подключений.
type CredentialProjection struct {
	Namespace, SecretName, SecretUID, SecretResourceVersion, ContentSHA256 string
	ProviderAuthKey                                                        string
	RuntimeSecretKeys                                                      map[string]string
}

// managedChatGPTAuthentication holds the managed OAuth fields needed to compare account lineage during refresh; decoding this view is not full provider-schema validation.
// managedChatGPTAuthentication хранит поля управляемого OAuth для сравнения аккаунта при обновлении; разбор этого представления не является полной проверкой схемы провайдера.
type managedChatGPTAuthentication struct {
	AuthMode string `json:"auth_mode"`
	Tokens   struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
}

// Manager owns the Kubernetes client, validated process configuration and parsed session-volume request used to materialize and reconcile runtime resources; business execution state remains in control-plane.
// Manager владеет Kubernetes-клиентом, проверенной конфигурацией процесса и разобранным размером тома сессии для материализации и сверки runtime-ресурсов; бизнесовое состояние исполнения остаётся в control-plane.
type Manager struct {
	client     kubernetes.Interface
	config     Config
	pvcRequest resource.Quantity
}

// InCluster loads Kubernetes in-cluster credentials, limits client requests to five seconds and delegates manager configuration validation to New without creating runtime resources.
// InCluster загружает внутрикластерные реквизиты Kubernetes, ограничивает запросы клиента пятью секундами и передаёт проверку конфигурации в New, не создавая runtime-ресурсы.
func InCluster(config Config) (*Manager, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load Kubernetes runtime configuration")
	}
	restConfig.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.New("create Kubernetes runtime client")
	}
	return New(client, config)
}

// New checks the supplied client, namespace separation, controller IP/identity, storage, callback profile, egress and image/ABI configuration, then constructs the manager without network I/O.
// New проверяет клиент, разделение пространств, IP и идентичность контроллера, хранилище, профиль callback, выходную сеть и образ с ABI, затем создаёт менеджер без сетевого ввода-вывода.
func New(client kubernetes.Interface, config Config) (*Manager, error) {
	if config.RPCProfile != "" && config.RPCProfile != runtimecontract.CallbackProfileTrustedCluster {
		return nil, errors.New("runtime manager RPC profile is invalid")
	}
	pvcRequest, err := resource.ParseQuantity(config.SessionPVCSize)
	if client == nil || err != nil || pvcRequest.Sign() <= 0 || config.ControlNamespace == "" ||
		config.RuntimeNamespace == "" || config.ControlNamespace == config.RuntimeNamespace ||
		config.ControllerPodUID == "" || net.ParseIP(config.ControllerPodIP) == nil ||
		(config.RPCProfile == "" && (config.CallbackTLSServerName == "" || config.CallbackClientCASecret == "" || config.CallbackClientTLSSecret == "")) ||
		config.ProviderHTTPSProxy == "" ||
		len(config.RuntimeEgressSigningKey) < 32 ||
		!validDNSLabel(config.RuntimeEgressCASecret) ||
		(config.ProviderAppArmorProfile != "" && config.ProviderAppArmorProfile != "kodex-provider-runtime") ||
		(config.StorageClass != "" && !validDNSSubdomain(config.StorageClass)) ||
		config.RunnerServiceAccount == "" ||
		config.PromotedRoleImageRepository == "" || config.RoleRuntimeContractRevision == 0 ||
		!validPinnedImageReference(config.DefaultRoleImageReference) ||
		len(config.RoleRuntimeContractSHA256) != sha256.Size*2 {
		return nil, errors.New("Kubernetes runtime manager configuration is invalid")
	}
	return &Manager{client: client, config: config, pvcRequest: pvcRequest}, nil
}

// Check performs a bounded one-item Pod list in the runtime namespace and wraps an observation failure; it does not prove workload readiness, callback delivery or all Kubernetes permissions.
// Check запрашивает не более одного Pod в пространстве runtime и оборачивает ошибку наблюдения; готовность workload, доставку callback и все права Kubernetes не доказывает.
func (manager *Manager) Check(ctx context.Context) error {
	if _, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("Kubernetes runtime namespace observation failed: %w", err)
	}
	return nil
}

// AllowsLastKnownGoodObservation admits only listed Kubernetes timeout/availability/server/rate-limit classes, deadline expiry or temporary network errors to the caller's LKG policy; nil and unclassified errors are rejected.
// AllowsLastKnownGoodObservation допускает в LKG-политику вызывающего кода только перечисленные классы ошибок Kubernetes, истечение срока и временные сетевые ошибки; nil и неклассифицированные ошибки отклоняются.
func AllowsLastKnownGoodObservation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTooManyRequests(err) || apierrors.IsInternalError(err) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

// RunAsLeader coordinates the fixed Kubernetes lease and passes cancellation causes to the worker, retaining election renewal for a bounded shutdown drain; lease election itself is not execution fencing or an owner-state commit.
// RunAsLeader координирует закреплённую Kubernetes-аренду и передаёт причины отмены работнику, сохраняя продление на ограниченное завершение; выбор лидера сам по себе не является fencing исполнения или фиксацией состояния владельца.
func (manager *Manager) RunAsLeader(ctx context.Context, run func(context.Context) error) error {
	if run == nil {
		return errors.New("runtime leader callback is required")
	}
	electionContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	watcherDone := make(chan struct{})
	finished := make(chan struct{})
	started := make(chan struct{})
	defer func() { close(finished); <-watcherDone }()
	go func() {
		defer close(watcherDone)
		select {
		case <-finished:
			return
		case <-ctx.Done():
		}
		select {
		case <-started:
		default:
			cancel()
			return
		}
		timer := time.NewTimer(ControllerShutdownDrain)
		defer timer.Stop()
		select {
		case <-finished:
		case <-timer.C:
			cancel()
		}
	}()
	result := make(chan error, 1)
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{LeaseMeta: metav1.ObjectMeta{Name: "runtime-controller-leader", Namespace: manager.config.ControlNamespace},
			Client: manager.client.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: manager.config.ControllerPodUID}},
		LeaseDuration: 15 * time.Second, RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second, ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{OnStartedLeading: func(leaderContext context.Context) {
			close(started)
			if ctx.Err() != nil {
				result <- ctx.Err()
				cancel()
				return
			}
			result <- runWithLeadership(ctx, leaderContext, run)
			cancel()
		}, OnStoppedLeading: func() {}},
	})
	if err != nil {
		return errors.New("configure runtime leader election")
	}
	elector.Run(electionContext)
	select {
	case <-started:
		return <-result
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("runtime leadership lost")
	}
}

// ControllerShutdownDrain budgets 210 seconds for stop, callback, fallback and cleanup before election cancellation; a time budget is not proof that those stages completed.
// ControllerShutdownDrain выделяет 210 секунд на stop, callback, запасной путь и очистку до отмены выбора лидера; бюджет времени не доказывает завершение этих этапов.
const ControllerShutdownDrain = 210 * time.Second

// ErrLeadershipLost marks cancellation caused by losing the Kubernetes leadership context, distinguishing it from ordinary lifecycle cancellation.
// ErrLeadershipLost отмечает отмену из-за потери контекста лидерства Kubernetes, отличая её от обычной отмены жизненного цикла.
var ErrLeadershipLost = errors.New("runtime leadership lost")

// leadershipContextKey privately identifies the underlying leadership context carried through the independently canceled worker context.
// leadershipContextKey закрыто обозначает исходный контекст лидерства, передаваемый через независимо отменяемый контекст работника.
type leadershipContextKey struct{}

// LeadershipDone exposes the attached leadership cancellation channel when present and otherwise returns nil; it does not create or renew a lease.
// LeadershipDone возвращает канал отмены прикреплённого лидерства либо nil при его отсутствии; аренду не создаёт и не продлевает.
func LeadershipDone(ctx context.Context) <-chan struct{} {
	if leadership, ok := ctx.Value(leadershipContextKey{}).(context.Context); ok {
		return leadership.Done()
	}
	return nil
}

// runWithLeadership removes inherited cancellation while retaining leadership values, then explicitly links lifecycle and leadership completion with distinct cancel causes for the synchronous worker.
// runWithLeadership убирает наследуемую отмену, сохраняя значения лидерства, затем явно связывает завершение жизненного цикла и лидерства с разными причинами отмены синхронного работника.
func runWithLeadership(lifecycle, leadership context.Context, run func(context.Context) error) error {
	ctx, cancel := context.WithCancelCause(context.WithValue(context.WithoutCancel(leadership), leadershipContextKey{}, leadership))
	defer cancel(context.Canceled)
	stopLifecycle := context.AfterFunc(lifecycle, func() { cancel(context.Canceled) })
	defer stopLifecycle()
	stopLeadership := context.AfterFunc(leadership, func() { cancel(ErrLeadershipLost) })
	defer stopLeadership()
	return run(ctx)
}

// CleanupStaleTurns paginates managed turn Pods and dependent policy/ticket/projection resources, retains hashes of Pods marked with the current controller and joins cleanup failures; successful deletes are not rolled back and session PVCs are untouched.
// CleanupStaleTurns постранично обходит управляемые Pod ходов и связанные политики, билеты и проекции, сохраняет хеши Pod с меткой текущего контроллера и объединяет ошибки очистки; успешные удаления не откатываются, PVC сессий не затрагиваются.
func (manager *Manager) CleanupStaleTurns(ctx context.Context) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "turn"}.AsSelector().String()
	var result error
	activeHashes := make(map[string]struct{})
	continueToken := ""
	for {
		pods, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return errors.New("list retained runtime turn pods")
		}
		for index := range pods.Items {
			pod := &pods.Items[index]
			if pod.Annotations[controllerAnnotation] == manager.config.ControllerPodUID {
				if executionHash := pod.Labels[executionHashLabel]; len(executionHash) == 16 {
					activeHashes[executionHash] = struct{}{}
				}
				continue
			}
			if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime turn pod"))
			}
			if err := manager.deleteExecutionPolicyByHash(ctx, pod.Labels[executionHashLabel]); err != nil {
				result = errors.Join(result, err)
			}
		}
		if pods.Continue == "" {
			break
		}
		continueToken = pods.Continue
	}
	orphaned, orphanErr := manager.orphanedExecutionPolicyHashes(ctx, selector, activeHashes)
	if orphanErr != nil {
		result = errors.Join(result, orphanErr)
	}
	for executionHash := range orphaned {
		if err := manager.deleteExecutionPolicyByHash(ctx, executionHash); err != nil {
			result = errors.Join(result, err)
		}
	}
	continueToken = ""
	for {
		tickets, ticketErr := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if ticketErr != nil {
			result = errors.Join(result, errors.New("list retained runtime turn tickets"))
			break
		}
		for index := range tickets.Items {
			ticket := &tickets.Items[index]
			executionHash := strings.TrimPrefix(ticket.Annotations[podAnnotation], "runtime-turn-")
			_, active := activeHashes[executionHash]
			if ticket.Annotations[controllerAnnotation] == manager.config.ControllerPodUID && active {
				continue
			}
			if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, ticket.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime turn ticket"))
			}
		}
		if tickets.Continue == "" {
			break
		}
		continueToken = tickets.Continue
	}
	continueToken = ""
	for {
		projections, projectionErr := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256, Continue: continueToken})
		if projectionErr != nil {
			result = errors.Join(result, errors.New("list retained runtime VFS projections"))
			break
		}
		for index := range projections.Items {
			projection := &projections.Items[index]
			executionHash := strings.TrimPrefix(projection.Annotations[podAnnotation], "runtime-turn-")
			_, active := activeHashes[executionHash]
			if projection.Annotations[controllerAnnotation] == manager.config.ControllerPodUID && active {
				continue
			}
			if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projection.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime VFS projection"))
			}
		}
		if projections.Continue == "" {
			break
		}
		continueToken = projections.Continue
	}
	return result
}

// orphanedExecutionPolicyHashes independently paginates service accounts, Roles, RoleBindings and NetworkPolicies, collecting sixteen-character hashes whose controller or active-Pod binding is stale; it performs no deletions.
// orphanedExecutionPolicyHashes отдельно обходит страницы service account, Role, RoleBinding и NetworkPolicy, собирая хеши из шестнадцати символов с устаревшим контроллером либо связью с активным Pod; удаления не выполняет.
func (manager *Manager) orphanedExecutionPolicyHashes(ctx context.Context, selector string, active map[string]struct{}) (map[string]struct{}, error) {
	orphaned := make(map[string]struct{})
	inspect := func(metadata metav1.ObjectMeta) {
		executionHash := metadata.Labels[executionHashLabel]
		_, isActive := active[executionHash]
		if len(executionHash) == 16 && (metadata.Annotations[controllerAnnotation] != manager.config.ControllerPodUID || !isActive) {
			orphaned[executionHash] = struct{}{}
		}
	}
	continueToken := ""
	for {
		accounts, err := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution ServiceAccounts")
		}
		for index := range accounts.Items {
			inspect(accounts.Items[index].ObjectMeta)
		}
		if accounts.Continue == "" {
			break
		}
		continueToken = accounts.Continue
	}
	continueToken = ""
	for {
		roles, err := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution Roles")
		}
		for index := range roles.Items {
			inspect(roles.Items[index].ObjectMeta)
		}
		if roles.Continue == "" {
			break
		}
		continueToken = roles.Continue
	}
	continueToken = ""
	for {
		bindings, err := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution RoleBindings")
		}
		for index := range bindings.Items {
			inspect(bindings.Items[index].ObjectMeta)
		}
		if bindings.Continue == "" {
			break
		}
		continueToken = bindings.Continue
	}
	continueToken = ""
	for {
		policies, err := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution NetworkPolicies")
		}
		for index := range policies.Items {
			inspect(policies.Items[index].ObjectMeta)
		}
		if policies.Continue == "" {
			break
		}
		continueToken = policies.Continue
	}
	return orphaned, nil
}

// TurnInputBuildError carries a closed build stage and local cause while exposing a fixed public error string; the cause must not be copied into runtime diagnostics.
// TurnInputBuildError хранит закрытый этап сборки и локальную причину, выдавая фиксированную внешнюю строку ошибки; причину нельзя переносить в runtime-диагностику.
type TurnInputBuildError struct {
	Stage string
	Cause error
}

// Error returns the fixed turn-input failure text without serializing Stage or the wrapped cause.
// Error возвращает фиксированный текст ошибки входа хода без сериализации Stage и вложенной причины.
func (failure *TurnInputBuildError) Error() string { return "runtime turn input build failed" }

// Unwrap retains the underlying cause for local error inspection; callers still own safe diagnostic projection.
// Unwrap сохраняет вложенную причину для локальной проверки ошибки; безопасная диагностическая проекция остаётся обязанностью вызывающего кода.
func (failure *TurnInputBuildError) Unwrap() error { return failure.Cause }

// ValidationReason projects only allowlisted validation/envelope reasons for the validation stage; unknown causes remain UNKNOWN and never authorize retry or change terminal handling.
// ValidationReason выдаёт только разрешённые причины проверки и envelope для этапа validation; неизвестные причины остаются UNKNOWN и не разрешают повтор или изменение terminal-обработки.
func (failure *TurnInputBuildError) ValidationReason() string {
	if failure == nil || failure.Stage != "validation" {
		return "UNKNOWN"
	}
	var validation *runnerInputValidationFailure
	if errors.As(failure.Cause, &validation) {
		switch validation.reason {
		case "REQUIRED_CONTEXT", "ATTACHMENT_LINEAGE", "ATTACHMENT_MANIFEST", "WORKSPACE_MANIFEST":
			return validation.reason
		case "RUNNER_INPUT":
			if runnerInputValidationReason(validation.cause) == "ENVELOPE" && knownEnvelopeReason(validation.envelopeReason) {
				return validation.envelopeReason
			}
			return runnerInputValidationReason(validation.cause)
		}
	}
	return "UNKNOWN"
}

// runnerInputValidationFailure preserves the local validator cause, closed category and optional envelope detail separately from the safe outer build error.
// runnerInputValidationFailure сохраняет локальную причину проверки, закрытую категорию и необязательную деталь envelope отдельно от безопасной внешней ошибки сборки.
type runnerInputValidationFailure struct {
	reason         string
	cause          error
	envelopeReason string
}

// Error delegates to the local validation cause and is not itself a sanitized public diagnostic boundary.
// Error возвращает текст локальной причины проверки и сам по себе не является безопасной внешней диагностической границей.
func (failure *runnerInputValidationFailure) Error() string { return failure.cause.Error() }

// Unwrap exposes the original validator error for typed local inspection without changing its outcome.
// Unwrap открывает исходную ошибку валидатора для типизированной локальной проверки без изменения результата.
func (failure *runnerInputValidationFailure) Unwrap() error { return failure.cause }

// validationFailure attaches a diagnostic category to a validator cause without changing the cause or granting execution permission.
// validationFailure присоединяет диагностическую категорию к причине проверки, не меняя причину и не выдавая право исполнения.
func validationFailure(reason string, cause error) error {
	return &runnerInputValidationFailure{reason: reason, cause: cause}
}

// runnerInputValidationReason uses one shared sentinel and an exact allowlist of current fixed error texts only for diagnostics; wrapped or arbitrary text becomes UNKNOWN rather than a new permission.
// runnerInputValidationReason использует один общий sentinel и точный список текущих фиксированных ошибок только для диагностики; обёрнутый либо произвольный текст становится UNKNOWN, а не новым разрешением.
func runnerInputValidationReason(err error) string {
	if errors.Is(err, runtimecontract.ErrRuntimeContext) {
		return "CONTEXT_SNAPSHOT"
	}
	if err == nil {
		return "UNKNOWN"
	}
	switch err.Error() {
	case "runtime prompt service materialization is invalid":
		return "PROMPT_SERVICE"
	case "runner assistant scope binding is invalid":
		return "ASSISTANT_SCOPE"
	case "runner input is invalid":
		return "ENVELOPE"
	case "managed MCP profile binding is invalid":
		return "MANAGED_MCP_PROFILE"
	case "required managed MCP health proof is unavailable":
		return "MANAGED_MCP_HEALTH"
	case "runtime execution deadline is invalid", "runtime execution clock is invalid", "runtime effective execution deadline is invalid", "workflow execution deadline scope is invalid":
		return "EXECUTION_DEADLINE"
	case "runner STT configuration binding is invalid":
		return "STT_BINDING"
	case "runner effective reasoning effort is invalid":
		return "REASONING_EFFORT"
	case "runner config overlay binding is invalid":
		return "CONFIG_OVERLAY"
	case "runner environment policy binding is invalid":
		return "ENVIRONMENT_POLICY"
	case "runner workspace policy binding is invalid":
		return "WORKSPACE_POLICY"
	case "runner file catalog binding is invalid":
		return "FILE_CATALOG"
	case "runner environment binding is invalid":
		return "ENVIRONMENT_BINDING"
	case "runner integration grant is outside effective capabilities":
		return "INTEGRATION_CAPABILITIES"
	case "runner project binding is invalid":
		return "PROJECT_BINDING"
	case "runner turn binding is invalid":
		return "TURN_BINDING"
	case "runner bounded input digest is invalid":
		return "BOUNDED_INPUT"
	case "runner execution binding digest is invalid":
		return "EXECUTION_BINDING"
	case "warm runner binding is invalid":
		return "WARM_BINDING"
	case "runner delegation catalog is invalid":
		return "DELEGATION_CATALOG"
	case "runner assistant context is invalid":
		return "ASSISTANT_CONTEXT"
	case "runner artifact catalog is invalid", "runner artifact provenance is invalid", "runner attachment set catalog is invalid", "runner attachment set binding is invalid", "runner attachment context is invalid":
		return "ATTACHMENT_CATALOG"
	default:
		return "UNKNOWN"
	}
}

// turnInputBuildError wraps a build-stage failure for local inspection while keeping the public turn-input error string fixed.
// turnInputBuildError оборачивает ошибку этапа сборки для локальной проверки, сохраняя фиксированную внешнюю строку ошибки входа хода.
func turnInputBuildError(stage string, cause error) error {
	return &TurnInputBuildError{Stage: stage, Cause: cause}
}

// BuildTurnInput materializes one complete claimed execution, hydrates its context/catalog, verifies provider and revision/execution digests and validates the final input; failures retain their build stage and create no Kubernetes resources.
// BuildTurnInput материализует полное заявленное исполнение, наполняет контекст и каталог, сверяет провайдера и хеши ревизии и исполнения, проверяет итоговый вход; ошибки сохраняют этап сборки, ресурсы Kubernetes не создаются.
func (manager *Manager) BuildTurnInput(execution *controlplanev1.ClaimedExecution) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if execution == nil || execution.GetRun() == nil || execution.GetNode() == nil || execution.GetRevision() == nil ||
		execution.GetRevision().GetRuntime() == nil || execution.GetLease() == nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("claim", errors.New("claimed execution is incomplete"))
	}
	revision := execution.GetRevision()
	input, err := manager.baseInput(revision, runtimecontract.RunnerModeTurn)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("base", err)
	}
	input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef = execution.GetRun().GetRef(), execution.GetNode().GetRef(), revision.GetSessionRef(), revision.GetTurnRef()
	input.ProjectRef = execution.GetRun().GetProjectRef()
	input.AgentRef, input.Attempt = revision.GetAgentRef(), revision.GetAttempt()
	input.InputDigest = revision.GetInputDigest()
	input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.GetLease().GetRef(), execution.GetLease().GetFence(), execution.GetLease().GetGeneration()
	input.Task, input.BoundedInput = execution.GetTask(), map[string]any{}
	if revision.GetBoundedInput() != nil {
		input.BoundedInput = revision.GetBoundedInput().AsMap()
	}
	if context := revision.GetAssistantContext(); context != nil {
		input.AssistantContext = &runtimecontract.RunnerAssistantContext{Route: context.GetRoute(), EntityKind: context.GetEntityKind(),
			EntityRef: context.GetEntityRef(), EntityName: context.GetEntityName(), EntityVersion: context.EntityVersion,
			AllowedOperations: make([]string, 0, len(context.GetAllowedOperations()))}
		for _, operation := range context.GetAllowedOperations() {
			if operation != controlplanev1.AssistantPlanOperation_TYPE_UNSPECIFIED {
				input.AssistantContext.AllowedOperations = append(input.AssistantContext.AllowedOperations, strings.TrimPrefix(operation.String(), "TYPE_"))
			}
		}
	}
	manager.addCatalog(&input, revision)
	if err := hydrateRuntimeContext(&input, revision); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("context", err)
	}
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("credential", err)
	}
	if err := validateRuntimeRevisionDigest(input, binding); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("revision_digest", err)
	}
	input.ExecutionBindingDigest, input.MCPBindingDigest, err = runtimecontract.RuntimeExecutionBindingDigests(input)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("execution_digest", err)
	}
	if err := validateRunnerInput(input); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, turnInputBuildError("validation", err)
	}
	return input, binding, nil
}

// BuildWarmInput accepts only a system-assistant warm revision, builds its context and catalogs and checks provider/revision bindings plus the final input without publishing a Pod.
// BuildWarmInput принимает только warm-ревизию системного помощника, собирает контекст и каталоги, проверяет привязки провайдера и ревизии и итоговый вход без публикации Pod.
func (manager *Manager) BuildWarmInput(revision *controlplanev1.RuntimeRevisionSnapshot) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if revision == nil || revision.GetRuntime() == nil || revision.GetAssistantScope() != controlplanev1.AssistantScope_ASSISTANT_SCOPE_SYSTEM {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, errors.New("warm runtime revision is invalid")
	}
	input, err := manager.baseInput(revision, runtimecontract.RunnerModeWarm)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	input.SessionRef, input.AgentRef = revision.GetSessionRef(), revision.GetAgentRef()
	manager.addCatalog(&input, revision)
	if err := hydrateRuntimeContext(&input, revision); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	if err := validateRuntimeRevisionDigest(input, binding); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	return input, binding, validateRunnerInput(input)
}

// validateRuntimeRevisionDigest recomputes the revision digest with exact provider Secret name/UID/resource-version pins and compares it in constant time, without reading Secret bytes.
// validateRuntimeRevisionDigest пересчитывает хеш ревизии с точными именем, UID и resourceVersion Secret провайдера и сравнивает за постоянное время без чтения байтов Secret.
func validateRuntimeRevisionDigest(input runtimecontract.RunnerInput, binding ProviderSecretBinding) error {
	digest, err := runtimecontract.RuntimeRevisionDigest(input, runtimecontract.RuntimeRevisionCredentialSource{
		SecretName: binding.Name, SecretUID: binding.UID, SecretResourceVersion: binding.ResourceVersion,
	})
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(input.RuntimeRevisionDigest)) != 1 {
		return errors.New("runtime revision digest mismatch")
	}
	return nil
}

// validateRunnerInput checks the required context and complete runtime envelope before turn lineage,
// exact per-set manifest digests and the combined workspace catalog; it returns classified failures
// without changing owner state, fetching artifact bytes or creating a workload.
// validateRunnerInput проверяет обязательный контекст и полный runtime-envelope до связи с ходом,
// точных хешей наборов и общего каталога workspace; возвращает классифицированные отказы
// без изменения состояния владельца, получения байтов артефактов или создания workload.
func validateRunnerInput(input runtimecontract.RunnerInput) error {
	if _, err := input.RequiredContextSnapshot(time.Now()); err != nil {
		return validationFailure("REQUIRED_CONTEXT", err)
	}
	if err := input.Validate(); err != nil {
		failure := &runnerInputValidationFailure{reason: "RUNNER_INPUT", cause: err}
		if runnerInputValidationReason(err) == "ENVELOPE" {
			failure.envelopeReason = runtimeEnvelopeReason(input)
		}
		return failure
	}
	for _, set := range input.AttachmentSets {
		if set.TurnRef == "" || set.Provenance == "CURRENT_TURN" && set.TurnRef != input.TurnRef ||
			set.Provenance == "SESSION_HISTORY" && set.TurnRef == input.TurnRef {
			return validationFailure("ATTACHMENT_LINEAGE", errors.New("runtime attachment turn lineage is invalid"))
		}
		manifest, err := runtimecontract.BuildAttachmentSetManifest(set, input.InputArtifacts)
		if err != nil || manifest.Digest != set.ManifestDigest {
			return validationFailure("ATTACHMENT_MANIFEST", errors.New("runtime attachment manifest digest is invalid"))
		}
	}
	if _, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts); err != nil {
		return validationFailure("WORKSPACE_MANIFEST", errors.New("runtime workspace attachment manifest is invalid"))
	}
	return nil
}

// baseInput maps immutable revision configuration and closed policy values into runner paths, callback transport and image/provider pins; trusted-cluster explicitly selects its separate HTTP profile, and downstream validation remains required.
// baseInput переносит неизменяемую конфигурацию ревизии и закрытые политики в пути runner, транспорт callback и привязки образа и провайдера; trusted-cluster явно выбирает отдельный HTTP-профиль, последующая проверка остаётся обязательной.
func (manager *Manager) baseInput(revision *controlplanev1.RuntimeRevisionSnapshot, mode string) (runtimecontract.RunnerInput, error) {
	var assistantScope runtimecontract.AssistantScope
	switch revision.GetAssistantScope() {
	case controlplanev1.AssistantScope_ASSISTANT_SCOPE_NONE:
		assistantScope = runtimecontract.AssistantScopeNone
	case controlplanev1.AssistantScope_ASSISTANT_SCOPE_SYSTEM:
		assistantScope = runtimecontract.AssistantScopeSystem
	case controlplanev1.AssistantScope_ASSISTANT_SCOPE_PROJECT:
		assistantScope = runtimecontract.AssistantScopeProject
	default:
		return runtimecontract.RunnerInput{}, errors.New("runtime revision assistant scope is invalid")
	}
	environmentPolicy, err := runtimeEnvironmentPolicyFromProto(revision.GetEnvironmentPolicy())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	effectiveAccess, err := runtimeKubernetesAccessFromProto(revision.GetEffectiveKubernetesAccess())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV9, Mode: mode, WorkloadInstance: manager.config.ControllerPodUID,
		OrganizationRef:    revision.GetOrganizationRef(),
		RuntimeRevisionRef: revision.GetRef(), RuntimeRevisionVersion: revision.GetVersion(), RuntimeRevisionDigest: revision.GetRevisionDigest(),
		ImageReference: revision.GetImageReference(), ImageManifestDigest: revision.GetImageManifestDigest(),
		EnvironmentImage: runtimecontract.RuntimeEnvironmentImage{
			ArtifactRef: revision.GetRoleImageArtifactRef(), RecipeRef: revision.GetRoleImageRecipeRef(),
			RecipeGeneration: revision.GetRoleImageRecipeGeneration(), Reference: revision.GetImageReference(), Digest: revision.GetImageManifestDigest(),
		},
		RoleRuntimeContractRevision: revision.GetRoleRuntimeContractRevision(), RoleRuntimeContractSHA256: revision.GetRoleRuntimeContractSha256(),
		RoleDefinitionRef: revision.GetRoleDefinitionRef(), RuntimeProfileRef: revision.GetRuntime().GetRef(), RuntimeProfileRevision: revision.GetRuntime().GetRevision(),
		InstructionRef: revision.GetInstructionRef(), InstructionDigest: revision.GetInstructionDigest(),
		PromptTemplateRef: revision.GetPromptTemplateRef(), PromptTemplateDigest: revision.GetPromptTemplateDigest(), PromptMaterializationDigest: revision.GetPromptMaterializationDigest(),
		PromptServiceTemplateRevision: revision.GetPromptServiceTemplateRevision(), PromptServiceTemplateDigest: revision.GetPromptServiceTemplateDigest(), PromptTargetKind: revision.GetPromptTargetKind(),
		SystemSTTConfigurationRef: revision.GetSystemSttConfigurationRef(), SystemSTTConfigurationRevisionRef: revision.GetSystemSttConfigurationRevisionRef(), SystemSTTConfigurationVersion: revision.GetSystemSttConfigurationVersion(), SystemSTTConfigurationDigest: revision.GetSystemSttConfigurationDigest(),
		AssistantScope: assistantScope, AssistantProfileRef: revision.GetAssistantProfileRef(), Instructions: revision.GetInstructions(), Provider: revision.GetRuntime().GetProvider(), Model: revision.GetRuntime().GetModel(),
		CodexSessionID:             revision.GetCodexSessionId(),
		ProviderAccountRef:         revision.GetProviderCredential().GetAccountRef(),
		ProviderCredentialRef:      revision.GetProviderCredential().GetCredentialRevisionRef(),
		ProviderCredentialRevision: revision.GetProviderCredential().GetCredentialRevision(),
		ProviderCredentialSHA256:   revision.GetProviderCredential().GetContentSha256(),
		RuntimeConfigRef:           revision.GetRuntimeConfigRef(),
		RuntimeConfigVersion:       revision.GetRuntimeConfigVersion(),
		RuntimeConfigDigest:        revision.GetRuntimeConfigDigest(),
		ProviderPolicyRef:          revision.GetProviderPolicyRef(),
		ProviderPolicyVersion:      revision.GetProviderPolicyVersion(),
		ProviderPolicyDigest:       revision.GetProviderPolicyDigest(),
		ConfigOverlayRef:           revision.GetConfigOverlayRef(),
		ConfigOverlayVersion:       revision.GetConfigOverlayVersion(),
		ConfigOverlayDigest:        revision.GetConfigOverlayDigest(),
		ConfigOverlay:              revision.GetConfigOverlay(),
		EffectiveReasoningEffort:   revision.GetEffectiveReasoningEffort(),
		ReasoningMode:              strings.TrimPrefix(revision.GetReasoningMode().String(), "RUNTIME_REASONING_MODE_"),
		RuntimeEnvironmentRef:      revision.GetRuntimeEnvironmentRef(),
		RuntimeEnvironmentVersion:  revision.GetRuntimeEnvironmentVersion(),
		RuntimeEnvironmentDigest:   revision.GetRuntimeEnvironmentDigest(),
		EnvironmentBindingRef:      revision.GetEnvironmentBindingRef(),
		EnvironmentBindingVersion:  revision.GetEnvironmentBindingVersion(),
		EnvironmentBindingDigest:   revision.GetEnvironmentBindingDigest(),
		EnvironmentPolicy:          environmentPolicy,
		EffectiveKubernetesAccess:  effectiveAccess,
		CodexSandbox:               "workspace-write", CodexApprovalPolicy: "never",
		CallbackURL: "https://" + net.JoinHostPort(manager.config.ControllerPodIP, "8444"),
		CallbackTLS: runtimecontract.RuntimeTLSBinding{ServerName: manager.config.CallbackTLSServerName,
			CAFile: "/var/run/config/kodex/runtime/callback/ca.crt", CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt", PrivateKeyFile: "/var/run/secrets/kodex/runtime/callback-client/tls.key"},
		ExecutionTicketFile: "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:    "/run/secrets/kodex/runtime/provider/auth.json", ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot: "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	if manager.config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		input.CallbackURL = "http://" + net.JoinHostPort(manager.config.ControllerPodIP, "8444")
		input.CallbackTLS = runtimecontract.RuntimeTLSBinding{Profile: runtimecontract.CallbackProfileTrustedCluster}
	}
	workspacePolicy, err := runtimeWorkspacePolicyFromProto(revision.GetWorkspacePolicy())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	input.WorkspacePolicy = workspacePolicy
	executionDeadline, err := RuntimeExecutionDeadlineFromProto(revision.GetExecutionDeadline())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	input.ExecutionDeadline = executionDeadline
	for _, item := range revision.GetEnvironmentValues() {
		input.EnvironmentValues = append(input.EnvironmentValues, runtimecontract.RuntimeEnvironmentValue{Name: item.GetName(), Value: item.GetValue()})
	}
	for _, item := range revision.GetSecretProjections() {
		input.SecretProjections = append(input.SecretProjections, runtimecontract.RuntimeSecretProjection{
			Name: item.GetName(), SecretName: item.GetSecretName(), SecretKey: item.GetSecretKey(),
			SecretUID: item.GetSecretUid(), SecretResourceVersion: item.GetSecretResourceVersion(),
			ContentSHA256: item.GetContentSha256(),
		})
	}
	for _, item := range revision.GetEnvironmentTools() {
		input.EnvironmentTools = append(input.EnvironmentTools, runtimecontract.RuntimeEnvironmentTool{
			Name: item.GetName(), Command: item.GetCommand(), Description: item.GetDescription(), UsageHint: item.GetUsageHint(),
		})
	}
	return input, nil
}

// runtimeWorkspacePolicyFromProto requires a policy, converts closed path-access/denial enums and validates its digest and limits through the shared workspace contract.
// runtimeWorkspacePolicyFromProto требует политику, преобразует закрытые enum доступа к путям и причин отказа, проверяет хеш и лимиты общим контрактом workspace.
func runtimeWorkspacePolicyFromProto(value *controlplanev1.RuntimeWorkspacePolicy) (runtimecontract.RuntimeWorkspacePolicy, error) {
	if value == nil {
		return runtimecontract.RuntimeWorkspacePolicy{}, errors.New("runtime workspace policy is missing")
	}
	policy := runtimecontract.RuntimeWorkspacePolicy{Revision: value.GetRevision(), Root: value.GetRoot(), MaximumWritableBytes: value.GetMaximumWritableBytes(), MaximumFileCount: value.GetMaximumFileCount(), Digest: value.GetDigest()}
	for _, rule := range value.GetRules() {
		policy.Rules = append(policy.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.GetPath(), Access: strings.TrimPrefix(rule.GetAccess().String(), "RUNTIME_WORKSPACE_ACCESS_")})
	}
	for _, reason := range value.GetDenialReasons() {
		policy.DenialReasons = append(policy.DenialReasons, strings.TrimPrefix(reason.String(), "RUNTIME_WORKSPACE_DENIAL_REASON_"))
	}
	if err := policy.Validate(); err != nil {
		return runtimecontract.RuntimeWorkspacePolicy{}, err
	}
	return policy, nil
}

// runtimeEnvironmentPolicyFromProto maps resource, volume, egress and Kubernetes profiles, normalizes the resulting policy and requires all four recorded digests to match before returning it.
// runtimeEnvironmentPolicyFromProto преобразует ресурсы, тома, исходящую сеть и профили Kubernetes, нормализует политику и требует совпадения всех четырёх записанных хешей до возврата.
func runtimeEnvironmentPolicyFromProto(value *controlplanev1.RuntimeEnvironmentPolicy) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	if value == nil || value.GetResources() == nil || value.GetNetwork() == nil || value.GetKubernetesAccess() == nil {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("runtime environment policy is incomplete")
	}
	resources := value.GetResources()
	var workspaceLimits *runtimecontract.RuntimeWorkspaceLimits
	if limits := resources.GetWorkspaceLimits(); limits != nil {
		workspaceLimits = &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: limits.GetMaxBytes(), MaxFiles: limits.GetMaxFiles()}
	}
	policy := runtimecontract.RuntimeEnvironmentPolicy{
		Resources: runtimecontract.RuntimeResourcePolicy{
			WorkspaceLimits: workspaceLimits,
			CPURequestMilli: resources.GetCpuRequestMilli(), CPULimitMilli: resources.GetCpuLimitMilli(),
			MemoryRequestMiB: resources.GetMemoryRequestMib(), MemoryLimitMiB: resources.GetMemoryLimitMib(),
			EphemeralStorageRequestMiB: resources.GetEphemeralStorageRequestMib(),
			EphemeralStorageLimitMiB:   resources.GetEphemeralStorageLimitMib(),
		},
		Network: runtimecontract.RuntimeNetworkPolicy{DenyByDefault: value.GetNetwork().GetDenyByDefault(),
			WebAccess: runtimecontract.RuntimeWebAccess{Mode: runtimeWebAccessMode(value.GetNetwork().GetWebAccess().GetMode()), Rules: []runtimecontract.RuntimeWebAccessRule{}}},
		KubernetesAccess: runtimecontract.RuntimeKubernetesAccessProfile{
			Kind:      runtimeKubernetesAccessKind(value.GetKubernetesAccess().GetKind()),
			Namespace: value.GetKubernetesAccess().GetNamespace(),
		},
		ResourcesDigest: value.GetResourcesDigest(), VolumesDigest: value.GetVolumesDigest(),
		NetworkDigest: value.GetNetworkDigest(), RBACDigest: value.GetRbacDigest(),
	}
	for _, volume := range value.GetVolumes() {
		policy.Volumes = append(policy.Volumes, runtimecontract.RuntimeVolume{
			Name: volume.GetName(), Kind: runtimeVolumeKind(volume.GetKind()), SizeMiB: volume.GetSizeMib(), MountPath: volume.GetMountPath(),
		})
	}
	for _, egress := range value.GetNetwork().GetEgress() {
		policy.Network.Egress = append(policy.Network.Egress, runtimecontract.RuntimeNetworkEgress{
			Destination: runtimeNetworkDestination(egress.GetDestination()), Protocol: runtimeNetworkProtocol(egress.GetProtocol()), Port: egress.GetPort(),
		})
	}
	for _, rule := range value.GetNetwork().GetWebAccess().GetRules() {
		policy.Network.WebAccess.Rules = append(policy.Network.WebAccess.Rules, runtimecontract.RuntimeWebAccessRule{
			DomainPattern: rule.GetDomainPattern(), Protocol: rule.GetProtocol(), Port: rule.GetPort(), HTTPMethods: append([]string(nil), rule.GetHttpMethods()...),
		})
	}
	normalized, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil || normalized.ResourcesDigest != policy.ResourcesDigest || normalized.VolumesDigest != policy.VolumesDigest ||
		normalized.NetworkDigest != policy.NetworkDigest || normalized.RBACDigest != policy.RBACDigest {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("runtime environment policy digest mismatch")
	}
	return normalized, nil
}

// runtimeWebAccessMode removes the protobuf enum prefix for the downstream closed-policy validator; trimming alone does not establish eligibility.
// runtimeWebAccessMode удаляет префикс protobuf-enum для последующей проверки закрытой политики; удаление префикса само по себе допустимость не подтверждает.
func runtimeWebAccessMode(value controlplanev1.RuntimeWebAccessMode) string {
	return strings.TrimPrefix(value.String(), "RUNTIME_WEB_ACCESS_MODE_")
}

// runtimeKubernetesAccessFromProto requires an effective profile, copies rule slices and verifies the resulting service-account/rule/digest binding through the shared validator.
// runtimeKubernetesAccessFromProto требует фактический профиль, копирует массивы правил и проверяет привязку service-account, правил и хеша общим валидатором.
func runtimeKubernetesAccessFromProto(value *controlplanev1.RuntimeKubernetesAccess) (runtimecontract.RuntimeKubernetesAccess, error) {
	if value == nil || value.GetProfile() == nil {
		return runtimecontract.RuntimeKubernetesAccess{}, errors.New("effective Kubernetes access is incomplete")
	}
	access := runtimecontract.RuntimeKubernetesAccess{
		Profile: runtimecontract.RuntimeKubernetesAccessProfile{
			Kind: runtimeKubernetesAccessKind(value.GetProfile().GetKind()), Namespace: value.GetProfile().GetNamespace(),
		},
		ServiceAccountName: value.GetServiceAccountName(), Rules: []runtimecontract.RuntimeKubernetesRule{}, Digest: value.GetDigest(),
	}
	for _, rule := range value.GetRules() {
		access.Rules = append(access.Rules, runtimecontract.RuntimeKubernetesRule{
			APIGroup: rule.GetApiGroup(), Resource: rule.GetResource(), Verbs: append([]string(nil), rule.GetVerbs()...),
			ResourceNames: append([]string(nil), rule.GetResourceNames()...),
		})
	}
	if err := runtimecontract.ValidateRuntimeKubernetesAccess(access); err != nil {
		return runtimecontract.RuntimeKubernetesAccess{}, err
	}
	return access, nil
}

// runtimeVolumeKind maps only disk and memory ephemeral-volume enum values; every unknown value becomes empty for later rejection.
// runtimeVolumeKind преобразует только enum временных дискового и оперативного томов; неизвестные значения становятся пустыми для последующего отказа.
func runtimeVolumeKind(value controlplanev1.RuntimeVolumeKind) string {
	switch value {
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK:
		return runtimecontract.RuntimeVolumeEphemeralDisk
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY:
		return runtimecontract.RuntimeVolumeEphemeralMemory
	default:
		return ""
	}
}

// runtimeNetworkDestination maps only DNS, runtime callback and provider proxy destinations, rejecting unregistered meanings through an empty result.
// runtimeNetworkDestination преобразует только назначения DNS, runtime callback и прокси провайдера, представляя незарегистрированные значения пустым результатом для отказа.
func runtimeNetworkDestination(value controlplanev1.RuntimeNetworkDestination) string {
	switch value {
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS:
		return runtimecontract.RuntimeEgressDNS
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK:
		return runtimecontract.RuntimeEgressRuntimeCallback
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY:
		return runtimecontract.RuntimeEgressProviderProxy
	default:
		return ""
	}
}

// runtimeNetworkProtocol maps only TCP and UDP enum values to contract protocol names, returning empty for all others.
// runtimeNetworkProtocol преобразует только enum TCP и UDP в имена протоколов контракта, возвращая пустую строку для остальных.
func runtimeNetworkProtocol(value controlplanev1.RuntimeNetworkProtocol) string {
	switch value {
	case controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP:
		return runtimecontract.RuntimeProtocolTCP
	case controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_UDP:
		return runtimecontract.RuntimeProtocolUDP
	default:
		return ""
	}
}

// runtimeKubernetesAccessKind recognizes only the NONE access profile; any other protobuf value remains unsupported rather than gaining a default permission.
// runtimeKubernetesAccessKind распознаёт только профиль доступа NONE; остальные значения protobuf остаются неподдерживаемыми, не получая разрешения по умолчанию.
func runtimeKubernetesAccessKind(value controlplanev1.RuntimeKubernetesAccessKind) string {
	switch value {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE:
		return runtimecontract.RuntimeKubernetesAccessNone
	default:
		return ""
	}
}

// providerSecretBinding requires a provider binding with a bounded DNS name, UID, resource version and digest length and returns metadata only; actual Secret bytes are verified separately.
// providerSecretBinding требует привязку провайдера с ограниченным DNS-именем, UID, resourceVersion и длиной хеша, возвращая только метаданные; фактические байты Secret проверяются отдельно.
func providerSecretBinding(revision *controlplanev1.RuntimeRevisionSnapshot) (ProviderSecretBinding, error) {
	binding := revision.GetProviderCredential()
	if binding == nil || !validDNSLabel(binding.GetSecretName()) || binding.GetSecretUid() == "" ||
		binding.GetSecretResourceVersion() == "" || len(binding.GetContentSha256()) != sha256.Size*2 {
		return ProviderSecretBinding{}, errors.New("provider credential binding is invalid")
	}
	return ProviderSecretBinding{Name: binding.GetSecretName(), UID: binding.GetSecretUid(),
		ResourceVersion: binding.GetSecretResourceVersion(), ContentSHA256: binding.GetContentSha256()}, nil
}

// validateCredentialProjection checks the exact runtime namespace, broker Secret identity, auth-key name, canonical SHA-256 and complete runtime-secret key mapping without reading its content.
// validateCredentialProjection проверяет точное пространство runtime, идентичность Secret брокера, имя ключа авторизации, канонический SHA-256 и полное соответствие runtime-ключей без чтения содержимого.
func validateCredentialProjection(input runtimecontract.RunnerInput, projection CredentialProjection, namespace string) error {
	if namespace != "kodex-runtime" || projection.Namespace != namespace || !validDNSLabel(projection.SecretName) ||
		projection.SecretUID == "" || projection.SecretResourceVersion == "" || projection.ProviderAuthKey != "provider-auth.json" {
		return errors.New("runtime credential projection binding is invalid")
	}
	digest, err := hex.DecodeString(projection.ContentSHA256)
	if err != nil || len(digest) != sha256.Size || projection.ContentSHA256 != hex.EncodeToString(digest) ||
		len(projection.RuntimeSecretKeys) != len(input.SecretProjections) {
		return errors.New("runtime credential projection binding is invalid")
	}
	for _, item := range input.SecretProjections {
		if projection.RuntimeSecretKeys[item.Name] != item.Name {
			return errors.New("runtime credential projection key set mismatch")
		}
	}
	return nil
}

// addCatalog copies revision capabilities, context, targets, enabled integration grants, MCP health pins and attachment metadata into the input; it skips absent artifact records and does not independently authorize the copied catalog.
// addCatalog переносит возможности, контекст, цели, включённые интеграционные гранты, привязки готовности MCP и метаданные вложений ревизии во вход; отсутствующие артефакты пропускает, скопированный каталог самостоятельно не авторизует.
func (manager *Manager) addCatalog(input *runtimecontract.RunnerInput, revision *controlplanev1.RuntimeRevisionSnapshot) {
	for _, capability := range revision.GetCapabilities() {
		input.Capabilities = append(input.Capabilities, capability.GetKey())
	}
	for _, message := range revision.GetSessionContext() {
		input.SessionContext = append(input.SessionContext, runtimecontract.RunnerSessionMessage{Role: message.GetRole(), Content: message.GetContent()})
	}
	for _, target := range revision.GetDelegationTargets() {
		input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{Ref: target.GetRef(), Name: target.GetName(), Purpose: target.GetPurpose(), RoleDescription: target.GetRoleDescription(), WorkflowStepKey: target.GetWorkflowStepKey(), WorkflowStepName: target.GetWorkflowStepName(), Instructions: target.GetInstructions(), ExpectedResult: target.GetExpectedResult()})
	}
	for _, grant := range revision.GetIntegrationGrants() {
		if !grant.GetEnabled() {
			continue
		}
		input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{
			Ref: grant.GetRef(), GrantVersion: grant.GetVersion(), ConnectionRef: grant.GetConnectionRef(), ConnectionVersion: grant.GetConnectionVersion(),
			ApprovalPolicy: strings.TrimPrefix(grant.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_"), DefinitionKey: grant.GetDefinitionKey(),
			DefinitionVersion: grant.GetDefinitionVersion(), DefinitionDigest: grant.GetDefinitionDigest(),
			ConnectionName: grant.GetConnectionName(), CapabilityKey: grant.GetCapabilityKey(),
			CapabilityName: grant.GetCapabilityName(), CapabilityDescription: grant.GetCapabilityDescription(),
			Operation: grant.GetOperation(), InputSchema: grant.GetInputSchema(), InputSchemaSHA256: grant.GetInputSchemaSha256(), Risk: grant.GetRisk(),
		})
	}
	for _, profile := range revision.GetManagedMcpProfiles() {
		health := profile.GetHealth()
		checkedAt := time.Time{}
		if health.GetCheckedAt() != nil && health.GetCheckedAt().CheckValid() == nil {
			checkedAt = health.GetCheckedAt().AsTime()
		}
		input.ManagedMCPProfiles = append(input.ManagedMCPProfiles, runtimecontract.ManagedMCPProfile{
			Provider: profile.GetProvider(), Version: int64(profile.GetVersion()), Namespace: profile.GetNamespace(), Required: profile.GetRequired(),
			ScopeKind: strings.TrimPrefix(profile.GetScopeKind().String(), "MANAGED_MCP_SCOPE_KIND_"), ScopeRef: profile.GetScopeRef(),
			ResolveGrantRef: profile.GetResolveGrantRef(), QueryGrantRef: profile.GetQueryGrantRef(), Digest: profile.GetDigest(),
			Health: runtimecontract.ManagedMCPHealthProof{TestRef: health.GetTestRef(), Generation: health.GetGeneration(), ConnectionRef: health.GetConnectionRef(),
				ConnectionVersion: health.GetConnectionVersion(), ConfigurationSHA256: health.GetConfigurationSha256(),
				CredentialRevisionRef: health.GetCredentialRevisionRef(), CredentialRevision: health.GetCredentialRevision(), CredentialSHA256: health.GetCredentialSha256(),
				DefinitionKey: health.GetDefinitionKey(), DefinitionVersion: health.GetDefinitionVersion(), DefinitionDigest: health.GetDefinitionDigest(),
				CheckedAt: checkedAt, Probe: health.GetProbe()},
		})
	}
	input.AttachmentSetRef = revision.GetAttachmentSetRef()
	input.AttachmentSetManifestDigest = revision.GetAttachmentSetManifestDigest()
	input.AttachmentContext = revision.GetAttachmentContext()
	for _, set := range revision.GetAttachmentSets() {
		input.AttachmentSets = append(input.AttachmentSets, runtimecontract.RunnerAttachmentSet{
			Ref: set.GetRef(), ManifestDigest: set.GetManifestDigest(), Purpose: set.GetPurpose(),
			Scope: set.GetScope(), Provenance: set.GetProvenance(), TurnRef: set.GetTurnRef(),
		})
	}
	for _, runtimeArtifact := range revision.GetInputArtifacts() {
		artifact := runtimeArtifact.GetArtifact()
		if artifact == nil {
			continue
		}
		input.InputArtifacts = append(input.InputArtifacts, runtimecontract.RunnerInputArtifact{
			Ref: artifact.GetRef(), FileName: artifact.GetFileName(), MediaType: artifact.GetMediaType(),
			Digest: artifact.GetDigest(), SizeBytes: artifact.GetSizeBytes(), Revision: int64(artifact.GetRevision()),
			Version: artifact.GetVersion(), Scope: runtimeArtifact.GetScope(), Position: runtimeArtifact.GetPosition(),
			Source: runnerArtifactSource(artifact.GetSource()), AttachmentSetRef: runtimeArtifact.GetAttachmentSetRef(),
			AttachmentPurpose: runtimeArtifact.GetAttachmentPurpose(), Provenance: runtimeArtifact.GetProvenance(),
		})
	}
}

// runnerArtifactSource maps the five supported artifact-source enum values to runner names and leaves unknown origins empty for validation failure.
// runnerArtifactSource преобразует пять поддерживаемых источников артефактов в имена runner, оставляя неизвестное происхождение пустым для отказа проверки.
func runnerArtifactSource(source controlplanev1.ArtifactSource) string {
	switch source {
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER:
		return "CONTROL_CENTER"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT:
		return "AGENT_RESULT"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTEGRATION_RESULT:
		return "INTEGRATION_RESULT"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_KNOWLEDGE_SOURCE:
		return "KNOWLEDGE_SOURCE"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTERACTION_ATTACHMENT:
		return "INTERACTION_ATTACHMENT"
	default:
		return ""
	}
}

// EnsureTurn delegates to guarded turn materialization using the supplied context without adding a lease-renewal check itself; callers requiring fresh publication authority use EnsureTurnGuarded.
// EnsureTurn делегирует материализацию хода с guard переданного контекста, самостоятельно не добавляя проверку продления аренды; вызывающий код со свежими полномочиями публикации использует EnsureTurnGuarded.
func (manager *Manager) EnsureTurn(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding, credentials CredentialProjection) error {
	return manager.EnsureTurnGuarded(ctx, input, providerBinding, credentials, func(publish func(context.Context) error) error {
		return publish(ctx)
	})
}

// EnsureTurnGuarded validates turn/image/revision/projection pins, prepares the PVC, policies and immutable data, then invokes the supplied guard only for Pod publication and verifies any existing Pod; preparation is not rolled back here.
// EnsureTurnGuarded проверяет привязки хода, образа, ревизии и проекции, готовит PVC, политики и неизменяемые данные, затем вызывает переданный guard только для публикации Pod и сверяет существующий Pod; подготовка здесь не откатывается.
func (manager *Manager) EnsureTurnGuarded(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding, credentials CredentialProjection, guard func(func(context.Context) error) error) error {
	if guard == nil {
		return errors.New("runtime publication guard is invalid")
	}
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || manager.validateImage(input) != nil {
		return errors.New("runtime turn input is invalid")
	}
	if err := validateRuntimeRevisionDigest(input, providerBinding); err != nil {
		return err
	}
	if err := validateCredentialProjection(input, credentials, manager.config.RuntimeNamespace); err != nil {
		return err
	}
	if err := manager.ensureSessionPVC(ctx, input); err != nil {
		return fmt.Errorf("ensure runtime session volume: %w", err)
	}
	token, err := newTicket()
	if err != nil {
		return fmt.Errorf("generate runtime execution ticket: %w", err)
	}
	secretName := ticketName(input.LeaseRef)
	podName := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
	if err := manager.ensureExecutionPolicy(ctx, input, podName); err != nil {
		return fmt.Errorf("ensure runtime execution policy: %w", err)
	}
	if err := manager.ensureProjection(ctx, podName, "turn", input); err != nil {
		return fmt.Errorf("ensure runtime VFS projection: %w", err)
	}
	if err := manager.ensureTicket(ctx, secretName, podName, "turn", input, token, nil, &providerBinding); err != nil {
		return fmt.Errorf("ensure runtime execution ticket: %w", err)
	}
	pod := manager.runtimePod(input, providerBinding, &credentials, secretName, podName, "turn")
	if pod == nil {
		return errors.New("materialize runtime egress grant")
	}
	return guard(func(publishContext context.Context) error {
		if err := publishContext.Err(); err != nil {
			return err
		}
		_, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(publishContext, pod, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, getErr := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(publishContext, podName, metav1.GetOptions{})
			if getErr != nil || !runtimePodMatches(existing, pod) {
				return errors.New("existing runtime turn pod conflicts with immutable revision")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("create runtime turn pod: %w", err)
		}
		return nil
	})
}

// ensureExecutionPolicy verifies the execution service-account/profile binding, creates a non-automount account, requires Role/RoleBinding absence and materializes the exact execution egress policy in that order.
// ensureExecutionPolicy проверяет привязку service-account и профиля исполнения, создаёт аккаунт без automount, требует отсутствия Role и RoleBinding, затем материализует точную выходную политику в этом порядке.
func (manager *Manager) ensureExecutionPolicy(ctx context.Context, input runtimecontract.RunnerInput, podName string) error {
	if input.EffectiveKubernetesAccess.ServiceAccountName != runtimecontract.RuntimeServiceAccountName(input.LeaseRef) ||
		input.EnvironmentPolicy.KubernetesAccess != input.EffectiveKubernetesAccess.Profile ||
		runtimecontract.ValidateRuntimeKubernetesAccess(input.EffectiveKubernetesAccess) != nil {
		return errors.New("effective Kubernetes access does not match execution identity")
	}
	metadata := manager.executionMetadata(input)
	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metadata, AutomountServiceAccountToken: boolPointer(false)}
	serviceAccount.Name = input.EffectiveKubernetesAccess.ServiceAccountName
	if err := manager.ensureServiceAccount(ctx, serviceAccount); err != nil {
		return err
	}
	if err := manager.ensureExecutionRBACAbsent(ctx, input); err != nil {
		return err
	}
	networkPolicy, err := manager.executionNetworkPolicy(input, podName)
	if err != nil {
		return err
	}
	return manager.ensureNetworkPolicy(ctx, networkPolicy)
}

// executionMetadata derives turn selection labels and revision/lease/controller/policy digest annotations from the selected input without granting Kubernetes access.
// executionMetadata выводит метки выбора хода и аннотации ревизии, аренды, контроллера и хешей политик из выбранного входа без выдачи доступа Kubernetes.
func (manager *Manager) executionMetadata(input runtimecontract.RunnerInput) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: "turn", executionHashLabel: shortHash(input.LeaseRef)},
		Annotations: map[string]string{
			revisionAnnotation: input.RuntimeRevisionDigest, leaseAnnotation: input.LeaseRef, controllerAnnotation: manager.config.ControllerPodUID,
			resourcesAnnotation: input.EnvironmentPolicy.ResourcesDigest, volumesAnnotation: input.EnvironmentPolicy.VolumesDigest,
			networkAnnotation: input.EnvironmentPolicy.NetworkDigest, rbacProfileAnnotation: input.EnvironmentPolicy.RBACDigest,
			effectiveRBACAnnotation: input.EffectiveKubernetesAccess.Digest,
		},
	}
}

// executionNetworkPolicy requires the lease-derived Pod name and emits only configured DNS, controller-callback or platform-proxy peers with explicit ports; unknown destinations fail instead of broadening egress.
// executionNetworkPolicy требует имя Pod, выведенное из аренды, и создаёт только заданных получателей DNS, callback контроллера и прокси платформы с явными портами; неизвестные назначения отклоняются без расширения выхода.
func (manager *Manager) executionNetworkPolicy(input runtimecontract.RunnerInput, podName string) (*networkingv1.NetworkPolicy, error) {
	if podName != runtimecontract.RuntimeTurnPodName(input.LeaseRef) {
		return nil, errors.New("runtime network policy Pod identity is invalid")
	}
	metadata := manager.executionMetadata(input)
	metadata.Name = runtimecontract.RuntimeNetworkPolicyName(input.LeaseRef)
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metadata, Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{managedLabel: "true", modeLabel: "turn", executionHashLabel: shortHash(input.LeaseRef)}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
	}}
	for _, item := range input.EnvironmentPolicy.Network.Egress {
		protocol := corev1.Protocol(item.Protocol)
		port := intstr.FromInt32(item.Port)
		rule := networkingv1.NetworkPolicyEgressRule{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}}}
		switch item.Destination {
		case runtimecontract.RuntimeEgressDNS:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
			}}
		case runtimecontract.RuntimeEgressRuntimeCallback:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": manager.config.ControlNamespace}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "runtime-controller", "app.kubernetes.io/component": "internal-controller"}},
			}}
		case runtimecontract.RuntimeEgressProviderProxy:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": manager.config.ControlNamespace}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "egress-gateway", "app.kubernetes.io/component": "platform-egress"}},
			}}
		default:
			return nil, errors.New("runtime network destination is unsupported")
		}
		policy.Spec.Egress = append(policy.Spec.Egress, rule)
	}
	return policy, nil
}

// ensureServiceAccount creates the desired account or verifies required metadata and token-automount settings on an existing one; conflicting immutable execution state is not patched.
// ensureServiceAccount создаёт нужный аккаунт либо сверяет обязательные метаданные и automount токена существующего; конфликтующее состояние исполнения не исправляет.
func (manager *Manager) ensureServiceAccount(ctx context.Context, desired *corev1.ServiceAccount) error {
	_, err := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
			!apiequality.Semantic.DeepEqual(existing.AutomountServiceAccountToken, desired.AutomountServiceAccountToken) {
			return errors.New("existing runtime ServiceAccount conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime ServiceAccount")
	}
	return nil
}

// ensureNetworkPolicy creates the desired egress policy or compares required metadata and the entire existing spec, rejecting conflicts without patching them.
// ensureNetworkPolicy создаёт нужную выходную политику либо сравнивает обязательные метаданные и всю существующую spec, отклоняя конфликт без исправления.
func (manager *Manager) ensureNetworkPolicy(ctx context.Context, desired *networkingv1.NetworkPolicy) error {
	_, err := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) || !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
			return errors.New("existing runtime NetworkPolicy conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime NetworkPolicy")
	}
	return nil
}

// ensureExecutionRBACAbsent requires NotFound for both exact lease-derived Role and RoleBinding names; an existing object or read failure rejects the no-access execution.
// ensureExecutionRBACAbsent требует NotFound для обоих точных имён Role и RoleBinding, выведенных из аренды; существующий объект либо ошибка чтения запрещает исполнение без доступа.
func (manager *Manager) ensureExecutionRBACAbsent(ctx context.Context, input runtimecontract.RunnerInput) error {
	if _, err := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return errors.New("runtime Role exists for a no-access execution")
	}
	if _, err := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleBindingName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return errors.New("runtime RoleBinding exists for a no-access execution")
	}
	return nil
}

// managedMetadataMatches requires every desired label and annotation to match while permitting additional existing metadata; it is not complete object equality or an ownership authorization check.
// managedMetadataMatches требует совпадения каждой нужной метки и аннотации, разрешая дополнительные существующие метаданные; полной проверкой объекта или авторизацией владения не является.
func managedMetadataMatches(existing, desired metav1.ObjectMeta) bool {
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			return false
		}
	}
	for key, value := range desired.Annotations {
		if existing.Annotations[key] != value {
			return false
		}
	}
	return true
}

// EnsureWarm validates the system warm input and credentials, reconciles its PVC and immutable ticket/projection, asynchronously replaces stale Pods and reports readiness; it does not recreate a ticket while the old Pod is still observed.
// EnsureWarm проверяет системный warm-вход и учётные данные, согласует PVC и неизменяемые билет и проекцию, асинхронно заменяет устаревший Pod и сообщает готовность; билет не пересоздаётся, пока старый Pod ещё наблюдается.
func (manager *Manager) EnsureWarm(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding) (bool, error) {
	if input.Mode != runtimecontract.RunnerModeWarm || input.Validate() != nil || manager.validateImage(input) != nil ||
		input.EnvironmentPolicy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone ||
		input.EffectiveKubernetesAccess.ServiceAccountName != manager.config.RunnerServiceAccount {
		return false, errors.New("warm runtime input is invalid")
	}
	if err := manager.validateProviderSecret(ctx, input, providerBinding); err != nil {
		return false, err
	}
	environmentSecrets, err := manager.materializeEnvironmentSecrets(ctx, input)
	if err != nil {
		return false, err
	}
	if err := manager.ensureSessionPVC(ctx, input); err != nil {
		return false, err
	}
	const podName = "system-assistant-warm"
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	compatibilityDigest, _ := runtimecontract.WarmCompatibilityDigest(input)
	existing, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil && (existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
		existing.Annotations[warmCompatibilityAnnotation] != compatibilityDigest ||
		existing.Annotations[controllerAnnotation] != manager.config.ControllerPodUID ||
		runtimePodTerminal(existing)) {
		boundTicket := runtimeInputSecretName(existing)
		if deleteErr := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return false, errors.New("replace stale warm runtime pod")
		}
		if boundTicket != "" {
			if deleteErr := manager.deleteOwnedWarmTicket(ctx, boundTicket); deleteErr != nil {
				return false, deleteErr
			}
		}
		// Pod deletion is asynchronous. Recreating an immutable Secret with the
		// same name while the old Pod still runs would make its mounted token
		// differ from the callback verifier. A later reconciliation creates the
		// fresh ticket and Pod only after Kubernetes reports the old Pod absent.
		return false, nil
	}
	if apierrors.IsNotFound(err) {
		if ticketErr := manager.removeConflictingWarmTicket(ctx, secretName, input, environmentSecrets, &providerBinding); ticketErr != nil {
			return false, ticketErr
		}
		token, ticketErr := newTicket()
		if ticketErr != nil {
			return false, ticketErr
		}
		if ticketErr = manager.ensureTicket(ctx, secretName, podName, "warm", input, token, environmentSecrets, &providerBinding); ticketErr != nil {
			return false, ticketErr
		}
		if projectionErr := manager.ensureProjection(ctx, podName, "warm", input); projectionErr != nil {
			return false, projectionErr
		}
		pod := manager.runtimePod(input, providerBinding, nil, secretName, podName, "warm")
		if pod == nil {
			return false, errors.New("materialize warm runtime egress grant")
		}
		existing, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(ctx, pod, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
		}
		if err != nil {
			return false, fmt.Errorf("create warm runtime Pod: %w", err)
		}
	} else if err != nil {
		return false, errors.New("read warm runtime pod")
	}
	if err := manager.cleanupStaleWarmTickets(ctx, secretName); err != nil {
		return false, err
	}
	if err := manager.cleanupStaleWarmProjections(ctx, runtimeProjectionName(input)); err != nil {
		return false, err
	}
	return podReady(existing), nil
}

// DeactivateWarm deletes the fixed warm Pod only after managed/warm label checks, removes its bound ticket and stale warm projections/tickets, and retains session PVCs; it does not wait for Pod disappearance here.
// DeactivateWarm удаляет закреплённый warm Pod после проверки меток managed и warm, убирает связанный билет и устаревшие warm-проекции и билеты, сохраняя PVC сессий; исчезновения Pod здесь не ожидает.
func (manager *Manager) DeactivateWarm(ctx context.Context) error {
	const podName = "system-assistant-warm"
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		if pod.Labels[managedLabel] != "true" || pod.Labels[modeLabel] != "warm" {
			return errors.New("warm runtime Pod ownership is invalid")
		}
		boundTicket := runtimeInputSecretName(pod)
		if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete warm runtime Pod")
		}
		if boundTicket != "" {
			if err := manager.deleteOwnedWarmTicket(ctx, boundTicket); err != nil {
				return err
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return errors.New("read warm runtime Pod")
	}
	if err := manager.cleanupStaleWarmTickets(ctx, ""); err != nil {
		return err
	}
	return manager.cleanupStaleWarmProjections(ctx, "")
}

// deleteOwnedWarmTicket requires managed/warm labels, optionally deletes the projection derived from a decodable ticket input, then removes the ticket; an undecodable input prevents derived projection deletion, not ticket deletion.
// deleteOwnedWarmTicket требует метки managed и warm, при разбираемом входе билета удаляет производную проекцию, затем билет; неразбираемый вход препятствует удалению производной проекции, но не билета.
func (manager *Manager) deleteOwnedWarmTicket(ctx context.Context, name string) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read stale warm runtime ticket")
	}
	if secret.Labels[managedLabel] != "true" || secret.Labels[modeLabel] != "warm" {
		return errors.New("stale warm runtime ticket ownership is invalid")
	}
	if input, decodeErr := runtimecontract.DecodeRunnerInput(secret.Data[inputKey]); decodeErr == nil {
		if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, runtimeProjectionName(input), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete stale warm runtime projection")
		}
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete stale warm runtime ticket")
	}
	return nil
}

// cleanupStaleWarmProjections inspects one managed/warm ConfigMap page of up to 256 objects, retains the named current projection and joins deletion failures without following continuation pages.
// cleanupStaleWarmProjections просматривает одну страницу до 256 ConfigMap с метками managed и warm, сохраняет указанную текущую проекцию и объединяет ошибки удаления без перехода к следующим страницам.
func (manager *Manager) cleanupStaleWarmProjections(ctx context.Context, current string) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String()
	projections, err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list stale warm runtime projections")
	}
	var result error
	for index := range projections.Items {
		if projections.Items[index].Name == current {
			continue
		}
		if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projections.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete stale warm runtime projection"))
		}
	}
	return result
}

// cleanupStaleWarmTickets inspects one managed/warm Secret page of up to 256 objects, retains the named current ticket and joins deletion failures; this helper does not paginate further.
// cleanupStaleWarmTickets просматривает одну страницу до 256 Secret с метками managed и warm, сохраняет указанный текущий билет и объединяет ошибки удаления; дальше страницы не обходит.
func (manager *Manager) cleanupStaleWarmTickets(ctx context.Context, current string) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String()
	secrets, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list stale warm runtime tickets")
	}
	var result error
	for index := range secrets.Items {
		if secrets.Items[index].Name == current {
			continue
		}
		if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secrets.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete stale warm runtime ticket"))
		}
	}
	return result
}

// runtimeInputSecretName reads the first Secret-backed runtime-ticket volume name from a Pod, returning empty when the Pod or binding is absent without authorizing that name.
// runtimeInputSecretName читает имя первого Secret-тома runtime-ticket из Pod, возвращая пустую строку при отсутствии Pod либо привязки, не авторизуя найденное имя.
func runtimeInputSecretName(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "runtime-ticket" && volume.Secret != nil {
			return volume.Secret.SecretName
		}
	}
	return ""
}

// removeConflictingWarmTicket reads the selected warm-ticket name and removes it if exact immutable readback does not match; it neither creates the replacement nor checks Pod absence itself.
// removeConflictingWarmTicket читает выбранное имя warm-билета и удаляет билет при несовпадении точной неизменяемой сверки; замену не создаёт и отсутствие Pod самостоятельно не проверяет.
func (manager *Manager) removeConflictingWarmTicket(ctx context.Context, secretName string, input runtimecontract.RunnerInput, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read warm runtime ticket")
	}
	if runtimeTicketMatches(secret, "system-assistant-warm", "warm", input, "", environmentSecrets, providerBinding) {
		return nil
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("replace stale warm runtime ticket")
	}
	return nil
}

// RegisterWarmTurn validates a turn input and nonempty token, resolves the running warm provider binding and creates an exact warm-turn ticket without publishing a separate Pod.
// RegisterWarmTurn проверяет вход хода и непустой токен, разрешает привязку провайдера работающего warm runtime и создаёт точный билет warm-turn без публикации отдельного Pod.
func (manager *Manager) RegisterWarmTurn(ctx context.Context, input runtimecontract.RunnerInput, token string) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || token == "" {
		return errors.New("warm turn registration is invalid")
	}
	binding, err := manager.warmProviderSecretBinding(ctx, input)
	if err != nil {
		return err
	}
	return manager.ensureTicket(ctx, ticketName(input.LeaseRef), "system-assistant-warm", "warm-turn", input, token, nil, &binding)
}

// warmProviderSecretBinding resolves the fixed warm Pod's ticket, matches provider account/revision/content pins to the turn and verifies the pinned provider Secret before returning metadata.
// warmProviderSecretBinding разрешает билет закреплённого warm Pod, сопоставляет аккаунт, ревизию и содержимое провайдера с ходом и проверяет закреплённый Secret провайдера до возврата метаданных.
func (manager *Manager) warmProviderSecretBinding(ctx context.Context, input runtimecontract.RunnerInput) (ProviderSecretBinding, error) {
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, "system-assistant-warm", metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read warm runtime Pod credential binding")
	}
	name := runtimeInputSecretName(pod)
	if name == "" {
		return ProviderSecretBinding{}, errors.New("warm runtime Pod credential binding is missing")
	}
	ticket, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read warm runtime credential binding")
	}
	bound, err := runtimecontract.DecodeRunnerInput(ticket.Data[inputKey])
	if err != nil || bound.Mode != runtimecontract.RunnerModeWarm ||
		bound.ProviderAccountRef != input.ProviderAccountRef ||
		bound.ProviderCredentialRef != input.ProviderCredentialRef ||
		bound.ProviderCredentialSHA256 != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, errors.New("warm runtime credential binding is invalid")
	}
	binding, err := providerSecretBindingFromTicket(ticket, bound)
	if err != nil || manager.validateProviderSecret(ctx, input, binding) != nil {
		return ProviderSecretBinding{}, errors.New("warm runtime provider credential is unavailable")
	}
	return binding, nil
}

// WarmTicket reads the revision/controller-derived Secret and returns a token of at least thirty-two bytes to trusted callers; it does not itself validate the stored runtime binding.
// WarmTicket читает Secret с именем из ревизии и контроллера и возвращает доверенному вызывающему токен длиной не менее тридцати двух байтов; сохранённую привязку runtime самостоятельно не проверяет.
func (manager *Manager) WarmTicket(ctx context.Context, revisionRef, revisionDigest string) (string, error) {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
	if err != nil || len(secret.Data[ticketKey]) < 32 {
		return "", errors.New("read warm runtime ticket")
	}
	return string(secret.Data[ticketKey]), nil
}

// ResolveWarm requires revision pins and a token, compares the stored token in constant time and decodes an exact warm revision; live owner eligibility and lease decisions remain outside this local ticket lookup.
// ResolveWarm требует привязки ревизии и токен, сравнивает сохранённый токен за постоянное время и декодирует точную warm-ревизию; актуальная допустимость владельца и решения об аренде остаются вне локального поиска билета.
func (manager *Manager) ResolveWarm(ctx context.Context, revisionRef, revisionDigest, token string) (runtimecontract.RunnerInput, error) {
	if revisionRef == "" || len(revisionDigest) != sha256.Size*2 || token == "" {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback authority is incomplete")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
	if err != nil {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback ticket is unavailable")
	}
	if subtle.ConstantTimeCompare(secret.Data[ticketKey], []byte(token)) != 1 {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback ticket does not match")
	}
	input, err := runtimecontract.DecodeRunnerInput(secret.Data[inputKey])
	if err != nil || input.Mode != runtimecontract.RunnerModeWarm || input.RuntimeRevisionRef != revisionRef ||
		input.RuntimeRevisionDigest != revisionDigest {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback binding is invalid")
	}
	return input, nil
}

// ResolveTurn verifies the exact lease-derived ticket token and decodes a turn input with the same lease reference; it does not replace fresh authoritative execution checks.
// ResolveTurn проверяет токен точного билета, выведенного из аренды, и декодирует вход хода с той же ссылкой аренды; свежие авторитетные проверки исполнения не заменяет.
func (manager *Manager) ResolveTurn(ctx context.Context, leaseRef, token string) (runtimecontract.RunnerInput, error) {
	if leaseRef == "" || token == "" {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback authority is invalid")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(leaseRef), metav1.GetOptions{})
	if err != nil || subtle.ConstantTimeCompare(secret.Data[ticketKey], []byte(token)) != 1 {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback authority is invalid")
	}
	input, err := runtimecontract.DecodeRunnerInput(secret.Data[inputKey])
	if err != nil || input.Mode != runtimecontract.RunnerModeTurn || input.LeaseRef != leaseRef {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback binding is invalid")
	}
	return input, nil
}

// StopTurn requests Pod deletion using the ticket's bound name or lease-derived fallback while retaining ticket, projection and network policy for terminal callback; a successful Delete request is not a joined stop.
// StopTurn запрашивает удаление Pod по имени из билета либо запасному имени из аренды, сохраняя билет, проекцию и сеть для terminal-callback; успешный запрос Delete не означает подтверждённую остановку.
func (manager *Manager) StopTurn(ctx context.Context, leaseRef string) error {
	secretName := ticketName(leaseRef)
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.New("read runtime ticket before stopping pod")
	}
	podName := runtimecontract.RuntimeTurnPodName(leaseRef)
	if secret != nil {
		if boundName := secret.Annotations[podAnnotation]; boundName != "" {
			podName = boundName
		}
	}
	if podName != "" {
		if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("stop runtime pod")
		}
	}
	return nil
}

// DeleteTurn uses a best-effort ticket lookup to retain the shared warm Pod, otherwise requests stop, then attempts ticket/projection/policy cleanup and joins failures; it neither deletes session PVCs nor commits owner terminal state.
// DeleteTurn использует необязательное чтение билета для сохранения общего warm Pod, иначе запрашивает остановку, затем пытается очистить билет, проекцию и политики, объединяя ошибки; PVC сессий не удаляет и terminal-состояние владельца не фиксирует.
func (manager *Manager) DeleteTurn(ctx context.Context, leaseRef string) error {
	secretName := ticketName(leaseRef)
	secret, _ := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, secretName, metav1.GetOptions{})
	var result error
	// Успешный warm turn сохраняет общий Pod; остановка timeout выполняется StopTurn.
	if secret == nil || secret.Annotations[podAnnotation] != "system-assistant-warm" {
		result = manager.StopTurn(ctx, leaseRef)
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		result = errors.Join(result, errors.New("delete completed runtime ticket"))
	}
	projectionName := "runtime-projection-" + shortHash(leaseRef)
	if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projectionName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		result = errors.Join(result, errors.New("delete completed runtime VFS projection"))
	}
	if err := manager.deleteExecutionPolicy(ctx, leaseRef); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

// deleteExecutionPolicy derives the shortened lease identity and delegates cleanup of only its named execution policy resources.
// deleteExecutionPolicy выводит сокращённый идентификатор аренды и делегирует очистку только соответствующих именованных ресурсов политики исполнения.
func (manager *Manager) deleteExecutionPolicy(ctx context.Context, leaseRef string) error {
	return manager.deleteExecutionPolicyByHash(ctx, shortHash(leaseRef))
}

// deleteExecutionPolicyByHash requires a sixteen-character hash, attempts the exact NetworkPolicy, RoleBinding, Role and ServiceAccount deletions in order, ignores NotFound and joins other failures without rollback.
// deleteExecutionPolicyByHash требует хеш из шестнадцати символов, последовательно пытается удалить точные NetworkPolicy, RoleBinding, Role и ServiceAccount, игнорирует NotFound и объединяет прочие ошибки без отката.
func (manager *Manager) deleteExecutionPolicyByHash(ctx context.Context, executionHash string) error {
	if len(executionHash) != 16 {
		return errors.New("runtime execution identity is unavailable for policy cleanup")
	}
	var result error
	deletions := []struct {
		name   string
		remove func(string) error
	}{
		{name: "runtime-net-" + executionHash, remove: func(name string) error {
			return manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-rb-" + executionHash, remove: func(name string) error {
			return manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-role-" + executionHash, remove: func(name string) error {
			return manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-sa-" + executionHash, remove: func(name string) error {
			return manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
	}
	for _, deletion := range deletions {
		if err := deletion.remove(deletion.name); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete completed runtime execution policy"))
		}
	}
	return result
}

// TurnPodObservation carries a closed observational state and diagnostic code, not an authoritative run result or proof of callback completion.
// TurnPodObservation содержит закрытое наблюдаемое состояние и диагностический код, а не авторитетный результат запуска или доказательство завершения callback.
type TurnPodObservation struct {
	State          string
	DiagnosticCode string
	ObservedPodUID string
}

// TurnPodMemoryDiagnostic identifies a current OOMKilled termination of a known runtime container, rather than inferring memory failure from exit code 137 alone.
// TurnPodMemoryDiagnostic обозначает текущее завершение известного runtime-контейнера с OOMKilled, не выводя нехватку памяти только из кода выхода 137.
const TurnPodMemoryDiagnostic = "CONTAINER_OOM_KILLED"

// TurnPodEvictedDiagnostic identifies the typed Pod reason without inferring storage,
// memory, node pressure or any particular resource from the private message.
// TurnPodEvictedDiagnostic обозначает типизированную причину Pod без вывода о диске,
// памяти, нагрузке узла либо конкретном ресурсе из закрытого сообщения.
const TurnPodEvictedDiagnostic = "ENVIRONMENT_EVICTED"

// ObserveTurnPod checks cold execution metadata or warm compatibility from one GET.
// Closed cold resource diagnostics carry that observed UID, never a historical UID
// proof. Creation/adoption keeps full spec checks; observation cannot complete a run.
// ObserveTurnPod проверяет метаданные холодного исполнения либо warm-совместимость
// по одному GET. Закрытая диагностика несёт наблюдённый UID, не доказательство его
// истории. Создание сохраняет полную проверку spec; наблюдение не завершает запуск.
func (manager *Manager) ObserveTurnPod(ctx context.Context, input runtimecontract.RunnerInput, warmExecution bool) (TurnPodObservation, error) {
	podName := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
	if warmExecution {
		podName = "system-assistant-warm"
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return TurnPodObservation{State: "MISSING", DiagnosticCode: "POD_MISSING"}, nil
	}
	if err != nil {
		return TurnPodObservation{}, errors.New("read runtime execution pod")
	}
	if warmExecution {
		compatibility, compatibilityErr := runtimecontract.WarmCompatibilityDigest(input)
		if compatibilityErr != nil || pod.Annotations[warmCompatibilityAnnotation] != compatibility {
			return TurnPodObservation{State: "CONFLICT", DiagnosticCode: "WARM_COMPATIBILITY_CONFLICT"}, nil
		}
	} else {
		if pod.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest {
			return TurnPodObservation{State: "CONFLICT", DiagnosticCode: "RUNTIME_REVISION_CONFLICT"}, nil
		}
		expected := manager.runtimePodMetadata(input, podName, "turn")
		if pod.UID == "" || pod.Name != expected.Name || pod.Namespace != expected.Namespace ||
			!managedMetadataMatches(pod.ObjectMeta, expected) {
			return TurnPodObservation{State: "CONFLICT", DiagnosticCode: "RUNTIME_EXECUTION_CONFLICT"}, nil
		}
	}
	diagnostic := "POD_FAILED"
	if !warmExecution {
		diagnostic = terminalContainerDiagnostic(pod)
	}
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return TurnPodObservation{State: "SUCCEEDED", DiagnosticCode: "POD_SUCCEEDED_WITHOUT_CALLBACK"}, nil
	case corev1.PodFailed:
		return TurnPodObservation{State: "FAILED", DiagnosticCode: diagnostic, ObservedPodUID: string(pod.UID)}, nil
	case corev1.PodRunning:
		if !warmExecution && runtimePodTerminal(pod, "role-runtime", "provider-runtime", "provider-credential-relay") {
			return TurnPodObservation{State: "FAILED", DiagnosticCode: diagnostic, ObservedPodUID: string(pod.UID)}, nil
		}
		if podReady(pod) {
			return TurnPodObservation{State: "READY"}, nil
		}
		return TurnPodObservation{State: "STARTING"}, nil
	case corev1.PodPending:
		return TurnPodObservation{State: "STARTING"}, nil
	default:
		return TurnPodObservation{State: "UNKNOWN"}, nil
	}
}

// TurnPodState returns only the state portion of ObserveTurnPod while preserving its error; it does not perform a separate observation or state transition.
// TurnPodState возвращает только состояние ObserveTurnPod, сохраняя ошибку; отдельное наблюдение или переход состояния не выполняет.
func (manager *Manager) TurnPodState(ctx context.Context, input runtimecontract.RunnerInput, warmExecution bool) (string, error) {
	observation, err := manager.ObserveTurnPod(ctx, input, warmExecution)
	return observation.State, err
}

// terminalContainerDiagnostic prioritizes current known-container OOMKilled, then
// typed Pod eviction and generic exits. History, exit137 and messages prove no cause.
// terminalContainerDiagnostic отдаёт приоритет текущему OOMKilled известного
// контейнера, затем типизированному выселению Pod и общим выходам. История, exit137
// и сообщения не доказывают причину.
func terminalContainerDiagnostic(pod *corev1.Pod) string {
	// OOM одного известного контейнера важнее обычного выхода соседа.
	// Исторический LastTerminationState и один exit137 не доказывают OOM.
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses} {
		for _, status := range statuses {
			if status.State.Terminated == nil || status.State.Terminated.Reason != "OOMKilled" {
				continue
			}
			switch status.Name {
			case "workspace-prepare", "workspace-init", "role-runtime", "provider-runtime", "provider-credential-relay":
				return TurnPodMemoryDiagnostic
			}
		}
	}
	if pod.Status.Reason == "Evicted" {
		return TurnPodEvictedDiagnostic
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Terminated == nil {
			continue
		}
		switch status.Name {
		case "workspace-prepare":
			if status.State.Terminated.ExitCode != 0 {
				return "WORKSPACE_PREPARE_EXITED_NONZERO"
			}
		case "workspace-init":
			if status.State.Terminated.ExitCode != 0 {
				return "WORKSPACE_INIT_EXITED_NONZERO"
			}
		}
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated == nil {
			continue
		}
		switch status.Name {
		case "role-runtime":
			if status.State.Terminated.ExitCode == 0 {
				return "ROLE_RUNTIME_EXITED_ZERO"
			}
			return "ROLE_RUNTIME_EXITED_NONZERO"
		case "provider-runtime":
			return "PROVIDER_RUNTIME_EXITED"
		case "provider-credential-relay":
			return "PROVIDER_CREDENTIAL_RELAY_EXITED"
		}
	}
	return "POD_FAILED"
}

// runtimePodMatches compares required metadata and selected security, identity, container and volume fields after normalizing known API defaults; it is not equality of every Pod field.
// runtimePodMatches сравнивает обязательные метаданные и выбранные поля безопасности, идентичности, контейнеров и томов после нормализации известных значений API по умолчанию; равенством всех полей Pod не является.
func runtimePodMatches(existing, desired *corev1.Pod) bool {
	if existing == nil || desired == nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
		existing.Spec.ServiceAccountName != desired.Spec.ServiceAccountName ||
		existing.Spec.HostNetwork || existing.Spec.HostPID || existing.Spec.HostIPC ||
		!apiequality.Semantic.DeepEqual(existing.Spec.SecurityContext, desired.Spec.SecurityContext) ||
		!apiequality.Semantic.DeepEqual(existing.Spec.EnableServiceLinks, desired.Spec.EnableServiceLinks) ||
		existing.Spec.RestartPolicy != desired.Spec.RestartPolicy ||
		!apiequality.Semantic.DeepEqual(existing.Spec.AutomountServiceAccountToken, desired.Spec.AutomountServiceAccountToken) ||
		!apiequality.Semantic.DeepEqual(canonicalRuntimeContainers(existing.Spec.InitContainers), canonicalRuntimeContainers(desired.Spec.InitContainers)) ||
		!apiequality.Semantic.DeepEqual(canonicalRuntimeContainers(existing.Spec.Containers), canonicalRuntimeContainers(desired.Spec.Containers)) ||
		!apiequality.Semantic.DeepEqual(existing.Spec.Volumes, desired.Spec.Volumes) {
		return false
	}
	return true
}

// canonicalRuntimeContainers deep-copies containers and clears termination-message defaults, port protocols and probe success thresholds solely for immutable Pod comparison, leaving supplied objects unchanged.
// canonicalRuntimeContainers глубоко копирует контейнеры и очищает значения по умолчанию сообщения завершения, протоколов портов и порогов успеха probes только для сравнения Pod, не меняя исходные объекты.
func canonicalRuntimeContainers(values []corev1.Container) []corev1.Container {
	result := make([]corev1.Container, len(values))
	for index := range values {
		result[index] = *values[index].DeepCopy()
		result[index].TerminationMessagePath = ""
		result[index].TerminationMessagePolicy = ""
		for portIndex := range result[index].Ports {
			result[index].Ports[portIndex].Protocol = ""
		}
		for _, probe := range []*corev1.Probe{result[index].StartupProbe, result[index].ReadinessProbe, result[index].LivenessProbe} {
			if probe != nil {
				probe.SuccessThreshold = 0
			}
		}
	}
	return result
}

// runtimeProjectionName hashes the lease reference or, when absent, revision/digest/workload tuple into a bounded projection name without using the hash as authorization.
// runtimeProjectionName хеширует ссылку аренды либо при её отсутствии набор ревизии, хеша и workload в ограниченное имя проекции, не используя хеш как авторизацию.
func runtimeProjectionName(input runtimecontract.RunnerInput) string {
	identity := input.LeaseRef
	if identity == "" {
		identity = input.RuntimeRevisionRef + "|" + input.RuntimeRevisionDigest + "|" + input.WorkloadInstance
	}
	return "runtime-projection-" + shortHash(identity)
}

// runtimeProjectionData encodes validated runtime/workspace input, canonical attachments and identity/digest-bound skill, memory, MCP, callback and result views; it stores the provider digest but no provider-authentication bytes.
// runtimeProjectionData кодирует проверенный вход runtime и workspace, канонические вложения и связанные с идентичностью и хешами представления навыков, памяти, MCP, callback и результатов; хранит хеш провайдера, но не байты его авторизации.
func runtimeProjectionData(input runtimecontract.RunnerInput) (map[string]string, error) {
	runtimeRaw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		return nil, err
	}
	workspaceRaw, err := json.Marshal(input.WorkspacePolicy)
	if err != nil {
		return nil, errors.New("encode runtime workspace policy")
	}
	inputs, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return nil, errors.New("encode runtime input projection")
	}
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil {
		return nil, err
	}
	identity := map[string]any{
		"organization_ref": input.OrganizationRef, "project_ref": input.ProjectRef, "run_ref": input.RunRef, "node_ref": input.NodeRef,
		"session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": input.Attempt,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_version": input.RuntimeRevisionVersion,
		"runtime_revision_digest": input.RuntimeRevisionDigest, "input_digest": input.InputDigest,
	}
	encode := func(value any, diagnostic string) (string, error) {
		raw, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return "", errors.New(diagnostic)
		}
		return string(raw), nil
	}
	skills, err := encode(map[string]any{"identity": identity, "context_digest": snapshot.Digest, "skills": snapshot.Skills}, "encode runtime skill projection")
	if err != nil {
		return nil, err
	}
	memory, err := encode(map[string]any{"identity": identity, "context_digest": snapshot.Digest, "memories": snapshot.Memories}, "encode runtime memory projection")
	if err != nil {
		return nil, err
	}
	mcp, err := encode(map[string]any{"identity": identity, "binding_digest": input.MCPBindingDigest, "capabilities": input.Capabilities, "grants": input.IntegrationGrants}, "encode runtime MCP projection")
	if err != nil {
		return nil, err
	}
	callback, err := encode(map[string]any{"identity": identity, "binding_digest": input.ExecutionBindingDigest, "url": input.CallbackURL, "tls_server_name": input.CallbackTLS.ServerName}, "encode runtime callback projection")
	if err != nil {
		return nil, err
	}
	results, err := encode(map[string]any{"identity": identity, "root": input.OutboxRoot, "maximum_writable_bytes": input.WorkspacePolicy.MaximumWritableBytes, "maximum_file_count": input.WorkspacePolicy.MaximumFileCount}, "encode runtime result projection")
	if err != nil {
		return nil, err
	}
	return map[string]string{
		inputKey: string(runtimeRaw), workspacePolicyKey: string(workspaceRaw), inputManifestKey: string(inputs.Bytes),
		resultManifestKey: results, skillManifestKey: skills, memoryManifestKey: memory,
		mcpManifestKey: mcp, callbackManifestKey: callback, providerDigestKey: input.ProviderCredentialSHA256,
	}, nil
}

// runtimeProjectionAnnotations derives required revision/configuration/environment/execution/policy bindings and shortened organization/project/session/turn identities for later immutable readback.
// runtimeProjectionAnnotations выводит обязательные привязки ревизии, конфигурации, окружения, исполнения и политик и сокращённые идентификаторы организации, проекта, сессии и хода для дальнейшей проверки неизменяемости.
func runtimeProjectionAnnotations(input runtimecontract.RunnerInput, podName string) map[string]string {
	return map[string]string{
		revisionAnnotation: input.RuntimeRevisionDigest, configAnnotation: input.RuntimeConfigDigest,
		environmentAnnotation: input.RuntimeEnvironmentDigest, workspacePolicyAnnotation: input.WorkspacePolicy.Digest,
		executionBindingAnnotation: input.ExecutionBindingDigest, mcpBindingAnnotation: input.MCPBindingDigest,
		controllerAnnotation: input.WorkloadInstance, podAnnotation: podName,
		resourcesAnnotation: input.EnvironmentPolicy.ResourcesDigest, volumesAnnotation: input.EnvironmentPolicy.VolumesDigest,
		networkAnnotation: input.EnvironmentPolicy.NetworkDigest, rbacProfileAnnotation: input.EnvironmentPolicy.RBACDigest,
		effectiveRBACAnnotation:    input.EffectiveKubernetesAccess.Digest,
		organizationHashAnnotation: shortHash(input.OrganizationRef), projectHashAnnotation: shortHash(input.ProjectRef), sessionHashAnnotation: shortHash(input.SessionRef),
		turnHashAnnotation: shortHash(input.TurnRef), attemptAnnotation: strconv.FormatInt(int64(input.Attempt), 10),
	}
}

// ensureProjection creates a size-bounded immutable ConfigMap or requires matching metadata, exact text data and no binary data on an existing one; conflicts are rejected rather than patched.
// ensureProjection создаёт ограниченную по размеру неизменяемую ConfigMap либо требует совпадения метаданных, точных текстовых данных и отсутствия бинарных данных существующей; конфликт отклоняется, не исправляется.
func (manager *Manager) ensureProjection(ctx context.Context, podName, mode string, input runtimecontract.RunnerInput) error {
	data, err := runtimeProjectionData(input)
	if err != nil {
		return err
	}
	if stringDataSize(data) > maximumKubernetesProjectionBytes {
		return errors.New("runtime revision projection exceeds Kubernetes object limit")
	}
	immutable := true
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: runtimeProjectionName(input), Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: runtimeProjectionAnnotations(input, podName)},
		Immutable: &immutable, Data: data}
	_, err = manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || existing.Immutable == nil || !*existing.Immutable || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) || !apiequality.Semantic.DeepEqual(existing.Data, desired.Data) || len(existing.BinaryData) != 0 {
			return errors.New("existing runtime projection conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create immutable runtime projection")
	}
	return nil
}

// ensureTicket encodes the input and token, copies environment values under the projection budget and creates an immutable Secret with provider pins; an existing Secret must pass exact ticket readback.
// ensureTicket кодирует вход и токен, копирует значения окружения в пределах бюджета проекции и создаёт неизменяемый Secret с привязками провайдера; существующий Secret обязан пройти точную сверку билета.
func (manager *Manager) ensureTicket(ctx context.Context, name, podName, mode string, input runtimecontract.RunnerInput, token string, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) error {
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		return err
	}
	immutable := true
	data := map[string][]byte{inputKey: raw, ticketKey: []byte(token)}
	for key, value := range environmentSecrets {
		data[key] = append([]byte(nil), value...)
	}
	if byteDataSize(data) > maximumKubernetesProjectionBytes {
		return errors.New("runtime execution ticket exceeds Kubernetes object limit")
	}
	annotations := runtimeProjectionAnnotations(input, podName)
	if providerBinding != nil {
		annotations[providerSecretNameAnnotation] = providerBinding.Name
		annotations[providerSecretUIDAnnotation] = providerBinding.UID
		annotations[providerSecretResourceVersionAnnotation] = providerBinding.ResourceVersion
		annotations[providerAccountRefAnnotation] = input.ProviderAccountRef
		annotations[providerCredentialRefAnnotation] = input.ProviderCredentialRef
		annotations[providerCredentialDigestAnnotation] = providerBinding.ContentSHA256
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: annotations},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data}
	_, err = manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil || !runtimeTicketMatches(existing, podName, mode, input, token, environmentSecrets, providerBinding) {
			return errors.New("existing runtime ticket conflicts with immutable execution")
		}
		return nil
	}
	if err != nil {
		return errors.New("create immutable runtime ticket")
	}
	return nil
}

// stringDataSize sums key and string-value byte lengths for the projection data budget, excluding serialized object metadata and encoding overhead.
// stringDataSize суммирует байтовые длины ключей и строковых значений для бюджета данных проекции, не включая метаданные объекта и накладные расходы кодирования.
func stringDataSize(data map[string]string) int {
	result := 0
	for key, value := range data {
		result += len(key) + len(value)
	}
	return result
}

// byteDataSize sums key and byte-slice lengths for the ticket data budget without copying or serializing private values.
// byteDataSize суммирует длины ключей и массивов байтов для бюджета данных билета без копирования или сериализации закрытых значений.
func byteDataSize(data map[string][]byte) int {
	result := 0
	for key, value := range data {
		result += len(key) + len(value)
	}
	return result
}

// runtimeTicketMatches checks immutable metadata, provider pins, canonical input, exact data cardinality and valid token format; only warm-turn reuse requires the supplied token to match, while other modes retain their stored random token.
// runtimeTicketMatches проверяет неизменяемые метаданные, привязки провайдера, канонический вход, точное число записей и формат токена; совпадение переданного токена требуется только warm-turn, остальные режимы сохраняют свой случайный токен.
func runtimeTicketMatches(existing *corev1.Secret, podName, mode string, input runtimecontract.RunnerInput, token string, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) bool {
	expectedMetadata := metav1.ObjectMeta{Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: runtimeProjectionAnnotations(input, podName)}
	if existing == nil || existing.Immutable == nil || !*existing.Immutable || !managedMetadataMatches(existing.ObjectMeta, expectedMetadata) ||
		existing.Labels[managedLabel] != "true" || existing.Labels[modeLabel] != mode ||
		existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
		existing.Annotations[configAnnotation] != input.RuntimeConfigDigest ||
		existing.Annotations[environmentAnnotation] != input.RuntimeEnvironmentDigest ||
		existing.Annotations[controllerAnnotation] != input.WorkloadInstance ||
		existing.Annotations[podAnnotation] != podName ||
		existing.Annotations[resourcesAnnotation] != input.EnvironmentPolicy.ResourcesDigest ||
		existing.Annotations[volumesAnnotation] != input.EnvironmentPolicy.VolumesDigest ||
		existing.Annotations[networkAnnotation] != input.EnvironmentPolicy.NetworkDigest ||
		existing.Annotations[rbacProfileAnnotation] != input.EnvironmentPolicy.RBACDigest ||
		existing.Annotations[effectiveRBACAnnotation] != input.EffectiveKubernetesAccess.Digest ||
		!providerTicketBindingMatches(existing, input, providerBinding) ||
		len(existing.Data) != len(environmentSecrets)+2 {
		return false
	}
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil || subtle.ConstantTimeCompare(existing.Data[inputKey], raw) != 1 {
		return false
	}
	if len(existing.Data[ticketKey]) != 64 {
		return false
	}
	if _, err := hex.DecodeString(string(existing.Data[ticketKey])); err != nil {
		return false
	}
	if mode == "warm-turn" && subtle.ConstantTimeCompare(existing.Data[ticketKey], []byte(token)) != 1 {
		return false
	}
	for key, value := range environmentSecrets {
		if subtle.ConstantTimeCompare(existing.Data[key], value) != 1 {
			return false
		}
	}
	return true
}

// ensureSessionPVC creates or verifies an exact organization/project/session-bound RWO volume and configured size/class, allowing only the explicit assistant project-binding promotion; create races require fresh matching readback.
// ensureSessionPVC создаёт либо проверяет точный RWO-том организации, проекта и сессии с заданными размером и классом, допуская только явный перевод привязки проекта помощника; гонка создания требует свежей совпадающей сверки.
func (manager *Manager) ensureSessionPVC(ctx context.Context, input runtimecontract.RunnerInput) error {
	volumeLabels, volumeAnnotations, err := runtimecontract.SessionVolumeMetadata(input.OrganizationRef, input.ProjectRef, input.SessionRef)
	if err != nil {
		return err
	}
	name, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		return err
	}
	var storageClassName *string
	if manager.config.StorageClass != "" {
		storageClassName = &manager.config.StorageClass
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.RuntimeNamespace,
		Labels:      volumeLabels,
		Annotations: volumeAnnotations},
		Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: storageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	existing, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if sessionPVCMatches(existing, pvc, manager.config.StorageClass) {
			return nil
		}
		if !assistantSessionPVCNeedsPromotion(existing, pvc, input, manager.config.StorageClass) {
			return errors.New("existing runtime session volume conflicts with exact session binding")
		}
		if err := manager.promoteAssistantSessionPVC(ctx, existing, pvc); err != nil {
			return err
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime session volume")
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Create(ctx, pvc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// Победитель гонки проходит ту же проверку scope до создания Pod.
		existing, readErr := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
		if readErr != nil || !sessionPVCMatches(existing, pvc, manager.config.StorageClass) {
			return errors.New("existing runtime session volume conflicts with exact session binding")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime session volume")
	}
	return nil
}

// assistantSessionPVCNeedsPromotion recognizes only a system-assistant input with a project and an otherwise matching volume still bound to the empty project hash; it does not perform the promotion.
// assistantSessionPVCNeedsPromotion распознаёт только вход системного помощника с проектом и иначе совпадающий том, ещё связанный с хешем пустого проекта; сам перевод не выполняет.
func assistantSessionPVCNeedsPromotion(existing, desired *corev1.PersistentVolumeClaim, input runtimecontract.RunnerInput, storageClass string) bool {
	if !input.IsSystemAssistant() || input.ProjectRef == "" || existing == nil || desired == nil {
		return false
	}
	global := desired.DeepCopy()
	global.Annotations[projectHashAnnotation] = shortHash("")
	return sessionPVCMatches(existing, global, storageClass)
}

// promoteAssistantSessionPVC rejects active incompatible consumers, deletes only eligible terminating Pods with UID preconditions and bounded disappearance waits, then updates the project annotation and verifies the PVC readback.
// promoteAssistantSessionPVC отклоняет активных несовместимых потребителей, удаляет только подходящие завершаемые Pod с условием UID и ограниченным ожиданием исчезновения, затем меняет аннотацию проекта и сверяет PVC.
func (manager *Manager) promoteAssistantSessionPVC(ctx context.Context, existing, desired *corev1.PersistentVolumeClaim) error {
	pods, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return errors.New("inspect assistant session volume consumers")
	}
	draining := make([]corev1.Pod, 0)
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == desired.Name {
				if !assistantSessionConsumerCanDrain(pod, desired) {
					return errors.New("assistant session volume still has an active consumer")
				}
				draining = append(draining, pod)
				break
			}
		}
	}
	for _, pod := range draining {
		grace := int64(0)
		uid := pod.UID
		if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: &grace,
			Preconditions:      &metav1.Preconditions{UID: &uid},
		}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("drain terminating assistant session consumer")
		}
		if err := manager.waitForDeletedSessionConsumer(ctx, pod.Name, uid); err != nil {
			return err
		}
	}
	updated := existing.DeepCopy()
	updated.Annotations[projectHashAnnotation] = desired.Annotations[projectHashAnnotation]
	if _, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		return errors.New("promote assistant session volume project binding")
	}
	readback, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil || !sessionPVCMatches(readback, desired, manager.config.StorageClass) {
		return errors.New("assistant session volume project binding readback failed")
	}
	return nil
}

// assistantSessionConsumerCanDrain requires a terminating managed turn Pod with an exact organization/session binding, empty-project hash, UID and lease reference before promotion may drain it.
// assistantSessionConsumerCanDrain требует завершаемый управляемый Pod хода с точной организацией и сессией, хешем пустого проекта, UID и ссылкой аренды, прежде чем перевод сможет его остановить.
func assistantSessionConsumerCanDrain(pod corev1.Pod, desired *corev1.PersistentVolumeClaim) bool {
	return desired != nil && pod.DeletionTimestamp != nil && pod.UID != "" &&
		pod.Labels[managedLabel] == "true" && pod.Labels[modeLabel] == "turn" &&
		pod.Annotations[organizationHashAnnotation] == desired.Annotations[organizationHashAnnotation] &&
		pod.Annotations[projectHashAnnotation] == shortHash("") &&
		pod.Annotations[sessionHashAnnotation] == desired.Labels[sessionHashAnnotation] &&
		pod.Annotations[leaseAnnotation] != ""
}

// waitForDeletedSessionConsumer polls within the caller and drain budgets until NotFound for the named Pod, rejecting API errors, UID replacement and timeout instead of assuming deletion completed.
// waitForDeletedSessionConsumer опрашивает в пределах сроков вызывающего кода и drain до NotFound именованного Pod, отклоняя ошибки API, замену UID и таймаут вместо предположения о завершённом удалении.
func (manager *Manager) waitForDeletedSessionConsumer(ctx context.Context, name string, uid types.UID) error {
	err := wait.PollUntilContextTimeout(ctx, assistantSessionDrainPollInterval, assistantSessionDrainTimeout, true,
		func(ctx context.Context) (bool, error) {
			pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, errors.New("read terminating assistant session consumer")
			}
			if pod.UID != uid {
				return false, errors.New("terminating assistant session consumer identity changed")
			}
			return false, nil
		})
	if err != nil {
		return errors.New("assistant session consumer did not terminate before promotion")
	}
	return nil
}

// sessionPVCMatches checks required scope metadata, access modes, filesystem form, requested capacity and any explicit storage class, rejecting deletion/data-source/selector state; it does not assert storage health.
// sessionPVCMatches проверяет обязательные метаданные области, режим доступа, файловую форму, ёмкость и явно заданный класс, отклоняя удаление, источники данных и selector; работоспособность хранилища не подтверждает.
func sessionPVCMatches(existing, desired *corev1.PersistentVolumeClaim, storageClass string) bool {
	if existing == nil || existing.DeletionTimestamp != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
		!apiequality.Semantic.DeepEqual(existing.Spec.AccessModes, desired.Spec.AccessModes) ||
		existing.Spec.Selector != nil || existing.Spec.DataSource != nil || existing.Spec.DataSourceRef != nil ||
		existing.Spec.VolumeMode != nil && *existing.Spec.VolumeMode != corev1.PersistentVolumeFilesystem ||
		storageClass != "" && (existing.Spec.StorageClassName == nil || *existing.Spec.StorageClassName != storageClass) {
		return false
	}
	actual := existing.Spec.Resources.Requests[corev1.ResourceStorage]
	expected := desired.Spec.Resources.Requests[corev1.ResourceStorage]
	return actual.Cmp(expected) == 0
}

// runtimePod builds the configured multi-container Pod with separate role/provider/relay UIDs and mounts, read-only input views and signed proxy routing; it returns nil on egress-grant failure and does not submit the Pod or prove node enforcement.
// runtimePod строит заданный многоконтейнерный Pod с отдельными UID и подключениями роли, провайдера и relay, входами только для чтения и подписанным маршрутом прокси; при ошибке выходного гранта возвращает nil, Pod не публикует и исполнение правил узлом не доказывает.
func (manager *Manager) runtimePod(input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding, credentials *CredentialProjection, ticketSecret, podName, mode string) *corev1.Pod {
	providerProxy, err := manager.runtimeProxyURL(input)
	if err != nil {
		return nil
	}
	roleArgs := []string{"runtime-session"}
	if mode == "warm" {
		roleArgs = []string{"runtime-warm"}
	}
	sessionVolumeName, _ := runtimecontract.SessionPVCName(input.SessionRef)
	workspaceLimit := *resource.NewQuantity(input.WorkspacePolicy.MaximumWritableBytes, resource.BinarySI)
	providerSecretName, providerAuthKey, runtimeSecretName := providerBinding.Name, "auth.json", ticketSecret
	runtimeSecretKeys := make(map[string]string, len(input.SecretProjections))
	for _, item := range input.SecretProjections {
		runtimeSecretKeys[item.Name] = environmentProjectionKey(item.Name)
	}
	if credentials != nil {
		providerSecretName, providerAuthKey, runtimeSecretName = credentials.SecretName, credentials.ProviderAuthKey, credentials.SecretName
		runtimeSecretKeys = credentials.RuntimeSecretKeys
	}
	volumes := []corev1.Volume{
		{Name: "session", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sessionVolumeName}}},
		{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "vfs-input", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "vfs-knowledge", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "runtime-context", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("520Mi"))}}},
		{Name: "runtime-input", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: runtimeProjectionName(input)}, DefaultMode: int32Pointer(0o440)}}},
		{Name: "runtime-ticket", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ticketSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientCASecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-client", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientTLSSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "provider-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: providerSecretName, DefaultMode: int32Pointer(0o400)}}},
		{Name: "runtime-egress-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.RuntimeEgressCASecret, DefaultMode: int32Pointer(0o440), Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}}},
		{Name: "runtime-egress-trust", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("2Mi"))}}},
		{Name: "provider-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "provider-credential-relay", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
		{Name: "provider-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
	}
	materializerWorkspaceMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "session", MountPath: "/workspace/.kodex/state"},
		{Name: "vfs-input", MountPath: "/workspace/input"},
		{Name: "vfs-knowledge", MountPath: "/workspace/knowledge"},
		{Name: "runtime-context", MountPath: runtimecontract.RuntimeContextRoot},
	}
	sandboxWorkspaceMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "session", MountPath: "/workspace/.kodex/state"},
		{Name: "vfs-input", MountPath: "/workspace/input", ReadOnly: true},
		{Name: "vfs-knowledge", MountPath: "/workspace/knowledge", ReadOnly: true},
		{Name: "runtime-context", MountPath: runtimecontract.RuntimeContextRoot, ReadOnly: true},
	}
	roleMounts := append([]corev1.VolumeMount(nil), sandboxWorkspaceMounts...)
	roleMounts = append(roleMounts, corev1.VolumeMount{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true}, corev1.VolumeMount{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true}, corev1.VolumeMount{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true}, corev1.VolumeMount{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true}, corev1.VolumeMount{Name: "provider-socket", MountPath: "/run/kodex/provider"}, corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"})
	initMounts := append([]corev1.VolumeMount(nil), materializerWorkspaceMounts...)
	initMounts = append(initMounts,
		corev1.VolumeMount{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
		corev1.VolumeMount{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true},
		corev1.VolumeMount{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true},
		corev1.VolumeMount{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true},
		corev1.VolumeMount{Name: "runtime-egress-ca", MountPath: "/var/run/config/kodex/runtime/egress-ca", ReadOnly: true},
		corev1.VolumeMount{Name: "runtime-egress-trust", MountPath: "/var/run/config/kodex/runtime/egress-trust"},
		corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"},
	)
	environmentMounts := make([]corev1.VolumeMount, 0, len(input.EnvironmentPolicy.Volumes))
	for _, item := range input.EnvironmentPolicy.Volumes {
		volumeName := "environment-" + shortHash(item.Name)
		source := &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(*resource.NewQuantity(item.SizeMiB<<20, resource.BinarySI))}
		if item.Kind == runtimecontract.RuntimeVolumeEphemeralMemory {
			source.Medium = corev1.StorageMediumMemory
		}
		volumes = append(volumes, corev1.Volume{Name: volumeName, VolumeSource: corev1.VolumeSource{EmptyDir: source}})
		environmentMounts = append(environmentMounts, corev1.VolumeMount{Name: volumeName, MountPath: item.MountPath})
	}
	roleMounts = append(roleMounts, environmentMounts...)
	// Инициализатор выполняет ту же canary объявленных томов до запуска provider.
	initMounts = append(initMounts, environmentMounts...)
	serviceAccountName := manager.config.RunnerServiceAccount
	if mode == "turn" {
		serviceAccountName = input.EffectiveKubernetesAccess.ServiceAccountName
	}
	resources := runtimePolicyResourceRequirements(input.EnvironmentPolicy.Resources)
	role := corev1.Container{Name: "role-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: roleArgs,
		WorkingDir: runtimecontract.RuntimeWorkspaceWorkingRoot,
		Env: []corev1.EnvVar{
			{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"},
			{Name: "OTEL_SDK_DISABLED", Value: "true"},
			{Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment},
		},
		Ports: []corev1.ContainerPort{{Name: "runtime-health", ContainerPort: 9090}}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: roleMounts,
		Resources:    smallResources(),
		StartupProbe: httpProbe("/readyz", "runtime-health", 2, 60), ReadinessProbe: httpProbe("/readyz", "runtime-health", 5, 3), LivenessProbe: httpProbe("/healthz", "runtime-health", 10, 3)}
	provider := corev1.Container{Name: "provider-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-provider"},
		WorkingDir: runtimecontract.RuntimeWorkspaceWorkingRoot,
		Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "CODEX_HOME", Value: input.CodexHome},
			{Name: "HTTPS_PROXY", Value: providerProxy}, {Name: "HTTP_PROXY", Value: providerProxy},
			{Name: "SSL_CERT_FILE", Value: "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"},
			{Name: "CURL_CA_BUNDLE", Value: "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"},
			{Name: "REQUESTS_CA_BUNDLE", Value: "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"},
			{Name: "NODE_EXTRA_CA_CERTS", Value: "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"},
			{Name: "GIT_SSL_CAINFO", Value: "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"},
			{Name: "NO_PROXY", Value: "127.0.0.1,localhost"}, {Name: "OTEL_SDK_DISABLED", Value: "true"}, {Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment}}, SecurityContext: providerSandboxSecurityContext(10002, manager.config.ProviderAppArmorProfile),
		VolumeMounts: append(append([]corev1.VolumeMount(nil), sandboxWorkspaceMounts...), []corev1.VolumeMount{
			{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
			{Name: "provider-auth", MountPath: "/run/secrets/kodex/runtime/provider/auth.json", SubPath: providerAuthKey, ReadOnly: true},
			{Name: "runtime-input", MountPath: "/run/secrets/kodex/runtime/provider/auth.sha256", SubPath: providerDigestKey, ReadOnly: true},
			{Name: "provider-socket", MountPath: "/run/kodex/provider"},
			{Name: "provider-credential-relay", MountPath: "/run/kodex/provider-credential-relay"},
			{Name: "provider-tmp", MountPath: "/tmp"},
			{Name: "runtime-egress-trust", MountPath: "/var/run/config/kodex/runtime/egress-trust", ReadOnly: true},
		}...),
		Resources: resources, ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/kodex/provider/provider.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}
	provider.VolumeMounts = append(provider.VolumeMounts, environmentMounts...)
	relay := corev1.Container{Name: "provider-credential-relay", Image: manager.config.DefaultRoleImageReference, ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{"runtime-provider-credential-relay"},
		Env: []corev1.EnvVar{
			{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"},
			{Name: "OTEL_SDK_DISABLED", Value: "true"},
			{Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment},
		},
		SecurityContext: restrictedSecurityContext(10003),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
			{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true},
			{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true},
			{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true},
			{Name: "provider-credential-relay", MountPath: "/run/kodex/provider-credential-relay"},
		},
		Resources: smallResources(), ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/kodex/provider-credential-relay/relay.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}
	for _, item := range input.EnvironmentValues {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	for _, item := range input.SecretProjections {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: runtimeSecretName}, Key: runtimeSecretKeys[item.Name], Optional: boolPointer(false),
		}}})
	}
	metadata := manager.runtimePodMetadata(input, podName, mode)
	if mode == "turn" && credentials != nil {
		metadata.Annotations[credentialProjectionNameAnnotation] = credentials.SecretName
		metadata.Annotations[credentialProjectionUIDAnnotation] = credentials.SecretUID
		metadata.Annotations[credentialProjectionVersionAnnotation] = credentials.SecretResourceVersion
		metadata.Annotations[credentialProjectionDigestAnnotation] = credentials.ContentSHA256
	}
	pod := &corev1.Pod{ObjectMeta: metadata,
		Spec: corev1.PodSpec{ServiceAccountName: serviceAccountName, AutomountServiceAccountToken: boolPointer(false), EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: int64Pointer(150),
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(29000), FSGroupChangePolicy: fsGroupChangePolicyPointer(corev1.FSGroupChangeOnRootMismatch), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			InitContainers: []corev1.Container{
				{Name: "workspace-prepare", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-prepare-workspace"}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}}, Resources: smallResources()},
				{Name: "workspace-init", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-init-workspace"}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: initMounts, Resources: smallResources()},
			},
			Containers: []corev1.Container{role, provider, relay}, Volumes: volumes}}
	if manager.config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		pod.Spec.Volumes = slices.DeleteFunc(pod.Spec.Volumes, func(volume corev1.Volume) bool {
			return volume.Name == "callback-ca" || volume.Name == "callback-client"
		})
		for _, group := range [][]corev1.Container{pod.Spec.Containers, pod.Spec.InitContainers} {
			for index := range group {
				group[index].VolumeMounts = slices.DeleteFunc(group[index].VolumeMounts, func(mount corev1.VolumeMount) bool {
					return mount.Name == "callback-ca" || mount.Name == "callback-client"
				})
			}
		}
	}
	return pod
}

// runtimePodMetadata shares server-derived execution metadata between creation and
// observation. It contains no credentials or UID history and grants no authority.
// runtimePodMetadata объединяет серверные метаданные исполнения для создания и
// наблюдения. Она не содержит реквизитов либо истории UID и не даёт полномочий.
func (manager *Manager) runtimePodMetadata(input runtimecontract.RunnerInput, podName, mode string) metav1.ObjectMeta {
	annotations := runtimeProjectionAnnotations(input, podName)
	labels := map[string]string{managedLabel: "true", modeLabel: mode, "app.kubernetes.io/name": "agent-runner", "app.kubernetes.io/component": "role-runtime", "kodex.dev/environment": manager.config.Environment}
	if mode == "turn" {
		annotations[leaseAnnotation] = input.LeaseRef
		labels[executionHashLabel] = shortHash(input.LeaseRef)
	}
	if mode == "warm" {
		compatibility, _ := runtimecontract.WarmCompatibilityDigest(input)
		annotations[warmCompatibilityAnnotation] = compatibility
	}
	if manager.config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		labels["kodex.dev/security-profile"] = runtimecontract.CallbackProfileTrustedCluster
	}
	return metav1.ObjectMeta{Name: podName, Namespace: manager.config.RuntimeNamespace, Labels: labels, Annotations: annotations}
}

// runtimeProxyURL signs a web-access grant bound to the lease or warm revision/session identity and policy digest, embedding it as proxy credentials; the returned URL is private and must not be logged.
// runtimeProxyURL подписывает web-грант, связанный с арендой либо идентичностью warm-ревизии и сессии и хешем политики, помещая его в реквизиты прокси; возвращённый URL закрытый, логировать его нельзя.
func (manager *Manager) runtimeProxyURL(input runtimecontract.RunnerInput) (string, error) {
	workloadRef := input.LeaseRef
	if workloadRef == "" && input.Mode == runtimecontract.RunnerModeWarm {
		workloadRef = "warm:" + input.OrganizationRef + ":" + input.SessionRef + ":" + input.RuntimeRevisionRef + ":" + input.RuntimeRevisionDigest
	}
	grant, err := runtimecontract.SignRuntimeWebAccessGrant(manager.config.RuntimeEgressSigningKey, workloadRef, input.EnvironmentPolicy.NetworkDigest, input.EnvironmentPolicy.Network.WebAccess)
	if err != nil {
		return "", errors.New("sign runtime egress grant")
	}
	proxy, err := url.Parse(manager.config.ProviderHTTPSProxy)
	if err != nil {
		return "", errors.New("parse runtime egress proxy")
	}
	proxy.User = url.UserPassword("kodex", grant)
	return proxy.String(), nil
}

// runtimePolicyResourceRequirements maps milli-CPU and MiB policy values into Kubernetes request/limit quantities without choosing defaults or validating the policy itself.
// runtimePolicyResourceRequirements переносит значения политики в милли-CPU и МиБ в Kubernetes requests и limits, не выбирая значения по умолчанию и не проверяя саму политику.
func runtimePolicyResourceRequirements(policy runtimecontract.RuntimeResourcePolicy) corev1.ResourceRequirements {
	requests := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(policy.CPURequestMilli, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(policy.MemoryRequestMiB<<20, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(policy.EphemeralStorageRequestMiB<<20, resource.BinarySI),
	}
	limits := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(policy.CPULimitMilli, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(policy.MemoryLimitMiB<<20, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(policy.EphemeralStorageLimitMiB<<20, resource.BinarySI),
	}
	return corev1.ResourceRequirements{Requests: requests, Limits: limits}
}

// validateImage requires the exact promoted-repository or pinned-default reference/digest combination and matching runtime-contract revision/hash; it does not independently verify registry signatures or admission history.
// validateImage требует точную комбинацию ссылки и хеша допущенного репозитория либо закреплённого базового образа и совпадение ревизии и хеша runtime-контракта; подписи registry и историю допуска самостоятельно не проверяет.
func (manager *Manager) validateImage(input runtimecontract.RunnerInput) error {
	promoted := input.ImageReference == manager.config.PromotedRoleImageRepository+"@"+input.ImageManifestDigest
	defaultPinned := input.ImageReference == manager.config.DefaultRoleImageReference &&
		strings.HasSuffix(manager.config.DefaultRoleImageReference, "@"+input.ImageManifestDigest)
	if (!promoted && !defaultPinned) ||
		input.RoleRuntimeContractRevision != manager.config.RoleRuntimeContractRevision ||
		input.RoleRuntimeContractSHA256 != manager.config.RoleRuntimeContractSHA256 {
		return errors.New("runtime role image is outside promoted policy")
	}
	return nil
}

// validPinnedImageReference checks an exact lowercase sixty-four-digit SHA-256 suffix and rejects interpolation/additional-at characters in the repository portion; repository admission is a separate check.
// validPinnedImageReference проверяет точный суффикс SHA-256 из шестидесяти четырёх строчных hex-цифр и запрещает подстановки и дополнительную собаку в репозитории; допуск репозитория проверяется отдельно.
func validPinnedImageReference(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	if separator <= 0 || separator+len("@sha256:")+64 != len(reference) {
		return false
	}
	for _, character := range reference[separator+len("@sha256:"):] {
		if (character < 'a' || character > 'f') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return !strings.ContainsAny(reference[:separator], "@${}")
}

// validateProviderSecret delegates exact Secret identity/content verification to the authentication reader and returns only its error; it does not switch the selected credential revision.
// validateProviderSecret делегирует точную проверку идентичности и содержимого Secret читателю авторизации, возвращая только ошибку; выбранную ревизию учётных данных не переключает.
func (manager *Manager) validateProviderSecret(ctx context.Context, input runtimecontract.RunnerInput, binding ProviderSecretBinding) error {
	_, err := manager.readProviderAuthentication(ctx, input, binding)
	return err
}

// readProviderAuthentication requires the exact immutable Secret UID/resource version, bounds authentication bytes and verifies both digest file and expected SHA-256, returning an independent private byte copy.
// readProviderAuthentication требует точные UID и resourceVersion неизменяемого Secret, ограничивает байты авторизации и сверяет файл хеша и ожидаемый SHA-256, возвращая отдельную закрытую копию байтов.
func (manager *Manager) readProviderAuthentication(ctx context.Context, input runtimecontract.RunnerInput, binding ProviderSecretBinding) ([]byte, error) {
	if !validDNSLabel(binding.Name) || binding.UID == "" || binding.ResourceVersion == "" ||
		binding.ContentSHA256 != input.ProviderCredentialSHA256 {
		return nil, errors.New("provider credential binding is invalid")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != binding.UID ||
		secret.ResourceVersion != binding.ResourceVersion {
		return nil, errors.New("provider credential revision is unavailable")
	}
	authentication := secret.Data["auth.json"]
	digestFile := strings.TrimSpace(string(secret.Data["auth.sha256"]))
	digest := sha256.Sum256(authentication)
	actual := hex.EncodeToString(digest[:])
	if len(authentication) == 0 || len(authentication) > 1<<20 || digestFile != actual || actual != binding.ContentSHA256 {
		return nil, errors.New("provider credential revision digest is invalid")
	}
	return append([]byte(nil), authentication...), nil
}

// MaterializeProviderCredentialRefresh validates the current ticket and predecessor account/content lineage, creates a new immutable authentication Secret and verifies exact readback; atomic current-revision switching remains control-plane's responsibility.
// MaterializeProviderCredentialRefresh проверяет текущий билет и происхождение аккаунта и содержимого предшественника, создаёт новый неизменяемый Secret авторизации и сверяет точный результат; атомарное переключение текущей ревизии остаётся обязанностью control-plane.
func (manager *Manager) MaterializeProviderCredentialRefresh(ctx context.Context, input runtimecontract.RunnerInput, request runtimecontract.RunnerProviderCredentialRefreshRequest) (ProviderSecretBinding, error) {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || request.Validate() != nil ||
		request.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		request.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
		request.PreviousContentSHA256 != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, ErrProviderCredentialRefreshRejected
	}
	ticket, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read runtime provider credential binding")
	}
	oldBinding, err := providerSecretBindingFromTicket(ticket, input)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential binding is invalid", ErrProviderCredentialRefreshRejected)
	}
	oldAuthentication, err := manager.readProviderAuthentication(ctx, input, oldBinding)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential is unavailable", ErrProviderCredentialRefreshRejected)
	}
	defer clear(oldAuthentication)
	oldSnapshot, err := parseManagedChatGPTAuthentication(oldAuthentication)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential is not managed OAuth", ErrProviderCredentialRefreshRejected)
	}
	newSnapshot, err := parseManagedChatGPTAuthentication(request.Authentication)
	if err != nil || subtle.ConstantTimeCompare([]byte(oldSnapshot.Tokens.AccountID), []byte(newSnapshot.Tokens.AccountID)) != 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider account does not match", ErrProviderCredentialRefreshRejected)
	}
	digest := sha256.Sum256(request.Authentication)
	contentSHA256 := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(contentSHA256), []byte(input.ProviderCredentialSHA256)) == 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: provider credential did not change", ErrProviderCredentialRefreshRejected)
	}
	name := providerCredentialRefreshSecretName(input.ProviderAccountRef, contentSHA256)
	immutable := true
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: manager.config.RuntimeNamespace,
			Labels: map[string]string{managedLabel: "true", "app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/managed-by": "runtime-controller"},
			Annotations: map[string]string{
				providerAccountRefAnnotation:       input.ProviderAccountRef,
				previousCredentialRefAnnotation:    input.ProviderCredentialRef,
				previousCredentialDigestAnnotation: input.ProviderCredentialSHA256,
				providerCredentialDigestAnnotation: contentSHA256,
			},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"auth.json":   append([]byte(nil), request.Authentication...),
			"auth.sha256": []byte(contentSHA256),
		},
	}
	if _, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return ProviderSecretBinding{}, errors.New("create refreshed provider credential Secret")
	}
	materialized, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read back refreshed provider credential Secret")
	}
	return validateProviderCredentialRefreshReadback(materialized, desired, contentSHA256)
}

// parseManagedChatGPTAuthentication bounds one duplicate-free JSON document and requires trimmed nonempty managed-OAuth account/access/refresh fields; unrelated fields are not prohibited and returned token strings remain private.
// parseManagedChatGPTAuthentication ограничивает один JSON-документ без повторов ключей и требует непустые поля управляемого OAuth без пробелов по краям; посторонние поля не запрещает, возвращённые строки токенов остаются закрытыми.
func parseManagedChatGPTAuthentication(authentication []byte) (managedChatGPTAuthentication, error) {
	var snapshot managedChatGPTAuthentication
	var root map[string]json.RawMessage
	var tokens map[string]json.RawMessage
	if len(authentication) == 0 || len(authentication) > runtimecontract.MaximumProviderAuthBytes || rejectDuplicateJSONKeys(authentication) != nil ||
		json.Unmarshal(authentication, &root) != nil || json.Unmarshal(root["auth_mode"], &snapshot.AuthMode) != nil ||
		json.Unmarshal(root["tokens"], &tokens) != nil ||
		json.Unmarshal(tokens["account_id"], &snapshot.Tokens.AccountID) != nil ||
		json.Unmarshal(tokens["access_token"], &snapshot.Tokens.AccessToken) != nil ||
		json.Unmarshal(tokens["refresh_token"], &snapshot.Tokens.RefreshToken) != nil || snapshot.AuthMode != "chatgpt" ||
		snapshot.Tokens.AccountID == "" || len(snapshot.Tokens.AccountID) > 512 ||
		strings.TrimSpace(snapshot.Tokens.AccountID) != snapshot.Tokens.AccountID ||
		snapshot.Tokens.AccessToken == "" || strings.TrimSpace(snapshot.Tokens.AccessToken) != snapshot.Tokens.AccessToken ||
		snapshot.Tokens.RefreshToken == "" || strings.TrimSpace(snapshot.Tokens.RefreshToken) != snapshot.Tokens.RefreshToken {
		return managedChatGPTAuthentication{}, errors.New("managed ChatGPT authentication is invalid")
	}
	return snapshot, nil
}

// rejectDuplicateJSONKeys consumes exactly one JSON value with preserved number tokens, rejects duplicate keys recursively and requires EOF; it does not impose a separate nesting-depth budget.
// rejectDuplicateJSONKeys разбирает ровно одно JSON-значение с сохранением числовых токенов, рекурсивно отклоняет повтор ключей и требует EOF; отдельного бюджета глубины не задаёт.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("managed ChatGPT authentication has trailing JSON")
	}
	return nil
}

// consumeUniqueJSONValue recursively tracks each object's keys, traverses array elements and verifies matching closing delimiters, returning parser errors without logging private input.
// consumeUniqueJSONValue рекурсивно учитывает ключи каждого объекта, обходит элементы массивов и проверяет парные закрывающие разделители, возвращая ошибки разбора без логирования закрытого входа.
func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return errors.New("managed ChatGPT authentication object is invalid")
			}
			if _, duplicate := keys[key]; duplicate {
				return errors.New("managed ChatGPT authentication has duplicate keys")
			}
			keys[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("managed ChatGPT authentication JSON is invalid")
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	expected := json.Delim('}')
	if delimiter == '[' {
		expected = ']'
	}
	if closing != expected {
		return errors.New("managed ChatGPT authentication JSON is invalid")
	}
	return nil
}

// providerCredentialRefreshSecretName derives a bounded name from the account hash and first twelve digest characters; callers must supply a complete validated digest before slicing.
// providerCredentialRefreshSecretName выводит ограниченное имя из хеша аккаунта и первых двенадцати символов хеша содержимого; вызывающий код обязан передать полный проверенный хеш до среза.
func providerCredentialRefreshSecretName(accountRef, digest string) string {
	return "runtime-provider-refresh-" + shortHash(accountRef) + "-" + digest[:12]
}

// validateProviderCredentialRefreshReadback checks exact identity, immutable opaque form, managed labels, predecessor/account annotations, two data entries and authentication SHA-256 before returning fresh UID/resource-version pins.
// validateProviderCredentialRefreshReadback проверяет точную идентичность, неизменяемую opaque-форму, управляющие метки, аннотации предшественника и аккаунта, две записи данных и SHA-256 авторизации до возврата свежих UID и resourceVersion.
func validateProviderCredentialRefreshReadback(actual, desired *corev1.Secret, digest string) (ProviderSecretBinding, error) {
	if actual == nil || desired == nil || actual.Name != desired.Name || actual.Namespace != desired.Namespace ||
		actual.UID == "" || actual.ResourceVersion == "" || actual.Immutable == nil || !*actual.Immutable ||
		actual.Type != corev1.SecretTypeOpaque || len(actual.Data) != 2 ||
		actual.Labels[managedLabel] != "true" || actual.Labels["app.kubernetes.io/part-of"] != "kodex" ||
		actual.Labels["app.kubernetes.io/managed-by"] != "runtime-controller" ||
		actual.Annotations[providerAccountRefAnnotation] != desired.Annotations[providerAccountRefAnnotation] ||
		actual.Annotations[previousCredentialRefAnnotation] != desired.Annotations[previousCredentialRefAnnotation] ||
		actual.Annotations[previousCredentialDigestAnnotation] != desired.Annotations[previousCredentialDigestAnnotation] ||
		actual.Annotations[providerCredentialDigestAnnotation] != digest ||
		subtle.ConstantTimeCompare(actual.Data["auth.json"], desired.Data["auth.json"]) != 1 ||
		strings.TrimSpace(string(actual.Data["auth.sha256"])) != digest {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider credential readback does not match", ErrProviderCredentialRefreshRejected)
	}
	computed := sha256.Sum256(actual.Data["auth.json"])
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(computed[:])), []byte(digest)) != 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider credential digest does not match", ErrProviderCredentialRefreshRejected)
	}
	return ProviderSecretBinding{Name: actual.Name, UID: string(actual.UID), ResourceVersion: actual.ResourceVersion, ContentSHA256: digest}, nil
}

// providerSecretBindingFromTicket requires an immutable ticket with exact account/revision/content annotations and complete provider Secret metadata; it does not read or validate that Secret's actual bytes.
// providerSecretBindingFromTicket требует неизменяемый билет с точными аннотациями аккаунта, ревизии и содержимого и полными метаданными Secret провайдера; реальные байты этого Secret не читает и не проверяет.
func providerSecretBindingFromTicket(ticket *corev1.Secret, input runtimecontract.RunnerInput) (ProviderSecretBinding, error) {
	if ticket == nil || ticket.Immutable == nil || !*ticket.Immutable ||
		ticket.Annotations[providerAccountRefAnnotation] != input.ProviderAccountRef ||
		ticket.Annotations[providerCredentialRefAnnotation] != input.ProviderCredentialRef ||
		ticket.Annotations[providerCredentialDigestAnnotation] != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, errors.New("runtime provider credential ticket binding is invalid")
	}
	binding := ProviderSecretBinding{
		Name:            ticket.Annotations[providerSecretNameAnnotation],
		UID:             ticket.Annotations[providerSecretUIDAnnotation],
		ResourceVersion: ticket.Annotations[providerSecretResourceVersionAnnotation],
		ContentSHA256:   ticket.Annotations[providerCredentialDigestAnnotation],
	}
	if !validDNSLabel(binding.Name) || binding.UID == "" || binding.ResourceVersion == "" || len(binding.ContentSHA256) != sha256.Size*2 {
		return ProviderSecretBinding{}, errors.New("runtime provider credential ticket binding is incomplete")
	}
	return binding, nil
}

// providerTicketBindingMatches compares decoded ticket metadata to a supplied exact provider binding, while a nil expected binding deliberately skips this check.
// providerTicketBindingMatches сравнивает декодированные метаданные билета с переданной точной привязкой провайдера; отсутствие ожидаемой привязки намеренно пропускает эту проверку.
func providerTicketBindingMatches(ticket *corev1.Secret, input runtimecontract.RunnerInput, binding *ProviderSecretBinding) bool {
	if binding == nil {
		return true
	}
	actual, err := providerSecretBindingFromTicket(ticket, input)
	return err == nil && actual == *binding
}

// materializeEnvironmentSecrets verifies each exact immutable Secret revision and selected UTF-8/NUL-free value/digest under per-value and aggregate budgets, returning copied hashed-key values for trusted warm-ticket construction.
// materializeEnvironmentSecrets проверяет каждую точную неизменяемую ревизию Secret и выбранное значение UTF-8 без NUL с хешем в пределах индивидуального и общего бюджета, возвращая копии с хешированными ключами для доверенной сборки warm-билета.
func (manager *Manager) materializeEnvironmentSecrets(ctx context.Context, input runtimecontract.RunnerInput) (map[string][]byte, error) {
	result := make(map[string][]byte, len(input.SecretProjections))
	totalBytes := 0
	for _, item := range input.EnvironmentValues {
		totalBytes += len(item.Name) + len(item.Value)
	}
	for _, item := range input.SecretProjections {
		if !validDNSSubdomain(item.SecretName) {
			return nil, errors.New("runtime Secret projection is invalid")
		}
		secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, item.SecretName, metav1.GetOptions{})
		if err != nil || secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != item.SecretUID ||
			secret.ResourceVersion != item.SecretResourceVersion {
			return nil, errors.New("runtime Secret projection is unavailable")
		}
		value, present := secret.Data[item.SecretKey]
		digest := sha256.Sum256(value)
		if !present || len(value) > 8<<10 || !utf8.Valid(value) || bytesContainNUL(value) ||
			hex.EncodeToString(digest[:]) != item.ContentSHA256 {
			return nil, errors.New("runtime Secret projection digest is invalid")
		}
		totalBytes += len(item.Name) + len(value)
		result[environmentProjectionKey(item.Name)] = append([]byte(nil), value...)
	}
	if totalBytes > runtimecontract.MaximumRuntimeEnvironmentBytes {
		return nil, errors.New("runtime Secret projection byte limit exceeded")
	}
	return result, nil
}

// environmentProjectionMatches checks presence and SHA-256 of every expected environment projection entry without rejecting unrelated extra entries or performing Secret identity validation.
// environmentProjectionMatches проверяет наличие и SHA-256 каждой ожидаемой записи проекции окружения, не запрещая лишние записи и не проверяя идентичность Secret.
func environmentProjectionMatches(input runtimecontract.RunnerInput, data map[string][]byte) bool {
	for _, item := range input.SecretProjections {
		value, present := data[environmentProjectionKey(item.Name)]
		digest := sha256.Sum256(value)
		if !present || hex.EncodeToString(digest[:]) != item.ContentSHA256 {
			return false
		}
	}
	return true
}

// environmentProjectionKey derives a bounded private-ticket key from the shortened environment-variable name hash without carrying its value.
// environmentProjectionKey выводит ограниченный ключ закрытого билета из сокращённого хеша имени переменной окружения без её значения.
func environmentProjectionKey(name string) string {
	return "environment-" + shortHash(name)
}

// bytesContainNUL detects any zero byte in a projected environment value without interpreting or exposing its remaining content.
// bytesContainNUL обнаруживает нулевой байт в проецируемом значении окружения без интерпретации или раскрытия остального содержимого.
func bytesContainNUL(value []byte) bool {
	return bytes.IndexByte(value, 0) >= 0
}

// podReady requires an explicit true PodReady condition and treats a missing Pod or condition as not ready, without checking execution completion.
// podReady требует явное истинное условие PodReady и считает отсутствие Pod либо условия неготовностью, не проверяя завершение исполнения.
func podReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// runtimePodTerminal treats nil, failed/succeeded phases or a terminated selected runtime container as terminal; without a selected-name filter any terminated ordinary container qualifies.
// runtimePodTerminal считает terminal отсутствие Pod, фазы failed и succeeded либо завершение выбранного runtime-контейнера; без фильтра имён учитывает любой завершённый обычный контейнер.
func runtimePodTerminal(pod *corev1.Pod, requiredContainers ...string) bool {
	if pod == nil || pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		return true
	}
	required := make(map[string]struct{}, len(requiredContainers))
	for _, name := range requiredContainers {
		required[name] = struct{}{}
	}
	for _, status := range pod.Status.ContainerStatuses {
		_, requiredContainer := required[status.Name]
		if status.State.Terminated != nil && (len(required) == 0 || requiredContainer) {
			return true
		}
	}
	return false
}

// newTicket reads thirty-two cryptographically random bytes and returns their sixty-four-character hex encoding, failing without exposing partial entropy.
// newTicket читает тридцать два криптографически случайных байта и возвращает hex-строку из шестидесяти четырёх символов, при ошибке не раскрывая частичную энтропию.
func newTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate execution ticket")
	}
	return hex.EncodeToString(raw), nil
}

// shortHash returns the first eight SHA-256 bytes as sixteen lowercase hex characters for bounded names and metadata, not collision-free identity or bearer authority.
// shortHash возвращает первые восемь байтов SHA-256 как шестнадцать строчных hex-символов для ограниченных имён и метаданных, не как безколлизионную идентичность или bearer-полномочия.
func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

// warmTicketName binds a ticket name to the warm revision/digest and controller UID/IP tuple so a different controller instance does not silently reuse the same name.
// warmTicketName связывает имя билета с warm-ревизией, её хешем, UID и IP контроллера, чтобы другой экземпляр не переиспользовал то же имя неявно.
func (manager *Manager) warmTicketName(revisionRef, revisionDigest string) string {
	return ticketName("warm-" + revisionRef + "|" + revisionDigest + "|" + manager.config.ControllerPodUID + "|" + manager.config.ControllerPodIP)
}

// ticketName adds the fixed runtime-ticket prefix to a shortened identity hash, preserving the existing Kubernetes naming contract.
// ticketName добавляет закреплённый префикс runtime-ticket к сокращённому хешу идентичности, сохраняя существующий контракт имён Kubernetes.
func ticketName(value string) string { return "runtime-ticket-" + shortHash(value) }

// turnPodName delegates lease-derived turn Pod naming to the shared runtime contract rather than duplicating its naming algorithm.
// turnPodName делегирует построение имени Pod хода по аренде общему runtime-контракту вместо копирования алгоритма.
func turnPodName(value string) string { return runtimecontract.RuntimeTurnPodName(value) }

// int64Pointer returns an independent pointer to the supplied integer for explicit optional Kubernetes fields, preserving zero as distinct from nil.
// int64Pointer возвращает отдельный указатель на переданное целое для явных необязательных полей Kubernetes, отличая ноль от nil.
func int64Pointer(value int64) *int64 { return &value }

// int32Pointer preserves an explicit 32-bit field value when constructing pointer-valued Kubernetes API options.
// int32Pointer сохраняет явно заданное 32-битное значение при построении указательных параметров Kubernetes API.
func int32Pointer(value int32) *int32 { return &value }

// boolPointer returns a pointer to an explicit boolean so false is not confused with an omitted Kubernetes API field.
// boolPointer возвращает указатель на явное логическое значение, чтобы false не смешивалось с отсутствующим полем Kubernetes API.
func boolPointer(value bool) *bool { return &value }

// stringPointer returns an independent string pointer for optional Kubernetes fields without interpreting the string as a trusted path or identity.
// stringPointer возвращает отдельный строковый указатель для необязательных полей Kubernetes без признания строки доверенным путём или идентификатором.
func stringPointer(value string) *string { return &value }

// quantityPointer returns a pointer to the supplied quantity value for volume limits without reparsing, normalizing or choosing a default amount.
// quantityPointer возвращает указатель на переданное значение quantity для лимитов тома без повторного разбора, нормализации или выбора размера по умолчанию.
func quantityPointer(value resource.Quantity) *resource.Quantity { return &value }

// fsGroupChangePolicyPointer preserves an explicit Kubernetes filesystem-group policy value in an optional Pod security field.
// fsGroupChangePolicyPointer сохраняет явное значение политики файловой группы Kubernetes в необязательном поле безопасности Pod.
func fsGroupChangePolicyPointer(value corev1.PodFSGroupChangePolicy) *corev1.PodFSGroupChangePolicy {
	return &value
}

// validDNSLabel accepts one to sixty-three lowercase ASCII letters, digits or internal hyphens; uppercase, Unicode and leading/trailing hyphens are rejected without a DNS lookup.
// validDNSLabel принимает от одного до шестидесяти трёх строчных ASCII-букв, цифр либо внутренних дефисов; верхний регистр, Unicode и крайние дефисы отклоняются без DNS-запроса.
func validDNSLabel(value string) bool {
	return runtimecontract.ValidDNSLabel(value)
}

// validDNSSubdomain checks a nonempty name of at most 253 bytes as individually valid dot-separated DNS labels, rejecting empty components and trailing dots.
// validDNSSubdomain проверяет непустое имя до 253 байтов как допустимые отдельные DNS-метки через точки, отклоняя пустые части и завершающую точку.
func validDNSSubdomain(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

// restrictedSecurityContext sets the supplied UID/GID, non-root flag, read-only root filesystem, dropped capabilities, disabled privilege escalation and RuntimeDefault seccomp for ordinary runtime containers.
// restrictedSecurityContext задаёт переданные UID и GID, признак non-root, корневую файловую систему только для чтения, удалённые capabilities, запрет повышения привилегий и RuntimeDefault seccomp обычных runtime-контейнеров.
func restrictedSecurityContext(uid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(uid), RunAsGroup: int64Pointer(uid), AllowPrivilegeEscalation: boolPointer(false), ReadOnlyRootFilesystem: boolPointer(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
}

// providerSandboxSecurityContext starts with the restricted container profile but explicitly uses unconfined seccomp for the provider's bubblewrap user namespace, optionally adding a configured node-local AppArmor profile; it is not the ordinary RuntimeDefault sandbox.
// providerSandboxSecurityContext берёт ограниченный профиль контейнера, но явно включает unconfined seccomp для user namespace bubblewrap провайдера, необязательно добавляя заданный локальный AppArmor; обычным RuntimeDefault sandbox не является.
func providerSandboxSecurityContext(uid int64, appArmorProfile string) *corev1.SecurityContext {
	securityContext := restrictedSecurityContext(uid)
	// Codex строит внутреннюю файловую и сетевую границу через bubblewrap.
	// Default seccomp блокирует создание его user namespace. Host-owned dev
	// профиль может дополнительно назначить node-local AppArmor policy, но base
	// профиль не предполагает наличие такого артефакта на чужих узлах.
	securityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
	if appArmorProfile != "" {
		securityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost,
			LocalhostProfile: stringPointer(appArmorProfile)}
	}
	return securityContext
}

// smallResources supplies the fixed lightweight helper-container CPU/memory requests and limits, not the provider's separately configured resource policy.
// smallResources задаёт фиксированные requests и limits CPU и памяти лёгких служебных контейнеров, а не отдельно настроенную политику ресурсов провайдера.
func smallResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}
}

// httpProbe builds a named-port HTTP probe with caller-selected path, period and failure threshold and a fixed two-second timeout.
// httpProbe создаёт HTTP-probe именованного порта с выбранными путём, периодом и порогом отказов и фиксированным таймаутом две секунды.
func httpProbe(path, port string, period, failures int32) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromString(port)}}, PeriodSeconds: period, TimeoutSeconds: 2, FailureThreshold: failures}
}

// DebugSummary exposes only the runtime namespace and shortened controller UID for trusted diagnostics, without serializing configuration, credentials or runtime input.
// DebugSummary выдаёт доверенной диагностике только пространство runtime и сокращённый UID контроллера без сериализации конфигурации, реквизитов или runtime-входа.
func (manager *Manager) DebugSummary() string {
	return fmt.Sprintf("namespace=%s controller=%s", manager.config.RuntimeNamespace, shortHash(manager.config.ControllerPodUID))
}
