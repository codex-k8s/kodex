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
	readbackManifestRootType    = "kodex-internal-rpc-readback-manifest-root+jws"
	readbackCredentialTrustType = "kodex-internal-rpc-readback-credential-trust+jws"
)

// ReadbackTrustOptions задаёт независимую цепочку доверия проверки выдачи.
type ReadbackTrustOptions struct {
	RootPublicJWKFile      string
	RootMetadataFile       string
	ManifestBundleJWSFile  string
	CredentialTrustJWSFile string
	Now                    time.Time
}

// ReadbackTrustMetadata фиксирует назначение и поколение доверенного ключа.
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

type readbackManifestKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"manifest_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

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

type readbackCredentialKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// LoadReadbackTrust проверяет корневую подпись и назначение ключа.
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
		thumbprint, thumbprintErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbprintErr != nil ||
			key.KeyID != entry.KeyID ||
			thumbprint != entry.Thumbprint ||
			entry.Generation == 0 ||
			(entry.Status != "CURRENT" && entry.Status != "NEXT" && entry.Status != "PREVIOUS") ||
			now.Before(time.Unix(entry.NotBefore, 0)) ||
			!now.Before(time.Unix(entry.NotAfter, 0)) {
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
		thumbprint, thumbprintErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbprintErr != nil ||
			key.KeyID != entry.KeyID ||
			thumbprint != entry.Thumbprint ||
			entry.Generation == 0 ||
			(entry.Status != "CURRENT" && entry.Status != "NEXT" && entry.Status != "PREVIOUS") ||
			now.Before(time.Unix(entry.NotBefore, 0)) ||
			!now.Before(time.Unix(entry.NotAfter, 0)) {
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
