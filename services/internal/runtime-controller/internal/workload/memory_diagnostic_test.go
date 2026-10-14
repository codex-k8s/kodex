package workload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestTerminalContainerMemoryDiagnosticUsesCurrentKnownContainerReason(t *testing.T) {
	terminated := func(name, reason string, exit int32) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: exit}}}
	}
	for _, name := range []string{"workspace-prepare", "workspace-init", "role-runtime", "provider-runtime", "provider-credential-relay"} {
		t.Run(name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{terminated("role-runtime", "Error", 1), terminated(name, "OOMKilled", 137)}}}
			if name == "workspace-prepare" || name == "workspace-init" {
				pod.Status.InitContainerStatuses = pod.Status.ContainerStatuses[1:]
				pod.Status.ContainerStatuses = pod.Status.ContainerStatuses[:1]
			}
			if got := terminalContainerDiagnostic(pod); got != TurnPodMemoryDiagnostic {
				t.Fatalf("diagnostic = %q", got)
			}
		})
	}
	for _, scenario := range []struct {
		name   string
		status corev1.ContainerStatus
		want   string
	}{
		{"exit137 alone", terminated("role-runtime", "Error", 137), "ROLE_RUNTIME_EXITED_NONZERO"},
		{"unknown reason", terminated("role-runtime", "PRIVATE_SENTINEL", 137), "ROLE_RUNTIME_EXITED_NONZERO"},
		{"unknown container", terminated("PRIVATE_CONTAINER", "OOMKilled", 137), "POD_FAILED"},
		{"previous OOM", corev1.ContainerStatus{Name: "role-runtime", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137}}}, "ROLE_RUNTIME_EXITED_NONZERO"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			pod := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{scenario.status}}}
			if got := terminalContainerDiagnostic(pod); got != scenario.want {
				t.Fatalf("diagnostic = %q, want %q", got, scenario.want)
			}
		})
	}
}
