package grpc

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// TestKubernetesStagingCleanupPublicProjection rejects mixed upstream counters and unrelated repair
// receipts before transport, without turning a valid repair acknowledgement into deletion success.
// TestKubernetesStagingCleanupPublicProjection отклоняет mixed upstream counters и чужие repair
// receipts до transport; valid repair acknowledgement не становится подтверждением удаления.
func TestKubernetesStagingCleanupPublicProjection(t *testing.T) {
	now := time.Now().UTC()
	view := entity.KubernetesCaptureStatus{KubernetesCaptureSummary: entity.KubernetesCaptureSummary{CaptureRef: "koc_synthetic", StreamRef: "kst_synthetic", CaptureGeneration: 1, Version: 3, State: enum.KubernetesCaptureWaitingOwner, ReasonCode: "CAPTURE_RETRY_EXHAUSTED"}, InvocationRef: "inv_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", Operation: "kubernetes.exec", AttemptCount: 3, SealedAt: now.Add(-time.Minute), Cleanup: &entity.KubernetesStagingCleanupStatus{Version: 2, RepairGeneration: 1, OutstandingCount: 2, OutstandingBytes: 30}}
	wire, _, err := kubernetesCaptureStatusToProto(view, nil)
	if err != nil || wire.Cleanup == nil || wire.Cleanup.OutstandingBytes != 30 || wire.Summary.Version != 3 {
		t.Fatal("cleanup metadata replaced capture OCC or released quota", err)
	}
	receipt := entity.KubernetesStagingCleanupRepairReceipt{CaptureRef: view.CaptureRef, Version: 2, RepairGeneration: 1, RepairedCount: 1, AcceptedAt: now}
	if result, err := kubernetesStagingCleanupReceiptToProto(view, &receipt); err != nil || result == nil || result.RepairedCount != 1 {
		t.Fatal("valid cleanup receipt was lost", err)
	}
	for _, change := range []func(*entity.KubernetesStagingCleanupStatus){
		func(v *entity.KubernetesStagingCleanupStatus) { v.ExhaustedCount = 3 },
		func(v *entity.KubernetesStagingCleanupStatus) { v.EligibleExhaustedCount = 1 },
		func(v *entity.KubernetesStagingCleanupStatus) { v.OutstandingBytes = -1 },
		func(v *entity.KubernetesStagingCleanupStatus) { v.Version = 1 << 53 },
	} {
		candidate := *view.Cleanup
		change(&candidate)
		if _, err := kubernetesStagingCleanupToProto(&candidate); err == nil {
			t.Fatal("invalid cleanup counters reached transport")
		}
	}
	for _, change := range []func(*entity.KubernetesStagingCleanupRepairReceipt){
		func(r *entity.KubernetesStagingCleanupRepairReceipt) { r.CaptureRef = "koc_foreignxx" },
		func(r *entity.KubernetesStagingCleanupRepairReceipt) { r.Version = 3 },
		func(r *entity.KubernetesStagingCleanupRepairReceipt) { r.RepairedCount = 101 },
		func(r *entity.KubernetesStagingCleanupRepairReceipt) { r.AcceptedAt = view.SealedAt.Add(-time.Second) },
	} {
		candidate := receipt
		change(&candidate)
		if _, err := kubernetesStagingCleanupReceiptToProto(view, &candidate); err == nil {
			t.Fatal("invalid cleanup receipt reached transport")
		}
	}
}
