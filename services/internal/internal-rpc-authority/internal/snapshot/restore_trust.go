package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/service"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const restoreRoleTrustType = "mattercodex-internal-rpc-restore-role-trust+jws"

const restoreRoleTrustMaximumValidity = 366 * 24 * time.Hour

// RestoreRoleTrustOptions задаёт цепочку доверия роли восстановления.
type RestoreRoleTrustOptions struct {
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	RestoreRoleTrustJWSFile    string
	Now                        time.Time
}

type restoreRoleTrustDocument struct {
	Version          int                   `json:"v"`
	Issuer           string                `json:"iss"`
	Audience         string                `json:"aud"`
	SourceRevision   uint64                `json:"source_revision"`
	KeySetRevision   uint64                `json:"key_set_revision"`
	TrustSetDigest   string                `json:"trust_set_digest_sha256"`
	Predecessor      revisionDigest        `json:"predecessor"`
	History          []revisionDigest      `json:"history"`
	SignerGeneration uint64                `json:"manifest_signer_generation"`
	Keys             []restoreRoleTrustKey `json:"keys"`
	PublishedAt      int64                 `json:"published_at"`
	ValidUntil       int64                 `json:"valid_until"`
}

type restoreRoleTrustKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// LoadRestoreRoleTrust проверяет корневую подпись и ключ роли восстановления.
func LoadRestoreRoleTrust(options RestoreRoleTrustOptions) (
	map[string]service.VerificationKeyRecord,
	model.RestoreRoleTrustMetadata,
	error,
) {
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	raw, err := readRegularFile(
		options.RestoreRoleTrustJWSFile,
		maxSnapshotBytes,
	)
	if err != nil {
		return nil, model.RestoreRoleTrustMetadata{}, fmt.Errorf(
			"read restore role trust snapshot: %w",
			err,
		)
	}
	compact := string(trimSingleTrailingNewline(raw))
	manifestKey, signerGeneration, err := loadManifestVerificationKeyForType(
		LoadOptions{
			ManifestRootPublicJWKFile:  options.ManifestRootPublicJWKFile,
			ManifestRootMetadataFile:   options.ManifestRootMetadataFile,
			ManifestTrustBundleJWSFile: options.ManifestTrustBundleJWSFile,
		},
		compact,
		now,
		restoreRoleTrustType,
		internalrpcauth.MaxCompactJWSBytes,
	)
	if err != nil {
		return nil, model.RestoreRoleTrustMetadata{}, err
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		manifestKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  restoreRoleTrustType,
			KeyID: manifestKey.KeyID,
		},
	)
	if err != nil {
		return nil, model.RestoreRoleTrustMetadata{}, fmt.Errorf(
			"verify restore role trust snapshot: %w",
			err,
		)
	}
	var document restoreRoleTrustDocument
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&document,
	); err != nil {
		return nil, model.RestoreRoleTrustMetadata{}, fmt.Errorf(
			"decode restore role trust snapshot: %w",
			err,
		)
	}
	if document.Version != model.ContractVersion ||
		document.Issuer !=
			"spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-publisher" ||
		document.Audience !=
			"urn:mattercodex:internal-rpc-authority-restore-controller" ||
		document.SourceRevision == 0 ||
		document.KeySetRevision == 0 ||
		document.SignerGeneration != signerGeneration ||
		!snapshotDigestPattern.MatchString(document.TrustSetDigest) ||
		document.PublishedAt > now.Add(5*time.Second).Unix() ||
		document.ValidUntil <= document.PublishedAt ||
		document.ValidUntil > document.PublishedAt+
			int64(restoreRoleTrustMaximumValidity/time.Second) ||
		!now.Before(time.Unix(document.ValidUntil, 0)) ||
		len(document.Keys) < 2 ||
		len(document.Keys) > 3 {
		return nil, model.RestoreRoleTrustMetadata{}, errors.New(
			"restore role trust metadata rejected",
		)
	}
	if err := validateHistory(
		document.SourceRevision,
		document.Predecessor,
		document.History,
	); err != nil {
		return nil, model.RestoreRoleTrustMetadata{}, fmt.Errorf(
			"restore role trust history rejected: %w",
			err,
		)
	}
	keys := make(map[string]service.VerificationKeyRecord, len(document.Keys))
	currentCount := 0
	nextCount := 0
	for _, entry := range document.Keys {
		key, parseErr := internalrpcauth.ParsePublicJWK(entry.PublicJWK)
		if parseErr != nil {
			return nil, model.RestoreRoleTrustMetadata{}, errors.New(
				"restore role credential key rejected",
			)
		}
		thumbprint, thumbErr := internalrpcauth.PublicJWKThumbprintSHA256(key)
		if thumbErr != nil ||
			key.KeyID != entry.KeyID ||
			thumbprint != entry.Thumbprint ||
			entry.Generation == 0 ||
			(entry.Status != "CURRENT" &&
				entry.Status != "NEXT" &&
				entry.Status != "PREVIOUS") ||
			entry.NotAfter <= entry.NotBefore ||
			now.Before(time.Unix(entry.NotBefore, 0)) ||
			!now.Before(time.Unix(entry.NotAfter, 0)) {
			return nil, model.RestoreRoleTrustMetadata{}, errors.New(
				"restore role credential key metadata rejected",
			)
		}
		if _, duplicate := keys[key.KeyID]; duplicate {
			return nil, model.RestoreRoleTrustMetadata{}, errors.New(
				"duplicate restore role credential key",
			)
		}
		if entry.Status == "CURRENT" {
			currentCount++
		}
		if entry.Status == "NEXT" {
			nextCount++
		}
		keys[key.KeyID] = service.VerificationKeyRecord{
			Key:        key,
			Issuer:     document.Issuer,
			Generation: entry.Generation,
			Status:     entry.Status,
			Purpose:    "RESTORE_ROLE_CREDENTIAL",
			Audiences:  map[string]struct{}{document.Audience: {}},
			NotBefore:  time.Unix(entry.NotBefore, 0).UTC(),
			NotAfter:   time.Unix(entry.NotAfter, 0).UTC(),
		}
	}
	if currentCount != 1 || nextCount != 1 {
		return nil, model.RestoreRoleTrustMetadata{}, errors.New(
			"restore role trust must contain one CURRENT and one NEXT key",
		)
	}
	return keys, model.RestoreRoleTrustMetadata{
		SourceRevision:   document.SourceRevision,
		SourceDigest:     document.TrustSetDigest,
		KeySetRevision:   document.KeySetRevision,
		SignerGeneration: document.SignerGeneration,
	}, nil
}
