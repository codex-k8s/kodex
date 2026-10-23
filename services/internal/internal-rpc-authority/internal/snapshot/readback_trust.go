package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	// readbackManifestRootType pins the independently root-signed readback manifest bundle's protected JWS
	// type, not the ordinary authority snapshot type.
	// readbackManifestRootType закрепляет защищённый тип JWS независимо подписанного корнем набора манифеста
	// проверки доставки, не тип обычного снимка полномочий.
	readbackManifestRootType = "kodex-internal-rpc-readback-manifest-root+jws"
	// readbackCredentialTrustType pins the signed normal-readback credential trust document type consumed
	// after manifest-key verification.
	// readbackCredentialTrustType закрепляет тип подписанного документа доверия обычным учётным данным
	// проверки доставки, читаемого после проверки ключа манифеста.
	readbackCredentialTrustType = "kodex-internal-rpc-readback-credential-trust+jws"
)

// ReadbackTrustOptions supplies independently mounted readback root, metadata, manifest bundle and
// credential-trust files with an optional evaluation clock; it performs no loading itself.
// ReadbackTrustOptions задаёт независимо монтируемые корень проверки доставки, метаданные, набор
// манифеста и файл доверия учётным данным с необязательными часами; загрузка сама по себе не
// выполняется.
type ReadbackTrustOptions struct {
	RootPublicJWKFile      string
	RootMetadataFile       string
	ManifestBundleJWSFile  string
	CredentialTrustJWSFile string
	Now                    time.Time
}

// ReadbackTrustMetadata returns checked root/manifest/trust pins and derived predecessor/served-state
// digests for subsequent durable activation; it does not itself record a persistent high-watermark.
// ReadbackTrustMetadata возвращает проверенные привязки корня, манифеста и доверия и вычисленные хеши
// предшествующего и обслуживаемого состояний для последующей устойчивой активации; собственную
// устойчивую границу версий структура не фиксирует.
type ReadbackTrustMetadata struct {
	RootID                   string
	RootFingerprintSHA256    string
	ManifestBundleRevision   uint64
	ManifestBundleDigest     string
	ManifestSignerGeneration uint64
	TrustSourceRevision      uint64
	TrustSetDigest           string
	TrustKeySetRevision      uint64
	PredecessorStateDigest   string
	ServedStateDigest        string
}

// readbackRootMaterial carries canonical mounted root metadata, public JWK and fingerprint with fixed
// purpose/audience and validity for comparison to the separately mounted root key.
// readbackRootMaterial содержит канонические монтируемые метаданные корня, открытый JWK и отпечаток с
// фиксированными назначением, аудиторией и сроком для сравнения с отдельно монтируемым корневым ключом.
type readbackRootMaterial struct {
	Version        int             `json:"v"`
	RootID         string          `json:"root_id"`
	RootGeneration uint64          `json:"root_generation"`
	Purpose        string          `json:"purpose"`
	Audience       string          `json:"aud"`
	KeyID          string          `json:"kid"`
	PublicJWK      json.RawMessage `json:"public_jwk"`
	Thumbprint     string          `json:"jwk_thumbprint_sha256"`
	SourceRevision uint64          `json:"source_revision"`
	SourceDigest   string          `json:"source_digest_sha256"`
	NotBefore      int64           `json:"not_before"`
	NotAfter       int64           `json:"not_after"`
}

// readbackManifestBundle carries the independent root-signed manifest signer set, publication deadline
// and bounded predecessor history; it is distinct from credential trust.
// readbackManifestBundle содержит независимо подписанный корнем набор подписантов манифеста, срок
// публикации и ограниченную историю; это не набор доверия учётным данным.
type readbackManifestBundle struct {
	Version        int                   `json:"v"`
	RootID         string                `json:"root_id"`
	Purpose        string                `json:"purpose"`
	Issuer         string                `json:"iss"`
	Audience       string                `json:"aud"`
	BundleRevision uint64                `json:"bundle_revision"`
	BundleDigest   string                `json:"bundle_digest_sha256"`
	Predecessor    revisionDigest        `json:"predecessor"`
	History        []revisionDigest      `json:"history"`
	Keys           []readbackManifestKey `json:"keys"`
	PublishedAt    int64                 `json:"published_at"`
	ValidUntil     int64                 `json:"valid_until"`
}

// readbackManifestKey is the public manifest-signer wire record whose generation uses
// manifest_signer_generation; it is not a credential-signing key record.
// readbackManifestKey — открытая запись подписанта манифеста с полем поколения
// manifest_signer_generation; это не запись ключа подписи учётных данных.
type readbackManifestKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"manifest_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// readbackCredentialTrust binds a signed credential-key set to exact root fingerprint, manifest
// revision/signer, source/key-set revisions, declared trust digest and predecessor history.
// readbackCredentialTrust связывает подписанный набор ключей учётных данных с точными отпечатком корня,
// ревизией и подписантом манифеста, ревизиями источника и ключей, заявленным хешем доверия и историей.
type readbackCredentialTrust struct {
	Version             int                     `json:"v"`
	Issuer              string                  `json:"iss"`
	Audience            string                  `json:"aud"`
	RootID              string                  `json:"readback_manifest_root_id"`
	RootFingerprint     string                  `json:"readback_manifest_root_fingerprint_sha256"`
	BundleRevision      uint64                  `json:"readback_manifest_bundle_revision"`
	ManifestSignerKeyID string                  `json:"readback_manifest_signer_kid"`
	SourceRevision      uint64                  `json:"source_revision"`
	KeySetRevision      uint64                  `json:"key_set_revision"`
	TrustSetDigest      string                  `json:"trust_set_digest_sha256"`
	Predecessor         revisionDigest          `json:"predecessor"`
	History             []revisionDigest        `json:"history"`
	SignerGeneration    uint64                  `json:"manifest_signer_generation"`
	Keys                []readbackCredentialKey `json:"keys"`
	PublishedAt         int64                   `json:"published_at"`
	ValidUntil          int64                   `json:"valid_until"`
}

// readbackCredentialKey carries a public normal-readback credential signer with
// credential_signer_generation; its use is selected by the verified outer document, not by field shape
// alone.
// readbackCredentialKey содержит открытого подписанта обычных учётных данных проверки доставки с
// credential_signer_generation; назначение выбирается проверенным внешним документом, а не одной формой
// полей.
type readbackCredentialKey credentialSignerKey

// LoadReadbackTrust verifies mounted root identity, root-signed manifest and CURRENT-signed credential
// trust, checks exact chain/history/key bindings and returns records plus derived state digests without
// durable activation or external effects.
// LoadReadbackTrust проверяет монтируемый корень, подписанный им манифест и доверие учётным данным с
// подписью CURRENT, точные связи цепочки, истории и ключей и возвращает записи и хеши состояний без
// устойчивой активации и внешних эффектов.
func LoadReadbackTrust(options ReadbackTrustOptions) (
	map[string]service.VerificationKeyRecord,
	ReadbackTrustMetadata,
	error,
) {
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	rootRaw, err := readRegularFile(options.RootPublicJWKFile, maxKeyFileBytes)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("read immutable readback root key: %w", err)
	}
	rootKey, err := internalrpcauth.ParsePublicJWK(rootRaw)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("parse immutable readback root key: %w", err)
	}
	metadataRaw, err := readRegularFile(options.RootMetadataFile, maxKeyFileBytes)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("read immutable readback root metadata: %w", err)
	}
	var root readbackRootMaterial
	if err := internalrpcauth.DecodeCanonicalJSON(metadataRaw, &root); err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("decode immutable readback root metadata: %w", err)
	}
	metadataKey, err := internalrpcauth.ParsePublicJWK(root.PublicJWK)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, errors.New("readback root metadata key is invalid")
	}
	rootThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(rootKey)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, errors.New("readback root fingerprint failed")
	}
	metadataThumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(metadataKey)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, errors.New("readback root metadata fingerprint failed")
	}
	if root.Version != model.ContractVersion ||
		root.RootID != "internal-rpc-authority-readback-manifest-root-v1" ||
		root.Purpose != "NORMAL_READBACK_ROOT_VERIFICATION" ||
		root.Audience != "urn:kodex:internal-rpc-authority-readback-attestor:root-verification" ||
		root.RootGeneration == 0 ||
		root.SourceRevision == 0 ||
		root.KeyID != rootKey.KeyID ||
		metadataKey.KeyID != rootKey.KeyID ||
		root.Thumbprint != rootThumbprint ||
		metadataThumbprint != rootThumbprint ||
		!snapshotDigestPattern.MatchString(root.SourceDigest) ||
		now.Before(time.Unix(root.NotBefore, 0)) ||
		!now.Before(time.Unix(root.NotAfter, 0)) {
		return nil, ReadbackTrustMetadata{}, errors.New("immutable readback root binding rejected")
	}
	bundleRaw, err := readRegularFile(options.ManifestBundleJWSFile, maxSnapshotBytes)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("read signed readback manifest bundle: %w", err)
	}
	verifiedBundle, err := internalrpcauth.VerifyCanonicalJSON(
		string(trimSingleTrailingNewline(bundleRaw)),
		rootKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackManifestRootType,
			KeyID: rootKey.KeyID,
		},
	)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("verify signed readback manifest bundle: %w", err)
	}
	var bundle readbackManifestBundle
	if err := internalrpcauth.DecodeCanonicalJSON(verifiedBundle.CanonicalPayload, &bundle); err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("decode signed readback manifest bundle: %w", err)
	}
	if bundle.Version != model.ContractVersion ||
		bundle.RootID != root.RootID ||
		bundle.Purpose != "NORMAL_READBACK_CREDENTIAL_TRUST_VERIFICATION" ||
		bundle.Issuer != "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-readback-trust-root-controller" ||
		bundle.Audience != "urn:kodex:internal-rpc-authority-readback-attestor:manifest-root" ||
		bundle.BundleRevision == 0 ||
		!snapshotDigestPattern.MatchString(bundle.BundleDigest) ||
		!now.Before(time.Unix(bundle.ValidUntil, 0)) ||
		len(bundle.Keys) < 2 ||
		len(bundle.Keys) > 3 {
		return nil, ReadbackTrustMetadata{}, errors.New("signed readback manifest bundle binding rejected")
	}
	if err := validateHistory(bundle.BundleRevision, bundle.Predecessor, bundle.History); err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("readback manifest history rejected: %w", err)
	}
	manifestKeys := make(map[string]service.VerificationKeyRecord, len(bundle.Keys))
	currentCount := 0
	for _, entry := range bundle.Keys {
		key, parseErr := internalrpcauth.ParsePublicJWK(entry.PublicJWK)
		if parseErr != nil {
			return nil, ReadbackTrustMetadata{}, errors.New("readback manifest signer key rejected")
		}
		if !validReadbackSignerMetadata(
			key, entry.KeyID, entry.Thumbprint, entry.Generation,
			entry.Status, entry.NotBefore, entry.NotAfter, now,
		) {
			return nil, ReadbackTrustMetadata{}, errors.New("readback manifest signer metadata rejected")
		}
		if _, duplicate := manifestKeys[key.KeyID]; duplicate {
			return nil, ReadbackTrustMetadata{}, errors.New("duplicate readback manifest signer key")
		}
		if entry.Status == "CURRENT" {
			currentCount++
		}
		manifestKeys[key.KeyID] = service.VerificationKeyRecord{
			Key: key, Generation: entry.Generation, Status: entry.Status,
			Purpose: "READBACK_MANIFEST_SIGNER", NotBefore: time.Unix(entry.NotBefore, 0),
			NotAfter: time.Unix(entry.NotAfter, 0),
		}
	}
	if currentCount != 1 {
		return nil, ReadbackTrustMetadata{}, errors.New("readback manifest bundle must contain one CURRENT signer")
	}
	trustRaw, err := readRegularFile(options.CredentialTrustJWSFile, maxSnapshotBytes)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("read readback credential trust snapshot: %w", err)
	}
	trustCompact := string(trimSingleTrailingNewline(trustRaw))
	trustHeader, err := internalrpcauth.ParseProtectedHeader(trustCompact)
	if err != nil || trustHeader.Type != readbackCredentialTrustType {
		return nil, ReadbackTrustMetadata{}, errors.New("readback credential trust header rejected")
	}
	manifestSigner, ok := manifestKeys[trustHeader.KeyID]
	if !ok || manifestSigner.Status != "CURRENT" {
		return nil, ReadbackTrustMetadata{}, errors.New("readback credential trust signer is not CURRENT")
	}
	verifiedTrust, err := internalrpcauth.VerifyCanonicalJSON(
		trustCompact,
		manifestSigner.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: readbackCredentialTrustType, KeyID: trustHeader.KeyID,
		},
	)
	if err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("verify readback credential trust snapshot: %w", err)
	}
	var trust readbackCredentialTrust
	if err := internalrpcauth.DecodeCanonicalJSON(verifiedTrust.CanonicalPayload, &trust); err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("decode readback credential trust snapshot: %w", err)
	}
	if trust.Version != model.ContractVersion ||
		trust.Issuer != "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher" ||
		trust.Audience != "urn:kodex:internal-rpc-authority-readback-attestor" ||
		trust.RootID != root.RootID ||
		trust.RootFingerprint != rootThumbprint ||
		trust.BundleRevision != bundle.BundleRevision ||
		trust.ManifestSignerKeyID != manifestSigner.Key.KeyID ||
		trust.SignerGeneration != manifestSigner.Generation ||
		trust.SourceRevision == 0 ||
		trust.KeySetRevision == 0 ||
		!snapshotDigestPattern.MatchString(trust.TrustSetDigest) ||
		!now.Before(time.Unix(trust.ValidUntil, 0)) ||
		len(trust.Keys) < 2 ||
		len(trust.Keys) > 3 {
		return nil, ReadbackTrustMetadata{}, errors.New("readback credential trust binding rejected")
	}
	if err := validateHistory(trust.SourceRevision, trust.Predecessor, trust.History); err != nil {
		return nil, ReadbackTrustMetadata{}, fmt.Errorf("readback credential trust history rejected: %w", err)
	}
	records := make(map[string]service.VerificationKeyRecord, len(trust.Keys))
	credentialCurrentCount := 0
	for _, entry := range trust.Keys {
		key, parseErr := internalrpcauth.ParsePublicJWK(entry.PublicJWK)
		if parseErr != nil {
			return nil, ReadbackTrustMetadata{}, errors.New("readback credential signer key rejected")
		}
		if !validReadbackSignerMetadata(
			key, entry.KeyID, entry.Thumbprint, entry.Generation,
			entry.Status, entry.NotBefore, entry.NotAfter, now,
		) {
			return nil, ReadbackTrustMetadata{}, errors.New("readback credential signer metadata rejected")
		}
		if _, duplicate := records[key.KeyID]; duplicate {
			return nil, ReadbackTrustMetadata{}, errors.New("duplicate readback credential signer key")
		}
		if entry.Status == "CURRENT" {
			credentialCurrentCount++
		}
		records[key.KeyID] = service.VerificationKeyRecord{
			Key:        key,
			Issuer:     trust.Issuer,
			Generation: entry.Generation,
			Status:     entry.Status,
			Purpose:    "READBACK_CREDENTIAL",
			Audiences:  map[string]struct{}{trust.Audience: {}},
			NotBefore:  time.Unix(entry.NotBefore, 0).UTC(),
			NotAfter:   time.Unix(entry.NotAfter, 0).UTC(),
		}
	}
	if credentialCurrentCount != 1 {
		return nil, ReadbackTrustMetadata{}, errors.New("readback credential trust must contain one CURRENT key")
	}
	predecessorStateDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		RootID              string `json:"root_id"`
		RootFingerprint     string `json:"root_fingerprint_sha256"`
		ManifestRevision    uint64 `json:"manifest_bundle_revision"`
		ManifestDigest      string `json:"manifest_bundle_digest_sha256"`
		TrustSourceRevision uint64 `json:"trust_source_revision"`
		TrustSetDigest      string `json:"trust_set_digest_sha256"`
	}{
		RootID: root.RootID, RootFingerprint: rootThumbprint,
		ManifestRevision:    bundle.Predecessor.Revision,
		ManifestDigest:      bundle.Predecessor.DigestSHA256,
		TrustSourceRevision: trust.Predecessor.Revision,
		TrustSetDigest:      trust.Predecessor.DigestSHA256,
	})
	if err != nil {
		return nil, ReadbackTrustMetadata{}, errors.New(
			"digest readback predecessor trust state",
		)
	}
	servedStateDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		RootID              string `json:"root_id"`
		RootFingerprint     string `json:"root_fingerprint_sha256"`
		ManifestRevision    uint64 `json:"manifest_bundle_revision"`
		ManifestDigest      string `json:"manifest_bundle_digest_sha256"`
		TrustSourceRevision uint64 `json:"trust_source_revision"`
		TrustSetDigest      string `json:"trust_set_digest_sha256"`
	}{
		RootID: root.RootID, RootFingerprint: rootThumbprint,
		ManifestRevision:    bundle.BundleRevision,
		ManifestDigest:      bundle.BundleDigest,
		TrustSourceRevision: trust.SourceRevision,
		TrustSetDigest:      trust.TrustSetDigest,
	})
	if err != nil {
		return nil, ReadbackTrustMetadata{}, errors.New(
			"digest served readback trust state",
		)
	}
	return records, ReadbackTrustMetadata{
		RootID:                   root.RootID,
		RootFingerprintSHA256:    rootThumbprint,
		ManifestBundleRevision:   bundle.BundleRevision,
		ManifestBundleDigest:     bundle.BundleDigest,
		ManifestSignerGeneration: manifestSigner.Generation,
		TrustSourceRevision:      trust.SourceRevision,
		TrustSetDigest:           trust.TrustSetDigest,
		TrustKeySetRevision:      trust.KeySetRevision,
		PredecessorStateDigest:   predecessorStateDigest,
		ServedStateDigest:        servedStateDigest,
	}, nil
}
