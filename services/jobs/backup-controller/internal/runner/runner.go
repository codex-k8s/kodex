// Package runner orchestrates backup, independent verification, retention and owner-gated restore drills.
// RU: Package runner координирует копирование, независимую проверку, хранение и пробное восстановление с подтверждением владельца.
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

// Observer records completed business effects and authoritative readback timestamps.
// RU: Observer учитывает завершённые бизнес-действия и времена авторитетного перечитывания.
type Observer interface {
	// BackupFinished records the final outcome of one backup attempt.
	// RU: BackupFinished фиксирует итог одной попытки копирования.
	BackupFinished(outcome string)
	// DatabaseCompleted counts a completed backup or restore database action.
	// RU: DatabaseCompleted учитывает завершённое действие с БД при копировании либо восстановлении.
	DatabaseCompleted(operation, outcome string)
	// ObjectCompleted counts a completed immutable object action, including partial work before failure.
	// RU: ObjectCompleted учитывает завершённое действие с неизменяемым объектом, включая частичный успех до ошибки.
	ObjectCompleted(operation, outcome string)
	// RetentionFinished records one cleanup evaluation and its already-deleted count.
	// RU: RetentionFinished учитывает одну оценку хранения и число уже удалённых копий.
	RetentionFinished(outcome string, deleted int)
	// RestoreFinished records the final outcome of one owner-gated restore drill.
	// RU: RestoreFinished фиксирует итог одного подтверждённого пробного восстановления.
	RestoreFinished(outcome string)
	// SetLastSuccessfulBackup publishes the most recent independently verified backup time.
	// RU: SetLastSuccessfulBackup публикует время последней независимо проверенной копии.
	SetLastSuccessfulBackup(time.Time)
	// SetLastVerifiedRestore publishes the latest successful restore-drill time.
	// RU: SetLastVerifiedRestore публикует время последнего успешного пробного восстановления.
	SetLastVerifiedRestore(time.Time)
}

// Runner binds PostgreSQL, versioned S3, protected credentials, workspace and a business observer.
// RU: Runner связывает PostgreSQL, версионированный S3, защищённые реквизиты, workspace и бизнес-наблюдатель.
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

// Policy sets a minimum backup age and count to keep before retention may delete anything.
// RU: Policy задаёт минимальный возраст и количество сохраняемых копий до возможного удаления.
type Policy struct {
	MinimumAge time.Duration
	Keep       int
}

const (
	// operationLockTTL bounds the controller's cross-operation S3 lock lifetime.
	// RU: operationLockTTL ограничивает срок общей S3-блокировки операций контроллера.
	operationLockTTL = 25 * time.Hour
	// operationLockReleaseTimeout bounds detached cleanup after cancellation of the parent operation.
	// RU: operationLockReleaseTimeout ограничивает независимую очистку после отмены основной операции.
	operationLockReleaseTimeout = 90 * time.Second
	// operationLockReleaseRetryInterval spaces bounded lock-release retries.
	// RU: operationLockReleaseRetryInterval задаёт паузу между ограниченными попытками освобождения блокировки.
	operationLockReleaseRetryInterval = time.Second
)

// attemptDocument records an immutable start marker for an in-progress backup.
// RU: attemptDocument фиксирует неизменяемую метку начала выполняемой копии.
type attemptDocument struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	BackupID      string    `json:"backupId"`
	StartedAt     time.Time `json:"startedAt"`
}

// failureDocument records a coarse failure class without exposing source credentials or raw errors.
// RU: failureDocument фиксирует общий класс отказа без раскрытия исходных реквизитов и сырых ошибок.
type failureDocument struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	OperationID   string    `json:"operationId"`
	FailureClass  string    `json:"failureClass"`
	FailedAt      time.Time `json:"failedAt"`
}

// New rejects missing dependencies and creates one runner with a UTC clock for operation receipts.
// RU: New отклоняет отсутствие зависимостей и создаёт runner с UTC-часами для квитанций операций.
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

// Check validates the destination and all declared S3 and PostgreSQL sources before work.
// RU: Check проверяет назначение и все объявленные S3/PostgreSQL-источники до начала работы.
func (runner *Runner) Check(ctx context.Context) error {
	if err := runner.repository.Check(ctx, runner.credentials.ObjectStores); err != nil {
		return err
	}
	return runner.postgres.Check(ctx, runner.credentials.Databases)
}

// Backup locks the repository, snapshots each source, copies exact objects and publishes verification after readback.
// RU: Backup блокирует репозиторий, снимает копии источников и публикует проверку только после точного перечитывания.
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

// Verify rechecks a manifest and all versioned payloads before ensuring its independent verification receipt.
// RU: Verify повторно сверяет манифест и все версионированные данные до записи независимой квитанции проверки.
func (runner *Runner) Verify(ctx context.Context, backupID string) (resultErr error) {
	lock, err := runner.repository.AcquireOperationLock(ctx, "verify", backupID, runner.now(), operationLockTTL)
	if err != nil {
		return verificationFailure("lock_acquire", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, verificationFailure("lock_release", releaseOperationLock(ctx, lock)))
	}()
	value, receipt, err := runner.repository.LoadManifest(ctx, backupID)
	if err != nil {
		return verificationFailure("manifest_load", err)
	}
	if err := runner.repository.Verify(ctx, value, receipt, runner.workDirectory); err != nil {
		return verificationFailure("payload_verify", err)
	}
	verification := manifest.Verification{SchemaVersion: manifest.SchemaVersion, Kind: "kodex-backup-verification",
		BackupID: backupID, Manifest: receipt, VerifiedAt: runner.now(),
		ObjectCount: len(value.Databases)*2 + len(value.PlatformObjects) + 1}
	return verificationFailure("receipt_ensure", runner.repository.EnsureVerification(ctx, value, receipt, verification))
}

// Retain deletes only policy-selected backups after a successful restore point exists, counting partial deletions.
// RU: Retain удаляет лишь выбранные политикой копии при наличии проверенной точки восстановления и учитывает частичные удаления.
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

// RestoreDrill binds targets to owner approval, refuses prior terminal failures and verifies isolated restored data.
// RU: RestoreDrill связывает цели с подтверждением владельца, отклоняет прежние terminal-отказы и проверяет восстановленные данные.
func (runner *Runner) RestoreDrill(ctx context.Context, approval configspec.RestoreApproval, targets configspec.RestoreTargets) (resultErr error) {
	if err := configspec.RequireFreshRestoreApproval(approval, runner.now()); err != nil {
		return err
	}
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

// Due compares the newest verified backup completion with the configured interval.
// RU: Due сравнивает завершение последней проверенной копии с заданным интервалом.
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

// Readback derives latest backup and drill times from the verified repository catalog.
// RU: Readback выводит времена последних копии и восстановления из проверенного каталога репозитория.
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

// newBackupID combines a UTC timestamp with a random eight-byte UUID prefix.
// RU: newBackupID соединяет UTC-время со случайными первыми восемью байтами UUID.
func newBackupID(now time.Time) string {
	id := uuid.New()
	return now.UTC().Format("20060102T150405Z") + "-" + fmt.Sprintf("%x", id[:8])
}

// releaseOperationLock detaches bounded lock cleanup from a potentially cancelled operation context.
// RU: releaseOperationLock отделяет ограниченную очистку блокировки от возможной отмены контекста операции.
func releaseOperationLock(ctx context.Context, lock *s3backup.OperationLock) error {
	releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationLockReleaseTimeout)
	defer cancel()
	return retryOperationLockRelease(releaseContext, operationLockReleaseRetryInterval, lock.Release)
}

// retryOperationLockRelease repeats release until success or deadline and preserves the final observed error.
// RU: retryOperationLockRelease повторяет освобождение до успеха либо срока и сохраняет последнюю наблюдаемую ошибку.
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
