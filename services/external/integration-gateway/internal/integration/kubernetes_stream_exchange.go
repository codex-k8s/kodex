package integration

import (
	"bytes"
	"context"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"

	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/protobuf/proto"
)

// Exchange performs one bounded unary exchange and acknowledges only previously accepted output.
// Any attempted transport, malformed reply or rejected sink permanently closes further exchanges;
// the caller reconciles the original owner and explicitly calls Cancel, never replays input.
// Exchange выполняет один bounded unary exchange, подтверждая только ранее принятый output.
// Transport uncertainty, malformed reply либо отказ sink закрывают последующие exchanges;
// caller сверяет original owner и явно вызывает Cancel, не повторяя input.
func (consumer *KubernetesStreamConsumer) Exchange(ctx context.Context, input *sb.ManagedKubernetesFrame) (KubernetesStreamProgress, error) {
	var empty KubernetesStreamProgress
	if consumer == nil || ctx == nil || !consumer.acquire(ctx) {
		return empty, &SafeError{Code: kubernetesOwnerUnavailable}
	}
	defer consumer.release()
	if input != nil {
		if len(input.Payload) > int(consumer.descriptor.MaximumFrameBytes) || len(input.ProtoReflect().GetUnknown()) != 0 || len(input.PayloadDigest) > 64 {
			return empty, &SafeError{Code: kubernetesOwnerRequestRejected}
		}
		input = proto.Clone(input).(*sb.ManagedKubernetesFrame)
		defer clear(input.Payload)
	}
	consumer.control.Lock()
	stopped := consumer.stopped
	consumer.control.Unlock()
	now := time.Now()
	if stopped || consumer.uncertain || consumer.terminal || !consumer.descriptor.ExpiresAt.AsTime().After(now) || !consumer.acceptedUntil.IsZero() && !consumer.acceptedUntil.After(now) {
		return empty, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	expectedInput := consumer.acknowledgedInput
	if input != nil {
		if !consumer.validInput(input) {
			return empty, &SafeError{Code: kubernetesOwnerRequestRejected}
		}
		expectedInput = input.Sequence
	}
	deadline := now.Add(kubernetesStreamCallTimeout)
	if consumer.descriptor.ExpiresAt.AsTime().Before(deadline) {
		deadline = consumer.descriptor.ExpiresAt.AsTime()
	}
	if !consumer.acceptedUntil.IsZero() && consumer.acceptedUntil.Before(deadline) {
		deadline = consumer.acceptedUntil
	}
	work, stop := context.WithDeadline(ctx, consumer.descriptor.ExpiresAt.AsTime())
	defer stop()
	call, cancel := context.WithDeadline(work, deadline)
	consumer.control.Lock()
	if consumer.stopped {
		consumer.control.Unlock()
		cancel()
		return empty, &SafeError{Code: kubernetesOwnerRequestRejected}
	}
	consumer.inflight = stop
	consumer.control.Unlock()
	defer func() { cancel(); consumer.control.Lock(); consumer.inflight = nil; consumer.control.Unlock() }()
	request := &sb.ExchangeManagedKubernetesStreamRequest{Binding: proto.Clone(consumer.binding).(*sb.ManagedKubernetesInvocationBinding), StreamRef: consumer.descriptor.StreamRef, AcknowledgedOutputSequence: consumer.acceptedOutput}
	if input != nil {
		request.Input = proto.Clone(input).(*sb.ManagedKubernetesFrame)
		defer clear(request.Input.Payload)
	}
	response, err := consumer.broker.ExchangeManagedKubernetesStream(call, request)
	if response != nil {
		defer func() {
			for _, frame := range response.Output {
				if frame != nil {
					clear(frame.Payload)
				}
			}
		}()
	}
	if err != nil || call.Err() != nil {
		consumer.uncertain = true
		return empty, consumer.failure("transport", input != nil)
	}
	if !consumer.validExchangeResponse(response, expectedInput, input) {
		consumer.uncertain = true
		return empty, consumer.failure("response_validation", input != nil)
	}
	if input != nil {
		consumer.acknowledgedInput = input.Sequence
		consumer.totalBytes += uint64(len(input.Payload))
		if input.EndOfInput {
			consumer.inputClosed[input.Channel] = true
		}
	}
	// The request's prior output ACK succeeded; its credit cannot authorize the next frame.
	// Прежний output ACK запроса принят; его credit не разрешает следующий frame.
	consumer.acceptedUntil = time.Time{}
	if len(response.Output) != 0 {
		frame := response.Output[0]
		authorizedUntil := response.AuthorizedUntil.AsTime()
		if !authorizedUntil.After(time.Now()) {
			consumer.uncertain = true
			return empty, consumer.failure("response_validation", input != nil)
		}
		delivery, finish := context.WithDeadline(work, authorizedUntil)
		output := KubernetesStreamOutput{InvocationRef: consumer.binding.InvocationRef, StreamRef: consumer.descriptor.StreamRef, BindingDigest: consumer.bindingDigest, Generation: consumer.binding.Generation, Sequence: frame.Sequence, Channel: frame.Channel, PayloadDigest: frame.PayloadDigest, DeliveryOrdinal: frame.DeliveryOrdinal, Payload: bytes.Clone(frame.Payload), AuthorizedUntil: authorizedUntil}
		acceptedErr := consumer.sink(delivery, proto.Clone(consumer.binding).(*sb.ManagedKubernetesInvocationBinding), proto.Clone(consumer.descriptor).(*sb.ManagedKubernetesStreamDescriptor), output)
		expired := delivery.Err() != nil || !authorizedUntil.After(time.Now())
		finish()
		if acceptedErr != nil || expired {
			consumer.uncertain = true
			return empty, consumer.failure("output_delivery", input != nil)
		}
		consumer.acceptedOutput = frame.Sequence
		consumer.acceptedUntil = authorizedUntil
		consumer.totalBytes += uint64(len(frame.Payload))
	}
	if response.State != sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE {
		consumer.terminal = true
	}
	return KubernetesStreamProgress{State: response.State, RemoteOutcome: response.RemoteOutcome, ReasonCode: response.ReasonCode, AcknowledgedInput: consumer.acknowledgedInput, AcceptedOutput: consumer.acceptedOutput}, nil
}

// validInput checks next sequence, immutable channel credit, combined budgets and a closed input channel before dispatch.
// validInput проверяет next sequence, immutable channel credit, общие budgets и закрытый input channel до dispatch.
func (consumer *KubernetesStreamConsumer) validInput(frame *sb.ManagedKubernetesFrame) bool {
	return consumer.totalBytes <= consumer.descriptor.MaximumTotalBytes && validKubernetesStreamPayload(frame, consumer.descriptor.MaximumFrameBytes) && frame.DeliveryOrdinal == 0 && consumer.acknowledgedInput < managedkubernetes.MaximumFrameSequence && frame.Sequence == consumer.acknowledgedInput+1 &&
		slices.Contains(consumer.descriptor.InputChannels, frame.Channel) && !consumer.inputClosed[frame.Channel] &&
		(len(frame.Payload) != 0 || frame.EndOfInput) &&
		uint64(len(frame.Payload)) <= consumer.descriptor.MaximumTotalBytes-consumer.totalBytes
}

// validExchangeResponse admits only the exact next bounded output and a known finite lifecycle/credit tuple.
// It checks the whole response before passing any bytes to the sink; terminal replies cannot carry unread bytes.
// validExchangeResponse допускает только exact next bounded output и известный finite lifecycle/credit tuple.
// Полный response проверяется до sink; terminal reply не может скрывать непринятые bytes.
func (consumer *KubernetesStreamConsumer) validExchangeResponse(response *sb.ExchangeManagedKubernetesStreamResponse, expectedInput uint64, input *sb.ManagedKubernetesFrame) bool {
	if consumer.totalBytes > consumer.descriptor.MaximumTotalBytes || response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || response.AcknowledgedInputSequence != expectedInput || len(response.Output) > 1 ||
		!validKubernetesStreamState(response.State, response.RemoteOutcome, response.ReasonCode) {
		return false
	}
	if len(response.Output) == 0 {
		return response.AuthorizedUntil == nil
	}
	credit := response.AuthorizedUntil
	now := time.Now()
	if response.State != sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE || credit == nil || credit.CheckValid() != nil || len(credit.ProtoReflect().GetUnknown()) != 0 || !credit.AsTime().After(now) || credit.AsTime().After(now.Add(5*time.Second)) || credit.AsTime().After(consumer.descriptor.ExpiresAt.AsTime()) {
		return false
	}
	frame := response.Output[0]
	if !validKubernetesStreamPayload(frame, consumer.descriptor.MaximumFrameBytes) || frame.DeliveryOrdinal == 0 || frame.DeliveryOrdinal > managedkubernetes.MaximumOutputDeliveries || frame.EndOfInput || len(frame.Payload) == 0 || consumer.acceptedOutput >= managedkubernetes.MaximumFrameSequence || frame.Sequence != consumer.acceptedOutput+1 ||
		!slices.Contains(consumer.descriptor.OutputChannels, frame.Channel) {
		return false
	}
	inputBytes := uint64(0)
	if input != nil {
		inputBytes = uint64(len(input.Payload))
	}
	return inputBytes <= consumer.descriptor.MaximumTotalBytes-consumer.totalBytes && uint64(len(frame.Payload)) <= consumer.descriptor.MaximumTotalBytes-consumer.totalBytes-inputBytes
}

// failure preserves mutation/input uncertainty while suppressing raw broker and sink errors.
// failure сохраняет mutation/input uncertainty, скрывая raw broker и sink errors.
func (consumer *KubernetesStreamConsumer) failure(stage string, input bool) error {
	risk := consumer.risk
	if input {
		risk = "WRITE"
	}
	return kubernetesBrokerFailure(risk, stage)
}

// acquire serializes exchange and cleanup without creating a detached goroutine on cancellation.
// acquire сериализует exchange и cleanup без создания detached goroutine при cancellation.
func (consumer *KubernetesStreamConsumer) acquire(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-consumer.lane:
		return true
	}
}

// release returns the local lane only after the RPC and synchronous sink have returned.
// release возвращает local lane только после завершения RPC и synchronous sink.
func (consumer *KubernetesStreamConsumer) release() { consumer.lane <- struct{}{} }
