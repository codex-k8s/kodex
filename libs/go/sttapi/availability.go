// Package sttapi содержит клиентские операции поверх generated STT API.
package sttapi

import (
	"context"
	"errors"
	"io"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
)

// CheckAvailability использует тот же защищённый Transcribe stream. Caller
// обязан выпустить свежий пользовательский authority; payload его не заменяет.
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
