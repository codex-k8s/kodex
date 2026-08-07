// Package model содержит безопасный отчёт и внутреннюю модель cutover-плана.
package model

import "time"

type SnapshotRow struct {
	Table   string
	Payload []byte
}

type TargetResource struct {
	ID               string         `json:"id"`
	OrganizationID   string         `json:"organization_id"`
	ProjectID        string         `json:"project_id"`
	OwnerActorID     string         `json:"owner_actor_id"`
	Kind             string         `json:"kind"`
	State            string         `json:"state"`
	Version          uint64         `json:"version"`
	ProjectionSHA256 string         `json:"projection_sha256,omitempty"`
	Historical       bool           `json:"historical,omitempty"`
	Spec             map[string]any `json:"spec"`
	Canonical        []byte         `json:"-"`
}

type Counts struct {
	Source  map[string]uint64 `json:"source"`
	Mapped  map[string]uint64 `json:"mapped"`
	Archive map[string]uint64 `json:"archive"`
}

type Plan struct {
	SchemaVersion  string            `json:"schemaVersion"`
	PlanID         string            `json:"planId"`
	SourceSHA256   string            `json:"sourceSha256"`
	TargetSHA256   string            `json:"targetSha256"`
	MappingSHA256  string            `json:"mappingSha256"`
	Counts         Counts            `json:"counts"`
	Violations     map[string]uint64 `json:"violations"`
	PlanSHA256     string            `json:"planSha256"`
	BackupSHA256   string            `json:"backupSha256,omitempty"`
	ManifestSHA256 string            `json:"manifestSha256,omitempty"`
	CutoverState   string            `json:"cutoverState,omitempty"`
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
	PlanID          string
	PlanSHA256      string
	SourceSHA256    string
	TargetSHA256    string
	BackupSHA256    string
	ManifestSHA256  string
	State           string
	RestoreVerified bool
}

type RestoreVerification struct {
	SchemaVersion  string            `json:"schemaVersion"`
	PlanID         string            `json:"planId"`
	SourceSHA256   string            `json:"sourceSha256"`
	BackupSHA256   string            `json:"backupSha256"`
	ManifestSHA256 string            `json:"manifestSha256"`
	TableCounts    map[string]uint64 `json:"tableCounts"`
	Outcome        string            `json:"outcome"`
	VerifiedAt     time.Time         `json:"verifiedAt"`
}

type CutoverAudit struct {
	SchemaVersion  string    `json:"schemaVersion"`
	PlanID         string    `json:"planId"`
	PlanSHA256     string    `json:"planSha256"`
	SourceSHA256   string    `json:"sourceSha256"`
	TargetSHA256   string    `json:"targetSha256"`
	BackupSHA256   string    `json:"backupSha256"`
	ManifestSHA256 string    `json:"manifestSha256"`
	SourceState    string    `json:"sourceState"`
	TargetState    string    `json:"targetState"`
	Outcome        string    `json:"outcome"`
	RecordedAt     time.Time `json:"recordedAt"`
}
