// Package runner оркестрирует полный backup, verification, retention и restore drill.
package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/postgresbackup"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/retention"
	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
	"github.com/google/uuid"
)

type Observer interface {
	BackupFinished(outcome string)
	DatabaseCompleted(operation, outcome string)
	ObjectCompleted(operation, outcome string)
	RetentionFinished(outcome string, deleted int)
	RestoreFinished(outcome string)
	SetLastSuccessfulBackup(time.Time)
	SetLastVerifiedRestore(time.Time)
}

type Runner struct {
	postgres          *postgresbackup.Manager
	repository        *s3backup.Repository
	credentials       configspec.Credentials
	workDirectory     string
	controllerVersion string
	releaseRevision   string
	observer          Observer
	now               func() time.Time
}

type Policy struct {
	MinimumAge time.Duration
	Keep       int
}

const (
	operationLockTTL                  = 25 * time.Hour
	operationLockReleaseTimeout       = 90 * time.Second
	operationLockReleaseRetryInterval = time.Second
)

type attemptDocument struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	BackupID      string    `json:"backupId"`
	StartedAt     time.Time `json:"startedAt"`
}

type failureDocument struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	OperationID   string    `json:"operationId"`
	FailureClass  string    `json:"failureClass"`
	FailedAt      time.Time `json:"failedAt"`
}

func New(postgres *postgresbackup.Manager, repository *s3backup.Repository, credentials configspec.Credentials,
	workDirectory, controllerVersion, releaseRevision string, observer Observer) (*Runner, error) {
	if postgres == nil || repository == nil || observer == nil || workDirectory == "" ||
		controllerVersion == "" || releaseRevision == "" {
		return nil, errors.New("backup runner configuration is invalid")
	}
	return &Runner{postgres: postgres, repository: repository, credentials: credentials,
		workDirectory: workDirectory, controllerVersion: controllerVersion,
		releaseRevision: releaseRevision, observer: observer, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (runner *Runner) Check(ctx context.Context) error {
	if err := runner.repository.Check(ctx, runner.credentials.ObjectStores); err != nil {
		return err
	}
	return runner.postgres.Check(ctx, runner.credentials.Databases)
}

func (runner *Runner) Backup(ctx context.Context) (backupID string, resultErr error) {
	startedAt := runner.now()
	backupID = newBackupID(startedAt)
	lock, err := runner.repository.AcquireOperationLock(ctx, "backup", backupID, startedAt, operationLockTTL)
	if err != nil {
		runner.observer.BackupFinished("error")
		return backupID, err
	}
	defer func() {
		resultErr = errors.Join(resultErr, releaseOperationLock(ctx, lock))
		if resultErr == nil {
			runner.observer.BackupFinished("success")
			runner.observer.SetLastSuccessfulBackup(runner.now())
			return
		}
		runner.observer.BackupFinished("error")
		failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = runner.repository.PutJSON(failureContext,
			path.Join("backups", backupID, "failure.json"), failureDocument{
				SchemaVersion: 1, Kind: "kodex-backup-failure", OperationID: backupID,
				FailureClass: "backup", FailedAt: runner.now(),
			})
	}()
	directory := filepath.Join(runner.workDirectory, backupID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return backupID, errors.New("create backup attempt workspace")
	}
	defer os.RemoveAll(directory)
	if _, err := runner.repository.PutJSON(ctx, path.Join("backups", backupID, "attempt.json"), attemptDocument{
		SchemaVersion: 1, Kind: "kodex-backup-attempt", BackupID: backupID, StartedAt: startedAt,
	}); err != nil {
		return backupID, fmt.Errorf("record backup attempt: %w", err)
	}
	databases := make([]manifest.Database, 0, len(runner.credentials.Databases))
	for _, database := range runner.credentials.Databases {
		snapshot, err := runner.postgres.Backup(ctx, database, directory)
		if err != nil {
			runner.observer.DatabaseCompleted("backup", "error")
			return backupID, err
		}
		dump, err := runner.repository.PutFile(ctx, path.Join("backups", backupID, "postgres", database.Name+".dump"),
			"application/vnd.postgresql.custom", snapshot.DumpPath, snapshot.DumpDigest, snapshot.DumpSize)
		if err != nil {
			runner.observer.DatabaseCompleted("backup", "error")
			return backupID, fmt.Errorf("store PostgreSQL dump %s: %w", database.Name, err)
		}
		schema, err := runner.repository.PutFile(ctx, path.Join("backups", backupID, "postgres", database.Name+".schema.sql"),
			"application/sql", snapshot.SchemaPath, snapshot.SchemaDigest, snapshot.SchemaSize)
		if err != nil {
			runner.observer.DatabaseCompleted("backup", "error")
			return backupID, fmt.Errorf("store PostgreSQL schema %s: %w", database.Name, err)
		}
		runner.observer.DatabaseCompleted("backup", "success")
		if err := os.Remove(snapshot.DumpPath); err != nil {
			return backupID, fmt.Errorf("remove PostgreSQL dump workspace %s: failed", database.Name)
		}
		if err := os.Remove(snapshot.SchemaPath); err != nil {
			return backupID, fmt.Errorf("remove PostgreSQL schema workspace %s: failed", database.Name)
		}
		databases = append(databases, manifest.Database{
			Name: snapshot.Name, Engine: "postgresql", ServerVersion: snapshot.ServerVersion,
			SchemaKind: snapshot.SchemaKind, SchemaVersion: snapshot.SchemaVersion,
			SchemaChecksum: snapshot.SchemaDigest, SnapshotStarted: snapshot.StartedAt,
			SnapshotFinished: snapshot.FinishedAt, Dump: dump, Schema: schema,
		})
	}
	objects, err := runner.repository.CopyPlatformObjects(ctx, backupID, directory, runner.credentials.ObjectStores)
	for range objects {
		runner.observer.ObjectCompleted("backup", "success")
	}
	if err != nil {
		runner.observer.ObjectCompleted("backup", "error")
		return backupID, err
	}
	completedAt := runner.now()
	value := manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup", BackupID: backupID,
		State: "complete", ControllerVersion: runner.controllerVersion, ReleaseRevision: runner.releaseRevision,
		StartedAt: startedAt, CompletedAt: completedAt, ConsistencyModel: manifest.BoundedCrashConsistencyModel,
		ConsistencyStarted: startedAt, ConsistencyFinished: completedAt, Databases: databases, PlatformObjects: objects,
		DatabaseCount: len(databases), PlatformObjectCount: len(objects),
	}
	if err := value.Validate(); err != nil {
		return backupID, err
	}
	manifestReceipt, err := runner.repository.PutJSON(ctx, path.Join("backups", backupID, "manifest.json"), value)
	if err != nil {
		return backupID, fmt.Errorf("store immutable backup manifest: %w", err)
	}
	if err := runner.repository.Verify(ctx, value, manifestReceipt, directory); err != nil {
		return backupID, fmt.Errorf("independent backup readback: %w", err)
	}
	verification := manifest.Verification{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup-verification",
		BackupID: backupID, Manifest: manifestReceipt, VerifiedAt: runner.now(),
		ObjectCount: len(databases)*2 + len(objects) + 1}
	if err := runner.repository.EnsureVerification(ctx, value, manifestReceipt, verification); err != nil {
		return backupID, fmt.Errorf("store backup verification receipt: %w", err)
	}
	return backupID, nil
}

func (runner *Runner) Verify(ctx context.Context, backupID string) (resultErr error) {
	lock, err := runner.repository.AcquireOperationLock(ctx, "verify", backupID, runner.now(), operationLockTTL)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseOperationLock(ctx, lock)) }()
	value, receipt, err := runner.repository.LoadManifest(ctx, backupID)
	if err != nil {
		return err
	}
	if err := runner.repository.Verify(ctx, value, receipt, runner.workDirectory); err != nil {
		return err
	}
	verification := manifest.Verification{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup-verification",
		BackupID: backupID, Manifest: receipt, VerifiedAt: runner.now(),
		ObjectCount: len(value.Databases)*2 + len(value.PlatformObjects) + 1}
	return runner.repository.EnsureVerification(ctx, value, receipt, verification)
}

func (runner *Runner) Retain(ctx context.Context, policy Policy) (resultErr error) {
	lock, err := runner.repository.AcquireOperationLock(ctx, "retention", newBackupID(runner.now()),
		runner.now(), operationLockTTL)
	if err != nil {
		runner.observer.RetentionFinished("error", 0)
		return err
	}
	outcome := "error"
	deleted := 0
	defer func() {
		resultErr = errors.Join(resultErr, releaseOperationLock(ctx, lock))
		if resultErr != nil {
			outcome = "error"
		}
		runner.observer.RetentionFinished(outcome, deleted)
	}()
	values, drilled, err := runner.repository.Catalog(ctx)
	if err != nil {
		return err
	}
	candidates := make([]retention.Candidate, 0, len(values))
	for _, value := range values {
		candidates = append(candidates, retention.Candidate{BackupID: value.BackupID,
			CompletedAt: value.CompletedAt, Drilled: !drilled[value.BackupID].IsZero()})
	}
	selected, err := retention.Select(candidates, runner.now(), policy.MinimumAge, policy.Keep)
	if errors.Is(err, retention.ErrNoVerifiedRestorePoint) {
		outcome = "protected"
		return nil
	}
	if err != nil {
		return err
	}
	for _, backupID := range selected {
		if err := runner.repository.DeleteBackup(ctx, backupID); err != nil {
			return err
		}
		deleted++
	}
	outcome = "success"
	return nil
}

func (runner *Runner) RestoreDrill(ctx context.Context, approval configspec.RestoreApproval, targets configspec.RestoreTargets) (resultErr error) {
	defer func() {
		if resultErr == nil {
			runner.observer.RestoreFinished("success")
			return
		}
		runner.observer.RestoreFinished("error")
	}()
	targetDigest, err := configspec.FingerprintTargets(targets)
	if err != nil || targetDigest != approval.TargetSetSHA256 {
		return errors.New("restore target fingerprint does not match owner approval")
	}
	lock, err := runner.repository.AcquireOperationLock(ctx, "restore", approval.RestoreID,
		runner.now(), operationLockTTL)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, releaseOperationLock(ctx, lock)) }()
	requestDigest := manifest.RequestDigest(approval.ApprovalID, approval.RestoreID, approval.BackupID, targetDigest)
	successKey := path.Join("backups", approval.BackupID, "restore-drills", approval.RestoreID+".json")
	value, _, err := runner.repository.LoadVerifiedManifest(ctx, approval.BackupID)
	if err != nil {
		return err
	}
	if exists, err := runner.repository.Exists(ctx, successKey); err != nil {
		return err
	} else if exists {
		drill, err := runner.repository.LoadRestoreDrill(ctx, successKey, value)
		if err != nil || drill.RestoreID != approval.RestoreID || drill.ApprovalID != approval.ApprovalID ||
			drill.RequestSHA256 != requestDigest || drill.TargetSetSHA256 != targetDigest {
			return errors.New("existing restore drill does not match owner approval")
		}
		runner.observer.SetLastVerifiedRestore(drill.CompletedAt)
		return nil
	}
	failureKey := path.Join("restores", approval.RestoreID, "failure.json")
	if exists, err := runner.repository.Exists(ctx, failureKey); err != nil {
		return err
	} else if exists {
		return errors.New("restore attempt is terminally failed; a new owner approval is required")
	}
	intent := manifest.RestoreIntent{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-restore-intent",
		RestoreID: approval.RestoreID, ApprovalID: approval.ApprovalID, BackupID: approval.BackupID,
		RequestSHA256: requestDigest, TargetSetSHA256: targetDigest, CreatedAt: runner.now()}
	if err := runner.repository.EnsureRestoreIntent(ctx, intent); err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		failureContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = runner.repository.PutJSON(failureContext, failureKey, failureDocument{
			SchemaVersion: 1, Kind: "kodex-restore-failure", OperationID: approval.RestoreID,
			FailureClass: "restore", FailedAt: runner.now(),
		})
	}()
	directory := filepath.Join(runner.workDirectory, "restore-"+approval.RestoreID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New("create restore workspace")
	}
	defer os.RemoveAll(directory)
	targetByName := make(map[string]configspec.RestoreDatabase, len(targets.Databases))
	for _, target := range targets.Databases {
		targetByName[target.Name] = target
	}
	if len(value.Databases) != len(targetByName) {
		return errors.New("restore database target set is incomplete")
	}
	readbacks := make([]manifest.RestoreDatabase, 0, len(value.Databases))
	for _, database := range value.Databases {
		target, targetExists := targetByName[database.Name]
		if !targetExists {
			return errors.New("restore database target does not match backup manifest")
		}
		source := configspec.Database{SchemaKind: database.SchemaKind}
		if database.SchemaKind == "declared" {
			source.DeclaredSchemaVersion = strings.TrimPrefix(database.SchemaVersion, "declared:")
		}
		dumpPath := filepath.Join(directory, database.Name+".dump")
		if err := runner.repository.Download(ctx, database.Dump, dumpPath); err != nil {
			return err
		}
		readback, err := runner.postgres.Restore(ctx, target, source, dumpPath, database.SchemaVersion)
		if err != nil {
			runner.observer.DatabaseCompleted("restore", "error")
			return err
		}
		runner.observer.DatabaseCompleted("restore", "success")
		if err := os.Remove(dumpPath); err != nil {
			return errors.New("remove restored PostgreSQL dump workspace")
		}
		readbacks = append(readbacks, manifest.RestoreDatabase{Name: readback.Name,
			SchemaVersion: readback.SchemaVersion, TargetDigest: readback.TargetDigest})
	}
	objects, err := runner.repository.RestorePlatformObjects(ctx, value, targets.ObjectStore, directory)
	for range objects {
		runner.observer.ObjectCompleted("restore", "success")
	}
	if err != nil {
		runner.observer.ObjectCompleted("restore", "error")
		return err
	}
	sort.Slice(readbacks, func(i, j int) bool { return readbacks[i].Name < readbacks[j].Name })
	drill := manifest.RestoreDrill{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-restore-drill",
		RestoreID: approval.RestoreID, ApprovalID: approval.ApprovalID, BackupID: approval.BackupID,
		RequestSHA256: requestDigest, TargetSetSHA256: targetDigest, CompletedAt: runner.now(),
		Databases: readbacks, Objects: objects}
	if _, err := runner.repository.PutJSON(ctx, successKey, drill); err != nil {
		return err
	}
	runner.observer.SetLastVerifiedRestore(drill.CompletedAt)
	return nil
}

func (runner *Runner) Due(ctx context.Context, interval time.Duration) (bool, error) {
	values, _, err := runner.repository.Catalog(ctx)
	if err != nil {
		return false, err
	}
	if len(values) == 0 {
		return true, nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CompletedAt.After(values[j].CompletedAt) })
	return !runner.now().Before(values[0].CompletedAt.Add(interval)), nil
}

func (runner *Runner) Readback(ctx context.Context) (string, time.Time, time.Time, error) {
	values, drilled, err := runner.repository.Catalog(ctx)
	if err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	var backupID string
	var lastBackup, lastRestore time.Time
	for _, value := range values {
		if value.CompletedAt.After(lastBackup) {
			backupID = value.BackupID
			lastBackup = value.CompletedAt
		}
		if drilled[value.BackupID].After(lastRestore) {
			lastRestore = drilled[value.BackupID]
		}
	}
	return backupID, lastBackup, lastRestore, nil
}

func newBackupID(now time.Time) string {
	id := uuid.New()
	return now.UTC().Format("20060102T150405Z") + "-" + fmt.Sprintf("%x", id[:8])
}

func releaseOperationLock(ctx context.Context, lock *s3backup.OperationLock) error {
	releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationLockReleaseTimeout)
	defer cancel()
	return retryOperationLockRelease(releaseContext, operationLockReleaseRetryInterval, lock.Release)
}

func retryOperationLockRelease(ctx context.Context, retryInterval time.Duration,
	release func(context.Context) error) error {
	if release == nil || retryInterval <= 0 {
		return errors.New("backup repository operation lock release retry is invalid")
	}
	var lastErr error
	for {
		if lastErr != nil && ctx.Err() != nil {
			return lastErr
		}
		if err := release(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return lastErr
		case <-timer.C:
		}
	}
}
