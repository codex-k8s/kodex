package snapshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const zeroSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"

// PublisherSnapshotValidity ограничивает срок одного versioned snapshot.
const PublisherSnapshotValidity = 24 * time.Hour

// PublisherKey задаёт один server-owned ключ снимка.
type PublisherKey struct {
	Issuer     string
	WorkloadID string
	Status     string
	Generation uint64
	Purpose    string
	Audiences  []string
	Key        internalrpcauth.ES256Key
}

// PublisherBuildOptions задаёт независимо проверяемые входы публикации.
type PublisherBuildOptions struct {
	ManifestSigner             internalrpcauth.ES256Key
	ManifestSignerGeneration   uint64
	ManifestRootPublicJWKFile  string
	ManifestRootMetadataFile   string
	ManifestTrustBundleJWSFile string
	PolicyFile                 string
	SourceRevision             uint64
	KeySetRevision             uint64
	History                    []model.RevisionDigest
	AuthorizationKeys          []PublisherKey
	AuthorityProofKeys         []PublisherKey
	SourceRegistryDigestSHA256 string
	Now                        time.Time
}

// PublisherBuildResult содержит полный подписанный снимок и proof trust.
type PublisherBuildResult struct {
	SnapshotCompactJWS string
	SnapshotPayload    []byte
	SourceDigestSHA256 string
	ProofTrustJSON     []byte
	PolicyRevision     uint64
}

type publisherPolicyDocument struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         policy `json:"policy"`
}

// BuildForPublisher проверяет root→manifest signer и строит полный снимок.
func BuildForPublisher(options PublisherBuildOptions) (PublisherBuildResult, error) {
	if options.ManifestSigner.Private == nil ||
		options.ManifestSignerGeneration == 0 ||
		options.SourceRevision == 0 ||
		options.KeySetRevision == 0 ||
		!snapshotDigestPattern.MatchString(options.SourceRegistryDigestSHA256) {
		return PublisherBuildResult{}, errors.New("authority snapshot publisher inputs are invalid")
	}
	now := options.Now.UTC().Truncate(time.Second)
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Second)
	}
	if err := VerifyPublisherManifestSigner(
		options.ManifestSigner,
		options.ManifestSignerGeneration,
		options.ManifestRootPublicJWKFile,
		options.ManifestRootMetadataFile,
		options.ManifestTrustBundleJWSFile,
		now,
	); err != nil {
		return PublisherBuildResult{}, err
	}
	policyRaw, err := os.ReadFile(options.PolicyFile)
	if err != nil || len(policyRaw) == 0 || len(policyRaw) > maxSnapshotBytes {
		return PublisherBuildResult{}, errors.New("read authority publisher policy")
	}
	policyRaw = trimSingleTrailingNewline(policyRaw)
	var policyDocument publisherPolicyDocument
	if err := internalrpcauth.DecodeCanonicalJSON(
		policyRaw,
		&policyDocument,
	); err != nil ||
		policyDocument.Version != model.ContractVersion ||
		policyDocument.PolicyRevision == 0 ||
		policyDocument.Policy.TrustDomain != "mattercodex.local" ||
		policyDocument.Policy.DefaultDecision != "DENY" ||
		len(policyDocument.Policy.OperationBindings) == 0 ||
		len(policyDocument.Policy.ProofProducers) == 0 {
		return PublisherBuildResult{}, errors.New("authority publisher policy rejected")
	}
	issuers, err := publisherIssuerSets(options.AuthorizationKeys, now)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	predecessor, history, err := publisherHistory(
		options.SourceRevision,
		options.History,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	value := document{
		Version:          model.ContractVersion,
		SourceRevision:   options.SourceRevision,
		KeySetRevision:   options.KeySetRevision,
		PolicyRevision:   policyDocument.PolicyRevision,
		SignerGeneration: options.ManifestSignerGeneration,
		PublishedAt:      now.Unix(),
		ValidFrom:        now.Add(-5 * time.Second).Unix(),
		ValidUntil:       now.Add(PublisherSnapshotValidity).Unix(),
		Predecessor:      predecessor,
		History:          history,
		Issuers:          issuers,
		Policy:           policyDocument.Policy,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		value,
		options.ManifestSigner,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: options.ManifestSigner.KeyID,
		},
	)
	if err != nil {
		return PublisherBuildResult{}, errors.New("sign complete authority snapshot")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		options.ManifestSigner.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: options.ManifestSigner.KeyID,
		},
	)
	if err != nil {
		return PublisherBuildResult{}, errors.New("read back signed authority snapshot")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(value)
	if err != nil {
		return PublisherBuildResult{}, errors.New("digest signed authority snapshot")
	}
	expectedPayload, err := internalrpcauth.CanonicalJSON(value)
	if err != nil {
		return PublisherBuildResult{}, errors.New(
			"encode signed authority snapshot readback",
		)
	}
	if !bytes.Equal(verified.CanonicalPayload, expectedPayload) {
		return PublisherBuildResult{}, errors.New("signed authority snapshot readback mismatch")
	}
	proofTrust, err := publisherProofTrust(
		options,
		now,
		digest,
	)
	if err != nil {
		return PublisherBuildResult{}, err
	}
	return PublisherBuildResult{
		SnapshotCompactJWS: compact,
		SnapshotPayload:    append([]byte(nil), verified.CanonicalPayload...),
		SourceDigestSHA256: digest,
		ProofTrustJSON:     proofTrust,
		PolicyRevision:     policyDocument.PolicyRevision,
	}, nil
}

// VerifyPublisherManifestSigner проверяет root→bundle→CURRENT signer до I/O.
func VerifyPublisherManifestSigner(
	signer internalrpcauth.ES256Key,
	signerGeneration uint64,
	rootPublicJWKFile string,
	rootMetadataFile string,
	trustBundleJWSFile string,
	now time.Time,
) error {
	if signer.Private == nil ||
		signerGeneration == 0 ||
		rootPublicJWKFile == "" ||
		rootMetadataFile == "" ||
		trustBundleJWSFile == "" {
		return errors.New("authority manifest signer preflight is invalid")
	}
	dummy, err := internalrpcauth.SignCanonicalJSON(
		struct {
			Version int `json:"v"`
		}{Version: model.ContractVersion},
		signer,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: signer.KeyID,
		},
	)
	if err != nil {
		return errors.New("sign manifest signer verification probe")
	}
	selected, generation, err := loadManifestVerificationKeyForType(
		LoadOptions{
			ManifestRootPublicJWKFile:  rootPublicJWKFile,
			ManifestRootMetadataFile:   rootMetadataFile,
			ManifestTrustBundleJWSFile: trustBundleJWSFile,
		},
		dummy,
		now,
		snapshotProtectedType,
	)
	if err != nil ||
		generation != signerGeneration ||
		!samePublicKey(selected, signer) {
		return errors.New(
			"manifest signer does not match independently rooted CURRENT key",
		)
	}
	return nil
}

func publisherIssuerSets(
	values []PublisherKey,
	now time.Time,
) ([]issuerKeySet, error) {
	grouped := make(map[string]*issuerKeySet)
	order := make([]string, 0)
	for _, value := range values {
		if value.Purpose != "AUTHORIZATION_CONTEXT" ||
			value.Key.Public == nil ||
			value.Issuer == "" ||
			value.WorkloadID == "" ||
			value.Generation == 0 ||
			len(value.Audiences) == 0 {
			return nil, errors.New("authorization key publication record rejected")
		}
		publicJWK, err := internalrpcauth.MarshalPublicJWK(value.Key.PublicOnly())
		if err != nil {
			return nil, errors.New("encode authorization public key")
		}
		if grouped[value.Issuer] == nil {
			grouped[value.Issuer] = &issuerKeySet{
				Issuer: value.Issuer, WorkloadID: value.WorkloadID,
			}
			order = append(order, value.Issuer)
		}
		if grouped[value.Issuer].WorkloadID != value.WorkloadID {
			return nil, errors.New("authorization issuer workload mutation rejected")
		}
		grouped[value.Issuer].Keys = append(grouped[value.Issuer].Keys, keyEntry{
			Status: value.Status, Generation: value.Generation,
			Purpose:   value.Purpose,
			Audiences: append([]string(nil), value.Audiences...),
			NotBefore: now.Add(-time.Minute).Unix(),
			NotAfter:  now.Add(PublisherSnapshotValidity).Unix(),
			JWK:       json.RawMessage(publicJWK),
		})
	}
	result := make([]issuerKeySet, 0, len(order))
	for _, issuer := range order {
		result = append(result, *grouped[issuer])
	}
	return result, nil
}

func publisherProofTrust(
	options PublisherBuildOptions,
	now time.Time,
	sourceDigest string,
) ([]byte, error) {
	_, history, err := publisherHistory(options.SourceRevision, options.History)
	if err != nil {
		return nil, err
	}
	predecessor, _, _ := publisherHistory(options.SourceRevision, options.History)
	documentValue := proofTrustDocument{
		Version:        model.ContractVersion,
		Purpose:        "AUTHORITY_PROOF_VERIFICATION",
		SourceRevision: options.SourceRevision,
		SourceDigest:   sourceDigest,
		Predecessor:    predecessor,
		History:        history,
	}
	for _, value := range options.AuthorityProofKeys {
		if value.Purpose != "AUTHORITY_PROOF" ||
			value.Key.Public == nil ||
			value.Issuer == "" ||
			value.Generation == 0 ||
			len(value.Audiences) == 0 {
			return nil, errors.New("authority proof key publication record rejected")
		}
		publicJWK, err := internalrpcauth.MarshalPublicJWK(value.Key.PublicOnly())
		if err != nil {
			return nil, errors.New("encode authority proof public key")
		}
		documentValue.Keys = append(documentValue.Keys, proofTrustKey{
			Issuer:     value.Issuer,
			Generation: value.Generation,
			Status:     value.Status,
			Purpose:    value.Purpose,
			Audiences:  append([]string(nil), value.Audiences...),
			NotBefore:  now.Add(-time.Minute).Unix(),
			NotAfter:   now.Add(PublisherSnapshotValidity).Unix(),
			JWK:        json.RawMessage(publicJWK),
		})
	}
	if len(documentValue.Keys) == 0 {
		return nil, errors.New("authority proof trust has no key")
	}
	return internalrpcauth.CanonicalJSON(documentValue)
}

func publisherHistory(
	sourceRevision uint64,
	values []model.RevisionDigest,
) (revisionDigest, []revisionDigest, error) {
	if sourceRevision == 1 {
		if len(values) != 0 {
			return revisionDigest{}, nil, errors.New("bootstrap snapshot history is not empty")
		}
		return revisionDigest{Revision: 0, DigestSHA256: zeroSHA256}, nil, nil
	}
	if len(values) == 0 || len(values) > 32 {
		return revisionDigest{}, nil, errors.New("snapshot predecessor history is unavailable")
	}
	history := make([]revisionDigest, 0, len(values))
	for _, value := range values {
		history = append(history, revisionDigest{
			Revision: value.Revision, DigestSHA256: value.DigestSHA256,
		})
	}
	predecessor := history[len(history)-1]
	if err := validateHistory(sourceRevision, predecessor, history); err != nil {
		return revisionDigest{}, nil, err
	}
	return predecessor, history, nil
}
