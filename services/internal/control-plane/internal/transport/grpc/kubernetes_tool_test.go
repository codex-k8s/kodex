package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"testing"
)

// TestKubernetesToolProjectionPreservesPendingClosure rejects invented states, mixed locators and false terminal Gate metadata.
// TestKubernetesToolProjectionPreservesPendingClosure отклоняет выдуманные состояния, смешанные ссылки и открытый Gate у terminal вызова.
func TestKubernetesToolProjectionPreservesPendingClosure(t *testing.T) {
	base := entity.KubernetesToolView{InvocationRef: "inv_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", ConnectionRef: "int_synthetic", TargetRef: "target_synthetic", TargetRevision: 1, Operation: "kubernetes.exec", State: "RUNNING", Version: 2, WaitingForGate: true}
	if view, err := kubernetesToolViewToProto(base); err != nil || !view.WaitingForGate {
		t.Fatal("pending tool lost", err)
	}
	for name, mutate := range map[string]func(*entity.KubernetesToolView){
		"unknown state":        func(v *entity.KubernetesToolView) { v.State = "FUTURE" },
		"unknown operation":    func(v *entity.KubernetesToolView) { v.Operation = "shell.raw" },
		"foreign locator":      func(v *entity.KubernetesToolView) { v.InvocationRef = "run_synthetic" },
		"zero version":         func(v *entity.KubernetesToolView) { v.Version = 0 },
		"inexact JSON version": func(v *entity.KubernetesToolView) { v.Version = 1 << 53 },
		"zero target":          func(v *entity.KubernetesToolView) { v.TargetRevision = 0 },
		"terminal open Gate":   func(v *entity.KubernetesToolView) { v.State = "CANCELLED" },
	} {
		t.Run(name, func(t *testing.T) {
			v := base
			mutate(&v)
			if _, err := kubernetesToolViewToProto(v); err == nil {
				t.Fatal("unsafe metadata accepted")
			}
		})
	}
	for _, state := range []string{"READY", "WAITING_APPROVAL", "RUNNING", "SUCCEEDED", "FAILED", "REJECTED", "CANCELLED", "UNKNOWN_OUTCOME"} {
		v := base
		v.State = enum.KubernetesToolState(state)
		v.WaitingForGate = false
		if _, err := kubernetesToolViewToProto(v); err != nil {
			t.Fatal("registered owner state rejected", state, err)
		}
	}
}

// TestKubernetesToolProjectionRejectsUnknownReason pins the typed no-stream failure while refusing unknown or mismatched display codes.
// TestKubernetesToolProjectionRejectsUnknownReason сохраняет typed no-stream отказ, отклоняя unknown либо mismatched display codes.
func TestKubernetesToolProjectionRejectsUnknownReason(t *testing.T) {
	base := entity.KubernetesToolView{InvocationRef: "inv_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", ConnectionRef: "int_synthetic", TargetRef: "target_synthetic", TargetRevision: 1, Operation: "kubernetes.port-forward", State: "FAILED", Version: 2, SafeReasonCode: enum.KubernetesToolReasonPortForwardProfileUnsupported}
	view, err := kubernetesToolViewToProto(base)
	if err != nil || view.SafeReasonCode != cp.ManagedKubernetesToolSafeReasonCode_MANAGED_KUBERNETES_TOOL_SAFE_REASON_CODE_PORT_FORWARD_PROFILE_UNSUPPORTED {
		t.Fatal("exact persisted preflight reason lost", err)
	}
	for _, mode := range []string{"unknown", "operation", "state"} {
		v := base
		switch mode {
		case "unknown":
			v.SafeReasonCode = 99
		case "operation":
			v.Operation = "kubernetes.exec"
		case "state":
			v.State = "RUNNING"
		}
		if _, err := kubernetesToolViewToProto(v); err == nil {
			t.Fatal("foreign reason crossed owner mapper", mode)
		}
	}
}
