package kubernetestarget

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	strictjson "sigs.k8s.io/json"
)

// WriteFrame binds one previously admitted input to its shorter frame deadline and joins cancellation.
// Cancellation closes the whole non-replayable transport; it never proves remote process termination.
// This adapter does not obtain authority: the service must first consume the exact owner frame credit.
// WriteFrame связывает ранее допущенный input с коротким frame deadline и ожидает завершения отмены.
// Отмена закрывает весь transport без replay и не доказывает завершение remote process.
// Adapter не получает полномочия: service обязан сначала израсходовать exact owner frame credit.
func (stream *nativeExecWebSocket) WriteFrame(ctx context.Context, frame value.KubernetesStreamFrame) error {
	if ctx == nil || ctx.Err() != nil || stream == nil || len(frame.Payload) > managedkubernetes.MaximumFrameBytes {
		return errNativeStreamUnknown
	}
	if stream.operation == "kubernetes.cp.download" {
		return errNativeStreamUnknown
	}
	copyInput := stream.operation == "kubernetes.cp.upload"
	if copyInput {
		if frame.Channel != "FILE_CONTENT" {
			return errNativeStreamUnknown
		}
		frame.Channel = "STDIN"
	}
	deadline, bounded := ctx.Deadline()
	if !bounded || !deadline.After(time.Now()) {
		return errNativeStreamUnknown
	}
	var columns, rows uint16
	switch frame.Channel {
	case "STDIN":
		if len(frame.Payload) == 0 && !frame.EndOfInput || len(frame.Payload) != 0 && frame.EndOfInput && !copyInput {
			return errNativeStreamUnknown
		}
	case "TERMINAL_RESIZE":
		var size struct {
			Columns uint16 `json:"columns"`
			Rows    uint16 `json:"rows"`
		}
		failures, err := strictjson.UnmarshalStrict(frame.Payload, &size, strictjson.DisallowDuplicateFields, strictjson.DisallowUnknownFields)
		if err != nil || len(failures) != 0 || frame.EndOfInput || size.Columns == 0 || size.Columns > 1024 || size.Rows == 0 || size.Rows > 512 {
			return errNativeStreamUnknown
		}
		columns, rows = size.Columns, size.Rows
	default:
		return errNativeStreamUnknown
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(joined)
		_ = stream.Close()
	})
	defer func() {
		if !stop() {
			<-joined
		}
	}()
	if ctx.Err() != nil {
		return errNativeStreamUnknown
	}
	var err error
	if copyInput {
		err = stream.sendFileInput(frame.Payload, frame.EndOfInput)
	} else if frame.Channel == "STDIN" {
		err = stream.SendStdin(frame.Payload, frame.EndOfInput)
	} else {
		err = stream.Resize(columns, rows)
	}
	if err != nil || ctx.Err() != nil {
		return errNativeStreamUnknown
	}
	return nil
}
