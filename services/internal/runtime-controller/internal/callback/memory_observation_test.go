package callback

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

func TestCommittedMemoryObservationIsBoundedReadOnlyAndClosed(t *testing.T) {
	for _, scenario := range []string{"current OOM", "exit137", "previous OOM", "revision mismatch", "missing Pod", "read failure", "success", "owner failure", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			manager, kube, input, _, _ := providerCredentialRefreshRouteFixture(t)
			name := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
			pod, err := kube.CoreV1().Pods("kodex-runtime").Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			pod.Status.Phase = corev1.PodFailed
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "provider-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, Message: "PRIVATE_POD_MESSAGE"}}}}
			if scenario == "exit137" || scenario == "previous OOM" {
				pod.Status.ContainerStatuses[0].State.Terminated.Reason = "Error"
				pod.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}
			}
			if scenario == "revision mismatch" {
				pod.Annotations["runtime.kodex.dev/revision-digest"] = strings.Repeat("f", 64)
			}
			if _, err := kube.CoreV1().Pods("kodex-runtime").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "missing Pod" {
				if err := kube.CoreV1().Pods("kodex-runtime").Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			reads := 0
			kube.PrependReactor("get", "pods", func(k8stesting.Action) (bool, k8sruntime.Object, error) {
				reads++
				if scenario == "read failure" {
					return true, nil, errors.New("PRIVATE_READ_FAILURE")
				}
				return false, nil, nil
			})
			var logs bytes.Buffer
			server := &Server{manager: manager, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			payload := runtimecontract.RunnerCompletionRequest{SafeErrorCode: "PROVIDER_UNAVAILABLE", ResultSummary: "PRIVATE_PAYLOAD"}
			var ownerError error
			if scenario == "success" {
				payload.Success = true
			}
			if scenario == "owner failure" {
				ownerError = errors.New("PRIVATE_OWNER_ERROR")
			}
			if scenario == "replay" {
				ownerError = status.Error(codes.AlreadyExists, "PRIVATE_OWNER_ERROR")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			server.logCommittedProviderDiagnostic(ctx, input, payload, ownerError)
			if scenario == "success" || scenario == "owner failure" || scenario == "replay" {
				if reads != 0 || logs.Len() != 0 {
					t.Fatal("non-commit acquired a memory observation")
				}
				return
			}
			if reads != 1 {
				t.Fatalf("reads = %d", reads)
			}
			want := `"pod_memory_observation":"UNAVAILABLE"`
			if scenario == "current OOM" {
				want = `"pod_memory_observation":"OBSERVED"`
			}
			if !strings.Contains(logs.String(), want) {
				t.Fatal("observation guessed a resource failure")
			}
			for _, private := range []string{"PRIVATE_POD_MESSAGE", "PRIVATE_READ_FAILURE", "PRIVATE_PAYLOAD", "PRIVATE_OWNER_ERROR", input.LeaseFence} {
				if private != "" && strings.Contains(logs.String(), private) {
					t.Fatal("memory observation exposed private data")
				}
			}
		})
	}
}
