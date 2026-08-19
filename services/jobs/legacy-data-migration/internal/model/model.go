// Package model содержит безопасный отчёт и внутреннюю модель cutover-плана.
package model

import "time"

type SnapshotRow struct {
	Table   string
	Payload []byte
}

type Counts struct {
	Source  map[string]uint64 `json:"source"`
	Mapped  map[string]uint64 `json:"mapped"`
	Archive map[string]uint64 `json:"archive"`
}

type Plan struct {
	SchemaVersion         string            `json:"schemaVersion"`
	PlanID                string            `json:"planId"`
	SourceSHA256          string            `json:"sourceSha256"`
	TargetSHA256          string            `json:"targetSha256,omitempty"`
	MappingSHA256         string            `json:"mappingSha256"`
	MaterializationSHA256 string            `json:"materializationSha256"`
	MaterializationCount  uint64            `json:"materializationCount"`
	Counts                Counts            `json:"counts"`
	Violations            map[string]uint64 `json:"violations"`
	PlanSHA256            string            `json:"planSha256"`
	BackupSHA256          string            `json:"backupSha256,omitempty"`
	ManifestSHA256        string            `json:"manifestSha256,omitempty"`
	CutoverState          string            `json:"cutoverState,omitempty"`
	OwnerRequestSHA256    string            `json:"ownerRequestSha256"`
}

func (plan Plan) Ready() bool {
	for _, count := range plan.Violations {
		if count != 0 {
			return false
		}
	}
	return true
}

type Manifest struct {
	SchemaVersion string            `json:"schemaVersion"`
	PlanID        string            `json:"planId"`
	SourceSHA256  string            `json:"sourceSha256"`
	BackupSHA256  string            `json:"backupSha256"`
	BackupBytes   int64             `json:"backupBytes"`
	TableCounts   map[string]uint64 `json:"tableCounts"`
	CreatedAt     time.Time         `json:"createdAt"`
	RestoreCheck  string            `json:"restoreCheck"`
}

type Receipt struct {
	PlanID                string
	PlanSHA256            string
	SourceSHA256          string
	TargetSHA256          string
	BackupSHA256          string
	ManifestSHA256        string
	MaterializationSHA256 string
	MaterializationCount  uint64
	State                 string
	RestoreVerified       bool
}

type RestoreVerification struct {
	SchemaVersion         string            `json:"schemaVersion"`
	PlanID                string            `json:"planId"`
	SourceSHA256          string            `json:"sourceSha256"`
	BackupSHA256          string            `json:"backupSha256"`
	ManifestSHA256        string            `json:"manifestSha256"`
	MaterializationSHA256 string            `json:"materializationSha256"`
	MaterializationCount  uint64            `json:"materializationCount"`
	TableCounts           map[string]uint64 `json:"tableCounts"`
	Outcome               string            `json:"outcome"`
	VerifiedAt            time.Time         `json:"verifiedAt"`
}

type CutoverAudit struct {
	SchemaVersion         string    `json:"schemaVersion"`
	PlanID                string    `json:"planId"`
	PlanSHA256            string    `json:"planSha256"`
	SourceSHA256          string    `json:"sourceSha256"`
	TargetSHA256          string    `json:"targetSha256"`
	BackupSHA256          string    `json:"backupSha256"`
	ManifestSHA256        string    `json:"manifestSha256"`
	MaterializationSHA256 string    `json:"materializationSha256"`
	MaterializationCount  uint64    `json:"materializationCount"`
	SourceState           string    `json:"sourceState"`
	TargetState           string    `json:"targetState"`
	Outcome               string    `json:"outcome"`
	RecordedAt            time.Time `json:"recordedAt"`
}

type ConfigurationImportProject struct {
	LegacyProjectID int64  `json:"legacyProjectId"`
	ProjectName     string `json:"projectName"`
	Plan            Plan   `json:"plan"`
}

type ConfigurationImport struct {
	SchemaVersion  string                       `json:"schemaVersion"`
	PlanID         string                       `json:"planId"`
	SourceSHA256   string                       `json:"sourceSha256"`
	BackupSHA256   string                       `json:"backupSha256"`
	ManifestSHA256 string                       `json:"manifestSha256"`
	BackupBytes    int64                        `json:"backupBytes"`
	Projects       []ConfigurationImportProject `json:"projects"`
	Outcome        string                       `json:"outcome"`
	ImportedAt     time.Time                    `json:"importedAt"`
}
