// Package httptransport maps canonical OpenAPI operations to generated owner gRPC clients and bounded public JSON projections.
// Package httptransport отображает канонические OpenAPI-операции на generated gRPC clients владельца и ограниченные публичные JSON-проекции.
package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	emailbridgeapi "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	internalobservability "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/observability"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maximumJSONBody is the one-MiB nominal budget used by JSON decoders, which read through LimitReader with one extra byte; this constant alone is not proof of exact request-length rejection.
// maximumJSONBody задаёт номинальный бюджет JSON в один МиБ; decoder читает через LimitReader ещё один байт, поэтому сама константа не доказывает точный отказ по полной длине запроса.
const maximumJSONBody = 1 << 20

// Server adapts generated HTTP operations to owner clients, secret brokers, optional speech/realtime and the browser boundary. Dependencies are attached during construction; fields are not synchronized for concurrent reconfiguration.
// Server связывает generated HTTP-операции с клиентами владельца, secret brokers, необязательными speech/realtime и браузерной границей. Зависимости подключаются при сборке; конкурентная перенастройка полей не синхронизирована.
type Server struct {
	control      *controlplaneclient.Client
	secrets      secretbrokerv1.SecretBrokerServiceClient
	secretDrafts secretbrokerv1.SecretBrokerServiceClient
	speech       speechToTextClient
	boundary     *boundary.Boundary
	logger       *slog.Logger
	realtime     http.Handler
	texts        *texti18n.Localizer
}

// New requires nonnil control client, boundary, logger and localizer and retains them without dialing upstream services or checking every client field; optional attachments remain unset.
// New требует непустые control client, boundary, logger и localizer, сохраняя их без соединения с сервисами и проверки каждого поля клиента; необязательные зависимости остаются неподключёнными.
func New(control *controlplaneclient.Client, security *boundary.Boundary, logger *slog.Logger, texts *texti18n.Localizer) (*Server, error) {
	if control == nil || security == nil || logger == nil || texts == nil {
		return nil, errors.New("control API HTTP server configuration is invalid")
	}
	return &Server{control: control, boundary: security, logger: logger, texts: texts}, nil
}

// AttachSecretBroker sets the secret-operation client exactly once and rejects nil or replacement; callers must attach it before serving concurrent requests.
// AttachSecretBroker однократно задаёт клиент операций с секретами, отклоняя nil и замену; подключение должно предшествовать конкурентной обработке запросов.
func (server *Server) AttachSecretBroker(client secretbrokerv1.SecretBrokerServiceClient) error {
	if client == nil || server.secrets != nil {
		return errors.New("secret broker attachment is invalid")
	}
	server.secrets = client
	return nil
}

// AttachSecretDraftBroker independently sets the secret-draft client once, rejecting nil or replacement without changing the main secret-operation client.
// AttachSecretDraftBroker отдельно и однократно задаёт клиент черновиков секретов, отклоняя nil и замену без изменения основного клиента секретов.
func (server *Server) AttachSecretDraftBroker(client secretbrokerv1.SecretBrokerServiceClient) error {
	if client == nil || server.secretDrafts != nil {
		return errors.New("secret draft broker attachment is invalid")
	}
	server.secretDrafts = client
	return nil
}

// AttachRealtime replaces the optional stream handler without validation or synchronization; Handler snapshots its current presence when building the router.
// AttachRealtime заменяет необязательный обработчик потока без проверки и синхронизации; Handler учитывает его текущее наличие при построении router.
func (server *Server) AttachRealtime(session http.Handler) {
	server.realtime = session
}

// Handler builds a fresh mux with optional session stream and generated HTTP routes, maps route decoding failures to INVALID_REQUEST, and wraps it in boundary middleware inside localization. It does not start a listener.
// Handler создаёт новый mux с необязательным потоком и generated HTTP-маршрутами, превращает ошибки разбора маршрута в INVALID_REQUEST и оборачивает его границей внутри локализации. Слушатель не запускается.
func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	if server.realtime != nil {
		mux.Handle("GET /api/v1/session/stream", server.realtime)
	}
	generated.HandlerWithOptions(server, generated.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: func(writer http.ResponseWriter, _ *http.Request, _ error) {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
	}})
	return server.localizationMiddleware(server.boundary.Middleware(mux))
}

// localizedResponseWriter carries the selected locale and shared localizer while wrapping ResponseWriter; Unwrap preserves access to the underlying writer for response controllers.
// localizedResponseWriter хранит выбранный язык и общий localizer поверх ResponseWriter; Unwrap сохраняет доступ response controllers к исходному writer.
type localizedResponseWriter struct {
	http.ResponseWriter
	texts      *texti18n.Localizer
	locale     string
	diagnostic requestDiagnostic
}

// Localize resolves a message identifier with the request-selected locale and no template data; it does not expose upstream diagnostic text.
// Localize переводит идентификатор сообщения на выбранный язык запроса без template data и без выдачи диагностического текста upstream.
func (writer *localizedResponseWriter) Localize(messageID string) string {
	return writer.texts.Localize(writer.locale, messageID, nil)
}

// LocalizeFor delegates an explicit locale and message identifier to the shared catalog without changing this writer's selected locale.
// LocalizeFor передаёт явные язык и идентификатор сообщения общему каталогу без изменения выбранного языка writer.
func (writer *localizedResponseWriter) LocalizeFor(locale, messageID string) string {
	return writer.texts.Localize(locale, messageID, nil)
}

// Unwrap exposes the wrapped ResponseWriter for standard response-controller capability discovery without independently implementing flush or hijack.
// Unwrap выдаёт исходный ResponseWriter для штатного поиска возможностей response controller без отдельной реализации flush или hijack.
func (writer *localizedResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// localizationMiddleware resolves Accept-Language through the bounded locale policy and passes a localizing response wrapper to the next handler; it does not authenticate requests.
// localizationMiddleware выбирает язык Accept-Language по ограниченной политике и передаёт следующему handler wrapper локализации; аутентификацию не выполняет.
func (server *Server) localizationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		locale := texti18n.ResolveAcceptLanguage(request.Header.Get("Accept-Language"))
		next.ServeHTTP(&localizedResponseWriter{ResponseWriter: writer, texts: server.texts, locale: locale,
			diagnostic: requestDiagnostic{ctx: request.Context(), logger: server.logger,
				route: internalobservability.Route(request.URL.Path), method: diagnosticMethod(request.Method)}}, request)
	})
}

// CreateOwnerSession requires verified authorization context, decodes optional purpose and delegates session issuance to the boundary. It maps rate, purpose and fresh-auth failures, then sets secure session/CSRF cookies, revision ETag and a no-store 204 response.
// CreateOwnerSession требует проверенный контекст авторизации, разбирает необязательную цель и поручает выпуск сессии границе. Отображает отказы лимита, цели и свежего входа, затем задаёт cookies сессии/CSRF, ETag ревизии и no-store ответ204.
func (server *Server) CreateOwnerSession(writer http.ResponseWriter, request *http.Request, _ generated.CreateOwnerSessionParams) {
	principal, bearer, ok := boundary.VerifiedAuthorizationFromContext(request.Context())
	if !ok {
		writeLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		return
	}
	body, ok := decodeOptionalJSON[generated.OwnerSessionCreateInput](writer, request)
	if !ok {
		return
	}
	var purpose *boundary.SessionPurpose
	if body != nil && body.Purpose != nil {
		purpose = sessionPurposeMap(body.Purpose)
	}
	claims, encoded, csrf, err := server.boundary.IssueSession(principal, bearer, purpose)
	if err != nil {
		switch {
		case errors.Is(err, boundary.ErrRateLimited):
			writeLocalProblem(writer, http.StatusTooManyRequests, "RATE_LIMITED", true)
		case errors.Is(err, boundary.ErrSessionPurposeInvalid):
			writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		case errors.Is(err, boundary.ErrFreshAuthenticationRequired):
			writeLocalProblem(writer, http.StatusForbidden, "FRESH_AUTHENTICATION_REQUIRED", false)
		default:
			writeLocalProblem(writer, http.StatusUnauthorized, "UNAUTHENTICATED", false)
		}
		return
	}
	boundary.SetOwnerSessionCookies(writer, claims, encoded, csrf)
	writer.Header().Set("ETag", fmt.Sprintf("\"%d\"", claims.SessionRevision))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

// RenewOwnerSession reuses GetOwnerSession and its existing boundary-managed renewal behavior rather than issuing a separate session or trusting a client revision.
// RenewOwnerSession использует GetOwnerSession с существующим обновлением под управлением границы вместо выпуска отдельной сессии и доверия клиентской ревизии.
func (server *Server) RenewOwnerSession(writer http.ResponseWriter, request *http.Request, _ generated.RenewOwnerSessionParams) {
	server.GetOwnerSession(writer, request)
}

// DeleteOwnerSession requires verified identity and an exact quoted revision, revokes through the boundary before expiring both secure cookies, and emits no-store 204 only after success. A failed revoke leaves cookie cleanup unperformed here.
// DeleteOwnerSession требует проверенную личность и точную ревизию в кавычках, отзывает сессию через границу до удаления двух защищённых cookies и выдаёт no-store204 только при успехе. При ошибке отзыва cookies здесь не очищаются.
func (server *Server) DeleteOwnerSession(writer http.ResponseWriter, request *http.Request, parameters generated.DeleteOwnerSessionParams) {
	identity, ok := boundary.IdentityFromContext(request.Context())
	if !ok || string(parameters.IfMatch) != fmt.Sprintf("\"%d\"", identity.SessionRevision) {
		writeLocalProblem(writer, http.StatusPreconditionFailed, "STALE_VERSION", false)
		return
	}
	if err := server.boundary.RevokeSession(request.Context(), identity); err != nil {
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	for _, item := range []struct {
		name     string
		httpOnly bool
	}{{boundary.SessionCookieName, true}, {boundary.CSRFCookieName, false}} {
		http.SetCookie(writer, &http.Cookie{Name: item.name, Path: "/", Secure: true, HttpOnly: item.httpOnly, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

// decodeJSON delegates required-body decoding with the nominal one-MiB budget and preserves the decoder's value/ok contract; false results must not be used.
// decodeJSON передаёт разбор обязательного тела с номинальным бюджетом один МиБ и сохраняет пару value/ok; результат при false использовать нельзя.
func decodeJSON[T any](writer http.ResponseWriter, request *http.Request) (T, bool) {
	return decodeJSONWithLimit[T](writer, request, maximumJSONBody)
}

// decodeJSONWithLimit rejects unknown struct fields and a second JSON value while reading at most maximumBody+1 bytes. Failure writes INVALID_REQUEST and may return partial T with false; duplicate keys and total length beyond the limited reader are not independently checked.
// decodeJSONWithLimit отклоняет неизвестные поля структуры и второе JSON-значение, читая не более maximumBody+1 байт. При ошибке пишет INVALID_REQUEST и может вернуть частичный T с false; повторные ключи и полная длина за пределами reader отдельно не проверяются.
func decodeJSONWithLimit[T any](writer http.ResponseWriter, request *http.Request, maximumBody int64) (T, bool) {
	var result T
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return result, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return result, false
	}
	return result, true
}

// decodeOptionalJSON accepts an empty/whitespace-only limited body as nil, otherwise requires one closed-field JSON document. It reads at most the nominal budget plus one byte, returning nil/false after INVALID_REQUEST on decoding or trailing-value failure.
// decodeOptionalJSON принимает пустое либо пробельное ограниченное тело как nil, иначе требует один JSON-документ без неизвестных полей. Читает не более бюджета плюс один байт; при ошибке разбора или лишнем значении пишет INVALID_REQUEST и возвращает nil/false.
func decodeOptionalJSON[T any](writer http.ResponseWriter, request *http.Request) (*T, bool) {
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximumJSONBody+1))
	decoder.DisallowUnknownFields()
	var result T
	if err := decoder.Decode(&result); errors.Is(err, io.EOF) {
		return nil, true
	} else if err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return &result, true
}

// mutation copies the idempotency key and optionally parses one quoted positive int64 ETag into ExpectedVersion. It does not validate key syntax, resolve ownership or enforce idempotency itself.
// mutation копирует ключ идемпотентности и при наличии разбирает положительный int64 ETag в кавычках в ExpectedVersion. Формат ключа, владение и исполнение идемпотентности здесь не проверяются.
func mutation(idempotency, etag string) (*controlplanev1.MutationContext, bool) {
	result := &controlplanev1.MutationContext{IdempotencyKey: idempotency}
	if etag == "" {
		return result, true
	}
	if len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return nil, false
	}
	version, err := strconv.ParseInt(etag[1:len(etag)-1], 10, 64)
	if err != nil || version < 1 {
		return nil, false
	}
	result.ExpectedVersion = &version
	return result, true
}

// requireMutation wraps mutation and emits INVALID_REQUEST for malformed version syntax; successful construction is not authorization or an owner-side concurrency check.
// requireMutation оборачивает mutation и выдаёт INVALID_REQUEST при неверном формате версии; успешная сборка не является авторизацией или проверкой конкуренции владельца.
func requireMutation(writer http.ResponseWriter, idempotency, etag string) (*controlplanev1.MutationContext, bool) {
	result, ok := mutation(idempotency, etag)
	if !ok {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
	}
	return result, ok
}

// decodeMutationBody decodes the request body before parsing mutation headers and stops at the first local error. It only prepares syntax for a typed owner command; it does not authorize the caller or resolve the target.
// decodeMutationBody разбирает тело запроса перед контекстом изменения и останавливается на первой локальной ошибке. Он лишь готовит синтаксис для типизированной команды владельца, не авторизуя вызывающего и не разрешая цель.
func decodeMutationBody[T any](writer http.ResponseWriter, request *http.Request, idempotency, etag string) (T, *controlplanev1.MutationContext, bool) {
	body, ok := decodeJSON[T](writer, request)
	if !ok {
		return body, nil, false
	}
	mutationContext, ok := requireMutation(writer, idempotency, etag)
	return body, mutationContext, ok
}

// page creates a fresh page request defaulting to fifty and copies optional size/token without range validation; the int-to-int32 conversion is unchecked here.
// page создаёт новый запрос страницы с размером пятьдесят по умолчанию и копирует размер и token без проверки диапазона; преобразование int в int32 здесь не проверяется.
func page(size *int, token *string) *controlplanev1.PageRequest {
	result := &controlplanev1.PageRequest{PageSize: 50}
	if size != nil {
		result.PageSize = int32(*size)
	}
	if token != nil {
		result.PageToken = *token
	}
	return result
}

// stringValue converts an optional string-like value to string, using an empty string for nil without validating enum membership or reference syntax.
// stringValue преобразует необязательное строкоподобное значение в string, возвращая пустую строку для nil без проверки enum или формата ссылки.
func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

// withProjectReference attaches a syntactically accepted project selector to the request context or writes INVALID_REQUEST; it does not establish membership or mutate the original request.
// withProjectReference добавляет допустимый по формату селектор проекта в контекст нового запроса либо пишет INVALID_REQUEST; членство не устанавливается, исходный запрос не меняется.
func withProjectReference(writer http.ResponseWriter, request *http.Request, reference string) (*http.Request, bool) {
	ctx, err := controlplaneclient.WithProjectReference(request.Context(), reference)
	if err != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return request.WithContext(ctx), true
}

// writeMessage projects and validates an owner protobuf response, optionally selects a nested field and reshapes a page with approved metadata, then localizes safe errors and writes no-store JSON/ETag. Invalid known public shapes map to bad gateway; encoding errors after headers are ignored, so success is not a delivery receipt.
// writeMessage проецирует и проверяет protobuf-ответ владельца, при необходимости выбирает вложенное поле и перестраивает страницу с разрешённой metadata, затем локализует безопасные ошибки и пишет no-store JSON/ETag. Известные неверные публичные формы дают bad gateway; ошибка encoding после заголовков игнорируется, поэтому доставка не подтверждается.
func writeMessage(writer http.ResponseWriter, statusCode int, message proto.Message, field string, pageField string) {
	value, err := messageMap(message)
	if err != nil {
		if errors.Is(err, errPublicSecretDescriptor) || errors.Is(err, errPublicProviderStatusReason) || errors.Is(err, errPublicAvatarShape) || errors.Is(err, errPublicIntegrationShape) || errors.Is(err, errPublicRuntimeEnvironmentShape) || errors.Is(err, errRuntimeCatalogView) || errors.Is(err, errOwnerGateShape) {
			writeLocalProblem(writer, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	if field != "" {
		value, _ = value[field].(map[string]any)
	}
	if pageField != "" {
		items := []any{}
		if present, ok := value[pageField].([]any); ok {
			items = present
		}
		output := map[string]any{"items": items}
		// Only explicitly required OpenAPI pages receive a terminal cursor; optional pages retain field absence.
		// Только явно требуемые OpenAPI-страницы получают terminal cursor; необязательные страницы сохраняют отсутствие поля.
		if requiresTerminalCursor(message) {
			output["nextPageToken"] = ""
		}
		if pageValue, ok := value["page"].(map[string]any); ok {
			if next, ok := pageValue["nextPageToken"].(string); ok && next != "" {
				output["nextPageToken"] = next
			}
		}
		if nextActions, ok := value["nextActions"].([]any); ok {
			output["nextActions"] = nextActions
		}
		if coreReady, ok := value["coreReady"].(bool); ok {
			output["coreReady"] = coreReady
		}
		if total, ok := value["total"].(float64); ok {
			output["total"] = total
		}
		for _, key := range []string{"subject", "evaluatedAt", "role"} {
			if preserved, ok := value[key]; ok {
				output[key] = preserved
			}
		}
		value = output
	}
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		LocalizeSafeErrors(value, localizer.Localize)
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if version, ok := value["version"].(float64); ok && version >= 1 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%.0f\"", version))
	} else if version, ok := value["agentVersion"].(float64); ok && version >= 1 {
		writer.Header().Set("ETag", fmt.Sprintf("\"%.0f\"", version))
	}
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(value)
}

// writeReadRPCResult handles an already completed read RPC: an error uses the existing closed problem mapper before the message is inspected; success uses the existing public projection with HTTP200. Callers retain request authority, RPC choice and any response-specific validation.
// writeReadRPCResult обрабатывает уже завершённый RPC чтения: ошибка проходит прежнее закрытое отображение до обращения к сообщению, успех — прежнюю публичную проекцию с HTTP200. Полномочия запроса, выбор RPC и специальные проверки ответа остаются у вызывающего.
func writeReadRPCResult(writer http.ResponseWriter, message proto.Message, err error, field, pageField string) {
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusOK, message, field, pageField)
}

// messageMap validates the typed runtime catalog, marshals lowerCamelCase proto JSON, checks/normalizes the public shape and applies browser projections in a fresh map. Run graph response snapshots are removed; on failure no projected map is returned, and this is not an owner authorization operation.
// messageMap проверяет типизированный runtime-каталог, сериализует proto JSON с lowerCamelCase, проверяет и нормализует публичную форму и применяет браузерные проекции в новой map. Снимки ответов графа удаляются; при ошибке проекция не возвращается. Это не авторизация владельца.
func messageMap(message proto.Message) (map[string]any, error) {
	if err := validateRuntimeCatalogMessage(message.ProtoReflect(), 0); err != nil {
		slog.Warn("upstream response validation failed", "error_class", "runtime_catalog", "safe_reason", err.Error())
		return nil, err
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: false}).Marshal(message)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if err := normalizeProtoJSONShape(value, message.ProtoReflect().Descriptor()); err != nil {
		return nil, err
	}
	normalize(value)
	if message.ProtoReflect().Descriptor().FullName() == "controlplane.v1.GetRunGraphResponse" {
		delete(value, "runs")
	}
	return value, nil
}

// maximumSafeJSONInteger is the largest positive integer exactly representable by browser JSON numbers; ordinary 64-bit protobuf values outside its symmetric range are rejected.
// maximumSafeJSONInteger — наибольшее положительное целое, точно представимое числом браузерного JSON; обычные64-битные protobuf-значения вне симметричного диапазона отклоняются.
const maximumSafeJSONInteger = int64(1<<53 - 1)

var (
	// errPublicSecretDescriptor classifies an invalid public secret-descriptor revision for closed upstream-response error mapping without exposing secret data.
	// errPublicSecretDescriptor обозначает неверную публичную ревизию описателя секрета для закрытого отображения upstream-ошибки без раскрытия секрета.
	errPublicSecretDescriptor = errors.New("public Secret descriptor revision is invalid")
	// errPublicProviderStatusReason rejects provider status reasons outside the explicit public vocabulary rather than forwarding arbitrary provider diagnostics.
	// errPublicProviderStatusReason отклоняет причины состояния вне явного публичного словаря вместо передачи произвольной диагностики провайдера.
	errPublicProviderStatusReason = errors.New("public provider status reason is invalid")
	// errPublicAvatarShape classifies invalid public avatar source/enum shapes and is mapped to an invalid upstream response, not a missing-image fallback.
	// errPublicAvatarShape обозначает неверный публичный source или enum аватара и отображается как неверный upstream-ответ, а не запасная картинка.
	errPublicAvatarShape = errors.New("public agent avatar response is invalid")
	// errPublicRuntimeEnvironmentShape classifies an invalid runtime-environment public enum shape without exposing the source response as user text.
	// errPublicRuntimeEnvironmentShape обозначает неверную публичную форму enum окружения без выдачи исходного ответа как пользовательского текста.
	errPublicRuntimeEnvironmentShape = errors.New("public runtime environment response is invalid")
)

// normalizeProtoJSONShape mutates decoded JSON according to its protobuf descriptor, restoring selected required defaults, normalizing typed fields and validating public source, scope, revision and binding invariants. It redacts private secret/image pins and delegates specialized validators; errors may follow partial mutation, so callers must discard the projection rather than treat it as authorization.
// normalizeProtoJSONShape меняет разобранный JSON по protobuf-описателю: восстанавливает выбранные обязательные defaults, нормализует типы и проверяет публичные source, scope, ревизии и привязки. Удаляет закрытые поля секретов и образов и вызывает профильные validators; ошибка может следовать за частичной правкой, поэтому проекцию надо отбросить, а не считать авторизацией.
func normalizeProtoJSONShape(value map[string]any, descriptor protoreflect.MessageDescriptor) (normalizationErr error) {
	if descriptor.FullName() == "controlplane.v1.RunMessage" || descriptor.FullName() == "controlplane.v1.AssistantTurn" {
		if source, ok := value["source"].(map[string]any); !ok || source["origin"] == nil {
			return errors.New("public message source is missing")
		}
	}
	defer func() {
		if normalizationErr != nil && descriptor.FullName() == "controlplane.v1.OwnerGate" {
			normalizationErr = errOwnerGateShape
		}
	}()
	if descriptor.FullName() == "controlplane.v1.IntegrationDefinition" {
		version, ok := value["version"].(string)
		parsed, err := strconv.ParseInt(version, 10, 64)
		if !ok || err != nil || !validManagedVersion(parsed) {
			return errors.New("integration definition catalog version is invalid")
		}
		if actions, present := value["nextActions"]; present {
			items, ok := actions.([]any)
			if !ok || len(items) > 1 || len(items) == 1 && items[0] != "COPY" {
				return errors.New("integration definition actions are invalid")
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowVersion" {
		ref, ok := value["ref"].(string)
		if !ok || !fileTargetRef(ref) {
			return errors.New("workflow revision reference is invalid")
		}
		if _, ok := controlplanev1.WorkflowState_value[fmt.Sprint(value["state"])]; !ok || value["state"] == "WORKFLOW_STATE_UNSPECIFIED" {
			return errors.New("workflow revision state is invalid")
		}
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccount" {
		if reason, exists := value["safeStatusReason"]; exists {
			switch reason {
			case "AUTHORIZED", "ACCOUNT_DISABLED", "ACCOUNT_REVOKED", "REAUTHORIZATION_REQUIRED", "DEVICE_AUTHORIZATION_PENDING", "CREDENTIAL_CONFIGURATION_REQUIRED", "ACCOUNT_STATE_UNKNOWN", "DEVICE_AUTHORIZATION_EXPIRED", "DEVICE_AUTHORIZATION_FAILED", "CREDENTIAL_MATERIALIZATION_FAILED", "ACCOUNT_DELETING", "ACCOUNT_DELETED":
			default:
				return errPublicProviderStatusReason
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.RuntimeSecretDescriptor" {
		revision, ok := value["revision"].(string)
		pin, err := strconv.ParseInt(revision, 10, 64)
		if !ok || err != nil || !validManagedVersion(pin) {
			return errPublicSecretDescriptor
		}
		delete(value, "namespace")
	}
	if descriptor.FullName() == "controlplane.v1.RuntimeSecret" {
		// The exact Kubernetes identity of the current revision belongs only to owner-side operations, not the browser RuntimeSecret.
		// Точная Kubernetes identity текущей ревизии нужна только операциям владельца и не входит в браузерный RuntimeSecret.
		delete(value, "namespace")
		delete(value, "currentRevisionDescriptor")
	}
	if descriptor.FullName() == "controlplane.v1.RoleImageRecipe" {
		// Supply-chain pins belong to the internal build lifecycle; the public contract exposes only readiness and the safe promotion reference.
		// Supply-chain pins принадлежат внутреннему жизненному циклу сборки; публичный контракт сообщает только готовность и безопасную ссылку promotion.
		for _, field := range []string{
			"specSha256",
			"policyRevision",
			"policySha256",
			"roleRuntimeContractRevision",
			"roleRuntimeContractSha256",
		} {
			delete(value, field)
		}
		promotedReference, _ := value["promotedImageReference"].(string)
		value["promotedImageReady"] = promotedReference != ""
	}
	fields := descriptor.Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		current, exists := value[field.JSONName()]
		if !exists {
			if field.IsList() {
				value[field.JSONName()] = []any{}
			} else if field.IsMap() {
				value[field.JSONName()] = map[string]any{}
			} else if defaultValue, required := requiredProtoScalarDefault(descriptor, field); required {
				value[field.JSONName()] = defaultValue
			}
			continue
		}
		if field.IsList() {
			items, ok := current.([]any)
			if !ok {
				return errors.New("public protobuf list shape is invalid")
			}
			for itemIndex := range items {
				normalized, err := normalizeProtoField(items[itemIndex], field)
				if err != nil {
					return err
				}
				items[itemIndex] = normalized
			}
			continue
		}
		normalized, err := normalizeProtoField(current, field)
		if err != nil {
			return err
		}
		value[field.JSONName()] = normalized
	}
	if descriptor.FullName() == "controlplane.v1.RunEvent" {
		if message, ok := value["message"].(map[string]any); ok {
			if source, ok := message["source"].(map[string]any); ok && source["origin"] == "CALLBACK_CONTINUATION" {
				actor, actorOK := value["actor"].(map[string]any)
				execution, executionOK := value["execution"].(map[string]any)
				if !actorOK || actor["kind"] != "RUN_EVENT_ACTOR_KIND_AGENT" || !executionOK || execution["turnRef"] != message["ref"] || message["phase"] != "RUN_MESSAGE_PHASE_USER" {
					return errors.New("public callback message binding is invalid")
				}
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.AssistantTurn" {
		if source := value["source"].(map[string]any); source["origin"] == "CALLBACK_CONTINUATION" && value["role"] != "SYSTEM_RECEIPT" {
			return errors.New("public callback turn role is invalid")
		}
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowVersion" {
		for _, key := range []string{"version", "revision"} {
			number, ok := value[key].(float64)
			if !ok || number < 1 || number > float64(maximumSafeJSONInteger) || number != float64(int64(number)) {
				return errors.New("workflow revision number is invalid")
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.OwnerGate" {
		organizationRef, ok := value["organizationRef"].(string)
		if !ok || !fileTargetRef(organizationRef) {
			return errors.New("public owner gate organization is invalid")
		}
		projectRef, projectPresent := value["projectRef"]
		switch value["scopeKind"] {
		case "ORGANIZATION":
			if projectPresent {
				return errors.New("public organization gate cannot carry project")
			}
		case "PROJECT":
			ref, valid := projectRef.(string)
			if !projectPresent || !valid || !fileTargetRef(ref) {
				return errors.New("public project gate reference is invalid")
			}
		default:
			return errors.New("public owner gate scope is invalid")
		}
	}
	if descriptor.FullName() == "controlplane.v1.AgentInstructionsBinding" {
		ref, refOK := value["ref"].(string)
		revision, revisionOK := value["revisionRef"].(string)
		version, versionOK := value["version"].(float64)
		if !refOK || !revisionOK || !versionOK || !fileTargetRef(ref) || !fileTargetRef(revision) || version < 1 || version > float64(maximumSafeJSONInteger) || version != float64(int64(version)) {
			return errors.New("agent instructions binding shape is invalid")
		}
	}
	if descriptor.FullName() == "controlplane.v1.AgentAvatar" {
		switch value["source"] {
		case "FALLBACK", "ARTIFACT":
		default:
			return errPublicAvatarShape
		}
	}
	if err := validateRunSessionReadinessShape(value, descriptor); err != nil {
		return err
	}
	if err := validateRunIntegrationBinding(value, descriptor); err != nil {
		return err
	}
	if descriptor.FullName() == "controlplane.v1.RunEvent" {
		projectRunEventServiceCode(value)
	}
	return normalizeIntegrationShape(value, descriptor)
}

// requiredProtoScalarDefault supplies zero/empty defaults only for explicit message-and-field pairs required by the public contract. Other omitted scalars retain absence; financial optional fields deliberately remain omitted rather than inventing data.
// requiredProtoScalarDefault задаёт нулевые и пустые defaults только для явных пар message/field публичного контракта. Остальные пропущенные scalar сохраняют отсутствие; необязательные финансовые поля намеренно не заполняются выдуманными данными.
func requiredProtoScalarDefault(descriptor protoreflect.MessageDescriptor, field protoreflect.FieldDescriptor) (any, bool) {
	if descriptor.FullName() == "controlplane.v1.WorkflowAttemptResult" {
		switch field.JSONName() {
		case "accepted":
			return false, true
		case "reason":
			return "", true
		}
	}
	if descriptor.FullName() == "controlplane.v1.TokenUsage" && field.JSONName() == "completeness" {
		return "UNKNOWN", true
	}
	if descriptor.FullName() == "controlplane.v1.GetRepositoryBundlePublicationResponse" {
		if field.Kind() == protoreflect.BoolKind && field.JSONName() == "complete" {
			return false, true
		}
		if field.Kind() == protoreflect.Int32Kind && (field.JSONName() == "offset" || field.JSONName() == "nextOffset") {
			return float64(0), true
		}
	}
	if (descriptor.FullName() == "controlplane.v1.RepositoryBundleFile" && field.JSONName() == "bytes") || (descriptor.FullName() == "controlplane.v1.RepositoryBundlePublication" && field.JSONName() == "totalBytes") {
		return float64(0), true
	}

	if descriptor.FullName() == "controlplane.v1.ProviderFinancialOverview" && isProto64BitInteger(field.Kind()) {
		return float64(0), true
	}
	if descriptor.FullName() == "controlplane.v1.ListRunEventsResponse" {
		if field.Kind() == protoreflect.BoolKind && field.JSONName() == "complete" {
			return false, true
		}
		if field.Kind() == protoreflect.Int64Kind && field.JSONName() == "currentSequence" {
			return float64(0), true
		}
	}
	if descriptor.FullName() == "controlplane.v1.Agent" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "system" || field.JSONName() == "enabled"
	}
	if descriptor.FullName() == "controlplane.v1.RoleImageRecipe" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "sourceAvailable"
	}
	if field.Kind() == protoreflect.BoolKind && field.JSONName() == "ready" {
		switch descriptor.FullName() {
		case "controlplane.v1.RuntimeEnvironmentSet", "controlplane.v1.RuntimeEnvironmentReadiness":
			return false, true
		}
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowCardSummary" {
		if field.Kind() == protoreflect.BoolKind {
			return false, field.JSONName() == "hasHumanGate"
		}
		if field.Kind() == protoreflect.Int32Kind {
			switch field.JSONName() {
			case "stageCount", "uniqueAgentCount", "parallelGroupCount", "activeRunCount", "pendingGateCount":
				return float64(0), true
			}
		}
	}
	if descriptor.FullName() == "controlplane.v1.Project" && field.Kind() == protoreflect.Int32Kind {
		switch field.JSONName() {
		case "agentCount", "workflowCount", "activeRunCount", "pendingGateCount":
			return float64(0), true
		}
	}
	if field.Kind() == protoreflect.Int64Kind {
		switch descriptor.FullName() {
		case "controlplane.v1.IntegrationDefinition":
			return float64(0), field.JSONName() == "connectionCount" || field.JSONName() == "healthyConnectionCount"
		case "controlplane.v1.ProviderAccountBlockerCount":
			return float64(0), field.JSONName() == "total"
		case "controlplane.v1.ProviderAccountDeletion":
			return float64(0), field.JSONName() == "pendingCleanup"
		case "controlplane.v1.ListProviderAccountBlockersResponse":
			return float64(0), field.JSONName() == "total" || field.JSONName() == "hiddenCount" || field.JSONName() == "deletionIntentVersion"
		}
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccountBlocker" {
		if field.Kind() == protoreflect.BoolKind && field.JSONName() == "canCancel" {
			return false, true
		}
		if field.Kind() == protoreflect.StringKind && field.JSONName() == "name" {
			return "", true
		}
	}
	if descriptor.FullName() == "controlplane.v1.AgentInstructionsBinding" && field.Kind() == protoreflect.BoolKind && field.JSONName() == "effective" {
		return false, true
	}
	// Some proto3 zero values correspond to required OpenAPI fields; the list is explicit because other empty scalars may mean an absent reference.
	// Некоторым нулевым proto3-значениям соответствует обязательное OpenAPI-поле; список явный, поскольку другие пустые scalar могут означать отсутствие ссылки.
	if field.JSONName() == "total" && field.Kind() == protoreflect.Int64Kind {
		switch descriptor.FullName() {
		case "controlplane.v1.ListRunsResponse", "controlplane.v1.ListArtifactsResponse", "controlplane.v1.ListConfigOverlayRevisionsResponse":
			return float64(0), true
		}
	}
	if descriptor.FullName() == "controlplane.v1.TokenUsage" && isProto64BitInteger(field.Kind()) {
		return float64(0), true
	}
	if field.Kind() == protoreflect.StringKind {
		switch descriptor.FullName() {
		case "controlplane.v1.IntegrationConfigurationField":
			return "", field.JSONName() == "help"
		case "controlplane.v1.RuntimeEnvironmentSet", "controlplane.v1.RuntimeEnvironmentDraftSpecification":
			return "", field.JSONName() == "description"
		case "controlplane.v1.RuntimeEnvironmentTool":
			return "", field.JSONName() == "description" || field.JSONName() == "usageHint"
		case "controlplane.v1.RuntimeEnvironmentValue":
			return "", field.JSONName() == "value"
		case "controlplane.v1.OwnerGateDecisionConsequence":
			return "", field.JSONName() == "safeSummary"
		case "controlplane.v1.IntegrationIntent":
			return "", field.JSONName() == "connectionName"
		case "controlplane.v1.ProviderAccountCandidate":
			return "", field.JSONName() == "defaultReasoningEffort"
		case "controlplane.v1.ConfigOverlayField":
			return "", field.JSONName() == "defaultValue"
		case "controlplane.v1.ConfigOverlayDiagnostic":
			return "", field.JSONName() == "key"
		case "controlplane.v1.ConfigOverlayVersion":
			return "", field.JSONName() == "content"
		case "controlplane.v1.AgentRuntimeConfigurationView":
			return "", field.JSONName() == "safeEffectiveConfig"
		case "controlplane.v1.ProviderAccount":
			return "", field.JSONName() == "externalAccountMasked"
		case "controlplane.v1.ModelCapability":
			return "", field.JSONName() == "defaultReasoningEffort"
		case "controlplane.v1.ProviderDefinition":
			return "", field.JSONName() == "description" || field.JSONName() == "defaultModelId"
		}
	}
	if descriptor.FullName() == "controlplane.v1.ConfigOverlayDiagnostic" && field.Kind() == protoreflect.Int32Kind {
		return float64(0), field.JSONName() == "line" || field.JSONName() == "column"
	}
	if descriptor.FullName() == "controlplane.v1.RunSessionArchiveTask" && field.Kind() == protoreflect.Int32Kind {
		return float64(0), field.JSONName() == "attempt"
	}
	if descriptor.FullName() == "controlplane.v1.ProviderFinancialWindow" && field.Kind() == protoreflect.Int32Kind {
		return nil, false
	}
	if descriptor.FullName() == "controlplane.v1.ProviderFinancialCredits" && field.Kind() == protoreflect.BoolKind {
		return false, false
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccount" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "enabled" || field.JSONName() == "ready"
	}
	if descriptor.FullName() == "controlplane.v1.ProviderAccountUsage" {
		if field.Kind() == protoreflect.BoolKind {
			return false, field.JSONName() == "allowedToSubmit" || field.JSONName() == "eligibleForSelection"
		}
		if isProto64BitInteger(field.Kind()) {
			return float64(0), field.JSONName() == "agentVersion" || field.JSONName() == "activeExecutions"
		}
	}
	if descriptor.FullName() == "controlplane.v1.WorkflowLaunchReadiness" && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "allowedToSubmit"
	}
	if field.Kind() == protoreflect.BoolKind {
		switch descriptor.FullName() {
		case "controlplane.v1.OwnerGateDecisionConsequence":
			return false, field.JSONName() == "executesExternalEffect" || field.JSONName() == "terminalForRun"
		case "controlplane.v1.IntegrationCapability":
			return false, field.JSONName() == "approvalRequired"
		case "controlplane.v1.IntegrationConfigurationField":
			return false, field.JSONName() == "required"
		case "controlplane.v1.IntegrationGrant":
			return false, field.JSONName() == "enabled"
		case "controlplane.v1.IntegrationDefinition":
			return false, field.JSONName() == "builtIn" || field.JSONName() == "available"
		case "controlplane.v1.IntegrationConnection":
			return false, field.JSONName() == "credentialsConfigured"
		}
	}
	if (descriptor.FullName() == "controlplane.v1.ModelCapability" || descriptor.FullName() == "controlplane.v1.ProviderDefinition") && field.Kind() == protoreflect.BoolKind {
		return false, field.JSONName() == "available" || field.JSONName() == "ready"
	}
	return nil, false
}

// normalizeProtoField recursively normalizes maps and ordinary message objects, preserving well-known arbitrary JSON values, and handles closed enum projections and exact safe integer conversion. Only integration minimum/maximum may retain signed out-of-safe-range integers as strings; non-map message JSON passes through here, so this helper is not a universal schema validator.
// normalizeProtoField рекурсивно нормализует map и обычные message-объекты, сохраняя well-known произвольный JSON, и преобразует закрытые enum и точные безопасные целые. Только integration minimum/maximum могут сохранить signed целые вне безопасного диапазона строкой; message JSON не в форме map здесь проходит без изменений, поэтому helper не заменяет всю проверку схемы.
func normalizeProtoField(value any, field protoreflect.FieldDescriptor) (any, error) {
	if field.IsMap() {
		items, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("public protobuf map shape is invalid")
		}
		for key, item := range items {
			normalized, err := normalizeProtoField(item, field.MapValue())
			if err != nil {
				return nil, err
			}
			items[key] = normalized
		}
		return items, nil
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		if field.Message().FullName() == "google.protobuf.Struct" ||
			field.Message().FullName() == "google.protobuf.Value" ||
			field.Message().FullName() == "google.protobuf.ListValue" {
			return value, nil
		}
		item, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		return item, normalizeProtoJSONShape(item, field.Message())
	}
	switch field.Kind() {
	case protoreflect.EnumKind:
		if field.Enum().FullName() == "controlplane.v1.UsageCompleteness" {
			switch value {
			case "USAGE_COMPLETENESS_UNSPECIFIED":
				return "UNKNOWN", nil
			case "USAGE_COMPLETENESS_PARTIAL":
				return "PARTIAL", nil
			case "USAGE_COMPLETENESS_COMPLETE":
				return "COMPLETE", nil
			default:
				return nil, errors.New("public usage completeness is invalid")
			}
		}
		if field.Enum().FullName() == "controlplane.v1.MessageOrigin" {
			switch value {
			case "MESSAGE_ORIGIN_ORDINARY":
				return "ORDINARY", nil
			case "MESSAGE_ORIGIN_CALLBACK_CONTINUATION":
				return "CALLBACK_CONTINUATION", nil
			default:
				return nil, errors.New("public message origin is invalid")
			}
		}
		if normalized, owned, err := normalizeRunSessionReadinessEnum(value, field); owned {
			return normalized, err
		}
		if field.Enum().FullName() == "controlplane.v1.RuntimeResourceScopeKind" {
			name, ok := value.(string)
			kind := runtimeResourceScopeKind(name)
			if !ok || !kind.Valid() {
				return nil, errors.New("public runtime resource scope is invalid")
			}
			return string(kind), nil
		}
		if prefix, ok := runtimeEnvironmentEnumPrefixes[field.Enum().FullName()]; ok {
			name, valid := value.(string)
			item := field.Enum().Values().ByName(protoreflect.Name(name))
			if !valid || item == nil || item.Number() == 0 || !strings.HasPrefix(name, prefix) {
				return nil, errPublicRuntimeEnvironmentShape
			}
			return strings.TrimPrefix(name, prefix), nil
		}
		if field.Enum().FullName() == "controlplane.v1.AgentAvatar.Source" {
			name, ok := value.(string)
			item := field.Enum().Values().ByName(protoreflect.Name(name))
			if !ok || item == nil || item.Number() == 0 || !strings.HasPrefix(name, "SOURCE_") {
				return nil, errPublicAvatarShape
			}
			return strings.TrimPrefix(name, "SOURCE_"), nil
		}
		return normalizeIntegrationEnum(value, field.Enum())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("public protobuf int64 shape is invalid")
		}
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, errors.New("public protobuf int64 exceeds JSON safe range")
		}
		if parsed < -maximumSafeJSONInteger || parsed > maximumSafeJSONInteger {
			// Range metadata retains the full int64 as text without browser rounding.
			// Метаданные диапазона сохраняют полный int64 строкой без округления в браузере.
			if field.ContainingMessage().FullName() == "controlplane.v1.IntegrationConfigurationField" && (field.Name() == "minimum" || field.Name() == "maximum") {
				return strconv.FormatInt(parsed, 10), nil
			}
			return nil, errors.New("public protobuf int64 exceeds JSON safe range")
		}
		return float64(parsed), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("public protobuf uint64 shape is invalid")
		}
		parsed, err := strconv.ParseUint(text, 10, 64)
		if err != nil || parsed > uint64(maximumSafeJSONInteger) {
			return nil, errors.New("public protobuf uint64 exceeds JSON safe range")
		}
		return float64(parsed), nil
	default:
		return value, nil
	}
}

// runtimeEnvironmentEnumPrefixes binds the five supported runtime enum descriptors to exact wire prefixes, requiring known nonzero values before prefix removal; it is not a network or Kubernetes permission registry.
// runtimeEnvironmentEnumPrefixes связывает пять поддерживаемых описателей runtime enum с точными wire-префиксами и требует известные ненулевые значения до удаления префикса; это не реестр сетевых или Kubernetes-прав.
var runtimeEnvironmentEnumPrefixes = map[protoreflect.FullName]string{
	"controlplane.v1.RuntimeVolumeKind":           "RUNTIME_VOLUME_KIND_",
	"controlplane.v1.RuntimeNetworkDestination":   "RUNTIME_NETWORK_DESTINATION_",
	"controlplane.v1.RuntimeNetworkProtocol":      "RUNTIME_NETWORK_PROTOCOL_",
	"controlplane.v1.RuntimeWebAccessMode":        "RUNTIME_WEB_ACCESS_MODE_",
	"controlplane.v1.RuntimeKubernetesAccessKind": "RUNTIME_KUBERNETES_ACCESS_KIND_",
}

// isProto64BitInteger recognizes signed, unsigned, zigzag and fixed-width 64-bit protobuf integer kinds for safe-number/default handling, excluding 32-bit and floating kinds.
// isProto64BitInteger распознаёт signed, unsigned, zigzag и fixed64 виды protobuf для безопасных чисел и defaults, исключая32-битные и дробные виды.
func isProto64BitInteger(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return true
	default:
		return false
	}
}

// ProtoMap exposes the same validated public protobuf projection to realtime and HTTP, including typed enums and redaction; it does not authorize resource access or mutate the protobuf input.
// ProtoMap предоставляет realtime и HTTP одну проверяемую публичную protobuf-проекцию с типизированными enum и удалением закрытых полей; доступ не авторизует и исходный protobuf не меняет.
func ProtoMap(message proto.Message) (map[string]any, error) { return messageMap(message) }

// LocalizeSafeErrors recursively replaces explicit i18n markers, mail health summaries and safeErrorCode messages in a public JSON projection. It preserves message text/references and typed intent/readiness exclusions, and treats assistant pins specially; this is formatting of already projected data, not arbitrary upstream-error sanitization.
// LocalizeSafeErrors рекурсивно заменяет явные i18n-маркеры, почтовые health-сводки и сообщения safeErrorCode публичной JSON-проекции. Сохраняет текст и ссылки сообщений и исключения typed intent/readiness, отдельно обрабатывая assistant pins; это оформление уже спроецированных данных, а не очистка произвольных upstream-ошибок.
func LocalizeSafeErrors(value any, localize func(string) string) {
	if localize == nil {
		return
	}
	switch current := value.(type) {
	case []any:
		for _, item := range current {
			LocalizeSafeErrors(item, localize)
		}
	case map[string]any:
		// AssistantRunPin carries literal identity, not enum-like user strings; only its closed scope is normalized.
		// AssistantRunPin содержит literal identity, а не enum-подобные пользовательские строки; нормализуется только его закрытый scope.
		_, pinConversation := current["conversationRef"]
		_, pinAssistant := current["assistantRef"]
		_, pinOrganization := current["organizationRef"]
		if pinConversation && pinAssistant && pinOrganization {
			if text, ok := current["scope"].(string); ok {
				current["scope"] = normalizeEnum(text)
			}
			return
		}
		for key, item := range current {
			_, publishedMessage := current["phase"]
			if publishedMessage && (key == "text" || key == "ref") {
				continue
			}
			// Session readiness is a closed code-only projection; a generic safeErrorMessage would violate its public contract.
			// Session readiness — закрытая code-only проекция; общий safeErrorMessage нарушил бы её публичный контракт.
			if key == "integrationIntent" || key == "sessionReadiness" {
				continue
			}
			LocalizeSafeErrors(item, localize)
			if text, ok := item.(string); ok && (key == "lastTestOutcome" || key == "resultSummary") && strings.HasPrefix(text, emailbridgeapi.HealthSummaryPrefix) {
				current[key] = emailbridgeapi.LocalizeHealthSummary(text, localize)
			}
			if text, ok := item.(string); ok && strings.HasPrefix(text, "i18n:") {
				current[key] = localize(strings.TrimPrefix(text, "i18n:"))
			}
		}
		code, _ := current["safeErrorCode"].(string)
		if code != "" {
			current["safeErrorMessage"] = localize(strings.ToUpper(code))
		}
	}
}

// enumPrefixes is the ordered list of public enum prefixes used by heuristic normalization; the first matching prefix is stripped and unrelated strings are retained.
// enumPrefixes — упорядоченный список публичных enum-префиксов эвристической нормализации; удаляется первый совпавший префикс, посторонние строки сохраняются.
var enumPrefixes = []string{
	"PLATFORM_ROLE_", "PROJECT_PERMISSION_", "NEXT_ACTION_", "ENTITY_LIFECYCLE_",
	"AGENT_STATE_", "INSTRUCTION_STATE_", "WORKFLOW_STATE_", "RUN_STATE_", "RUN_SOURCE_",
	"RUN_NODE_TYPE_", "RUN_NODE_STATE_", "RUN_EDGE_TYPE_", "RUN_EVENT_TYPE_",
	"RUN_EVENT_ACTOR_KIND_", "RUN_EVENT_MESSAGE_KIND_", "RUN_TOOL_CALL_STATE_", "RUN_MESSAGE_PHASE_",
	"OWNER_GATE_STATE_", "OWNER_GATE_DECISION_", "ARTIFACT_SCAN_STATE_", "ARTIFACT_SOURCE_", "ARTIFACT_LIFECYCLE_STATE_",
	"ATTACHMENT_SET_STATE_", "ATTACHMENT_SET_PURPOSE_",
	"SCHEDULE_STATE_", "CONNECTION_STATE_", "ASSISTANT_RUNTIME_STATE_", "ASSISTANT_PLAN_STATE_", "ASSISTANT_CONVERSATION_STATE_", "ASSISTANT_SCOPE_",
	"PROVIDER_ACCOUNT_STATE_", "PROVIDER_AUTHORIZATION_METHOD_", "PROVIDER_AUTHORIZATION_STATE_",
	"PROVIDER_ACCOUNT_DELETION_STATE_", "PROVIDER_ACCOUNT_VERIFICATION_STATE_", "PROVIDER_ACCOUNT_VERIFICATION_SCOPE_", "PROVIDER_ACCOUNT_BLOCKER_KIND_", "PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_",
	"PROVIDER_ACCOUNT_USAGE_PURPOSE_", "PROVIDER_USAGE_STATE_", "PROVIDER_USAGE_REASON_", "PROVIDER_USAGE_REMEDIATION_", "PROVIDER_HEALTH_SCOPE_",
	"RUNTIME_SECRET_VALUE_TYPE_",
	"SEARCH_RESULT_KIND_",
	"PERMISSION_RISK_", "ACCESS_SUBJECT_KIND_", "ACCESS_SCOPE_KIND_", "ACCESS_RESOURCE_KIND_",
	"ACCESS_ROLE_KIND_", "ACCESS_ROLE_STATE_", "ACCESS_BINDING_STATE_", "ACCESS_BINDING_MANAGEMENT_KIND_", "OIDC_GROUP_STATE_",
	"ACCESS_DECISION_", "ACTION_", "TYPE_",
}

// normalize recursively mutates public JSON maps/lists, strips known enum prefixes outside explicit literal-data exclusions, renames catalog fields, flattens runtime/workflow views and restores selected collection defaults. Shape-presence heuristics are projection rules, not schema validation, eligibility or mutation of authoritative protobuf state.
// normalize рекурсивно меняет публичные JSON map/list, убирает известные enum-префиксы вне явных исключений literal data, переименовывает поля каталогов, разворачивает runtime/workflow и восстанавливает выбранные коллекции. Эвристики наличия полей задают проекцию, а не проверку схемы, допуск или изменение авторитетного protobuf.
func normalize(value any) {
	switch current := value.(type) {
	case []any:
		for index, item := range current {
			normalize(item)
			if text, ok := item.(string); ok {
				current[index] = normalizeEnum(text)
			}
		}
	case map[string]any:
		for key, item := range current {
			// Public message text and stable references are literal data, even when they match an enum prefix.
			// Публичные текст и стабильные ссылки сообщения — literal data даже при совпадении с enum-префиксом.
			_, publishedMessage := current["phase"]
			if publishedMessage && (key == "text" || key == "ref") {
				continue
			}
			// A reference is literal owner data even when it matches an enum prefix.
			// Ссылка — literal data владельца даже при совпадении с enum-префиксом.
			if key == "currentRunRef" {
				continue
			}
			// The typed intent was already checked against Proto; its owner data is not an enum.
			// Типизированный intent уже проверен по Proto; его данные владельца не являются enum.
			if key == "integrationIntent" || key == "decisionConsequences" {
				continue
			}
			// Context names and references are literal owner data, not enums.
			// Имена и ссылки контекста — literal data владельца, а не enum.
			_, contextRoute := current["route"]
			_, contextOperations := current["allowedOperations"]
			if contextRoute && contextOperations && (key == "route" || key == "entityKind" || key == "entityRef" || key == "entityName") {
				continue
			}
			_, blockerCancellation := current["canCancel"]
			if blockerCancellation && (key == "name" || key == "ref" || key == "projectRef") {
				continue
			}
			normalize(item)
			if text, ok := item.(string); ok {
				current[key] = normalizeEnum(text)
			}
		}
		if permissions, ok := current["projectPermissions"]; ok {
			current["permissions"] = permissions
			delete(current, "projectPermissions")
		}
		if grants, ok := current["integrationGrantRefs"]; ok {
			current["integrations"] = grants
			delete(current, "integrationGrantRefs")
		}
		if bindings, ok := current["agentBindingRefs"]; ok {
			current["agentBindings"] = bindings
			delete(current, "agentBindingRefs")
		}
		if runtimeValue, ok := current["runtime"].(map[string]any); ok {
			current["runtimeRef"] = runtimeValue["ref"]
			current["runtimeName"] = runtimeValue["name"]
			current["runtimeRevision"] = runtimeValue["revision"]
			current["runtimeProvider"] = runtimeValue["provider"]
			current["runtimeModel"] = runtimeValue["model"]
			runtimeReady, _ := runtimeValue["ready"].(bool)
			current["runtimeReady"] = runtimeReady
			delete(current, "runtime")
		}
		if assistantState, ok := current["corePromptRevision"]; ok && assistantState != nil {
			current["system"] = true
			current["removable"] = false
		}
		normalizeAssistantShape(current)
		if targetType, targetRef, ok := target(current); ok {
			current["type"] = targetType
			current["ref"] = targetRef
			if version, present := current["targetVersion"]; present {
				current["version"] = version
				delete(current, "targetVersion")
			}
			delete(current, "agentRef")
			delete(current, "workflowRef")
			delete(current, "systemAssistantRef")
		}
		if _, isWorkflow := current["publishedVersion"]; isWorkflow {
			flattenWorkflow(current)
		} else if _, hasDraft := current["draftVersion"]; hasDraft {
			flattenWorkflow(current)
		}
		ensureRequiredCollections(current)
	}
}

// normalizeAssistantShape restores route/plan/receipt defaults and reshapes recognized plan-operation targets in place, removing obsolete source keys only on matching shapes. It neither applies a plan nor treats selected/permitted display booleans as an execution grant.
// normalizeAssistantShape восстанавливает defaults маршрута, плана и receipt и перестраивает распознанные цели операций на месте, удаляя исходные ключи только при совпавшей форме. План не применяется, отображаемые selected/permitted не становятся grant выполнения.
func normalizeAssistantShape(value map[string]any) {
	if _, hasRoute := value["route"]; hasRoute {
		if _, hasOperations := value["allowedOperations"]; hasOperations {
			for _, key := range []string{"entityKind", "entityRef", "entityName"} {
				if _, exists := value[key]; !exists {
					value[key] = ""
				}
			}
		}
	}
	if _, isPlan := value["auditSummary"]; isPlan {
		if _, exists := value["applied"]; !exists {
			value["applied"] = false
		}
		for _, key := range []string{"operations", "validationProblems", "nextActions"} {
			if _, exists := value[key]; !exists {
				value[key] = []any{}
			}
		}
	}
	if _, isReceipt := value["planRevision"]; isReceipt {
		if operations, exists := value["operations"]; exists {
			value["operationReceipts"] = operations
			delete(value, "operations")
		}
		for _, key := range []string{"operationReceipts", "conflicts", "auditRefs", "createdResourceRefs"} {
			if _, exists := value[key]; !exists {
				value[key] = []any{}
			}
		}
	}
	if _, hasType := value["type"]; !hasType {
		if _, isSystemAssistant := value["corePromptRevision"]; isSystemAssistant {
			delete(value, "systemLabel")
		}
		return
	}
	if _, hasAction := value["action"]; !hasAction {
		return
	}
	targetKind, hasTargetKind := value["targetKind"]
	if !hasTargetKind {
		return
	}
	target := map[string]any{"kind": targetKind, "name": ""}
	if targetName, exists := value["targetName"]; exists {
		target["name"] = targetName
	}
	if targetRef, exists := value["targetRef"]; exists && targetRef != "" {
		target["ref"] = targetRef
	}
	if targetVersion, exists := value["targetVersion"]; exists {
		target["version"] = targetVersion
	}
	value["target"] = target
	delete(value, "targetKind")
	delete(value, "targetRef")
	delete(value, "targetName")
	delete(value, "targetVersion")
	for _, key := range []string{"parameters", "before", "after"} {
		if _, exists := value[key]; !exists {
			value[key] = map[string]any{}
		}
	}
	for _, key := range []string{"selected", "permitted"} {
		if _, exists := value[key]; !exists {
			value[key] = false
		}
	}
	if _, exists := value["validationProblems"]; !exists {
		value["validationProblems"] = []any{}
	}
}

// normalizeEnum strips only the first matching configured prefix and otherwise returns the original string; callers must exempt literal data before calling it.
// normalizeEnum убирает только первый настроенный совпавший префикс, иначе возвращая исходную строку; literal data вызывающий должен исключить заранее.
func normalizeEnum(value string) string {
	for _, prefix := range enumPrefixes {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	return value
}

// ensureRequiredCollections fills absent collections selected by shape heuristics and missing workflow-step scalar defaults. Present values, even of an unexpected type, are not repaired or validated here.
// ensureRequiredCollections заполняет отсутствующие коллекции по эвристике формы и пропущенные scalar defaults шага процесса. Уже присутствующие значения, даже неверного типа, здесь не исправляются и не проверяются.
func ensureRequiredCollections(value map[string]any) {
	for _, key := range requiredCollectionKeys(value) {
		if _, exists := value[key]; !exists {
			value[key] = []any{}
		}
	}
	if _, isStep := value["position"]; isStep {
		if _, exists := value["parallel"]; !exists {
			value["parallel"] = false
		}
		if _, exists := value["parallelGroup"]; !exists {
			value["parallelGroup"] = float64(0)
		}
		if _, exists := value["expectedResult"]; !exists {
			value["expectedResult"] = ""
		}
		if _, exists := value["humanGate"]; !exists {
			value["humanGate"] = false
		}
	}
}

// requiredCollectionKeys returns fresh required-key candidates for recognized workflow-step, complete-agent, membership and graph shapes without editing the map or validating its values.
// requiredCollectionKeys возвращает новый список обязательных ключей для распознанных шага, полного сотрудника, членства и графа без изменения map и проверки значений.
func requiredCollectionKeys(value map[string]any) []string {
	keys := []string{}
	if _, isStep := value["position"]; isStep {
		keys = append(keys, "gateDecisions", "requiredCapabilityKeys")
	}
	_, isAgent := value["roleDescription"]
	_, hasRef := value["ref"]
	_, hasProject := value["projectRef"]
	_, hasVersion := value["version"]
	if isAgent && hasRef && hasProject && hasVersion {
		keys = append(keys, "capabilities", "integrations", "knowledgeArtifactRefs", "nextActions")
	}
	_, hasPlatformRole := value["platformRole"]
	_, hasMembershipUser := value["user"]
	if hasPlatformRole && hasMembershipUser {
		keys = append(keys, "permissions", "nextActions")
	}
	if _, isGraph := value["sequence"]; isGraph {
		if _, hasRevision := value["revision"]; hasRevision {
			keys = append(keys, "nodes", "edges")
		}
	}
	return keys
}

// target recognizes only the closed RunTarget key set with exactly one agent/workflow/system-assistant selector, returning its raw value. Unknown keys or ambiguous selector presence reject the shape; reference type, syntax and authority remain unchecked here.
// target распознаёт только закрытый набор ключей RunTarget с единственным селектором сотрудника, процесса либо помощника, возвращая исходное значение. Неизвестные ключи и неоднозначное наличие селекторов отклоняются; тип, формат ссылки и полномочия здесь не проверяются.
func target(value map[string]any) (string, any, bool) {
	// RunTarget is a closed oneof with three selectors; RunNode, WorkflowStep and grants also have agentRef, so any unknown key excludes that map from target projection.
	// RunTarget — закрытый oneof с тремя селекторами; у RunNode, WorkflowStep и grants тоже есть agentRef, поэтому неизвестный ключ исключает такую map из проекции цели.
	for key := range value {
		switch key {
		case "agentRef", "workflowRef", "systemAssistantRef", "displayName", "targetVersion":
		default:
			return "", nil, false
		}
	}
	agentRef, hasAgent := value["agentRef"]
	workflowRef, hasWorkflow := value["workflowRef"]
	systemRef, hasSystem := value["systemAssistantRef"]
	if hasSystem {
		if hasAgent || hasWorkflow {
			return "", nil, false
		}
		return "SYSTEM_ASSISTANT", systemRef, true
	}
	if hasAgent == hasWorkflow {
		return "", nil, false
	}
	if hasAgent {
		ref := agentRef
		return "AGENT", ref, true
	}
	return "WORKFLOW", workflowRef, true
}

// flattenWorkflow preserves separate published/draft pins, copies a bounded draft view and selects the published body when present, otherwise the draft. It flattens selected fields, supplies missing collections and removes nested source snapshots without changing owner revision state.
// flattenWorkflow сохраняет раздельные published/draft pins, копирует ограниченный draft-view и выбирает опубликованное тело при наличии, иначе черновик. Разворачивает выбранные поля, заполняет отсутствующие коллекции и удаляет вложенные исходные снимки без изменения ревизий владельца.
func flattenWorkflow(value map[string]any) {
	for source, field := range map[string]string{"publishedVersion": "publishedRevisionRef", "draftVersion": "draftRevisionRef"} {
		if snapshot, ok := value[source].(map[string]any); ok {
			value[field] = snapshot["ref"]
		}
	}
	if draft, ok := value["draftVersion"].(map[string]any); ok {
		snapshot := map[string]any{}
		for _, key := range []string{"ref", "version", "revision", "state", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria", "executionMode", "validationMessages"} {
			if item, exists := draft[key]; exists {
				snapshot[key] = item
			}
		}
		value["draft"] = snapshot
	}
	version, _ := value["publishedVersion"].(map[string]any)
	if version == nil {
		version, _ = value["draftVersion"].(map[string]any)
	}
	if version != nil {
		value["revisionRef"] = version["ref"]
	}
	for _, key := range []string{"revision", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria", "executionMode", "validationMessages"} {
		if item, exists := version[key]; exists {
			value[key] = item
		}
	}
	for _, key := range []string{"inputFields", "steps", "validationMessages", "nextActions"} {
		if _, exists := value[key]; !exists {
			value[key] = []any{}
		}
	}
	delete(value, "publishedVersion")
	delete(value, "draftVersion")
}

// targetProto maps AGENT to its typed oneof and every other input kind to WORKFLOW, without validating the reference or supporting a system-assistant branch; callers must validate their allowed kinds first.
// targetProto преобразует AGENT в соответствующий oneof, а любой другой вид — в WORKFLOW без проверки ссылки и без ветви помощника; допустимые виды обязан заранее проверить вызывающий.
func targetProto(kind, reference string) *controlplanev1.RunTarget {
	result := &controlplanev1.RunTarget{}
	if kind == "AGENT" {
		result.Target = &controlplanev1.RunTarget_AgentRef{AgentRef: reference}
	} else {
		result.Target = &controlplanev1.RunTarget_WorkflowRef{WorkflowRef: reference}
	}
	return result
}

// permissions maps known public project-permission names into a newly allocated protobuf slice, preserving order and duplicates while silently omitting unknown values; it does not grant those permissions.
// permissions преобразует известные публичные project-permission имена в новый protobuf slice с сохранением порядка и дубликатов и молчаливым пропуском неизвестных значений; права при этом не выдаются.
func permissions[T ~string](values []T) []controlplanev1.ProjectPermission {
	result := make([]controlplanev1.ProjectPermission, 0, len(values))
	for _, item := range values {
		if value, ok := controlplanev1.ProjectPermission_value["PROJECT_PERMISSION_"+string(item)]; ok {
			result = append(result, controlplanev1.ProjectPermission(value))
		}
	}
	return result
}

// platformRole looks up the prefixed protobuf role name, yielding the zero enum for an unknown name rather than returning a validation error or assigning membership.
// platformRole ищет protobuf-роль по имени с префиксом, возвращая нулевой enum для неизвестного имени без ошибки проверки и назначения членства.
func platformRole(value string) controlplanev1.PlatformRole {
	return controlplanev1.PlatformRole(controlplanev1.PlatformRole_value["PLATFORM_ROLE_"+value])
}

// gateDecision looks up the prefixed owner-decision enum, returning zero for an unknown name; it does not record, approve or execute a decision.
// gateDecision ищет enum решения владельца по имени с префиксом и возвращает ноль для неизвестного; решение не записывается, не подтверждается и не выполняется.
func gateDecision(value string) controlplanev1.OwnerGateDecision {
	return controlplanev1.OwnerGateDecision(controlplanev1.OwnerGateDecision_value["OWNER_GATE_DECISION_"+value])
}

// jsonEncoder constructs the standard streaming JSON encoder for the supplied writer with default escaping; encoding and write errors remain the caller's responsibility.
// jsonEncoder создаёт стандартный потоковый JSON encoder для writer с обычным escaping; ошибки encoding и записи остаются ответственностью вызывающего.
func jsonEncoder(writer io.Writer) *json.Encoder { return json.NewEncoder(writer) }

// requiresTerminalCursor recognizes the explicit seven owner response types whose public pages require nextPageToken even at the terminal empty cursor. Other page types retain their existing optional-field shape regardless of collection names.
// requiresTerminalCursor распознаёт семь явных типов ответов владельца, страницы которых требуют nextPageToken даже при пустом terminal cursor. Остальные страницы сохраняют необязательное поле независимо от названий коллекций.
func requiresTerminalCursor(message proto.Message) bool {
	switch message.(type) {
	case *controlplanev1.ListIntegrationConnectionsResponse,
		*controlplanev1.ListSchedulesResponse,
		*controlplanev1.ListAuditEventsResponse,
		*controlplanev1.ListScheduleRevisionsResponse,
		*controlplanev1.ListScheduleRunsResponse,
		*controlplanev1.ListRoleImageRecipeRevisionsResponse,
		*controlplanev1.ListProviderDefinitionsResponse:
		return true
	default:
		return false
	}
}
