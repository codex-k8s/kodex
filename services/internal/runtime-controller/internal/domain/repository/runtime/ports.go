// Package runtime объявляет узкие порты orchestration runtime-controller.
package runtime

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
)

type AdmitResult struct {
	Execution  entity.Execution
	LeaseToken string
}

type ControlPlane interface {
	Check(context.Context) error
	Claim(context.Context, string) (entity.Execution, error)
	GetExecution(context.Context, string, uint64) (entity.Execution, error)
	GetRevision(context.Context, string, uint64) (entity.Revision, error)
	Admit(context.Context, string, entity.Execution) (AdmitResult, error)
	Reschedule(context.Context, string, entity.Execution) (entity.Execution, error)
	Heartbeat(context.Context, string, entity.Execution, string) (entity.Execution, error)
	Complete(context.Context, string, entity.Execution, string, string, string, string) (entity.Execution, error)
	Incident(context.Context, string, entity.Execution, enum.IncidentKind, string, string) (entity.Execution, error)
	Expire(context.Context, string) (entity.Execution, error)
	RecordArchive(context.Context, string, entity.Execution, string, string) (entity.Execution, error)
	VerifyRestore(context.Context, string, entity.Execution, string, string, string) (entity.Execution, error)
	AuthorizeCleanup(context.Context, string, entity.Execution, uint64, string, string, string) (entity.Execution, error)
	ExpireCleanup(context.Context, string, entity.Execution) (entity.Execution, error)
	ConsumeCleanup(context.Context, string, entity.Execution, entity.PVCDeletionProof) (entity.Execution, error)
	Close() error
}

type Journal struct {
	Execution               entity.Execution
	AdmitIdempotencyKey     string
	HeartbeatIdempotencyKey string
	CompleteIdempotencyKey  string
	IncidentIdempotencyKey  string
	ArchiveIdempotencyKey   string
	RestoreIdempotencyKey   string
	CleanupIdempotencyKey   string
	AdmissionRequest        entity.Execution
	Phase                   string
	LeaseToken              string
}

type Cluster interface {
	Check(context.Context) error
	Capacity(context.Context, entity.Execution, entity.Revision) (entity.CapacityDecision, error)
	EnsureJournal(context.Context, entity.Execution) (Journal, error)
	LoadJournal(context.Context, entity.RuntimeStatus) (Journal, error)
	Materialize(context.Context, entity.Execution, entity.Revision, string, Journal) (entity.RuntimeStatus, error)
	List(context.Context) ([]entity.RuntimeStatus, error)
	UpdateJournal(context.Context, entity.Execution, string) error
	RevokeAccess(context.Context, entity.Execution) error
	DeletePod(context.Context, entity.RuntimeStatus) error
	DeletePVC(context.Context, entity.Execution, entity.RuntimeStatus) (entity.PVCDeletionProof, error)
	EnsureArchiveJob(context.Context, entity.Execution, entity.RuntimeStatus) error
	EnsureRestoreVerifierJob(context.Context, entity.Execution, entity.RuntimeStatus) error
	EnsureCleanupAuthorizerJob(context.Context, entity.Execution, entity.RuntimeStatus) error
	OpenArchiveGate(context.Context, entity.Execution, entity.RuntimeStatus) error
	CleanupTemporary(context.Context, time.Time) (int, error)
}
