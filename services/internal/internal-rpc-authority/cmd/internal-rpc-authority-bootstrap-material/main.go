// Command internal-rpc-authority-bootstrap-material performs the offline
// ceremony for independently rooted production trust material.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

type revisionDigest struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

type trustKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
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

type readbackCredentialKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

func main() {
	manifestSignerFile := flag.String("manifest-signer", "", "canonical publisher manifest signer private JWK")
	readbackSignerFile := flag.String("readback-signer", "", "canonical publisher readback signer private JWK")
	output := flag.String("output", "", "private output directory for the ceremony")
	flag.Parse()
	if *manifestSignerFile == "" || *readbackSignerFile == "" || *output == "" {
		fatal(errors.New("manifest-signer, readback-signer and output are required"))
	}
	manifestSigner := readPrivateKey(*manifestSignerFile)
	readbackSigner := readPrivateKey(*readbackSignerFile)
	if err := generate(*output, manifestSigner, readbackSigner, time.Now().UTC().Truncate(time.Second)); err != nil {
		fatal(err)
	}
}

func generate(output string, manifestSigner, readbackSigner internalrpcauth.ES256Key, now time.Time) error {
	manifestRoot := mustGenerate("ira-manifest-root-g1")
	manifestNext := mustGenerate("ira-manifest-signer-g2")
	readbackRoot := mustGenerate("ira-readback-root-g1")
	readbackManifestCurrent := mustGenerate("ira-readback-manifest-g1")
	readbackManifestNext := mustGenerate("ira-readback-manifest-g2")
	readbackNext := mustGenerate("ira-readback-credential-g2")

	validFrom := now.Add(-5 * time.Minute).Unix()
	validUntil := now.Add(365 * 24 * time.Hour).Unix()
	rootValidUntil := now.Add(10 * 365 * 24 * time.Hour).Unix()
	predecessor := revisionDigest{DigestSHA256: zeroDigest}

	manifestRootPublic := mustPublic(manifestRoot)
	manifestRootThumbprint := mustThumbprint(manifestRoot)
	manifestRootMetadata := struct {
		Audience       string `json:"aud"`
		Thumbprint     string `json:"jwk_thumbprint_sha256"`
		KeyID          string `json:"kid"`
		NotAfter       int64  `json:"not_after"`
		NotBefore      int64  `json:"not_before"`
		Purpose        string `json:"purpose"`
		RootGeneration uint64 `json:"root_generation"`
		RootID         string `json:"root_id"`
		SourceDigest   string `json:"source_digest_sha256"`
		SourceRevision uint64 `json:"source_revision"`
		Version        int    `json:"v"`
	}{
		Audience: "urn:mattercodex:internal-rpc-authority:manifest-root", Thumbprint: manifestRootThumbprint,
		KeyID: manifestRoot.KeyID, NotAfter: rootValidUntil, NotBefore: validFrom,
		Purpose: "AUTHORITY_SNAPSHOT_MANIFEST_ROOT", RootGeneration: 1,
		RootID: "internal-rpc-authority-manifest-root-v1", SourceDigest: sha256Hex(manifestRootPublic),
		SourceRevision: 1, Version: 1,
	}
	manifestKeys := []trustKey{
		newTrustKey("CURRENT", 1, manifestSigner, validFrom, validUntil),
		newTrustKey("NEXT", 2, manifestNext, validFrom, validUntil),
	}
	manifestBundle := struct {
		Audience       string           `json:"aud"`
		BundleDigest   string           `json:"bundle_digest_sha256"`
		BundleRevision uint64           `json:"bundle_revision"`
		History        []revisionDigest `json:"history"`
		Keys           []trustKey       `json:"keys"`
		Predecessor    revisionDigest   `json:"predecessor"`
		PublishedAt    int64            `json:"published_at"`
		Purpose        string           `json:"purpose"`
		RootGeneration uint64           `json:"root_generation"`
		RootID         string           `json:"root_id"`
		ValidUntil     int64            `json:"valid_until"`
		Version        int              `json:"v"`
	}{
		Audience:     "urn:mattercodex:internal-rpc-authority:manifest-bundle",
		BundleDigest: mustDigest(manifestKeys), BundleRevision: 1, History: []revisionDigest{}, Keys: manifestKeys,
		Predecessor: predecessor, PublishedAt: now.Unix(), Purpose: "AUTHORITY_SNAPSHOT_MANIFEST_VERIFICATION",
		RootGeneration: 1, RootID: "internal-rpc-authority-manifest-root-v1", ValidUntil: validUntil, Version: 1,
	}
	manifestBundleJWS := mustSign(manifestBundle, manifestRoot, "mattercodex-internal-rpc-manifest-trust+jws")

	readbackRootPublic := mustPublic(readbackRoot)
	readbackRootThumbprint := mustThumbprint(readbackRoot)
	readbackRootMetadata := struct {
		Audience       string          `json:"aud"`
		Thumbprint     string          `json:"jwk_thumbprint_sha256"`
		KeyID          string          `json:"kid"`
		NotAfter       int64           `json:"not_after"`
		NotBefore      int64           `json:"not_before"`
		PublicJWK      json.RawMessage `json:"public_jwk"`
		Purpose        string          `json:"purpose"`
		RootGeneration uint64          `json:"root_generation"`
		RootID         string          `json:"root_id"`
		SourceDigest   string          `json:"source_digest_sha256"`
		SourceRevision uint64          `json:"source_revision"`
		Version        int             `json:"v"`
	}{
		Audience:   "urn:mattercodex:internal-rpc-authority-readback-attestor:root-verification",
		Thumbprint: readbackRootThumbprint, KeyID: readbackRoot.KeyID, NotAfter: rootValidUntil,
		NotBefore: validFrom, PublicJWK: readbackRootPublic, Purpose: "NORMAL_READBACK_ROOT_VERIFICATION",
		RootGeneration: 1, RootID: "internal-rpc-authority-readback-manifest-root-v1",
		SourceDigest: sha256Hex(readbackRootPublic), SourceRevision: 1, Version: 1,
	}
	readbackManifestKeys := []readbackManifestKey{
		newReadbackManifestKey("CURRENT", 1, readbackManifestCurrent, validFrom, validUntil),
		newReadbackManifestKey("NEXT", 2, readbackManifestNext, validFrom, validUntil),
	}
	readbackManifestBundle := struct {
		Audience       string                `json:"aud"`
		BundleDigest   string                `json:"bundle_digest_sha256"`
		BundleRevision uint64                `json:"bundle_revision"`
		History        []revisionDigest      `json:"history"`
		Issuer         string                `json:"iss"`
		Keys           []readbackManifestKey `json:"keys"`
		Predecessor    revisionDigest        `json:"predecessor"`
		PublishedAt    int64                 `json:"published_at"`
		Purpose        string                `json:"purpose"`
		RootID         string                `json:"root_id"`
		ValidUntil     int64                 `json:"valid_until"`
		Version        int                   `json:"v"`
	}{
		Audience:     "urn:mattercodex:internal-rpc-authority-readback-attestor:manifest-root",
		BundleDigest: mustDigest(readbackManifestKeys), BundleRevision: 1, History: []revisionDigest{},
		Issuer: "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-readback-trust-root-controller",
		Keys:   readbackManifestKeys, Predecessor: predecessor, PublishedAt: now.Unix(),
		Purpose: "NORMAL_READBACK_CREDENTIAL_TRUST_VERIFICATION",
		RootID:  "internal-rpc-authority-readback-manifest-root-v1", ValidUntil: validUntil, Version: 1,
	}
	readbackManifestJWS := mustSign(readbackManifestBundle, readbackRoot, "mattercodex-internal-rpc-readback-manifest-root+jws")

	readbackCredentialKeys := []readbackCredentialKey{
		newReadbackCredentialKey("CURRENT", 1, readbackSigner, validFrom, validUntil),
		newReadbackCredentialKey("NEXT", 2, readbackNext, validFrom, validUntil),
	}
	readbackCredentialTrust := struct {
		Audience                 string                  `json:"aud"`
		History                  []revisionDigest        `json:"history"`
		Issuer                   string                  `json:"iss"`
		KeySetRevision           uint64                  `json:"key_set_revision"`
		Keys                     []readbackCredentialKey `json:"keys"`
		ManifestSignerGeneration uint64                  `json:"manifest_signer_generation"`
		Predecessor              revisionDigest          `json:"predecessor"`
		PublishedAt              int64                   `json:"published_at"`
		ManifestBundleRevision   uint64                  `json:"readback_manifest_bundle_revision"`
		RootFingerprint          string                  `json:"readback_manifest_root_fingerprint_sha256"`
		RootID                   string                  `json:"readback_manifest_root_id"`
		ManifestSignerKeyID      string                  `json:"readback_manifest_signer_kid"`
		SourceRevision           uint64                  `json:"source_revision"`
		TrustSetDigest           string                  `json:"trust_set_digest_sha256"`
		ValidUntil               int64                   `json:"valid_until"`
		Version                  int                     `json:"v"`
	}{
		Audience: "urn:mattercodex:internal-rpc-authority-readback-attestor", History: []revisionDigest{},
		Issuer:         "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-publisher",
		KeySetRevision: 1, Keys: readbackCredentialKeys, ManifestSignerGeneration: 1,
		Predecessor: predecessor, PublishedAt: now.Unix(), ManifestBundleRevision: 1,
		RootFingerprint: readbackRootThumbprint, RootID: "internal-rpc-authority-readback-manifest-root-v1",
		ManifestSignerKeyID: readbackManifestCurrent.KeyID, SourceRevision: 1,
		TrustSetDigest: mustDigest(readbackCredentialKeys), ValidUntil: validUntil, Version: 1,
	}
	readbackCredentialJWS := mustSign(readbackCredentialTrust, readbackManifestCurrent, "mattercodex-internal-rpc-readback-credential-trust+jws")

	for path, data := range map[string][]byte{
		"public/manifest-root/bootstrap-public.jwk":     manifestRootPublic,
		"public/manifest-root/bootstrap-metadata.json":  mustCanonical(manifestRootMetadata),
		"public/readback-root/bootstrap-public.jwk":     readbackRootPublic,
		"public/readback-root/bootstrap-metadata.json":  mustCanonical(readbackRootMetadata),
		"external/publisher-manifest-trust.jws":         []byte(manifestBundleJWS),
		"external/readback-manifest-root.jws":           []byte(readbackManifestJWS),
		"external/readback-credential-trust.jws":        []byte(readbackCredentialJWS),
		"offline/manifest-root-private.jwk":             mustPrivate(manifestRoot),
		"offline/manifest-signer-next-private.jwk":      mustPrivate(manifestNext),
		"offline/readback-root-private.jwk":             mustPrivate(readbackRoot),
		"offline/readback-manifest-current-private.jwk": mustPrivate(readbackManifestCurrent),
		"offline/readback-manifest-next-private.jwk":    mustPrivate(readbackManifestNext),
		"offline/readback-credential-next-private.jwk":  mustPrivate(readbackNext),
	} {
		if err := writeFile(filepath.Join(output, path), data); err != nil {
			return err
		}
	}
	return nil
}

func newTrustKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) trustKey {
	return trustKey{status, generation, key.KeyID, mustPublic(key), mustThumbprint(key), notBefore, notAfter}
}

func newReadbackManifestKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) readbackManifestKey {
	return readbackManifestKey{status, generation, key.KeyID, mustPublic(key), mustThumbprint(key), notBefore, notAfter}
}

func newReadbackCredentialKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) readbackCredentialKey {
	return readbackCredentialKey{status, generation, key.KeyID, mustPublic(key), mustThumbprint(key), notBefore, notAfter}
}

func readPrivateKey(path string) internalrpcauth.ES256Key {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal(fmt.Errorf("read private signer: %w", err))
	}
	key, err := internalrpcauth.ParsePrivateJWK(raw)
	if err != nil {
		fatal(fmt.Errorf("parse private signer: %w", err))
	}
	return key
}

func mustGenerate(keyID string) internalrpcauth.ES256Key {
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		fatal(err)
	}
	return key
}

func mustPublic(key internalrpcauth.ES256Key) json.RawMessage {
	raw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		fatal(err)
	}
	return raw
}

func mustPrivate(key internalrpcauth.ES256Key) []byte {
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		fatal(err)
	}
	return raw
}

func mustThumbprint(key internalrpcauth.ES256Key) string {
	value, err := internalrpcauth.PublicJWKThumbprintSHA256(key.PublicOnly())
	if err != nil {
		fatal(err)
	}
	return value
}

func mustCanonical(value any) []byte {
	raw, err := internalrpcauth.CanonicalJSON(value)
	if err != nil {
		fatal(err)
	}
	return raw
}

func mustDigest(value any) string {
	digest, err := internalrpcauth.CanonicalJSONSHA256(value)
	if err != nil {
		fatal(err)
	}
	return digest
}

func mustSign(value any, key internalrpcauth.ES256Key, typ string) string {
	compact, err := internalrpcauth.SignCanonicalJSON(value, key, internalrpcauth.ProtectedHeaderExpectation{Type: typ, KeyID: key.KeyID})
	if err != nil {
		fatal(err)
	}
	return compact
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func writeFile(path string, value []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return fmt.Errorf("write ceremony output: %w", err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Authority bootstrap material generation failed: %v\n", err)
	os.Exit(1)
}
