package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"testing"
	"time"
)

// TestKubernetesFileCleanupProjection rejects unrelated source/receipt tuples and physical counters above one immutable object.
// TestKubernetesFileCleanupProjection отклоняет чужие source/receipt tuples и physical counters более одного immutable object.
func TestKubernetesFileCleanupProjection(t *testing.T) {
	result := entity.KubernetesFileCleanupReadback{View: entity.KubernetesFileCleanupView{SourceRef: "kfs_synthetic", InvocationRef: "inv_synthetic", Cleanup: entity.KubernetesStagingCleanupStatus{Version: 4, RepairGeneration: 1, OutstandingCount: 1, OutstandingBytes: 2048}}, Receipt: &entity.KubernetesFileCleanupRepairReceipt{SourceRef: "kfs_synthetic", Version: 4, RepairGeneration: 1, RepairedCount: 1, AcceptedAt: time.Now()}}
	view, receipt, err := kubernetesFileCleanupReadbackToProto(result)
	if err != nil || receipt == nil || view.SourceRef != result.View.SourceRef || view.Cleanup.OutstandingBytes != 2048 {
		t.Fatal("minimal cleanup receipt lost", err)
	}
	for _, change := range []func(*entity.KubernetesFileCleanupReadback){
		func(r *entity.KubernetesFileCleanupReadback) { r.View.SourceRef = "kfs_foreign00" },
		func(r *entity.KubernetesFileCleanupReadback) { r.View.Cleanup.OutstandingCount = 2 },
		func(r *entity.KubernetesFileCleanupReadback) { r.View.Cleanup.OutstandingBytes = 16<<20 + 1 },
		func(r *entity.KubernetesFileCleanupReadback) { r.Receipt.Version = 5 },
		func(r *entity.KubernetesFileCleanupReadback) { r.Receipt.RepairGeneration = 2 },
		func(r *entity.KubernetesFileCleanupReadback) { r.Receipt.AcceptedAt = time.Now().Add(time.Hour) },
	} {
		changed := result
		copy := *result.Receipt
		changed.Receipt = &copy
		change(&changed)
		if _, _, err := kubernetesFileCleanupReadbackToProto(changed); err == nil {
			t.Fatal("invalid FILE cleanup tuple reached public wire")
		}
	}
}
