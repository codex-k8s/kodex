package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/policydeclaration"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// snapshotDigestPattern accepts exactly 64 lowercase hexadecimal characters for declared SHA-256 pins;
// matching it is not proof that the document bytes have that digest.
// snapshotDigestPattern принимает ровно 64 строчных шестнадцатеричных символа для заявленной привязки
// SHA-256; совпадение не доказывает соответствие хеша байтам документа.
var snapshotDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const (
	// snapshotProtectedType pins the JWS protected type of full authority snapshots, separating them from
	// ordinary authorization contexts and trust bundles.
	// snapshotProtectedType закрепляет защищённый тип JWS полного снимка полномочий, отделяя его от обычных
	// контекстов авторизации и наборов доверия.
	snapshotProtectedType = "kodex-internal-rpc-snapshot+jws"
	// manifestBundleType pins the root-signed manifest verification bundle's protected type so another
	// signed document cannot substitute for it.
	// manifestBundleType закрепляет защищённый тип подписанного корнем набора проверки манифеста, не
	// позволяя заменить его другим подписанным документом.
	manifestBundleType = "kodex-internal-rpc-manifest-trust+jws"
	// MaxPublisherSnapshotBytes allows a complete service snapshot up to one MiB without increasing the
	// separate ordinary authorization-token limit.
	// MaxPublisherSnapshotBytes допускает полный служебный снимок размером до одного МиБ, не увеличивая
	// отдельный предел обычного токена авторизации.
	MaxPublisherSnapshotBytes = 1 << 20
	// maxSnapshotBytes is the local alias of the full snapshot byte budget used for bounded file reads and
	// complete snapshot JWS verification.
	// maxSnapshotBytes — локальный псевдоним бюджета полного снимка для ограниченного чтения файлов и
	// проверки его полного JWS.
	maxSnapshotBytes = MaxPublisherSnapshotBytes
	// maxKeyFileBytes limits individually mounted key and metadata files to 64 KiB; format, permissions and
	// trust validation remain separate checks.
	// maxKeyFileBytes ограничивает отдельные монтируемые файлы ключей и метаданных 64 КиБ; формат, права и
	// доверие проверяются отдельно.
	maxKeyFileBytes = 64 << 10
)

// Role selects the local issuer or verifier view of a signed snapshot; it does not grant an arbitrary
// caller a workload role.
// Role выбирает локальное представление выпускающей либо проверяющей стороны подписанного снимка, не
// выдавая произвольному вызывающему роль нагрузки.
type Role string

// Local roles select distinct workload policy views without granting authority.
// Локальные роли выбирают разные представления политики нагрузки, не выдавая полномочий.
const (
	// RoleIssuer selects caller-owned operation bindings, proof trust and a private current authorization
	// signing key during loading.
	// RoleIssuer выбирает при загрузке операции вызывающей стороны, доверие доказательствам и текущий
	// закрытый ключ подписи контекстов.
	RoleIssuer Role = "issuer"
	// RoleVerifier selects target-owned operation bindings and verification keys without loading a private
	// authorization signing key.
	// RoleVerifier выбирает операции целевой стороны и ключи проверки без загрузки закрытого ключа подписи
	// контекстов.
	RoleVerifier Role = "verifier"
)

// LoadOptions supplies the configured role/workload, independently pinned root files and mounted
// snapshot/key paths, with an optional evaluation clock; fields do not authenticate themselves.
// LoadOptions задаёт роль и нагрузку, независимо закреплённые корневые файлы, пути снимка и ключей и
// необязательные часы проверки; сами поля себя не удостоверяют.
type LoadOptions struct {
	Role                       Role
	WorkloadID                 string
	SnapshotJWSFile            string
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	ContextPrivateJWKFile      string
	ProofTrustJWKFile          string
	Now                        time.Time
}

// Loaded returns the checked policy view and separated signing/verification/proof key material; issuer
// results may contain a private signing key and must not be logged.
// Loaded возвращает проверенное представление политики и раздельные материалы подписи, проверки и
// доказательств; результат выпускающей стороны может содержать закрытый ключ и не журналируется.
type Loaded struct {
	Policy model.PolicySnapshot
	Keys   service.KeyMaterial
}

// document is the signed snapshot payload containing independent source/key/policy/signer revisions,
// lifetime, predecessor history, issuers and policy; decoding alone does not activate it.
// document представляет подписанное содержимое снимка с независимыми ревизиями источника, ключей,
// политики и подписанта, сроком, историей, подписантами и политикой; одно декодирование не активирует
// снимок.
type document struct {
	Version          int              `json:"v"`
	SourceRevision   uint64           `json:"source_revision"`
	KeySetRevision   uint64           `json:"key_set_revision"`
	PolicyRevision   uint64           `json:"policy_revision"`
	SignerGeneration uint64           `json:"signer_generation"`
	PublishedAt      int64            `json:"published_at"`
	ValidFrom        int64            `json:"valid_from"`
	ValidUntil       int64            `json:"valid_until"`
	Predecessor      revisionDigest   `json:"predecessor"`
	History          []revisionDigest `json:"history"`
	Issuers          []issuerKeySet   `json:"issuers"`
	Policy           policy           `json:"policy"`
}

// revisionDigest uses the shared exact revision/digest wire shape for signed trust history; monotonic
// acceptance still belongs to the persistent verifier.
// revisionDigest использует общую точную форму ревизии и хеша для подписанной истории доверия;
// монотонное принятие по-прежнему принадлежит устойчивой проверяющей стороне.
type revisionDigest = internalrpcauth.TrustRevisionDigest

// issuerKeySet groups public authorization keys under one issuer and workload identity; the loader
// enforces the current-key cardinality and audience binding.
// issuerKeySet объединяет открытые ключи авторизации по подписанту и нагрузке; число текущих ключей и
// привязку аудитории проверяет загрузчик.
type issuerKeySet struct {
	Issuer     string     `json:"issuer"`
	WorkloadID string     `json:"workload_id"`
	Keys       []keyEntry `json:"keys"`
}

// keyEntry carries one public JWK with lifecycle status, generation, purpose, audiences and validity
// interval inside the signed snapshot.
// keyEntry содержит один открытый JWK со статусом жизненного цикла, поколением, назначением, аудиториями
// и сроком внутри подписанного снимка.
type keyEntry struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

// manifestRootMetadata pins the mounted root's identity, generation, purpose, audience, key thumbprint,
// source metadata and lifetime; it is checked against the separately mounted public root.
// manifestRootMetadata закрепляет личность, поколение, назначение, аудиторию, отпечаток ключа,
// метаданные источника и срок корня; они сверяются с отдельно монтируемым открытым корнем.
type manifestRootMetadata struct {
	Version        int    `json:"v"`
	RootID         string `json:"root_id"`
	RootGeneration uint64 `json:"root_generation"`
	Purpose        string `json:"purpose"`
	Audience       string `json:"aud"`
	KeyID          string `json:"kid"`
	JWKThumbprint  string `json:"jwk_thumbprint_sha256"`
	SourceRevision uint64 `json:"source_revision"`
	SourceDigest   string `json:"source_digest_sha256"`
	NotBefore      int64  `json:"not_before"`
	NotAfter       int64  `json:"not_after"`
}

// manifestTrustBundle is the root-signed manifest-key registry with declared bundle digest, bounded
// predecessor history and publication lifetime; the selected signer must be exact CURRENT.
// manifestTrustBundle представляет подписанный корнем реестр ключей манифеста с заявленным хешем,
// ограниченной историей и сроком публикации; выбранный подписант должен быть точным CURRENT.
type manifestTrustBundle struct {
	Version        int                 `json:"v"`
	RootID         string              `json:"root_id"`
	RootGeneration uint64              `json:"root_generation"`
	Purpose        string              `json:"purpose"`
	Audience       string              `json:"aud"`
	BundleRevision uint64              `json:"bundle_revision"`
	BundleDigest   string              `json:"bundle_digest_sha256"`
	Predecessor    revisionDigest      `json:"predecessor"`
	History        []revisionDigest    `json:"history"`
	Keys           []manifestSignerKey `json:"keys"`
	PublishedAt    int64               `json:"published_at"`
	ValidUntil     int64               `json:"valid_until"`
}

// manifestSignerKey stores public manifest-key identity, thumbprint, lifecycle generation/status and
// time bounds; private signing material is not part of this wire record.
// manifestSignerKey хранит личность и отпечаток открытого ключа манифеста, поколение, статус и сроки;
// закрытый материал подписи в запись не входит.
type manifestSignerKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// proofTrustDocument aliases the shared authority-proof trust schema, preserving its canonical wire
// representation rather than introducing a second local definition.
// proofTrustDocument ссылается на общую схему доверия доказательствам полномочий, сохраняя каноническое
// представление вместо второго локального определения.
type proofTrustDocument = internalrpcauth.AuthorityProofTrustDocument

// proofTrustKey aliases the shared public proof-key record used by publication and loading; the alias
// itself performs no key validation.
// proofTrustKey ссылается на общую запись открытого ключа доказательств для публикации и загрузки; сам
// псевдоним ключ не проверяет.
type proofTrustKey = internalrpcauth.AuthorityProofTrustKey

// policy carries the signed machine-policy envelope and closed producer/operation declarations; role
// projection and domain validation remain separate from JSON decoding.
// policy содержит подписанную машинную политику и закрытые декларации производителей и операций; выбор
// по роли и доменная проверка отделены от разбора JSON.
type policy struct {
	AuthorityABIVersion     uint32                   `json:"authority_abi_version"`
	TrustDomain             string                   `json:"trust_domain"`
	DefaultDecision         string                   `json:"default_decision"`
	TokenTTLSeconds         int64                    `json:"token_ttl_seconds"`
	AllowedClockSkewSeconds int64                    `json:"allowed_clock_skew_seconds"`
	MaxCompactJWSBytes      int                      `json:"max_compact_jws_bytes"`
	ProofProducers          []authorityProofProducer `json:"authority_proof_producers"`
	OperationBindings       []operationBinding       `json:"operation_bindings"`
}

// authorityProofProducer keeps the snapshot loader's nominal producer record
// over the common machine-policy field layout; signed provenance and role-specific
// projection remain loader/domain checks rather than properties of the type.
// authorityProofProducer сохраняет отдельную запись загрузчика снимка на общей
// форме полей машинной политики; подписанное происхождение и выбор по роли
// проверяются загрузчиком и доменом, а не обеспечиваются самим типом.
type authorityProofProducer policydeclaration.ProofProducer

// operationBinding pins caller, target, full RPC, permission, proof producer, local peers and
// request/continuation profiles in the signed policy; the loader projects only the applicable workload
// role.
// operationBinding закрепляет в подписанной политике вызывающую и целевую стороны, полный RPC, право,
// производителя доказательств, локальные стороны и профили запроса и продолжения; загрузчик выбирает
// лишь применимую роль нагрузки.
type operationBinding struct {
	OperationID         string               `json:"operation_id"`
	CallerWorkloadID    string               `json:"caller_workload_id"`
	CallerSPIFFEID      string               `json:"caller_spiffe_id"`
	Issuer              string               `json:"issuer"`
	TargetWorkloadID    string               `json:"target_workload_id"`
	TargetSPIFFEID      string               `json:"target_spiffe_id"`
	Audience            string               `json:"audience"`
	FullMethod          string               `json:"full_method"`
	TargetTLSServerName string               `json:"target_tls_server_name"`
	TargetTrustBundleID string               `json:"target_trust_bundle_id"`
	Permission          string               `json:"permission"`
	ProofProducerID     string               `json:"authority_proof_producer_id"`
	AuthoritySources    []string             `json:"authority_sources"`
	ProjectRequired     bool                 `json:"project_required"`
	LocalCaller         localPeer            `json:"local_caller"`
	LocalTarget         localPeer            `json:"local_target"`
	RequestProfile      requestProfile       `json:"request_profile"`
	Continuation        *continuationProfile `json:"continuation,omitempty"`
}

// requestProfile names request-binding dimensions required by policy, not caller-supplied resource,
// version, attempt or idempotency values.
// requestProfile называет требуемые политикой измерения привязки, а не переданные вызывающим значения
// ресурса, версии, попытки или идемпотентности.
type requestProfile struct {
	Mode        string `json:"mode"`
	Resource    string `json:"resource"`
	Version     string `json:"version"`
	Attempt     string `json:"attempt"`
	Idempotency string `json:"idempotency"`
}

// continuationProfile restricts a child operation to one exact parent operation and full RPC method; it
// does not authorize unrelated delegation edges.
// continuationProfile ограничивает дочернюю операцию одной точной родительской операцией и полным
// методом RPC, не разрешая посторонние рёбра делегирования.
type continuationProfile struct {
	ParentOperationID string `json:"parent_operation_id"`
	ParentFullMethod  string `json:"parent_full_method"`
}

// localPeer carries policy-owned local UID, primary GID and shared filesystem GID pins for Unix-socket
// boundaries; these values are not observed peer credentials by themselves.
// localPeer содержит закреплённые политикой UID, основной GID и общий файловый GID для границ
// Unix-сокетов; сами значения не являются наблюдаемыми учётными данными стороны.
type localPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}

// Load verifies the independently rooted manifest signer and canonical signed snapshot, checks
// revision/lifetime/history and key bindings, then projects role-specific policy and key material. It
// does not persist or activate a rollback high-watermark.
// Load проверяет независимо укоренённого подписанта и канонический подписанный снимок, ревизии, сроки,
// историю и привязки ключей, затем выбирает политику и ключи роли. Устойчивую границу откатов функция не
// сохраняет и не активирует.
func Load(options LoadOptions) (Loaded, error) {
	if options.Role != RoleIssuer && options.Role != RoleVerifier {
		return Loaded{}, errors.New("invalid authority snapshot role")
	}
	if options.WorkloadID == "" {
		return Loaded{}, errors.New("authority workload id is required")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	compactRaw, err := readRegularFile(options.SnapshotJWSFile, maxSnapshotBytes)
	if err != nil {
		return Loaded{}, fmt.Errorf("read signed authority snapshot: %w", err)
	}
	compact := string(trimSingleTrailingNewline(compactRaw))
	manifestKey, manifestGeneration, err := loadManifestVerificationKey(
		options,
		compact,
		now,
	)
	if err != nil {
		return Loaded{}, err
	}
	verified, err := internalrpcauth.VerifyCanonicalJSONWithLimit(
		compact,
		manifestKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  snapshotProtectedType,
			KeyID: manifestKey.KeyID,
		},
		maxSnapshotBytes,
	)
	if err != nil {
		return Loaded{}, fmt.Errorf("verify signed authority snapshot: %w", err)
	}
	var snapshot document
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &snapshot); err != nil {
		return Loaded{}, fmt.Errorf("decode signed authority snapshot: %w", err)
	}
	if snapshot.Version != model.ContractVersion ||
		snapshot.Policy.AuthorityABIVersion != model.AuthorityABIVersion ||
		snapshot.SourceRevision == 0 ||
		snapshot.KeySetRevision == 0 ||
		snapshot.PolicyRevision == 0 ||
		snapshot.SignerGeneration == 0 ||
		snapshot.SignerGeneration != manifestGeneration ||
		snapshot.ValidUntil <= snapshot.ValidFrom ||
		now.Before(time.Unix(snapshot.ValidFrom, 0)) ||
		!now.Before(time.Unix(snapshot.ValidUntil, 0)) {
		return Loaded{}, errors.New("signed authority snapshot is outside its validity or revision boundary")
	}
	if err := validateHistory(snapshot.SourceRevision, snapshot.Predecessor, snapshot.History); err != nil {
		return Loaded{}, err
	}
	digest := sha256.Sum256(verified.CanonicalPayload)
	sourceDigest := hex.EncodeToString(digest[:])
	verificationKeys, ownCurrent, err := loadIssuerKeys(
		snapshot.Issuers,
		snapshot.Policy.OperationBindings,
		options.WorkloadID,
		options.Role == RoleIssuer,
		now,
	)
	if err != nil {
		return Loaded{}, err
	}
	proofKeys := make(map[string]service.VerificationKeyRecord)
	if options.Role == RoleIssuer {
		proofKeys, err = loadProofTrust(
			options.ProofTrustJWKFile,
			now,
			snapshot.SourceRevision,
			sourceDigest,
		)
		if err != nil {
			return Loaded{}, fmt.Errorf("load authority proof trust: %w", err)
		}
	}
	var signingKey internalrpcauth.ES256Key
	if options.Role == RoleIssuer {
		signingRaw, err := readRegularFile(options.ContextPrivateJWKFile, maxKeyFileBytes)
		if err != nil {
			return Loaded{}, fmt.Errorf("read authorization signing key: %w", err)
		}
		signingKey, err = internalrpcauth.ParsePrivateJWK(signingRaw)
		if err != nil {
			return Loaded{}, fmt.Errorf("parse authorization signing key: %w", err)
		}
		if signingKey.KeyID != ownCurrent.Key.KeyID ||
			!samePublicKey(signingKey, ownCurrent.Key) {
			return Loaded{}, errors.New("authorization signing key does not match CURRENT snapshot key")
		}
	}
	producers := make(map[string]authorityProofProducer, len(snapshot.Policy.ProofProducers))
	for _, producer := range snapshot.Policy.ProofProducers {
		if producer.ProducerID == "" {
			return Loaded{}, errors.New("authority proof producer id is empty")
		}
		if _, exists := producers[producer.ProducerID]; exists {
			return Loaded{}, errors.New("duplicate authority proof producer")
		}
		producers[producer.ProducerID] = producer
	}
	selectedBindings, err := selectRoleBindings(options.Role, options.WorkloadID, snapshot.Policy.OperationBindings)
	if err != nil {
		return Loaded{}, err
	}
	bindings := make([]model.OperationBinding, 0, len(selectedBindings))
	for _, binding := range selectedBindings {
		var proofIssuer, proofAudience string
		if binding.Continuation == nil {
			producer, ok := producers[binding.ProofProducerID]
			if !ok {
				return Loaded{}, errors.New("operation binding references unknown authority proof producer")
			}
			proofIssuer, proofAudience = producer.AuthorityProofIssuer, producer.AuthorityProofAudience
		} else if binding.ProofProducerID != "" {
			return Loaded{}, errors.New("continuation binding must not reference authority proof producer")
		}
		bindings = append(bindings, model.OperationBinding{
			OperationID:            binding.OperationID,
			CallerWorkloadID:       binding.CallerWorkloadID,
			CallerSPIFFEID:         binding.CallerSPIFFEID,
			Issuer:                 binding.Issuer,
			TargetWorkloadID:       binding.TargetWorkloadID,
			TargetSPIFFEID:         binding.TargetSPIFFEID,
			Audience:               binding.Audience,
			FullMethod:             binding.FullMethod,
			Permission:             binding.Permission,
			AuthorityProofIssuer:   proofIssuer,
			AuthorityProofAudience: proofAudience,
			AuthoritySources:       append([]string(nil), binding.AuthoritySources...),
			ProjectRequired:        binding.ProjectRequired,
			TokenTTLSeconds:        snapshot.Policy.TokenTTLSeconds,
			RequestProfile: model.RequestProfile{
				Mode: binding.RequestProfile.Mode, Resource: binding.RequestProfile.Resource,
				Version: binding.RequestProfile.Version, Attempt: binding.RequestProfile.Attempt,
				Idempotency: binding.RequestProfile.Idempotency,
			},
			Continuation: castContinuationProfile(binding.Continuation),
		})
	}
	if len(bindings) == 0 && len(snapshot.Policy.OperationBindings) != 0 {
		return Loaded{}, errors.New("signed authority snapshot has no binding for configured workload role")
	}
	policySnapshot := buildPolicySnapshot(
		snapshot,
		ownCurrent,
		sourceDigest,
		bindings,
	)
	return Loaded{
		Policy: policySnapshot,
		Keys: service.KeyMaterial{
			SigningKey:       signingKey,
			VerificationKeys: verificationKeys,
			ProofKeys:        proofKeys,
		},
	}, nil
}

// buildPolicySnapshot maps checked snapshot metadata and selected bindings into the domain policy, using
// the current workload key's generation rather than the manifest signer's generation.
// buildPolicySnapshot преобразует проверенные метаданные и выбранные привязки в доменную политику,
// используя поколение текущего ключа нагрузки, а не подписанта манифеста.
func buildPolicySnapshot(
	snapshot document,
	current service.VerificationKeyRecord,
	sourceDigest string,
	bindings []model.OperationBinding,
) model.PolicySnapshot {
	return model.PolicySnapshot{
		Version:                 snapshot.Version,
		AuthorityABIVersion:     snapshot.Policy.AuthorityABIVersion,
		TrustDomain:             snapshot.Policy.TrustDomain,
		DefaultDecision:         snapshot.Policy.DefaultDecision,
		TokenTTLSeconds:         snapshot.Policy.TokenTTLSeconds,
		AllowedClockSkewSeconds: snapshot.Policy.AllowedClockSkewSeconds,
		MaxCompactJWSBytes:      snapshot.Policy.MaxCompactJWSBytes,
		Issuer:                  current.Issuer,
		SignerKeyID:             current.Key.KeyID,
		SourceRevision:          snapshot.SourceRevision,
		SourceDigestSHA256:      sourceDigest,
		PredecessorRevision:     snapshot.Predecessor.Revision,
		PredecessorDigestSHA256: snapshot.Predecessor.DigestSHA256,
		KeySetRevision:          snapshot.KeySetRevision,
		PolicyRevision:          snapshot.PolicyRevision,
		SignerGeneration:        current.Generation,
		History:                 modelHistory(snapshot.History),
		OperationBindings:       bindings,
	}
}

// castContinuationProfile preserves nil or allocates a domain copy of the exact parent operation/method
// pair without validating the edge itself.
// castContinuationProfile сохраняет nil либо выделяет доменную копию точной пары родительской операции и
// метода без самостоятельной проверки ребра.
func castContinuationProfile(value *continuationProfile) *model.ContinuationProfile {
	if value == nil {
		return nil
	}
	return &model.ContinuationProfile{ParentOperationID: value.ParentOperationID, ParentFullMethod: value.ParentFullMethod}
}

// loadManifestVerificationKey selects a rooted manifest key for the snapshot protected type with the
// complete-snapshot header budget; actual payload verification follows in Load.
// loadManifestVerificationKey выбирает укоренённый ключ манифеста для типа снимка с бюджетом полного
// снимка при чтении заголовка; содержимое затем проверяет Load.
func loadManifestVerificationKey(
	options LoadOptions,
	snapshotCompact string,
	now time.Time,
) (internalrpcauth.ES256Key, uint64, error) {
	return loadManifestVerificationKeyForType(
		options,
		snapshotCompact,
		now,
		snapshotProtectedType,
		maxSnapshotBytes,
	)
}

// loadManifestVerificationKeyForType checks pinned root metadata, the root-signed bundle and bounded
// history, then selects the single active CURRENT key matching the requested document type/header. It
// resolves trust without activating persistent state.
// loadManifestVerificationKeyForType проверяет закреплённые метаданные корня, подписанный им набор и
// ограниченную историю, затем выбирает единственный активный CURRENT, соответствующий типу и заголовку
// документа. Доверие разрешается без активации устойчивого состояния.
func loadManifestVerificationKeyForType(
	options LoadOptions,
	signedCompact string,
	now time.Time,
	expectedProtectedType string,
	maxCompactBytes int,
) (internalrpcauth.ES256Key, uint64, error) {
	rootRaw, err := readRegularFile(
		options.ManifestRootPublicJWKFile,
		maxKeyFileBytes,
	)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("read pinned manifest root key: %w", err)
	}
	rootKey, err := internalrpcauth.ParsePublicJWK(rootRaw)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("parse pinned manifest root key: %w", err)
	}
	metadataRaw, err := readRegularFile(
		options.ManifestRootMetadataFile,
		maxKeyFileBytes,
	)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("read pinned manifest root metadata: %w", err)
	}
	var metadata manifestRootMetadata
	if err := internalrpcauth.DecodeCanonicalJSON(metadataRaw, &metadata); err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("decode pinned manifest root metadata: %w", err)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(rootKey)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("derive pinned manifest root thumbprint: %w", err)
	}
	if metadata.Version != model.ContractVersion ||
		metadata.RootID != "internal-rpc-authority-manifest-root-v1" ||
		metadata.RootGeneration == 0 ||
		metadata.Purpose != "AUTHORITY_SNAPSHOT_MANIFEST_ROOT" ||
		metadata.Audience !=
			"urn:kodex:internal-rpc-authority:manifest-root" ||
		metadata.KeyID != rootKey.KeyID ||
		metadata.JWKThumbprint != thumbprint ||
		metadata.SourceRevision == 0 ||
		!snapshotDigestPattern.MatchString(metadata.SourceDigest) ||
		now.Before(time.Unix(metadata.NotBefore, 0)) ||
		!now.Before(time.Unix(metadata.NotAfter, 0)) {
		return internalrpcauth.ES256Key{}, 0, errors.New("pinned manifest root metadata rejected")
	}
	bundleRaw, err := readRegularFile(
		options.ManifestTrustBundleJWSFile,
		maxSnapshotBytes,
	)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("read manifest trust bundle: %w", err)
	}
	bundleCompact := string(trimSingleTrailingNewline(bundleRaw))
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		bundleCompact,
		rootKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  manifestBundleType,
			KeyID: rootKey.KeyID,
		},
	)
	if err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("verify manifest trust bundle root signature: %w", err)
	}
	var bundle manifestTrustBundle
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&bundle,
	); err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("decode manifest trust bundle: %w", err)
	}
	if bundle.Version != model.ContractVersion ||
		bundle.RootID != metadata.RootID ||
		bundle.RootGeneration != metadata.RootGeneration ||
		bundle.Purpose != "AUTHORITY_SNAPSHOT_MANIFEST_VERIFICATION" ||
		bundle.Audience !=
			"urn:kodex:internal-rpc-authority:manifest-bundle" ||
		bundle.BundleRevision == 0 ||
		bundle.BundleRevision < metadata.SourceRevision ||
		!snapshotDigestPattern.MatchString(bundle.BundleDigest) ||
		bundle.PublishedAt > now.Add(5*time.Second).Unix() ||
		bundle.ValidUntil <= bundle.PublishedAt ||
		bundle.ValidUntil > bundle.PublishedAt+366*24*60*60 ||
		!now.Before(time.Unix(bundle.ValidUntil, 0)) {
		return internalrpcauth.ES256Key{}, 0, errors.New("manifest trust bundle metadata rejected")
	}
	if err := validateHistory(
		bundle.BundleRevision,
		bundle.Predecessor,
		bundle.History,
	); err != nil {
		return internalrpcauth.ES256Key{}, 0, fmt.Errorf("manifest trust bundle history rejected: %w", err)
	}
	snapshotHeader, err := internalrpcauth.ParseProtectedHeaderWithLimit(
		signedCompact,
		maxCompactBytes,
	)
	if err != nil || snapshotHeader.Type != expectedProtectedType {
		return internalrpcauth.ES256Key{}, 0, errors.New("signed document header rejected before manifest key resolution")
	}
	currentCount := 0
	var selected internalrpcauth.ES256Key
	var selectedGeneration uint64
	for _, entry := range bundle.Keys {
		key, parseErr := internalrpcauth.ParsePublicJWK(entry.PublicJWK)
		if parseErr != nil {
			return internalrpcauth.ES256Key{}, 0, fmt.Errorf("parse manifest signer key: %w", parseErr)
		}
		entryThumbprint, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbErr != nil ||
			key.KeyID != entry.KeyID ||
			entry.Thumbprint != entryThumbprint ||
			entry.Generation == 0 ||
			(entry.Status != "CURRENT" &&
				entry.Status != "NEXT" &&
				entry.Status != "PREVIOUS") ||
			entry.NotAfter <= entry.NotBefore {
			return internalrpcauth.ES256Key{}, 0, errors.New("manifest signer record rejected")
		}
		if entry.Status == "CURRENT" {
			currentCount++
		}
		if entry.KeyID == snapshotHeader.KeyID {
			if entry.Status != "CURRENT" ||
				now.Before(time.Unix(entry.NotBefore, 0)) ||
				!now.Before(time.Unix(entry.NotAfter, 0)) ||
				selected.Public != nil {
				return internalrpcauth.ES256Key{}, 0, errors.New("snapshot manifest signer is ambiguous or inactive")
			}
			selected = key
			selectedGeneration = entry.Generation
		}
	}
	if currentCount != 1 || selected.Public == nil {
		return internalrpcauth.ES256Key{}, 0, errors.New("exact CURRENT snapshot manifest signer is unavailable")
	}
	return selected, selectedGeneration, nil
}

// validateHistory checks a bootstrap-zero predecessor or a contiguous bounded history of at most 32
// revisions ending at the exact predecessor; it does not compare a persisted local high-watermark.
// validateHistory проверяет нулевого начального предшественника либо непрерывную историю до 32 ревизий с
// точным последним предшественником; локальную устойчивую границу версий она не сравнивает.
func validateHistory(
	sourceRevision uint64,
	predecessor revisionDigest,
	history []revisionDigest,
) error {
	if len(history) > 32 {
		return errors.New("signed authority snapshot history exceeds the bounded window")
	}
	if sourceRevision == 1 {
		if predecessor.Revision != 0 || predecessor.DigestSHA256 != zeroSHA256 || len(history) != 0 {
			return errors.New("bootstrap authority snapshot predecessor or history is invalid")
		}
		return nil
	}
	if predecessor.Revision != sourceRevision-1 ||
		!snapshotDigestPattern.MatchString(predecessor.DigestSHA256) ||
		len(history) == 0 {
		return errors.New("signed authority snapshot predecessor is invalid")
	}
	firstRevision := sourceRevision - uint64(len(history))
	seen := make(map[uint64]struct{}, len(history))
	for index, entry := range history {
		expectedRevision := firstRevision + uint64(index)
		if entry.Revision != expectedRevision ||
			!snapshotDigestPattern.MatchString(entry.DigestSHA256) {
			return errors.New("signed authority snapshot history is gapped or malformed")
		}
		if _, duplicate := seen[entry.Revision]; duplicate {
			return errors.New("signed authority snapshot history contains a duplicate revision")
		}
		seen[entry.Revision] = struct{}{}
	}
	last := history[len(history)-1]
	if last.Revision != predecessor.Revision ||
		last.DigestSHA256 != predecessor.DigestSHA256 {
		return errors.New("signed authority snapshot history does not end at the predecessor")
	}
	return nil
}

// modelHistory allocates a domain revision/digest slice in input order without sharing its backing array
// or adding validation.
// modelHistory выделяет доменный срез ревизий и хешей в исходном порядке без общего базового массива и
// дополнительной проверки.
func modelHistory(values []revisionDigest) []model.RevisionDigest {
	result := make([]model.RevisionDigest, 0, len(values))
	for _, value := range values {
		result = append(result, model.RevisionDigest{
			Revision:     value.Revision,
			DigestSHA256: value.DigestSHA256,
		})
	}
	return result
}

// loadIssuerKeys parses unique public key IDs, exact policy audience sets and validity, requires one
// CURRENT per issuer set and optionally the configured workload's key set, then returns verification
// records and the selected current key.
// loadIssuerKeys разбирает уникальные открытые ключи, точные аудитории политики и сроки, требует один
// CURRENT на набор подписанта и при необходимости набор заданной нагрузки, затем возвращает записи
// проверки и выбранный текущий ключ.
func loadIssuerKeys(
	keySets []issuerKeySet,
	bindings []operationBinding,
	workloadID string,
	requireOwnCurrent bool,
	now time.Time,
) (
	map[string]service.VerificationKeyRecord,
	service.VerificationKeyRecord,
	error,
) {
	keys := make(map[string]service.VerificationKeyRecord)
	var ownCurrent service.VerificationKeyRecord
	audiencesByIssuer := make(map[string]map[string]struct{})
	for _, binding := range bindings {
		if audiencesByIssuer[binding.Issuer] == nil {
			audiencesByIssuer[binding.Issuer] = make(map[string]struct{})
		}
		audiencesByIssuer[binding.Issuer][binding.Audience] = struct{}{}
	}
	for _, keySet := range keySets {
		currentCount := 0
		for _, entry := range keySet.Keys {
			key, err := internalrpcauth.ParsePublicJWK(entry.JWK)
			if err != nil {
				return nil, service.VerificationKeyRecord{}, fmt.Errorf("parse snapshot public key: %w", err)
			}
			if _, duplicate := keys[key.KeyID]; duplicate {
				return nil, service.VerificationKeyRecord{}, errors.New("snapshot key id is not globally unique")
			}
			audiences := stringSet(entry.Audiences)
			record := service.VerificationKeyRecord{
				Key:        key,
				Issuer:     keySet.Issuer,
				Generation: entry.Generation,
				Status:     entry.Status,
				Purpose:    entry.Purpose,
				Audiences:  audiences,
				NotBefore:  time.Unix(entry.NotBefore, 0).UTC(),
				NotAfter:   time.Unix(entry.NotAfter, 0).UTC(),
			}
			if record.Generation == 0 ||
				record.Purpose != "AUTHORIZATION_CONTEXT" ||
				len(record.Audiences) == 0 ||
				!sameStringSet(record.Audiences, audiencesByIssuer[keySet.Issuer]) ||
				!record.NotBefore.Before(record.NotAfter) ||
				now.Before(record.NotBefore) ||
				(record.Status != "PREVIOUS" && !now.Before(record.NotAfter)) {
				return nil, service.VerificationKeyRecord{}, errors.New("snapshot key metadata is invalid")
			}
			keys[key.KeyID] = record
			if entry.Status == "CURRENT" {
				currentCount++
				if ownCurrent.Key.Public == nil {
					ownCurrent = record
				}
				if keySet.WorkloadID == workloadID {
					ownCurrent = record
				}
			}
		}
		if currentCount != 1 {
			return nil, service.VerificationKeyRecord{}, errors.New("issuer key set must contain exactly one CURRENT key")
		}
	}
	if requireOwnCurrent {
		matched := false
		for _, keySet := range keySets {
			if keySet.WorkloadID == workloadID {
				matched = true
				break
			}
		}
		if !matched {
			return nil, service.VerificationKeyRecord{}, errors.New("configured issuer workload has no CURRENT key")
		}
	}
	if ownCurrent.Key.Public == nil {
		return nil, service.VerificationKeyRecord{}, errors.New("configured workload has no CURRENT issuer key")
	}
	return keys, ownCurrent, nil
}

// loadProofTrust reads the shared proof-trust format pinned to exact snapshot revision/digest, validates
// bounded public key records and checks that each encountered CURRENT issuer count is one; it does not
// activate trust or mint proofs.
// loadProofTrust читает общий формат доверия с точной ревизией и хешем снимка, проверяет ограниченные
// записи открытых ключей и единичность каждого встреченного счётчика CURRENT по подписанту; доверие не
// активирует и доказательств не выпускает.
func loadProofTrust(
	path string,
	now time.Time,
	expectedSourceRevision uint64,
	expectedSourceDigest string,
) (map[string]service.VerificationKeyRecord, error) {
	raw, err := readRegularFile(path, maxKeyFileBytes)
	if err != nil {
		return nil, err
	}
	document, err := internalrpcauth.DecodeAuthorityProofTrustDocument(raw)
	if err != nil ||
		document.SourceRevision == 0 ||
		!snapshotDigestPattern.MatchString(document.SourceDigest) ||
		document.SourceRevision != expectedSourceRevision ||
		document.SourceDigest != expectedSourceDigest ||
		len(document.Keys) == 0 ||
		len(document.Keys) > 32 {
		return nil, errors.New("authority proof trust document is invalid")
	}
	result := make(map[string]service.VerificationKeyRecord, len(document.Keys))
	currentByIssuer := make(map[string]int)
	for _, entry := range document.Keys {
		key, err := internalrpcauth.ParsePublicJWK(entry.JWK)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[key.KeyID]; duplicate {
			return nil, errors.New("duplicate authority proof key id")
		}
		record := service.VerificationKeyRecord{
			Key:        key,
			Issuer:     entry.Issuer,
			Generation: entry.Generation,
			Status:     entry.Status,
			Purpose:    entry.Purpose,
			Audiences:  stringSet(entry.Audiences),
			NotBefore:  time.Unix(entry.NotBefore, 0).UTC(),
			NotAfter:   time.Unix(entry.NotAfter, 0).UTC(),
		}
		if record.Issuer == "" ||
			record.Generation == 0 ||
			record.Purpose != "AUTHORITY_PROOF" ||
			len(record.Audiences) == 0 ||
			(record.Status != "CURRENT" &&
				record.Status != "NEXT" &&
				record.Status != "PREVIOUS") ||
			!record.NotBefore.Before(record.NotAfter) ||
			now.Before(record.NotBefore) ||
			(record.Status != "PREVIOUS" && !now.Before(record.NotAfter)) {
			return nil, errors.New("authority proof key metadata is invalid")
		}
		if record.Status == "CURRENT" {
			currentByIssuer[record.Issuer]++
		}
		result[key.KeyID] = record
	}
	for issuer, count := range currentByIssuer {
		if issuer == "" || count != 1 {
			return nil, errors.New("authority proof issuer must have exactly one CURRENT key")
		}
	}
	return result, nil
}

// stringSet copies nonempty strings into a set, dropping empty and repeated entries without otherwise
// normalizing their contents.
// stringSet копирует непустые строки в множество, отбрасывая пустые и повторные элементы без иной
// нормализации.
func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}

// sameStringSet compares exact membership and cardinality without depending on map order; nil and empty
// sets compare equal.
// sameStringSet сравнивает точные состав и размер без зависимости от порядка карты; nil и пустое
// множество равны.
func sameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for value := range left {
		if _, ok := right[value]; !ok {
			return false
		}
	}
	return true
}

// bindingApplies selects caller workload for issuer role and target workload otherwise; callers validate
// the role before using this selector.
// bindingApplies выбирает нагрузку вызывающей стороны для выпускающей роли и целевую нагрузку иначе;
// допустимость роли проверяет вызывающий.
func bindingApplies(role Role, workloadID string, binding operationBinding) bool {
	if role == RoleIssuer {
		return binding.CallerWorkloadID == workloadID
	}
	return binding.TargetWorkloadID == workloadID
}

// samePublicKey rejects missing or unencodable public keys and compares their encoded public points,
// ignoring key IDs and private scalars.
// samePublicKey отклоняет отсутствующие или некодируемые открытые ключи и сравнивает закодированные
// открытые точки без учёта идентификаторов и закрытых скаляров.
func samePublicKey(left, right internalrpcauth.ES256Key) bool {
	if left.Public == nil || right.Public == nil {
		return false
	}
	leftBytes, leftErr := left.Public.Bytes()
	rightBytes, rightErr := right.Public.Bytes()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

// readRegularFile rejects an empty path and delegates bounded read-only file validation to securefile,
// returning fixed diagnostics rather than exposing failed file contents.
// readRegularFile отклоняет пустой путь и передаёт securefile ограниченную проверку файла только для
// чтения, возвращая фиксированную диагностику вместо содержимого ошибочного файла.
func readRegularFile(path string, limit int64) ([]byte, error) {
	if path == "" {
		return nil, errors.New("file path is empty")
	}
	value, err := securefile.Read(path, limit)
	if err != nil {
		return nil, errors.New("file type, mode or size is invalid")
	}
	return value, nil
}

// trimSingleTrailingNewline removes at most one final LF byte by subslicing; it does not trim CR,
// repeated newlines or other whitespace and may share the input storage.
// trimSingleTrailingNewline удаляет не более одного завершающего LF срезом; CR, повторные переводы
// строки и прочие пробелы не удаляются, память может оставаться общей с входом.
func trimSingleTrailingNewline(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}
