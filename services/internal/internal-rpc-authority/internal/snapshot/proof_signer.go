package snapshot

import (
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// VerifyProofSigner связывает обслуживаемый private key с exact CURRENT
// записью independently delivered proof trust для того же снимка.
func VerifyProofSigner(
	privateJWKFile string,
	proofTrustJWKFile string,
	expectedIssuer string,
	expectedAudience string,
	expectedSourceRevision uint64,
	expectedSourceDigest string,
	expectedGeneration uint64,
	now time.Time,
) error {
	if expectedIssuer == "" ||
		expectedAudience == "" ||
		expectedSourceRevision == 0 ||
		!snapshotDigestPattern.MatchString(expectedSourceDigest) ||
		expectedGeneration == 0 {
		return errors.New("authority proof signer expectation is invalid")
	}
	privateRaw, err := readRegularFile(privateJWKFile, maxKeyFileBytes)
	if err != nil {
		return errors.New("read authority proof signer private key")
	}
	privateKey, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return errors.New("parse authority proof signer private key")
	}
	trustRaw, err := readRegularFile(proofTrustJWKFile, maxKeyFileBytes)
	if err != nil {
		return errors.New("read authority proof signer trust")
	}
	document, err := internalrpcauth.DecodeAuthorityProofTrustDocument(trustRaw)
	if err != nil ||
		document.SourceRevision != expectedSourceRevision ||
		document.SourceDigest != expectedSourceDigest {
		return errors.New("authority proof signer trust source rejected")
	}
	keys, err := loadProofTrust(
		proofTrustJWKFile,
		now.UTC(),
		expectedSourceRevision,
		expectedSourceDigest,
	)
	if err != nil {
		return err
	}
	record, found := keys[privateKey.KeyID]
	if !found ||
		record.Issuer != expectedIssuer ||
		record.Generation != expectedGeneration ||
		record.Status != "CURRENT" ||
		record.Purpose != "AUTHORITY_PROOF" {
		return errors.New("authority proof signer CURRENT binding rejected")
	}
	if _, allowed := record.Audiences[expectedAudience]; !allowed ||
		!samePublicKey(record.Key, privateKey.PublicOnly()) {
		return errors.New("authority proof signer audience or public key rejected")
	}
	return nil
}
