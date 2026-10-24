package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const (
	// maximumAudioBytes caps the declared and forwarded audio payload at 25 MiB independently of the
	// multipart framing allowance, preventing oversized data from entering the STT stream.
	// maximumAudioBytes ограничивает объявленный и передаваемый аудио payload 25 MiB отдельно от
	// допуска multipart оформления, исключая слишком большие данные из STT потока.
	maximumAudioBytes = 25 << 20
	// maximumAudioChunkBytes bounds each audio read/send buffer to 64 KiB. Forwarding copies each used
	// slice before calling the stream sender so later reads cannot mutate the sent bytes.
	// maximumAudioChunkBytes ограничивает буфер чтения и отправки аудио 64 KiB. Перед stream sender
	// используемый срез копируется, чтобы следующее чтение не изменило переданные байты.
	maximumAudioChunkBytes = 64 << 10
	// maximumMultipartOverhead reserves 128 KiB for multipart boundaries and headers in the total HTTP
	// body limit; it does not increase the permitted audio payload.
	// maximumMultipartOverhead резервирует 128 KiB для multipart границ и заголовков в общем HTTP body
	// лимите; разрешённый размер аудио не увеличивается.
	maximumMultipartOverhead = 128 << 10
)

var (
	// errAudioSizeMismatch is the fixed classification for invalid declared audio size or observed
	// under/overrun, without exposing audio data or arbitrary reader errors.
	// errAudioSizeMismatch задаёт фиксированную классификацию неверного объявленного размера либо
	// недобора/перебора аудио без раскрытия аудиоданных и произвольных ошибок reader.
	errAudioSizeMismatch = errors.New("audio size does not match the declared size")
	// errAudioBodyRead marks non-EOF audio reader failure for safe local HTTP mapping, keeping the
	// underlying body-read error out of the response.
	// errAudioBodyRead обозначает ошибку чтения аудио кроме EOF для безопасного локального HTTP
	// отображения, исключая исходную ошибку body reader из ответа.
	errAudioBodyRead = errors.New("audio request body read failed")
)

// speechToTextClient aliases the generated STT service client used by HTTP transcription,
// availability and model-catalog calls, retaining their typed RPC contract.
// speechToTextClient задаёт alias generated клиента STT для HTTP транскрипции, availability и
// каталога моделей с сохранением типизированного RPC контракта.
type speechToTextClient = sttv1.SpeechToTextServiceClient

// AttachSpeechToText attaches a nonnil STT client exactly once during server composition and
// rejects nil or repeated attachment. It starts no worker and makes no provider call.
// AttachSpeechToText подключает ненулевой STT клиент ровно один раз при сборке сервера и отклоняет
// nil и повторное подключение. Worker не запускается и провайдер не вызывается.
func (server *Server) AttachSpeechToText(client speechToTextClient) error {
	if client == nil || server.speech != nil {
		return errors.New("STT attachment is invalid")
	}
	server.speech = client
	return nil
}

// TranscribeSpeech binds the project locator into trusted request routing before forwarding the
// declared audio size to the shared streaming handler. Project authorization remains with the
// protected STT path.
// TranscribeSpeech связывает locator проекта с доверенной маршрутизацией запроса перед передачей
// объявленного размера общему потоковому handler. Авторизация проекта остаётся у защищённого STT
// пути.
func (server *Server) TranscribeSpeech(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.TranscribeSpeechParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	server.transcribeSpeech(writer, request, parameters.XAudioSize)
}

// TranscribeOrganizationSpeech routes an organization transcription request to the shared bounded
// streaming handler without adding a project locator or deriving organization authority from
// request payload.
// TranscribeOrganizationSpeech направляет запрос организационной транскрипции общему ограниченному
// потоковому handler без добавления проекта и вывода полномочий организации из payload.
func (server *Server) TranscribeOrganizationSpeech(writer http.ResponseWriter, request *http.Request, parameters generated.TranscribeOrganizationSpeechParams) {
	server.transcribeSpeech(writer, request, parameters.XAudioSize)
}

// transcribeSpeech accepts exactly one supported multipart audio part, enforces declared/observed
// byte bounds, streams metadata and copied chunks, and commits the observed size/digest only after
// trailing-part rejection. A valid PROVIDER_COMPLETED receipt is required before returning text
// under no-store.
// transcribeSpeech принимает ровно одну поддерживаемую multipart audio часть, проверяет
// объявленный и наблюдённый размер, передаёт metadata и копии chunks и отправляет commit
// размера/digest только после отказа лишним частям. Для текста под no-store требуется корректный
// PROVIDER_COMPLETED receipt.
func (server *Server) transcribeSpeech(writer http.ResponseWriter, request *http.Request, declaredSize int64) {
	if server.speech == nil {
		writeLocalProblem(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
		return
	}
	if declaredSize < 1 || declaredSize > maximumAudioBytes {
		writeLocalProblem(writer, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
		return
	}
	if request.ContentLength > maximumAudioBytes+maximumMultipartOverhead {
		writeLocalProblem(writer, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
		return
	}
	if request.ContentLength >= 0 && request.ContentLength < declaredSize {
		writeLocalProblem(writer, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
		return
	}
	mediaType, values, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || values["boundary"] == "" {
		writeLocalProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", false)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maximumAudioBytes+maximumMultipartOverhead)
	multipartReader := multipart.NewReader(request.Body, values["boundary"])
	part, err := multipartReader.NextPart()
	if err != nil || part.FormName() != "audio" || part.FileName() == "" {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	audioMediaType, audioParameters, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	if err != nil || !supportedAudioMediaType(audioMediaType) || !supportedAudioParameters(audioParameters) {
		writeLocalProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", false)
		return
	}
	streamContext, cancelStream := context.WithCancel(request.Context())
	defer cancelStream()
	stream, err := server.speech.Transcribe(streamContext)
	if err != nil {
		writeSpeechProblem(writer, err)
		return
	}
	if err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Metadata{Metadata: &sttv1.TranscribeMetadata{
		MediaType: mime.FormatMediaType(audioMediaType, audioParameters), SizeBytes: uint64(declaredSize),
	}}}); err != nil {
		writeSpeechSendProblem(writer, stream, err)
		return
	}
	received, digest, err := forwardAudioPart(part, declaredSize, func(chunk []byte) error {
		return stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Chunk{Chunk: chunk}})
	})
	if errors.Is(err, errAudioSizeMismatch) {
		writeLocalProblem(writer, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
		return
	}
	if errors.Is(err, errAudioBodyRead) {
		writeLocalProblem(writer, http.StatusBadRequest, "REQUEST_BODY_READ_FAILED", false)
		return
	}
	if err != nil {
		writeSpeechSendProblem(writer, stream, err)
		return
	}
	if trailing, trailingErr := multipartReader.NextPart(); trailingErr != io.EOF || trailing != nil {
		writeLocalProblem(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_Commit{Commit: &sttv1.TranscribeCommit{
		SizeBytes: uint64(received), Sha256: digest,
	}}}); err != nil {
		writeSpeechSendProblem(writer, stream, err)
		return
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		writeSpeechProblem(writer, err)
		return
	}
	receipt := response.GetReceipt()
	if receipt == nil || response.GetText() == "" || receipt.GetRequestId() == "" || receipt.GetCorrelationId() == "" ||
		receipt.GetAuthoritySourceRevision() == 0 || receipt.GetConfigRevision() == 0 || receipt.GetModel() == "" ||
		!validSTTReceiptLanguage(receipt.GetLanguage()) || receipt.GetCompletedStage() != sttv1.TranscriptionStage_TRANSCRIPTION_STAGE_PROVIDER_COMPLETED {
		writeLocalProblem(writer, http.StatusBadGateway, "UPSTREAM_RESPONSE_INVALID", false)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"text": response.GetText(),
		"receipt": map[string]any{
			"requestId": receipt.GetRequestId(), "correlationId": receipt.GetCorrelationId(),
			"authoritySourceRevision": receipt.GetAuthoritySourceRevision(), "configRevision": receipt.GetConfigRevision(),
			"model": receipt.GetModel(), "language": receipt.GetLanguage(), "completedStage": "PROVIDER_COMPLETED",
		},
	})
}

// validSTTReceiptLanguage permits an absent singular language for auto-detection/model-specific
// language lists or exactly two lowercase ASCII letters. It does not infer or normalize a detected
// language.
// validSTTReceiptLanguage допускает отсутствие единственного языка для auto-detect и
// model-specific списков либо ровно две ASCII буквы нижнего регистра. Вывод и нормализация
// распознанного языка не выполняются.
func validSTTReceiptLanguage(value string) bool {
	return value == "" || len(value) == 2 && value[0] >= 'a' && value[0] <= 'z' && value[1] >= 'a' && value[1] <= 'z'
}

// writeSpeechSendProblem resolves a Send EOF through CloseAndRecv to recover the server status; an
// unexpected successful close becomes invalid upstream response. Other failures use the safe
// speech error mapper without retrying audio.
// writeSpeechSendProblem разрешает Send EOF через CloseAndRecv для получения статуса сервера;
// неожиданно успешное закрытие становится неверным upstream ответом. Прочие ошибки проходят
// безопасный mapper без повторной отправки аудио.
func writeSpeechSendProblem(writer http.ResponseWriter, stream sttv1.SpeechToTextService_TranscribeClient, err error) {
	// Send возвращает EOF при раннем отказе сервера; точный статус приходит в Recv.
	if errors.Is(err, io.EOF) {
		_, err = stream.CloseAndRecv()
		if err == nil {
			writeLocalProblem(writer, http.StatusBadGateway, "UPSTREAM_RESPONSE_INVALID", false)
			return
		}
	}
	writeSpeechProblem(writer, err)
}

// supportedAudioMediaType accepts only the explicit audio/container MIME aliases supported by the
// transcription boundary, rejecting all other media types without guessing from filenames.
// supportedAudioMediaType допускает только явные audio/container MIME aliases границы
// транскрипции, отклоняя прочие типы без определения по имени файла.
func supportedAudioMediaType(value string) bool {
	switch value {
	case "audio/mpeg", "audio/mp3", "audio/mpga", "audio/wav", "audio/x-wav", "audio/wave", "audio/flac", "audio/x-flac",
		"audio/webm", "video/webm", "audio/ogg", "application/ogg", "audio/mp4", "audio/m4a", "audio/x-m4a", "video/mp4":
		return true
	default:
		return false
	}
}

// supportedAudioParameters allows no MIME parameters or only codecs with opus, vorbis or
// mp4a.40.2. Unknown parameter keys or codec values are rejected rather than forwarded implicitly.
// supportedAudioParameters допускает отсутствие MIME параметров либо только codecs со значением
// opus, vorbis или mp4a.40.2. Неизвестные ключи и codecs отклоняются вместо неявной передачи.
func supportedAudioParameters(parameters map[string]string) bool {
	for key, value := range parameters {
		if key != "codecs" || value != "opus" && value != "vorbis" && value != "mp4a.40.2" {
			return false
		}
	}
	return true
}

// forwardAudioPart reads at most declaredSize plus one byte in bounded chunks, rejects
// under/overrun and sends independent byte copies while accumulating SHA-256. Failures retain the
// observed count but return no digest; callers own cancellation, stream commit and HTTP error
// mapping.
// forwardAudioPart читает не более declaredSize плюс один байт ограниченными chunks, отклоняет
// недобор/перебор и отправляет независимые копии, накапливая SHA-256. При ошибке сохраняется
// наблюдённый count без digest; caller владеет отменой, stream commit и HTTP отображением.
func forwardAudioPart(reader io.Reader, declaredSize int64, send func([]byte) error) (int64, string, error) {
	if reader == nil || send == nil || declaredSize < 1 || declaredSize > maximumAudioBytes {
		return 0, "", errAudioSizeMismatch
	}
	digest := sha256.New()
	buffer := make([]byte, maximumAudioChunkBytes)
	limited := io.LimitReader(reader, declaredSize+1)
	var received int64
	for {
		count, readErr := limited.Read(buffer)
		if count > 0 {
			received += int64(count)
			if received > declaredSize {
				return received, "", errAudioSizeMismatch
			}
			_, _ = digest.Write(buffer[:count])
			if err := send(append([]byte(nil), buffer[:count]...)); err != nil {
				return received, "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return received, "", errAudioBodyRead
		}
	}
	if received != declaredSize {
		return received, "", errAudioSizeMismatch
	}
	return received, hex.EncodeToString(digest.Sum(nil)), nil
}
