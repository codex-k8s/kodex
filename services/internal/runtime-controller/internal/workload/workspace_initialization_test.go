package workload

import (
	"context"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestInitializationObservationRejectsForeignAndMalformedProvenance uses actual manager reads.
// It never reads logs, credentials or historical termination states to manufacture a cause.
// TestInitializationObservationRejectsForeignAndMalformedProvenance проверяет реальные manager reads,
// без чтения logs, credentials либо historical termination для выдумывания причины.
func TestInitializationObservationRejectsForeignAndMalformedProvenance(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "bad-json", "bad-tuple", "unknown-stage", "uid", "foreign-label", "foreign-turn", "previous-only", "fallback-policy", "duplicate-status"} {
		t.Run(scenario, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			manager := newTestManager(t, client)
			input, binding, err := manager.BuildTurnInput(testExecution(false))
			if err != nil {
				t.Fatal("build native-shape input")
			}
			record := runtimecontract.NewInitializationObservation(input, "ARTIFACT_TRANSFER", "AUTHORITY_REJECTED")
			raw, err := runtimecontract.EncodeInitializationFailure(record)
			if err != nil {
				t.Fatal("encode exact init observation")
			}
			pod := manager.runtimePod(input, binding, nil, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
			pod.UID = "10000000-0000-4000-8000-000000001941"
			pod.Status = corev1.PodStatus{Phase: corev1.PodFailed, InitContainerStatuses: []corev1.ContainerStatus{{Name: "workspace-init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Message: string(raw)}}}}}
			conflict := false
			switch scenario {
			case "missing":
				pod.Status.InitContainerStatuses[0].State.Terminated.Message = ""
			case "bad-json":
				pod.Status.InitContainerStatuses[0].State.Terminated.Message = `{"private":"PRIVATE_SENTINEL"}`
			case "bad-tuple":
				record.Attempt++
				altered, _ := runtimecontract.EncodeInitializationFailure(record)
				pod.Status.InitContainerStatuses[0].State.Terminated.Message = string(altered)
			case "unknown-stage":
				pod.Status.InitContainerStatuses[0].State.Terminated.Message = strings.Replace(string(raw), "ARTIFACT_TRANSFER", "PRIVATE_STAGE", 1)
			case "uid":
				pod.UID = ""
				conflict = true
			case "foreign-label":
				pod.Labels[executionHashLabel] = "foreign"
				conflict = true
			case "foreign-turn":
				pod.Annotations[turnHashAnnotation] = "foreign"
				conflict = true
			case "previous-only":
				pod.Status.InitContainerStatuses[0].LastTerminationState = pod.Status.InitContainerStatuses[0].State
				pod.Status.InitContainerStatuses[0].State = corev1.ContainerState{}
				conflict = false
			case "fallback-policy":
				pod.Spec.InitContainers[1].TerminationMessagePolicy = corev1.TerminationMessageFallbackToLogsOnError
				conflict = true
			case "duplicate-status":
				pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses, pod.Status.InitContainerStatuses[0])
				conflict = true
			}
			if _, err := client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
				t.Fatal("create fake Pod")
			}
			observed, err := manager.ObserveTurnPod(context.Background(), input, false)
			if err != nil {
				t.Fatal("observe exact init Pod")
			}
			if conflict {
				if observed.State != "CONFLICT" || observed.Initialization != nil {
					t.Fatal("foreign init provenance accepted")
				}
				return
			}
			if scenario == "previous-only" {
				if observed.Initialization != nil {
					t.Fatal("previous termination attributed to current init")
				}
				return
			}
			if observed.Initialization == nil || !observed.Initialization.Valid() {
				t.Fatal("closed init observation missing")
			}
			if scenario == "valid" {
				if !observed.Initialization.RecordValidated || observed.Initialization.Stage != "ARTIFACT_TRANSFER" || observed.Initialization.Reason != "AUTHORITY_REJECTED" {
					t.Fatal("valid cause lost")
				}
			} else if observed.Initialization.RecordValidated || observed.Initialization.Stage != "UNKNOWN" || observed.Initialization.Reason != "UNKNOWN" {
				t.Fatal("untrusted record acquired a specific cause")
			}
		})
	}
}
