package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"testing"
)

// TestKubernetesToolReasonNeverDisclosesProviderDiagnostics keeps unknown persisted errors out of Tool read metadata and pins the one supported preflight reason to its exact failed operation.
// TestKubernetesToolReasonNeverDisclosesProviderDiagnostics скрывает unknown persisted errors в Tool metadata и связывает поддержанную preflight причину с exact failed operation.
func TestKubernetesToolReasonNeverDisclosesProviderDiagnostics(t *testing.T) {
	for _, item := range []struct {
		operation string
		state     enum.KubernetesToolState
		persisted string
		want      enum.KubernetesToolSafeReasonCode
	}{
		{"kubernetes.port-forward", "FAILED", managedkubernetes.ReasonPortForwardProfileUnsupported, enum.KubernetesToolReasonPortForwardProfileUnsupported},
		{"kubernetes.exec", "FAILED", managedkubernetes.ReasonPortForwardProfileUnsupported, enum.KubernetesToolReasonNone},
		{"kubernetes.port-forward", "RUNNING", managedkubernetes.ReasonPortForwardProfileUnsupported, enum.KubernetesToolReasonNone},
		{"kubernetes.port-forward", "FAILED", "private provider diagnostic", enum.KubernetesToolReasonNone},
		{"kubernetes.port-forward", "FAILED", "", enum.KubernetesToolReasonNone},
	} {
		if got := kubernetesToolSafeReason(item.operation, item.state, item.persisted); got != item.want {
			t.Fatal("persisted diagnostic escaped closed display projection")
		}
	}
}
