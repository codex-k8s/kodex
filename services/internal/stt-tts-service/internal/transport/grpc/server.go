// Package grpc implements STT RPC framing, bounded stream admission and safe domain/transport mapping without owning user policy or provider credentials.
// RU: Пакет grpc реализует RPC framing STT, ограниченный допуск потоков и безопасное преобразование домена/транспорта, не владея пользовательской политикой или реквизитами провайдера.
package grpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/sttapi"
	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/authorization"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	transcriptionservice "github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/transport/grpc/casters"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service exposes the domain transcription, local readiness and separately authorized catalog/availability paths consumed by the RPC boundary.
// RU: Service задаёт потребляемые RPC-границей доменное распознавание, локальную готовность и отдельно разрешаемые пути каталога/доступности.
type Service interface {
	// Transcribe processes one seekable, bounded request under the supplied principal and returns its text and request-bound receipt.
	// RU: Transcribe обрабатывает один ограниченный запрос с перемоткой под переданным субъектом и возвращает текст с квитанцией, привязанной к запросу.
	Transcribe(context.Context, transcriptionservice.Input) (value.TranscriptionResult, error)
	// CheckLocal tests local runtime dependencies without implying provider or user-policy readiness.
	// RU: CheckLocal проверяет локальные зависимости исполнения без утверждения готовности провайдера или пользовательской политики.
	CheckLocal(context.Context) error
	// CheckProtectedPath probes the domain's delegated infrastructure path separately from the precomputed local ready snapshot.
	// RU: CheckProtectedPath проверяет делегированный инфраструктурный путь домена отдельно от готового локального readiness снимка.
	CheckProtectedPath(context.Context) error
	// CheckAvailability evaluates the exact principal's configured request path and returns a bounded validity/stage result without transcribing audio.
	// RU: CheckAvailability оценивает настроенный путь точного субъекта и возвращает ограниченные срок/этап без распознавания аудио.
	CheckAvailability(context.Context, value.Principal, string) (transcriptionservice.Availability, error)
	// Catalog returns the adapter's local model profile for inclusion in an already authorized response.
	// RU: Catalog возвращает локальный профиль моделей адаптера для включения в уже разрешённый ответ.
	Catalog() modelprofile.Catalog
	// GetModelCatalog authorizes the separate configuration-catalog read for the supplied principal.
	// RU: GetModelCatalog проверяет отдельное чтение каталога конфигурации для переданного субъекта.
	GetModelCatalog(context.Context, value.Principal) (modelprofile.Catalog, error)
}

// Readiness supplies only the precomputed process-ready snapshot, not a network probe triggered by the incoming RPC.
// RU: Readiness предоставляет только заранее вычисленный снимок готовности процесса, а не запускаемую входящим RPC сетевую проверку.
type Readiness interface {
	// Ready returns local admission state and its diagnostic reason without issuing downstream requests.
	// RU: Ready возвращает локальное состояние допуска и диагностическую причину без исходящих запросов.
	Ready() (bool, string)
}

// principalResolver maps the accepted request context and exact RPC method into a domain principal before stream admission.
// RU: principalResolver преобразует принятый context запроса и точный RPC метод в доменного субъекта до допуска потока.
type principalResolver func(context.Context, string) (value.Principal, error)

// Server owns one process's stream/byte budgets and canonical spool location while delegating policy, credentials and audio semantics to the domain.
// RU: Server владеет бюджетами потоков/байтов одного процесса и каноническим spool, делегируя политику, реквизиты и смысл аудио домену.
type Server struct {
	sttv1.UnimplementedSpeechToTextServiceServer
	service        Service
	spoolDirectory string
	readiness      Readiness
	requestTimeout time.Duration
	principal      principalResolver
	admission      *byteAdmission
}

// New validates required ports, an absolute clean spool path and the request-timeout range without creating files, listeners or provider calls.
// RU: New проверяет обязательные порты, абсолютный чистый путь spool и диапазон timeout без создания файлов, listener или вызовов провайдера.
func New(service Service, spoolDirectory string, readiness Readiness, requestTimeout time.Duration) (*Server, error) {
	if service == nil || readiness == nil || !filepath.IsAbs(spoolDirectory) || filepath.Clean(spoolDirectory) != spoolDirectory ||
		requestTimeout < time.Second || requestTimeout > 20*time.Second {
		return nil, errors.New("transcription transport configuration is invalid")
	}
	return &Server{
		service: service, spoolDirectory: spoolDirectory, readiness: readiness,
		requestTimeout: requestTimeout, principal: authorization.Principal,
		admission: &byteAdmission{},
	}, nil
}

// StreamServerInterceptor resolves accepted authority and reserves a process-wide stream slot before any receive, bounding the handler by request and authority expiry.
// RU: StreamServerInterceptor разрешает принятую authority и резервирует общий slot потока до любого чтения, ограничивая handler сроками запроса и полномочий.
// The reservation is released when the handler returns; a shorter transport deadline is preserved by the child context.
// RU: Резерв освобождается при возврате handler; более короткий транспортный срок сохраняется дочерним контекстом.
func (server *Server) StreamServerInterceptor() googlegrpc.StreamServerInterceptor {
	return func(service any, stream googlegrpc.ServerStream, info *googlegrpc.StreamServerInfo, handler googlegrpc.StreamHandler) error {
		principal, err := server.principal(stream.Context(), info.FullMethod)
		if err != nil {
			return statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
		}
		now := time.Now()
		deadline := now.Add(server.requestTimeout)
		if principal.ExpiresAt.Before(deadline) {
			deadline = principal.ExpiresAt
		}
		if !now.Before(deadline) {
			return transportError(context.DeadlineExceeded)
		}
		reservation, ok := server.admission.acquireStream()
		if !ok {
			return statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
		}
		defer reservation.release()
		ctx, cancel := context.WithDeadline(stream.Context(), deadline)
		defer cancel()
		ctx = context.WithValue(ctx, streamReservationContextKey{}, reservation)
		return handler(service, &deadlineServerStream{ServerStream: stream, ctx: ctx})
	}
}

// Transcribe requires accepted stream authority and its admission reservation, then handles either an exact one-frame availability probe or metadata/chunks/commit audio.
// RU: Transcribe требует принятую stream authority и её admission резерв, затем обрабатывает точную однофреймовую проверку доступности либо аудио metadata/chunks/commit.
// Audio reserves bytes before spool creation, requires exact size/digest/EOF and removes its temporary file on return; only the domain may invoke the provider.
// RU: Аудио резервирует байты до создания spool, требует точные size/digest/EOF и удаляет временный файл при возврате; провайдера вызывает только домен.
func (server *Server) Transcribe(stream sttv1.SpeechToTextService_TranscribeServer) error {
	ctx := stream.Context()
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_Transcribe_FullMethodName)
	if err != nil {
		return statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
	}
	reservation, ok := ctx.Value(streamReservationContextKey{}).(*streamReservation)
	if !ok || reservation == nil {
		return statusError(codes.Internal, "transcription admission is missing", "INTERNAL")
	}
	metadataMessage, err := stream.Recv()
	if err != nil {
		if ctx.Err() != nil {
			return transportError(ctx.Err())
		}
		return transportError(errs.ErrInvalidRequest)
	}
	if metadataMessage.GetAvailabilityCheck() != nil {
		if trailing, err := stream.Recv(); err != io.EOF || trailing != nil {
			return transportError(errs.ErrInvalidRequest)
		}
		if ready, _ := server.readiness.Ready(); !ready {
			return transportError(errs.ErrProviderUnavailable)
		}
		availability, err := server.service.CheckAvailability(ctx, principal, sharedobservability.CorrelationID(ctx))
		response := &sttv1.CheckProtectedPathResponse{Stage: protectedStage(availability.Stage), Catalog: sttapi.ModelCatalog(server.service.Catalog())}
		if err == nil {
			response.Ready = true
			response.ValidUntil = timestamppb.New(availability.ValidUntil)
		}
		if ctx.Err() != nil {
			return transportError(ctx.Err())
		}
		return stream.SendAndClose(&sttv1.TranscribeResponse{Availability: response})
	}
	if metadataMessage.GetMetadata() == nil || metadataMessage.GetMetadata().GetSizeBytes() == 0 ||
		metadataMessage.GetMetadata().GetSizeBytes() > uint64(value.MaximumAbsoluteBytes) {
		return transportError(errs.ErrInvalidRequest)
	}
	sizeBytes := int64(metadataMessage.GetMetadata().GetSizeBytes())
	if !reservation.reserveBytes(sizeBytes) {
		return statusError(codes.ResourceExhausted, "transcription capacity is exhausted", "RATE_LIMITED")
	}
	spool, err := os.CreateTemp(server.spoolDirectory, "request-*.audio")
	if err != nil {
		return statusError(codes.Unavailable, "transcription spool is unavailable", "UNAVAILABLE")
	}
	spoolName := spool.Name()
	defer func() {
		_ = spool.Close()
		_ = os.Remove(spoolName)
	}()
	written, digest, err := receiveAudio(stream, spool, sizeBytes)
	if err != nil {
		return transportError(err)
	}
	if written != sizeBytes || digest == "" {
		return transportError(errs.ErrInvalidRequest)
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return statusError(codes.Unavailable, "transcription spool is unavailable", "UNAVAILABLE")
	}
	result, err := server.service.Transcribe(ctx, casters.TranscriptionInput(
		principal, sharedobservability.CorrelationID(ctx), spool, sizeBytes, metadataMessage.GetMetadata().GetMediaType(),
	))
	if err != nil {
		return transportError(err)
	}
	return stream.SendAndClose(casters.TranscriptionResponse(result))
}

// receiveAudio streams bounded nonempty chunks into the output and SHA-256, accepting only an exact size/digest commit followed immediately by EOF.
// RU: receiveAudio записывает ограниченные непустые chunks в выход и SHA-256, принимая только точный size/digest commit с немедленным EOF.
// Cancellation is preserved; malformed framing or a short write cannot produce an accepted partial recording.
// RU: Отмена сохраняется; неверный framing или неполная запись не дают принятую частичную аудиозапись.
func receiveAudio(stream sttv1.SpeechToTextService_TranscribeServer, output io.Writer, expected int64) (int64, string, error) {
	digest := sha256.New()
	destination := io.MultiWriter(output, digest)
	var written int64
	for {
		message, err := stream.Recv()
		if err != nil {
			if stream.Context().Err() != nil {
				return 0, "", stream.Context().Err()
			}
			return 0, "", errs.ErrInvalidRequest
		}
		if chunk := message.GetChunk(); chunk != nil {
			if len(chunk) == 0 || len(chunk) > value.MaximumChunkBytes || int64(len(chunk)) > expected-written {
				return 0, "", errs.ErrInvalidRequest
			}
			count, writeErr := destination.Write(chunk)
			if writeErr != nil || count != len(chunk) {
				return 0, "", errs.ErrProviderUnavailable
			}
			written += int64(count)
			continue
		}
		commit := message.GetCommit()
		if commit == nil || written != expected || commit.GetSizeBytes() != uint64(expected) || !matchesDigest(digest, commit.GetSha256()) {
			return 0, "", errs.ErrInvalidRequest
		}
		if trailing, trailingErr := stream.Recv(); trailingErr != io.EOF || trailing != nil {
			return 0, "", errs.ErrInvalidRequest
		}
		return written, commit.GetSha256(), nil
	}
}

// matchesDigest compares the accumulated lowercase SHA-256 text with an exactly sized declaration using a constant-time byte comparison.
// RU: matchesDigest сравнивает накопленный строчный SHA-256 текст с заявлением точного размера через constant-time сравнение байтов.
func matchesDigest(digest hash.Hash, declared string) bool {
	if len(declared) != sha256.Size*2 {
		return false
	}
	actual := make([]byte, hex.EncodedLen(digest.Size()))
	hex.Encode(actual, digest.Sum(nil))
	return subtle.ConstantTimeCompare(actual, []byte(declared)) == 1
}

// CheckReadiness returns the same precomputed local snapshot used by process probes and reports Unavailable when false, without remote work.
// RU: CheckReadiness возвращает тот же готовый локальный снимок, что проверки процесса, и Unavailable при false без удалённой работы.
func (server *Server) CheckReadiness(ctx context.Context, _ *sttv1.CheckReadinessRequest) (*sttv1.CheckReadinessResponse, error) {
	ready, _ := server.readiness.Ready()
	if !ready {
		return &sttv1.CheckReadinessResponse{Ready: false}, statusError(codes.Unavailable, "STT local runtime is unavailable", "UNAVAILABLE")
	}
	return &sttv1.CheckReadinessResponse{Ready: true}, nil
}

// GetModelCatalog requires its distinct configuration principal and an empty known-field request, then delegates under the earliest caller/five-second/authority deadline.
// RU: GetModelCatalog требует отдельного субъекта конфигурации и пустой запрос без неизвестных полей, затем делегирует в более ранний срок клиента/пяти секунд/полномочий.
func (server *Server) GetModelCatalog(ctx context.Context, request *sttv1.GetModelCatalogRequest) (*sttv1.GetModelCatalogResponse, error) {
	principal, err := authorization.Principal(ctx, sttv1.SpeechToTextService_GetModelCatalog_FullMethodName)
	if err != nil {
		return nil, statusError(codes.Unauthenticated, "verified STT authorization context is required", "UNAUTHENTICATED")
	}
	if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
		return nil, transportError(errs.ErrInvalidRequest)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx, cancelAuthority := context.WithDeadline(ctx, principal.ExpiresAt)
	defer cancelAuthority()
	catalog, err := server.service.GetModelCatalog(ctx, principal)
	if err != nil {
		return nil, transportError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, transportError(err)
	}
	return &sttv1.GetModelCatalogResponse{Catalog: sttapi.ModelCatalog(catalog)}, nil
}

// CheckProtectedPath returns a fixed pending-authority technical response; real user availability is checked through the authorized stream probe instead.
// RU: CheckProtectedPath возвращает фиксированный технический ответ ожидания authority; настоящая пользовательская доступность проверяется через разрешённый stream probe.
func (server *Server) CheckProtectedPath(ctx context.Context, _ *sttv1.CheckProtectedPathRequest) (*sttv1.CheckProtectedPathResponse, error) {
	return &sttv1.CheckProtectedPathResponse{Ready: false, Stage: sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_DELEGATED_AUTHORITY}, nil
}

// protectedStage maps only the closed domain availability stages and falls back to delegated-authority pending for unknown or unmapped values.
// RU: protectedStage отображает только закрытые доменные этапы доступности, используя ожидание delegated authority для неизвестных или несопоставленных значений.
func protectedStage(stage value.Stage) sttv1.ProtectedPathStage {
	switch stage {
	case value.StagePolicy:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_POLICY
	case value.StageCredential:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_CREDENTIAL
	case value.StageEgress:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_EGRESS
	case value.StageProvider:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_PROVIDER
	case value.StageSuccess:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_READY
	default:
		return sttv1.ProtectedPathStage_PROTECTED_PATH_STAGE_DELEGATED_AUTHORITY
	}
}

// byteAdmission synchronizes this server instance's active stream count and total declared inflight audio bytes, not a cluster-wide quota.
// RU: byteAdmission синхронизирует число активных потоков и сумму заявленных байтов аудио экземпляра сервера, а не общекластерную квоту.
type byteAdmission struct {
	mu      sync.Mutex
	streams int
	bytes   int64
}

// streamReservationContextKey privately carries the interceptor-owned stream reservation into the handler context.
// RU: streamReservationContextKey приватно передаёт принадлежащий перехватчику резерв потока в context handler.
type streamReservationContextKey struct{}

// streamReservation owns one stream slot and an optional declared-byte reservation, releasing aggregate counts at most once.
// RU: streamReservation владеет одним slot потока и необязательным резервом заявленных байтов, освобождая общие счётчики не более одного раза.
type streamReservation struct {
	admission *byteAdmission
	once      sync.Once
	bytes     int64
}

// acquireStream atomically admits one stream below the fixed per-process cap without reserving audio bytes before metadata arrives.
// RU: acquireStream атомарно допускает поток ниже фиксированного предела процесса без резервирования аудиобайтов до metadata.
func (admission *byteAdmission) acquireStream() (*streamReservation, bool) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if admission.streams >= value.MaximumConcurrentStreams {
		return nil, false
	}
	admission.streams++
	return &streamReservation{admission: admission}, true
}

// snapshot reads a mutually consistent stream/byte count under the same lock used for aggregate reservation updates.
// RU: snapshot читает согласованные число потоков и байты под тем же lock, что изменения общих резервов.
func (admission *byteAdmission) snapshot() (int, int64) {
	admission.mu.Lock()
	defer admission.mu.Unlock()
	return admission.streams, admission.bytes
}

// reserveBytes records one positive bounded audio size if the shared byte budget permits it; each reservation is assigned by its single stream reader.
// RU: reserveBytes сохраняет один положительный ограниченный размер аудио при наличии общего бюджета байтов; резерв назначается единственным reader своего потока.
func (reservation *streamReservation) reserveBytes(size int64) bool {
	if reservation == nil || reservation.admission == nil || reservation.bytes != 0 {
		return false
	}
	admission := reservation.admission
	admission.mu.Lock()
	defer admission.mu.Unlock()
	if size <= 0 || size > value.MaximumAbsoluteBytes || admission.bytes > value.MaximumInflightBytes-size {
		return false
	}
	admission.bytes += size
	reservation.bytes = size
	return true
}

// release returns this reservation's stream and byte counts exactly once and safely accepts a nil/uninitialized reservation.
// RU: release возвращает счётчики потока и байтов этого резерва ровно один раз и безопасно принимает nil/неинициализированный резерв.
func (reservation *streamReservation) release() {
	if reservation == nil || reservation.admission == nil {
		return
	}
	reservation.once.Do(func() {
		admission := reservation.admission
		admission.mu.Lock()
		defer admission.mu.Unlock()
		admission.streams--
		admission.bytes -= reservation.bytes
	})
}

// deadlineServerStream exposes the bounded handler context and wraps send/receive waits while retaining the original gRPC transport.
// RU: deadlineServerStream предоставляет ограниченный context handler и оборачивает ожидание отправки/приёма, сохраняя исходный gRPC транспорт.
type deadlineServerStream struct {
	googlegrpc.ServerStream
	ctx context.Context
}

// Context returns the admitted deadline/cancellation context containing the private stream reservation.
// RU: Context возвращает допущенный deadline/cancellation context с приватным резервом потока.
func (stream *deadlineServerStream) Context() context.Context { return stream.ctx }

// RecvMsg waits for the underlying receive or context completion; transport shutdown remains responsible for unblocking an already running receive.
// RU: RecvMsg ждёт нижележащий приём либо завершение контекста; разблокированием уже начатого приёма по-прежнему управляет остановка транспорта.
func (stream *deadlineServerStream) RecvMsg(message any) error {
	return awaitStreamOperation(stream.ctx, func() error { return stream.ServerStream.RecvMsg(message) })
}

// SendMsg waits for the underlying send or context completion without claiming that an early timeout proves no bytes were sent.
// RU: SendMsg ждёт нижележащую отправку либо завершение контекста, не считая ранний timeout доказательством отсутствия отправленных байтов.
func (stream *deadlineServerStream) SendMsg(message any) error {
	return awaitStreamOperation(stream.ctx, func() error { return stream.ServerStream.SendMsg(message) })
}

// awaitStreamOperation starts one operation with a buffered result channel and returns either its error or the context's gRPC cancellation status.
// RU: awaitStreamOperation запускает одну операцию с буферизованным каналом результата и возвращает её ошибку либо gRPC статус отмены контекста.
// It does not forcibly cancel or join the callback after an early return; the underlying gRPC stream lifetime must finish that operation.
// RU: Функция не принуждает callback к отмене или завершению после раннего возврата; операцию должен завершить жизненный цикл нижележащего gRPC потока.
func awaitStreamOperation(ctx context.Context, operation func() error) error {
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	}
}
