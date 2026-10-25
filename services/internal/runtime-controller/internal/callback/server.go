// Package callback serves execution-ticket-scoped callbacks and MCP under the
// configured mTLS/trusted transport profile, without owning durable execution state.
// Пакет callback обслуживает execution-ticket-scoped callbacks и MCP под выбранным
// mTLS/trusted transport profile, не владея устойчивым состоянием выполнения.
package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/providerfinancewire"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/runtime-controller/internal/workload"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// maximumRequestBytes is the general completion-body reader budget; field and
	// artifact bounds are validated separately rather than implied by this number.
	// maximumRequestBytes задаёт общий бюджет чтения completion body; ограничения
	// полей и артефактов проверяются отдельно и не следуют из одного этого числа.
	maximumRequestBytes = 16 << 20
	// maximumProviderCredentialRefreshRequestBytes allows Base64 expansion of the
	// bounded auth blob plus JSON overhead; decoded auth limits remain separate.
	// maximumProviderCredentialRefreshRequestBytes учитывает расширение ограниченного
	// auth blob в Base64 и JSON overhead; decoded auth limits остаются отдельными.
	maximumProviderCredentialRefreshRequestBytes = ((runtimecontract.MaximumProviderAuthBytes + 2) / 3 * 4) + (16 << 10)
	// runtimeProgressRejectionMessage identifies progress rejection logs, not an
	// accepted owner state transition or a raw provider diagnostic.
	// runtimeProgressRejectionMessage обозначает логи отказа progress, не принятый
	// owner-переход состояния и не сырую диагностику провайдера.
	runtimeProgressRejectionMessage = "runtime progress callback rejected"
	// runtimeProgressRejectionStage names the callback's payload/owner/observation
	// rejection stage dimension rather than copying arbitrary request content.
	// runtimeProgressRejectionStage именует стадию отказа payload/owner/observation
	// callback вместо копирования произвольного содержимого запроса.
	runtimeProgressRejectionStage = "callback_stage"
	// runtimeProgressRejectionCode names the rejection-code field in progress logs.
	// runtimeProgressRejectionCode именует поле кода отказа в progress-логах.
	runtimeProgressRejectionCode = "callback_code"
)

// progressCodePattern recognizes a bounded uppercase progress token with digits
// and underscores; matching is syntax, not evidence that work actually ran.
// progressCodePattern распознаёт ограниченный uppercase progress token с цифрами
// и подчёркиваниями; форма не доказывает фактическое выполнение работы.
var progressCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

// Config supplies the listener, transport profile, mounted identity paths,
// request/transfer budgets and private spool location. New validates selected
// bounds; this value contains locators, not execution authority or secret bytes.
// Config задаёт listener, transport profile, пути identity mounts, бюджеты
// запросов/передачи и приватный spool. New проверяет выбранные ограничения;
// значение содержит locators, не полномочия выполнения и не байты секретов.
type Config struct {
	RPCProfile                                                                    string
	Listen, CertificateFile, PrivateKeyFile, ClientCAFile, ExpectedClientSPIFFEID string
	RequestTimeout, WarmLongPoll                                                  time.Duration
	FileTransferTimeout                                                           time.Duration
	ArtifactSpoolDirectory                                                        string
}

// Coordinator holds process-local warm dispatch, completion channels and
// provider observations under one mutex, without becoming the lease owner.
// Restart loses this state; durable leases remain owned by control-plane.
// Coordinator хранит локальные warm dispatch, каналы завершения и наблюдения
// провайдера под одним mutex, не становясь владельцем аренды. Restart теряет
// эти данные; устойчивые leases принадлежат control-plane.
type Coordinator struct {
	mu                 sync.Mutex
	warm               []warmExecution
	wake               chan struct{}
	done               map[string]chan struct{}
	providerExecutions map[string]providerProcessExecution
}

// warmExecution pairs a queued turn input with its supplied compatibility key;
// neither field is a durable claim or an independently verified owner receipt.
// warmExecution связывает queued turn input с переданным compatibility key;
// ни одно поле не является устойчивым claim или отдельной owner receipt.
type warmExecution struct {
	input               runtimecontract.RunnerInput
	compatibilityDigest string
}

// NewCoordinator allocates empty process-local completion/observation maps and
// a single-slot wake signal; it starts no worker and restores no durable leases.
// NewCoordinator создаёт пустые локальные maps завершений/наблюдений и односотовый
// wake-сигнал, не запуская worker и не восстанавливая устойчивые leases.
func NewCoordinator() *Coordinator {
	return &Coordinator{wake: make(chan struct{}, 1), done: make(map[string]chan struct{}), providerExecutions: make(map[string]providerProcessExecution)}
}

// Register updates local provider-registration state and returns the same open
// completion channel for an existing lease, or creates one under the mutex.
// Registration does not validate a fresh owner lease or persist a claim.
// Register обновляет локальную регистрацию провайдера и под mutex возвращает
// прежний открытый канал lease либо создаёт новый. Свежая owner lease
// не проверяется, claim устойчиво не сохраняется.
func (coordinator *Coordinator) Register(input runtimecontract.RunnerInput) <-chan struct{} {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.registerProviderExecution(input)
	if existing := coordinator.done[input.LeaseRef]; existing != nil {
		return existing
	}
	done := make(chan struct{})
	coordinator.done[input.LeaseRef] = done
	return done
}

// EnqueueWarm validates a system-assistant turn and a 64-byte compatibility key,
// deduplicates queued leases, caps the local queue at sixteen and signals waiters
// without blocking. It checks key length, not its hexadecimal form or authority.
// EnqueueWarm проверяет системный turn и 64-байтовый compatibility key,
// исключает queued lease duplicates, ограничивает очередь шестнадцатью и будит
// ожидающих без блокировки. Проверяется длина ключа, не hex-форма и не полномочие.
func (coordinator *Coordinator) EnqueueWarm(input runtimecontract.RunnerInput, compatibilityDigest string) error {
	if input.Mode != runtimecontract.RunnerModeTurn || !input.IsSystemAssistant() || input.Validate() != nil ||
		len(compatibilityDigest) != sha256.Size*2 {
		return errors.New("warm execution input is invalid")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	for _, current := range coordinator.warm {
		if current.input.LeaseRef == input.LeaseRef {
			return nil
		}
	}
	if len(coordinator.warm) >= 16 {
		return errors.New("warm execution queue is full")
	}
	coordinator.warm = append(coordinator.warm, warmExecution{input: input, compatibilityDigest: compatibilityDigest})
	select {
	case coordinator.wake <- struct{}{}:
	default:
	}
	return nil
}

// NextWarm removes the first exact-key queued input or waits for a wake/context
// cancellation. It neither validates owner freshness nor reserves a durable
// claim; the returned input retains the queued value's reference fields.
// NextWarm удаляет первый queued input с точным ключом либо ждёт wake/отмены.
// Свежесть у владельца не проверяется, durable claim не резервируется;
// возвращаемый input сохраняет ссылочные поля значения в очереди.
func (coordinator *Coordinator) NextWarm(ctx context.Context, revisionDigest string) (runtimecontract.RunnerInput, bool) {
	for {
		coordinator.mu.Lock()
		for index, input := range coordinator.warm {
			if input.compatibilityDigest == revisionDigest {
				coordinator.warm = append(coordinator.warm[:index], coordinator.warm[index+1:]...)
				coordinator.mu.Unlock()
				return input.input, true
			}
		}
		coordinator.mu.Unlock()
		select {
		case <-ctx.Done():
			return runtimecontract.RunnerInput{}, false
		case <-coordinator.wake:
		}
	}
}

// Complete removes local process evidence and closes/removes a registered done
// channel once. It performs no owner transition and does not purge warm-queue
// entries or delete Kubernetes resources for the lease.
// Complete удаляет локальные process evidence и однократно закрывает/удаляет
// зарегистрированный done-канал. Owner-переход, очистка warm-очереди
// и удаление Kubernetes-ресурсов lease здесь не выполняются.
func (coordinator *Coordinator) Complete(leaseRef string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	delete(coordinator.providerExecutions, leaseRef)
	if done := coordinator.done[leaseRef]; done != nil {
		close(done)
		delete(coordinator.done, leaseRef)
	}
}

// Server owns the callback HTTP boundary and private artifact spool while
// delegating durable state to control-plane and resource operations to Manager.
// HTTP drain and spool close are separate lifecycle operations.
// Server владеет callback HTTP boundary и приватным artifact spool, передавая
// устойчивое состояние control-plane, а операции ресурсов Manager.
// HTTP drain и закрытие spool являются отдельными lifecycle-операциями.
type Server struct {
	config      Config
	manager     *workload.Manager
	control     *controlplaneclient.Client
	coordinator *Coordinator
	logger      *slog.Logger
	http        *http.Server
	spool       *artifactSpool
}

// New validates required collaborators and timeout bounds, constructs the
// selected TLS/trusted transport and opens the private spool before configuring
// HTTP limits. It does not bind the listener or start serving requests.
// New проверяет collaborators и timeout bounds, собирает TLS/trusted transport
// и открывает приватный spool до настройки HTTP limits. Listener
// не привязывается и обслуживание запросов не запускается.
func New(config Config, manager *workload.Manager, control *controlplaneclient.Client, coordinator *Coordinator, logger *slog.Logger) (*Server, error) {
	if manager == nil || control == nil || coordinator == nil || logger == nil || config.Listen == "" ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.WarmLongPoll < time.Second || config.WarmLongPoll > 30*time.Second ||
		config.FileTransferTimeout < time.Second || config.FileTransferTimeout > runtimecontract.MaximumArtifactTransferDuration {
		return nil, errors.New("runtime callback configuration is invalid")
	}
	tlsConfig, err := serverTLS(config)
	if err != nil {
		return nil, err
	}
	spool, err := openArtifactSpool(config.ArtifactSpoolDirectory)
	if err != nil {
		return nil, err
	}
	server := &Server{config: config, manager: manager, control: control, coordinator: coordinator, logger: logger, spool: spool}
	server.http = &http.Server{Addr: config.Listen, Handler: http.HandlerFunc(server.route), TLSConfig: tlsConfig,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: max(2*config.FileTransferTimeout+10*time.Second, time.Duration(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds+10)*time.Second),
		IdleTimeout:  60 * time.Second, MaxHeaderBytes: 16 << 10}
	return server, nil
}

// Run serves with the supplied lifecycle context and optional TLS listener.
// Cancellation gets an independent five-second HTTP drain, forced close on
// failure and a Serve join; spool ownership remains with the caller's Close.
// Run обслуживает запросы с переданным lifecycle context и необязательным TLS.
// При отмене даёт отдельные пять секунд на HTTP drain, принудительно закрывает
// при ошибке и join-ит Serve; spool закрывает caller через Close.
func (server *Server) Run(ctx context.Context) error {
	server.http.BaseContext = func(net.Listener) context.Context { return ctx }
	listener, err := net.Listen("tcp", server.config.Listen)
	if err != nil {
		return errors.New("listen runtime callback")
	}
	if server.http.TLSConfig != nil {
		listener = tls.NewListener(listener, server.http.TLSConfig)
	}
	done := make(chan error, 1)
	go func() { done <- server.http.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := server.http.Shutdown(shutdown)
		if err != nil {
			_ = server.http.Close()
		}
		serveErr := <-done
		if !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
		return err
	}
}

// Shutdown drains HTTP using the caller's budget and force-closes on failure,
// returning the drain error without closing the artifact spool itself.
// Shutdown завершает HTTP в бюджете caller, при ошибке принудительно закрывает
// и возвращает ошибку drain, не закрывая сам artifact spool.
func (server *Server) Shutdown(ctx context.Context) error {
	err := server.http.Shutdown(ctx)
	if err != nil {
		_ = server.http.Close()
	}
	return err
}

// Close releases the private spool; callers must coordinate HTTP shutdown
// separately so active handlers cannot race this resource cleanup.
// Close освобождает приватный spool; caller отдельно согласует HTTP shutdown,
// чтобы активные handlers не конкурировали с очисткой ресурса.
func (server *Server) Close() error { return server.spool.close() }

// CheckArtifactSpool checks only spool usability under the supplied context,
// not full callback authorization, owner RPC readiness or artifact provenance.
// CheckArtifactSpool проверяет лишь доступность spool в переданном контексте,
// не полную callback authorization, owner RPC readiness и artifact provenance.
func (server *Server) CheckArtifactSpool(ctx context.Context) error { return server.spool.check(ctx) }

// route sets no-store and dispatches the closed method/path families after
// trimming outer slashes. Fragments and unsupported queries/methods return 404;
// artifact queries have their own handlers, and authorization remains per route.
// route ставит no-store и выбирает закрытые method/path после удаления внешних
// слешей. Fragment и лишние query/method дают 404; artifact query обрабатываются
// отдельно, а авторизация остаётся обязанностью конкретного маршрута.
func (server *Server) route(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.URL.Fragment != "" {
		http.NotFound(writer, request)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/v1/warm/next" && request.URL.RawQuery == "" {
		server.nextWarm(writer, request)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 5 && parts[0] == "v1" && parts[1] == "executions" && parts[2] != "" && parts[3] == "artifacts" && parts[4] != "" {
		if request.Method != http.MethodGet {
			http.NotFound(writer, request)
			return
		}
		server.artifact(writer, request, parts[2], parts[4])
		return
	}
	if request.URL.RawQuery != "" {
		http.NotFound(writer, request)
		return
	}
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "executions" || parts[2] == "" {
		http.NotFound(writer, request)
		return
	}
	switch parts[3] {
	case "progress":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.progress(writer, request, parts[2])
	case "complete":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.complete(writer, request, parts[2])
	case "mcp":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.mcp(writer, request, parts[2])
	case "native-tool-call":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.nativeToolCall(writer, request, parts[2])
	case "provider-credential-refresh":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.providerCredentialRefresh(writer, request, parts[2])
	default:
		http.NotFound(writer, request)
	}
}

// providerCredentialRefresh resolves the execution ticket, checks predecessor
// pins, materializes immutable refreshed auth and commits only its locator to
// the owner, requiring exact successor readback. The accepted auth buffer is
// cleared on return; failed commit does not itself delete the materialized Secret.
// providerCredentialRefresh разрешает execution ticket, проверяет predecessor
// pins, материализует immutable auth и фиксирует у владельца лишь locator
// с точным successor readback. Принятый auth buffer очищается при возврате;
// неуспешный commit сам по себе не удаляет материализованный Secret.
func (server *Server) providerCredentialRefresh(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerProviderCredentialRefreshRequest
	if decode(request, &payload, maximumProviderCredentialRefreshRequestBytes) != nil || payload.Validate() != nil ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		payload.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
		payload.PreviousContentSHA256 != input.ProviderCredentialSHA256 {
		http.Error(writer, "invalid provider credential refresh", http.StatusBadRequest)
		return
	}
	defer clear(payload.Authentication)
	requestContext, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	binding, err := server.manager.MaterializeProviderCredentialRefresh(requestContext, input, payload)
	if errors.Is(err, workload.ErrProviderCredentialRefreshRejected) {
		http.Error(writer, "provider credential refresh conflict", http.StatusConflict)
		return
	}
	if err != nil {
		server.logger.WarnContext(request.Context(), "provider credential refresh materialization failed", "failure_class", "kubernetes")
		http.Error(writer, "provider credential refresh unavailable", http.StatusServiceUnavailable)
		return
	}
	response, err := server.control.Runtime.CommitProviderCredentialRefresh(requestContext, providerCredentialRefreshProjection(input, payload, binding))
	if err != nil {
		server.logger.WarnContext(request.Context(), "control-plane provider credential refresh request failed",
			"grpc_code", status.Code(err).String(), "failure_class", controlFailureClass(err))
		writeControlError(writer, err)
		return
	}
	if !providerCredentialRefreshReadbackMatches(input, binding, response.GetProviderCredential()) {
		http.Error(writer, "runtime owner unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// providerCredentialRefreshProjection binds the mutation to lease/fence/generation
// and old/new credential digests, deriving a stable refresh key without sending
// authentication bytes. The owner still validates lineage and current authority.
// providerCredentialRefreshProjection связывает mutation с lease/fence/generation
// и old/new credential digests, выводя стабильный ключ без auth bytes.
// Lineage и актуальные полномочия по-прежнему проверяет владелец.
func providerCredentialRefreshProjection(input runtimecontract.RunnerInput, payload runtimecontract.RunnerProviderCredentialRefreshRequest, binding workload.ProviderSecretBinding) *controlplanev1.CommitProviderCredentialRefreshRequest {
	return &controlplanev1.CommitProviderCredentialRefreshRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, "provider-credential-refresh:"+binding.ContentSHA256)},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		PreviousCredentialRevisionRef: payload.PreviousCredentialRevisionRef,
		PreviousContentSha256:         payload.PreviousContentSHA256,
		SecretName:                    binding.Name,
		SecretUid:                     binding.UID,
		SecretResourceVersion:         binding.ResourceVersion,
		ContentSha256:                 binding.ContentSHA256,
	}
}

// providerCredentialRefreshReadbackMatches requires the same account, a new
// nonempty reference, exactly the next revision and exact Secret metadata/digest.
// Only the digest uses constant-time comparison; matching does not create a grant.
// providerCredentialRefreshReadbackMatches требует тот же аккаунт, новую
// непустую ссылку, следующую ревизию и точные Secret metadata/digest. Лишь digest
// сравнивается за постоянное время; совпадение не создаёт grant.
func providerCredentialRefreshReadbackMatches(input runtimecontract.RunnerInput, materialized workload.ProviderSecretBinding, actual *controlplanev1.ProviderCredentialBinding) bool {
	return actual != nil && actual.GetAccountRef() == input.ProviderAccountRef &&
		actual.GetCredentialRevisionRef() != "" && actual.GetCredentialRevisionRef() != input.ProviderCredentialRef &&
		actual.GetCredentialRevision() == int64(input.ProviderCredentialRevision)+1 &&
		actual.GetSecretName() == materialized.Name && actual.GetSecretUid() == materialized.UID &&
		actual.GetSecretResourceVersion() == materialized.ResourceVersion &&
		subtle.ConstantTimeCompare([]byte(actual.GetContentSha256()), []byte(materialized.ContentSHA256)) == 1
}

// nativeToolCall authorizes the execution, validates a bounded activity update
// and sends its lease-bound safe projection to the owner. A nonempty returned
// event reference is required before acknowledging the callback.
// nativeToolCall авторизует выполнение, проверяет ограниченное activity update
// и передаёт lease-bound safe-проекцию владельцу. Подтверждение callback
// требует непустую ссылку event в ответе.
func (server *Server) nativeToolCall(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerNativeToolCallRequest
	if decode(request, &payload, 8<<10) != nil || payload.Validate() != nil ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
		http.Error(writer, "invalid native tool call", http.StatusBadRequest)
		return
	}
	projection, err := nativeToolCallProjection(input, payload)
	if err != nil {
		http.Error(writer, "invalid native tool call", http.StatusBadRequest)
		return
	}
	requestContext, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.RecordRunToolCall(requestContext, projection)
	if err != nil {
		server.logger.WarnContext(request.Context(), "control-plane native tool projection request failed",
			"tool", payload.Kind, "grpc_code", status.Code(err).String(), "failure_class", controlFailureClass(err))
		writeControlError(writer, err)
		return
	}
	if response.GetEvent().GetRef() == "" {
		http.Error(writer, "runtime owner unavailable", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// nativeToolCallProjection validates the lease/revision tuple and native payload,
// JSON-normalizes safe parameters, adds the Codex item ID and derives stable
// call/mutation identifiers. It records activity rather than executing the tool;
// event persistence and authorization stay with the owner.
// nativeToolCallProjection проверяет lease/revision tuple и native payload,
// нормализует safe parameters через JSON, добавляет Codex item ID и выводит
// стабильные call/mutation IDs. Это activity, не запуск инструмента;
// сохранение event и authorization остаются у владельца.
func nativeToolCallProjection(input runtimecontract.RunnerInput, payload runtimecontract.RunnerNativeToolCallRequest) (*controlplanev1.RecordRunToolCallRequest, error) {
	if input.LeaseRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 || payload.Validate() != nil ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
		return nil, errors.New("native tool call projection is invalid")
	}
	rawParameters, err := json.Marshal(payload.SafeParameters)
	var normalizedParameters map[string]any
	if err != nil || json.Unmarshal(rawParameters, &normalizedParameters) != nil {
		return nil, errors.New("native tool call projection is invalid")
	}
	normalizedParameters["codex_item_id"] = payload.CallID
	parameters, err := structpb.NewStruct(normalizedParameters)
	if err != nil {
		return nil, errors.New("native tool call projection is invalid")
	}
	state := controlplanev1.RunToolCallState(controlplanev1.RunToolCallState_value["RUN_TOOL_CALL_STATE_"+payload.State])
	correlationKey := "native:" + payload.CallID
	digest := sha256.Sum256([]byte(stableKey(input.LeaseRef, correlationKey)))
	return &controlplanev1.RecordRunToolCallRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, correlationKey+":activity:"+strconv.FormatInt(payload.Revision, 10))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		CallRef: "tcl_" + hex.EncodeToString(digest[:16]), Tool: payload.Kind, SafeParameters: parameters,
		State: state, DurationMs: payload.DurationMS, SafeResult: payload.SafeResult, Revision: payload.Revision,
	}, nil
}

// artifact resolves an execution before selecting context/catalog query handling
// or a revision-pinned input artifact transfer. A matching name alone does not
// authorize arbitrary storage paths; unmatched references return 404.
// artifact разрешает выполнение до выбора context/catalog query либо передачи
// revision-pinned input artifact. Одно совпадение имени не разрешает произвольный
// storage path; неизвестные ссылки дают 404.
func (server *Server) artifact(writer http.ResponseWriter, request *http.Request, leaseRef, artifactRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	if request.URL.RawQuery != "" {
		if strings.HasPrefix(request.URL.RawQuery, "context_kind=") || request.URL.Query().Has("context_kind") {
			server.contextArtifact(writer, request, input, artifactRef)
		} else {
			server.catalogArtifact(writer, request, input, artifactRef)
		}
		return
	}
	for _, expected := range input.InputArtifacts {
		if expected.Ref == artifactRef {
			pin := artifactTransferPin{ref: expected.Ref, project: input.ProjectRef, name: expected.FileName, media: expected.MediaType,
				digest: expected.Digest, size: expected.SizeBytes, revision: expected.Revision, version: expected.Version}
			server.serveArtifactTransfer(writer, request, input, pin, expected.MediaType)
			return
		}
	}
	http.NotFound(writer, request)
}

// nextWarm resolves the warm ticket with revision headers, computes its shared
// compatibility key and long-polls only that local queue. Timeout returns 204;
// dequeuing does not establish a new durable owner claim.
// nextWarm разрешает warm ticket по revision headers, вычисляет общий
// compatibility key и long-poll-ит лишь соответствующую локальную очередь.
// Timeout даёт 204; извлечение не создаёт новый durable owner claim.
func (server *Server) nextWarm(writer http.ResponseWriter, request *http.Request) {
	revisionRef := request.Header.Get("X-Kodex-Runtime-Revision")
	revisionDigest := request.Header.Get("X-Kodex-Runtime-Revision-Digest")
	token, ok := bearer(request)
	if !ok {
		server.logger.WarnContext(request.Context(), "warm runtime callback authorization rejected", "error_class", "bearer")
		http.NotFound(writer, request)
		return
	}
	bound, err := server.manager.ResolveWarm(request.Context(), revisionRef, revisionDigest, token)
	if err != nil {
		server.logger.WarnContext(request.Context(), "warm runtime callback authorization rejected", "error_class", "ticket", "reason", err.Error())
		http.NotFound(writer, request)
		return
	}
	compatibilityDigest, err := runtimecontract.WarmCompatibilityDigest(bound)
	if err != nil {
		server.logger.WarnContext(request.Context(), "warm runtime callback authorization rejected", "error_class", "compatibility")
		http.NotFound(writer, request)
		return
	}
	wait, cancel := context.WithTimeout(request.Context(), server.config.WarmLongPoll)
	defer cancel()
	input, available := server.coordinator.NextWarm(wait, compatibilityDigest)
	writer.Header().Set("Content-Type", "application/json")
	if !available {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(writer).Encode(input)
}

// progress authorizes an exact execution, validates the selected progress/message/
// financial/process shape and submits the lease-fenced projection to the owner.
// Local process observation is recorded only after owner acceptance; financial
// casting preserves measurements without deciding account eligibility.
// progress авторизует точное выполнение, проверяет выбранную форму progress,
// message, financial либо process и передаёт lease-fenced проекцию владельцу.
// Локальное наблюдение процесса записывается лишь после принятия владельцем;
// перевод financial сохраняет измерения, не решая допустимость аккаунта.
func (server *Server) progress(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerProgressRequest
	if decode(request, &payload, runtimecontract.MaximumRuntimeMessageBytes*6+2048) != nil ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		payload.Financial == nil && payload.Message == nil && !progressCodePattern.MatchString(payload.Progress) ||
		payload.Financial != nil && (payload.Message != nil || payload.ProviderProcess != nil || payload.Progress != "" || payload.Financial.Validate() != nil) ||
		payload.Message != nil && (payload.Progress != "" || payload.Message.Validate() != nil) ||
		payload.ProviderProcess != nil && (payload.Message != nil || payload.Progress != runtimecontract.ProviderProcessInitializedProgress || !payload.ProviderProcess.Matches(input)) {
		server.logger.WarnContext(request.Context(), runtimeProgressRejectionMessage, runtimeProgressRejectionStage, "PAYLOAD", runtimeProgressRejectionCode, "INVALID")
		http.Error(writer, "invalid runtime progress", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	projection := &controlplanev1.ReportExecutionProgressRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration}
	if payload.Financial != nil {
		projection.Financial = providerfinancewire.ObservationToProto(*payload.Financial)
	} else if message := payload.Message; message != nil {
		digest := sha256.Sum256([]byte(stableKey(input.LeaseRef, "message:"+message.ItemID)))
		projection.Message = &controlplanev1.RunMessage{Ref: "msg_" + hex.EncodeToString(digest[:16]),
			Phase: controlplanev1.RunMessagePhase(controlplanev1.RunMessagePhase_value["RUN_MESSAGE_PHASE_"+message.Phase]), Revision: message.Revision, Text: message.Text}
	} else {
		projection.Progress = "i18n:" + payload.Progress
	}
	_, err := server.control.Runtime.ReportExecutionProgress(ctx, projection)
	if err != nil {
		server.logger.WarnContext(request.Context(), runtimeProgressRejectionMessage, runtimeProgressRejectionStage, "OWNER", runtimeProgressRejectionCode, status.Code(err).String())
		writeControlError(writer, err)
		return
	}
	if payload.ProviderProcess != nil && server.coordinator.recordProviderProcess(input, *payload.ProviderProcess) != nil {
		server.logger.WarnContext(request.Context(), runtimeProgressRejectionMessage, runtimeProgressRejectionStage, "PROCESS_OBSERVATION", runtimeProgressRejectionCode, "CONFLICT")
		http.Error(writer, "provider process observation conflict", http.StatusConflict)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// complete validates completion provenance before a cancellation-independent
// owner RPC. Nil or AlreadyExists permits local completion and a flushed 204,
// then bounded resource cleanup; AlreadyExists is not a new diagnostic commit,
// and cleanup failure is logged without undoing the accepted owner result.
// complete проверяет provenance до owner RPC независимо от отмены request.
// Nil либо AlreadyExists допускает локальное завершение и flush 204,
// затем ограниченную очистку; AlreadyExists не доказывает новый diagnostic
// commit, а ошибка cleanup логируется без отмены принятого owner result.
func (server *Server) complete(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerCompletionRequest
	// Порядок и predicates прежнего short-circuit guard сохраняются.
	rejectionReason := ""
	if decode(request, &payload, maximumRequestBytes) != nil {
		rejectionReason = completionDecodeReason
	} else if payload.Validate() != nil {
		rejectionReason = completionValidateReason
	} else if payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || payload.Attempt != input.Attempt {
		rejectionReason = completionProvenanceReason
	} else if payload.ProviderDiagnostic != nil && !payload.ProviderDiagnostic.Matches(input) {
		rejectionReason = completionProviderDiagnosticReason
	}
	if rejectionReason != "" {
		server.logCompletionRejection(request.Context(), input, completionPayloadStage, rejectionReason, codes.InvalidArgument)
		http.Error(writer, "invalid runtime completion", http.StatusBadRequest)
		return
	}
	artifacts := make([]*controlplanev1.CompletedArtifactInput, 0, len(payload.Artifacts))
	for _, artifact := range payload.Artifacts {
		artifacts = append(artifacts, &controlplanev1.CompletedArtifactInput{FileName: artifact.FileName, MediaType: artifact.MediaType, SizeBytes: int64(len(artifact.Content)), Content: artifact.Content, Sha256: artifact.SHA256})
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), server.config.RequestTimeout)
	defer cancel()
	usage := &controlplanev1.TokenUsage{Completeness: controlplanev1.UsageCompleteness(payload.Usage.Completeness), TotalTokens: payload.Usage.TotalTokens, InputTokens: payload.Usage.InputTokens, CachedInputTokens: payload.Usage.CachedInputTokens, CacheWriteInputTokens: payload.Usage.CacheWriteInputTokens, OutputTokens: payload.Usage.OutputTokens, ReasoningOutputTokens: payload.Usage.ReasoningOutputTokens, ModelContextWindow: payload.Usage.ModelContextWindow}
	_, err := server.control.Runtime.CompleteExecution(ctx, &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, "complete")}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Success: payload.Success, ResultSummary: payload.ResultSummary, SafeErrorCode: payload.SafeErrorCode, Artifacts: artifacts, Usage: usage, CodexSessionId: payload.CodexSessionID, CodexArchiveRelativePath: payload.ArchiveRelativePath, CodexArchiveSha256: payload.ArchiveSHA256, CodexArchiveSizeBytes: payload.ArchiveSizeBytes})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		server.logCompletionRejection(ctx, input, completionOwnerRPCStage, completionRPCRejectedReason, status.Code(err))
		writeControlError(writer, err)
		return
	}
	server.logCommittedProviderDiagnostic(ctx, input, payload, err)
	server.coordinator.Complete(input.LeaseRef)
	writer.WriteHeader(http.StatusNoContent)
	// Ответ о durable commit отправляется до удаления вызывающего Pod;
	// cleanup остаётся частью handler, которого дожидается HTTP shutdown.
	_ = http.NewResponseController(writer).Flush()
	cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(request.Context()), 10*time.Second)
	defer cleanupCancel()
	if cleanupErr := server.manager.DeleteTurn(cleanup, input.LeaseRef); cleanupErr != nil {
		server.logger.ErrorContext(cleanup, "runtime resource cleanup failed", "error_class", "kubernetes")
	}
}

// mcpRequest stores the JSON-RPC envelope and raw ID/params for route-specific
// handling; decoding this shape alone does not validate protocol semantics.
// mcpRequest хранит JSON-RPC envelope и сырые ID/params для маршрутизации;
// одно декодирование формы не проверяет семантику протокола.
type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpToolCallParams separates the selected tool, argument object and optional
// raw metadata. Metadata is not an authorization source or merged into arguments.
// mcpToolCallParams разделяет выбранный tool, arguments object и необязательную
// сырую metadata; она не источник полномочий и не объединяется с arguments.
type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments"`
	Metadata  json.RawMessage `json:"_meta,omitempty"`
}

// mcp resolves the exact execution ticket and headers before handing its trusted
// input to MCP dispatch; the JSON payload cannot choose a different execution.
// mcp разрешает точные execution ticket и headers до передачи доверенного input
// в MCP dispatch; JSON payload не выбирает другое выполнение.
func (server *Server) mcp(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	server.serveMCP(writer, request, input)
}

// serveMCP accepts bounded JSON-RPC2 messages, acknowledges notifications and
// serves initialize/tool-list/tool-call for the already resolved input. It is a
// deliberately limited MCP surface, not a general remote transport or OAuth client.
// serveMCP принимает ограниченные JSON-RPC2 messages, подтверждает notifications
// и обслуживает initialize/tool-list/tool-call для уже разрешённого input.
// Это ограниченный MCP surface, не общий remote transport или OAuth client.
func (server *Server) serveMCP(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput) {
	var rpc mcpRequest
	if decode(request, &rpc, 1<<20) != nil || rpc.JSONRPC != "2.0" || rpc.Method == "" {
		http.Error(writer, "invalid MCP message", http.StatusBadRequest)
		return
	}
	if len(rpc.ID) == 0 {
		if rpc.Method == "notifications/initialized" && !emptyMCPParams(rpc.Params) {
			http.Error(writer, "invalid MCP notification", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusAccepted)
		return
	}
	if rpc.Method == "notifications/initialized" {
		server.writeMCPError(writer, rpc.ID, -32600, "Invalid Request")
		return
	}
	switch rpc.Method {
	case "initialize":
		server.writeMCPResult(writer, rpc.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]string{"name": "kodex-runtime-tools", "version": "1"}})
	case "tools/list":
		server.writeMCPResult(writer, rpc.ID, map[string]any{"tools": tools(input)})
	case "tools/call":
		server.callTool(writer, request, rpc, input)
	default:
		server.writeMCPError(writer, rpc.ID, -32601, "Method not found")
	}
}

// emptyMCPParams accepts omitted params, null or an empty object with EOF;
// unknown struct fields and additional JSON values fail the empty-shape check.
// emptyMCPParams принимает отсутствие params, null либо пустой object с EOF;
// неизвестные поля struct и дополнительный JSON отвергаются.
func emptyMCPParams(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var params struct{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&params) == nil && errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

// tools composes the advertised catalog from runtime scope, snapshot/file
// availability, delegation/workflow targets and integration/MCP grants. Listing
// a tool is descriptive; invocation still needs its owner-side authorization.
// tools собирает каталог по runtime scope, snapshot/files, delegation/workflow
// targets и integration/MCP grants. Наличие tool в списке — описание;
// вызову по-прежнему нужна owner-side authorization.
func tools(input runtimecontract.RunnerInput) []map[string]any {
	result := []map[string]any{runMetadataTool()}
	if runtimecontract.RuntimeExecutionSnapshotAvailable(input) {
		result = append(result, executionSnapshotTool())
	}
	result = append(result, runtimeFileTools(input)...)
	if input.IsAssistant() {
		result = append(result, configurationCatalogTool(input), assistantResourceSearchTool(), assistantTaskSessionTool(), assistantPlanTool(input), assistantMetadataTool())
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, delegationTool(input.DelegationTargets))
	}
	if workflowLaunchAvailable(input) {
		result = append(result, workflowCatalogTool(), workflowLaunchTool())
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, integrationCatalogTool(), integrationTool())
	}
	if repositoryBundleAvailable(input) {
		result = append(result, repositoryBundleTool())
	}
	result = append(result, managedMCPTools(input)...)
	return result
}

// integrationTool describes the exact grant_ref/input invocation shape and
// directs the model to read the pinned grant schema; it executes no integration.
// integrationTool описывает точную форму grant_ref/input и направляет модель
// к закреплённой grant-схеме, не выполняя интеграцию.
func integrationTool() map[string]any {
	return map[string]any{
		"name": "invoke_integration", "description": "Invoke one exact grant from this RuntimeRevision. First read the compact get_integration_catalog index, then select its grant_ref to read the input schema. Copy that same grant_ref here with an input matching that schema. The server resolves and revalidates the pinned connection, capability, versions and authority.",
		"inputSchema": objectSchema([]string{"grant_ref", "input"}, map[string]any{
			"grant_ref": opaqueRefSchema(),
			"input":     map[string]any{"type": "object"},
		}),
	}
}

// integrationGrantForCall requires exactly grant_ref/input keys and selects a
// nonempty string reference from the immutable runtime catalog. It does not
// validate input shape or replace fresh owner policy/version checks.
// integrationGrantForCall требует ровно ключи grant_ref/input и выбирает
// непустую строковую ссылку immutable-каталога. Форма input здесь не проверяется,
// свежие owner policy/version checks не заменяются.
func integrationGrantForCall(input runtimecontract.RunnerInput, arguments map[string]any) (runtimecontract.RunnerIntegrationGrant, bool) {
	if !onlyKeys(arguments, "grant_ref", "input") || len(arguments) != 2 {
		return runtimecontract.RunnerIntegrationGrant{}, false
	}
	if ref, selected := arguments["grant_ref"].(string); selected {
		if ref == "" {
			return runtimecontract.RunnerIntegrationGrant{}, false
		}
		for _, grant := range input.IntegrationGrants {
			if grant.Ref == ref {
				return grant, true
			}
		}
		return runtimecontract.RunnerIntegrationGrant{}, false
	}
	return runtimecontract.RunnerIntegrationGrant{}, false
}

// integrationCallInputError retains a local input-failure reason separately
// from the fixed public message and InvalidArgument transport classification.
// integrationCallInputError хранит локальную причину отдельно от фиксированного
// публичного текста и transport-классификации InvalidArgument.
type integrationCallInputError struct{ reason string }

// Error returns fixed input diagnostics without reflecting the stored reason.
// Error возвращает фиксированную диагностику, не отражая сохранённую причину.
func (inputErr *integrationCallInputError) Error() string {
	return "integration call input is invalid"
}

// GRPCStatus maps this local error to InvalidArgument with the fixed safe text.
// GRPCStatus переводит локальную ошибку в InvalidArgument с фиксированным текстом.
func (inputErr *integrationCallInputError) GRPCStatus() *status.Status {
	return status.New(codes.InvalidArgument, inputErr.Error())
}

// delegationTargetsDescriptionPrefix marks bounded target metadata in the tool
// description; descriptive names cannot substitute for server-owned target refs.
// delegationTargetsDescriptionPrefix отмечает ограниченные target metadata
// в описании tool; названия не заменяют server-owned target refs.
const delegationTargetsDescriptionPrefix = "Server-owned targets (metadata only): "

// delegationTool builds enums from pinned targets and bounded descriptive
// metadata, requiring a step key if any target has one. Separate enums do not
// prove valid target/step pairs; runtime delegation validation resolves that pair.
// delegationTool строит enum из pinned targets и ограниченных описаний,
// требуя step key при его наличии хотя бы у одного target. Раздельные enums
// не доказывают пару target/step; её разрешает runtime-проверка делегирования.
func delegationTool(targets []runtimecontract.RunnerDelegationTarget) map[string]any {
	targetRefs := make([]string, 0, len(targets))
	stepKeys := make([]string, 0, len(targets))
	targetMetadata := make([]map[string]string, 0, len(targets))
	requiresStep := false
	for _, target := range targets {
		targetRefs = append(targetRefs, target.Ref)
		metadata := map[string]string{
			"ref":  target.Ref,
			"name": truncateRunes(target.Name, 160),
		}
		if target.Purpose != "" {
			metadata["purpose"] = truncateRunes(target.Purpose, 240)
		}
		if target.RoleDescription != "" {
			metadata["role_description"] = truncateRunes(target.RoleDescription, 240)
		}
		if target.WorkflowStepKey != "" {
			requiresStep = true
			stepKeys = append(stepKeys, target.WorkflowStepKey)
			metadata["workflow_step_key"] = target.WorkflowStepKey
			metadata["workflow_step_name"] = truncateRunes(target.WorkflowStepName, 160)
		}
		targetMetadata = append(targetMetadata, metadata)
	}
	encodedMetadata, _ := json.Marshal(targetMetadata)
	required := []string{"target_agent_ref", "task"}
	properties := map[string]any{
		"target_agent_ref": map[string]any{"type": "string", "enum": targetRefs, "description": delegationTargetsDescriptionPrefix + string(encodedMetadata)},
		"task":             map[string]any{"type": "string", "minLength": 1, "maxLength": 65536},
		"input":            map[string]any{"type": "object", "additionalProperties": true},
	}
	if requiresStep {
		required = append(required, "workflow_step_key")
		properties["workflow_step_key"] = map[string]any{"type": "string", "enum": stepKeys}
	}
	return map[string]any{
		"name":        "delegate_agent",
		"description": "Delegate exact pairs; end turn, await callback. " + runtimeFileHandoffGuidance,
		"inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties},
	}
}

// callTool decodes the selection, records a safe running projection before
// dispatch and records completion afterward, returning bounded recovery guidance
// for recognized input failures. A final projection error is joined with the
// operation error; it cannot roll back an external effect already performed.
// callTool разбирает выбор, записывает безопасную running-проекцию до dispatch
// и completion после него, выдавая recovery guidance для известных ошибок ввода.
// Ошибка конечной проекции объединяется с ошибкой операции и не откатывает
// уже совершённое внешнее действие.
func (server *Server) callTool(writer http.ResponseWriter, request *http.Request, rpc mcpRequest, input runtimecontract.RunnerInput) {
	params, err := decodeMCPToolCallParams(rpc.Params)
	if err != nil {
		server.writeMCPError(writer, rpc.ID, -32602, "Invalid params")
		return
	}
	if params.Name == runtimecontract.FileToolRead {
		bounded, cancel := context.WithTimeout(request.Context(), maximumFileReadDuration)
		defer cancel()
		request = request.WithContext(bounded)
	}
	if params.Name == "invoke_integration" {
		if _, valid := integrationGrantForCall(input, params.Arguments); !valid {
			guidance := map[string]any{"error_code": "INTEGRATION_INPUT_INVALID", "retryable": true,
				"guidance": "Read get_integration_catalog with {} and copy one exact grant_ref. Retry at most once with grant_ref and input matching its schema."}
			encoded, _ := json.Marshal(guidance)
			server.writeMCPResult(writer, rpc.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": guidance, "isError": true})
			return
		}
	}
	var result any
	workflowCatalogInputRejected := false
	startedAt := time.Now()
	err = nil
	// До запуска effect сохраняется только закрытая безопасная проекция.
	// Если owner отклонил полномочия, сам инструмент не вызывается.
	if err := server.recordToolCallPhase(request.Context(), input, params.Name, params.Arguments, nil, nil, rpc.ID, 0, 1); err != nil {
		server.writeMCPError(writer, rpc.ID, -32603, "Tool authorization unavailable")
		return
	}
	switch params.Name {
	case runtimecontract.ExecutionSnapshotTool:
		result, err = server.executionSnapshot(input, params.Arguments)
	case "get_configuration_catalog":
		result, err = server.configurationCatalog(request.Context(), input, params.Arguments)
	case "get_integration_catalog":
		result, err = integrationCatalog(input, params.Arguments)
	case "find_platform_resources":
		result, err = server.findPlatformResources(request.Context(), input, params.Arguments)
	case "read_task_session":
		result, err = server.readTaskSession(request.Context(), input, params.Arguments)
	case "propose_configuration_plan":
		result, err = server.proposeAssistantPlan(request.Context(), input, params.Arguments, rpc.ID)
	case "propose_assistant_metadata":
		result, err = server.proposeAssistantMetadata(request.Context(), input, params.Arguments, rpc.ID)
	case "propose_run_metadata":
		result, err = server.proposeRunMetadata(request.Context(), input, params.Arguments, rpc.ID)
	case "delegate_agent":
		result, err = server.delegate(request.Context(), input, params.Arguments, rpc.ID)
	case "launch_workflow":
		result, err = server.launchWorkflow(request.Context(), input, params.Arguments, rpc.ID)
	case "get_workflow_catalog":
		err = validateWorkflowCatalogInput(params.Arguments)
		workflowCatalogInputRejected = err != nil
		if err == nil {
			result, err = server.workflowCatalog(request.Context(), input, params.Arguments)
		}
	case repositorybundle.ToolName:
		result, err = server.prepareRepositoryBundle(request.Context(), input, params.Arguments, rpc.ID)
	case "invoke_integration":
		result, err = server.invoke(request.Context(), input, params.Arguments, rpc.ID)
	case runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool:
		var invokeArguments map[string]any
		invokeArguments, err = managedMCPArguments(input, params.Name, params.Arguments)
		if err == nil {
			result, err = server.invoke(request.Context(), input, invokeArguments, rpc.ID)
		}
	case runtimecontract.FileToolSearch, runtimecontract.FileToolMetadata, runtimecontract.FileToolPreview, runtimecontract.FileToolManifest, runtimecontract.FileToolRead:
		result, err = server.callFileTool(request.Context(), input, params.Name, params.Arguments)
	default:
		err = errors.New("tool is not available")
	}
	invocationInputInvalid := params.Name == "invoke_integration" && status.Code(err) == codes.InvalidArgument
	projectionErr := server.recordToolCall(request.Context(), input, params.Name, params.Arguments, result, err, rpc.ID, time.Since(startedAt))
	if err != nil {
		failureClass := controlFailureClass(err)
		var planInputErr *assistantPlanInputError
		if errors.As(err, &planInputErr) {
			if _, ok := assistantPlanInvalidDetails(err); !ok {
				failureClass = "assistant_plan_" + planInputErr.reason
			}
		}
		var catalogInputErr *integrationCatalogInputError
		if errors.As(err, &catalogInputErr) {
			failureClass = "integration_catalog_" + catalogInputErr.reason
		}
		var invocationInputErr *integrationCallInputError
		if errors.As(err, &invocationInputErr) {
			failureClass = "integration_call_" + invocationInputErr.reason
		}
		attributes := []any{"tool", params.Name, "stage", "operation", "grpc_code", status.Code(err).String(), "failure_class", failureClass}
		if params.Name == "read_task_session" {
			if stage, _, ok := taskSessionFailureDetails(err); ok {
				attributes[len(attributes)-1] = taskSessionFailureClass(stage)
				attributes = append(attributes, taskSessionFailureAttributes(input, rpc.ID, stage)...)
			}
		}
		if _, index := assistantPlanFailureDiagnostic(err); index > 0 {
			attributes = append(attributes, "operation_index", index)
		}
		if diagnostic, ok := assistantPlanInvalidDetails(err); ok && diagnostic.field != "" {
			attributes = append(attributes, "failure_field", diagnostic.field)
		}
		server.logger.WarnContext(request.Context(), "runtime MCP tool operation failed", attributes...)
	}
	if projectionErr != nil {
		server.logger.WarnContext(request.Context(), "runtime MCP tool projection failed",
			"tool", params.Name, "stage", "projection", "grpc_code", status.Code(projectionErr).String(),
			"failure_class", controlFailureClass(projectionErr))
	}
	if projectionErr != nil {
		err = errors.Join(err, projectionErr)
	}
	encoded, _ := json.Marshal(result)
	structured := result
	if err != nil {
		structured = map[string]any{"error_code": "TOOL_UNAVAILABLE", "retryable": false}
		if params.Name == "get_workflow_catalog" && workflowCatalogInputRejected && projectionErr == nil {
			if guidance := workflowCatalogInputRecoveryGuidance(err); guidance != "" {
				structured = map[string]any{"error_code": workflowCatalogInputInvalidCode, "retryable": true,
					"guidance": guidance}
			}
		}
		if runtimecontract.IsRuntimeFileTool(params.Name) && projectionErr == nil && errors.Is(err, errRuntimeFileInput) {
			structured = map[string]any{"error_code": runtimeFileInputInvalidCode, "retryable": true,
				"guidance": runtimeFileInputGuidance}
		}
		if params.Name == "delegate_agent" && projectionErr == nil && delegationInputFailureClass(err) != "" {
			structured = map[string]any{"error_code": delegationInputInvalidCode, "retryable": true,
				"guidance": delegationInputInvalidGuidance}
		}
		if params.Name == "get_configuration_catalog" && projectionErr == nil {
			if guidance := assistantCatalogRecoveryGuidance(err); guidance != "" {
				structured = map[string]any{"error_code": assistantCatalogInputInvalidCode, "retryable": true,
					"guidance": guidance}
			}
		}
		if params.Name == "find_platform_resources" && projectionErr == nil {
			switch assistantSearchFailureClass(err) {
			case assistantSearchInputShapeInvalid, assistantSearchQueryInvalid:
				structured = map[string]any{"error_code": assistantSearchInputInvalidCode, "retryable": true,
					"guidance": assistantSearchInputInvalidGuidance}
			}
		}
		var planInputErr *assistantPlanInputError
		if errors.As(err, &planInputErr) {
			guidance := "Read the current tool schema and retry once with exactly the required operation fields and camelCase parameter names."
			if planInputErr.reason == "environment_variable_name" {
				guidance = "Environment variable names cannot use reserved platform prefixes such as KODEX_. Do not rename a user-requested variable silently; explain the restriction and ask for a non-reserved name."
			}
			structured = map[string]any{
				"error_code": "PLAN_INPUT_INVALID",
				"retryable":  true,
				"guidance":   guidance,
			}
			if diagnostic, ok := assistantPlanInvalidDetails(err); ok {
				guidanceResult := structured.(map[string]any)
				guidanceResult["failure_stage"] = diagnostic.stage
				if diagnostic.field != "" {
					guidanceResult["failure_field"] = diagnostic.field
				}
			}
		}
		var catalogInputErr *integrationCatalogInputError
		if errors.As(err, &catalogInputErr) {
			guidance := "Retry once using either query and offset, or the exact grant_ref copied from the catalog index, but never both."
			if catalogInputErr.reason == "selection_missing" {
				guidance = "That grant is not bound to this run. Call get_integration_catalog with {} to read the compact index, then copy grant_ref from one entry exactly. Do not use an OpenAPI operationId or display name as a grant_ref."
			}
			structured = map[string]any{
				"error_code": "CATALOG_INPUT_INVALID",
				"retryable":  true,
				"guidance":   guidance,
			}
		}
		if invocationInputInvalid {
			structured = map[string]any{
				"error_code": "INTEGRATION_INPUT_INVALID",
				"retryable":  true,
				"guidance":   "Read get_integration_catalog with {} and select one exact grant_ref to obtain its input_schema. Call invoke_integration using only that grant_ref and input matching the schema. Retry at most once.",
			}
		}
		encoded, _ = json.Marshal(structured)
	}
	server.writeMCPResult(writer, rpc.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "structuredContent": structured, "isError": err != nil})
}

// assistantPlanInputError keeps an internal reason and optional recognized
// diagnostic separate from the fixed public text used for plan-input recovery.
// assistantPlanInputError хранит внутреннюю причину и необязательную известную
// диагностику отдельно от фиксированного публичного текста plan-input recovery.
type assistantPlanInputError struct {
	reason     string
	diagnostic *assistantPlanInvalidDiagnostic
}

// Error returns fixed plan-input diagnostics without embedding parameters,
// target contents or the internal failure reason.
// Error возвращает фиксированную диагностику без parameters, содержимого target
// и внутренней причины отказа.
func (planErr *assistantPlanInputError) Error() string { return "assistant plan input is invalid" }

// invalidAssistantPlan constructs a local typed reason without attaching owner
// metadata or performing any proposal/authorization operation.
// invalidAssistantPlan создаёт локальную типизированную причину без owner metadata,
// предложения плана и операции авторизации.
func invalidAssistantPlan(reason string) error {
	return &assistantPlanInputError{reason: reason}
}

// controlFailureClass recognizes selected typed/sentinel/RPC diagnostics and
// otherwise derives a class from the gRPC code. It does not return arbitrary
// upstream message text or make an authorization decision from that text.
// controlFailureClass распознаёт выбранные typed/sentinel/RPC diagnostics,
// иначе выводит класс из gRPC-кода. Произвольный upstream message не возвращается,
// решение о полномочиях по этому тексту не принимается.
func controlFailureClass(err error) string {
	if class := delegationInputFailureClass(err); class != "" {
		return class
	}
	if errors.Is(err, errRuntimeFileInput) {
		return runtimeFileInputFailureClass
	}
	if errors.Is(err, errRuntimeFileReply) {
		return runtimeFileReplyFailureClass
	}
	if class := assistantCatalogFailureClass(err); class != "" {
		return class
	}
	if class := assistantSearchFailureClass(err); class != "" {
		return class
	}
	if class, _ := assistantPlanFailureDiagnostic(err); class != "" {
		return class
	}
	if value := status.Convert(err); value.Code() == codes.Unavailable {
		details := value.Details()
		if len(details) == 1 {
			if info, ok := details[0].(*errdetails.ErrorInfo); ok && info.Domain == "kodex.control-plane" && len(info.Metadata) == 0 {
				switch info.Reason {
				case "ASSISTANT_CURRENT_CONFIGURATION_PROMPT_CONTEXT":
					return "assistant_current_configuration_prompt_context"
				case "ASSISTANT_CURRENT_CONFIGURATION_CONFIG_VIEW":
					return "assistant_current_configuration_config_view"
				case "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_READ":
					return "assistant_current_configuration_owner_core_read"
				case "ASSISTANT_CURRENT_CONFIGURATION_OWNER_CORE_VERSION":
					return "assistant_current_configuration_owner_core_version"
				case "ASSISTANT_CURRENT_CONFIGURATION_TEMPLATE_PROJECTION":
					return "assistant_current_configuration_template_projection"
				case "ASSISTANT_CURRENT_CONFIGURATION_UNCLASSIFIED":
					return "assistant_current_configuration_unclassified"
				}
			}
		}
	}
	switch status.Convert(err).Message() {
	case "authority proof permission is rejected":
		return "authority_proof_permission"
	case "authority proof request is rejected":
		return "authority_proof_request"
	case "internal RPC operation is not registered":
		return "operation_registry"
	case "operation is not permitted":
		return "domain_permission"
	case "authorization snapshot rollback rejected":
		return "authority_snapshot_rollback"
	case "record tool call projection":
		return "projection_" + strings.ToLower(status.Code(err).String())
	default:
		return "control_" + strings.ToLower(status.Code(err).String())
	}
}

// assistantPlanFailureDiagnostic accepts recognized local invalid details or
// one exact Aborted ErrorInfo with a known stage/category and canonical index.
// Unknown fields, metadata shape or out-of-range indices yield no diagnostic;
// this classification does not establish the caller's authority.
// assistantPlanFailureDiagnostic принимает известные local invalid details либо
// один точный Aborted ErrorInfo с известными stage/category и canonical index.
// Неизвестные поля, metadata shape и неверный index не дают диагноза;
// такая классификация не устанавливает полномочия caller.
func assistantPlanFailureDiagnostic(err error) (string, int) {
	if diagnostic, ok := assistantPlanInvalidDetails(err); ok {
		return "assistant_plan_" + diagnostic.stage + "_invalid", diagnostic.index
	}
	value := status.Convert(err)
	if value.Code() != codes.Aborted || len(value.Details()) != 1 {
		return "", 0
	}
	info, ok := value.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != "kodex.control-plane" || len(info.ProtoReflect().GetUnknown()) != 0 {
		return "", 0
	}
	stage := ""
	switch info.Reason {
	case "ASSISTANT_PLAN_HYDRATE":
		stage = "hydrate"
	case "ASSISTANT_PLAN_NORMALIZE":
		stage = "normalize"
	case "ASSISTANT_PLAN_COMMAND":
		stage = "command"
	case "ASSISTANT_PLAN_BIND":
		stage = "bind"
	case "ASSISTANT_PLAN_AUTHORIZE":
		stage = "authorize"
	case "ASSISTANT_PLAN_EMPTY":
		stage = "empty"
	default:
		return "", 0
	}
	category := info.Metadata["category"]
	if category != "CONFLICT" && category != "VERSION" {
		return "", 0
	}
	index := 0
	if stage == "empty" {
		if len(info.Metadata) != 1 {
			return "", 0
		}
	} else {
		index, _ = strconv.Atoi(info.Metadata["operation_index"])
		if len(info.Metadata) != 2 || index < 1 || index > 32 || strconv.Itoa(index) != info.Metadata["operation_index"] {
			return "", 0
		}
	}
	return "assistant_plan_" + stage + "_" + strings.ToLower(category), index
}

// decodeMCPToolCallParams requires one object with a nonempty tool name and
// nonnil arguments map, rejecting unknown struct fields and trailing JSON.
// Optional metadata must decode as an object or null; decoder acceptance does
// not enforce duplicate-key/case canonicality or the selected tool's schema.
// decodeMCPToolCallParams требует один object с непустым tool name и ненулевой
// map arguments, отвергая неизвестные struct fields и лишний JSON. Metadata
// декодируется как object либо null; это не проверка повторных ключей,
// canonical case или схемы выбранного инструмента.
func decodeMCPToolCallParams(raw json.RawMessage) (mcpToolCallParams, error) {
	var params mcpToolCallParams
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		params.Name == "" || params.Arguments == nil {
		return mcpToolCallParams{}, errors.New("MCP tool call params are invalid")
	}
	if len(params.Metadata) != 0 {
		var metadata map[string]json.RawMessage
		if json.Unmarshal(params.Metadata, &metadata) != nil {
			return mcpToolCallParams{}, errors.New("MCP tool call metadata is invalid")
		}
	}
	return params, nil
}

// proposeAssistantPlan validates assistant proposal shape and bounded operations,
// normalizes supported locators, assigns ordinal refs and clears caller versions
// for owner-hydrated targets before a lease-fenced proposal RPC. Returned plan/
// conversation refs acknowledge a proposal, not application or human approval.
// proposeAssistantPlan проверяет форму и ограниченные операции предложения,
// нормализует locators, назначает ordinal refs и сбрасывает caller versions
// для owner-hydrated targets до lease-fenced proposal RPC. Plan/conversation refs
// подтверждают предложение, не применение и не одобрение человеком.
func (server *Server) proposeAssistantPlan(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !input.IsAssistant() || !onlyKeys(arguments, "summary", "operations") {
		return nil, invalidAssistantPlan("top_level_shape")
	}
	summary, _ := arguments["summary"].(string)
	rawOperations, _ := arguments["operations"].([]any)
	if !assistantPlanTextWithinLimit(summary, 2000) || len(rawOperations) == 0 || len(rawOperations) > 32 {
		return nil, invalidAssistantPlan("summary_or_count")
	}
	operations := make([]*controlplanev1.AssistantPlanOperation, 0, len(rawOperations))
	currentEntityName := ""
	if input.AssistantContext != nil {
		currentEntityName = input.AssistantContext.EntityName
	}
	for index, raw := range rawOperations {
		operation, ok := raw.(map[string]any)
		if !ok || !onlyKeys(operation, "type", "action", "title", "summary", "target", "parameters", "expectedVersion", "before", "after", "selected") {
			return nil, invalidAssistantPlan("operation_shape")
		}
		operation, normalizeErr := normalizeServerHydratedAssistantOperation(operation, summary, input.ProjectRef, currentEntityName)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		kind, _ := operation["type"].(string)
		if kind == "CREATE_PROJECT_ASSISTANT" && !input.IsSystemAssistant() {
			return nil, invalidAssistantPlan("operation_scope")
		}
		serverHydrated := assistantServerHydratedOperation(kind)
		action, _ := operation["action"].(string)
		title, _ := operation["title"].(string)
		operationSummary, _ := operation["summary"].(string)
		parameters, _ := operation["parameters"].(map[string]any)
		if !assistantConfigurationParametersAllowed(input, kind, parameters) {
			return nil, invalidAssistantPlan("operation_scope_or_parameters")
		}
		before, beforeOK := operation["before"].(map[string]any)
		after, afterOK := operation["after"].(map[string]any)
		if kind == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || (kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" || kind == "PREPARE_AGENT_RUNTIME_CONFIGURATION") || projectAssistantLocatorOperation(kind, parameters) {
			before, beforeOK = map[string]any{}, true
			after, afterOK = parameters, true
		}
		if serverHydrated {
			if !beforeOK {
				before, beforeOK = map[string]any{}, true
			}
			if !afterOK {
				after, afterOK = parameters, true
			}
		}
		target, targetOK := operation["target"].(map[string]any)
		selected, selectedOK := operation["selected"].(bool)
		if serverHydrated {
			action = assistantServerAction(kind)
			target = assistantServerTarget(kind, parameters, assistantOperationTargetContext(input, kind, parameters))
			targetOK = target != nil
			selected, selectedOK = true, true
		}
		typeValue, exists := controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+kind]
		actionValue, actionExists := controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+action]
		if !exists || typeValue == 0 {
			return nil, invalidAssistantPlan("operation_type")
		}
		if !actionExists || actionValue == 0 {
			return nil, invalidAssistantPlan("operation_action")
		}
		if !assistantPlanTextWithinLimit(title, 200) {
			return nil, invalidAssistantPlan("operation_title")
		}
		if !assistantPlanTextWithinLimit(operationSummary, 500) {
			return nil, invalidAssistantPlan("operation_summary")
		}
		if parameters == nil {
			return nil, invalidAssistantPlan("operation_parameters")
		}
		if !beforeOK || !afterOK {
			return nil, invalidAssistantPlan("operation_projection")
		}
		if !targetOK {
			return nil, invalidAssistantPlan("operation_target")
		}
		if !selectedOK || !selected {
			return nil, invalidAssistantPlan("operation_selection")
		}
		parameterStruct, parameterErr := structpb.NewStruct(parameters)
		beforeStruct, beforeErr := structpb.NewStruct(before)
		afterStruct, afterErr := structpb.NewStruct(after)
		if parameterErr != nil || beforeErr != nil || afterErr != nil {
			return nil, invalidAssistantPlan("operation_struct")
		}
		targetKind, _ := target["kind"].(string)
		targetRef, _ := target["ref"].(string)
		targetName, _ := target["name"].(string)
		expectedVersion, expectedOK := exactJSONInt64(operation["expectedVersion"])
		targetVersion, targetVersionOK := exactJSONInt64(target["version"])
		if serverHydrated {
			expectedVersion, expectedOK = 0, false
			targetVersion, targetVersionOK = 0, false
		}
		if expectedOK != targetVersionOK || expectedOK && expectedVersion != targetVersion || targetKind == "" || targetName == "" {
			return nil, invalidAssistantPlan("operation_target_version")
		}
		var expected *int64
		if expectedOK {
			expected = &expectedVersion
		}
		operations = append(operations, &controlplanev1.AssistantPlanOperation{Ref: fmt.Sprintf("operation-%03d", index+1),
			Type: controlplanev1.AssistantPlanOperation_Type(typeValue), Action: controlplanev1.AssistantPlanOperation_Action(actionValue),
			Title: strings.TrimSpace(title), Summary: strings.TrimSpace(operationSummary), TargetKind: targetKind, TargetRef: targetRef,
			TargetName: targetName, ExpectedVersion: expected, Parameters: parameterStruct, Before: beforeStruct, After: afterStruct, Selected: true})
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.ProposeAssistantPlan(requestContext, &controlplanev1.ProposeAssistantPlanRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		Summary: strings.TrimSpace(summary), Operations: operations,
	})
	if err != nil {
		attributes := []any{"grpc_code", status.Code(err).String(), "failure_class", controlFailureClass(err)}
		if _, index := assistantPlanFailureDiagnostic(err); index > 0 {
			attributes = append(attributes, "operation_index", index)
		}
		if diagnostic, ok := assistantPlanInvalidDetails(err); ok && diagnostic.field != "" {
			attributes = append(attributes, "failure_field", diagnostic.field)
		}
		server.logger.WarnContext(ctx, "control-plane assistant plan request failed", attributes...)
		return nil, assistantPlanControlError(err)
	}
	if response.GetPlan().GetRef() == "" || response.GetConversation().GetRef() == "" {
		return nil, errors.New("propose assistant plan")
	}
	return map[string]any{"ok": true, "plan_ref": response.GetPlan().GetRef(), "plan_version": response.GetPlan().GetVersion(), "plan_revision": response.GetPlan().GetRevision(),
		"conversation_ref": response.GetConversation().GetRef()}, nil
}

// assistantPlanControlError sanitizes errors from the owner proposal RPC,
// converts InvalidArgument into local recoverable input failure and preserves
// only recognized failure details. It expects an RPC error, not arbitrary local
// diagnostic objects, and does not retry or apply the plan.
// assistantPlanControlError очищает ошибки owner proposal RPC, превращает
// InvalidArgument в локальный recoverable input failure и сохраняет лишь
// известные failure details. Ожидается RPC-ошибка, не произвольный local object;
// повтор и применение плана не выполняются.
func assistantPlanControlError(err error) error {
	if status.Code(err) == codes.InvalidArgument {
		planErr := &assistantPlanInputError{reason: "server_validation"}
		if diagnostic, ok := assistantPlanInvalidDetails(err); ok {
			planErr.diagnostic = &diagnostic
		}
		return planErr
	}
	if class, _ := assistantPlanFailureDiagnostic(err); class != "" {
		info := status.Convert(err).Details()[0].(*errdetails.ErrorInfo)
		withDetails, detailErr := status.New(status.Code(err), "propose assistant plan").WithDetails(info)
		if detailErr == nil {
			return withDetails.Err()
		}
	}
	return status.Error(status.Code(err), "propose assistant plan")
}

// normalizeServerHydratedAssistantOperation copies supported operation/parameter
// maps, resolves accepted aliases, binds selected project-scoped parameters and
// supplies presentation fields. Unsupported kinds/shapes remain for later guards;
// this normalization does not resolve current owner versions or permissions.
// normalizeServerHydratedAssistantOperation копирует поддержанные operation/
// parameter maps, разрешает принятые aliases, привязывает выбранные project
// parameters и задаёт presentation fields. Иные kinds/shapes проверяются позже;
// текущие owner versions и permissions здесь не разрешаются.
func normalizeServerHydratedAssistantOperation(operation map[string]any, planSummary, projectRef, projectName string) (map[string]any, error) {
	kind, _ := operation["type"].(string)
	if kind == "" {
		candidate, _ := operation["action"].(string)
		if assistantServerHydratedOperation(candidate) {
			kind = candidate
		}
	}
	if !assistantServerHydratedOperation(kind) {
		return operation, nil
	}
	parameters, ok := operation["parameters"].(map[string]any)
	if !ok {
		return operation, nil
	}
	normalizedParameters, err := normalizeAssistantParameterNames(parameters)
	if err != nil {
		return nil, invalidAssistantPlan("operation_parameter_alias")
	}
	if (kind == "CREATE_RUNTIME_ENVIRONMENT_DRAFT" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION") &&
		!assistantEnvironmentVariableNamesValid(normalizedParameters) {
		return nil, invalidAssistantPlan("environment_variable_name")
	}
	if assistantProjectScopedOperation(kind) && strings.TrimSpace(projectRef) != "" {
		normalizedParameters["projectRef"] = strings.TrimSpace(projectRef)
	}
	normalized := make(map[string]any, len(operation)+3)
	for key, value := range operation {
		normalized[key] = value
	}
	normalized["type"] = kind
	normalized["parameters"] = normalizedParameters
	if title, _ := normalized["title"].(string); strings.TrimSpace(title) == "" || kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT" || kind == "UPDATE_WORKFLOW" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || kind == "UPDATE_INTEGRATION_CONNECTION" || kind == "UPDATE_SCHEDULE" || kind == "UPDATE_ROLE_IMAGE_RECIPE" || kind == "PUBLISH_INTEGRATION_DEFINITION" || kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
		normalized["title"] = assistantOperationTitle(kind, normalizedParameters, projectName)
	}
	if operationSummary, _ := normalized["summary"].(string); strings.TrimSpace(operationSummary) == "" {
		normalized["summary"] = truncateRunes(planSummary, 500)
	}
	if kind == "UPDATE_PROJECT" {
		normalized["summary"] = assistantProjectUpdateSummary(normalizedParameters, projectName)
	}
	return normalized, nil
}

// assistantEnvironmentVariableNamesValid rejects invalid string names in the
// recognized env lists/removals. Wrong list/item/name types are deliberately
// left to later shape validation, so true is not a complete environment verdict.
// assistantEnvironmentVariableNamesValid отвергает неверные строковые имена
// в известных env lists/removals. Иные типы list/item/name проверяются позднее,
// поэтому true не является полным заключением об окружении.
func assistantEnvironmentVariableNamesValid(parameters map[string]any) bool {
	for _, field := range []string{"publicValues", "publicValueUpdates", "secretBindings"} {
		entries, supplied := parameters[field]
		if !supplied {
			continue
		}
		list, ok := entries.([]any)
		if !ok {
			continue
		}
		for _, entry := range list {
			item, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			name, ok := item["name"].(string)
			if ok && !runtimecontract.ValidRuntimeEnvironmentName(name) {
				return false
			}
		}
	}
	if entries, ok := parameters["publicValueRemovals"].([]any); ok {
		for _, entry := range entries {
			name, ok := entry.(string)
			if ok && !runtimecontract.ValidRuntimeEnvironmentName(name) {
				return false
			}
		}
	}
	return true
}

// assistantProjectScopedOperation selects operation kinds whose normalized
// parameters receive the immutable execution project; it grants no project access.
// assistantProjectScopedOperation выбирает kinds, получающие проект immutable
// execution в normalized parameters, не выдавая доступ к проекту.
func assistantProjectScopedOperation(kind string) bool {
	switch kind {
	case "UPDATE_PROJECT", "CREATE_PROJECT_FILE", "CREATE_AGENT", "CREATE_PROJECT_ASSISTANT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "CREATE_RUNTIME_ENVIRONMENT_DRAFT", "CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE":
		return true
	default:
		return false
	}
}

// assistantParameterAliases is the explicit accepted name-normalization table
// for proposal parameter maps. Recursive normalization rejects collisions instead
// of letting an alias overwrite another value; the table conveys no authority.
// assistantParameterAliases задаёт явные принятые соответствия имён parameters.
// Рекурсивная нормализация отвергает collisions вместо перезаписи другого
// значения alias-ключом; таблица не передаёт полномочий.
var assistantParameterAliases = map[string]string{
	"agent_ref": "agentRef", "artifact_refs": "artifactRefs", "avatar_url": "avatarUrl",
	"file_name": "fileName", "media_type": "mediaType",
	"capability_key": "capabilityKey", "completion_criteria": "completionCriteria",
	"connection_ref": "connectionRef", "coordinator_agent_ref": "coordinatorAgentRef",
	"day_of_week": "dayOfWeek", "definition_key": "definitionKey", "gate_decisions": "gateDecisions",
	"human_gate": "humanGate", "input_fields": "inputFields", "max_concurrency": "maxConcurrency",
	"image_artifact_ref": "imageArtifactRef", "environment_key": "environmentKey",
	"recipe_ref":            "recipeRef",
	"environment_ref":       "environmentRef",
	"public_value_updates":  "publicValueUpdates",
	"public_value_removals": "publicValueRemovals",
	"notification_policy":   "notificationPolicy", "parallel_group": "parallelGroup",
	"project_ref": "projectRef", "public_configuration": "publicConfiguration",
	"schedule_ref": "scheduleRef", "cron_expression": "cronExpression", "automation_text": "automationText",
	"required_capability_keys": "requiredCapabilityKeys", "role_definition_ref": "roleDefinitionRef",
	"role_description": "roleDescription", "runtime_ref": "runtimeRef", "session_policy": "sessionPolicy",
	"session_ref": "sessionRef", "target_ref": "targetRef", "target_type": "targetType",
	"time_of_day": "timeOfDay", "timeout_seconds": "timeoutSeconds", "value_type": "valueType",
	"workflow_ref": "workflowRef",
}

// normalizeAssistantParameterNames creates a new map with recognized key aliases,
// recursively processes supported nested values and rejects normalized-key
// collisions. Unknown keys remain unchanged for the operation-specific validator.
// normalizeAssistantParameterNames создаёт новую map с известными key aliases,
// рекурсивно обрабатывает вложенные значения и отвергает совпадение normalized
// keys. Неизвестные ключи оставляются operation-specific validator без изменения.
func normalizeAssistantParameterNames(value map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(value))
	for key, item := range value {
		normalizedKey := key
		if alias := assistantParameterAliases[key]; alias != "" {
			normalizedKey = alias
		}
		if _, duplicate := result[normalizedKey]; duplicate {
			return nil, errors.New("assistant parameter aliases conflict")
		}
		normalizedItem, err := normalizeAssistantParameterValue(item)
		if err != nil {
			return nil, err
		}
		result[normalizedKey] = normalizedItem
	}
	return result, nil
}

// normalizeAssistantParameterValue copies and normalizes map[string]any and
// []any recursively; other values are returned as-is, not deep-cloned or granted
// special semantics by normalization.
// normalizeAssistantParameterValue рекурсивно копирует и нормализует
// map[string]any и []any; другие значения возвращаются как есть,
// без deep clone и назначения особой семантики.
func normalizeAssistantParameterValue(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeAssistantParameterNames(typed)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			normalized, err := normalizeAssistantParameterValue(item)
			if err != nil {
				return nil, err
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return value, nil
	}
}

// assistantOperationTitle chooses a Russian operation label and selected
// name/context/project fallback, trimming and bounding the displayed result.
// It does not resolve resource identity or validate an otherwise unknown kind.
// assistantOperationTitle выбирает русский label и name/context/project fallback,
// обрезая пробелы и длину отображения. Идентичность ресурса не разрешается,
// неизвестный kind этим не проверяется.
func assistantOperationTitle(kind string, parameters map[string]any, entityName string) string {
	name, _ := parameters["name"].(string)
	if kind == "CREATE_PROJECT_FILE" {
		name, _ = parameters["fileName"].(string)
	}
	if (kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT" || kind == "UPDATE_WORKFLOW" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || kind == "UPDATE_INTEGRATION_CONNECTION" || kind == "UPDATE_SCHEDULE") && strings.TrimSpace(entityName) != "" {
		name = entityName
	}
	if strings.TrimSpace(name) == "" && kind != "UPDATE_ROLE_IMAGE_RECIPE" {
		name, _ = parameters["projectRef"].(string)
	}
	labels := map[string]string{
		"CREATE_PROJECT":                             "Создать Проект",
		"CREATE_PROJECT_FILE":                        "Создать файл",
		"CREATE_PROJECT_FILE_REVISION":               "Создать новую версию файла",
		"UPDATE_PROJECT":                             "Изменить Проект",
		"CREATE_AGENT":                               "Создать ИИ-сотрудника",
		"CREATE_PROJECT_ASSISTANT":                   "Настроить помощника Проекта",
		"UPDATE_AGENT":                               "Изменить ИИ-сотрудника",
		"CREATE_INSTRUCTION_DRAFT":                   "Подготовить инструкции ИИ-сотрудника",
		"BIND_AGENT_RUNTIME_ENVIRONMENT":             "Назначить окружение ИИ-сотрудника",
		"CREATE_WORKFLOW":                            "Создать Процесс",
		"CREATE_INTEGRATION_CONNECTION":              "Создать подключение",
		"UPDATE_INTEGRATION_CONNECTION":              "Изменить подключение",
		"CREATE_SCHEDULE":                            "Создать автоматизацию",
		"UPDATE_WORKFLOW":                            "Изменить процесс",
		"UPDATE_SCHEDULE":                            "Изменить автоматизацию",
		"CREATE_RUNTIME_ENVIRONMENT_DRAFT":           "Создать черновик среды",
		"PREPARE_RUNTIME_ENVIRONMENT_REVISION":       "Подготовить новую ревизию среды",
		"CREATE_ROLE_IMAGE_RECIPE":                   "Создать рецепт образа",
		"UPDATE_ROLE_IMAGE_RECIPE":                   "Изменить рецепт образа",
		"PUBLISH_INTEGRATION_DEFINITION":             "Опубликовать интеграцию",
		"UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":       "Изменить инструкции Kodex",
		"CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":  "Создать рецепт образа Kodex",
		"UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":  "Изменить рецепт образа Kodex",
		"PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":    "Подготовить настройку модели помощника",
		"PREPARE_AGENT_RUNTIME_CONFIGURATION":        "Подготовить настройку модели сотрудника",
		"CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT":  "Изменить права интеграции Kodex",
		"CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT": "Изменить права интеграции помощника проекта",
	}
	labels["PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION"] = "Подготовить подключение помощника Проекта"
	label := labels[kind]
	if strings.TrimSpace(name) == "" {
		return label
	}
	return truncateRunes(fmt.Sprintf("%s «%s»", label, strings.TrimSpace(name)), 200)
}

// assistantProjectUpdateSummary describes supplied nonblank name/purpose/language
// updates using a bounded Russian presentation string. It does not read actual
// previous values or claim the changes have already been committed.
// assistantProjectUpdateSummary описывает непустые name/purpose/language updates
// ограниченной русской строкой. Прежние значения не читаются,
// фиксация изменений не утверждается.
func assistantProjectUpdateSummary(parameters map[string]any, projectName string) string {
	changes := make([]string, 0, 3)
	for _, field := range []struct {
		key, label string
	}{{"name", "название"}, {"purpose", "назначение"}, {"language", "язык"}} {
		value, _ := parameters[field.key].(string)
		if strings.TrimSpace(value) != "" {
			changes = append(changes, fmt.Sprintf("%s: «%s»", field.label, strings.TrimSpace(value)))
		}
	}
	project := "Проект"
	if strings.TrimSpace(projectName) != "" {
		project = fmt.Sprintf("Проект «%s»", strings.TrimSpace(projectName))
	}
	if len(changes) == 0 {
		return "Изменить параметры " + project + "."
	}
	return truncateRunes("Изменить "+project+" — "+strings.Join(changes, "; ")+".", 500)
}

// assistantServerHydratedOperation identifies kinds whose proposal action,
// target and selection are constructed locally for owner hydration. It is not
// an allowlist granting execution or bypassing the owner's version/approval gate.
// assistantServerHydratedOperation определяет kinds с локальной подготовкой
// proposal action/target/selection для owner hydration. Это не выдача права
// выполнения и не обход owner version/approval gate.
func assistantServerHydratedOperation(kind string) bool {
	switch kind {
	case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", "PREPARE_AGENT_RUNTIME_CONFIGURATION", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT", "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION":
		return true
	case "CREATE_PROJECT", "CREATE_PROJECT_FILE", "CREATE_PROJECT_FILE_REVISION", "CREATE_AGENT", "CREATE_PROJECT_ASSISTANT", "CREATE_WORKFLOW", "CREATE_INTEGRATION_CONNECTION", "CREATE_SCHEDULE", "CREATE_RUNTIME_ENVIRONMENT_DRAFT", "CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE", "UPDATE_PROJECT", "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "CHANGE_CAPABILITY", "CHANGE_INTEGRATION_GRANT", "UPDATE_WORKFLOW", "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "UPDATE_INTEGRATION_CONNECTION", "UPDATE_SCHEDULE", "PUBLISH_INTEGRATION_DEFINITION", "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
		return true
	default:
		return false
	}
}

// assistantConfigurationParametersAllowed performs selected scope/shape checks
// for known special operations, including self locators, grants and model policy.
// Other kinds return true for later validation; even a successful local check
// leaves current ownership, versions, catalog eligibility and approval to CP.
// assistantConfigurationParametersAllowed выполняет выбранные scope/shape checks
// специальных операций, включая self locators, grants и model policy. Другие
// kinds переходят к поздней проверке; true не заменяет текущие ownership,
// versions, catalog eligibility и approval у CP.
func assistantConfigurationParametersAllowed(input runtimecontract.RunnerInput, kind string, parameters map[string]any) bool {
	if _, supplied := parameters["projectAssistantRef"]; supplied && kind != "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION" && kind != "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT" {
		return projectAssistantLocatorParametersAllowed(input, kind, parameters)
	}
	switch kind {
	case "CREATE_PROJECT_FILE_REVISION":
		if !assistantFileRevisionContextAllowed(input) || !onlyKeys(parameters, "artifactRef", "mediaType", "contentEncoding", "content") ||
			!assistantRequiredStrings(parameters, "artifactRef", "mediaType") {
			return false
		}
		content, ok := parameters["content"].(string)
		encoding, _ := parameters["contentEncoding"].(string)
		mediaType, _ := parameters["mediaType"].(string)
		if !ok || len(content) > 1<<20 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) ||
			(encoding != "" && encoding != "UTF8") || !slices.Contains([]string{"text/plain", "text/markdown", "text/csv", "application/json"}, mediaType) {
			return false
		}
		return true
	case "CHANGE_CAPABILITY":
		if !input.IsAssistant() || !onlyKeys(parameters, "agentRef", "capabilityKey", "enabled") || !assistantRequiredStrings(parameters, "agentRef", "capabilityKey") {
			return false
		}
		if _, ok := parameters["enabled"].(bool); !ok {
			return false
		}
		key := parameters["capabilityKey"].(string)
		if key != "platform.artifact.manage" && key != "platform.run.delegate" && key != "platform.run.launch" {
			return false
		}
		if input.AssistantScope == runtimecontract.AssistantScopeProject && input.AgentRef != "" && parameters["agentRef"] == input.AgentRef {
			return true
		}
		context := input.AssistantContext
		return context != nil && context.EntityKind == "AGENT" && context.EntityRef == parameters["agentRef"] && slices.Contains(context.AllowedOperations, kind)
	case "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT":
		if input.AssistantScope != runtimecontract.AssistantScopeProject || input.AgentRef == "" || parameters["projectAssistantRef"] != input.AgentRef ||
			!onlyKeys(parameters, "projectAssistantRef", "connectionRef", "capabilityKey", "enabled", "approvalPolicy", "approvalScopePaths") {
			return false
		}
		// Общая проверка закрытой формы policy без изменения runtime authority.
		bounded := make(map[string]any, len(parameters)-1)
		for key, value := range parameters {
			if key != "projectAssistantRef" {
				bounded[key] = value
			}
		}
		return assistantRequiredStrings(bounded, "connectionRef", "capabilityKey", "approvalPolicy") && assistantIntegrationGrantPolicyShape(bounded)
	case "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION":
		if input.AssistantScope != runtimecontract.AssistantScopeProject || input.AgentRef == "" || parameters["projectAssistantRef"] != input.AgentRef ||
			!onlyKeys(parameters, "projectAssistantRef", "definitionKey", "name", "publicConfiguration") || !assistantRequiredStrings(parameters, "projectAssistantRef", "definitionKey", "name") {
			return false
		}
		configuration, valid := parameters["publicConfiguration"].(map[string]any)
		if !valid || len(configuration) > 100 {
			return false
		}
		for _, value := range configuration {
			text, valid := value.(string)
			if !valid || len(text) > 4096 {
				return false
			}
		}
		return len(parameters["name"].(string)) <= 160
	case "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT":
		if !input.IsSystemAssistant() || input.AgentRef == "" || !onlyKeys(parameters, "connectionRef", "capabilityKey", "enabled", "approvalPolicy", "approvalScopePaths") ||
			!assistantRequiredStrings(parameters, "connectionRef", "capabilityKey", "approvalPolicy") {
			return false
		}
		return assistantIntegrationGrantPolicyShape(parameters)
	case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":
		if !input.IsSystemAssistant() || input.AgentRef == "" || parameters == nil || parameters["systemAssistantRef"] != input.AgentRef {
			return false
		}
		if kind == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
			return onlyKeys(parameters, "systemAssistantRef", "name", "environmentKey", "dockerfile") &&
				assistantRequiredStrings(parameters, "name", "environmentKey") && assistantOptionalStrings(parameters, "dockerfile")
		}
		return onlyKeys(parameters, "systemAssistantRef", "recipeRef", "name", "environmentKey", "dockerfile") &&
			assistantRequiredStrings(parameters, "recipeRef") && assistantOptionalStrings(parameters, "name", "environmentKey", "dockerfile") && len(parameters) > 2
	case "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", "PREPARE_AGENT_RUNTIME_CONFIGURATION":
		if !input.IsAssistant() || input.AgentRef == "" || parameters == nil ||
			!onlyKeys(parameters, "agentRef", "runtimeProfileRef", "model", "reasoningEffort", "webSearchMode", "providerPolicyMode", "providerAccounts") ||
			!assistantRequiredStrings(parameters, "agentRef", "runtimeProfileRef", "model", "providerPolicyMode") {
			return false
		}
		if kind == "PREPARE_AGENT_RUNTIME_CONFIGURATION" {
			if input.AssistantScope != runtimecontract.AssistantScopeProject || input.ProjectRef == "" || parameters["agentRef"] == input.AgentRef {
				return false
			}
		} else if input.AssistantScope == runtimecontract.AssistantScopeProject && parameters["agentRef"] != input.AgentRef {
			return false
		}
		effort, ok := parameters["reasoningEffort"].(string)
		if raw, present := parameters["webSearchMode"]; present {
			searchMode, valid := raw.(string)
			if !valid || !runtimecontract.ValidWebSearchMode(searchMode) {
				return false
			}
		}
		if !ok || effort != "" && runtimecontract.ValidateEffectiveReasoningEffort("", effort, runtimecontract.ReasoningSupported) != nil {
			return false
		}
		mode := parameters["providerPolicyMode"].(string)
		if mode != "FIXED" && mode != "LEAST_USED" && mode != "WEIGHTED" {
			return false
		}
		accounts, ok := parameters["providerAccounts"].([]any)
		if !ok || len(accounts) < 1 || len(accounts) > 128 || mode == "FIXED" && len(accounts) != 1 {
			return false
		}
		for _, raw := range accounts {
			account, ok := raw.(map[string]any)
			if !ok || !onlyKeys(account, "accountRef", "weight") || !assistantRequiredStrings(account, "accountRef") {
				return false
			}
			weight, ok := exactJSONInt64(account["weight"])
			if !ok || weight > 100 || mode != "WEIGHTED" && weight != 1 {
				return false
			}
		}
		return true
	default:
		return true
	}
}

// assistantFileRevisionContextAllowed filters the immutable execution snapshot
// by assistant/artifact capability, project and allowed operation/context kind.
// It neither issues artifact authority nor verifies the current target/version.
// assistantFileRevisionContextAllowed фильтрует immutable execution по assistant/
// artifact capability, проекту и allowed operation/context kind,
// не выдавая artifact authority и не проверяя текущие target/version.
func assistantFileRevisionContextAllowed(input runtimecontract.RunnerInput) bool {
	context := input.AssistantContext
	return input.IsAssistant() && slices.Contains(input.Capabilities, runtimecontract.ArtifactCapability) &&
		input.ProjectRef != "" && context != nil && slices.Contains(context.AllowedOperations, "CREATE_PROJECT_FILE_REVISION") &&
		(input.AssistantScope == runtimecontract.AssistantScopeProject || input.IsSystemAssistant() && (context.EntityKind == "PROJECT" || context.EntityKind == "FILE"))
}

// assistantIntegrationGrantPolicyShape checks enabled/approval enum consistency
// and at most sixteen unique bounded scope strings, requiring scopes only for
// enabled HUMAN_SCOPED. It does not parse scope-path semantics or grant rights.
// assistantIntegrationGrantPolicyShape проверяет enabled/approval enum и не более
// шестнадцати уникальных ограниченных scopes, требуя их лишь для включённого
// HUMAN_SCOPED. Семантика путей и предоставление прав здесь не проверяются.
func assistantIntegrationGrantPolicyShape(parameters map[string]any) bool {
	enabled, ok := parameters["enabled"].(bool)
	if !ok {
		return false
	}
	policy, valid := parameters["approvalPolicy"].(string)
	if !valid {
		return false
	}
	if policy != "NONE" && policy != "HUMAN_EACH_EFFECT" && policy != "HUMAN_SCOPED" {
		return false
	}
	if raw, supplied := parameters["approvalScopePaths"]; supplied {
		paths, ok := assistantGrantScopePaths(raw)
		if !ok || len(paths) > 16 || policy == "HUMAN_SCOPED" && enabled && len(paths) == 0 || (policy != "HUMAN_SCOPED" || !enabled) && len(paths) != 0 {
			return false
		}
		seen := map[string]bool{}
		for _, path := range paths {
			if len(path) < 1 || len(path) > 200 || seen[path] {
				return false
			}
			seen[path] = true
		}
	} else if policy == "HUMAN_SCOPED" && enabled {
		return false
	}
	return true
}

// projectAssistantLocatorOperation recognizes the three locator-based assistant
// configuration kinds only when projectAssistantRef is supplied; type, ownership
// and current profile validity are checked elsewhere.
// projectAssistantLocatorOperation распознаёт три locator-based kind настройки
// лишь при наличии projectAssistantRef; тип, принадлежность и актуальность
// профиля проверяются отдельно.
func projectAssistantLocatorOperation(kind string, parameters map[string]any) bool {
	_, supplied := parameters["projectAssistantRef"]
	return supplied && (kind == "CREATE_INSTRUCTION_DRAFT" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION")
}

// projectAssistantLocatorParametersAllowed checks assistant/self locator shape,
// allowed schema property names and selected instruction/environment requirements.
// Its revision anyOf check tests presence only, not full JSON Schema semantics;
// current resource resolution and authorization remain owner operations.
// projectAssistantLocatorParametersAllowed проверяет assistant/self locator,
// допустимые schema property names и выбранные instruction/environment поля.
// Revision anyOf проверяет лишь наличие, не всю JSON Schema;
// актуальные ресурсы и полномочия разрешает владелец.
func projectAssistantLocatorParametersAllowed(input runtimecontract.RunnerInput, kind string, parameters map[string]any) bool {
	ref, ok := parameters["projectAssistantRef"].(string)
	if !ok || !input.IsAssistant() || input.AgentRef == "" || !validAssistantResourceRef(ref) ||
		input.AssistantScope == runtimecontract.AssistantScopeProject && ref != input.AgentRef || !projectAssistantLocatorOperation(kind, parameters) {
		return false
	}
	schema := projectAssistantOperationParameters(input, kind)
	keys := make([]string, 0, len(schema["properties"].(map[string]any)))
	for key := range schema["properties"].(map[string]any) {
		keys = append(keys, key)
	}
	if !onlyKeys(parameters, keys...) {
		return false
	}
	switch kind {
	case "CREATE_INSTRUCTION_DRAFT":
		instructions, ok := parameters["instructions"].(string)
		return ok && assistantPlanTextWithinLimit(instructions, 65536) && utf8.RuneCountInString(instructions) >= 20
	case "BIND_AGENT_RUNTIME_ENVIRONMENT":
		environmentRef, ok := parameters["environmentRef"].(string)
		return ok && validAssistantResourceRef(environmentRef)
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		if raw, exists := parameters["environmentRef"]; exists {
			environmentRef, ok := raw.(string)
			if !ok || !validAssistantResourceRef(environmentRef) {
				return false
			}
		}
		for _, branch := range schema["anyOf"].([]map[string]any) {
			if _, supplied := parameters[branch["required"].([]string)[0]]; supplied {
				return true
			}
		}
	}
	return false
}

// assistantGrantScopePaths accepts []string without copying or converts []any
// only when every element is a string. Count/path constraints belong to callers,
// and accepted strings are not interpreted as capabilities.
// assistantGrantScopePaths принимает []string без копирования либо переводит
// []any, если каждый элемент строковый. Число и форма путей проверяются caller;
// принятые строки не интерпретируются как capabilities.
func assistantGrantScopePaths(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		return values, true
	case []any:
		result := make([]string, len(values))
		for index, value := range values {
			item, ok := value.(string)
			if !ok {
				return nil, false
			}
			result[index] = item
		}
		return result, true
	default:
		return nil, false
	}
}

// assistantRequiredStrings requires each selected field to be a nonblank string,
// treating missing or wrong types as failure without altering the stored text.
// assistantRequiredStrings требует непустую строку для каждого выбранного поля,
// отвергая отсутствие и неверные типы без изменения сохранённого текста.
func assistantRequiredStrings(parameters map[string]any, fields ...string) bool {
	for _, field := range fields {
		value, ok := parameters[field].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// assistantOptionalStrings permits absent fields but applies the required-string
// check to every supplied selected field; an explicitly blank value fails.
// assistantOptionalStrings допускает отсутствие полей, но применяет проверку
// обязательной строки к переданным; явно пустое значение отвергается.
func assistantOptionalStrings(parameters map[string]any, fields ...string) bool {
	for _, field := range fields {
		if _, exists := parameters[field]; exists && !assistantRequiredStrings(parameters, field) {
			return false
		}
	}
	return true
}

// assistantServerAction maps known revision/update/grant kinds to UPDATE and
// all remaining kinds to CREATE. Kind validation and owner command selection
// happen separately; this presentation mapping grants no mutation permission.
// assistantServerAction отображает известные revision/update/grant kinds в UPDATE,
// остальные — в CREATE. Kind и owner command проверяются отдельно;
// такое отображение не даёт права изменения.
func assistantServerAction(kind string) string {
	if kind == "CREATE_PROJECT_FILE_REVISION" {
		return "UPDATE"
	}
	if kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || (kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" || kind == "PREPARE_AGENT_RUNTIME_CONFIGURATION") || kind == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" || kind == "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT" {
		return "UPDATE"
	}
	if kind == "UPDATE_PROJECT" || kind == "UPDATE_AGENT" || kind == "CREATE_INSTRUCTION_DRAFT" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" || kind == "CHANGE_CAPABILITY" || kind == "CHANGE_INTEGRATION_GRANT" || kind == "UPDATE_WORKFLOW" || kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || kind == "UPDATE_INTEGRATION_CONNECTION" || kind == "UPDATE_SCHEDULE" || kind == "UPDATE_ROLE_IMAGE_RECIPE" || kind == "PUBLISH_INTEGRATION_DEFINITION" || kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
		return "UPDATE"
	}
	return "CREATE"
}

// assistantOperationTargetContext derives project-assistant self context only
// for exact agent/environment locators in the immutable input, otherwise returns
// the existing context pointer. It constructs target metadata, not fresh authority.
// assistantOperationTargetContext выводит project-assistant self context лишь
// по точным agent/environment locators immutable input, иначе возвращает прежний
// указатель context. Это target metadata, не новое полномочие.
func assistantOperationTargetContext(input runtimecontract.RunnerInput, kind string, parameters map[string]any) *runtimecontract.RunnerAssistantContext {
	if input.AssistantScope != runtimecontract.AssistantScopeProject {
		return input.AssistantContext
	}
	if kind == "UPDATE_AGENT" || kind == "CREATE_INSTRUCTION_DRAFT" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" || kind == "CHANGE_CAPABILITY" {
		requested, _ := parameters["agentRef"].(string)
		if requested != "" && requested == input.AgentRef {
			return &runtimecontract.RunnerAssistantContext{EntityKind: "AGENT", EntityRef: input.AgentRef, EntityName: input.AgentRef}
		}
	}
	if kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
		requested, _ := parameters["environmentRef"].(string)
		if requested != "" && requested == input.RuntimeEnvironmentRef {
			return &runtimecontract.RunnerAssistantContext{EntityKind: "ENVIRONMENT", EntityRef: input.RuntimeEnvironmentRef, EntityName: input.RuntimeEnvironmentRef}
		}
	}
	return input.AssistantContext
}

// assistantServerTarget builds the proposal's kind/name locator from supported
// parameters and exact supplied context matches, returning nil when required
// context is absent. It intentionally does not assign current target refs or
// versions; the owner hydrates and authorizes them before any application.
// assistantServerTarget собирает proposal kind/name locator по поддержанным
// parameters и точным context matches, возвращая nil при отсутствии нужного
// контекста. Текущие target refs и versions не назначаются;
// владелец разрешает и авторизует их до применения.
func assistantServerTarget(kind string, parameters map[string]any, context *runtimecontract.RunnerAssistantContext) map[string]any {
	if kind == "CREATE_PROJECT_FILE_REVISION" {
		ref, _ := parameters["artifactRef"].(string)
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		return map[string]any{"kind": "ARTIFACT", "name": strings.TrimSpace(ref)}
	}
	if parameters == nil {
		return nil
	}
	if projectAssistantLocatorOperation(kind, parameters) {
		targetKind := "AGENT"
		if kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			targetKind = "ENVIRONMENT"
		}
		return map[string]any{"kind": targetKind, "name": parameters["projectAssistantRef"]}
	}
	targetKind := strings.TrimPrefix(kind, "CREATE_")
	if kind == "PREPARE_PROJECT_ASSISTANT_INTEGRATION_CONNECTION" {
		ref, _ := parameters["projectAssistantRef"].(string)
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		return map[string]any{"kind": "PROJECT_ASSISTANT", "name": ref}
	}
	if kind == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" || kind == "PREPARE_AGENT_RUNTIME_CONFIGURATION" {
		ref, _ := parameters["agentRef"].(string)
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		return map[string]any{"kind": "AGENT", "name": strings.TrimSpace(ref)}
	}
	if kind == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		targetKind = "ROLE_IMAGE_RECIPE"
	}
	if kind == "CREATE_PROJECT_FILE" {
		name, _ := parameters["fileName"].(string)
		if strings.TrimSpace(name) == "" {
			return nil
		}
		return map[string]any{"kind": "ARTIFACT", "name": strings.TrimSpace(name)}
	} else if kind == "UPDATE_PROJECT" {
		targetKind = "PROJECT"
	} else if kind == "UPDATE_ROLE_IMAGE_RECIPE" || kind == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		ref, _ := parameters["recipeRef"].(string)
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		return map[string]any{"kind": "ROLE_IMAGE_RECIPE", "name": strings.TrimSpace(ref)}
	} else if kind == "PUBLISH_INTEGRATION_DEFINITION" {
		ref, _ := parameters["configurationRef"].(string)
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		return map[string]any{"kind": "INTEGRATION_DEFINITION", "name": strings.TrimSpace(ref)}
	} else if kind == "UPDATE_AGENT" || kind == "CREATE_INSTRUCTION_DRAFT" || kind == "BIND_AGENT_RUNTIME_ENVIRONMENT" || kind == "CHANGE_CAPABILITY" {
		requestedRef, _ := parameters["agentRef"].(string)
		if context == nil || context.EntityKind != "AGENT" || context.EntityRef == "" ||
			context.EntityRef != strings.TrimSpace(requestedRef) || context.EntityName == "" {
			return nil
		}
		return map[string]any{"kind": "AGENT", "name": context.EntityName}
	} else if kind == "UPDATE_INTEGRATION_CONNECTION" {
		requestedRef, _ := parameters["connectionRef"].(string)
		if context == nil || context.EntityKind != "INTEGRATION_CONNECTION" || context.EntityRef == "" ||
			context.EntityRef != strings.TrimSpace(requestedRef) || context.EntityName == "" {
			return nil
		}
		return map[string]any{"kind": "INTEGRATION_CONNECTION", "name": context.EntityName}
	} else if kind == "CHANGE_INTEGRATION_GRANT" || kind == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" || kind == "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT" {
		connectionRef, _ := parameters["connectionRef"].(string)
		if strings.TrimSpace(connectionRef) == "" {
			return nil
		}
		return map[string]any{"kind": "INTEGRATION_CONNECTION", "name": strings.TrimSpace(connectionRef)}
	} else if kind == "UPDATE_SCHEDULE" {
		requestedRef, _ := parameters["scheduleRef"].(string)
		if context == nil || context.EntityKind != "SCHEDULE" || context.EntityRef == "" ||
			context.EntityRef != strings.TrimSpace(requestedRef) || context.EntityName == "" {
			return nil
		}
		return map[string]any{"kind": "SCHEDULE", "name": context.EntityName}
	} else if kind == "UPDATE_WORKFLOW" {
		requestedRef, _ := parameters["workflowRef"].(string)
		if context == nil || context.EntityKind != "WORKFLOW" || context.EntityRef == "" ||
			context.EntityRef != strings.TrimSpace(requestedRef) || context.EntityName == "" {
			return nil
		}
		return map[string]any{"kind": "WORKFLOW", "name": context.EntityName}
	} else if kind == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
		requestedRef, _ := parameters["environmentRef"].(string)
		systemAssistantRef, _ := parameters["systemAssistantRef"].(string)
		if strings.TrimSpace(systemAssistantRef) != "" && strings.TrimSpace(requestedRef) != "" {
			return map[string]any{"kind": "ENVIRONMENT", "name": "Среда Kodex"}
		}
		if context == nil || context.EntityKind != "ENVIRONMENT" || context.EntityRef == "" ||
			context.EntityRef != strings.TrimSpace(requestedRef) || context.EntityName == "" {
			return nil
		}
		return map[string]any{"kind": "ENVIRONMENT", "name": context.EntityName}
	} else if kind == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
		requestedRef, _ := parameters["systemAssistantRef"].(string)
		if strings.TrimSpace(requestedRef) == "" {
			return nil
		}
		return map[string]any{"kind": "SYSTEM_ASSISTANT", "name": "Kodex"}
	}
	name, _ := parameters["name"].(string)
	if strings.TrimSpace(name) == "" {
		name, _ = parameters["projectRef"].(string)
	}
	if targetKind == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	return map[string]any{"kind": targetKind, "name": strings.TrimSpace(name)}
}

// exactJSONInt64 accepts only integral float64 values from generic JSON decoding,
// from one through the largest exactly representable safe JSON integer. It does
// not coerce Go integers, json.Number, numeric strings, zero or negative values.
// exactJSONInt64 принимает лишь целый float64 из общего JSON-decoding,
// от единицы до наибольшего безопасного JSON integer. Go integers, json.Number,
// строковые числа, ноль и отрицательные значения не приводятся.
func exactJSONInt64(value any) (int64, bool) {
	number, ok := value.(float64)
	if !ok || number < 1 || number > 9007199254740991 || number != float64(int64(number)) {
		return 0, false
	}
	return int64(number), true
}

// proposeAssistantMetadata validates an assistant title and sends a trimmed,
// lease-fenced idempotent metadata proposal. It requires a conversation ref and
// returns its title revision, not an unrestricted conversation-update permission.
// proposeAssistantMetadata проверяет заголовок помощника и отправляет trimmed,
// lease-fenced idempotent proposal. Требует conversation ref и возвращает
// title revision, не неограниченное право изменения диалога.
func (server *Server) proposeAssistantMetadata(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !input.IsAssistant() || !onlyKeys(arguments, "title") {
		return nil, errors.New("assistant metadata tool is not available")
	}
	title, _ := arguments["title"].(string)
	if strings.TrimSpace(title) == "" || len([]rune(title)) > 160 {
		return nil, errors.New("assistant metadata is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.ProposeAssistantMetadata(requestContext, &controlplanev1.ProposeAssistantMetadataRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Title: strings.TrimSpace(title),
	})
	if err != nil || response.GetConversation().GetRef() == "" {
		return nil, errors.New("propose assistant metadata")
	}
	return map[string]any{"ok": true, "conversation_ref": response.GetConversation().GetRef(), "title_revision": response.GetConversation().GetTitleRevision()}, nil
}

// proposeRunMetadata requires title or activity text within rune bounds and
// submits only trimmed presentation fields with lease fencing/idempotency.
// An owner run ref is required; task execution and authority are not changed here.
// proposeRunMetadata требует title либо activity в пределах rune bounds
// и передаёт trimmed presentation fields с lease fencing/idempotency.
// Нужна owner run ref; выполнение задачи и полномочия здесь не изменяются.
func (server *Server) proposeRunMetadata(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !onlyKeys(arguments, "title", "activity_summary") {
		return nil, errors.New("run metadata is invalid")
	}
	title, _ := arguments["title"].(string)
	activity, _ := arguments["activity_summary"].(string)
	if strings.TrimSpace(title) == "" && strings.TrimSpace(activity) == "" || len([]rune(title)) > 240 || len([]rune(activity)) > 500 {
		return nil, errors.New("run metadata is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.ProposeRunMetadata(requestContext, &controlplanev1.ProposeRunMetadataRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		Title: strings.TrimSpace(title), ActivitySummary: strings.TrimSpace(activity),
	})
	if err != nil || response.GetRun().GetRef() == "" {
		return nil, errors.New("propose run metadata")
	}
	return map[string]any{"ok": true, "run_ref": response.GetRun().GetRef()}, nil
}

// onlyKeys rejects keys outside the supplied set but does not require any key;
// nil/empty maps pass, so required fields need separate checks.
// onlyKeys отвергает ключи вне заданного набора, но не требует ни одного;
// nil/empty map проходит, обязательные поля проверяются отдельно.
func onlyKeys(values map[string]any, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range values {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

// recordToolCall records revision-two completion activity through the same
// safe projection path used for the running phase, preserving the tool outcome.
// recordToolCall записывает revision-two completion activity тем же безопасным
// путём, что running phase, сохраняя результат инструмента.
func (server *Server) recordToolCall(ctx context.Context, input runtimecontract.RunnerInput, tool string, arguments map[string]any,
	result any, toolErr error, callID json.RawMessage, duration time.Duration,
) error {
	return server.recordToolCallPhase(ctx, input, tool, arguments, result, toolErr, callID, duration, 2)
}

// recordToolCallPhase filters parameters, selects safe result commitments and
// submits a stable lease-bound activity revision, requiring an owner event ref.
// Revision one is RUNNING; later status follows toolErr, not business OK inside
// an integration result. Persistence failure does not undo an external effect.
// recordToolCallPhase фильтрует parameters, выбирает безопасные commitments
// и передаёт стабильную lease-bound activity revision с обязательной owner event
// ref. Первая revision — RUNNING; дальнейший status зависит от toolErr, не business
// OK внутри integration result. Ошибка записи не отменяет внешний effect.
func (server *Server) recordToolCallPhase(ctx context.Context, input runtimecontract.RunnerInput, tool string, arguments map[string]any,
	result any, toolErr error, callID json.RawMessage, duration time.Duration, revision int64,
) error {
	parameters, capabilityRef, grantRef, ok := safeToolCallParameters(input, tool, arguments)
	if !ok {
		return errors.New("record tool call projection")
	}
	structure, err := structpb.NewStruct(parameters)
	if err != nil {
		return errors.New("record tool call projection")
	}
	state := controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_SUCCEEDED
	if toolErr != nil {
		state = controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED
	}
	safeResult := safeToolCallResult(tool, result, toolErr)
	if tool == runtimecontract.FileToolRead && revision == 2 && toolErr == nil {
		safeResult, err = safeFileReadReceipt(input, arguments, result)
		if err != nil {
			return err
		}
	}
	if tool == "get_workflow_catalog" && revision == 2 && toolErr == nil {
		safeResult, err = safeWorkflowCatalogReceipt(input, arguments, result)
		if err != nil {
			return err
		}
	}
	if revision == 1 {
		state, safeResult = controlplanev1.RunToolCallState_RUN_TOOL_CALL_STATE_RUNNING, ""
	}
	digest := sha256.Sum256([]byte(stableKey(input.LeaseRef, string(callID))))
	callRef := "tcl_" + hex.EncodeToString(digest[:16])
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.RecordRunToolCall(requestContext, &controlplanev1.RecordRunToolCallRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID)+":activity:"+strconv.FormatInt(revision, 10))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		CallRef: callRef, Tool: tool, SafeParameters: structure, CapabilityRef: capabilityRef, GrantRef: grantRef,
		State: state, DurationMs: duration.Milliseconds(), SafeResult: safeResult, Revision: revision,
	})
	if err != nil {
		server.logger.WarnContext(ctx, "control-plane tool projection request failed",
			"tool", tool, "grpc_code", status.Code(err).String(), "failure_class", controlFailureClass(err))
		return status.Error(status.Code(err), "record tool call projection")
	}
	if response.GetEvent().GetRef() == "" {
		return errors.New("record tool call projection")
	}
	return nil
}

// safeToolCallParameters selects per-tool audit fields and capability/grant
// locators without copying arbitrary request bodies or search/file contents.
// Its bool is local projection eligibility; current owner permissions and exact
// resource checks still apply when recording or invoking the operation.
// safeToolCallParameters выбирает audit fields и capability/grant locators,
// не копируя произвольные request bodies и search/file content. Bool означает
// локальную допустимость проекции; запись и вызов сохраняют owner permission
// и exact resource checks.
func safeToolCallParameters(input runtimecontract.RunnerInput, tool string, arguments map[string]any) (map[string]any, string, string, bool) {
	if runtimecontract.IsRuntimeFileTool(tool) {
		purpose, _ := arguments["purpose"].(string)
		_, ok := runtimeFilePurpose(input, purpose)
		if !ok {
			return nil, "", "", false
		}
		return map[string]any{"purpose": purpose}, "", input.FileCatalog.Ref, true
	}
	switch tool {
	case runtimecontract.ExecutionSnapshotTool:
		return map[string]any{}, "", "", len(arguments) == 0 && runtimecontract.RuntimeExecutionSnapshotAvailable(input)
	case "get_configuration_catalog":
		parameters := map[string]any{}
		if catalog, ok := arguments["assistant_configuration_catalog"].(map[string]any); ok {
			if kind, ok := catalog["kind"].(string); ok && assistantConfigurationCatalogKindKnown(kind) {
				// Вид и проверенные координаты запроса страницы не раскрывают ресурс или содержимое.
				parameters["catalogKind"] = kind
				if kind == "WORKFLOW_CONFIGURATION" || kind == "WORKFLOW_METADATA" || kind == "AGENT_CONFIGURATION" || kind == "AGENT_RUNTIME_CONFIGURATION" {
					if _, err := configurationCatalog(input, arguments); err == nil {
						if _, err := parseAssistantConfigurationCatalog(input, arguments, catalog); err == nil {
							if page, err := parseAssistantConfigurationPage(catalog, kind); err == nil {
								parameters["offset_bytes"] = page.offset
								parameters["maximum_bytes"] = page.maximum
							}
						}
					}
				}
			}
		}
		return parameters, "platform.configuration.read", "", input.IsAssistant()
	case "get_integration_catalog":
		return map[string]any{}, "platform.integration.catalog", "", len(input.IntegrationGrants) != 0
	case "find_platform_resources":
		return map[string]any{}, "platform.resources.search", "", input.IsAssistant()
	case "read_task_session":
		return map[string]any{}, "platform.resources.search", "", input.IsAssistant()
	case "propose_configuration_plan":
		operations, _ := arguments["operations"].([]any)
		parameters := map[string]any{"operation_count": len(operations)}
		if len(operations) > 0 && len(operations) <= 32 {
			allowed := assistantOperationTypes(input)
			types := make([]any, 0, len(operations))
			for _, raw := range operations {
				operation, ok := raw.(map[string]any)
				kind, _ := operation["type"].(string)
				if !ok || !slices.Contains(allowed, kind) {
					types = nil
					break
				}
				types = append(types, kind)
			}
			if len(types) == len(operations) {
				parameters["operation_types"] = types
			}
		}
		return parameters, "platform.configuration.plan", "", input.IsAssistant()
	case "propose_assistant_metadata":
		title, _ := arguments["title"].(string)
		return map[string]any{"title": truncateRunes(title, 160)}, "platform.presentation.propose", "", input.IsAssistant()
	case "propose_run_metadata":
		title, _ := arguments["title"].(string)
		activity, _ := arguments["activity_summary"].(string)
		return map[string]any{"title": truncateRunes(title, 240), "activity_summary": truncateRunes(activity, 500)}, "platform.presentation.propose", "", true
	case "delegate_agent":
		target, _ := arguments["target_agent_ref"].(string)
		step, _ := arguments["workflow_step_key"].(string)
		return map[string]any{"target_agent_ref": target, "workflow_step_key": step}, "platform.run.delegate", "", true
	case "launch_workflow":
		workflow, _ := arguments["workflow_ref"].(string)
		return map[string]any{"workflow_ref": workflow}, "platform.run.launch", "", workflowLaunchAvailable(input)
	case "get_workflow_catalog":
		return workflowCatalogSafeParameters(arguments), "platform.run.launch", "", workflowLaunchAvailable(input)
	case repositorybundle.ToolName:
		if grant, ok := repositoryBundleGrant(input, arguments); ok {
			return map[string]any{"connection_ref": grant.ConnectionRef, "capability_key": grant.CapabilityKey}, grant.CapabilityKey, grant.Ref, true
		}
	case "invoke_integration":
		if grant, ok := integrationGrantForCall(input, arguments); ok {
			return map[string]any{"connection_ref": grant.ConnectionRef, "capability_key": grant.CapabilityKey}, grant.CapabilityKey, grant.Ref, true
		}
	case runtimecontract.Context7ResolveTool, runtimecontract.Context7QueryTool:
		invokeArguments, err := managedMCPArguments(input, tool, arguments)
		if err == nil {
			if grant, ok := integrationGrantForCall(input, invokeArguments); ok {
				return map[string]any{"connection_ref": grant.ConnectionRef, "capability_key": grant.CapabilityKey}, grant.CapabilityKey, grant.Ref, true
			}
		}
	}
	return nil, "", "", false
}

// integrationToolResult carries an owner-observed invocation outcome, including
// pending approval or unknown external outcome, with private audit commitments.
// Agent JSON and durable activity use different projections; this value neither
// approves effects nor proves that every reported state is terminal.
// integrationToolResult хранит наблюдённый у владельца invocation outcome,
// включая ожидание approval и неизвестный внешний outcome, с private commitments.
// Agent JSON и durable activity имеют разные проекции; значение не одобряет
// effects и не доказывает terminal-состояние каждого результата.
type integrationToolResult struct {
	OK                    bool   `json:"ok"`
	Result                string `json:"result,omitempty"`
	ErrorCode             string `json:"error_code,omitempty"`
	OwnerDecisionRequired bool   `json:"owner_decision_required,omitempty"`
	InvocationRef         string `json:"invocationRef"`
	state                 string
	inputSHA256           string
}

// MarshalJSON emits result on OK or error_code otherwise, plus invocationRef
// and optional owner-decision flag. Private state/input digest remain absent;
// serialization itself does not validate or authorize an invocation.
// MarshalJSON выдаёт result при OK либо error_code, invocationRef и необязательный
// owner-decision flag. Private state/input digest не выводятся;
// сериализация не проверяет и не авторизует invocation.
func (r integrationToolResult) MarshalJSON() ([]byte, error) {
	value := map[string]any{"ok": r.OK, "invocationRef": r.InvocationRef}
	if r.OK {
		value["result"] = r.Result
	} else {
		value["error_code"] = r.ErrorCode
	}
	if r.OwnerDecisionRequired {
		value["owner_decision_required"] = true
	}
	return json.Marshal(value)
}

// safeInvocationRef recognizes an eight-to-128-byte ASCII alphanumeric,
// underscore/hyphen locator; it does not prove existence, tenant or provenance.
// safeInvocationRef распознаёт ASCII alphanumeric locator длиной 8..128 bytes
// с underscore/hyphen, не доказывая существование, tenant и provenance.
func safeInvocationRef(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// safeToolCallResult returns fixed failures, selected reference markers or
// private-evidence commitments rather than raw tool/provider content. Successful
// file/workflow reads need specialized receipts in recordToolCallPhase; invocation
// receipts require an exact digest/reference and recognized owner-observed state.
// safeToolCallResult возвращает фиксированные ошибки, выбранные reference markers
// либо private-evidence commitments вместо raw tool/provider content. Для file/
// workflow reads нужны специальные receipts в recordToolCallPhase; invocation
// receipts требуют точные digest/ref и известное наблюдённое owner-состояние.
func safeToolCallResult(tool string, result any, toolErr error) string {
	if toolErr != nil {
		var planErr *assistantPlanInputError
		if tool == "propose_configuration_plan" && errors.As(toolErr, &planErr) {
			return "PLAN_INPUT_INVALID"
		}
		return "TOOL_UNAVAILABLE"
	}
	if tool == runtimecontract.ExecutionSnapshotTool {
		// Process observation доступно только current leased read, не из истории.
		return runtimecontract.ExecutionSnapshotTool + ":completed"
	}
	// У read_file нет нового successful legacy fallback: terminal проекция
	// требует private evidence и authenticated input в recordToolCallPhase.
	if tool == runtimecontract.FileToolRead || tool == "get_workflow_catalog" {
		return "TOOL_UNAVAILABLE"
	}
	if tool == "read_task_session" {
		value, ok := result.(taskSessionToolResult)
		if !ok {
			return "TOOL_UNAVAILABLE"
		}
		// Durable activity хранит только commitment, не текст и не raw response.
		raw, _ := json.Marshal(struct {
			Version          int    `json:"version"`
			SourceSHA256     string `json:"source_sha256"`
			ProjectionSHA256 string `json:"projection_sha256"`
			Messages         int    `json:"messages"`
			Truncated        bool   `json:"truncated"`
		}{1, value.SourceSHA256, value.ProjectionSHA256, len(value.Messages), value.Truncated})
		return string(raw)
	}
	if tool == repositorybundle.ToolName {
		value, ok := result.(repositoryBundleToolResult)
		if !ok || value.ProposalRef == "" || value.GateRef == "" || !runtimecontract.ValidSHA256Text(value.ManifestSHA256) {
			return "TOOL_UNAVAILABLE"
		}
		raw, _ := json.Marshal(value)
		return string(raw)
	}
	if tool == "invoke_integration" || tool == runtimecontract.Context7ResolveTool || tool == runtimecontract.Context7QueryTool {
		value, ok := result.(integrationToolResult)
		if !ok || !safeInvocationRef(value.InvocationRef) || !runtimecontract.ValidSHA256Text(value.inputSHA256) {
			return "TOOL_UNAVAILABLE"
		}
		switch value.state {
		case "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "WAITING_APPROVAL", "UNKNOWN_OUTCOME":
		default:
			return "TOOL_UNAVAILABLE"
		}
		raw, _ := json.Marshal(struct {
			Version       int    `json:"version"`
			InvocationRef string `json:"invocationRef"`
			State         string `json:"state"`
			InputSHA256   string `json:"inputSHA256"`
		}{1, value.InvocationRef, value.state, value.inputSHA256})
		return string(raw)
	}
	values, _ := result.(map[string]any)
	for _, key := range []string{"plan_ref", "conversation_ref", "child_run_ref", "run_ref"} {
		if value, ok := values[key].(string); ok && value != "" {
			return tool + ":" + value
		}
	}
	return tool + ":completed"
}

// truncateRunes trims outer whitespace and bounds by runes without byte slicing;
// callers must supply a nonnegative maximum. Converting invalid UTF-8 replaces
// invalid sequences rather than serving as a strict text validator.
// truncateRunes удаляет внешние пробелы и ограничивает runes, не байты;
// caller передаёт неотрицательный maximum. Неверный UTF-8 при переводе
// заменяется, а не отвергается строгим text validator.
func truncateRunes(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum])
}

// assistantPlanTextWithinLimit requires nonblank trimmed text but counts the
// original runes against the maximum. It does not independently validate UTF-8,
// NUL or domain-specific instruction semantics.
// assistantPlanTextWithinLimit требует непустой trimmed text, но считает
// исходные runes. UTF-8, NUL и доменная семантика инструкций
// самостоятельно не проверяются.
func assistantPlanTextWithinLimit(value string, maximum int) bool {
	return strings.TrimSpace(value) != "" && len([]rune(value)) <= maximum
}

// delegate validates the pinned target/step/task/input pair before a bounded,
// lease-fenced owner delegation RPC with stable call idempotency. Returned child
// and callback references are owner response data, not caller-supplied lineage.
// delegate проверяет pinned target/step/task/input до ограниченного lease-fenced
// owner delegation RPC со стабильной call idempotency. Child/callback refs
// приходят от владельца, а не назначают lineage из caller payload.
func (server *Server) delegate(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	target, stepKey, task, structure, err := validateDelegationInput(input, arguments)
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.DelegateExecution(requestContext, &controlplanev1.DelegateExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, TargetAgentRef: target, WorkflowStepKey: stepKey, Task: task, Input: structure})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "child_run_ref": response.GetChildRun().GetRef(), "callback_edge_ref": response.GetCallbackEdgeRef()}, nil
}

// invoke selects one pinned grant, computes the JSON input digest and resolves
// an owner invocation with at most one transient retry using the same key.
// It polls owner state until outcome or caller cancellation, returning pending/
// unknown states without approving or retrying the external effect itself.
// invoke выбирает pinned grant, вычисляет input digest и разрешает owner
// invocation с одним transient retry того же ключа. Ожидает owner state до
// outcome либо отмены caller, возвращая pending/unknown без одобрения
// и самостоятельного повторения внешнего effect.
func (server *Server) invoke(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	grant, allowed := integrationGrantForCall(input, arguments)
	if !allowed || grant.Operation == repositorybundle.Operation || grant.CapabilityKey == repositorybundle.Operation {
		return nil, &integrationCallInputError{reason: "grant_selection"}
	}
	bounded, ok := arguments["input"].(map[string]any)
	if !ok {
		return nil, &integrationCallInputError{reason: "input_shape"}
	}
	structure, err := structpb.NewStruct(bounded)
	if err != nil {
		return nil, errors.New("integration input is invalid")
	}
	inputBytes, marshalErr := json.Marshal(bounded)
	if marshalErr != nil {
		return nil, errors.New("integration input digest is invalid")
	}
	inputDigest := sha256.Sum256(inputBytes)
	resolveRequest := &controlplanev1.ResolveIntegrationInvocationRequest{RunRef: input.RunRef, NodeRef: input.NodeRef, ConnectionRef: grant.ConnectionRef, CapabilityKey: grant.CapabilityKey, BoundedInput: structure, IdempotencyKey: stableKey(input.LeaseRef, string(callID))}
	var resolved *controlplanev1.ResolveIntegrationInvocationResponse
	for attempt := 0; attempt < 2; attempt++ {
		requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
		resolved, err = server.control.Runtime.ResolveIntegrationInvocation(requestContext, resolveRequest)
		cancel()
		if err == nil || attempt == 1 || status.Code(err) != codes.Unavailable && status.Code(err) != codes.DeadlineExceeded {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
			attempt = 1
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err != nil {
		return nil, status.Error(status.Code(err), "resolve integration invocation")
	}
	if !safeInvocationRef(resolved.GetInvocationRef()) {
		return nil, errors.New("resolve integration invocation: invalid reference")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		readContext, readCancel := context.WithTimeout(ctx, server.config.RequestTimeout)
		state, readErr := server.control.Runtime.GetIntegrationInvocation(readContext, &controlplanev1.GetIntegrationInvocationRequest{InvocationRef: resolved.GetInvocationRef()})
		readCancel()
		if readErr != nil {
			return nil, status.Error(status.Code(readErr), "read integration invocation")
		}
		switch state.GetState() {
		case "SUCCEEDED":
			return integrationToolResult{OK: true, Result: state.GetResultSummary(), InvocationRef: resolved.GetInvocationRef(), state: state.GetState(), inputSHA256: hex.EncodeToString(inputDigest[:])}, nil
		case "FAILED", "REJECTED", "CANCELLED":
			return integrationToolResult{ErrorCode: state.GetSafeErrorCode(), InvocationRef: resolved.GetInvocationRef(), state: state.GetState(), inputSHA256: hex.EncodeToString(inputDigest[:])}, nil
		case "WAITING_APPROVAL":
			return integrationToolResult{ErrorCode: "INTEGRATION_APPROVAL_PENDING", OwnerDecisionRequired: true, InvocationRef: resolved.GetInvocationRef(), state: state.GetState(), inputSHA256: hex.EncodeToString(inputDigest[:])}, nil
		case "UNKNOWN_OUTCOME":
			return integrationToolResult{ErrorCode: "INTEGRATION_OUTCOME_UNKNOWN", OwnerDecisionRequired: true, InvocationRef: resolved.GetInvocationRef(), state: state.GetState(), inputSHA256: hex.EncodeToString(inputDigest[:])}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// authorize parses the callback ticket, resolves its execution through Manager
// and compares exact headers against that trusted input. Header values alone
// are not authority and cannot select another tenant or lease.
// authorize разбирает callback ticket, разрешает execution через Manager
// и сверяет headers с доверенным input. Одни header values
// не являются полномочием и не выбирают другой tenant либо lease.
func (server *Server) authorize(request *http.Request, leaseRef string) (runtimecontract.RunnerInput, bool) {
	token, ok := bearer(request)
	if !ok {
		return runtimecontract.RunnerInput{}, false
	}
	input, err := server.manager.ResolveTurn(request.Context(), leaseRef, token)
	return input, err == nil && executionHeadersMatch(request, input)
}

// executionHeadersMatch checks the route-derived callback method and exact
// execution tuple, using constant-time digest comparisons. It assumes Manager
// already resolved the ticket; matching untrusted headers is not authentication.
// executionHeadersMatch сверяет callback method из маршрута и exact execution
// tuple с constant-time digest comparisons. Ticket уже должен быть разрешён
// Manager; совпадение недоверенных headers не является authentication.
func executionHeadersMatch(request *http.Request, input runtimecontract.RunnerInput) bool {
	method := ""
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) == 5 && parts[3] == "artifacts" {
		method = "artifact"
	} else if len(parts) == 4 {
		method = parts[3]
	}
	return method != "" && request.Header.Get("X-Kodex-Callback-Method") == method &&
		request.Header.Get("X-Kodex-Organization-Ref") == input.OrganizationRef &&
		request.Header.Get("X-Kodex-Project-Ref") == input.ProjectRef && request.Header.Get("X-Kodex-Run-Ref") == input.RunRef &&
		request.Header.Get("X-Kodex-Node-Ref") == input.NodeRef && request.Header.Get("X-Kodex-Session-Ref") == input.SessionRef &&
		request.Header.Get("X-Kodex-Turn-Ref") == input.TurnRef && request.Header.Get("X-Kodex-Attempt") == strconv.FormatInt(int64(input.Attempt), 10) &&
		subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Kodex-Runtime-Revision-Digest")), []byte(input.RuntimeRevisionDigest)) == 1 &&
		subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Kodex-Input-Digest")), []byte(input.InputDigest)) == 1 &&
		subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Kodex-Execution-Binding-Digest")), []byte(input.ExecutionBindingDigest)) == 1 &&
		subtle.ConstantTimeCompare([]byte(request.Header.Get("X-Kodex-MCP-Binding-Digest")), []byte(input.MCPBindingDigest)) == 1
}

// bearer accepts the exact Bearer prefix and 64 hexadecimal characters without
// trimming or case normalization. This is syntax only; ticket resolution still
// decides whether the token belongs to the requested execution.
// bearer принимает точный Bearer prefix и 64 hex characters без trimming
// и нормализации регистра. Это лишь форма; принадлежность token выполнению
// определяется при разрешении ticket.
func bearer(request *http.Request) (string, bool) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) != len("Bearer ")+64 {
		return "", false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if _, err := hex.DecodeString(token); err != nil {
		return "", false
	}
	return token, true
}

// decode closes the body and requires one known-field JSON value plus EOF in a
// maximum+1 limited reader view. It does not drain/count the complete body or
// reject duplicate/case-aliased JSON keys; callers own semantic field bounds.
// decode закрывает body и требует один JSON с известными полями и EOF в
// maximum+1 limited reader view. Полное body не дочитывается и не подсчитывается,
// duplicate/case-alias keys не запрещаются; semantic bounds проверяет caller.
func decode(request *http.Request, target any, maximum int64) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximum+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return errors.New("request body is invalid")
	}
	return nil
}

// stableKey derives a deterministic namespace UUID from NUL-separated inputs;
// it is an idempotency/correlation locator, not a secret or permission proof.
// stableKey выводит детерминированный namespace UUID из NUL-разделённых входов;
// это idempotency/correlation locator, не секрет и не proof полномочий.
func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}

// writeControlError maps selected owner gRPC statuses to fixed HTTP errors,
// hiding permission/authentication failures as 404 and omitting upstream details.
// writeControlError переводит выбранные owner gRPC statuses в фиксированные HTTP
// ошибки, скрывая permission/authentication как 404 без upstream details.
func writeControlError(writer http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		http.Error(writer, "invalid runtime request", http.StatusBadRequest)
	case codes.NotFound:
		http.Error(writer, "not found", http.StatusNotFound)
	case codes.PermissionDenied, codes.Unauthenticated:
		http.Error(writer, "not found", http.StatusNotFound)
	case codes.Aborted, codes.AlreadyExists, codes.FailedPrecondition:
		http.Error(writer, "runtime state conflict", http.StatusConflict)
	case codes.DeadlineExceeded:
		http.Error(writer, "runtime owner timed out", http.StatusGatewayTimeout)
	default:
		http.Error(writer, "runtime owner unavailable", http.StatusServiceUnavailable)
	}
}

// writeMCPResult writes one JSON-RPC result envelope with the original raw ID;
// encoder/write errors are intentionally not propagated by this helper.
// writeMCPResult пишет JSON-RPC result envelope с исходным raw ID;
// encoder/write errors намеренно не возвращаются этим helper.
func (server *Server) writeMCPResult(writer http.ResponseWriter, id json.RawMessage, result any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

// writeMCPError writes the supplied protocol error code/message and raw ID as
// JSON; callers must provide safe messages because it performs no redaction.
// writeMCPError пишет заданные protocol error code/message и raw ID в JSON;
// caller передаёт безопасный message, отдельной redaction здесь нет.
func (server *Server) writeMCPError(writer http.ResponseWriter, id json.RawMessage, code int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

// serverTLS selects explicit trusted-cluster plaintext transport or constructs
// TLS1.3-only mutual authentication with the configured CA and exact verified
// leaf URI. Other profiles and material failures reject; trusted mode is an
// explicit deployment boundary, not fallback after certificate failure.
// serverTLS выбирает явный trusted-cluster plaintext transport либо TLS1.3 mTLS
// с настроенным CA и точным URI проверенного leaf. Другие profiles и ошибки
// материалов отвергаются; trusted mode — отдельная deployment boundary,
// не fallback после ошибки сертификата.
func serverTLS(config Config) (*tls.Config, error) {
	if config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		return nil, nil
	}
	if config.RPCProfile != "" {
		return nil, errors.New("runtime callback RPC profile is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime callback server identity")
	}
	ca, err := os.ReadFile(config.ClientCAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read runtime callback client CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse runtime callback client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
				return errors.New("runtime callback client certificate is unverified")
			}
			for _, identity := range state.VerifiedChains[0][0].URIs {
				if subtle.ConstantTimeCompare([]byte(identity.String()), []byte(config.ExpectedClientSPIFFEID)) == 1 {
					return nil
				}
			}
			return errors.New("runtime callback client identity is invalid")
		}}, nil
}
