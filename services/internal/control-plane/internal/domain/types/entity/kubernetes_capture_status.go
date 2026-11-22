package entity

import (
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// KubernetesCaptureSummary exposes only the original owner's discoverable capture identity and
// technical status before an artifact exists. Version is OCC metadata, never a task fence or grant.
// KubernetesCaptureSummary раскрывает лишь доступные original owner capture identity и technical
// status до создания artifact. Version — OCC metadata, не task fence либо grant.
type KubernetesCaptureSummary struct {
	CaptureRef, StreamRef      string
	CaptureGeneration, Version int64
	State                      enum.ManagedKubernetesCaptureState
	ReasonCode                 string
}

// KubernetesCaptureStatus records immutable capture provenance and current technical retry
// progress without private object coordinates, credentials, execution credit or proposed outcome.
// KubernetesCaptureStatus хранит immutable capture provenance и current technical retry progress
// без private object coordinates, credentials, execution credit либо proposed outcome.
type KubernetesCaptureStatus struct {
	KubernetesCaptureSummary
	InvocationRef, RunRef, NodeRef, Operation string
	Kind                                      enum.ManagedKubernetesCaptureKind
	AttemptCount, RepairGeneration            int64
	SealedAt                                  time.Time
	RetainUntil                               *time.Time
	Artifacts                                 []ManagedKubernetesCapturedArtifact
	Cleanup                                   *KubernetesStagingCleanupStatus
}

// KubernetesCaptureRepairReceipt acknowledges one server-created retry cycle. It never renews
// an old task lease or admits native execution; the original payload and intent remain immutable.
// KubernetesCaptureRepairReceipt подтверждает один server-created retry cycle. Старый task lease
// не продлевается, native execution не допускается; original payload и intent неизменны.
type KubernetesCaptureRepairReceipt struct {
	CaptureRef                              string
	RepairGeneration, Version, AttemptCount int64
	AcceptedAt                              time.Time
}

// KubernetesCaptureStatusReadback separates current metadata from the exact original repair
// acknowledgement so a lost mutation response needs one Get, never another repair submission.
// KubernetesCaptureStatusReadback отделяет current metadata от exact original repair acknowledgement:
// потерянный mutation response требует один Get, не новый repair submission.
type KubernetesCaptureStatusReadback struct {
	View           KubernetesCaptureStatus
	RepairReceipt  *KubernetesCaptureRepairReceipt
	CleanupReceipt *KubernetesStagingCleanupRepairReceipt
}

// KubernetesStagingCleanupStatus exposes physical outstanding reservations; Version counts explicit
// repair cycles, not worker progress. No object tuple or cleanup fence crosses this public boundary.
// KubernetesStagingCleanupStatus раскрывает physical outstanding reservations; Version считает explicit
// repair cycles, не worker progress. Object tuple либо cleanup fence не выходят в public boundary.
type KubernetesStagingCleanupStatus struct {
	Version, RepairGeneration                                                  int64
	OutstandingCount, OutstandingBytes, ExhaustedCount, EligibleExhaustedCount int64
}

// KubernetesStagingCleanupRepairReceipt preserves one accepted batch acknowledgement across lost ACKs.
// It does not assert successful deletion, quota release or renewed native execution.
// KubernetesStagingCleanupRepairReceipt сохраняет acknowledgement одного accepted batch при lost ACK.
// Успешное удаление, освобождение quota либо renewed native execution не утверждаются.
type KubernetesStagingCleanupRepairReceipt struct {
	CaptureRef                                                        string
	Version, RepairGeneration, RepairedCount, RemainingExhaustedCount int64
	AcceptedAt                                                        time.Time
}
