// Package manifest defines immutable backup, verification, restore intent and drill receipts.
// RU: Package manifest определяет неизменяемые квитанции копирования, проверки, намерения восстановления и пробного восстановления.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	// SchemaVersion is the accepted wire version for backup and restore receipts.
	// RU: SchemaVersion задаёт принимаемую wire-версию квитанций копирования и восстановления.
	SchemaVersion = 2
	// BoundedCrashConsistencyModel names the bounded, non-transactional snapshot guarantee.
	// RU: BoundedCrashConsistencyModel обозначает ограниченную, но не транзакционную согласованность снимка.
	BoundedCrashConsistencyModel = "BOUNDED_CRASH_CONSISTENT"
)

var (
	// backupIDPattern binds receipts to a UTC timestamp and fixed random suffix.
	// RU: backupIDPattern связывает квитанции с UTC-временем и фиксированным случайным суффиксом.
	backupIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$`)
	// operationIDPattern restricts logical restore and approval identifiers.
	// RU: operationIDPattern ограничивает логические идентификаторы восстановления и подтверждения.
	operationIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	// serverVersionPattern accepts the declared PostgreSQL major and minor version format.
	// RU: serverVersionPattern допускает объявленный формат старшей и младшей версии PostgreSQL.
	serverVersionPattern = regexp.MustCompile(`^(17|18)[0-9]{4}$`)
	// gooseVersionPattern recognizes a numeric Goose migration version.
	// RU: gooseVersionPattern распознаёт числовую версию миграции Goose.
	gooseVersionPattern = regexp.MustCompile(`^goose:[0-9]+$`)
)

// Receipt identifies an exact versioned object and its size and digest for authoritative readback.
// RU: Receipt определяет точную версию объекта, размер и дайджест для авторитетного перечитывания.
type Receipt struct {
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	VersionID      string `json:"versionId"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksumSha256"`
	SizeBytes      int64  `json:"sizeBytes"`
}

// Database records a source schema and the exact dump and schema object receipts within the consistency window.
// RU: Database фиксирует исходную схему и точные квитанции объектов дампа и схемы в окне согласованности.
type Database struct {
	Name             string    `json:"name"`
	Engine           string    `json:"engine"`
	ServerVersion    string    `json:"serverVersion"`
	SchemaKind       string    `json:"schemaKind"`
	SchemaVersion    string    `json:"schemaVersion"`
	SchemaChecksum   string    `json:"schemaChecksumSha256"`
	SnapshotStarted  time.Time `json:"snapshotStartedAt"`
	SnapshotFinished time.Time `json:"snapshotFinishedAt"`
	Dump             Receipt   `json:"dump"`
	Schema           Receipt   `json:"schema"`
}

// PlatformObject pairs one exact source object version with its backup copy receipt.
// RU: PlatformObject связывает точную исходную версию объекта с квитанцией её резервной копии.
type PlatformObject struct {
	StoreName string  `json:"storeName"`
	Source    Receipt `json:"source"`
	Backup    Receipt `json:"backup"`
}

// Manifest is the completed backup snapshot with bounded timing, source counts and exact object receipts.
// RU: Manifest описывает завершённый снимок с ограниченным окном времени, числом источников и точными квитанциями объектов.
type Manifest struct {
	SchemaVersion       int              `json:"schemaVersion"`
	Kind                string           `json:"kind"`
	BackupID            string           `json:"backupId"`
	State               string           `json:"state"`
	ControllerVersion   string           `json:"controllerVersion"`
	ReleaseRevision     string           `json:"releaseRevision"`
	StartedAt           time.Time        `json:"startedAt"`
	CompletedAt         time.Time        `json:"completedAt"`
	ConsistencyModel    string           `json:"consistencyModel"`
	ConsistencyStarted  time.Time        `json:"consistencyWindowStartedAt"`
	ConsistencyFinished time.Time        `json:"consistencyWindowFinishedAt"`
	Databases           []Database       `json:"databases"`
	PlatformObjects     []PlatformObject `json:"platformObjects"`
	DatabaseCount       int              `json:"databaseCount"`
	PlatformObjectCount int              `json:"platformObjectCount"`
}

// Verification attests to readback of one completed manifest and the expected number of objects.
// RU: Verification удостоверяет перечитывание одного завершённого манифеста и ожидаемого числа объектов.
type Verification struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	BackupID      string    `json:"backupId"`
	Manifest      Receipt   `json:"manifest"`
	VerifiedAt    time.Time `json:"verifiedAt"`
	ObjectCount   int       `json:"objectCount"`
}

// RestoreDatabase records one restored database's schema version and target readback digest.
// RU: RestoreDatabase фиксирует версию схемы и дайджест перечитывания одной восстановленной БД.
type RestoreDatabase struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schemaVersion"`
	TargetDigest  string `json:"targetDigest"`
}

// RestoreDrill records completed restore readback under one approval, request and target-set digest.
// RU: RestoreDrill фиксирует завершённое пробное восстановление под одним подтверждением и дайджестами запроса и целей.
type RestoreDrill struct {
	SchemaVersion   int               `json:"schemaVersion"`
	Kind            string            `json:"kind"`
	RestoreID       string            `json:"restoreId"`
	ApprovalID      string            `json:"approvalId"`
	BackupID        string            `json:"backupId"`
	RequestSHA256   string            `json:"requestSha256"`
	TargetSetSHA256 string            `json:"targetSetSha256"`
	CompletedAt     time.Time         `json:"completedAt"`
	Databases       []RestoreDatabase `json:"databases"`
	Objects         []Receipt         `json:"objects"`
}

// RestoreIntent binds the immutable restore command to approval, backup, request and target set before effects.
// RU: RestoreIntent связывает неизменяемую команду восстановления с подтверждением, копией, запросом и набором целей до побочных эффектов.
type RestoreIntent struct {
	SchemaVersion   int       `json:"schemaVersion"`
	Kind            string    `json:"kind"`
	RestoreID       string    `json:"restoreId"`
	ApprovalID      string    `json:"approvalId"`
	BackupID        string    `json:"backupId"`
	RequestSHA256   string    `json:"requestSha256"`
	TargetSetSHA256 string    `json:"targetSetSha256"`
	CreatedAt       time.Time `json:"createdAt"`
}

// Validate rejects incomplete manifests, invalid consistency timing, duplicate sources and non-versioned receipts.
// RU: Validate отклоняет неполные манифесты, ошибочное окно согласованности, дубли источников и квитанции без версии.
func (value Manifest) Validate() error {
	if value.SchemaVersion != SchemaVersion || value.Kind != "kodex-backup" ||
		!backupIDPattern.MatchString(value.BackupID) || value.State != "complete" ||
		value.ControllerVersion == "" || value.ReleaseRevision == "" || value.StartedAt.IsZero() ||
		value.CompletedAt.Before(value.StartedAt) || value.ConsistencyModel != BoundedCrashConsistencyModel ||
		value.ConsistencyStarted.IsZero() || value.ConsistencyStarted.Before(value.StartedAt) ||
		value.ConsistencyFinished.Before(value.ConsistencyStarted) || value.CompletedAt.Before(value.ConsistencyFinished) ||
		len(value.Databases) == 0 ||
		value.DatabaseCount != len(value.Databases) ||
		value.PlatformObjectCount != len(value.PlatformObjects) {
		return errors.New("backup manifest is invalid")
	}
	databaseNames := map[string]struct{}{}
	for _, database := range value.Databases {
		if !operationIDPattern.MatchString(database.Name) || database.Engine != "postgresql" ||
			!serverVersionPattern.MatchString(database.ServerVersion) ||
			(database.SchemaKind != "goose" && database.SchemaKind != "declared") ||
			!validSchemaVersion(database.SchemaKind, database.SchemaVersion) ||
			!validDigest(database.SchemaChecksum) || database.SnapshotStarted.IsZero() ||
			database.SnapshotFinished.Before(database.SnapshotStarted) ||
			database.SnapshotStarted.Before(value.ConsistencyStarted) ||
			value.ConsistencyFinished.Before(database.SnapshotFinished) ||
			!database.Dump.Valid() || !database.Schema.Valid() {
			return errors.New("backup database manifest is invalid")
		}
		if _, exists := databaseNames[database.Name]; exists {
			return errors.New("backup database manifest is duplicated")
		}
		databaseNames[database.Name] = struct{}{}
	}
	objectKeys := map[string]struct{}{}
	for _, object := range value.PlatformObjects {
		if !operationIDPattern.MatchString(object.StoreName) || !object.Source.Valid() || !object.Backup.Valid() {
			return errors.New("backup object manifest is invalid")
		}
		key := object.StoreName + "\x00" + object.Source.Key + "\x00" + object.Source.VersionID
		if _, exists := objectKeys[key]; exists {
			return errors.New("backup object manifest is duplicated")
		}
		objectKeys[key] = struct{}{}
	}
	return nil
}

// Validate checks that a verification receipt matches the exact manifest receipt and all expected objects.
// RU: Validate проверяет соответствие квитанции проверки точному манифесту и всем ожидаемым объектам.
func (value Verification) Validate(backup Manifest, receipt Receipt) error {
	if value.SchemaVersion != SchemaVersion || value.Kind != "kodex-backup-verification" ||
		value.BackupID != backup.BackupID || value.Manifest != receipt || value.VerifiedAt.IsZero() ||
		value.VerifiedAt.Before(backup.CompletedAt) ||
		value.ObjectCount != len(backup.Databases)*2+len(backup.PlatformObjects)+1 {
		return errors.New("backup verification receipt is invalid")
	}
	return nil
}

// Matches checks an immutable, well-formed restore intent against the approved command fields.
// RU: Matches сверяет неизменяемое корректное намерение восстановления с полями подтверждённой команды.
func (value RestoreIntent) Matches(expected RestoreIntent) bool {
	return value.SchemaVersion == SchemaVersion && value.Kind == "kodex-restore-intent" &&
		operationIDPattern.MatchString(value.RestoreID) && operationIDPattern.MatchString(value.ApprovalID) &&
		backupIDPattern.MatchString(value.BackupID) && validDigest(value.RequestSHA256) &&
		validDigest(value.TargetSetSHA256) && !value.CreatedAt.IsZero() &&
		value.RestoreID == expected.RestoreID && value.ApprovalID == expected.ApprovalID &&
		value.BackupID == expected.BackupID && value.RequestSHA256 == expected.RequestSHA256 &&
		value.TargetSetSHA256 == expected.TargetSetSHA256
}

// Validate checks the drill's approval bindings, database schemas and object readback against the backup.
// RU: Validate проверяет привязки подтверждения, схемы БД и перечитанные объекты пробного восстановления по копии.
func (value RestoreDrill) Validate(backup Manifest) error {
	if value.SchemaVersion != SchemaVersion || value.Kind != "kodex-restore-drill" ||
		!operationIDPattern.MatchString(value.RestoreID) || !operationIDPattern.MatchString(value.ApprovalID) ||
		value.BackupID != backup.BackupID || !validDigest(value.RequestSHA256) ||
		!validDigest(value.TargetSetSHA256) || value.CompletedAt.IsZero() ||
		len(value.Databases) != len(backup.Databases) || len(value.Objects) != len(backup.PlatformObjects) {
		return errors.New("restore drill receipt is invalid")
	}
	backupDatabases := make(map[string]string, len(backup.Databases))
	for _, database := range backup.Databases {
		backupDatabases[database.Name] = database.SchemaVersion
	}
	seen := make(map[string]struct{}, len(value.Databases))
	for _, database := range value.Databases {
		if backupDatabases[database.Name] != database.SchemaVersion || !validDigest(database.TargetDigest) {
			return errors.New("restore drill database readback is invalid")
		}
		if _, exists := seen[database.Name]; exists {
			return errors.New("restore drill database readback is duplicated")
		}
		seen[database.Name] = struct{}{}
	}
	for index, object := range value.Objects {
		if !object.Valid() || object.ChecksumSHA256 != backup.PlatformObjects[index].Source.ChecksumSHA256 ||
			object.SizeBytes != backup.PlatformObjects[index].Source.SizeBytes {
			return errors.New("restore drill object readback is invalid")
		}
	}
	return nil
}

// Valid requires an exact S3 version, nonempty locator and ETag, valid SHA-256 and nonnegative size.
// RU: Valid требует точную версию S3, непустые адрес и ETag, корректный SHA-256 и неотрицательный размер.
func (receipt Receipt) Valid() bool {
	return receipt.Bucket != "" && receipt.Key != "" && receipt.VersionID != "" && receipt.ETag != "" &&
		validDigest(receipt.ChecksumSHA256) && receipt.SizeBytes >= 0
}

// Marshal encodes one immutable receipt and returns the SHA-256 digest of those exact JSON bytes.
// RU: Marshal кодирует неизменяемую квитанцию и возвращает SHA-256 ровно этих JSON-байтов.
func Marshal(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", errors.New("encode immutable manifest")
	}
	digest := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// RequestDigest hashes named approval, restore, backup and target fields so swapped positions cannot share a digest.
// RU: RequestDigest хеширует именованные поля подтверждения, восстановления, копии и целей, различая переставленные значения.
func RequestDigest(approvalID, restoreID, backupID, targetDigest string) string {
	payload, _ := json.Marshal(struct {
		ApprovalID   string `json:"approvalId"`
		RestoreID    string `json:"restoreId"`
		BackupID     string `json:"backupId"`
		TargetDigest string `json:"targetSetSha256"`
	}{approvalID, restoreID, backupID, targetDigest})
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// validDigest accepts only a full hexadecimal SHA-256 with its algorithm prefix.
// RU: validDigest принимает лишь полный шестнадцатеричный SHA-256 с префиксом алгоритма.
func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

// validSchemaVersion distinguishes a numeric Goose version from a nonempty declared version.
// RU: validSchemaVersion отличает числовую версию Goose от непустой объявленной версии.
func validSchemaVersion(kind, value string) bool {
	if kind == "goose" {
		return gooseVersionPattern.MatchString(value)
	}
	return kind == "declared" && strings.TrimSpace(strings.TrimPrefix(value, "declared:")) != "" &&
		strings.HasPrefix(value, "declared:")
}
