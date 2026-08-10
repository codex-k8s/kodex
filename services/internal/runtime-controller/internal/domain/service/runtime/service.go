// Package runtime реализует orchestration без Kubernetes и transport DTO.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/repository/runtime"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
)

const (
	terminalSucceeded = "SUCCEEDED"
	terminalFailed    = "FAILED"
	terminalBlocked   = "BLOCKED"
)

type Observer interface {
	Observe(operation, outcome string)
}

type Service struct {
	controlPlane          runtimerepo.ControlPlane
	cluster               runtimerepo.Cluster
	observer              Observer
	warmTTL               time.Duration
	watchdog              time.Duration
	archiveRestoreEnabled bool
}

func New(
	controlPlane runtimerepo.ControlPlane,
	cluster runtimerepo.Cluster,
	observer Observer,
	warmTTL time.Duration,
	watchdog time.Duration,
	archiveRestoreEnabled bool,
) (*Service, error) {
	if controlPlane == nil || cluster == nil || observer == nil ||
		warmTTL != 4*time.Hour || watchdog < 30*time.Second || watchdog > 10*time.Minute {
		return nil, errs.ErrInvalidInput
	}
	return &Service{
		controlPlane:          controlPlane,
		cluster:               cluster,
		observer:              observer,
		warmTTL:               warmTTL,
		watchdog:              watchdog,
		archiveRestoreEnabled: archiveRestoreEnabled,
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
	if !service.archiveRestoreEnabled && execution.RestoreSourceExecutionID != "" {
		service.observer.Observe("restore", "disabled")
		return errs.ErrStateConflict
	}
	journal, err := service.cluster.EnsureJournal(ctx, execution)
	if err != nil {
		return err
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
	decision, err := service.cluster.Capacity(ctx, execution, revision)
	if err != nil {
		service.observer.Observe("capacity", "error")
		return err
	}
	if !decision.Admitted && decision.Eviction != nil {
		if err := service.evict(ctx, *decision.Eviction, decision.Reason == "session_replacement"); err != nil {
			service.observer.Observe("evict", "rejected")
			return errs.ErrCapacityDeferred
		}
		decision, err = service.cluster.Capacity(ctx, execution, revision)
		if err != nil {
			return err
		}
	}
	if !decision.Admitted {
		if decision.Reason == "quota_stale" && !time.Now().UTC().Before(execution.RescheduleAfter) {
			previous, err := service.controlPlane.Reschedule(ctx,
				uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-reschedule:"+execution.ID+":"+strconv.FormatUint(execution.Version, 10))).String(), execution)
			if err != nil {
				return err
			}
			return service.cluster.UpdateJournal(ctx, previous, "")
		}
		service.observer.Observe("capacity", "deferred")
		return errs.ErrCapacityDeferred
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
	if err := service.authorizeRestoreMaterialization(ctx, admitted.Execution); err != nil {
		return err
	}
	if err := service.cluster.UpdateJournal(ctx, admitted.Execution, admitted.LeaseToken); err != nil {
		return err
	}
	journal.Execution = admitted.Execution
	journal.LeaseToken = admitted.LeaseToken
	status, err := service.cluster.Materialize(
		ctx, admitted.Execution, revision, admitted.LeaseToken, journal,
	)
	if err != nil {
		if errors.Is(err, errs.ErrDependency) {
			service.observer.Observe("bootstrap", "materializing")
			return nil
		}
		return err
	}
	if status.Ready {
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
	if (execution.State == enum.ExecutionAdmitted || execution.State == enum.ExecutionRunning) && journal.LeaseToken == "" {
		journal, err = service.recoverAdmission(ctx, journal)
		if err != nil {
			return err
		}
		execution = journal.Execution
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
		if errors.Is(err, errs.ErrDependency) && execution.RestoreSourceExecutionID != "" {
			service.observer.Observe("rehydrate", "scheduled")
			return nil
		}
		return err
	}
	if execution.State.Terminal() {
		return service.reconcileRetention(ctx, journal, status)
	}
	if status.Handoff != nil {
		if err := validateHandoff(execution, *status.Handoff); err != nil {
			return err
		}
		return service.complete(ctx, journal, status, status.Handoff.Outcome)
	}
	switch status.Phase {
	case "Succeeded", "Failed":
		return service.recordIncident(
			ctx, journal, execution, enum.IncidentWorkloadUnavailable, "runner_exited_without_handoff",
		)
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

func (service *Service) recoverAdmission(
	ctx context.Context,
	journal runtimerepo.Journal,
) (runtimerepo.Journal, error) {
	request := journal.AdmissionRequest
	if request.Validate() != nil || request.State != enum.ExecutionPending ||
		!sameExecutionLineage(request, journal.Execution) {
		return runtimerepo.Journal{}, errs.ErrStateConflict
	}
	admitted, err := service.controlPlane.Admit(ctx, journal.AdmitIdempotencyKey, request)
	if err != nil {
		return runtimerepo.Journal{}, err
	}
	if admitted.LeaseToken == "" || !sameExecutionLineage(request, admitted.Execution) ||
		(admitted.Execution.State != enum.ExecutionAdmitted && admitted.Execution.State != enum.ExecutionRunning) {
		return runtimerepo.Journal{}, errs.ErrStateConflict
	}
	if admitted.Execution.State == enum.ExecutionAdmitted {
		if err := service.authorizeRestoreMaterialization(ctx, admitted.Execution); err != nil {
			return runtimerepo.Journal{}, err
		}
	}
	if err := service.cluster.UpdateJournal(ctx, admitted.Execution, admitted.LeaseToken); err != nil {
		return runtimerepo.Journal{}, err
	}
	journal.Execution = admitted.Execution
	journal.LeaseToken = admitted.LeaseToken
	return journal, nil
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
			if reflect.DeepEqual(current, journal.Execution) {
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
		left.EffectiveRuntimeSHA256 == right.EffectiveRuntimeSHA256 &&
		left.ImmutableInputSHA256 == right.ImmutableInputSHA256 &&
		left.AgentSessionKey == right.AgentSessionKey && left.AgentSessionID == right.AgentSessionID &&
		left.AgentSessionTurnID == right.AgentSessionTurnID && left.AgentRunID == right.AgentRunID &&
		left.AgentBindingSHA256 == right.AgentBindingSHA256 &&
		left.CredentialSnapshotSHA256 == right.CredentialSnapshotSHA256 &&
		left.WorkloadTicketSHA256 == right.WorkloadTicketSHA256 &&
		left.RestoreOperationID == right.RestoreOperationID &&
		left.RestoreOperationGeneration == right.RestoreOperationGeneration &&
		left.RestoreSourceAuthoritySHA256 == right.RestoreSourceAuthoritySHA256 &&
		left.RestoreSourceExecutionID == right.RestoreSourceExecutionID &&
		left.RestoreSourceVersion == right.RestoreSourceVersion &&
		left.RestoreSourceFence == right.RestoreSourceFence &&
		left.RestoreSourceArchiveReference == right.RestoreSourceArchiveReference &&
		left.RestoreSourceArchiveSHA256 == right.RestoreSourceArchiveSHA256 &&
		left.RestoreSourceArchiveObjectKey == right.RestoreSourceArchiveObjectKey &&
		left.RestoreSourceArchiveVersionID == right.RestoreSourceArchiveVersionID &&
		left.RestoreSourceArchiveKMSKeyARN == right.RestoreSourceArchiveKMSKeyARN &&
		left.RestoreSourceArchiveObjectLockMode == right.RestoreSourceArchiveObjectLockMode &&
		left.RestoreSourceArchiveRetainUntil.Equal(right.RestoreSourceArchiveRetainUntil) &&
		left.RestoreSourceRuntimeRevisionSHA256 == right.RestoreSourceRuntimeRevisionSHA256 &&
		left.RestoreSourceImmutableInputSHA256 == right.RestoreSourceImmutableInputSHA256 &&
		left.RestoreSourceProofReference == right.RestoreSourceProofReference &&
		left.RestoreSourceProofSHA256 == right.RestoreSourceProofSHA256 &&
		left.RestoreSourceRetentionPolicyID == right.RestoreSourceRetentionPolicyID &&
		left.RestoreSourceRetentionPolicyVersion == right.RestoreSourceRetentionPolicyVersion &&
		left.RestoreSourceProvenanceSHA256 == right.RestoreSourceProvenanceSHA256 &&
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
	if err := revision.ValidateFor(journal.Execution); err != nil {
		return errs.ErrStateConflict
	}
	decision, err := service.cluster.Capacity(ctx, journal.Execution, revision)
	if err != nil {
		return err
	}
	if !decision.Admitted && decision.Eviction != nil {
		if err := service.evict(ctx, *decision.Eviction, decision.Reason == "session_replacement"); err != nil {
			return errs.ErrCapacityDeferred
		}
		decision, err = service.cluster.Capacity(ctx, journal.Execution, revision)
		if err != nil {
			return err
		}
	}
	if !decision.Admitted {
		if decision.Reason == "quota_stale" && !time.Now().UTC().Before(journal.Execution.RescheduleAfter) {
			previous, err := service.controlPlane.Reschedule(ctx,
				uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-reschedule:"+journal.Execution.ID+":"+strconv.FormatUint(journal.Execution.Version, 10))).String(), journal.Execution)
			if err != nil {
				return err
			}
			return service.cluster.UpdateJournal(ctx, previous, "")
		}
		service.observer.Observe("capacity", "deferred")
		return errs.ErrCapacityDeferred
	}
	admitted, err := service.controlPlane.Admit(
		ctx, journal.AdmitIdempotencyKey, journal.AdmissionRequest,
	)
	if err != nil {
		return err
	}
	journal.Execution = admitted.Execution
	journal.LeaseToken = admitted.LeaseToken
	if err := service.authorizeRestoreMaterialization(ctx, admitted.Execution); err != nil {
		return err
	}
	if err := service.cluster.UpdateJournal(ctx, admitted.Execution, admitted.LeaseToken); err != nil {
		return err
	}
	_, err = service.cluster.Materialize(
		ctx, admitted.Execution, revision, admitted.LeaseToken, journal,
	)
	if errors.Is(err, errs.ErrDependency) {
		service.observer.Observe("bootstrap", "materializing")
		return nil
	}
	return err
}

func (service *Service) authorizeRestoreMaterialization(ctx context.Context, execution entity.Execution) error {
	if execution.RestoreOperationID == "" {
		return nil
	}
	if !service.archiveRestoreEnabled {
		return errs.ErrStateConflict
	}
	if execution.State != enum.ExecutionAdmitted || execution.RestoreOperationGeneration == 0 ||
		execution.RestoreSourceAuthoritySHA256 == "" {
		return errs.ErrStateConflict
	}
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-restore-materialize:"+
		execution.RestoreOperationID+":"+strconv.FormatUint(execution.RestoreOperationGeneration, 10))).String()
	return service.controlPlane.AuthorizeRestoreEffect(ctx, key, execution, "KUBERNETES_MATERIALIZATION")
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
	digestHex := ""
	if status.Handoff != nil {
		reference = status.Handoff.TerminalReference
		digestHex = status.Handoff.TerminalSHA256
	} else {
		digest := sha256.Sum256([]byte(reference + ":" + outcome))
		digestHex = hex.EncodeToString(digest[:])
	}
	updated, err := service.controlPlane.Complete(
		ctx, journal.CompleteIdempotencyKey, journal.Execution,
		journal.LeaseToken, outcome, reference, digestHex, status.Handoff,
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

func validateHandoff(execution entity.Execution, handoff entity.RuntimeHandoff) error {
	if handoff.Schema != "mattercodex.runtime-turn-handoff.v2" || handoff.ExecutionID != execution.ID ||
		handoff.ExecutionVersion == 0 || handoff.Fence == 0 || handoff.GrantGeneration != execution.GrantGeneration ||
		handoff.RuntimeRevisionSHA256 != execution.RuntimeRevisionSHA256 ||
		handoff.EffectiveRuntimeSHA256 != execution.EffectiveRuntimeSHA256 ||
		handoff.ImmutableInputSHA256 != execution.ImmutableInputSHA256 ||
		handoff.SessionID != execution.SessionID || handoff.TurnID != execution.TurnID || handoff.Attempt != execution.Attempt ||
		handoff.ProviderBindingID != execution.ProviderBindingID ||
		handoff.ProviderBindingVersion != execution.ProviderBindingVersion ||
		handoff.ProviderBindingSHA256 != execution.ProviderBindingSHA256 ||
		handoff.ExecutionVersion > execution.Version || handoff.Fence > execution.Fence ||
		execution.Version-handoff.ExecutionVersion != execution.Fence-handoff.Fence ||
		(handoff.Outcome != terminalSucceeded && handoff.Outcome != terminalFailed &&
			handoff.Outcome != terminalBlocked) ||
		handoff.TerminalReference == "" || !sha256PatternString(handoff.TerminalSHA256) || len(handoff.Outputs) == 0 ||
		len(handoff.Outputs) > 32 || !validCodexTerminalBinding(handoff) ||
		handoff.ObservedAt.IsZero() || handoff.ObservedAt.After(time.Now().UTC().Add(time.Minute)) {
		return errs.ErrStateConflict
	}
	for _, output := range handoff.Outputs {
		if output.ArtifactID == "" || output.ArtifactVersion == 0 || output.ArtifactName == "" ||
			!sha256PatternString(output.ArtifactSHA256) || output.ArtifactSizeBytes == 0 ||
			output.ArtifactSizeBytes > 256<<20 || output.Sequence == 0 || output.Total == 0 || output.Sequence > output.Total {
			return errs.ErrStateConflict
		}
		if len(output.ArtifactPayload) != 0 {
			resultDigest := sha256.Sum256(output.ArtifactPayload)
			if output.ArtifactStorageRef != "" || output.ArtifactSizeBytes != uint64(len(output.ArtifactPayload)) ||
				hex.EncodeToString(resultDigest[:]) != output.ArtifactSHA256 {
				return errs.ErrStateConflict
			}
		} else if !strings.HasPrefix(output.ArtifactStorageRef, "s3://") ||
			len(output.ArtifactStorageRef) > 2048 || strings.ContainsAny(output.ArtifactStorageRef, "\x00\r\n") {
			return errs.ErrStateConflict
		}
	}
	return nil
}

func validCodexTerminalBinding(handoff entity.RuntimeHandoff) bool {
	if handoff.CodexSessionID == "" && handoff.ArchiveRelativePath == "" &&
		handoff.ArchiveSHA256 == "" && handoff.ArchiveProvenance == "" {
		return handoff.Outcome == terminalBlocked && strings.HasPrefix(handoff.TerminalReference, "preflight://")
	}
	return uuid.Validate(handoff.CodexSessionID) == nil && validCodexArchivePath(handoff.ArchiveRelativePath) &&
		sha256PatternString(handoff.ArchiveSHA256) && handoff.ArchiveProvenance != "" &&
		validCodexArchiveProvenance(handoff.ArchiveProvenance, handoff.ArchiveRelativePath, handoff.ArchiveSHA256)
}

func validCodexArchiveProvenance(value, path, digest string) bool {
	const prefix = "codex-app-server-rollout-v1:"
	suffix := ":" + path + ":" + digest
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	sourceExecutionID := strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix)
	return uuid.Validate(sourceExecutionID) == nil
}

func validCodexArchivePath(value string) bool {
	return regexp.MustCompile(`^\.matter-codex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`).MatchString(value) &&
		len(value) <= 255 &&
		!strings.Contains(value, "\\") && !strings.Contains(value, "..")
}

func sha256PatternString(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
	// Execution-specific Pod нельзя сохранять как warm authority: его immutable
	// revision, grants и credential mounts уже terminal. Successor получает новый
	// Pod поверх retained PVC; cancel/expiry тем самым останавливают provider
	// sidecar сразу после authoritative owner readback.
	if status.PodName != "" {
		if err := service.cluster.DeletePod(ctx, status); err != nil {
			return err
		}
		status.PodName = ""
		service.observer.Observe("terminal_eviction", "deleted")
	}
	if !status.RetentionOwner {
		return nil
	}
	if !service.archiveRestoreEnabled {
		service.observer.Observe("archive", "disabled")
		return nil
	}
	switch {
	case execution.ArchiveSHA256 == "":
		service.observer.Observe("archive", "scheduled")
		return service.cluster.EnsureArchiveJob(ctx, execution, status)
	case execution.RestoreProofSHA256 == "":
		service.observer.Observe("restore", "scheduled")
		return service.cluster.EnsureRestoreVerifierJob(ctx, execution, status)
	case status.PodName != "":
		if err := service.cluster.OpenArchiveGate(ctx, execution, status); err != nil {
			return err
		}
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
		if !execution.CleanupAuthorizationExpiresAt.After(time.Now().UTC()) {
			service.observer.Observe("cleanup_authorization", "scheduled")
			return service.cluster.EnsureCleanupAuthorizerJob(ctx, execution, status)
		}
		if execution.CleanupPVCName != status.PVCName || execution.CleanupPVCUID != status.PVCUID ||
			execution.CleanupPVCResourceVersion != status.PVCResourceVersion {
			return errs.ErrCleanupUnauthorized
		}
		proof, err := service.cluster.DeletePVC(ctx, execution, status)
		if err != nil {
			return err
		}
		updated, err := service.controlPlane.ConsumeCleanup(
			ctx, journal.CleanupIdempotencyKey, execution, proof,
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
