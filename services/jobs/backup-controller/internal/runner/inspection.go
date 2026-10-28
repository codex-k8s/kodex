package runner

import (
	"context"
	"errors"
	"math"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/manifest"
)

// MetadataDatabase exposes schema identity without rows, dump bodies or target credentials.
// MetadataDatabase раскрывает идентичность схемы без строк, содержимого дампа и реквизитов цели.
type MetadataDatabase struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schemaVersion"`
	SchemaSHA256  string `json:"schemaSha256"`
}

// BackupMetadata is a bounded safe projection of one independently verified manifest.
// BackupMetadata представляет ограниченную безопасную проекцию подтверждённого манифеста.
type BackupMetadata struct {
	BackupID              string             `json:"backupId"`
	State                 string             `json:"state"`
	ManifestSHA256        string             `json:"manifestSha256"`
	ManifestVersion       string             `json:"manifestVersion"`
	ControllerVersion     string             `json:"controllerVersion"`
	ReleaseRevision       string             `json:"releaseRevision"`
	StartedAt             time.Time          `json:"startedAt"`
	CompletedAt           time.Time          `json:"completedAt"`
	ConsistencyStartedAt  time.Time          `json:"consistencyStartedAt"`
	ConsistencyFinishedAt time.Time          `json:"consistencyFinishedAt"`
	Databases             []MetadataDatabase `json:"databases"`
	ObjectCount           int                `json:"objectCount"`
	PayloadBytes          int64              `json:"payloadBytes"`
}

// RestoreMetadata binds the safe backup projection to exact immutable approval and target receipts without object keys or credentials.
// RestoreMetadata связывает безопасную проекцию копии с точными подтверждением и целью без ключей объектов и реквизитов.
type RestoreMetadata struct {
	Backup          BackupMetadata `json:"backup"`
	RestoreID       string         `json:"restoreId"`
	ApprovalID      string         `json:"approvalId"`
	RequestSHA256   string         `json:"requestSha256"`
	TargetSetSHA256 string         `json:"targetSetSha256"`
	CompletedAt     time.Time      `json:"completedAt"`
	DatabaseCount   int            `json:"databaseCount"`
	ObjectCount     int            `json:"objectCount"`
}

// ListVerifiedMetadata returns a complete bounded catalog with only safe IDs; an incomplete or corrupt receipt returns no partial catalog.
// ListVerifiedMetadata возвращает полный ограниченный каталог безопасных идентификаторов; неполная или повреждённая квитанция закрывает весь результат.
func (runner *Runner) ListVerifiedMetadata(ctx context.Context) ([]string, error) {
	values, err := runner.repository.ListVerified(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.BackupID)
	}
	return result, nil
}

// InspectBackup verifies exact manifest/verification binding before returning allowlisted metadata; it does not perform another payload verify or create a receipt.
// InspectBackup проверяет связь манифеста и verification до выдачи разрешённых метаданных; повторной проверки payload и записи квитанции нет.
func (runner *Runner) InspectBackup(ctx context.Context, backupID string) (BackupMetadata, error) {
	value, receipt, err := runner.repository.LoadVerifiedManifest(ctx, backupID)
	if err != nil {
		return BackupMetadata{}, err
	}
	return projectBackupMetadata(value, receipt)
}

// InspectRestore requires an exact expected approval and target digest as well as the existing verified manifest/drill validators; mismatch never becomes a partial success.
// InspectRestore требует точных ожидаемых подтверждения и дайджеста цели вместе с проверкой манифеста и drill; несовпадение не становится частичным успехом.
func (runner *Runner) InspectRestore(ctx context.Context, backupID, restoreID, approvalID, targetSHA256 string) (RestoreMetadata, error) {
	value, receipt, err := runner.repository.LoadVerifiedManifest(ctx, backupID)
	if err != nil {
		return RestoreMetadata{}, err
	}
	drill, err := runner.repository.LoadRestoreDrill(ctx, path.Join("backups", backupID, "restore-drills", restoreID+".json"), value)
	if err != nil {
		return RestoreMetadata{}, err
	}
	return projectRestoreMetadata(value, receipt, drill, restoreID, approvalID, targetSHA256)
}

// projectRestoreMetadata validates receipt structure and exact expected owner pins before projecting bounded metadata.
// projectRestoreMetadata проверяет структуру квитанции и точные ожидаемые owner pins до безопасной проекции.
func projectRestoreMetadata(value manifest.Manifest, receipt manifest.Receipt, drill manifest.RestoreDrill, restoreID, approvalID, targetSHA256 string) (RestoreMetadata, error) {
	if err := drill.Validate(value); err != nil {
		return RestoreMetadata{}, err
	}
	if drill.RestoreID != restoreID || drill.ApprovalID != approvalID || drill.TargetSetSHA256 != targetSHA256 || drill.RequestSHA256 != manifest.RequestDigest(approvalID, restoreID, value.BackupID, targetSHA256) {
		return RestoreMetadata{}, errors.New("restore metadata owner binding mismatches")
	}
	backup, err := projectBackupMetadata(value, receipt)
	if err != nil {
		return RestoreMetadata{}, err
	}
	return RestoreMetadata{Backup: backup, RestoreID: drill.RestoreID, ApprovalID: drill.ApprovalID, RequestSHA256: drill.RequestSHA256, TargetSetSHA256: drill.TargetSetSHA256, CompletedAt: drill.CompletedAt, DatabaseCount: len(drill.Databases), ObjectCount: len(drill.Objects)}, nil
}

// projectBackupMetadata strips object keys and bounds total arithmetic after manifest validation; bytes count dump/schema and backed-up object bodies once each.
// projectBackupMetadata удаляет ключи объектов и ограничивает арифметику после проверки манифеста; размер учитывает дамп, схему и тела объектов один раз.
func projectBackupMetadata(value manifest.Manifest, receipt manifest.Receipt) (BackupMetadata, error) {
	result := BackupMetadata{BackupID: value.BackupID, State: "VERIFIED", ManifestSHA256: receipt.ChecksumSHA256, ManifestVersion: receipt.VersionID, StartedAt: value.StartedAt, CompletedAt: value.CompletedAt, ConsistencyStartedAt: value.ConsistencyStarted, ConsistencyFinishedAt: value.ConsistencyFinished, Databases: make([]MetadataDatabase, 0, len(value.Databases)), ObjectCount: value.PlatformObjectCount}
	if err := value.Validate(); err != nil {
		return BackupMetadata{}, err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`).MatchString(value.ControllerVersion) ||
		!regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(value.ReleaseRevision) || value.ReleaseRevision == "sha256:"+strings.Repeat("0", 64) {
		return BackupMetadata{}, errors.New("backup metadata release identity is invalid")
	}
	result.ControllerVersion, result.ReleaseRevision = value.ControllerVersion, value.ReleaseRevision
	sizes := make([]int64, 0, len(value.Databases)*2+len(value.PlatformObjects))
	for _, db := range value.Databases {
		result.Databases = append(result.Databases, MetadataDatabase{Name: db.Name, SchemaVersion: db.SchemaVersion, SchemaSHA256: db.SchemaChecksum})
		sizes = append(sizes, db.Dump.SizeBytes, db.Schema.SizeBytes)
	}
	for _, object := range value.PlatformObjects {
		sizes = append(sizes, object.Backup.SizeBytes)
	}
	for _, size := range sizes {
		if size < 0 || size > math.MaxInt64-result.PayloadBytes {
			return BackupMetadata{}, errors.New("backup metadata byte count exceeds bounds")
		}
		result.PayloadBytes += size
	}
	return result, nil
}
