package model

import (
	"encoding/json"
	"time"
)

// RestoreState содержит каноническое устойчивое состояние восстановления.
type RestoreState struct {
	Version                int                                           `json:"v"`
	RestoreID              string                                        `json:"restore_id"`
	DatabaseClusterID      string                                        `json:"database_cluster_id"`
	BackupManifestDigest   string                                        `json:"backup_manifest_digest_sha256"`
	RecoveryTargetUnix     int64                                         `json:"recovery_target_unix"`
	Phase                  string                                        `json:"phase"`
	RestoreEpoch           uint64                                        `json:"restore_epoch"`
	CoordinationRevision   uint64                                        `json:"coordination_revision"`
	ControllerGeneration   uint64                                        `json:"controller_signer_generation"`
	WorkloadSetRevision    uint64                                        `json:"workload_set_revision"`
	AnchorRevision         uint64                                        `json:"anchor_revision"`
	EvidenceDigest         string                                        `json:"evidence_digest_sha256"`
	EvidenceAnchorRevision uint64                                        `json:"evidence_anchor_revision,omitempty"`
	RestoredClusterUID     string                                        `json:"restored_cluster_uid,omitempty"`
	RestoredTimelineID     uint64                                        `json:"restored_timeline_id,omitempty"`
	SafeWindowNotBefore    int64                                         `json:"safe_window_not_before,omitempty"`
	PrepareIdempotencyKey  string                                        `json:"prepare_idempotency_key"`
	PrepareSemanticDigest  string                                        `json:"prepare_semantic_digest_sha256"`
	CompleteIdempotencyKey string                                        `json:"complete_idempotency_key,omitempty"`
	CompleteSemanticDigest string                                        `json:"complete_semantic_digest_sha256,omitempty"`
	ExpectedTargets        map[string]RestoreExpectedTarget              `json:"expected_targets"`
	Issuances              map[string]RestoreIssuanceRecord              `json:"issuances"`
	Deliveries             map[string]RestoreDeliveryRecord              `json:"deliveries"`
	Directives             map[string]RestoreDirectiveRecord             `json:"directives"`
	ACKs                   map[string]RestoreACKRecord                   `json:"acks"`
	OperatorAuthorizations map[string]RestoreOperatorAuthorizationRecord `json:"operator_authorizations"`
	UpdatedAt              int64                                         `json:"updated_at"`
}

// RestoreOperatorCredential — результат server-side TokenReview projected
// ServiceAccount token; само значение bearer token в модель не попадает.
type RestoreOperatorCredential struct {
	Subject           string
	Namespace         string
	ServiceAccount    string
	Audience          string
	TokenDigestSHA256 string
}

// RestoreOperatorAuthorizationRecord связывает одноразовый application
// credential с exact RPC, idempotency и canonical semantic digest.
type RestoreOperatorAuthorizationRecord struct {
	TokenDigestSHA256    string `json:"token_digest_sha256"`
	Subject              string `json:"subject"`
	FullMethod           string `json:"full_method"`
	IdempotencyKey       string `json:"idempotency_key"`
	SemanticDigestSHA256 string `json:"semantic_digest_sha256"`
	AuthorizedAt         int64  `json:"authorized_at"`
}

// RestoreExpectedTarget задаёт точную ожидаемую роль восстановления.
type RestoreExpectedTarget struct {
	TargetID             string `json:"target_id"`
	WorkloadID           string `json:"workload_id"`
	WorkloadSPIFFEID     string `json:"workload_spiffe_id"`
	Role                 string `json:"role"`
	WorkloadGeneration   uint64 `json:"workload_generation"`
	CredentialGeneration uint64 `json:"credential_generation"`
	ACKKeyGeneration     uint64 `json:"ack_key_generation"`
}

// RestoreIssuanceRecord фиксирует идемпотентный выпуск для одной цели.
type RestoreIssuanceRecord struct {
	TargetID       string `json:"target_id"`
	JTI            string `json:"jti"`
	IdempotencyKey string `json:"idempotency_key"`
	IssuedAt       int64  `json:"issued_at"`
}

// RestoreDeliveryRecord фиксирует криптографический readback доставки.
type RestoreDeliveryRecord struct {
	TargetID                   string `json:"target_id"`
	DeliveryReceiptCompactJWS  string `json:"delivery_receipt_compact_jws"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	ACKKeyGeneration           uint64 `json:"ack_key_generation"`
}

// RestoreDirectiveRecord фиксирует точную директиву остановки и drain.
type RestoreDirectiveRecord struct {
	TargetID     string `json:"target_id"`
	JTI          string `json:"jti"`
	CompactJWS   string `json:"compact_jws"`
	DigestSHA256 string `json:"digest_sha256"`
	ExpiresAt    int64  `json:"expires_at"`
}

// RestoreACKRecord фиксирует одноразовый подтверждённый ACK.
type RestoreACKRecord struct {
	TargetID              string `json:"target_id"`
	ReceiptID             string `json:"receipt_id"`
	IdempotencyKey        string `json:"idempotency_key"`
	ACKJTI                string `json:"ack_jti"`
	SemanticRequestDigest string `json:"semantic_request_digest_sha256"`
	AcceptedACKDigest     string `json:"accepted_ack_digest_sha256"`
	AcceptedAt            int64  `json:"accepted_at"`
	ResultingPhase        string `json:"resulting_phase"`
}

// PrepareRestoreCommand задаёт pinned intent подготовки восстановления.
type PrepareRestoreCommand struct {
	RestoreID            string
	DatabaseClusterID    string
	BackupManifestDigest string
	RecoveryTarget       time.Time
	IdempotencyKey       string
	SemanticDigest       string
	ExpectedTargets      map[string]RestoreExpectedTarget
	ControllerGeneration uint64
	WorkloadSetRevision  uint64
	Now                  time.Time
}

// CompleteRestoreCommand задаёт завершение и открытие безопасного окна.
type CompleteRestoreCommand struct {
	RestoreID            string
	DatabaseClusterID    string
	BackupManifestDigest string
	RecoveryTarget       time.Time
	IdempotencyKey       string
	SemanticDigest       string
	EvidenceDigest       string
	EvidenceAnchor       uint64
	EvidenceRestoreEpoch uint64
	RestoredClusterUID   string
	RestoredTimelineID   uint64
	RestoreCompletedAt   time.Time
	Now                  time.Time
}

// RestoreCompletionEvidence — проверенное независимое доказательство
// фактического PITR, сформированное отдельным executor workload.
type RestoreCompletionEvidence struct {
	CompactJWSDigestSHA256 string
	AnchorRevision         uint64
	RestoreEpoch           uint64
	RestoredClusterUID     string
	RestoredTimelineID     uint64
	RestoreCompletedAt     time.Time
}

// RestoreFenceEvidenceClaims — канонический payload внешнего evidence.
type RestoreFenceEvidenceClaims struct {
	Version                               int                    `json:"v"`
	Issuer                                string                 `json:"iss"`
	Audience                              string                 `json:"aud"`
	AnchorRevision                        uint64                 `json:"anchor_revision"`
	RestoreEpoch                          uint64                 `json:"restore_epoch"`
	Phase                                 string                 `json:"phase"`
	DatabaseClusterID                     string                 `json:"database_cluster_id"`
	RestoreID                             string                 `json:"restore_id"`
	BackupManifestDigestSHA256            string                 `json:"backup_manifest_digest_sha256"`
	BackupResourceName                    string                 `json:"backup_resource_name"`
	BackupResourceUID                     string                 `json:"backup_resource_uid"`
	BackupResourceVersion                 string                 `json:"backup_resource_version"`
	BackupResourceGeneration              uint64                 `json:"backup_resource_generation"`
	ProviderBackupID                      string                 `json:"provider_backup_id"`
	ProviderBackupName                    string                 `json:"provider_backup_name"`
	SourceClusterUID                      string                 `json:"source_cluster_uid"`
	SourceClusterResourceVersion          string                 `json:"source_cluster_resource_version"`
	SourceClusterGeneration               uint64                 `json:"source_cluster_generation"`
	SourceClusterSpecSHA256               string                 `json:"source_cluster_spec_sha256"`
	BarmanObjectName                      string                 `json:"barman_object_name"`
	BarmanServerName                      string                 `json:"barman_server_name"`
	SourceTimelineID                      uint64                 `json:"source_timeline_id"`
	RecoveryTargetTime                    int64                  `json:"recovery_target_time"`
	ControllerSignerGeneration            uint64                 `json:"controller_signer_generation"`
	WorkloadSetRevision                   uint64                 `json:"workload_set_revision"`
	ExpectedWorkloadRoleGenerationsSHA256 string                 `json:"expected_workload_role_generations_sha256"`
	QuiescenceACKSetSHA256                string                 `json:"quiescence_ack_set_sha256"`
	ExpectedACKCount                      uint32                 `json:"expected_ack_count"`
	AcceptedACKCount                      uint32                 `json:"accepted_ack_count"`
	SemanticTransition                    string                 `json:"semantic_transition"`
	Predecessor                           RestoreEvidencePointer `json:"predecessor"`
	IssuedAt                              int64                  `json:"issued_at"`
	RestoreCompletedAt                    int64                  `json:"restore_completed_at"`
	RestoredClusterUID                    string                 `json:"restored_cluster_uid"`
	RestoredClusterResourceVersion        string                 `json:"restored_cluster_resource_version"`
	RestoredPrimary                       string                 `json:"restored_primary"`
	RestoredTimelineID                    uint64                 `json:"restored_timeline_id"`
	ProviderObservedGeneration            uint64                 `json:"provider_observed_generation"`
}

// RestoreEvidencePointer связывает evidence с точным predecessor.
type RestoreEvidencePointer struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// RoleBoundRestoreDirectiveClaims связывает directive с ролью и поколением.
type RoleBoundRestoreDirectiveClaims struct {
	Version                    int    `json:"v"`
	Issuer                     string `json:"iss"`
	Audience                   string `json:"aud"`
	Subject                    string `json:"sub"`
	JTI                        string `json:"jti"`
	RestoreID                  string `json:"restore_id"`
	RestoreEpoch               uint64 `json:"restore_epoch"`
	CoordinationRevision       uint64 `json:"coordination_revision"`
	Phase                      string `json:"phase"`
	WorkloadID                 string `json:"workload_id"`
	WorkloadSPIFFEID           string `json:"workload_spiffe_id"`
	Role                       string `json:"role"`
	WorkloadGeneration         uint64 `json:"workload_generation"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	StopAcceptingRequired      bool   `json:"stop_accepting_required"`
	DrainInflightRequired      bool   `json:"drain_inflight_required"`
	IssuedAt                   int64  `json:"iat"`
	NotBefore                  int64  `json:"nbf"`
	ExpiresAt                  int64  `json:"exp"`
}

// QuiescenceACKClaims доказывает остановку и дренирование workload.
type QuiescenceACKClaims struct {
	Version                    int    `json:"v"`
	Issuer                     string `json:"iss"`
	Audience                   string `json:"aud"`
	Subject                    string `json:"sub"`
	JTI                        string `json:"jti"`
	DirectiveJTI               string `json:"directive_jti"`
	RestoreID                  string `json:"restore_id"`
	RestoreEpoch               uint64 `json:"restore_epoch"`
	CoordinationRevision       uint64 `json:"coordination_revision"`
	WorkloadID                 string `json:"workload_id"`
	WorkloadSPIFFEID           string `json:"workload_spiffe_id"`
	Role                       string `json:"role"`
	WorkloadGeneration         uint64 `json:"workload_generation"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	ACKKeyID                   string `json:"ack_key_kid"`
	ACKKeyGeneration           uint64 `json:"ack_key_generation"`
	ACKKeyThumbprintSHA256     string `json:"ack_key_thumbprint_sha256"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	ServedSnapshotDigest       string `json:"served_snapshot_digest_sha256"`
	AcceptingStopped           bool   `json:"accepting_stopped"`
	InflightDrained            bool   `json:"inflight_drained"`
	InflightCount              uint64 `json:"inflight_count"`
	IssuedAt                   int64  `json:"iat"`
	NotBefore                  int64  `json:"nbf"`
	ExpiresAt                  int64  `json:"exp"`
}

// RestoreRoleTrustMetadata закрепляет доверие к ключу роли восстановления.
type RestoreRoleTrustMetadata struct {
	SourceRevision   uint64
	SourceDigest     string
	KeySetRevision   uint64
	SignerGeneration uint64
}

// JSONDocument хранит строго проверенный исходный документ JSON.
type JSONDocument = json.RawMessage
