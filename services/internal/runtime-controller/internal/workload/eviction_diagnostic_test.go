package workload

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// TestEvictedObservationRequiresCurrentExecutionMetadata rejects same-name foreign
// Pods, missing UID and mismatched immutable execution pins before projecting
// either resource reason. It compares one observed UID, not historical ownership.
// TestEvictedObservationRequiresCurrentExecutionMetadata отклоняет одноимённый
// чужой Pod, пустой UID и чужие execution pins до выдачи диагноза ресурса.
// Проверяется UID одного наблюдения, а не историческое владение.
func TestEvictedObservationRequiresCurrentExecutionMetadata(t *testing.T) {
	for _, reason := range []string{"Evicted", "OOMKilled"} {
		for _, changed := range []string{"none", "uid", "name", "namespace", managedLabel, modeLabel,
			revisionAnnotation, leaseAnnotation, executionBindingAnnotation, controllerAnnotation,
			turnHashAnnotation, attemptAnnotation, organizationHashAnnotation, projectHashAnnotation,
			sessionHashAnnotation, resourcesAnnotation, networkAnnotation} {
			t.Run(reason+"/"+changed, func(t *testing.T) {
				client := fake.NewSimpleClientset()
				manager := newTestManager(t, client)
				input, binding, err := manager.BuildTurnInput(testExecution(false))
				if err != nil {
					t.Fatal(err)
				}
				credentials := testCredentialProjection(input)
				pod := manager.runtimePod(input, binding, &credentials, ticketName(input.LeaseRef), turnPodName(input.LeaseRef), "turn")
				pod.UID = "current-observed-uid"
				pod.Status.Phase = corev1.PodFailed
				if reason == "Evicted" {
					pod.Status.Reason = "Evicted"
				} else {
					pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "provider-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}}
				}
				pod.Status.Message = "PRIVATE_MESSAGE storage memory unknown"
				switch changed {
				case "none":
				case "uid":
					pod.UID = ""
				case "name":
					pod.Name = "foreign-name"
				case "namespace":
					pod.Namespace = "foreign-namespace"
				case managedLabel, modeLabel:
					pod.Labels[changed] = "foreign"
				default:
					pod.Annotations[changed] = "foreign"
				}
				// Return the object from the exact requested GET even for wrong name/namespace,
				// proving the defensive metadata check rather than fake-client NotFound.
				// Возвращаем объект на точный GET даже с чужим именем/namespace,
				// доказывая защитную проверку вместо NotFound fake-клиента.
				client.PrependReactor("get", "pods", func(k8stesting.Action) (bool, k8sruntime.Object, error) { return true, pod, nil })
				client.ClearActions()
				observed, err := manager.ObserveTurnPod(t.Context(), input, false)
				if err != nil {
					t.Fatal(err)
				}
				if changed != "none" {
					if observed.State != "CONFLICT" || observed.ObservedPodUID != "" || observed.DiagnosticCode == TurnPodMemoryDiagnostic || observed.DiagnosticCode == TurnPodEvictedDiagnostic {
						t.Fatal("foreign or unbound Pod acquired a resource diagnosis")
					}
					return
				}
				want := TurnPodEvictedDiagnostic
				if reason == "OOMKilled" {
					want = TurnPodMemoryDiagnostic
				}
				if observed.State != "FAILED" || observed.DiagnosticCode != want || observed.ObservedPodUID != "current-observed-uid" {
					t.Fatal("exact observation lost its closed reason or UID")
				}
				if len(client.Actions()) != 1 {
					t.Fatal("observation performed more than one read")
				}
			})
		}
	}
}

// TestEvictedDiagnosticDoesNotParseMessagesOrWarmHistory distinguishes typed
// current eviction, OOM precedence and arbitrary text, then proves shared warm
// Pod resource history cannot become this turn's cold resource diagnosis.
// TestEvictedDiagnosticDoesNotParseMessagesOrWarmHistory различает текущий typed
// Evicted, приоритет OOM и произвольный текст, затем доказывает, что история общего
// warm Pod не становится диагнозом ресурсов холодного хода.
func TestEvictedDiagnosticDoesNotParseMessagesOrWarmHistory(t *testing.T) {
	pod := &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Message: "Evicted PRIVATE storage"}}
	if got := terminalContainerDiagnostic(pod); got != "POD_FAILED" {
		t.Fatal("message invented eviction")
	}
	pod.Status.Reason = "Evicted"
	if got := terminalContainerDiagnostic(pod); got != TurnPodEvictedDiagnostic {
		t.Fatal("typed eviction lost")
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "provider-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}}}
	if got := terminalContainerDiagnostic(pod); got != TurnPodMemoryDiagnostic {
		t.Fatal("current known OOM precedence changed")
	}
	client := fake.NewSimpleClientset()
	manager := newTestManager(t, client)
	input, _, err := manager.BuildTurnInput(testExecution(true))
	if err != nil {
		t.Fatal(err)
	}
	compatibility, err := runtimecontract.WarmCompatibilityDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	pod.ObjectMeta = metav1.ObjectMeta{Name: "system-assistant-warm", Namespace: "kodex-runtime", UID: "warm-observed-uid", Annotations: map[string]string{warmCompatibilityAnnotation: compatibility}}
	if _, err := client.CoreV1().Pods("kodex-runtime").Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	observation, err := manager.ObserveTurnPod(t.Context(), input, true)
	if err != nil || observation.State != "FAILED" || observation.DiagnosticCode != "POD_FAILED" {
		t.Fatal("shared warm resource history was attributed to this turn")
	}
}
