package entity

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"time"
)

// KubernetesFileInputView reuses the shared closed FILE metadata representation without carrying private bytes.
// Domain ownership and fresh source/target authority remain with the repository.
// KubernetesFileInputView использует shared closed FILE metadata representation без private bytes.
// Domain ownership и fresh source/target authority остаются у repository.
type KubernetesFileInputView struct {
	managedkubernetes.FileInputView
	Cleanup *KubernetesStagingCleanupStatus
}

// KubernetesFileCleanupView contains only original-root closure metadata, never source provenance or bytes.
// KubernetesFileCleanupView содержит лишь original-root closure metadata, не source provenance либо bytes.
type KubernetesFileCleanupView struct {
	SourceRef, InvocationRef string
	Cleanup                  KubernetesStagingCleanupStatus
}

// KubernetesFileCleanupRepairReceipt retains one bounded cycle acknowledgement without claiming deletion or execution.
// KubernetesFileCleanupRepairReceipt хранит bounded cycle acknowledgement, не утверждая deletion либо execution.
type KubernetesFileCleanupRepairReceipt struct {
	SourceRef                                                         string
	Version, RepairGeneration, RepairedCount, RemainingExhaustedCount int64
	AcceptedAt                                                        time.Time
}

// KubernetesFileCleanupReadback pairs current minimal metadata with an optional immutable original-key receipt.
// KubernetesFileCleanupReadback связывает current minimal metadata с optional immutable original-key receipt.
type KubernetesFileCleanupReadback struct {
	View    KubernetesFileCleanupView
	Receipt *KubernetesFileCleanupRepairReceipt
}

// ManagedKubernetesFilePreparation separates safe metadata from the fresh private source allowance.
// The allowance cannot authorize native execution by itself and never survives its original bound.
// ManagedKubernetesFilePreparation отделяет safe metadata от fresh private source allowance.
// Allowance сам не разрешает native execution и не переживает original bound.
type ManagedKubernetesFilePreparation struct {
	View            KubernetesFileInputView
	AuthorizedUntil time.Time
}
