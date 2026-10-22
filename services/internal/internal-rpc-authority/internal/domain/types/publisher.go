package model

import (
	"encoding/json"
	"sort"
	"time"
)

// DeliveryTarget declares one registry-owned workload/role generation and exact secret, mount, TLS,
// network-policy and readback/restore routes. These are permitted delivery locators and pins, not
// private credential values or caller-chosen destinations.
// DeliveryTarget объявляет один registry-owned workload/role generation и exact secret, mount, TLS,
// network-policy и readback/restore маршруты. Это разрешённые delivery locators и привязки, не private
// credential значения и не выбранные caller destinations.
type DeliveryTarget struct {
	TargetID                   string
	WorkloadID                 string
	WorkloadSPIFFEID           string
	Role                       string
	Namespace                  string
	ServiceAccount             string
	WorkloadGeneration         uint64
	StartupReadbackRequired    bool
	CredentialGeneration       uint64
	AuthoritySnapshotSecret    string
	AuthoritySnapshotMountPath string
	AuthPrivateKeySecret       string
	AuthPrivateKeyMountPath    string
	ManifestTrustSecret        string
	ManifestTrustMountPath     string
	ProofTrustSecret           string
	ProofTrustMountPath        string
	ProofPrivateKeySecret      string
	ProofPrivateKeyMountPath   string
	DatabaseLoginPrincipal     string
	DatabaseCredentialSecret   string
	DatabaseDSNMountPath       string
	RestoreCredentialID        string
	RestoreCredentialPath      string
	RestoreCredentialMountPath string
	RestoreACKKeyID            string
	RestoreACKKeyGeneration    uint64
	RestoreACKKeyPath          string
	RestoreACKKeyMountPath     string
	RestoreControllerAddress   string
	RestoreControllerSNI       string
	RestoreControllerCAPath    string
	RestoreControllerAudience  string
	RestoreControllerMethod    string
	RestoreNetworkPolicy       string
	ReadbackID                 string
	ReadbackCredentialPath     string
	ReadbackCredentialMount    string
	ReadbackCredentialID       string
	ReadbackPossessionKeyPath  string
	ReadbackPossessionKeyMount string
	ReadbackPossessionKeyID    string
	ReadbackIntentRevision     uint64
	ReadbackMaterialGeneration uint64
	ReadbackSourceRevision     uint64
	ReadbackServedStateDigest  string
	ReadbackAttestorAddress    string
	ReadbackAttestorSNI        string
	ReadbackAttestorCAPath     string
	ReadbackAttestorAudience   string
	ReadbackChallengeMethod    string
	ReadbackAttestationMethod  string
	ReadbackNetworkPolicy      string
}

// DeliveryTargetRegistry binds a closed target map to one reviewed version and source revision/digest;
// map entries must be validated before publication or readback uses their locations.
// DeliveryTargetRegistry связывает закрытую target map с проверенными version и source revision/digest;
// entries проверяются до использования их locators публикацией и readback.
type DeliveryTargetRegistry struct {
	Version        int
	SourceRevision uint64
	SourceDigest   string
	Targets        map[string]DeliveryTarget
}

// SnapshotReadbackTarget identifies one mandatory workload/role/generation tuple for startup admission,
// without secret paths or mutable latest-version aliases.
// SnapshotReadbackTarget обозначает один обязательный workload/role/generation tuple для startup
// admission без secret путей и изменяемых latest-version aliases.
type SnapshotReadbackTarget struct {
	WorkloadID         string
	Role               string
	WorkloadGeneration uint64
}

// StartupReadbackTargets selects entries with StartupReadbackRequired and returns fresh metadata sorted
// by workload, role and generation. It neither validates nor deduplicates registry entries and does not
// mutate the source map.
// StartupReadbackTargets выбирает entries с StartupReadbackRequired и возвращает новые metadata,
// отсортированные по workload, role и generation. Entries не проверяются и не дедуплицируются, source
// map не меняется.
func (registry DeliveryTargetRegistry) StartupReadbackTargets() []SnapshotReadbackTarget {
	targets := make([]SnapshotReadbackTarget, 0, len(registry.Targets))
	for _, target := range registry.Targets {
		if !target.StartupReadbackRequired {
			continue
		}
		targets = append(targets, SnapshotReadbackTarget{
			WorkloadID:         target.WorkloadID,
			Role:               target.Role,
			WorkloadGeneration: target.WorkloadGeneration,
		})
	}
	sort.Slice(targets, func(left, right int) bool {
		if targets[left].WorkloadID != targets[right].WorkloadID {
			return targets[left].WorkloadID < targets[right].WorkloadID
		}
		if targets[left].Role != targets[right].Role {
			return targets[left].Role < targets[right].Role
		}
		return targets[left].WorkloadGeneration < targets[right].WorkloadGeneration
	})
	return targets
}

// StartupReadbackTargetCount returns the length of the sorted required-target projection, counting
// role/generation entries rather than distinct workload IDs or successful receipts.
// StartupReadbackTargetCount возвращает длину sorted required-target проекции, считая role/generation
// entries, а не уникальные workload ID и успешные receipts.
func (registry DeliveryTargetRegistry) StartupReadbackTargetCount() int {
	return len(registry.StartupReadbackTargets())
}

// CredentialIssuanceDirective binds a signed issuance request to exact restore epoch,
// coordination/registry pins, delivery target and workload/credential/ACK generations with one-time
// identity and expiry.
// CredentialIssuanceDirective связывает signed issuance запрос с exact restore epoch,
// coordination/registry привязками, delivery target и workload/credential/ACK поколениями с одноразовой
// identity и expiry.
type CredentialIssuanceDirective struct {
	Version                int    `json:"v"`
	Issuer                 string `json:"iss"`
	Audience               string `json:"aud"`
	Subject                string `json:"sub"`
	JTI                    string `json:"jti"`
	RestoreID              string `json:"restore_id"`
	RestoreEpoch           uint64 `json:"restore_epoch"`
	CoordinationRevision   uint64 `json:"coordination_revision"`
	TargetRegistryRevision uint64 `json:"target_registry_revision"`
	TargetRegistryDigest   string `json:"target_registry_digest_sha256"`
	DeliveryTargetID       string `json:"delivery_target_id"`
	WorkloadID             string `json:"workload_id"`
	WorkloadSPIFFEID       string `json:"workload_spiffe_id"`
	Role                   string `json:"role"`
	WorkloadGeneration     uint64 `json:"workload_generation"`
	CredentialGeneration   uint64 `json:"credential_generation"`
	ACKKeyGeneration       uint64 `json:"ack_key_generation"`
	IssuedAt               int64  `json:"iat"`
	NotBefore              int64  `json:"nbf"`
	ExpiresAt              int64  `json:"exp"`
}

// RestoreRoleCredentialClaims carries role-bound restore authorization, signer provenance and the public
// ACK key/thumbprint binding; signature and exact workload/restore eligibility are verified separately,
// and no ACK private key belongs here.
// RestoreRoleCredentialClaims несёт role-bound restore авторизацию, signer происхождение и привязку
// public ACK key/thumbprint; подпись и exact workload/restore допустимость проверяются отдельно, private
// ACK key сюда не входит.
type RestoreRoleCredentialClaims struct {
	Version                  int             `json:"v"`
	Issuer                   string          `json:"iss"`
	Audience                 string          `json:"aud"`
	Subject                  string          `json:"sub"`
	JTI                      string          `json:"jti"`
	WorkloadID               string          `json:"workload_id"`
	WorkloadSPIFFEID         string          `json:"workload_spiffe_id"`
	Role                     string          `json:"role"`
	WorkloadGeneration       uint64          `json:"workload_generation"`
	CredentialGeneration     uint64          `json:"credential_generation"`
	RestoreID                string          `json:"restore_id"`
	RestoreEpoch             uint64          `json:"restore_epoch"`
	CoordinationRevision     uint64          `json:"coordination_revision"`
	SignerSourceRevision     uint64          `json:"credential_signer_source_revision"`
	SignerSourceDigestSHA256 string          `json:"credential_signer_source_digest_sha256"`
	SignerKeySetRevision     uint64          `json:"credential_signer_key_set_revision"`
	SignerGeneration         uint64          `json:"credential_signer_generation"`
	SignerKeyID              string          `json:"credential_signer_kid"`
	ACKKeyID                 string          `json:"ack_key_kid"`
	ACKKeyGeneration         uint64          `json:"ack_key_generation"`
	ACKPublicJWK             json.RawMessage `json:"ack_public_jwk"`
	ACKKeyThumbprintSHA256   string          `json:"ack_key_thumbprint_sha256"`
	IssuedAt                 int64           `json:"iat"`
	NotBefore                int64           `json:"nbf"`
	ExpiresAt                int64           `json:"exp"`
}

// CredentialDeliveryReceiptClaims signs the exact issuance/restore/target/generation and readback-digest
// relationship so delivery can be verified independently of a publisher's success flag; the claims alone
// are not verified evidence.
// CredentialDeliveryReceiptClaims подписывает точную связь issuance/restore/target/generation и readback
// digest для проверки доставки независимо от publisher success flag; сами claims не являются уже
// проверенным evidence.
type CredentialDeliveryReceiptClaims struct {
	Version                    int    `json:"v"`
	Issuer                     string `json:"iss"`
	Audience                   string `json:"aud"`
	Subject                    string `json:"sub"`
	JTI                        string `json:"jti"`
	IssuanceDirectiveJTI       string `json:"issuance_directive_jti"`
	RestoreID                  string `json:"restore_id"`
	RestoreEpoch               uint64 `json:"restore_epoch"`
	CoordinationRevision       uint64 `json:"coordination_revision"`
	DeliveryTargetID           string `json:"delivery_target_id"`
	WorkloadID                 string `json:"workload_id"`
	Role                       string `json:"role"`
	WorkloadGeneration         uint64 `json:"workload_generation"`
	CredentialGeneration       uint64 `json:"credential_generation"`
	RoleCredentialDigestSHA256 string `json:"role_credential_digest_sha256"`
	ACKKeyID                   string `json:"ack_key_kid"`
	ACKKeyGeneration           uint64 `json:"ack_key_generation"`
	ACKKeyThumbprintSHA256     string `json:"ack_key_thumbprint_sha256"`
	SecretGeneration           uint64 `json:"secret_generation"`
	DeliveryReadbackDigest     string `json:"delivery_readback_digest_sha256"`
	IssuedAt                   int64  `json:"iat"`
	NotBefore                  int64  `json:"nbf"`
	ExpiresAt                  int64  `json:"exp"`
}

// PublishedCredential records the durable idempotent issuance result, accepted directive identity,
// receipt and generations without retaining raw role private-key material.
// PublishedCredential фиксирует durable idempotent issuance результат, принятую directive identity,
// receipt и поколения без сырых role private-key данных.
type PublishedCredential struct {
	IdempotencyKey       string
	DirectiveJTI         string
	DirectiveDigest      string
	DeliveryReceiptJWS   string
	RoleCredentialDigest string
	CredentialGeneration uint64
	ACKKeyGeneration     uint64
	AcceptedAt           time.Time
}

// PublishedReadbackMaterial carries both the signed readback credential and private possession JWK with
// exact intent/secret generations for trusted delivery; unlike safe metadata projections it must never
// be logged or exposed as an ordinary API reply.
// PublishedReadbackMaterial несёт signed readback credential и private possession JWK с exact
// intent/secret поколениями для доверенной доставки; в отличие от safe metadata проекций его нельзя
// логировать и выдавать обычным API ответом.
type PublishedReadbackMaterial struct {
	Intent                     ReadbackIntent
	ReadbackCredentialJWS      string
	PossessionPrivateJWK       string
	CredentialSecretGeneration uint64
	PossessionSecretGeneration uint64
}

// AuthoritySnapshotPublication records one publication intent/input digest, complete
// source/predecessor/key/policy pins, signed snapshot bytes and exact resourceVersion readback;
// consumers must still establish publication/readiness, not infer it from a filled structure.
// AuthoritySnapshotPublication фиксирует publication intent/input digest, полные
// source/predecessor/key/policy привязки, signed snapshot байты и exact resourceVersion readback;
// consumer подтверждает publication/readiness, не выводя их из заполненной структуры.
type AuthoritySnapshotPublication struct {
	IntentID                string
	InputDigestSHA256       string
	SourceRevision          uint64
	SourceDigestSHA256      string
	KeySetRevision          uint64
	PolicyRevision          uint64
	SignerGeneration        uint64
	PredecessorRevision     uint64
	PredecessorDigestSHA256 string
	SnapshotCompactJWS      string
	SnapshotResourceVersion string
	PublishedAt             time.Time
}

// AuthoritySnapshotPredecessor links a stored predecessor publication to the actual registry digest and
// recorded rotation phase so retries do not reconstruct history from a mutable current snapshot.
// AuthoritySnapshotPredecessor связывает сохранённую predecessor публикацию с фактическим registry
// digest и записанной rotation phase, исключая восстановление истории повтором из изменяемого current
// snapshot.
type AuthoritySnapshotPredecessor struct {
	Publication          AuthoritySnapshotPublication
	RegistryDigestSHA256 string
	RotationPhase        string
}

// AuthorityRotationIntent pins one forward-only transition and its expected readback cardinality before
// external delivery, including a publication-input digest but no private key bytes.
// AuthorityRotationIntent закрепляет один forward-only переход и ожидаемую readback cardinality до
// внешней доставки, включая publication-input digest без private key байтов.
type AuthorityRotationIntent struct {
	IntentID                     string
	SourceRevision               uint64
	SourceDigestSHA256           string
	PredecessorRevision          uint64
	PredecessorDigestSHA256      string
	ExpectedReadbackCount        int
	PublicationInputDigestSHA256 string
}

// AuthorityRotationOperation coordinates one registry/base-pinned multi-phase rotation with
// PostgreSQL-assigned switch/expiry/completion times; retry must not invent a new base or extend those
// deadlines.
// AuthorityRotationOperation координирует одну registry/base-pinned многофазную ротацию с назначенными
// PostgreSQL switch/expiry/completion сроками; retry не выдумывает новый base и не продлевает дедлайны.
type AuthorityRotationOperation struct {
	OperationID           string
	RegistryRevision      uint64
	RegistryDigestSHA256  string
	BaseRevision          uint64
	BaseDigestSHA256      string
	ExpectedReadbackCount int
	Status                string
	SwitchNotBefore       *time.Time
	PreviousNotAfter      *time.Time
	CompletedAt           *time.Time
}

// AuthoritySnapshotHistory holds the current forward-only revision/digest chain used to check
// predecessor continuity; the slice itself does not validate monotonicity or signatures.
// AuthoritySnapshotHistory хранит текущую forward-only revision/digest цепочку для проверки predecessor
// непрерывности; сам slice не проверяет монотонность и подписи.
type AuthoritySnapshotHistory struct {
	Current []RevisionDigest
}
