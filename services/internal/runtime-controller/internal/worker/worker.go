// Package worker реализует ограниченные одноразовые archive/restore/cleanup команды.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/adapters/archive"
	kubeadapter "github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/adapters/kubernetes"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/clients/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/domain/types/enum"
	"github.com/google/uuid"
)

const journalFile = "/var/run/config/mattercodex/runtime-journal/journal.json"

type journalDocument = entity.RuntimeJournal

type Config struct {
	ExecutionID, JournalName, Namespace string
	PVCName, PVCUID, PVCResourceVersion string
	SnapshotPVCUID                      string
	ExpectedVersion, ExpectedFence      uint64
	ControlPlane                        controlplane.Config
	Archive                             archive.Config
}

func RunArchive(ctx context.Context) error {
	config, err := loadConfig(controlplane.ModeArchive)
	if err != nil {
		return err
	}
	document, client, store, err := open(ctx, config)
	if err != nil {
		return err
	}
	defer client.Close()
	execution := document.Execution
	if err := exactExecution(config, document.Execution, execution); err != nil ||
		!execution.State.Terminal() || execution.ArchiveSHA256 != "" {
		return errs.ErrStateConflict
	}
	result, err := store.Archive(ctx, "/archive-source", execution, archive.SnapshotProvenance{
		SnapshotPVCUID: config.SnapshotPVCUID, SourcePVCUID: config.PVCUID,
		SourcePVCResourceVersion: config.PVCResourceVersion,
	})
	if err != nil {
		return err
	}
	updated, err := client.RecordArchive(ctx, document.ArchiveKey, execution, result.Reference, result.SHA256)
	if err != nil {
		return err
	}
	return kubeadapter.PatchWorkerJournal(ctx, config.Namespace, config.JournalName, execution.ID, updated)
}

func RunRestoreVerifier(ctx context.Context) error {
	config, err := loadConfig(controlplane.ModeRestoreVerifier)
	if err != nil {
		return err
	}
	document, client, store, err := open(ctx, config)
	if err != nil {
		return err
	}
	defer client.Close()
	execution := document.Execution
	if err := exactExecution(config, document.Execution, execution); err != nil ||
		!execution.State.Terminal() || execution.ArchiveReference == "" ||
		execution.ArchiveSHA256 == "" || execution.RestoreProofSHA256 != "" {
		return errs.ErrStateConflict
	}
	proof, err := store.RestoreAndProve(ctx, execution, execution.ArchiveReference, execution.ArchiveSHA256)
	if err != nil {
		return err
	}
	updated, err := client.VerifyRestore(
		ctx, document.RestoreKey, execution, execution.ArchiveSHA256, proof.Reference, proof.SHA256,
	)
	if err != nil {
		return err
	}
	return kubeadapter.PatchWorkerJournal(ctx, config.Namespace, config.JournalName, execution.ID, updated)
}

func RunRehydrate(ctx context.Context) error {
	config, err := loadConfig(controlplane.ModeRestoreVerifier)
	if err != nil {
		return err
	}
	document, client, store, err := open(ctx, config)
	if err != nil {
		return err
	}
	defer client.Close()
	target := document.Execution
	if err := exactExecution(config, document.Execution, target); err != nil ||
		target.RestoreSourceExecutionID == "" || document.RehydratePhase != "PENDING" ||
		target.RestoreAssignmentState != "ASSIGNED" || config.PVCUID == "" ||
		config.PVCName == "" || config.PVCResourceVersion == "" {
		return errs.ErrStateConflict
	}
	bindKey := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-rehydrate-bind:"+document.RestoreKey)).String()
	target, err = client.BindRestoreTarget(ctx, bindKey, target,
		config.PVCName, config.PVCUID, config.PVCResourceVersion)
	if err != nil {
		return err
	}
	source := target
	source.ID = target.RestoreSourceExecutionID
	source.RuntimeRevisionSHA256 = target.RestoreSourceRuntimeRevisionSHA256
	source.ImmutableInputSHA256 = target.RestoreSourceImmutableInputSHA256
	source.ArchiveReference = target.RestoreSourceArchiveReference
	source.ArchiveSHA256 = target.RestoreSourceArchiveSHA256
	source.State = enum.ExecutionSucceeded
	source.RestoreSourceExecutionID, source.RestoreSourceArchiveReference = "", ""
	source.RestoreSourceArchiveSHA256, source.RestoreSourceRuntimeRevisionSHA256 = "", ""
	source.RestoreSourceImmutableInputSHA256, source.RestoreSourceProofSHA256 = "", ""
	source.RestoreAssignmentState, source.RestoreAssignmentGeneration = "NONE", 0
	source.RestoreTargetPVCName, source.RestoreTargetPVCUID, source.RestoreTargetPVCResourceVersion = "", "", ""
	source.RehydrateProofReference, source.RehydrateProofSHA256 = "", ""
	proof, err := store.RestoreToAndProve(ctx, source, target, "/restore-target", config.PVCUID)
	if err != nil {
		return err
	}
	completeKey := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-rehydrate-complete:"+document.RestoreKey)).String()
	target, err = client.CompleteRehydrate(ctx, completeKey, target,
		config.PVCName, config.PVCUID, config.PVCResourceVersion, proof.Reference, proof.SHA256)
	if err != nil {
		return err
	}
	return kubeadapter.PatchWorkerRehydration(
		ctx, config.Namespace, config.JournalName, target, config.PVCUID,
		proof.Reference, proof.SHA256,
	)
}

func RunCleanupAuthorizer(ctx context.Context) error {
	config, err := loadConfig(controlplane.ModeCleanupAuthorizer)
	if err != nil {
		return err
	}
	document, client, _, err := open(ctx, config)
	if err != nil {
		return err
	}
	defer client.Close()
	execution := document.Execution
	if err := exactExecution(config, document.Execution, execution); err != nil ||
		!execution.State.Terminal() || execution.ArchiveSHA256 == "" ||
		execution.RestoreProofSHA256 == "" {
		return errs.ErrStateConflict
	}
	switch execution.CleanupAuthorizationState {
	case "ACTIVE":
		expireKey := uuid.NewSHA1(uuid.NameSpaceOID, []byte("runtime-cleanup-expire:"+document.CleanupKey)).String()
		expired, expireErr := client.ExpireCleanup(ctx, expireKey, execution)
		if expireErr != nil {
			return expireErr
		}
		return kubeadapter.PatchWorkerJournal(ctx, config.Namespace, config.JournalName, execution.ID, expired)
	case "NONE", "EXPIRED":
	default:
		return errs.ErrStateConflict
	}
	updated, err := client.AuthorizeCleanup(
		ctx, document.CleanupKey, execution, execution.CleanupAuthorizationGeneration,
		config.PVCName, config.PVCUID, config.PVCResourceVersion,
	)
	if err != nil {
		return err
	}
	return kubeadapter.PatchWorkerJournal(ctx, config.Namespace, config.JournalName, execution.ID, updated)
}

func open(
	ctx context.Context,
	config Config,
) (journalDocument, *controlplane.Client, *archive.Store, error) {
	document, err := readJournal(config)
	if err != nil {
		return journalDocument{}, nil, nil, err
	}
	client, err := controlplane.Dial(ctx, config.ControlPlane)
	if err != nil {
		return journalDocument{}, nil, nil, err
	}
	if err := client.Check(ctx); err != nil {
		_ = client.Close()
		return journalDocument{}, nil, nil, err
	}
	var store *archive.Store
	if config.ControlPlane.Mode != controlplane.ModeCleanupAuthorizer {
		store, err = archive.Open(ctx, config.Archive)
		if err != nil {
			_ = client.Close()
			return journalDocument{}, nil, nil, err
		}
	}
	return document, client, store, nil
}

func readJournal(config Config) (journalDocument, error) {
	info, err := os.Stat(journalFile)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return journalDocument{}, errors.New("runtime journal file is unsafe")
	}
	raw, err := os.ReadFile(journalFile)
	if err != nil {
		return journalDocument{}, errors.New("read runtime journal file")
	}
	var document journalDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		document.Execution.Validate() != nil ||
		document.Execution.ID != config.ExecutionID || document.Execution.Version != config.ExpectedVersion ||
		document.Execution.Fence != config.ExpectedFence || document.ArchiveKey == "" ||
		document.RestoreKey == "" || document.CleanupKey == "" || document.AdmitKey == "" ||
		document.HeartbeatKey == "" || document.CompleteKey == "" || document.IncidentKey == "" ||
		document.AdmissionRequest.Validate() != nil || document.AdmissionRequest.State != "PENDING" ||
		document.Phase == "" || document.PodName == "" || document.PVCName == "" ||
		document.CreatedAt.IsZero() || document.LastTransition.Before(document.CreatedAt) {
		return journalDocument{}, errs.ErrStateConflict
	}
	pvcEvidenceEmpty := document.PVCUID == "" && document.PVCResourceVersion == ""
	pvcEvidencePresent := document.PVCUID != "" && document.PVCResourceVersion != ""
	if (!pvcEvidenceEmpty && !pvcEvidencePresent) || document.PVCDeletionOwner && !pvcEvidencePresent ||
		document.PVCDeleted && !document.PVCDeletionOwner {
		return journalDocument{}, errs.ErrStateConflict
	}
	return document, nil
}

func exactExecution(config Config, journal, current entity.Execution) error {
	if current.ID != config.ExecutionID || current.Version != config.ExpectedVersion ||
		current.Fence != config.ExpectedFence || current.GrantGeneration != journal.GrantGeneration ||
		current.RuntimeRevisionID != journal.RuntimeRevisionID ||
		current.RuntimeRevisionVersion != journal.RuntimeRevisionVersion ||
		current.ImmutableInputSHA256 != journal.ImmutableInputSHA256 || current.Attempt != journal.Attempt ||
		current.SessionID != journal.SessionID || current.TurnID != journal.TurnID {
		return errs.ErrStateConflict
	}
	return nil
}

func loadConfig(mode controlplane.Mode) (Config, error) {
	version, err := strconv.ParseUint(os.Getenv("RUNTIME_EXPECTED_VERSION"), 10, 64)
	if err != nil || version == 0 {
		return Config{}, errors.New("runtime worker expected version is invalid")
	}
	fence, err := strconv.ParseUint(os.Getenv("RUNTIME_EXPECTED_FENCE"), 10, 64)
	if err != nil || fence == 0 {
		return Config{}, errors.New("runtime worker expected fence is invalid")
	}
	namespaceRaw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return Config{}, errors.New("read runtime worker namespace")
	}
	config := Config{
		ExecutionID: os.Getenv("RUNTIME_EXECUTION_ID"), JournalName: os.Getenv("RUNTIME_JOURNAL_NAME"),
		Namespace: strings.TrimSpace(string(namespaceRaw)), ExpectedVersion: version, ExpectedFence: fence,
		PVCName: os.Getenv("RUNTIME_PVC_NAME"), PVCUID: os.Getenv("RUNTIME_PVC_UID"),
		PVCResourceVersion: os.Getenv("RUNTIME_PVC_RESOURCE_VERSION"),
		SnapshotPVCUID:     os.Getenv("RUNTIME_ARCHIVE_SNAPSHOT_PVC_UID"),
		ControlPlane: controlplane.Config{Mode: mode,
			Target:                "control-plane.mattercodex-system.svc:8443",
			TLSServerName:         "control-plane.mattercodex-system.svc.cluster.local",
			CAFile:                "/var/run/config/mattercodex/runtime-worker/control-plane/ca.pem",
			ClientCertificateFile: "/var/run/secrets/mattercodex/runtime-worker/workload-tls/tls.crt",
			ClientPrivateKeyFile:  "/var/run/secrets/mattercodex/runtime-worker/workload-tls/tls.key",
			ApplicationGrantFile:  "/var/run/secrets/mattercodex/runtime-worker/application-grant/application-grant.jws",
			ExpectedIssuerUID:     29001, ExpectedIssuerGID: 29000, DialTimeout: 2 * time.Second,
		},
		Archive: archive.Config{Endpoint: os.Getenv("RUNTIME_S3_ENDPOINT"), Bucket: os.Getenv("RUNTIME_S3_BUCKET"),
			Region: os.Getenv("RUNTIME_S3_REGION"), TLSServerName: os.Getenv("RUNTIME_S3_TLS_SERVER_NAME"),
			CAFile:              "/var/run/config/mattercodex/runtime-worker/s3/ca.pem",
			AccessKeyIDFile:     "/var/run/secrets/mattercodex/runtime-worker/s3/access-key-id",
			SecretAccessKeyFile: "/var/run/secrets/mattercodex/runtime-worker/s3/secret-access-key", RequestTimeout: 15 * time.Second,
			SessionTokenFile: "/var/run/secrets/mattercodex/runtime-worker/s3/session-token",
		},
	}
	if config.ExecutionID == "" || config.JournalName == "" || config.Namespace == "" ||
		filepath.Base(config.JournalName) != config.JournalName {
		return Config{}, errors.New("runtime worker identity is invalid")
	}
	if mode == controlplane.ModeCleanupAuthorizer && (config.PVCName == "" ||
		uuid.Validate(config.PVCUID) != nil || config.PVCResourceVersion == "") {
		return Config{}, errors.New("runtime cleanup PVC tuple is invalid")
	}
	if mode == controlplane.ModeArchive && (config.SnapshotPVCUID == "" ||
		uuid.Validate(config.PVCUID) != nil || config.PVCResourceVersion == "") {
		return Config{}, errors.New("runtime archive snapshot provenance is invalid")
	}
	return config, nil
}
