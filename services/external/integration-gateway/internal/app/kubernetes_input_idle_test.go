package app

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
)

// kubernetesIdleOwner supplies explicit NONE polls except the one protected staged input in the progress variant.
// kubernetesIdleOwner выдаёт explicit NONE polls, кроме единственного protected staged input в progress variant.
type kubernetesIdleOwner struct {
	*kubernetesExecutionInputOwner
	polls int
}

// ClaimManagedKubernetesInputFrame keeps empty polls metadata-only and never repeats staged input bytes.
// ClaimManagedKubernetesInputFrame сохраняет metadata-only empty polls и не повторяет staged input bytes.
func (owner *kubernetesIdleOwner) ClaimManagedKubernetesInputFrame(ctx context.Context, request *cp.ClaimManagedKubernetesInputFrameRequest, options ...grpc.CallOption) (*cp.ClaimManagedKubernetesInputFrameResponse, error) {
	owner.polls++
	if owner.mode == "progress" && owner.polls == 1 {
		return owner.kubernetesExecutionInputOwner.ClaimManagedKubernetesInputFrame(ctx, request, options...)
	}
	return &cp.ClaimManagedKubernetesInputFrameResponse{Disposition: cp.ManagedKubernetesInputFrameDisposition_MANAGED_KUBERNETES_INPUT_FRAME_DISPOSITION_NONE}, nil
}

// kubernetesIdleBroker records fake-clock intervals across actual runner exchanges without sockets or wall-clock sleeps.
// kubernetesIdleBroker фиксирует fake-clock intervals между actual runner exchanges без sockets либо wall-clock sleeps.
type kubernetesIdleBroker struct {
	*kubernetesExecutionInputBroker
	first    time.Time
	interval time.Duration
	cancel   context.CancelFunc
}

// ExchangeManagedKubernetesStream exposes a zero-progress active poll followed by terminal metadata; the progress variant acknowledges actual protected input instead.
// ExchangeManagedKubernetesStream выдаёт zero-progress active poll и затем terminal metadata; progress variant вместо этого подтверждает actual protected input.
func (broker *kubernetesIdleBroker) ExchangeManagedKubernetesStream(_ context.Context, request *sb.ExchangeManagedKubernetesStreamRequest, _ ...grpc.CallOption) (*sb.ExchangeManagedKubernetesStreamResponse, error) {
	if broker.inputOwner.mode == "output" {
		if broker.exchanges == 0 {
			broker.first = time.Now()
		} else {
			broker.interval = time.Since(broker.first)
		}
		return broker.kubernetesExecutionBroker.ExchangeManagedKubernetesStream(context.Background(), request)
	}
	broker.exchanges++
	acknowledged := uint64(0)
	if broker.inputOwner.mode == "progress" {
		acknowledged = 1
	}
	if broker.exchanges == 1 {
		broker.first = time.Now()
		if broker.inputOwner.mode == "progress" && (request.Input == nil || request.Input.Sequence != 1) {
			broker.owner.t.Fatal("progress lost actual staged input")
		}
		if broker.inputOwner.mode == "cancel" {
			time.AfterFunc(50*time.Millisecond, broker.cancel)
		}
		return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_ACTIVE, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_RUNNING, AcknowledgedInputSequence: acknowledged}, nil
	}
	if broker.exchanges != 2 || request.Input != nil {
		broker.owner.t.Fatal("empty polling replayed native input")
	}
	broker.interval = time.Since(broker.first)
	return &sb.ExchangeManagedKubernetesStreamResponse{State: sb.ManagedKubernetesStreamState_MANAGED_KUBERNETES_STREAM_STATE_COMPLETED, RemoteOutcome: sb.ManagedKubernetesRemoteOutcome_MANAGED_KUBERNETES_REMOTE_OUTCOME_SUCCEEDED, AcknowledgedInputSequence: acknowledged, ReasonCode: "CLEAN_EOF"}, nil
}

// TestKubernetesExecutionIdlePollingIsBounded uses standard-library fake time to prove idle backoff, immediate input progress and joined cancellation.
// TestKubernetesExecutionIdlePollingIsBounded использует standard-library fake time для доказательства idle backoff, немедленного input progress и joined cancellation.
func TestKubernetesExecutionIdlePollingIsBounded(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: 10 * time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"idle", "readonly", "progress", "output", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				operation := "kubernetes.exec"
				if mode == "readonly" {
					operation = "kubernetes.logs"
				}
				input := kubernetesExecutionInput(t, definitions[integrationpackage.KubernetesDefinitionKey], operation)
				base := &kubernetesExecutionOwner{kubernetesCompletionOwner: kubernetesCompletionFixture(t)}
				base.binding = input.binding
				digest, err := managedkubernetes.BindingDigest(managedkubernetes.Binding{WorkloadInstance: input.binding.WorkloadInstance, InvocationRef: input.binding.InvocationRef, LeaseRef: input.binding.LeaseRef, Fence: input.binding.Fence, Generation: input.binding.Generation, InputDigest: input.binding.InputDigest})
				if err != nil {
					t.Fatal(err)
				}
				for _, receipt := range []*cp.ManagedKubernetesInvocationReceipt{base.initial, base.after, base.response} {
					receipt.InputDigest = input.binding.InputDigest
					receipt.BindingDigest = digest
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				base.stop = cancel
				inputOwner := &kubernetesExecutionInputOwner{kubernetesExecutionOwner: base, mode: mode}
				owner := &kubernetesIdleOwner{kubernetesExecutionInputOwner: inputOwner}
				broker := &kubernetesIdleBroker{kubernetesExecutionInputBroker: &kubernetesExecutionInputBroker{kubernetesExecutionBroker: &kubernetesExecutionBroker{owner: base}, inputOwner: inputOwner}, cancel: cancel}
				execution, err := newKubernetesExecution(adapter, owner, broker, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				if err := execution.run(ctx, input); err != nil {
					t.Fatal(err)
				}
				if base.cancels != 1 || broker.cancels != 1 {
					t.Fatal("idle cancellation omitted positive cleanup join")
				}
				if mode == "cancel" {
					if broker.exchanges != 1 || owner.polls != 1 {
						t.Fatal("cancelled idle wait polled again")
					}
					return
				}
				expected := 100 * time.Millisecond
				if mode == "progress" || mode == "output" {
					expected = 0
				}
				if broker.exchanges != 2 || broker.interval != expected {
					t.Fatalf("incorrect virtual idle interval: got %s expected %s", broker.interval, expected)
				}
				if mode == "readonly" && owner.polls != 0 {
					t.Fatal("read-only stream polled input")
				}
			})
		})
	}
}
