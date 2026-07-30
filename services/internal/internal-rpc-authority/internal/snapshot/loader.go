package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
)

const (
	snapshotProtectedType = "mattercodex-internal-rpc-snapshot+jws"
	maxSnapshotBytes      = 1 << 20
	maxKeyFileBytes       = 64 << 10
)

type Role string

const (
	RoleIssuer   Role = "issuer"
	RoleVerifier Role = "verifier"
)

type LoadOptions struct {
	Role                   Role
	WorkloadID             string
	SnapshotJWSFile        string
	ManifestPublicJWKFile  string
	ContextPrivateJWKFile  string
	ProofTrustJWKFile      string
	ReadbackPrivateJWKFile string
	Now                    time.Time
}

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
	Status string          `json:"status"`
	JWK    json.RawMessage `json:"jwk"`
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
	manifestRaw, err := readRegularFile(options.ManifestPublicJWKFile, maxKeyFileBytes, 0)
	if err != nil {
		return Loaded{}, fmt.Errorf("read manifest verification key: %w", err)
	}
	manifestKey, err := internalrpcauth.ParsePublicJWK(manifestRaw)
	if err != nil {
		return Loaded{}, fmt.Errorf("parse manifest verification key: %w", err)
	}
	compactRaw, err := readRegularFile(options.SnapshotJWSFile, maxSnapshotBytes, 0o044)
	if err != nil {
		return Loaded{}, fmt.Errorf("read signed authority snapshot: %w", err)
	}
	compact := string(trimSingleTrailingNewline(compactRaw))
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		manifestKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  snapshotProtectedType,
			KeyID: manifestKey.KeyID,
		},
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
		snapshot.ValidUntil <= snapshot.ValidFrom ||
		now.Before(time.Unix(snapshot.ValidFrom, 0)) ||
		!now.Before(time.Unix(snapshot.ValidUntil, 0)) {
		return Loaded{}, errors.New("signed authority snapshot is outside its validity or revision boundary")
	}
	digest := sha256.Sum256(verified.CanonicalPayload)
	sourceDigest := hex.EncodeToString(digest[:])
	verificationKeys, keyIssuers, ownCurrent, err := loadIssuerKeys(
		snapshot.Issuers,
		options.WorkloadID,
		options.Role == RoleIssuer,
	)
	if err != nil {
		return Loaded{}, err
	}
	proofKeys := make(map[string]internalrpcauth.ES256Key)
	if options.Role == RoleIssuer {
		proofKeys, err = loadPublicKeySet(options.ProofTrustJWKFile)
		if err != nil {
			return Loaded{}, fmt.Errorf("load authority proof trust: %w", err)
		}
	}
	readbackRaw, err := readRegularFile(options.ReadbackPrivateJWKFile, maxKeyFileBytes, 0o077)
	if err != nil {
		return Loaded{}, fmt.Errorf("read readback private key: %w", err)
	}
	readbackKey, err := internalrpcauth.ParsePrivateJWK(readbackRaw)
	if err != nil {
		return Loaded{}, fmt.Errorf("parse readback private key: %w", err)
	}
	var signingKey internalrpcauth.ES256Key
	if options.Role == RoleIssuer {
		signingRaw, err := readRegularFile(options.ContextPrivateJWKFile, maxKeyFileBytes, 0o077)
		if err != nil {
			return Loaded{}, fmt.Errorf("read authorization signing key: %w", err)
		}
		signingKey, err = internalrpcauth.ParsePrivateJWK(signingRaw)
		if err != nil {
			return Loaded{}, fmt.Errorf("parse authorization signing key: %w", err)
		}
		if signingKey.KeyID != ownCurrent.KeyID ||
			!samePublicKey(signingKey, ownCurrent) {
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
	issuer := keyIssuers[ownCurrent.KeyID]
	return Loaded{
		Policy: model.PolicySnapshot{
			Version:                 snapshot.Version,
			TrustDomain:             snapshot.Policy.TrustDomain,
			DefaultDecision:         snapshot.Policy.DefaultDecision,
			TokenTTLSeconds:         snapshot.Policy.TokenTTLSeconds,
			AllowedClockSkewSeconds: snapshot.Policy.AllowedClockSkewSeconds,
			MaxCompactJWSBytes:      snapshot.Policy.MaxCompactJWSBytes,
			Issuer:                  issuer,
			SignerKeyID:             ownCurrent.KeyID,
			SourceRevision:          snapshot.SourceRevision,
			SourceDigestSHA256:      sourceDigest,
			PredecessorRevision:     snapshot.Predecessor.Revision,
			PredecessorDigestSHA256: snapshot.Predecessor.DigestSHA256,
			KeySetRevision:          snapshot.KeySetRevision,
			PolicyRevision:          snapshot.PolicyRevision,
			SignerGeneration:        snapshot.SignerGeneration,
			OperationBindings:       bindings,
		},
		Keys: service.KeyMaterial{
			SigningKey:       signingKey,
			VerificationKeys: verificationKeys,
			KeyIssuers:       keyIssuers,
			ProofKeys:        proofKeys,
			ReadbackKey:      readbackKey,
		},
	}, nil
}

func loadIssuerKeys(
	keySets []issuerKeySet,
	workloadID string,
	requireOwnCurrent bool,
) (map[string]internalrpcauth.ES256Key, map[string]string, internalrpcauth.ES256Key, error) {
	keys := make(map[string]internalrpcauth.ES256Key)
	issuers := make(map[string]string)
	var ownCurrent internalrpcauth.ES256Key
	for _, keySet := range keySets {
		currentCount := 0
		for _, entry := range keySet.Keys {
			key, err := internalrpcauth.ParsePublicJWK(entry.JWK)
			if err != nil {
				return nil, nil, internalrpcauth.ES256Key{}, fmt.Errorf("parse snapshot public key: %w", err)
			}
			if existingIssuer, duplicate := issuers[key.KeyID]; duplicate && existingIssuer != keySet.Issuer {
				return nil, nil, internalrpcauth.ES256Key{}, errors.New("snapshot key id is shared by different issuers")
			}
			keys[key.KeyID] = key
			issuers[key.KeyID] = keySet.Issuer
			if entry.Status == "CURRENT" {
				currentCount++
				if ownCurrent.Public == nil {
					ownCurrent = key
				}
				if keySet.WorkloadID == workloadID {
					ownCurrent = key
				}
			}
		}
		if currentCount != 1 {
			return nil, nil, internalrpcauth.ES256Key{}, errors.New("issuer key set must contain exactly one CURRENT key")
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
			return nil, nil, internalrpcauth.ES256Key{}, errors.New("configured issuer workload has no CURRENT key")
		}
	}
	if ownCurrent.Public == nil {
		return nil, nil, internalrpcauth.ES256Key{}, errors.New("configured workload has no CURRENT issuer key")
	}
	return keys, issuers, ownCurrent, nil
}

func loadPublicKeySet(path string) (map[string]internalrpcauth.ES256Key, error) {
	raw, err := readRegularFile(path, maxKeyFileBytes, 0)
	if err != nil {
		return nil, err
	}
	if key, parseErr := internalrpcauth.ParsePublicJWK(raw); parseErr == nil {
		return map[string]internalrpcauth.ES256Key{key.KeyID: key}, nil
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &set); err != nil || len(set.Keys) == 0 || len(set.Keys) > 32 {
		return nil, errors.New("authority proof trust is not a supported JWK set")
	}
	result := make(map[string]internalrpcauth.ES256Key, len(set.Keys))
	for _, encoded := range set.Keys {
		key, err := internalrpcauth.ParsePublicJWK(encoded)
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[key.KeyID]; duplicate {
			return nil, errors.New("duplicate authority proof key id")
		}
		result[key.KeyID] = key
	}
	return result, nil
}

func bindingApplies(role Role, workloadID string, binding operationBinding) bool {
	if role == RoleIssuer {
		return binding.CallerWorkloadID == workloadID
	}
	return binding.TargetWorkloadID == workloadID
}

func samePublicKey(left, right internalrpcauth.ES256Key) bool {
	return left.Public != nil && right.Public != nil &&
		left.Public.X.Cmp(right.Public.X) == 0 &&
		left.Public.Y.Cmp(right.Public.Y) == 0
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
