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
	maximumAudioBytes        = 25 << 20
	maximumAudioChunkBytes   = 64 << 10
	maximumMultipartOverhead = 128 << 10
)

var (
	errAudioSizeMismatch = errors.New("audio size does not match the declared size")
	errAudioBodyRead     = errors.New("audio request body read failed")
)

type speechToTextClient = sttv1.SpeechToTextServiceClient

func (server *Server) AttachSpeechToText(client speechToTextClient) error {
	if client == nil || server.speech != nil {
		return errors.New("STT attachment is invalid")
	}
	server.speech = client
	return nil
}

func (server *Server) TranscribeSpeech(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.TranscribeSpeechParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	server.transcribeSpeech(writer, request, parameters.XAudioSize)
}

func (server *Server) TranscribeOrganizationSpeech(writer http.ResponseWriter, request *http.Request, parameters generated.TranscribeOrganizationSpeechParams) {
	server.transcribeSpeech(writer, request, parameters.XAudioSize)
}

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
		writeRPCProblem(writer, err)
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
		writeRPCProblem(writer, err)
		return
	}
	receipt := response.GetReceipt()
	if receipt == nil || response.GetText() == "" || receipt.GetRequestId() == "" || receipt.GetCorrelationId() == "" ||
		receipt.GetAuthoritySourceRevision() == 0 || receipt.GetConfigRevision() == 0 || receipt.GetModel() == "" ||
		receipt.GetLanguage() == "" || receipt.GetCompletedStage() != sttv1.TranscriptionStage_TRANSCRIPTION_STAGE_PROVIDER_COMPLETED {
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

func writeSpeechSendProblem(writer http.ResponseWriter, stream sttv1.SpeechToTextService_TranscribeClient, err error) {
	// Send возвращает EOF при раннем отказе сервера; точный статус приходит в Recv.
	if errors.Is(err, io.EOF) {
		_, err = stream.CloseAndRecv()
		if err == nil {
			writeLocalProblem(writer, http.StatusBadGateway, "UPSTREAM_RESPONSE_INVALID", false)
			return
		}
	}
	writeRPCProblem(writer, err)
}

func supportedAudioMediaType(value string) bool {
	switch value {
	case "audio/mpeg", "audio/mp3", "audio/mpga", "audio/wav", "audio/x-wav", "audio/wave", "audio/flac", "audio/x-flac",
		"audio/webm", "video/webm", "audio/ogg", "application/ogg", "audio/mp4", "audio/m4a", "audio/x-m4a", "video/mp4":
		return true
	default:
		return false
	}
}

func supportedAudioParameters(parameters map[string]string) bool {
	for key, value := range parameters {
		if key != "codecs" || value != "opus" && value != "vorbis" && value != "mp4a.40.2" {
			return false
		}
	}
	return true
}

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
