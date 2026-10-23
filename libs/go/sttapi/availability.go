// Package sttapi contains handwritten client operations above the generated
// STT API. It does not derive authority from request payloads or expose
// provider credentials in the public model catalog.
// RU: Пакет sttapi содержит написанные вручную клиентские операции поверх
// сгенерированного STT API. Он не выводит полномочия из payload запроса и не
// раскрывает реквизиты провайдера в публичном каталоге моделей.
package sttapi

import (
	"context"
	"errors"
	"io"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
)

// CheckAvailability probes the protected Transcribe stream rather than an
// unauthenticated side endpoint. The caller must supply fresh user authority;
// the empty availability payload cannot replace that transport proof. It
// rejects a mixed transcription/receipt response before returning readiness.
// RU: CheckAvailability проверяет защищённый поток Transcribe вместо отдельного
// неавторизованного endpoint. Вызывающий код передаёт свежие полномочия
// пользователя; пустой payload их не заменяет. Смешанный ответ с текстом или
// квитанцией отклоняется до возврата готовности.
func CheckAvailability(ctx context.Context, client sttv1.SpeechToTextServiceClient) (*sttv1.CheckProtectedPathResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.Transcribe(ctx)
	if err != nil {
		return nil, err
	}
	err = stream.Send(&sttv1.TranscribeRequest{Body: &sttv1.TranscribeRequest_AvailabilityCheck{AvailabilityCheck: &sttv1.CheckProtectedPathRequest{}}})
	if err != nil && err != io.EOF {
		_ = stream.CloseSend()
		return nil, err
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	if response.GetAvailability() == nil || response.GetText() != "" || response.GetReceipt() != nil {
		return nil, errors.New("STT availability response is invalid")
	}
	return response.GetAvailability(), nil
}
