package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/protobuf/proto"
)

// kubernetesGatePollInterval bounds owner-backed approval polling without imposing an execution step ceiling.
// kubernetesGatePollInterval ограничивает частоту owner-backed approval polling без предела шагов выполнения.
const kubernetesGatePollInterval = 500 * time.Millisecond

// kubernetesStreamIdlePollInterval caps empty owner/broker polling without delaying useful output acknowledgements.
// kubernetesStreamIdlePollInterval ограничивает пустой owner/broker polling без задержки acknowledgement полезного output.
const kubernetesStreamIdlePollInterval = 100 * time.Millisecond

// kubernetesCapturePollInterval bounds metadata polling while the independent artifact job
// publishes a sealed result. It grants no new effect, child advance or native stream admission.
// kubernetesCapturePollInterval ограничивает metadata polling, пока independent artifact job
// публикует sealed result. Новый effect, child advance либо native stream admission не выдаёт.
const kubernetesCapturePollInterval = 500 * time.Millisecond

// kubernetesExecution composes actual adapters and the stream consumer under one application-owned activity.
// Protected input and durable output consumers are mandatory dependencies, not caller-selectable plugins.
// kubernetesExecution собирает actual adapters и stream consumer внутри одной app-owned activity.
// Protected input и durable output consumers обязательны и не являются выбираемыми caller plugins.
type kubernetesExecution struct {
	adapter       *integration.Adapter
	owner         cp.ManagedKubernetesOwnerServiceClient
	broker        sb.ManagedKubernetesServiceClient
	input         integration.KubernetesStreamInputSource
	output        integration.KubernetesStreamOutputSink
	cleanupBudget time.Duration
}

// newKubernetesExecution refuses an incomplete composition before claim capacity can be advertised.
// The output sink uses the same typed owner client; input source uses the same protected owner client; construction does not qualify remote storage or its owner producer.
// newKubernetesExecution отклоняет неполную композицию до объявления claim capacity.
// Output sink использует тот же typed owner client; INPUT source использует тот же protected owner client; создание не квалифицирует удалённое storage либо owner producer.
func newKubernetesExecution(adapter *integration.Adapter, owner cp.ManagedKubernetesOwnerServiceClient, broker sb.ManagedKubernetesServiceClient, cleanupBudget time.Duration) (*kubernetesExecution, error) {
	if adapter == nil || owner == nil || broker == nil || cleanupBudget <= 0 || cleanupBudget > time.Minute {
		return nil, errKubernetesActivity
	}
	return &kubernetesExecution{adapter: adapter, owner: owner, broker: broker, input: integration.NewKubernetesInputSource(owner), output: integration.NewKubernetesOutputSink(owner), cleanupBudget: cleanupBudget}, nil
}

// run keeps renewal and its independent watchdog joined with the complete execution/cleanup path.
// run удерживает renewal и независимый watchdog до join полного execution/cleanup path.
func (execution *kubernetesExecution) run(ctx context.Context, input kubernetesActivityInput) error {
	started := false
	err := runKubernetesRenewed(ctx, input, execution.owner, func(work context.Context, owned kubernetesActivityInput) error {
		started = true
		return execution.execute(work, owned)
	})
	// The renewable wrapper positively joins before returning, so this read cannot race its execution worker.
	// Renewable wrapper возвращается после positive join, поэтому это чтение не конкурирует с execution worker.
	if !started {
		cleanupErr := cleanupKubernetesActivity(ctx, input.binding, execution.owner, nil, execution.cleanupBudget)
		if cleanupErr == nil && kubernetesResolvedLeaseLoss(err) && ctx != nil && (ctx.Err() != nil || !input.leaseUntil.After(time.Now())) {
			return nil
		}
		return errors.Join(err, cleanupErr)
	}
	return err
}

// execute submits only the original claim, waits only on explicit approval metadata, and never replays uncertain effects.
// A terminal owner receipt and, for streams, positive broker join are both required before a successful local return.
// execute передаёт лишь original claim, ждёт только explicit approval metadata и не повторяет uncertain effects.
// Для успешного локального возврата нужны terminal owner receipt и positive broker join для streams.
func (execution *kubernetesExecution) execute(ctx context.Context, input kubernetesActivityInput) (result error) {
	var stream *integration.KubernetesStreamConsumer
	cancelledExchange := false
	defer func() {
		cleanupErr := cleanupKubernetesActivity(ctx, input.binding, execution.owner, stream, execution.cleanupBudget)
		// Local cancellation or a canceled known-stream exchange may normalize only after confirmed owner closure and broker join; protocol failures remain failures.
		// Local cancellation либо отменённый exchange известного stream нормализуется лишь после owner closure и broker join; protocol failures остаются ошибками.
		if cleanupErr == nil && (result == context.Canceled || result == context.DeadlineExceeded || stream != nil && cancelledExchange) {
			result = nil
		}
		result = errors.Join(result, cleanupErr)
	}()
	var response *sb.ExecuteManagedKubernetesOperationResponse
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var err error
		response, err = execution.adapter.ExecuteKubernetes(ctx, input.request, input.binding, execution.broker)
		if err == nil {
			break
		}
		var preflight *integration.KubernetesPreflightRejection
		if errors.As(err, &preflight) {
			return rejectKubernetesPortForwardPreflight(ctx, input.binding, execution.owner)
		}
		var pending *integration.KubernetesPending
		if !errors.As(err, &pending) {
			return err
		}
		if pending.GateRef == "" {
			// Reconciliation may aggregate accepted history, but cannot dispatch the effect again.
			// Reconciliation может собрать accepted history, но не запускает effect повторно.
			return execution.finish(ctx, input.binding)
		}
		observed, readErr := integration.ReadKubernetesInvocation(ctx, input.binding, execution.owner)
		if readErr != nil {
			return readErr
		}
		if observed.PlanRef != pending.PlanRef {
			return errKubernetesActivity
		}
		if observed.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
			return nil
		}
		timer := time.NewTimer(kubernetesGatePollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if completed := response.GetCompleted(); completed != nil {
		defer clear(completed.ResultJson)
		return execution.finish(ctx, input.binding)
	}
	descriptor := response.GetStream()
	var err error
	stream, err = integration.NewKubernetesStreamConsumer(input.binding, descriptor, input.request.Operation, execution.broker, execution.output, func(cleanup context.Context, binding *sb.ManagedKubernetesInvocationBinding, ref string) error {
		if !proto.Equal(binding, input.binding) || descriptor == nil || ref != descriptor.StreamRef {
			return errKubernetesActivity
		}
		return closeKubernetesActivity(cleanup, binding, execution.owner)
	})
	if err != nil {
		return err
	}
	var acknowledgedInput, acceptedOutput uint64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var delivery *integration.KubernetesInputDelivery
		if len(descriptor.InputChannels) != 0 {
			var sourceErr error
			delivery, sourceErr = execution.input(ctx, proto.Clone(input.binding).(*sb.ManagedKubernetesInvocationBinding), proto.Clone(descriptor).(*sb.ManagedKubernetesStreamDescriptor))
			if sourceErr != nil {
				if delivery != nil && delivery.Frame != nil {
					clear(delivery.Frame.Payload)
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return sourceErr
			}
		}
		var frame *sb.ManagedKubernetesFrame
		exchangeContext := ctx
		finishExchange := func() {}
		if delivery != nil {
			frame = delivery.Frame
			if frame == nil || !delivery.DeliveryExpiresAt.After(time.Now()) || delivery.DeliveryExpiresAt.After(descriptor.ExpiresAt.AsTime()) {
				if frame != nil {
					clear(frame.Payload)
				}
				return errKubernetesActivity
			}
			exchangeContext, finishExchange = context.WithDeadline(ctx, delivery.DeliveryExpiresAt)
		}
		progress, exchangeErr := stream.Exchange(exchangeContext, frame)
		finishExchange()
		if frame != nil {
			clear(frame.Payload)
		}

		if exchangeErr != nil {
			// Only the actual exchange transport cancellation may be normalized; joined errors and malformed responses remain visible.
			// Нормализуется лишь actual exchange transport cancellation; joined errors и malformed responses остаются видимыми.
			if ctx.Err() != nil {
				switch failure := exchangeErr.(type) {
				case *integration.SafeError:
					cancelledExchange = failure.Code == "INTEGRATION_UNAVAILABLE"
				case *integration.UnknownOutcomeError:
					cancelledExchange = integration.UnknownOutcomeStage(failure) == "transport"
				}
			}
			return exchangeErr
		}
		if progress.State != sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE {
			return execution.finish(ctx, input.binding)
		}
		if frame == nil && progress.AcknowledgedInput == acknowledgedInput && progress.AcceptedOutput == acceptedOutput {
			timer := time.NewTimer(kubernetesStreamIdlePollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		acknowledgedInput, acceptedOutput = progress.AcknowledgedInput, progress.AcceptedOutput
	}
}

// finish accepts only owner-proven terminal state. CAPTURE_PENDING permits bounded metadata-only
// Get/Complete polling under the same renewal lifecycle; no Execute or next child is called.
// A still-running compound plan independently requires its owner phase consumer.
// finish принимает лишь owner-proven terminal state. CAPTURE_PENDING разрешает bounded metadata-only
// Get/Complete polling внутри того же renewal lifecycle; Execute либо next child не вызывается.
// Незавершённому compound plan отдельно нужен owner phase consumer.
func (execution *kubernetesExecution) finish(ctx context.Context, binding *sb.ManagedKubernetesInvocationBinding) error {
	var original *cp.ManagedKubernetesInvocationReceipt
	for {
		receipt, err := settleKubernetesActivityWithPlan(ctx, binding, execution.owner, original)
		if err != nil {
			return err
		}
		if original != nil && (receipt.PlanRef != original.PlanRef || receipt.PlanRevision != original.PlanRevision || receipt.PlanDigest != original.PlanDigest || receipt.SnapshotDigest != original.SnapshotDigest) {
			return errKubernetesActivity
		}
		if receipt.State != cp.ManagedKubernetesInvocationState_MANAGED_KUBERNETES_INVOCATION_STATE_RUNNING {
			return nil
		}
		if receipt.CompletionPendingReason != cp.ManagedKubernetesCompletionPendingReason_MANAGED_KUBERNETES_COMPLETION_PENDING_REASON_CAPTURE_PENDING {
			return errKubernetesActivity
		}
		if original == nil {
			original = proto.Clone(receipt).(*cp.ManagedKubernetesInvocationReceipt)
		}
		timer := time.NewTimer(kubernetesCapturePollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
