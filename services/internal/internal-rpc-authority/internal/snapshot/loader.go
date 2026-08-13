package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

var snapshotDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const (
	snapshotProtectedType = "mattercodex-internal-rpc-snapshot+jws"
	manifestBundleType    = "mattercodex-internal-rpc-manifest-trust+jws"
	// MaxPublisherSnapshotBytes ограничивает полный служебный snapshot и не
	// расширяет лимит обычного authorization JWS.
	MaxPublisherSnapshotBytes = 1 << 20
	maxSnapshotBytes          = MaxPublisherSnapshotBytes
	maxKeyFileBytes           = 64 << 10
)

// Role выбирает назначение загружаемого снимка.
type Role string

// Поддерживаемые роли локального authority-компонента.
const (
	RoleIssuer   Role = "issuer"
	RoleVerifier Role = "verifier"
)

// LoadOptions задаёт доверенные корни и пути подписанного снимка.
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

// Loaded содержит проверенный снимок и разделённые наборы ключей.
type Loaded struct {
	Policy model.PolicySnapshot
	Keys   service.KeyMaterial
}

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

type revisionDigest struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

type issuerKeySet struct {
	Issuer     string     `json:"issuer"`
	WorkloadID string     `json:"workload_id"`
	Keys       []keyEntry `json:"keys"`
}

type keyEntry struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

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

type manifestSignerKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

type proofTrustDocument struct {
	Version        int              `json:"v"`
	Purpose        string           `json:"purpose"`
	SourceRevision uint64           `json:"source_revision"`
	SourceDigest   string           `json:"source_digest_sha256"`
	Predecessor    revisionDigest   `json:"predecessor"`
	History        []revisionDigest `json:"history"`
	Keys           []proofTrustKey  `json:"keys"`
}

type proofTrustKey struct {
	Issuer     string          `json:"issuer"`
	Generation uint64          `json:"generation"`
	Status     string          `json:"status"`
	Purpose    string          `json:"purpose"`
	Audiences  []string        `json:"audiences"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
	JWK        json.RawMessage `json:"jwk"`
}

type policy struct {
	TrustDomain             string                   `json:"trust_domain"`
	DefaultDecision         string                   `json:"default_decision"`
	TokenTTLSeconds         int64                    `json:"token_ttl_seconds"`
	AllowedClockSkewSeconds int64                    `json:"allowed_clock_skew_seconds"`
	MaxCompactJWSBytes      int                      `json:"max_compact_jws_bytes"`
	ProofProducers          []authorityProofProducer `json:"authority_proof_producers"`
	OperationBindings       []operationBinding       `json:"operation_bindings"`
}

type authorityProofProducer struct {
	ProducerID                         string   `json:"producer_id"`
	CallerWorkloadID                   string   `json:"caller_workload_id"`
	CallerSPIFFEID                     string   `json:"caller_spiffe_id"`
	OwnerWorkloadID                    string   `json:"owner_workload_id"`
	OwnerSPIFFEID                      string   `json:"owner_spiffe_id"`
	FullMethod                         string   `json:"full_method"`
	TLSServerName                      string   `json:"tls_server_name"`
	TransportTrustBundleID             string   `json:"transport_trust_bundle_id"`
	ApplicationCredential              string   `json:"application_credential"`
	ApplicationCredentialMetadata      string   `json:"application_credential_metadata"`
	ApplicationCredentialIssuer        string   `json:"application_credential_issuer"`
	ApplicationCredentialAudience      string   `json:"application_credential_audience"`
	ApplicationCredentialTrustBundleID string   `json:"application_credential_trust_bundle_id"`
	AuthorityProofIssuer               string   `json:"authority_proof_issuer"`
	AuthorityProofAudience             string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID        string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds        int64    `json:"authority_proof_max_age_seconds"`
	DeadlineMilliseconds               int64    `json:"deadline_milliseconds"`
	MaxAttempts                        int      `json:"max_attempts"`
	RetryableGRPCCodes                 []string `json:"retryable_grpc_codes"`
	IdempotencyScope                   string   `json:"idempotency_scope"`
	AuthoritySources                   []string `json:"authority_sources"`
	AllowedOperationIDs                []string `json:"allowed_operation_ids"`
	ServerResolvedFields               []string `json:"server_resolved_fields"`
}

type operationBinding struct {
	OperationID         string    `json:"operation_id"`
	CallerWorkloadID    string    `json:"caller_workload_id"`
	CallerSPIFFEID      string    `json:"caller_spiffe_id"`
	Issuer              string    `json:"issuer"`
	TargetWorkloadID    string    `json:"target_workload_id"`
	TargetSPIFFEID      string    `json:"target_spiffe_id"`
	Audience            string    `json:"audience"`
	FullMethod          string    `json:"full_method"`
	TargetTLSServerName string    `json:"target_tls_server_name"`
	TargetTrustBundleID string    `json:"target_trust_bundle_id"`
	Permission          string    `json:"permission"`
	ProofProducerID     string    `json:"authority_proof_producer_id"`
	AuthoritySources    []string  `json:"authority_sources"`
	ProjectRequired     bool      `json:"project_required"`
	LocalCaller         localPeer `json:"local_caller"`
	LocalTarget         localPeer `json:"local_target"`
}

type localPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}

// Load проверяет цепочку корня, signer и истории, затем загружает снимок.
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
	compactRaw, err := readRegularFile(options.SnapshotJWSFile, maxSnapshotBytes, 0o004)
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
		signingRaw, err := readRegularFile(options.ContextPrivateJWKFile, maxKeyFileBytes, 0o007)
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
	bindings := make([]model.OperationBinding, 0, len(snapshot.Policy.OperationBindings))
	for _, binding := range snapshot.Policy.OperationBindings {
		if !bindingApplies(options.Role, options.WorkloadID, binding) {
			continue
		}
		producer, ok := producers[binding.ProofProducerID]
		if !ok {
			return Loaded{}, errors.New("operation binding references unknown authority proof producer")
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
			AuthorityProofIssuer:   producer.AuthorityProofIssuer,
			AuthorityProofAudience: producer.AuthorityProofAudience,
			AuthoritySources:       append([]string(nil), binding.AuthoritySources...),
			ProjectRequired:        binding.ProjectRequired,
			TokenTTLSeconds:        snapshot.Policy.TokenTTLSeconds,
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

func buildPolicySnapshot(
	snapshot document,
	current service.VerificationKeyRecord,
	sourceDigest string,
	bindings []model.OperationBinding,
) model.PolicySnapshot {
	return model.PolicySnapshot{
		Version:                 snapshot.Version,
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
		0o022,
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
		0o022,
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
			"urn:mattercodex:internal-rpc-authority:manifest-root" ||
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
		0o004,
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
			"urn:mattercodex:internal-rpc-authority:manifest-bundle" ||
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

func validateHistory(
	sourceRevision uint64,
	predecessor revisionDigest,
	history []revisionDigest,
) error {
	const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if len(history) > 32 {
		return errors.New("signed authority snapshot history exceeds the bounded window")
	}
	if sourceRevision == 1 {
		if predecessor.Revision != 0 || predecessor.DigestSHA256 != zeroDigest || len(history) != 0 {
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
				!now.Before(record.NotAfter) {
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

func loadProofTrust(
	path string,
	now time.Time,
	expectedSourceRevision uint64,
	expectedSourceDigest string,
) (map[string]service.VerificationKeyRecord, error) {
	raw, err := readRegularFile(path, maxKeyFileBytes, 0)
	if err != nil {
		return nil, err
	}
	var document proofTrustDocument
	if err := internalrpcauth.DecodeCanonicalJSON(raw, &document); err != nil ||
		document.Version != model.ContractVersion ||
		document.Purpose != "AUTHORITY_PROOF_VERIFICATION" ||
		document.SourceRevision == 0 ||
		!snapshotDigestPattern.MatchString(document.SourceDigest) ||
		document.SourceRevision != expectedSourceRevision ||
		document.SourceDigest != expectedSourceDigest ||
		len(document.Keys) == 0 ||
		len(document.Keys) > 32 {
		return nil, errors.New("authority proof trust document is invalid")
	}
	if err := validateHistory(
		document.SourceRevision,
		document.Predecessor,
		document.History,
	); err != nil {
		return nil, fmt.Errorf("authority proof trust history rejected: %w", err)
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
			!now.Before(record.NotAfter) {
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

func bindingApplies(role Role, workloadID string, binding operationBinding) bool {
	if role == RoleIssuer {
		return binding.CallerWorkloadID == workloadID
	}
	return binding.TargetWorkloadID == workloadID
}

func samePublicKey(left, right internalrpcauth.ES256Key) bool {
	if left.Public == nil || right.Public == nil {
		return false
	}
	leftBytes, leftErr := left.Public.Bytes()
	rightBytes, rightErr := right.Public.Bytes()
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func readRegularFile(path string, limit int64, forbiddenMode os.FileMode) ([]byte, error) {
	if path == "" {
		return nil, errors.New("file path is empty")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(filepath.Dir(path), resolved)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == "../" {
		return nil, errors.New("file symlink escapes its mounted directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("file size is outside the allowed boundary")
	}
	if forbiddenMode != 0 && info.Mode().Perm()&forbiddenMode != 0 {
		return nil, errors.New("file permissions are too broad")
	}
	return os.ReadFile(resolved)
}

func trimSingleTrailingNewline(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		return value[:len(value)-1]
	}
	return value
}
