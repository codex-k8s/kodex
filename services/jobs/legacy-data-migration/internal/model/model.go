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
	ParentID         string         `json:"parent_id,omitempty"`
	OwnerActorID     string         `json:"owner_actor_id"`
	Kind             string         `json:"kind"`
	Name             string         `json:"name,omitempty"`
	State            string         `json:"state"`
	Version          uint64         `json:"version"`
	CreatedAt        time.Time      `json:"created_at,omitempty"`
	UpdatedAt        time.Time      `json:"updated_at,omitempty"`
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
	SchemaVersion         string                   `json:"schemaVersion"`
	PlanID                string                   `json:"planId"`
	SourceSHA256          string                   `json:"sourceSha256"`
	TargetSHA256          string                   `json:"targetSha256"`
	MappingSHA256         string                   `json:"mappingSha256"`
	MaterializationSHA256 string                   `json:"materializationSha256"`
	MaterializationCount  uint64                   `json:"materializationCount"`
	Counts                Counts                   `json:"counts"`
	Violations            map[string]uint64        `json:"violations"`
	PlanSHA256            string                   `json:"planSha256"`
	BackupSHA256          string                   `json:"backupSha256,omitempty"`
	ManifestSHA256        string                   `json:"manifestSha256,omitempty"`
	CutoverState          string                   `json:"cutoverState,omitempty"`
	Materialization       []MaterializationCommand `json:"-"`
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

// MaterializationCommand — закрытая семантическая команда target owner.
// Caller несёт source tuple и ожидаемый readback. Organization/owner всегда
// повторно выводятся target capability из Project/protected history.
type MaterializationCommand struct {
	Operation         string               `json:"operation"`
	SourceTable       string               `json:"sourceTable"`
	SourceID          int64                `json:"sourceId"`
	SourcePublicID    string               `json:"sourcePublicId,omitempty"`
	SourceRevision    uint64               `json:"sourceRevision"`
	SourceDigest      string               `json:"sourceDigest"`
	TargetID          string               `json:"targetId"`
	TargetKind        string               `json:"targetKind"`
	ProjectTargetID   string               `json:"projectTargetId"`
	AuthorityTargetID string               `json:"authorityTargetId,omitempty"`
	AuthorityVersion  uint64               `json:"authorityVersion,omitempty"`
	AuthoritySHA256   string               `json:"authoritySha256,omitempty"`
	Resource          MaterializedResource `json:"resource"`
	ProcessProvenance *ProcessProvenance   `json:"processProvenance,omitempty"`
}

// MaterializedResource — exact ожидаемый target readback без authority-полей.
type MaterializedResource struct {
	ParentID string         `json:"parentId,omitempty"`
	Name     string         `json:"name"`
	State    string         `json:"state"`
	Version  uint64         `json:"version"`
	Spec     map[string]any `json:"spec"`
}

// ProcessProvenance связывает provider identity source с server-owned Actor,
// policy и конкретным delegation/callback envelope.
type ProcessProvenance struct {
	RootActorSourceRef string `json:"rootActorSourceRef"`
	PolicyRevision     uint64 `json:"policyRevision"`
	PolicySHA256       string `json:"policySha256"`
	DelegationSourceID int64  `json:"delegationSourceId,omitempty"`
	DelegationTargetID string `json:"delegationTargetId,omitempty"`
	DelegationSHA256   string `json:"delegationSha256,omitempty"`
	CallbackRunID      string `json:"callbackRunId,omitempty"`
	CallbackSHA256     string `json:"callbackSha256,omitempty"`
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
