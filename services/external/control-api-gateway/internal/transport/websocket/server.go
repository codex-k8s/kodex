// Package websockettransport serves a resumable owner-authorized session stream with bounded queues and authoritative recovery reads.
// Package websockettransport обслуживает возобновляемый поток сессии с доступом владельца, ограниченными очередями и авторитетными чтениями восстановления.
package websockettransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	// maximumFrameBytes caps text commands and complete outbound RUN/platform envelopes at one MiB; platform wake input has its separate smaller cap.
	// maximumFrameBytes ограничивает текстовые команды и полные исходящие RUN/platform-конверты одним МиБ; входные сигналы платформы имеют отдельный меньший предел.
	maximumFrameBytes = 1 << 20
	// maximumRunSubscriptions permits at most thirty-two registered roots per socket, bounding resume and later subscription admission.
	// maximumRunSubscriptions допускает не более тридцати двух зарегистрированных корней сокета, ограничивая resume и последующие подписки.
	maximumRunSubscriptions = 32
	// maximumOutboundFrames bounds queued stream frames; a successful enqueue is not a network delivery receipt.
	// maximumOutboundFrames ограничивает очередь кадров; успешная постановка не является подтверждением сетевой доставки.
	maximumOutboundFrames = 256
	// maximumInboundCommands bounds reader-to-owner commands; queue exhaustion closes the session rather than allowing an unbounded backlog.
	// maximumInboundCommands ограничивает команды от reader владельцу; исчерпание очереди закрывает сессию вместо неограниченного накопления.
	maximumInboundCommands = 64
	// writeTimeout bounds individual writes/pings and terminal-attempt waiting at five seconds; library close-handshake timing is separate.
	// writeTimeout ограничивает запись, ping и ожидание терминальной попытки пятью секундами; время handshake закрытия библиотеки отдельно.
	writeTimeout = 5 * time.Second
	// readTimeout bounds only the initial SESSION_RESUME read at ten seconds; subsequent command reads follow the stream context.
	// readTimeout ограничивает только исходное чтение SESSION_RESUME десятью секундами; следующие команды используют контекст потока.
	readTimeout = 10 * time.Second
	// heartbeatInterval schedules fifteen-second reconciliation ticks, not a guarantee that dependency work completes within that interval.
	// heartbeatInterval задаёт сверку каждые пятнадцать секунд, а не гарантию завершения зависимостей за этот интервал.
	heartbeatInterval = 15 * time.Second
	// maximumHeartbeatWakes caps one heartbeat's initially captured wake prefix at four, leaving later arrivals for subsequent processing.
	// maximumHeartbeatWakes ограничивает исходный префикс одного heartbeat четырьмя сигналами, оставляя новые поступления дальнейшей обработке.
	maximumHeartbeatWakes = 4
	// heartbeatWakeTimeout caps each fresh catalog projection at two seconds within the common drain budget and any earlier parent deadline.
	// heartbeatWakeTimeout ограничивает каждую свежую проекцию каталога двумя секундами в общем бюджете и более раннем сроке родителя.
	heartbeatWakeTimeout = 2 * time.Second
	// heartbeatWakeDrainTimeout caps the four-item catalog drain context at eight seconds; deferred full-run refresh receives its own bounded context.
	// heartbeatWakeDrainTimeout ограничивает контекст обработки четырёх каталоговых сигналов восемью секундами; отложенное полное обновление запусков получает отдельный ограниченный контекст.
	heartbeatWakeDrainTimeout = maximumHeartbeatWakes * heartbeatWakeTimeout
	// pingInterval schedules thirty-second WebSocket pings for the writer; it is separate from owner-data heartbeat reconciliation.
	// pingInterval задаёт WebSocket ping writer каждые тридцать секунд отдельно от сверки данных владельца по heartbeat.
	pingInterval = 30 * time.Second
	// sessionSubprotocol selects the existing v2 browser stream handshake, requiring both CSRF offer and a one-use ticket.
	// sessionSubprotocol выбирает существующий handshake браузерного потока v2 с обязательными CSRF и одноразовым ticket.
	sessionSubprotocol = "kodex.session.v2"
	// legacySessionSubprotocol names the existing v1 transition branch accepted only before the configured absolute cutoff; this refactor does not extend or enable it.
	// legacySessionSubprotocol задаёт существующую переходную ветвь v1 только до настроенной абсолютной границы; этот рефакторинг её не продлевает и не включает.
	legacySessionSubprotocol = "kodex.session.v1"
	// platformStreamRef is the fixed browser platform-stream identifier, not an organization or resource authority supplied by the caller.
	// platformStreamRef — фиксированный браузерный идентификатор потока платформы, а не переданные вызывающим организация или полномочия ресурса.
	platformStreamRef = "PLATFORM"
)

var (
	// safeRef accepts 8..96 ASCII alphanumeric/underscore/hyphen reference syntax; matching it does not resolve ownership or a resource-specific prefix.
	// safeRef допускает синтаксис ссылки из 8..96 ASCII-букв, цифр, подчёркиваний и дефисов; совпадение не определяет владение или префикс вида ресурса.
	safeRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)
	// errOutboundOverflow reports failed bounded queue admission, not failure of an already acknowledged durable write.
	// errOutboundOverflow обозначает отказ допуска ограниченной очереди, а не ошибку уже подтверждённой устойчивой записи.
	errOutboundOverflow = errors.New("realtime outbound queue overflow")
	// errPeerClosed classifies reader-observed peer close or context cancellation without exposing raw transport errors to a browser problem.
	// errPeerClosed обозначает замеченные reader закрытие peer либо отмену без раскрытия сырых транспортных ошибок браузеру.
	errPeerClosed = errors.New("realtime peer closed")
)

// jscpd:ignore-start
// queryClient keeps explicit generated RPC signatures for independent resource
// queries. The repeated syntax is a typed contract, not duplicated executable
// logic; a generic dispatcher would discard method-specific request/response types.
// queryClient сохраняет явные generated RPC-сигнатуры независимых запросов.
// Повторяется типизированный контракт, а не исполняемая логика; универсальный
// dispatcher потерял бы отдельные типы запросов и ответов каждого метода.
type queryClient interface {
	// GetBootstrapState reads owner bootstrap/eligibility metadata under the supplied verified context; it neither transcribes audio nor configures the assistant.
	// GetBootstrapState читает metadata первоначальной настройки и допуска в переданном проверенном контексте без транскрипции и настройки помощника.
	GetBootstrapState(context.Context, *controlplanev1.GetBootstrapStateRequest, ...grpc.CallOption) (*controlplanev1.GetBootstrapStateResponse, error)
	// GetOverview reads the owner-authorized overview for the explicit request scope and returns its typed response/error.
	// GetOverview читает разрешённый владельцем обзор явной области запроса и возвращает типизированные ответ и ошибку.
	GetOverview(context.Context, *controlplanev1.GetOverviewRequest, ...grpc.CallOption) (*controlplanev1.GetOverviewResponse, error)
	// ListProjects reads the visible project page using owner-side eligibility; requested pagination does not establish membership.
	// ListProjects читает видимую страницу проектов по допуску владельца; запрос пагинации членство не устанавливает.
	ListProjects(context.Context, *controlplanev1.ListProjectsRequest, ...grpc.CallOption) (*controlplanev1.ListProjectsResponse, error)
	// ListTrashedProjects reads the owner-authorized trash page separately from active project data and propagates owner errors.
	// ListTrashedProjects читает разрешённую владельцем страницу корзины отдельно от активных проектов и возвращает его ошибки.
	ListTrashedProjects(context.Context, *controlplanev1.ListTrashedProjectsRequest, ...grpc.CallOption) (*controlplanev1.ListTrashedProjectsResponse, error)
	// GetProject resolves the exact selected project through the owner; its request reference is a selector rather than an access grant.
	// GetProject разрешает точный выбранный проект у владельца; ссылка запроса является селектором, а не grant доступа.
	GetProject(context.Context, *controlplanev1.GetProjectRequest, ...grpc.CallOption) (*controlplanev1.GetProjectResponse, error)
	// ListProjectMemberships reads the explicit project's owner-filtered membership page without deriving permission from returned display data.
	// ListProjectMemberships читает отфильтрованную владельцем страницу членства явного проекта без вывода прав из отображаемых данных.
	ListProjectMemberships(context.Context, *controlplanev1.ListProjectMembershipsRequest, ...grpc.CallOption) (*controlplanev1.ListProjectMembershipsResponse, error)
	// ListPlatformMemberships reads platform membership metadata through the owner context, not through a caller-provided organization claim.
	// ListPlatformMemberships читает metadata платформенного членства через контекст владельца, а не заявление организации вызывающего.
	ListPlatformMemberships(context.Context, *controlplanev1.ListPlatformMembershipsRequest, ...grpc.CallOption) (*controlplanev1.ListPlatformMembershipsResponse, error)
	// ListAgents reads the owner-authorized agent catalog for a project selector and pagination; it does not create or start agents.
	// ListAgents читает разрешённый владельцем каталог сотрудников по селектору проекта и пагинации без создания и запуска.
	ListAgents(context.Context, *controlplanev1.ListAgentsRequest, ...grpc.CallOption) (*controlplanev1.ListAgentsResponse, error)
	// ListWorkflows reads the scoped workflow catalog and its owner pagination without executing any process.
	// ListWorkflows читает каталог процессов своей области и пагинацию владельца без выполнения процессов.
	ListWorkflows(context.Context, *controlplanev1.ListWorkflowsRequest, ...grpc.CallOption) (*controlplanev1.ListWorkflowsResponse, error)
	// ListRuns reads the scoped owner run catalog; full subscribed run graphs are obtained through a different operation.
	// ListRuns читает каталог запусков области у владельца; полные графы подписок получает другая операция.
	ListRuns(context.Context, *controlplanev1.ListRunsRequest, ...grpc.CallOption) (*controlplanev1.ListRunsResponse, error)
	// ListOwnerGates reads visible owner-decision metadata without approving, rejecting or applying any external effect.
	// ListOwnerGates читает видимую metadata решений владельца без подтверждения, отклонения и внешнего эффекта.
	ListOwnerGates(context.Context, *controlplanev1.ListOwnerGatesRequest, ...grpc.CallOption) (*controlplanev1.ListOwnerGatesResponse, error)
	// ListArtifacts reads the scoped artifact catalog, not raw artifact bytes or private storage locations.
	// ListArtifacts читает каталог файлов области, а не сырые байты файлов или закрытые расположения хранилища.
	ListArtifacts(context.Context, *controlplanev1.ListArtifactsRequest, ...grpc.CallOption) (*controlplanev1.ListArtifactsResponse, error)
	// ListSchedules reads schedule metadata without activating timers or changing scheduler ownership.
	// ListSchedules читает metadata автоматизаций без запуска таймеров и изменения владения планировщиком.
	ListSchedules(context.Context, *controlplanev1.ListSchedulesRequest, ...grpc.CallOption) (*controlplanev1.ListSchedulesResponse, error)
	// ListIntegrationDefinitions reads typed public integration definitions through the owner, without invoking remote tools.
	// ListIntegrationDefinitions читает типизированные публичные определения интеграций у владельца без вызова удалённых инструментов.
	ListIntegrationDefinitions(context.Context, *controlplanev1.ListIntegrationDefinitionsRequest, ...grpc.CallOption) (*controlplanev1.ListIntegrationDefinitionsResponse, error)
	// ListIntegrationConnections reads eligible connection metadata and public grant projections, not credential values or executable authority.
	// ListIntegrationConnections читает допустимую metadata подключений и публичные проекции grants, а не credentials и исполняемые полномочия.
	ListIntegrationConnections(context.Context, *controlplanev1.ListIntegrationConnectionsRequest, ...grpc.CallOption) (*controlplanev1.ListIntegrationConnectionsResponse, error)
	// ListRuntimeEnvironmentSets reads scoped versioned environment metadata; it does not mount, publish or select a runtime itself.
	// ListRuntimeEnvironmentSets читает версионированную metadata окружений области, не монтируя, не публикуя и не выбирая runtime самостоятельно.
	ListRuntimeEnvironmentSets(context.Context, *controlplanev1.ListRuntimeEnvironmentSetsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeEnvironmentSetsResponse, error)
	// ListProviderAccounts requests account-page or financial-summary projections under owner policy; no provider credential is inferred from those views.
	// ListProviderAccounts запрашивает страницу аккаунтов либо финансовую проекцию по политике владельца; credentials провайдера из представлений не выводятся.
	ListProviderAccounts(context.Context, *controlplanev1.ListProviderAccountsRequest, ...grpc.CallOption) (*controlplanev1.ListProviderAccountsResponse, error)
	// ListProviderDefinitions reads the provider-definition page separately from user account and financial state.
	// ListProviderDefinitions читает страницу определений провайдеров отдельно от пользовательских аккаунтов и финансового состояния.
	ListProviderDefinitions(context.Context, *controlplanev1.ListProviderDefinitionsRequest, ...grpc.CallOption) (*controlplanev1.ListProviderDefinitionsResponse, error)
	// ListRuntimeSecrets reads scoped secret metadata only; the realtime catalog never calls a reveal operation through this port.
	// ListRuntimeSecrets читает только metadata секретов области; realtime-каталог через этот порт раскрытие не вызывает.
	ListRuntimeSecrets(context.Context, *controlplanev1.ListRuntimeSecretsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeSecretsResponse, error)
	// ListOrganizationRuntimeSecrets reads the dedicated organization-owner secret-metadata page without caller-supplied organization authority fields.
	// ListOrganizationRuntimeSecrets читает отдельную страницу metadata организационных секретов у владельца без входных полей полномочий организации.
	ListOrganizationRuntimeSecrets(context.Context, *controlplanev1.ListOrganizationRuntimeSecretsRequest, ...grpc.CallOption) (*controlplanev1.ListOrganizationRuntimeSecretsResponse, error)
	// ListManagedConfigurations reads one explicitly requested configuration-kind page while retaining owner-side scope and version policy.
	// ListManagedConfigurations читает страницу одного явно запрошенного вида конфигураций с политикой области и версий владельца.
	ListManagedConfigurations(context.Context, *controlplanev1.ListManagedConfigurationsRequest, ...grpc.CallOption) (*controlplanev1.ListManagedConfigurationsResponse, error)
	// ListRuntimeSelections reads permitted runtime selections for display; returned selectors do not issue execution grants.
	// ListRuntimeSelections читает разрешённые runtime selections для отображения; возвращаемые селекторы не выдают grants выполнения.
	ListRuntimeSelections(context.Context, *controlplanev1.ListRuntimeSelectionsRequest, ...grpc.CallOption) (*controlplanev1.ListRuntimeSelectionsResponse, error)
	// GetRunGraph reads the owner-authorized exact graph, optionally including complete run snapshots; client run references cannot bypass eligibility.
	// GetRunGraph читает разрешённый владельцем точный граф, при необходимости с полными снимками запусков; клиентская ссылка допуск не обходит.
	GetRunGraph(context.Context, *controlplanev1.GetRunGraphRequest, ...grpc.CallOption) (*controlplanev1.GetRunGraphResponse, error)
	// ListRunEvents reads owner events after the requested sequence for catch-up; contiguity and completion are checked by the consuming helper.
	// ListRunEvents читает события владельца после запрошенного sequence для догрузки; непрерывность и полноту проверяет helper-потребитель.
	ListRunEvents(context.Context, *controlplanev1.ListRunEventsRequest, ...grpc.CallOption) (*controlplanev1.ListRunEventsResponse, error)
	// GetPlatformEventCursor obtains the owner-derived organization/current sequence used for subscription and recovery, rather than trusting an organization payload.
	// GetPlatformEventCursor получает организацию и текущий sequence от владельца для подписки и восстановления вместо доверия организации из payload.
	GetPlatformEventCursor(context.Context, *controlplanev1.GetPlatformEventCursorRequest, ...grpc.CallOption) (*controlplanev1.GetPlatformEventCursorResponse, error)
}

// jscpd:ignore-end

// assistantQueryClient exposes only owner reads needed for assistant snapshots, with generated request/response types and no self-configuration commands.
// assistantQueryClient предоставляет только чтения владельца для снимков помощника с generated-типами без команд самонастройки.
type assistantQueryClient interface {
	// GetSystemAssistant reads the current system assistant's owner projection without starting a conversation or runtime.
	// GetSystemAssistant читает текущую проекцию системного помощника у владельца без запуска диалога или runtime.
	GetSystemAssistant(context.Context, *controlplanev1.GetSystemAssistantRequest, ...grpc.CallOption) (*controlplanev1.GetSystemAssistantResponse, error)
	// ListAssistantConversations reads a whole owner-filtered page for the explicit project selector, preserving owner pagination and conversation versions.
	// ListAssistantConversations читает целую отфильтрованную владельцем страницу по явному селектору проекта с его пагинацией и версиями диалогов.
	ListAssistantConversations(context.Context, *controlplanev1.ListAssistantConversationsRequest, ...grpc.CallOption) (*controlplanev1.ListAssistantConversationsResponse, error)
}

// roleImageQueryClient exposes typed environment/recipe metadata queries, not image build, admission or promotion commands.
// roleImageQueryClient предоставляет типизированные запросы metadata окружений и рецептов, а не сборку, допуск и продвижение образов.
type roleImageQueryClient interface {
	// ListRoleEnvironments reads the owner role-environment catalog used alongside scoped runtime/recipe views.
	// ListRoleEnvironments читает каталог role-environments владельца для отображения рядом с runtime и рецептами области.
	ListRoleEnvironments(context.Context, *controlplanev1.ListRoleEnvironmentsRequest, ...grpc.CallOption) (*controlplanev1.ListRoleEnvironmentsResponse, error)
	// ListRoleImageRecipes reads the selected project's visible recipe page; no recipe is built or admitted by this query.
	// ListRoleImageRecipes читает видимую страницу рецептов выбранного проекта без сборки и допуска рецепта.
	ListRoleImageRecipes(context.Context, *controlplanev1.ListRoleImageRecipesRequest, ...grpc.CallOption) (*controlplanev1.ListRoleImageRecipesResponse, error)
	// ListOrganizationRoleImageRecipes uses the dedicated owner-derived organization query without assigning organization authority from request fields.
	// ListOrganizationRoleImageRecipes использует отдельный запрос с организацией владельца без назначения полномочий из полей запроса.
	ListOrganizationRoleImageRecipes(context.Context, *controlplanev1.ListOrganizationRoleImageRecipesRequest, ...grpc.CallOption) (*controlplanev1.ListOrganizationRoleImageRecipesResponse, error)
}

// accessQueryClient reads public permission/subject/group/role/binding catalogs for display; none of these methods mutates or grants access.
// accessQueryClient читает публичные каталоги разрешений, субъектов, групп, ролей и привязок для отображения; методы доступ не изменяют и не выдают.
type accessQueryClient interface {
	// ListPermissionRegistry reads the closed permission registry through owner policy without deriving permissions from display labels.
	// ListPermissionRegistry читает закрытый реестр разрешений по политике владельца без вывода прав из отображаемых названий.
	ListPermissionRegistry(context.Context, *controlplanev1.ListPermissionRegistryRequest, ...grpc.CallOption) (*controlplanev1.ListPermissionRegistryResponse, error)
	// ListAccessSubjects reads the eligible subject page; its identifiers are display/selectors, not authenticated caller identity.
	// ListAccessSubjects читает допустимую страницу субъектов; идентификаторы являются данными отображения и селекторами, а не личностью вызывающего.
	ListAccessSubjects(context.Context, *controlplanev1.ListAccessSubjectsRequest, ...grpc.CallOption) (*controlplanev1.ListAccessSubjectsResponse, error)
	// ListOIDCGroups reads visible OIDC group metadata without changing IdP membership or trusting client-selected groups as authority.
	// ListOIDCGroups читает видимую metadata OIDC-групп без изменения членства IdP и доверия выбранным клиентом группам как полномочиям.
	ListOIDCGroups(context.Context, *controlplanev1.ListOIDCGroupsRequest, ...grpc.CallOption) (*controlplanev1.ListOIDCGroupsResponse, error)
	// ListAccessRoles reads the requested role page, including archived entries when explicitly requested, without activating roles.
	// ListAccessRoles читает запрошенную страницу ролей, включая архивные при явном запросе, без активации ролей.
	ListAccessRoles(context.Context, *controlplanev1.ListAccessRolesRequest, ...grpc.CallOption) (*controlplanev1.ListAccessRolesResponse, error)
	// ListAccessBindings reads permitted bindings for the requested project selector without creating or applying grants.
	// ListAccessBindings читает разрешённые привязки селектора проекта без создания и применения grants.
	ListAccessBindings(context.Context, *controlplanev1.ListAccessBindingsRequest, ...grpc.CallOption) (*controlplanev1.ListAccessBindingsResponse, error)
}

// Server holds external owner/STT clients, shared NATS, allowed origins and browser ticket boundary for one stream surface. It owns individual WebSocket handlers, not client/broker lifecycle; the constructor retains the supplied origins slice.
// Server хранит внешние owner/STT clients, общий NATS, origins и границу браузерных tickets одной поверхности потоков. Он владеет отдельными WebSocket handlers, а не жизнью клиентов и брокера; конструктор сохраняет переданный slice origins.
type Server struct {
	query       queryClient
	assistant   assistantQueryClient
	roleImages  roleImageQueryClient
	access      accessQueryClient
	speech      sttv1.SpeechToTextServiceClient
	nats        *nats.Conn
	origins     []string
	tickets     *boundary.Boundary
	legacyUntil time.Time
}

// New validates required client ports, connected NATS, speech/ticket dependencies and nonempty origins, then retains them without dialing owner RPCs or copying the origins slice. Callers must keep configuration stable.
// New проверяет обязательные порты, подключённый NATS, speech/ticket-зависимости и непустые origins, затем сохраняет их без owner RPC и копирования origins. Вызывающий должен сохранять конфигурацию неизменной.
func New(control *controlplaneclient.Client, connection *nats.Conn, speech sttv1.SpeechToTextServiceClient, origins []string, tickets *boundary.Boundary, legacyUntil time.Time) (*Server, error) {
	if control == nil || control.Query == nil || control.Assistant == nil || control.RoleImages == nil || control.Access == nil || connection == nil || !connection.IsConnected() || speech == nil || len(origins) == 0 || tickets == nil {
		return nil, errors.New("realtime server configuration is invalid")
	}
	return &Server{query: control.Query, assistant: control.Assistant, roleImages: control.RoleImages, access: control.Access, speech: speech, nats: connection, origins: origins, tickets: tickets, legacyUntil: legacyUntil}, nil
}

// busEnvelope is the minimal run-wake root/sequence shape; its bytes trigger a fresh owner read rather than becoming browser run data or authority.
// busEnvelope — минимальный формат корня и sequence сигнала запуска; байты запускают свежее чтение владельца, а не становятся браузерными данными или полномочиями.
type busEnvelope struct {
	RootRunRef string `json:"rootRunRef"`
	Sequence   int64  `json:"sequence"`
}

// streamProblemSpec carries only HTTP-like status and retryability for a known stream code, without storing provider error text.
// streamProblemSpec хранит только HTTP-подобный статус и повторяемость известного кода потока без текста ошибки провайдера.
type streamProblemSpec struct {
	status    int
	retryable bool
}

// streamProblemSpecs defines the closed local status/retryability mapping for stream problems; it does not authorize retries or normalize arbitrary code strings itself.
// streamProblemSpecs задаёт закрытое локальное отображение статуса и повторяемости ошибок потока, не разрешая повторы и не нормализуя произвольные строки кода.
var streamProblemSpecs = map[string]streamProblemSpec{
	"INTERNAL":              {status: http.StatusInternalServerError},
	"INVALID_COMMAND":       {status: http.StatusBadRequest},
	"SESSION_EXPIRED":       {status: http.StatusUnauthorized},
	"SESSION_REPLACED":      {status: http.StatusConflict, retryable: true},
	"PLATFORM_UNAVAILABLE":  {status: http.StatusServiceUnavailable, retryable: true},
	"RUN_UNAVAILABLE":       {status: http.StatusServiceUnavailable, retryable: true},
	"STREAM_UNAVAILABLE":    {status: http.StatusServiceUnavailable, retryable: true},
	"BACKPRESSURE_EXCEEDED": {status: http.StatusServiceUnavailable, retryable: true},
}

// protocolSelection retains untrusted CSRF and ticket offers parsed from subprotocols; separate boundary checks verify their session bindings.
// protocolSelection хранит непроверенные предложения CSRF и ticket из subprotocols; сессионные привязки проверяет отдельная граница.
type protocolSelection struct{ csrf, ticket string }

// outboundFrame queues a typed envelope and optional terminal close. written signals completion of an attempt, including a failed write, not delivery or durable acknowledgement.
// outboundFrame ставит типизированный конверт и необязательное закрытие в очередь. written сообщает завершение попытки, включая ошибочную запись, а не доставку или durable acknowledgement.
type outboundFrame struct {
	value       generated.SessionStream
	closeCode   websocket.StatusCode
	closeReason string
	written     chan struct{}
}

// sessionCommand is the package-closed marker contract for decoded subscribe/unsubscribe commands; its marker carries no execution or authorization behavior.
// sessionCommand — закрытый для пакета marker-контракт разобранных команд подписки и отписки; marker не несёт выполнения или авторизации.
type sessionCommand interface {
	// isSessionCommand limits command implementations to this package; the empty marker is not a validation or authority check.
	// isSessionCommand ограничивает реализации команд этим пакетом; пустой marker не является проверкой корректности или полномочий.
	isSessionCommand()
}

// subscribeRunCommand wraps the validated typed subscribe envelope for the owner event loop; the requested root remains subject to an owner read.
// subscribeRunCommand оборачивает проверенный типизированный subscribe-конверт для цикла владельца; запрошенный корень по-прежнему проверяется чтением у владельца.
type subscribeRunCommand struct{ generated.SubscribeRunEnvelope }

// unsubscribeRunCommand wraps a validated unsubscribe envelope, not a claim of ownership over the run or its execution.
// unsubscribeRunCommand оборачивает проверенный unsubscribe-конверт, а не заявление владения запуском или его выполнением.
type unsubscribeRunCommand struct {
	generated.UnsubscribeRunEnvelope
}

// isSessionCommand marks the typed subscribe wrapper without performing side effects or granting access.
// isSessionCommand помечает типизированную обёртку подписки без эффектов и выдачи доступа.
func (subscribeRunCommand) isSessionCommand() {}

// isSessionCommand marks the typed unsubscribe wrapper without closing a subscription by itself.
// isSessionCommand помечает типизированную обёртку отписки, самостоятельно подписку не закрывая.
func (unsubscribeRunCommand) isSessionCommand() {}

// runSubscription is event-loop-owned root/request/cursor/availability state plus one NATS subscription. Only pending is atomic for callback wake coalescing; other fields are not a general concurrent map contract.
// runSubscription хранит принадлежащие циклу root/request/cursor/availability и одну NATS-подписку. Только pending атомарен для объединения callback-сигналов; остальные поля не являются общим конкурентным контрактом.
type runSubscription struct {
	requestRef   string
	rootRef      string
	cursor       int64
	subscription *nats.Subscription
	pending      atomic.Bool
	available    bool
}

// sessionMultiplexer owns one socket's cursors, commands and subscriptions in one event loop, with bounded queues and mutex-protected wake inspection. Worker results and atomic pending flags do not transfer authority or subscription ownership to callbacks.
// sessionMultiplexer владеет курсорами, командами и подписками сокета в одном цикле с ограниченными очередями и защищённой mutex проверкой сигналов. Результаты работников и atomic pending не передают callbacks полномочия или владение подписками.
type sessionMultiplexer struct {
	server             *Server
	ctx                context.Context
	requestRef         string
	localize           func(string) string
	outbound           chan<- outboundFrame
	commands           <-chan sessionCommand
	readErrors         <-chan error
	platformSignals    chan platformSignal
	platformQueueMu    sync.Mutex
	runSignals         chan string
	overflow           chan struct{}
	platformRequestRef string
	organizationRef    string
	projectRef         string
	platformCursor     int64
	platformAvailable  bool
	platformSub        *nats.Subscription
	assistantPageHint  assistantSnapshotPageHint
	runs               map[string]*runSubscription
	runRefreshOffset   int
}

// ServeHTTP adapts the standard handler interface to ServeSessionHTTP without adding a second authentication or stream implementation.
// ServeHTTP передаёт стандартный handler в ServeSessionHTTP без второй реализации аутентификации или потока.
func (server *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.ServeSessionHTTP(writer, request)
}

// ServeSessionHTTP requires boundary identity, exact protocol/CSRF and a consumed v2 ticket before upgrading. It reads a bounded resume, owns the socket event loop/expiry/timers and requests reader/writer cancellation on return. The stream context detaches request cancellation; the reader/writer goroutines are not explicitly joined here. Queue admission and terminal written notifications are not delivery receipts, and no cookie renewal occurs in this loop.
// ServeSessionHTTP требует личность границы, точные protocol/CSRF и расход v2 ticket до upgrade. Он читает ограниченный resume, владеет циклом сокета, сроком и таймерами и запрашивает отмену reader/writer при возврате. Контекст потока отделён от отмены запроса; явного ожидания этих goroutines здесь нет. Допуск очереди и written терминальной попытки не подтверждают доставку; cookies в этом цикле не обновляются.
func (server *Server) ServeSessionHTTP(writer http.ResponseWriter, request *http.Request) {
	localize := streamLocalizer(writer, request)
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok {
		httptransport.WriteLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	protocols, selectedProtocol, csrfOK := server.selectProtocols(request, time.Now())
	if !csrfOK || !boundary.VerifyCSRFToken(identity, protocols.csrf) {
		httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
		return
	}
	if server.tickets == nil {
		httptransport.WriteLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	if selectedProtocol == sessionSubprotocol {
		if err := server.tickets.ConsumeWebSocketTicket(request.Context(), protocols.ticket); err != nil {
			if errors.Is(err, browserstate.ErrUnavailable) || errors.Is(err, browserstate.ErrConflict) || errors.Is(err, session.ErrRenewalPending) {
				writer.Header().Set("Retry-After", "1")
				httptransport.WriteLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
			} else {
				httptransport.WriteLocalProblem(writer, http.StatusForbidden, "CSRF_REJECTED", false)
			}
			return
		}
	}
	originPatterns := make([]string, 0, len(server.origins))
	for _, origin := range server.origins {
		originPatterns = append(originPatterns, strings.TrimPrefix(origin, "https://"))
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:   []string{selectedProtocol},
		OriginPatterns: originPatterns,
	})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	if connection.Subprotocol() != selectedProtocol {
		return
	}
	connection.SetReadLimit(maximumFrameBytes)
	streamContext, cancel := context.WithCancel(context.WithoutCancel(request.Context()))
	defer cancel()

	resume, err := readInitialResume(streamContext, connection)
	if err != nil {
		writeInitialProblem(streamContext, connection, localize, "INVALID_COMMAND")
		return
	}
	if identity.ExpiresAt.IsZero() || !identity.ExpiresAt.After(time.Now()) {
		writeInitialProblem(streamContext, connection, localize, "SESSION_EXPIRED")
		return
	}

	outbound := make(chan outboundFrame, maximumOutboundFrames)
	commands := make(chan sessionCommand, maximumInboundCommands)
	readErrors := make(chan error, 1)
	writerErrors := make(chan error, 1)
	go sessionWriter(streamContext, connection, outbound, writerErrors)
	go sessionReader(streamContext, connection, commands, readErrors)

	multiplexer := &sessionMultiplexer{
		server: server, ctx: streamContext, requestRef: resume.RequestRef,
		localize: localize, outbound: outbound, commands: commands, readErrors: readErrors,
		platformSignals: make(chan platformSignal, 128), runSignals: make(chan string, maximumRunSubscriptions),
		overflow: make(chan struct{}, 1), platformRequestRef: resume.RequestRef,
		runs: make(map[string]*runSubscription, maximumRunSubscriptions),
	}
	defer multiplexer.closeSubscriptions()
	if err := multiplexer.initialize(resume); err != nil {
		multiplexer.terminate("PLATFORM_UNAVAILABLE", websocket.StatusTryAgainLater)
		return
	}

	expiry := time.NewTimer(time.Until(identity.ExpiresAt))
	heartbeat := time.NewTicker(heartbeatInterval)
	defer expiry.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-streamContext.Done():
			return
		case <-expiry.C:
			multiplexer.terminate("SESSION_EXPIRED", websocket.StatusPolicyViolation)
			return
		case <-writerErrors:
			return
		case err := <-readErrors:
			if errors.Is(err, errOutboundOverflow) {
				multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			} else if !errors.Is(err, errPeerClosed) {
				multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
			}
			return
		case command := <-commands:
			if !multiplexer.applyCommand(command) {
				return
			}
		case signal := <-multiplexer.platformSignals:
			if !multiplexer.applyPlatformSignal(signal) {
				return
			}
		case runRef := <-multiplexer.runSignals:
			subscription := multiplexer.runs[runRef]
			if subscription == nil {
				continue
			}
			subscription.pending.Store(false)
			if !multiplexer.synchronizeRun(subscription) {
				return
			}
		case <-multiplexer.overflow:
			multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			return
		case now := <-heartbeat.C:
			if !multiplexer.heartbeat(now) {
				return
			}
		}
	}
}

// initialize applies the resume project selector, initializes owner platform state, subscribes requested roots and queues SESSION_READY with sorted current cursors. Earlier queued setup frames are not rolled back after a later failure.
// initialize применяет селектор проекта resume, получает состояние платформы владельца, подписывает корни и ставит SESSION_READY с отсортированными курсорами. Предыдущие подготовительные кадры после поздней ошибки не откатываются.
func (multiplexer *sessionMultiplexer) initialize(resume generated.SessionResumeEnvelope) error {
	if resume.ProjectRef != nil {
		multiplexer.projectRef = *resume.ProjectRef
	}
	if err := multiplexer.initializePlatform(resume.PlatformAfterSequence, resume.PlatformSnapshotRequired); err != nil {
		return err
	}
	for _, run := range resume.Runs {
		if !multiplexer.subscribeRun(run.RunRef, run.AfterSequence, resume.RequestRef) {
			return errOutboundOverflow
		}
	}
	streams := []generated.StreamCursor{{StreamKind: generated.StreamKindPlatform, StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor}}
	for _, runRef := range multiplexer.sortedRunRefs() {
		run := multiplexer.runs[runRef]
		streams = append(streams, generated.StreamCursor{StreamKind: generated.StreamKindRun, StreamRef: runRef, Cursor: run.cursor})
	}
	if !multiplexer.send(generated.SessionReadyEnvelope{Type: "SESSION_READY", RequestRef: resume.RequestRef, Streams: streams}) {
		return errOutboundOverflow
	}
	return nil
}

// initializePlatform obtains the organization/cursor from the owner, installs and flushes its wake subscription, then rereads the cursor to close the subscription race. It optionally queues bootstrap and always emits PLATFORM_READY with availability presence reflecting bootstrap versus exact cached resume; wake payloads do not establish authority.
// initializePlatform получает организацию и курсор владельца, устанавливает и flush-подтверждает подписку сигналов, затем перечитывает курсор против гонки. При необходимости ставится bootstrap, затем PLATFORM_READY с различием наличия availability для bootstrap и точного cache resume; payload полномочия не задаёт.
func (multiplexer *sessionMultiplexer) initializePlatform(after int64, snapshotRequired bool) error {
	cursor, err := multiplexer.server.query.GetPlatformEventCursor(multiplexer.ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || !safeRef.MatchString(cursor.GetOrganizationRef()) || cursor.GetCurrentSequence() < 0 {
		return errors.New("platform cursor is unavailable")
	}
	multiplexer.organizationRef = cursor.GetOrganizationRef()
	multiplexer.platformSub, err = multiplexer.server.nats.Subscribe("control_plane.platform."+multiplexer.organizationRef+".events", func(message *nats.Msg) {
		signal, valid := decodePlatformSignal(message.Data, multiplexer.organizationRef)
		if !valid {
			return
		}
		multiplexer.enqueuePlatformSignal(signal)
	})
	if err != nil || multiplexer.server.nats.FlushTimeout(2*time.Second) != nil {
		return errors.New("platform wake subscription is unavailable")
	}
	cursor, err = multiplexer.server.query.GetPlatformEventCursor(multiplexer.ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != multiplexer.organizationRef || cursor.GetCurrentSequence() < 0 {
		return errors.New("platform cursor changed during subscription")
	}
	multiplexer.platformCursor = cursor.GetCurrentSequence()
	multiplexer.platformAvailable = true
	var availableKinds *[]generated.PlatformResourceKind
	if needsPlatformBootstrap(after, multiplexer.platformCursor, snapshotRequired) {
		kinds, bootstrapErr := multiplexer.sendPlatformBootstrap()
		if bootstrapErr != nil {
			return bootstrapErr
		}
		availableKinds = &kinds
	}
	if !multiplexer.send(generated.PlatformReadyEnvelope{
		Type: "PLATFORM_READY", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor,
		AvailableKinds: availableKinds,
	}) {
		return errOutboundOverflow
	}
	return nil
}

// needsPlatformBootstrap preserves an explicitly complete browser cache only at the exact current owner cursor; it does not verify browser cache contents or authorize its resources.
// needsPlatformBootstrap сохраняет явно полный браузерный кэш только при точном текущем курсоре владельца, не проверяя содержимое кэша или права ресурсов.
func needsPlatformBootstrap(requestedAfter, current int64, snapshotRequired bool) bool {
	return snapshotRequired || requestedAfter != current
}

// applyCommand handles only the two typed commands, enforcing the new-root bound or unsubscribing an existing root and reporting its previous cursor. Unknown commands terminate; local unsubscribe errors are ignored rather than acknowledged as broker drain.
// applyCommand обрабатывает только две типизированные команды, ограничивая новый корень либо отписывая существующий с его прежним курсором. Неизвестная команда завершает поток; ошибки Unsubscribe игнорируются, а не подтверждают broker drain.
func (multiplexer *sessionMultiplexer) applyCommand(command sessionCommand) bool {
	switch value := command.(type) {
	case subscribeRunCommand:
		if _, exists := multiplexer.runs[value.RunRef]; !exists && len(multiplexer.runs) >= maximumRunSubscriptions {
			multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
			return false
		}
		return multiplexer.subscribeRun(value.RunRef, value.AfterSequence, value.RequestRef)
	case unsubscribeRunCommand:
		cursor := int64(0)
		if existing := multiplexer.runs[value.RunRef]; existing != nil {
			cursor = existing.cursor
			_ = existing.subscription.Unsubscribe()
			delete(multiplexer.runs, value.RunRef)
		}
		return multiplexer.send(generated.RunUnsubscribedEnvelope{
			Type: "RUN_UNSUBSCRIBED", RequestRef: value.RequestRef, StreamKind: "RUN",
			StreamRef: value.RunRef, Cursor: cursor,
		})
	default:
		multiplexer.terminate("INVALID_COMMAND", websocket.StatusPolicyViolation)
		return false
	}
}

// subscribeRun reads the exact owner root before and after installing/flushing its root wake subscription, coalesces callback notifications with atomic pending and recovers from the requested cursor. Failed owner/subscription setup emits a closed problem; replacing a prior subscription is not transactionally rolled back.
// subscribeRun читает точный корень владельца до и после установки и flush подписки, объединяет callbacks через atomic pending и восстанавливает запрошенный курсор. Отказ подготовки выдаёт закрытую ошибку; замена предыдущей подписки общей транзакцией не откатывается.
func (multiplexer *sessionMultiplexer) subscribeRun(runRef string, after int64, requestRef string) bool {
	snapshot, err := multiplexer.readRunSnapshot(runRef)
	if err != nil || snapshot.GetRun().GetRootRunRef() != runRef || !safeRef.MatchString(runRef) {
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "RUN_UNAVAILABLE")
	}
	if existing := multiplexer.runs[runRef]; existing != nil {
		_ = existing.subscription.Unsubscribe()
		delete(multiplexer.runs, runRef)
	}
	subscription := &runSubscription{requestRef: requestRef, rootRef: runRef, cursor: after}
	var subscribeErr error
	subscription.subscription, subscribeErr = multiplexer.server.nats.Subscribe("control_plane.run.*."+runRef+".events", func(message *nats.Msg) {
		if !validRunSignal(message.Data, runRef) || !subscription.pending.CompareAndSwap(false, true) {
			return
		}
		select {
		case multiplexer.runSignals <- runRef:
		default:
			subscription.pending.Store(false)
			multiplexer.signalOverflow()
		}
	})
	if subscribeErr != nil || multiplexer.server.nats.FlushTimeout(2*time.Second) != nil {
		if subscription.subscription != nil {
			_ = subscription.subscription.Unsubscribe()
		}
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "STREAM_UNAVAILABLE")
	}
	snapshot, err = multiplexer.readRunSnapshot(runRef)
	if err != nil || snapshot.GetRun().GetRootRunRef() != runRef {
		_ = subscription.subscription.Unsubscribe()
		return multiplexer.sendStreamProblem(requestRef, "RUN", runRef, after, "RUN_UNAVAILABLE")
	}
	multiplexer.runs[runRef] = subscription
	return multiplexer.recoverRun(subscription, snapshot, after)
}

// recoverRun uses complete snapshots or bounded event catch-up with explicit resync on ahead/gap cursors, then always rereads full owner storage/readiness even at an unchanged event cursor. It emits RUN_READY only after a valid available snapshot; earlier queued frames may precede a later failure.
// recoverRun использует полные снимки либо ограниченную догрузку событий с явным resync при опережении и разрыве, затем всегда перечитывает storage/readiness владельца даже при прежнем event cursor. RUN_READY выдаётся только после допустимого доступного снимка; прежние кадры могут предшествовать поздней ошибке.
func (multiplexer *sessionMultiplexer) recoverRun(subscription *runSubscription, snapshot *controlplanev1.GetRunGraphResponse, requestedAfter int64) bool {
	subscription.available = false
	current := snapshot.GetGraph().GetSequence()
	if requestedAfter == 0 || requestedAfter > current {
		if requestedAfter > current && !multiplexer.send(generated.RunResyncEnvelope{
			Type: "RUN_RESYNC_REQUIRED", RequestRef: subscription.requestRef, StreamKind: "RUN",
			StreamRef: subscription.rootRef, Cursor: current, RequestedAfterCursor: requestedAfter,
			Reason: generated.ResyncReasonProjectionRecovered,
		}) {
			return false
		}
		if !multiplexer.sendRunSnapshot(subscription, snapshot) {
			return false
		}
		subscription.cursor = current
	} else {
		latest, err := multiplexer.catchUp(subscription, requestedAfter)
		if err != nil {
			if errors.Is(err, errOutboundOverflow) {
				return false
			}
			if !multiplexer.send(generated.RunResyncEnvelope{
				Type: "RUN_RESYNC_REQUIRED", RequestRef: subscription.requestRef, StreamKind: "RUN",
				StreamRef: subscription.rootRef, Cursor: current, RequestedAfterCursor: requestedAfter,
				Reason: generated.ResyncReasonGapDetected,
			}) || !multiplexer.sendRunSnapshot(subscription, snapshot) {
				return false
			}
			latest = current
		}
		subscription.cursor = latest
	}
	// Rejoin always rereads storage through the owner snapshot even at the same run cursor: archive transitions do not increase the event sequence.
	// Rejoin всегда перечитывает хранилище через снимок владельца даже при прежнем курсоре: архивирование не увеличивает номер события запуска.
	snapshot, err := multiplexer.readRunSnapshot(subscription.rootRef)
	if err != nil || !multiplexer.sendRunSnapshot(subscription, snapshot) {
		subscription.available = false
		if err != nil {
			return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
		}
		return false
	}
	subscription.cursor = snapshot.GetGraph().GetSequence()
	if !subscription.available {
		return true
	}
	if !multiplexer.send(generated.RunReadyEnvelope{
		Type: "RUN_READY", RequestRef: subscription.requestRef, StreamKind: "RUN",
		StreamRef: subscription.rootRef, Cursor: subscription.cursor,
	}) {
		return false
	}
	subscription.available = true
	return true
}

// synchronizeRun requires full owner recovery for an unavailable subscription, otherwise catches up events and falls back to fresh snapshot recovery on read/gap errors. Backpressure returns false instead of retrying queued effects.
// synchronizeRun требует полного восстановления владельца для недоступной подписки, иначе догружает события и переходит к свежему снимку при ошибке чтения или разрыве. Перегрузка возвращает false без повтора кадров.
func (multiplexer *sessionMultiplexer) synchronizeRun(subscription *runSubscription) bool {
	if !subscription.available {
		snapshot, err := multiplexer.readRunSnapshot(subscription.rootRef)
		if err != nil {
			return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
		}
		return multiplexer.recoverRun(subscription, snapshot, subscription.cursor)
	}
	latest, err := multiplexer.catchUp(subscription, subscription.cursor)
	if err == nil {
		subscription.cursor = latest
		subscription.available = true
		return true
	}
	if errors.Is(err, errOutboundOverflow) {
		return false
	}
	snapshot, snapshotErr := multiplexer.readRunSnapshot(subscription.rootRef)
	if snapshotErr != nil {
		subscription.available = false
		return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
	}
	return multiplexer.recoverRun(subscription, snapshot, subscription.cursor)
}

// catchUp projects each contiguous owner event and queues its exact run/request/sequence envelope. A later error can follow an already queued prefix; failed admission returns errOutboundOverflow.
// catchUp проецирует непрерывные события владельца и ставит точные run/request/sequence-конверты. Поздняя ошибка возможна после уже поставленного префикса; отказ допуска возвращает errOutboundOverflow.
func (multiplexer *sessionMultiplexer) catchUp(subscription *runSubscription, after int64) (int64, error) {
	return readCatchUp(multiplexer.ctx, multiplexer.server.query, subscription.rootRef, after, func(event *controlplanev1.RunEvent) error {
		projected, err := projectRunEvent(event, multiplexer.localize)
		if err != nil {
			return err
		}
		if !multiplexer.send(generated.RunEventEnvelope{
			Type: "RUN_EVENT", RequestRef: subscription.requestRef, StreamKind: "RUN",
			StreamRef: subscription.rootRef, Cursor: event.GetSequence(), Event: projected,
		}) {
			return errOutboundOverflow
		}
		return nil
	})
}

// sendRunSnapshot closes projection failures with RUN_UNAVAILABLE; on success it updates subscription cursor/availability before attempting queue admission. A failed enqueue does not roll back those local assignments.
// sendRunSnapshot закрывает отказ проекции RUN_UNAVAILABLE; при успехе меняет курсор и availability до попытки постановки в очередь. Неудачный enqueue эти локальные присваивания не откатывает.
func (multiplexer *sessionMultiplexer) sendRunSnapshot(subscription *runSubscription, snapshot *controlplanev1.GetRunGraphResponse) bool {
	envelope, err := projectCompleteRunSnapshot(snapshot, subscription, multiplexer.localize)
	if err != nil {
		subscription.available = false
		return multiplexer.sendStreamProblem(subscription.requestRef, "RUN", subscription.rootRef, subscription.cursor, "RUN_UNAVAILABLE")
	}
	subscription.cursor = envelope.Cursor
	subscription.available = true
	return multiplexer.send(envelope)
}

// applyPlatformSignal applies one already decoded wake using the socket context and the normal fresh owner read/recovery path.
// applyPlatformSignal применяет один уже разобранный сигнал с контекстом сокета и обычным свежим чтением и восстановлением владельца.
func (multiplexer *sessionMultiplexer) applyPlatformSignal(signal platformSignal) bool {
	return multiplexer.applyPlatformSignalWithin(multiplexer.ctx, signal)
}

// applyPlatformSignalWithin forwards a caller context while retaining the standard complete registered-run refresher for invalidation recovery.
// applyPlatformSignalWithin передаёт контекст вызывающего с обычным полным обновлением зарегистрированных запусков при восстановлении.
func (multiplexer *sessionMultiplexer) applyPlatformSignalWithin(ctx context.Context, signal platformSignal) bool {
	return multiplexer.applyPlatformSignalWithRefresh(ctx, signal, multiplexer.refreshSubscribedRunsWithin)
}

// applyPlatformSignalWithRefresh injects the run-refresh callback but retains the bounded full platform snapshot reader and existing cursor/error semantics.
// applyPlatformSignalWithRefresh подставляет callback обновления запусков, сохраняя ограниченное полное чтение платформы и прежнюю семантику курсора и ошибок.
func (multiplexer *sessionMultiplexer) applyPlatformSignalWithRefresh(ctx context.Context, signal platformSignal, refresh func(context.Context) bool) bool {
	return multiplexer.applyPlatformSignalWithReaders(ctx, signal, refresh, multiplexer.boundedPlatformSnapshotWithin)
}

// applyPlatformSignalWithReaders ignores stale sequences, resyncs gaps/authority changes, advances only cursor metadata for foreign scope and reads eligible bodies otherwise. Provider denial forces resync; ordinary denial uses the cursor-only path, with RUN invalidation refreshing registered roots. An accepted envelope may already be queued when later refresh fails; cursor updates are not a distributed transaction.
// applyPlatformSignalWithReaders пропускает старые sequence, выполняет resync разрывов и смены полномочий, выдаёт только курсор чужой области и иначе читает допустимые тела. Запрет провайдера требует resync; обычный запрет даёт только курсор, RUN обновляет зарегистрированные корни. Конверт может быть уже в очереди при позднем отказе refresh; обновления курсора не являются распределённой транзакцией.
func (multiplexer *sessionMultiplexer) applyPlatformSignalWithReaders(ctx context.Context, signal platformSignal, refresh func(context.Context) bool, readSnapshot func(context.Context, generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error)) bool {
	if signal.Sequence <= multiplexer.platformCursor {
		return true
	}
	if signal.Sequence != multiplexer.platformCursor+1 {
		return multiplexer.synchronizePlatformWithRefresh(ctx, refresh)
	}
	// Eligibility changes require a full owner read so financial projections do not retain revoked access or old configuration. SYSTEM_ASSISTANT does not change project-agent financial eligibility; its messages and heartbeat retain the targeted delta/rejoin path.
	// Изменение допуска требует полного чтения владельца, чтобы финансовая проекция не сохраняла отозванный доступ или прежнюю конфигурацию. SYSTEM_ASSISTANT не меняет проектный допуск финансового каталога; сообщения и heartbeat сохраняют адресный путь delta/rejoin.
	if signal.Kind == "AGENT" || signal.Kind == "MEMBERSHIP" || signal.Kind == "PLATFORM_MEMBERSHIP" {
		return multiplexer.synchronizePlatformWithFloor(ctx, refresh, signal.Sequence)
	}
	if platformSignalOutsideScope(signal, multiplexer.projectRef) {
		return multiplexer.advancePlatformCursor(signal)
	}
	eventName := generated.PlatformEventName(signal.EventName)
	envelope := generated.PlatformSnapshotEnvelope{
		Type: "PLATFORM_SNAPSHOT", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: signal.Sequence,
		Mode: generated.PlatformSnapshotModeDelta, EventName: &eventName,
		Kind: generated.PlatformResourceKind(signal.Kind),
	}
	if multiplexer.projectRef != "" {
		envelope.ProjectRef = &multiplexer.projectRef
	}
	envelope, err := readSnapshot(ctx, envelope)
	if multiplexer.ctx.Err() != nil || len(multiplexer.overflow) > 0 {
		return false
	}
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			if signal.Kind == "PROVIDER_ACCOUNT" {
				return multiplexer.synchronizePlatformWithFloor(ctx, refresh, signal.Sequence)
			}
			if signal.EventName == "RUN_CHANGED" && !refresh(ctx) {
				return false
			}
			return multiplexer.advancePlatformCursor(signal)
		}
		multiplexer.platformAvailable = false
		code := "PLATFORM_UNAVAILABLE"
		if errors.Is(err, errPlatformSnapshotInvalid) {
			code = "INTERNAL"
		}
		return multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, code)
	}
	if !multiplexer.send(envelope) {
		return false
	}
	if signal.EventName == "RUN_CHANGED" {
		if !refresh(ctx) {
			return false
		}
	}
	multiplexer.platformCursor = signal.Sequence
	return true
}

// platformSignalOutsideScope skips only a different nonempty project when a project is selected, except PROJECT catalog changes; global and organization-wide wakes remain in scope.
// platformSignalOutsideScope пропускает только другой непустой проект при выбранном проекте, кроме каталога PROJECT; глобальные и организационные сигналы сохраняются.
func platformSignalOutsideScope(signal platformSignal, selectedProjectRef string) bool {
	if selectedProjectRef == "" || signal.ProjectRef == "" || signal.Kind == "PROJECT" {
		return false
	}
	return signal.ProjectRef != selectedProjectRef
}

// advancePlatformCursor queues a metadata-only envelope with the signal's optional project and commits the local cursor only after admission; no owner resource body is forwarded.
// advancePlatformCursor ставит metadata-конверт с необязательным проектом сигнала и меняет локальный курсор только после допуска; тело ресурса владельца не передаётся.
func (multiplexer *sessionMultiplexer) advancePlatformCursor(signal platformSignal) bool {
	eventName := generated.PlatformEventName(signal.EventName)
	envelope := generated.PlatformCursorEnvelope{
		Type: "PLATFORM_CURSOR", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: signal.Sequence,
		EventName: eventName, Kind: generated.PlatformResourceKind(signal.Kind),
	}
	if signal.ProjectRef != "" {
		envelope.ProjectRef = &signal.ProjectRef
	}
	if !multiplexer.send(envelope) {
		return false
	}
	multiplexer.platformCursor = signal.Sequence
	return true
}

// synchronizePlatform reconciles the owner platform cursor under the socket context, without treating queued wakes as authoritative state.
// synchronizePlatform сверяет курсор платформы владельца в контексте сокета, не считая очередь сигналов авторитетным состоянием.
func (multiplexer *sessionMultiplexer) synchronizePlatform() bool {
	return multiplexer.synchronizePlatformWithin(multiplexer.ctx)
}

// synchronizePlatformWithin uses the supplied context and standard registered-run refresher for cursor recovery.
// synchronizePlatformWithin использует переданный контекст и обычное обновление зарегистрированных запусков для восстановления курсора.
func (multiplexer *sessionMultiplexer) synchronizePlatformWithin(ctx context.Context) bool {
	return multiplexer.synchronizePlatformWithRefresh(ctx, multiplexer.refreshSubscribedRunsWithin)
}

// synchronizePlatformWithRefresh delegates cursor reconciliation with no minimum authority-wake floor, allowing a verified contiguous queued backlog to avoid false resync.
// synchronizePlatformWithRefresh сверяет курсор без минимальной границы authority-сигнала, позволяя непрерывной очереди после проверки избежать ложного resync.
func (multiplexer *sessionMultiplexer) synchronizePlatformWithRefresh(ctx context.Context, refresh func(context.Context) bool) bool {
	return multiplexer.synchronizePlatformWithFloor(ctx, refresh, 0)
}

// synchronizePlatformWithFloor rejects owner/rollback/minimum-cursor mismatch, otherwise accepts equal state or verified queued coverage when no floor is required. A real gap/floor emits AUTHORITATIVE_READ_REQUIRED, updates the local cursor and refreshes registered runs; this requests browser rejoin rather than rereading every platform catalog here.
// synchronizePlatformWithFloor отвергает несовпадение владельца, откат или меньший обязательный курсор, иначе принимает равное состояние либо проверенное покрытие очереди без floor. Разрыв или floor выдаёт AUTHORITATIVE_READ_REQUIRED, меняет локальный курсор и обновляет запуски; это запрос browser rejoin, а не перечитывание всех каталогов здесь.
func (multiplexer *sessionMultiplexer) synchronizePlatformWithFloor(ctx context.Context, refresh func(context.Context) bool, minimumCursor int64) bool {
	cursor, err := multiplexer.server.query.GetPlatformEventCursor(ctx, &controlplanev1.GetPlatformEventCursorRequest{})
	if err != nil || cursor.GetOrganizationRef() != multiplexer.organizationRef || cursor.GetCurrentSequence() < multiplexer.platformCursor || cursor.GetCurrentSequence() < minimumCursor {
		multiplexer.platformAvailable = false
		return multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, "PLATFORM_UNAVAILABLE")
	}
	multiplexer.platformAvailable = true
	current := cursor.GetCurrentSequence()
	if current == multiplexer.platformCursor {
		return true
	}
	// A contiguous unapplied FIFO is not a lost wake; each projection and cursor still follows the normal owner read.
	// Неприменённый последовательный FIFO не означает потерю сигнала; каждая проекция и курсор по-прежнему проходят обычное чтение владельца.
	if minimumCursor == 0 && multiplexer.queuedPlatformWakesCoverCursor(current) {
		return true
	}
	if !multiplexer.send(generated.PlatformResyncEnvelope{
		Type: "PLATFORM_RESYNC_REQUIRED", RequestRef: multiplexer.platformRequestRef,
		StreamKind: "PLATFORM", StreamRef: platformStreamRef, Cursor: current,
		Reason: "AUTHORITATIVE_READ_REQUIRED",
	}) {
		return false
	}
	multiplexer.platformCursor = current
	// A missed organization wake need not change Run.version or the run cursor.
	// Пропущенный организационный сигнал не обязательно меняет Run.version или курсор запуска.
	return refresh(ctx)
}

// enqueuePlatformSignal serializes nonblocking bounded wake admission with coverage inspection; a full queue sets overflow instead of blocking the NATS callback.
// enqueuePlatformSignal сериализует неблокирующий допуск сигналов с проверкой покрытия; полная очередь выставляет overflow вместо блокировки NATS callback.
func (multiplexer *sessionMultiplexer) enqueuePlatformSignal(signal platformSignal) {
	multiplexer.platformQueueMu.Lock()
	defer multiplexer.platformQueueMu.Unlock()
	select {
	case multiplexer.platformSignals <- signal:
	default:
		multiplexer.signalOverflow()
	}
}

// queuedPlatformWakesCoverCursor temporarily drains and restores the current FIFO under the enqueue mutex, checking contiguous coverage through the owner cursor while tolerating duplicates/future tail. Only the multiplexer may consume this queue; inspection neither advances state nor proves resource eligibility.
// queuedPlatformWakesCoverCursor временно извлекает и возвращает FIFO под mutex, проверяя непрерывное покрытие курсора владельца с допустимыми дубликатами и будущим хвостом. Только multiplexer потребляет очередь; проверка не продвигает состояние и не доказывает допуск ресурса.
func (multiplexer *sessionMultiplexer) queuedPlatformWakesCoverCursor(current int64) bool {
	multiplexer.platformQueueMu.Lock()
	defer multiplexer.platformQueueMu.Unlock()
	signals := make([]platformSignal, len(multiplexer.platformSignals))
	for index := range signals {
		signals[index] = <-multiplexer.platformSignals
	}
	expected := multiplexer.platformCursor
	valid := true
	for _, signal := range signals {
		multiplexer.platformSignals <- signal
		if !valid || expected == current || signal.Sequence <= expected {
			continue
		}
		if signal.Sequence != expected+1 {
			valid = false
			continue
		}
		expected = signal.Sequence
	}
	return valid && expected == current
}

// drainHeartbeatWakes captures at most four existing wakes within an eight-second aggregate context, giving each fresh catalog a bounded child context. It reuses a successful RUN catalog only within a contiguous exact-scope prefix, remeasuring every envelope, and coalesces required full-run refresh into a separate deferred budget. Cancellation, overflow, authority/kind/scope changes and failed reads retain their closed paths; arrivals cannot extend the initial prefix.
// drainHeartbeatWakes берёт не более четырёх готовых сигналов в восьмисекундном контексте с отдельным ограниченным чтением каждого свежего каталога. Успешный RUN-каталог переиспользуется лишь внутри непрерывного префикса точной области с измерением каждого конверта, а полное обновление запусков объединяется в отдельный отложенный бюджет. Отмена, перегрузка, смена полномочий, вида и области и отказы сохраняют закрытые пути; новые поступления префикс не продлевают.
func (multiplexer *sessionMultiplexer) drainHeartbeatWakes() (available, ok bool) {
	count := min(len(multiplexer.platformSignals), maximumHeartbeatWakes)
	ctx, cancel := context.WithTimeout(multiplexer.ctx, heartbeatWakeDrainTimeout)
	defer cancel()
	refreshRequired := false
	defer func() {
		if !refreshRequired || !ok {
			return
		}
		if multiplexer.ctx.Err() != nil {
			ok = false
			return
		}
		if len(multiplexer.overflow) > 0 {
			multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			ok = false
			return
		}
		// One full owner-read pass has its own shared budget, not the remainder of the catalog drain.
		// Один полный проход чтения владельца имеет отдельный общий бюджет, а не остаток обработки каталогов.
		ok = multiplexer.refreshSubscribedRunsWithin(multiplexer.ctx)
	}()
	deferRunRefresh := func(context.Context) bool {
		refreshRequired = true
		for _, subscription := range multiplexer.runs {
			subscription.available = false
		}
		return true
	}
	var runCatalog *generated.PlatformSnapshotPayload
	catalogProject := ""
	readCatalog := func(ctx context.Context, envelope generated.PlatformSnapshotEnvelope) (generated.PlatformSnapshotEnvelope, error) {
		if err := ctx.Err(); err != nil {
			return generated.PlatformSnapshotEnvelope{}, status.FromContextError(err).Err()
		}
		runChanged := envelope.Kind == generated.PlatformResourceKindRun && envelope.EventName != nil && *envelope.EventName == "RUN_CHANGED"
		if !runChanged || runCatalog == nil {
			value, err := multiplexer.boundedPlatformSnapshotWithin(ctx, envelope)
			if err == nil && runChanged {
				runCatalog = &value.Snapshot
			}
			return value, err
		}
		envelope.Snapshot = *runCatalog
		encoded, err := json.Marshal(envelope)
		if err != nil {
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotInvalid
		}
		if len(encoded) > maximumFrameBytes {
			return generated.PlatformSnapshotEnvelope{}, errPlatformSnapshotSize
		}
		return envelope, nil
	}
	for index := 0; ; index++ {
		if multiplexer.ctx.Err() != nil {
			return false, false
		}
		if len(multiplexer.overflow) > 0 {
			multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
			return false, false
		}
		if index == count {
			return true, true
		}
		if ctx.Err() != nil {
			multiplexer.platformAvailable = false
			return false, multiplexer.sendStreamProblem(multiplexer.platformRequestRef, "PLATFORM", platformStreamRef, multiplexer.platformCursor, "PLATFORM_UNAVAILABLE")
		}
		select {
		case signal := <-multiplexer.platformSignals:
			// The bundle belongs only to the contiguous RUN prefix of the exact scope.
			// Набор принадлежит только непрерывному RUN-префиксу точной области.
			if signal.Kind != "RUN" || signal.EventName != "RUN_CHANGED" || signal.ProjectRef != catalogProject ||
				(signal.Sequence > multiplexer.platformCursor && signal.Sequence != multiplexer.platformCursor+1) {
				runCatalog = nil
			}
			catalogProject = signal.ProjectRef
			// Each owner read retains its full child budget; the fixed prefix has a separate aggregate bound.
			// Каждое чтение владельца сохраняет полный дочерний бюджет; фиксированный префикс имеет отдельное общее ограничение.
			wakeCtx, cancelWake := context.WithTimeout(ctx, heartbeatWakeTimeout)
			applied := multiplexer.applyPlatformSignalWithReaders(wakeCtx, signal, deferRunRefresh, readCatalog)
			cancelWake()
			if !applied {
				if len(multiplexer.overflow) > 0 {
					multiplexer.terminate("BACKPRESSURE_EXCEEDED", websocket.StatusTryAgainLater)
				}
				return false, false
			}
			if multiplexer.ctx.Err() != nil {
				return false, false
			}
			if !multiplexer.platformAvailable {
				return false, true
			}
		default:
			return true, true
		}
	}
}

// heartbeat drains the bounded wake prefix, reconciles the owner cursor, projects STT availability and emits heartbeat frames only for available platform/run streams. It skips same-tick delta-only revival of a failed full snapshot; the bootstrap-read error used for STT projection is ignored here.
// heartbeat обрабатывает ограниченный префикс, сверяет курсор владельца, проецирует STT и выдаёт heartbeat только доступным потокам. В том же tick запрещено восстановление failed full snapshot одними delta; ошибка bootstrap-чтения для STT здесь игнорируется.
func (multiplexer *sessionMultiplexer) heartbeat(now time.Time) bool {
	if available, ok := multiplexer.drainHeartbeatWakes(); !ok || !available {
		return ok
	}
	if !multiplexer.synchronizePlatform() {
		return false
	}
	bootstrap, _ := multiplexer.server.query.GetBootstrapState(multiplexer.ctx, &controlplanev1.GetBootstrapStateRequest{})
	availability := multiplexer.server.projectSpeechAvailability(multiplexer.ctx, bootstrap.GetState().GetSpeechTranscription())
	if multiplexer.platformAvailable && !multiplexer.send(generated.StreamHeartbeatEnvelope{
		Type: "STREAM_HEARTBEAT", StreamKind: generated.StreamKindPlatform,
		StreamRef: platformStreamRef, Cursor: multiplexer.platformCursor, ServerTime: now.UTC().Format(time.RFC3339Nano),
		SpeechTranscription: availability,
	}) {
		return false
	}
	for _, runRef := range multiplexer.sortedRunRefs() {
		run := multiplexer.runs[runRef]
		// Do not revive a failed full snapshot through delta-only reads in the same tick.
		// Не восстанавливаем неудавшийся полный снимок одними delta-чтениями в том же tick.
		if !run.available {
			continue
		}
		if !multiplexer.synchronizeRun(run) {
			return false
		}
		if run.available && !multiplexer.send(generated.StreamHeartbeatEnvelope{
			Type: "STREAM_HEARTBEAT", StreamKind: generated.StreamKindRun,
			StreamRef: runRef, Cursor: run.cursor, ServerTime: now.UTC().Format(time.RFC3339Nano),
		}) {
			return false
		}
	}
	return true
}

// sendStreamProblem creates a localized typed problem using the local status/retryability spec and attempts bounded queue admission, not delivery acknowledgement.
// sendStreamProblem создаёт локализованную типизированную ошибку по локальным status/retryability и пытается поставить её в ограниченную очередь без подтверждения доставки.
func (multiplexer *sessionMultiplexer) sendStreamProblem(requestRef, kind, ref string, cursor int64, code string) bool {
	spec := problemSpec(code)
	return multiplexer.send(generated.StreamProblemEnvelope{
		Type: "STREAM_PROBLEM", RequestRef: requestRef, StreamKind: generated.StreamKind(kind),
		StreamRef: ref, Cursor: cursor, Status: spec.status, Code: generated.ProblemCode(code),
		Title: multiplexer.localize(code), Retryable: spec.retryable,
	})
}

// send performs nonblocking frame admission and sets overflow on failure; success does not mean serialization or socket write has completed.
// send неблокирующе допускает кадр в очередь и выставляет overflow при отказе; успех не означает завершение сериализации или записи в сокет.
func (multiplexer *sessionMultiplexer) send(value generated.SessionStream) bool {
	select {
	case multiplexer.outbound <- outboundFrame{value: value}:
		return true
	default:
		multiplexer.signalOverflow()
		return false
	}
}

// terminate best-effort queues a terminal session problem/close and waits for attempt completion, timeout or context cancellation. A full queue returns immediately, and written can also close after write failure; no final delivery guarantee is made.
// terminate пытается поставить терминальную ошибку и закрытие и ждёт завершения попытки, timeout либо отмены. При полной очереди возвращается сразу, written закрывается и после ошибочной записи; финальная доставка не гарантируется.
func (multiplexer *sessionMultiplexer) terminate(code string, status websocket.StatusCode) {
	spec := problemSpec(code)
	written := make(chan struct{})
	select {
	case multiplexer.outbound <- outboundFrame{value: generated.SessionProblemEnvelope{
		Type: "SESSION_PROBLEM", RequestRef: multiplexer.requestRef, Status: spec.status,
		Code: generated.ProblemCode(code), Title: multiplexer.localize(code), Retryable: spec.retryable,
	}, closeCode: status, closeReason: code, written: written}:
	default:
		return
	}
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	select {
	case <-written:
	case <-timer.C:
	case <-multiplexer.ctx.Done():
	}
}

// signalOverflow coalesces overload notification into a nonblocking single-slot signal without closing channels or mutating subscriptions.
// signalOverflow объединяет уведомление перегрузки в неблокирующий однослотовый сигнал без закрытия каналов и изменения подписок.
func (multiplexer *sessionMultiplexer) signalOverflow() {
	select {
	case multiplexer.overflow <- struct{}{}:
	default:
	}
}

// sortedRunRefs returns a newly allocated lexical snapshot of registered root keys for deterministic owner-loop processing; it is not a concurrent-map synchronization primitive.
// sortedRunRefs возвращает новый лексически отсортированный список зарегистрированных корней для детерминированного цикла, а не синхронизацию конкурентной map.
func (multiplexer *sessionMultiplexer) sortedRunRefs() []string {
	refs := make([]string, 0, len(multiplexer.runs))
	for ref := range multiplexer.runs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

// closeSubscriptions clears the socket-local page hint and best-effort unsubscribes platform/run interests. It does not close shared NATS, drain callbacks, clear every map field or join reader/writer goroutines.
// closeSubscriptions очищает локальную подсказку страницы и пытается отписать platform/run interests. Общий NATS не закрывается, callbacks не дренируются, все поля map не очищаются, reader/writer не ожидаются.
func (multiplexer *sessionMultiplexer) closeSubscriptions() {
	multiplexer.assistantPageHint = assistantSnapshotPageHint{}
	if multiplexer.platformSub != nil {
		_ = multiplexer.platformSub.Unsubscribe()
	}
	for _, run := range multiplexer.runs {
		_ = run.subscription.Unsubscribe()
	}
}

// sessionReader decodes commands into a bounded owner queue, reporting peer/cancellation or overflow errors best-effort. It stops on context cancellation but provides no explicit join acknowledgement to the handler.
// sessionReader разбирает команды в ограниченную очередь владельца и пытается сообщить peer/cancellation либо overflow. Он прекращает работу при отмене, но отдельного join-подтверждения handler не выдаёт.
func sessionReader(ctx context.Context, connection *websocket.Conn, commands chan<- sessionCommand, result chan<- error) {
	for {
		command, err := readSessionCommand(ctx, connection)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || errors.Is(err, context.Canceled) {
				err = errPeerClosed
			}
			select {
			case result <- err:
			default:
			}
			return
		}
		select {
		case commands <- command:
		case <-ctx.Done():
			return
		default:
			select {
			case result <- errOutboundOverflow:
			default:
			}
			return
		}
	}
}

// sessionWriter serializes bounded typed frames, bounds writes/pings and uses the library close handshake for terminal frames. It reports write/ping failures best-effort and closes written on success or failed attempts; it does not turn that signal into a browser receipt.
// sessionWriter сериализует ограниченные типизированные кадры, ограничивает запись и ping и использует handshake библиотеки для терминального закрытия. Ошибки сообщаются best-effort, written закрывается при успехе и ошибке; браузерным подтверждением он не становится.
func sessionWriter(ctx context.Context, connection *websocket.Conn, frames <-chan outboundFrame, result chan<- error) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-frames:
			payload, err := json.Marshal(frame.value)
			if err == nil && len(payload) <= maximumFrameBytes {
				bounded, cancel := context.WithTimeout(ctx, writeTimeout)
				err = connection.Write(bounded, websocket.MessageText, payload)
				cancel()
			} else if err == nil {
				err = errors.New("realtime frame exceeds maximum size")
			}
			if err != nil {
				if frame.written != nil {
					close(frame.written)
				}
				select {
				case result <- err:
				default:
				}
				return
			}
			if frame.closeCode != 0 {
				_ = connection.Close(frame.closeCode, frame.closeReason)
				if frame.written != nil {
					close(frame.written)
				}
				return
			}
			if frame.written != nil {
				close(frame.written)
			}
		case <-ticker.C:
			bounded, cancel := context.WithTimeout(ctx, writeTimeout)
			err := connection.Ping(bounded)
			cancel()
			if err != nil {
				select {
				case result <- err:
				default:
				}
				return
			}
		}
	}
}

// readInitialResume reads one bounded text resume under the initial ten-second child deadline, rejecting decode/validation errors with a zero result rather than using partial JSON state.
// readInitialResume читает один ограниченный текстовый resume с исходным дочерним сроком десять секунд, возвращая нулевой результат при ошибке разбора или проверки вместо частичного JSON.
func readInitialResume(ctx context.Context, connection *websocket.Conn) (generated.SessionResumeEnvelope, error) {
	readContext, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	payload, err := readTextFrame(readContext, connection)
	if err != nil {
		return generated.SessionResumeEnvelope{}, err
	}
	resume, err := decodeClosed[generated.SessionResumeEnvelope](payload)
	if err != nil || validateSessionResume(resume) != nil {
		return generated.SessionResumeEnvelope{}, errors.New("session resume is invalid")
	}
	return resume, nil
}

// validateSessionResume checks the exact type, safe request/project/root references, nonnegative cursors, root-count bound and duplicate roots. It validates syntax/cardinality, not the caller's ownership of requested runs.
// validateSessionResume проверяет точный type, допустимые request/project/root, неотрицательные курсоры, предел количества и дубликаты корней. Это формат и количество, а не владение запусками.
func validateSessionResume(resume generated.SessionResumeEnvelope) error {
	if resume.Type != "SESSION_RESUME" || !safeRef.MatchString(resume.RequestRef) ||
		resume.PlatformAfterSequence < 0 || len(resume.Runs) > maximumRunSubscriptions {
		return errors.New("session resume is invalid")
	}
	if resume.ProjectRef != nil && !safeRef.MatchString(*resume.ProjectRef) {
		return errors.New("session project scope is invalid")
	}
	seen := make(map[string]struct{}, len(resume.Runs))
	for _, run := range resume.Runs {
		if !safeRef.MatchString(run.RunRef) || run.AfterSequence < 0 {
			return errors.New("session run cursor is invalid")
		}
		if _, duplicated := seen[run.RunRef]; duplicated {
			return errors.New("session run cursor is duplicated")
		}
		seen[run.RunRef] = struct{}{}
	}
	return nil
}

// readSessionCommand reads a bounded text message under the supplied stream context and delegates closed typed command decoding without adding a per-command timeout.
// readSessionCommand читает ограниченное текстовое сообщение в переданном контексте и разбирает закрытую типизированную команду без отдельного timeout на команду.
func readSessionCommand(ctx context.Context, connection *websocket.Conn) (sessionCommand, error) {
	payload, err := readTextFrame(ctx, connection)
	if err != nil {
		return nil, err
	}
	return decodeSessionCommand(payload)
}

// decodeSessionCommand selects only SUBSCRIBE_RUN/UNSUBSCRIBE_RUN, then applies unknown-field/trailing-value rejection and reference/cursor checks. It does not consume authority or reject duplicate JSON keys by itself.
// decodeSessionCommand выбирает только SUBSCRIBE_RUN/UNSUBSCRIBE_RUN, затем отклоняет неизвестные поля, лишнее значение и неверные ссылки и курсоры. Полномочия не расходуются, повторные JSON-ключи сами по себе не запрещаются.
func decodeSessionCommand(payload []byte) (sessionCommand, error) {
	var header struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &header) != nil {
		return nil, errors.New("session command is invalid")
	}
	switch header.Type {
	case "SUBSCRIBE_RUN":
		value, decodeErr := decodeClosed[generated.SubscribeRunEnvelope](payload)
		if decodeErr != nil || !safeRef.MatchString(value.RequestRef) || !safeRef.MatchString(value.RunRef) || value.AfterSequence < 0 {
			return nil, errors.New("subscribe run command is invalid")
		}
		return subscribeRunCommand{value}, nil
	case "UNSUBSCRIBE_RUN":
		value, decodeErr := decodeClosed[generated.UnsubscribeRunEnvelope](payload)
		if decodeErr != nil || !safeRef.MatchString(value.RequestRef) || !safeRef.MatchString(value.RunRef) {
			return nil, errors.New("unsubscribe run command is invalid")
		}
		return unsubscribeRunCommand{value}, nil
	default:
		return nil, errors.New("unknown session command")
	}
}

// readTextFrame accepts only a nonempty text message within the shared byte cap, preserving transport errors; JSON/schema and identity checks happen elsewhere.
// readTextFrame принимает только непустое текстовое сообщение в общем байтовом пределе, сохраняя транспортные ошибки; JSON, схему и личность проверяют отдельно.
func readTextFrame(ctx context.Context, connection *websocket.Conn) ([]byte, error) {
	typ, payload, err := connection.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText || len(payload) == 0 || len(payload) > maximumFrameBytes {
		return nil, errors.New("realtime frame is invalid")
	}
	return payload, nil
}

// decodeClosed rejects unknown struct fields and additional JSON documents, but is not a duplicate-key or comprehensive semantic validator. Decode failure may return a partially populated value together with error; callers must discard it.
// decodeClosed отклоняет неизвестные поля структуры и дополнительные JSON-документы, но не является проверкой повторных ключей или всей семантики. При ошибке может вернуть частично заполненное значение вместе с ошибкой; вызывающий обязан его отбросить.
func decodeClosed[T any](payload []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return value, errors.New("multiple JSON values are forbidden")
	}
	return value, nil
}

// projectRunGraph delegates the protobuf graph to the common safe-error localization and closed generated-model projection without changing owner graph state.
// projectRunGraph передаёт protobuf-граф общей локализации безопасных ошибок и закрытой generated-проекции без изменения состояния графа владельца.
func projectRunGraph(value *controlplanev1.RunGraph, localize func(string) string) (generated.RunGraph, error) {
	return projectProto[generated.RunGraph](value, localize)
}

// projectRunEvent projects one owner event into the generated browser model, preserving typed fields and explicit safe localization rather than forwarding raw protobuf payload.
// projectRunEvent проецирует событие владельца в generated browser model с типизированными полями и явной безопасной локализацией вместо сырого protobuf.
func projectRunEvent(value *controlplanev1.RunEvent, localize func(string) string) (generated.RunEvent, error) {
	return projectProto[generated.RunEvent](value, localize)
}

// projectProto converts protobuf through the existing HTTP projection, localizes explicit safe errors and decodes the generated target with closed fields. It returns the zero target on any error; it does not establish authority or every domain invariant.
// projectProto преобразует protobuf существующей HTTP-проекцией, локализует явные безопасные ошибки и разбирает generated target с закрытыми полями. При ошибке возвращается нулевой target; полномочия и все доменные инварианты не устанавливаются.
func projectProto[T any](value proto.Message, localize func(string) string) (T, error) {
	var zero T
	projection, err := httptransport.ProtoMap(value)
	if err != nil {
		return zero, err
	}
	httptransport.LocalizeSafeErrors(projection, localize)
	payload, err := json.Marshal(projection)
	if err != nil {
		return zero, err
	}
	result, err := decodeClosed[T](payload)
	if err != nil {
		return zero, fmt.Errorf("project realtime model: %w", err)
	}
	return result, nil
}

// validRunSignal bounds wake bytes, closed-decodes only root/sequence and requires the exact subscribed root with a positive sequence. The wake does not advance the cursor or verify sender identity.
// validRunSignal ограничивает байты, закрыто разбирает root/sequence и требует точный корень подписки с положительным sequence. Сигнал не продвигает курсор и не проверяет отправителя.
func validRunSignal(payload []byte, rootRef string) bool {
	if len(payload) == 0 || len(payload) > maximumFrameBytes {
		return false
	}
	event, err := decodeClosed[busEnvelope](payload)
	return err == nil && event.RootRunRef == rootRef && event.Sequence > 0
}

// readCatchUp requests at most twenty owner event pages with limit200, skips old sequences and consumes only a contiguous new sequence. It checks empty/complete-page cursor agreement and returns the latest consumed prefix with errors; it does not add its own deadline, bind every event field or roll back consumer effects.
// readCatchUp запрашивает не более двадцати страниц событий владельца с limit200, пропускает старые sequence и потребляет лишь непрерывную новую последовательность. Проверяется курсор пустой или полной страницы; при ошибке возвращается последний потреблённый префикс. Отдельный срок, проверка всех полей события и откат эффектов здесь не добавляются.
func readCatchUp(ctx context.Context, client queryClient, rootRef string, after int64, consume func(*controlplanev1.RunEvent) error) (int64, error) {
	latest := after
	for page := 0; page < 20; page++ {
		response, err := client.ListRunEvents(ctx, &controlplanev1.ListRunEventsRequest{RunRef: rootRef, AfterSequence: latest, Limit: 200})
		if err != nil {
			return latest, err
		}
		if len(response.GetEvents()) == 0 {
			if response.GetCurrentSequence() != latest {
				return latest, errors.New("run event gap")
			}
			return latest, nil
		}
		for _, event := range response.GetEvents() {
			if event.GetSequence() <= latest {
				continue
			}
			if event.GetSequence() != latest+1 {
				return latest, errors.New("run event gap")
			}
			if err := consume(event); err != nil {
				return latest, err
			}
			latest = event.GetSequence()
		}
		if response.GetComplete() {
			if latest != response.GetCurrentSequence() {
				return latest, errors.New("incomplete run event catch-up")
			}
			return latest, nil
		}
	}
	return latest, errors.New("run catch-up bound exceeded")
}

// streamLocalizer selects only ru/en query locales when supported by the response wrapper, otherwise retaining its default localizer or identity fallback without reflecting arbitrary locale input.
// streamLocalizer выбирает только ru/en из query при поддержке wrapper, иначе сохраняет обычную локализацию либо возврат кода без отражения произвольного locale.
func streamLocalizer(writer http.ResponseWriter, request *http.Request) func(string) string {
	localize := func(messageID string) string { return messageID }
	if localized, ok := writer.(interface{ Localize(string) string }); ok {
		localize = localized.Localize
	}
	locale := request.URL.Query().Get("locale")
	if locale != "ru" && locale != "en" {
		return localize
	}
	if localized, ok := writer.(interface{ LocalizeFor(string, string) string }); ok {
		return func(messageID string) string { return localized.LocalizeFor(locale, messageID) }
	}
	return localize
}

// requestedProtocols parses exact known base plus a single nonempty csrf offer and, for v2 only, a single forty-three-character ticket. Unknown/duplicate offers fail closed; actual CSRF/ticket binding remains a separate check.
// requestedProtocols разбирает точный известный base, единственный непустой csrf и только для v2 единственный ticket из сорока трёх символов. Неизвестные и повторные предложения отклоняются; реальные привязки CSRF/ticket проверяются отдельно.
func requestedProtocols(request *http.Request, baseProtocol string) (protocolSelection, bool) {
	var result protocolSelection
	foundBase := false
	for _, header := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(header, ",") {
			value := strings.TrimSpace(raw)
			if value == baseProtocol {
				if foundBase {
					return protocolSelection{}, false
				}
				foundBase = true
				continue
			}
			if strings.HasPrefix(value, "csrf.") && len(value) > 5 {
				if result.csrf != "" {
					return protocolSelection{}, false
				}
				result.csrf = strings.TrimPrefix(value, "csrf.")
				continue
			}
			if strings.HasPrefix(value, "ticket.") && len(value) == 50 {
				if result.ticket != "" {
					return protocolSelection{}, false
				}
				result.ticket = strings.TrimPrefix(value, "ticket.")
				continue
			}
			return protocolSelection{}, false
		}
	}
	return result, foundBase && result.csrf != "" && ((baseProtocol == sessionSubprotocol && result.ticket != "") || (baseProtocol == legacySessionSubprotocol && result.ticket == ""))
}

// selectProtocols prefers valid v2 and permits the unchanged v1 branch only strictly before legacyUntil; zero/default cutoff disables v1 and missing v2 ticket never silently downgrades.
// selectProtocols предпочитает допустимый v2 и разрешает прежнюю ветвь v1 только строго до legacyUntil; нулевой cutoff отключает v1, отсутствие ticket v2 не вызывает скрытое понижение.
func (server *Server) selectProtocols(request *http.Request, now time.Time) (protocolSelection, string, bool) {
	if value, ok := requestedProtocols(request, sessionSubprotocol); ok {
		return value, sessionSubprotocol, true
	}
	if now.Before(server.legacyUntil) {
		if value, ok := requestedProtocols(request, legacySessionSubprotocol); ok {
			return value, legacySessionSubprotocol, true
		}
	}
	return protocolSelection{}, "", false
}

// problemSpec returns the known status/retryability pair or INTERNAL's fallback pair; it does not replace the caller's code string itself.
// problemSpec возвращает известную пару статуса и повторяемости либо пару INTERNAL, не заменяя саму строку кода вызывающего.
func problemSpec(code string) streamProblemSpec {
	if spec, ok := streamProblemSpecs[code]; ok {
		return spec
	}
	return streamProblemSpecs["INTERNAL"]
}

// writeInitialProblem best-effort writes a localized initial session problem under a child write deadline, then attempts a policy-violation close; ignored errors are not a successful delivery receipt.
// writeInitialProblem пытается записать локализованную исходную ошибку с дочерним сроком записи, затем закрыть поток policy violation; игнорирование ошибок не подтверждает доставку.
func writeInitialProblem(ctx context.Context, connection *websocket.Conn, localize func(string) string, code string) {
	spec := problemSpec(code)
	bounded, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	payload, _ := json.Marshal(generated.SessionProblemEnvelope{
		Type: "SESSION_PROBLEM", RequestRef: "request_invalid", Status: spec.status,
		Code: generated.ProblemCode(code), Title: localize(code), Retryable: spec.retryable,
	})
	_ = connection.Write(bounded, websocket.MessageText, payload)
	_ = connection.Close(websocket.StatusPolicyViolation, code)
}

// Check verifies only connected NATS and a context-bounded flush roundtrip; the caller must supply a deadline. It does not prove owner RPC authorization, active subscriptions, durable delivery or browser readiness.
// Check проверяет только подключённый NATS и flush roundtrip в ограниченном контексте; срок передаёт вызывающий. Owner RPC, активные подписки, durable delivery и готовность браузера не доказываются.
func (server *Server) Check(ctx context.Context) error {
	if server == nil || server.nats == nil || !server.nats.IsConnected() {
		return errors.New("realtime NATS consumer is unavailable")
	}
	return server.nats.FlushWithContext(ctx)
}
