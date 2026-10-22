package model

import (
	"encoding/json"
	"time"
)

// RestoreState is the canonical durable restore aggregate: pinned backup/time/epoch/coordination and
// evidence anchors plus exact expected targets, issuance/delivery/directive/ACK maps and consumed
// operator authorizations. Mutation and phase eligibility belong to the coordination owner.
// RestoreState задаёт canonical durable restore агрегат: закреплённые backup/time/epoch/coordination и
// evidence anchors с exact expected targets, issuance/delivery/directive/ACK maps и потреблёнными
// operator authorizations. Изменения и phase eligibility принадлежат coordination владельцу.
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

// RestoreOperatorCredential represents the server-reviewed projected ServiceAccount subject, namespace,
// account, audience and token digest; the bearer bytes are intentionally absent and a request cannot
// self-assert a valid review.
// RestoreOperatorCredential представляет проверенные сервером projected ServiceAccount subject,
// namespace, account, audience и token digest; bearer байты намеренно отсутствуют, request не может сам
// заявить корректный review.
type RestoreOperatorCredential struct {
	Subject           string
	Namespace         string
	ServiceAccount    string
	Audience          string
	TokenDigestSHA256 string
}

// RestoreOperatorAuthorizationRecord binds one reviewed application-token digest to exact RPC,
// idempotency key and canonical semantic digest at an assigned authorization time for durable replay
// control.
// RestoreOperatorAuthorizationRecord связывает один reviewed application-token digest с exact RPC,
// idempotency key и canonical semantic digest в назначенное authorization время для durable replay
// контроля.
type RestoreOperatorAuthorizationRecord struct {
	TokenDigestSHA256    string `json:"token_digest_sha256"`
	Subject              string `json:"subject"`
	FullMethod           string `json:"full_method"`
	IdempotencyKey       string `json:"idempotency_key"`
	SemanticDigestSHA256 string `json:"semantic_digest_sha256"`
	AuthorizedAt         int64  `json:"authorized_at"`
}

// RestoreExpectedTarget freezes one target's workload/SPIFFE/role and workload/credential/ACK
// generations for the restore aggregate rather than looking up mutable latest delivery settings during
// ACK acceptance.
// RestoreExpectedTarget закрепляет workload/SPIFFE/role и workload/credential/ACK поколения одной цели
// restore агрегата вместо поиска изменяемых latest delivery настроек при ACK принятии.
type RestoreExpectedTarget struct {
	TargetID             string `json:"target_id"`
	WorkloadID           string `json:"workload_id"`
	WorkloadSPIFFEID     string `json:"workload_spiffe_id"`
	Role                 string `json:"role"`
	WorkloadGeneration   uint64 `json:"workload_generation"`
	CredentialGeneration uint64 `json:"credential_generation"`
	ACKKeyGeneration     uint64 `json:"ack_key_generation"`
}

// RestoreIssuanceRecord stores the server-issued directive JTI and idempotency for one exact target with
// issuance time, allowing retries to reuse the same issuance identity.
// RestoreIssuanceRecord хранит server-issued directive JTI и idempotency одной exact цели со временем
// выпуска, позволяя повтору использовать ту же issuance identity.
type RestoreIssuanceRecord struct {
	TargetID       string `json:"target_id"`
	JTI            string `json:"jti"`
	IdempotencyKey string `json:"idempotency_key"`
	IssuedAt       int64  `json:"issued_at"`
}

// RestoreDeliveryRecord links a signed delivery receipt and role-credential digest to exact
// credential/ACK generations for one target; acceptance requires independent receipt/readback
// verification.
// RestoreDeliveryRecord связывает signed delivery receipt и role-credential digest с exact
// credential/ACK поколениями одной цели; принятие требует независимой receipt/readback проверки.
type RestoreDeliveryRecord struct {
	TargetID                   string `json:"target_id"`
	DeliveryReceiptCompactJWS  string `json:"delivery_receipt_compact_jws"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	ACKKeyGeneration           uint64 `json:"ack_key_generation"`
}

// RestoreDirectiveRecord persists one exact stop/drain directive's JTI, compact bytes, digest and expiry
// for a target so later ACKs cannot be rebound to a different directive.
// RestoreDirectiveRecord сохраняет JTI, compact байты, digest и expiry одной exact stop/drain directive
// для цели, исключая перепривязку последующих ACK к другой directive.
type RestoreDirectiveRecord struct {
	TargetID     string `json:"target_id"`
	JTI          string `json:"jti"`
	CompactJWS   string `json:"compact_jws"`
	DigestSHA256 string `json:"digest_sha256"`
	ExpiresAt    int64  `json:"expires_at"`
}

// RestoreACKRecord stores one accepted ACK's target, receipt/JTI, idempotency and semantic/evidence
// digests with acceptance time and resulting phase; it is the durable result, not raw unverified ACK
// input.
// RestoreACKRecord хранит target, receipt/JTI, idempotency и semantic/evidence digests одного принятого
// ACK со временем и resulting phase; это durable результат, не сырой непроверенный ACK вход.
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

// PrepareRestoreCommand combines caller intent with owner-derived expected targets, controller/workload
// revisions and current time for one idempotent prepare transition; fields are not standalone proof of
// operator authority.
// PrepareRestoreCommand объединяет caller intent с owner-derived expected targets, controller/workload
// ревизиями и текущим временем для одного idempotent prepare перехода; поля не являются самостоятельным
// proof operator полномочий.
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

// CompleteRestoreCommand binds exact original intent and idempotency to independently verified
// evidence/epoch/cluster/timeline/completion time before the owner can finish restore and open its safe
// window.
// CompleteRestoreCommand связывает exact исходный intent и idempotency с независимо проверенными
// evidence/epoch/cluster/timeline/completion time до завершения restore владельцем и открытия safe
// window.
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

// RestoreCompletionEvidence is the verifier's compact metadata result for independently signed physical
// recovery evidence, including anchor/epoch and actual restored cluster/timeline/time; it carries no raw
// executor credentials.
// RestoreCompletionEvidence задаёт compact metadata результат verifier для independently signed physical
// recovery evidence с anchor/epoch и фактическими restored cluster/timeline/time без сырых executor
// credentials.
type RestoreCompletionEvidence struct {
	CompactJWSDigestSHA256 string
	AnchorRevision         uint64
	RestoreEpoch           uint64
	RestoredClusterUID     string
	RestoredTimelineID     uint64
	RestoreCompletedAt     time.Time
}

// RestoreFenceEvidenceClaims carries the canonical signed external restore/fence evidence, tying
// backup/provider/source-cluster pins, expected/accepted ACK sets, semantic predecessor and physical
// recovery observations to one epoch/anchor. Filling this payload is not proof that PITR occurred.
// RestoreFenceEvidenceClaims несёт canonical signed external restore/fence evidence, связывая
// backup/provider/source-cluster привязки, expected/accepted ACK sets, semantic predecessor и physical
// recovery наблюдения с одним epoch/anchor. Заполнение payload не доказывает PITR.
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

// RestoreEvidencePointer pins a predecessor evidence revision and digest so recovery history cannot be
// redirected by an unversioned current-state reference.
// RestoreEvidencePointer закрепляет predecessor evidence revision и digest, исключая перенаправление
// recovery истории неверсионированной current-state ссылкой.
type RestoreEvidencePointer struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// RoleBoundRestoreDirectiveClaims signs one restore phase/epoch/coordination and exact
// workload/credential binding with explicit stop-accepting/drain requirements and expiry; interpretation
// remains role-specific.
// RoleBoundRestoreDirectiveClaims подписывает restore phase/epoch/coordination и exact
// workload/credential привязку с явными stop-accepting/drain требованиями и expiry; интерпретация
// остаётся role-specific.
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

// QuiescenceACKClaims binds a signed drain acknowledgement to its directive, restore epoch, exact
// role/credential/ACK key, served snapshot and observed inflight state with one-time identity/lifetime;
// boolean flags alone do not establish quiescence.
// QuiescenceACKClaims связывает signed drain подтверждение с directive, restore epoch, exact
// role/credential/ACK key, served snapshot и наблюдённым inflight состоянием с одноразовыми
// identity/сроком; одних boolean признаков недостаточно для quiescence.
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

// RestoreRoleTrustMetadata summarizes the actually served role-verification source, key-set and signer
// generations for readiness/readback without exposing key material.
// RestoreRoleTrustMetadata описывает фактически served role-verification source, key-set и signer
// поколения для readiness/readback без key material.
type RestoreRoleTrustMetadata struct {
	SourceRevision   uint64
	SourceDigest     string
	KeySetRevision   uint64
	SignerGeneration uint64
}

// JSONDocument aliases json.RawMessage to retain exact JSON bytes for domain consumers; the alias
// performs no parsing, normalization or validation by itself.
// JSONDocument задаёт alias json.RawMessage для сохранения exact JSON байтов domain consumer; сам alias
// не выполняет разбор, нормализацию и проверку.
type JSONDocument = json.RawMessage
