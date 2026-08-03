// Package runtime реализует orchestration без Kubernetes и transport DTO.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
)

const (
	terminalSucceeded  = "SUCCEEDED"
	terminalFailed     = "FAILED"
	cleanupExpiryGrace = 30 * time.Second
)

type Observer interface {
	Observe(operation, outcome string)
}

type Service struct {
	controlPlane    runtimerepo.ControlPlane
	cluster         runtimerepo.Cluster
	observer        Observer
	warmTTL         time.Duration
	pvcRetentionTTL time.Duration
	watchdog        time.Duration
}

func New(
	controlPlane runtimerepo.ControlPlane,
	cluster runtimerepo.Cluster,
	observer Observer,
	warmTTL time.Duration,
	pvcRetentionTTL time.Duration,
	watchdog time.Duration,
) (*Service, error) {
	if controlPlane == nil || cluster == nil || observer == nil ||
		warmTTL != 4*time.Hour || pvcRetentionTTL < 7*24*time.Hour ||
		pvcRetentionTTL < warmTTL || watchdog < 30*time.Second || watchdog > 10*time.Minute {
		return nil, errs.ErrInvalidInput
	}
	return &Service{
		controlPlane:    controlPlane,
		cluster:         cluster,
		observer:        observer,
		warmTTL:         warmTTL,
		pvcRetentionTTL: pvcRetentionTTL,
		watchdog:        watchdog,
	}, nil
}

func (service *Service) Check(ctx context.Context) error {
	if err := service.controlPlane.Check(ctx); err != nil {
		return fmt.Errorf("check protected control-plane path: %w", err)
	}
	if err := service.cluster.Check(ctx); err != nil {
		return fmt.Errorf("check Kubernetes runtime path: %w", err)
	}
	return nil
}

// ReconcileNext материализует не более одной server-owned attempt.
func (service *Service) ReconcileNext(ctx context.Context) error {
	execution, err := service.controlPlane.Claim(ctx, uuid.NewString())
	if err != nil {
		if errors.Is(err, errs.ErrNoWork) {
			service.observer.Observe("claim", "empty")
			return nil
		}
		service.observer.Observe("claim", "error")
		return err
	}
	if err := execution.Validate(); err != nil || execution.State != enum.ExecutionPending {
		service.observer.Observe("claim", "invalid")
		return errs.ErrStateConflict
	}
	revision, err := service.controlPlane.GetRevision(
		ctx, execution.RuntimeRevisionID, execution.RuntimeRevisionVersion,
	)
	if err != nil {
		return err
	}
	if err := revision.ValidateFor(execution); err != nil {
		return errs.ErrStateConflict
	}
	decision, err := service.cluster.Capacity(ctx, execution)
	if err != nil {
		service.observer.Observe("capacity", "error")
		return err
	}
	if !decision.Admitted && decision.Eviction != nil {
		if err := service.evict(ctx, *decision.Eviction, decision.Reason == "session_replacement"); err != nil {
			service.observer.Observe("evict", "rejected")
			return errs.ErrCapacityDeferred
		}
		decision, err = service.cluster.Capacity(ctx, execution)
		if err != nil {
			return err
		}
	}
	if !decision.Admitted {
		service.observer.Observe("capacity", "deferred")
		return errs.ErrCapacityDeferred
	}
	journal, err := service.cluster.EnsureJournal(ctx, execution)
	if err != nil {
		return err
	}
	admitted, err := service.controlPlane.Admit(
		ctx, journal.AdmitIdempotencyKey, execution,
	)
	if err != nil {
		return err
	}
	if err := admitted.Execution.Validate(); err != nil ||
		admitted.Execution.State != enum.ExecutionAdmitted || admitted.LeaseToken == "" {
		return errs.ErrStateConflict
	}
	if err := service.cluster.UpdateJournal(ctx, admitted.Execution, admitted.LeaseToken); err != nil {
		return err
	}
	status, err := service.cluster.Materialize(
		ctx, admitted.Execution, revision, admitted.LeaseToken, journal,
	)
	if err != nil {
		_ = service.recordIncident(ctx, journal, admitted.Execution,
			enum.IncidentReconcileFailed, "materialize_failed")
		return err
	}
	if status.Ready {
		journal.Execution = admitted.Execution
		journal.LeaseToken = admitted.LeaseToken
		return service.heartbeat(ctx, journal)
	}
	service.observer.Observe("reconcile", "materialized")
	return nil
}

func (service *Service) evict(ctx context.Context, status entity.RuntimeStatus, sessionReplacement bool) error {
	if status.AccessProfile == enum.AccessClusterAdmin && !sessionReplacement {
		return errs.ErrStateConflict
	}
	execution, err := service.controlPlane.GetExecution(
		ctx, status.ExecutionID, status.Version,
	)
	if err != nil || !execution.State.Terminal() {
		return errs.ErrStateConflict
	}
	if err := service.cluster.DeletePod(ctx, status); err != nil {
		return err
	}
	service.observer.Observe("evict", "deleted")
	return nil
}

// ReconcileExisting продолжает crash-safe journal и terminal archive chain.
func (service *Service) ReconcileExisting(ctx context.Context) error {
	statuses, err := service.cluster.List(ctx)
	if err != nil {
		return err
	}
	var joined error
	for _, status := range statuses {
		if err := service.reconcileStatus(ctx, status); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (service *Service) reconcileStatus(ctx context.Context, status entity.RuntimeStatus) error {
	journal, err := service.cluster.LoadJournal(ctx, status)
	if err != nil {
		return err
	}
	if !journal.Execution.State.Terminal() || status.RetentionOwner ||
		status.PVCDeletionStarted || status.PVCDeleted {
		journal, err = service.rejoinExecution(ctx, journal)
		if err != nil {
			return err
		}
	}
	execution := journal.Execution
	if execution.State == enum.ExecutionPending {
		return service.resumeAdmission(ctx, journal)
	}
	if (execution.State == enum.ExecutionAdmitted || execution.State == enum.ExecutionRunning) &&
		status.Phase == "Missing" {
		if time.Since(status.LastTransition) >= service.watchdog {
			if err := service.recordIncident(
				ctx, journal, execution, enum.IncidentHeartbeatMissed, "pod_missing",
			); err != nil {
				return err
			}
			journal, err = service.cluster.LoadJournal(ctx, status)
			if err != nil {
				return err
			}
			execution = journal.Execution
		}
		revision, err := service.controlPlane.GetRevision(
			ctx, execution.RuntimeRevisionID, execution.RuntimeRevisionVersion,
		)
		if err != nil {
			return err
		}
		_, err = service.cluster.Materialize(ctx, execution, revision, journal.LeaseToken, journal)
		return err
	}
	if execution.State.Terminal() {
		return service.reconcileRetention(ctx, journal, status)
	}
	switch status.Phase {
	case "Succeeded":
		return service.complete(ctx, journal, status, terminalSucceeded)
	case "Failed":
		return service.complete(ctx, journal, status, terminalFailed)
	case "Running":
		if status.Ready {
			err := service.heartbeat(ctx, journal)
			if errors.Is(err, errs.ErrStateConflict) {
				return service.cluster.DeletePod(ctx, status)
			}
			return err
		}
		if time.Since(status.LastTransition) >= service.watchdog {
			return service.recordIncident(
				ctx, journal, execution,
				enum.IncidentWorkloadUnavailable, "pod_not_ready",
			)
		}
	}
	return nil
}

func (service *Service) rejoinExecution(
	ctx context.Context,
	journal runtimerepo.Journal,
) (runtimerepo.Journal, error) {
	expectedVersion := journal.Execution.Version
	for attempt := 0; attempt < 4; attempt++ {
		current, err := service.controlPlane.GetExecution(ctx, journal.Execution.ID, expectedVersion)
		if err == nil {
			if !sameExecutionLineage(journal.Execution, current) ||
				current.Version < journal.Execution.Version || current.Fence < journal.Execution.Fence ||
				current.Version-journal.Execution.Version != current.Fence-journal.Execution.Fence ||
				current.Version-journal.Execution.Version > 3 {
				return runtimerepo.Journal{}, errs.ErrStateConflict
			}
			if current == journal.Execution {
				return journal, nil
			}
			leaseToken := journal.LeaseToken
			if current.State.Terminal() {
				leaseToken = ""
			}
			if err := service.cluster.UpdateJournal(ctx, current, leaseToken); err != nil {
				return runtimerepo.Journal{}, err
			}
			journal.Execution = current
			journal.LeaseToken = leaseToken
			return journal, nil
		}
		if !errors.Is(err, errs.ErrNoWork) {
			return runtimerepo.Journal{}, err
		}
		if expectedVersion == ^uint64(0) {
			break
		}
		expectedVersion++
	}
	return runtimerepo.Journal{}, errs.ErrStateConflict
}

func sameExecutionLineage(left, right entity.Execution) bool {
	return left.ID == right.ID && left.OrganizationID == right.OrganizationID &&
		left.ProjectID == right.ProjectID && left.ProcessID == right.ProcessID &&
		left.SessionID == right.SessionID && left.ThreadID == right.ThreadID &&
		left.RoleID == right.RoleID && left.TurnID == right.TurnID && left.Attempt == right.Attempt &&
		left.RuntimeRevisionID == right.RuntimeRevisionID &&
		left.RuntimeRevisionVersion == right.RuntimeRevisionVersion &&
		left.RuntimeRevisionSHA256 == right.RuntimeRevisionSHA256 &&
		left.ImmutableInputSHA256 == right.ImmutableInputSHA256 &&
		left.ResourceClass == right.ResourceClass && left.AccessProfile == right.AccessProfile &&
		left.WorkloadID == right.WorkloadID && left.WorkloadSPIFFEID == right.WorkloadSPIFFEID &&
		left.GrantGeneration == right.GrantGeneration
}

func (service *Service) resumeAdmission(
	ctx context.Context,
	journal runtimerepo.Journal,
) error {
	revision, err := service.controlPlane.GetRevision(
		ctx, journal.Execution.RuntimeRevisionID, journal.Execution.RuntimeRevisionVersion,
	)
	if err != nil {
		return err
	}
	admitted, err := service.controlPlane.Admit(
		ctx, journal.AdmitIdempotencyKey, journal.Execution,
	)
	if err != nil {
		return err
	}
	journal.Execution = admitted.Execution
	journal.LeaseToken = admitted.LeaseToken
	_, err = service.cluster.Materialize(
		ctx, admitted.Execution, revision, admitted.LeaseToken, journal,
	)
	return err
}

func (service *Service) heartbeat(ctx context.Context, journal runtimerepo.Journal) error {
	if journal.LeaseToken == "" {
		return errs.ErrStateConflict
	}
	updated, err := service.controlPlane.Heartbeat(
		ctx, journal.HeartbeatIdempotencyKey, journal.Execution, journal.LeaseToken,
	)
	if err != nil {
		service.observer.Observe("heartbeat", "error")
		return err
	}
	if err := service.cluster.UpdateJournal(ctx, updated, journal.LeaseToken); err != nil {
		return err
	}
	service.observer.Observe("heartbeat", "renewed")
	return nil
}

func (service *Service) complete(
	ctx context.Context,
	journal runtimerepo.Journal,
	status entity.RuntimeStatus,
	outcome string,
) error {
	reference := "kubernetes-pod:" + status.PodUID
	digest := sha256.Sum256([]byte(reference + ":" + outcome))
	updated, err := service.controlPlane.Complete(
		ctx, journal.CompleteIdempotencyKey, journal.Execution,
		journal.LeaseToken, outcome, reference, hex.EncodeToString(digest[:]),
	)
	if err != nil {
		return err
	}
	if err := service.cluster.UpdateJournal(ctx, updated, ""); err != nil {
		return err
	}
	journal.Execution = updated
	service.observer.Observe("complete", "terminal")
	return service.reconcileRetention(ctx, journal, status)
}

func (service *Service) recordIncident(
	ctx context.Context,
	journal runtimerepo.Journal,
	execution entity.Execution,
	kind enum.IncidentKind,
	reason string,
) error {
	digest := sha256.Sum256([]byte(execution.ID + ":" + reason))
	updated, err := service.controlPlane.Incident(
		ctx, journal.IncidentIdempotencyKey, execution, kind,
		uuid.NewSHA1(uuid.NameSpaceOID, []byte(journal.IncidentIdempotencyKey+":"+reason)).String(),
		hex.EncodeToString(digest[:]),
	)
	if err != nil {
		return err
	}
	service.observer.Observe("incident", "recorded")
	return service.cluster.UpdateJournal(ctx, updated, journal.LeaseToken)
}

func (service *Service) reconcileRetention(
	ctx context.Context,
	journal runtimerepo.Journal,
	status entity.RuntimeStatus,
) error {
	execution := journal.Execution
	if err := service.cluster.RevokeAccess(ctx, execution); err != nil {
		return err
	}
	idle := time.Since(status.LastTransition)
	podTerminal := status.Phase == "Succeeded" || status.Phase == "Failed"
	if status.PodName != "" && !podTerminal {
		if err := service.cluster.DeletePod(ctx, status); err != nil {
			return err
		}
		service.observer.Observe("idle_eviction", "deleted")
		return nil
	}
	if idle >= service.warmTTL && status.PodName != "" {
		if err := service.cluster.DeletePod(ctx, status); err != nil {
			return err
		}
		service.observer.Observe("idle_eviction", "deleted")
	}
	if !status.RetentionOwner {
		return nil
	}
	switch {
	case execution.ArchiveSHA256 == "":
		service.observer.Observe("archive", "scheduled")
		return service.cluster.EnsureArchiveJob(ctx, execution, status)
	case execution.RestoreProofSHA256 == "":
		service.observer.Observe("restore", "scheduled")
		return service.cluster.EnsureRestoreVerifierJob(ctx, execution, status)
	case idle < service.pvcRetentionTTL:
		return nil
	case execution.CleanupAuthorizationState == "NONE" ||
		execution.CleanupAuthorizationState == "EXPIRED":
		service.observer.Observe("cleanup_authorization", "scheduled")
		return service.cluster.EnsureCleanupAuthorizerJob(ctx, execution, status)
	case execution.CleanupAuthorizationState == "ACTIVE":
		if execution.CleanupAuthorizationID == "" ||
			execution.CleanupAuthorizationExpiresAt.IsZero() {
			return errs.ErrCleanupUnauthorized
		}
		if !execution.CleanupAuthorizationExpiresAt.Add(cleanupExpiryGrace).After(time.Now().UTC()) {
			service.observer.Observe("cleanup_authorization", "scheduled")
			return service.cluster.EnsureCleanupAuthorizerJob(ctx, execution, status)
		}
		if err := service.cluster.DeletePVC(ctx, status); err != nil {
			return err
		}
		updated, err := service.controlPlane.ConsumeCleanup(
			ctx, journal.CleanupIdempotencyKey, execution,
		)
		if err != nil {
			return err
		}
		service.observer.Observe("cleanup", "consumed")
		return service.cluster.UpdateJournal(ctx, updated, "")
	case execution.CleanupAuthorizationState == "CONSUMED":
		return nil
	default:
		return errs.ErrCleanupUnauthorized
	}
}

func (service *Service) ExpireOne(ctx context.Context) error {
	_, err := service.controlPlane.Expire(ctx, uuid.NewString())
	if errors.Is(err, errs.ErrNoWork) {
		return nil
	}
	return err
}

func (service *Service) CleanupTemporary(ctx context.Context, before time.Time) error {
	count, err := service.cluster.CleanupTemporary(ctx, before)
	if err != nil {
		return err
	}
	if count > 0 {
		service.observer.Observe("temporary_cleanup", "deleted")
	}
	return nil
}
