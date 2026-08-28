// Package manifest определяет immutable wire format backup и restore drill.
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

const SchemaVersion = 1

var (
	backupIDPattern      = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[a-f0-9]{16}$`)
	operationIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	serverVersionPattern = regexp.MustCompile(`^(17|18)[0-9]{4}$`)
	gooseVersionPattern  = regexp.MustCompile(`^goose:[0-9]+$`)
)

type Receipt struct {
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	VersionID      string `json:"versionId"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksumSha256"`
	SizeBytes      int64  `json:"sizeBytes"`
}

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

type PlatformObject struct {
	StoreName string  `json:"storeName"`
	Source    Receipt `json:"source"`
	Backup    Receipt `json:"backup"`
}

type Manifest struct {
	SchemaVersion       int              `json:"schemaVersion"`
	Kind                string           `json:"kind"`
	BackupID            string           `json:"backupId"`
	State               string           `json:"state"`
	ControllerVersion   string           `json:"controllerVersion"`
	ReleaseRevision     string           `json:"releaseRevision"`
	StartedAt           time.Time        `json:"startedAt"`
	CompletedAt         time.Time        `json:"completedAt"`
	Databases           []Database       `json:"databases"`
	PlatformObjects     []PlatformObject `json:"platformObjects"`
	DatabaseCount       int              `json:"databaseCount"`
	PlatformObjectCount int              `json:"platformObjectCount"`
}

type Verification struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	BackupID      string    `json:"backupId"`
	Manifest      Receipt   `json:"manifest"`
	VerifiedAt    time.Time `json:"verifiedAt"`
	ObjectCount   int       `json:"objectCount"`
}

type RestoreDatabase struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schemaVersion"`
	TargetDigest  string `json:"targetDigest"`
}

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

func (value Manifest) Validate() error {
	if value.SchemaVersion != SchemaVersion || value.Kind != "kodex-backup" ||
		!backupIDPattern.MatchString(value.BackupID) || value.State != "complete" ||
		value.ControllerVersion == "" || value.ReleaseRevision == "" || value.StartedAt.IsZero() ||
		value.CompletedAt.Before(value.StartedAt) || len(value.Databases) == 0 ||
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

func (value Verification) Validate(backup Manifest, receipt Receipt) error {
	if value.SchemaVersion != SchemaVersion || value.Kind != "kodex-backup-verification" ||
		value.BackupID != backup.BackupID || value.Manifest != receipt || value.VerifiedAt.IsZero() ||
		value.VerifiedAt.Before(backup.CompletedAt) ||
		value.ObjectCount != len(backup.Databases)*2+len(backup.PlatformObjects)+1 {
		return errors.New("backup verification receipt is invalid")
	}
	return nil
}

func (value RestoreIntent) Matches(expected RestoreIntent) bool {
	return value.SchemaVersion == SchemaVersion && value.Kind == "kodex-restore-intent" &&
		operationIDPattern.MatchString(value.RestoreID) && operationIDPattern.MatchString(value.ApprovalID) &&
		backupIDPattern.MatchString(value.BackupID) && validDigest(value.RequestSHA256) &&
		validDigest(value.TargetSetSHA256) && !value.CreatedAt.IsZero() &&
		value.RestoreID == expected.RestoreID && value.ApprovalID == expected.ApprovalID &&
		value.BackupID == expected.BackupID && value.RequestSHA256 == expected.RequestSHA256 &&
		value.TargetSetSHA256 == expected.TargetSetSHA256
}

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

func (receipt Receipt) Valid() bool {
	return receipt.Bucket != "" && receipt.Key != "" && receipt.VersionID != "" && receipt.ETag != "" &&
		validDigest(receipt.ChecksumSHA256) && receipt.SizeBytes >= 0
}

func Marshal(value any) ([]byte, string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, "", errors.New("encode immutable manifest")
	}
	digest := sha256.Sum256(payload)
	return payload, "sha256:" + hex.EncodeToString(digest[:]), nil
}

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

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validSchemaVersion(kind, value string) bool {
	if kind == "goose" {
		return gooseVersionPattern.MatchString(value)
	}
	return kind == "declared" && strings.TrimSpace(strings.TrimPrefix(value, "declared:")) != "" &&
		strings.HasPrefix(value, "declared:")
}
