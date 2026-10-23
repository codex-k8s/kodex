// Command internal-rpc-authority-bootstrap-material performs the offline
// ceremony for independently rooted production trust material.
// Команда internal-rpc-authority-bootstrap-material выполняет автономную
// подготовку промышленных материалов доверия с независимыми корнями.
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

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// zeroDigest is the all-zero SHA-256 predecessor marker for first-revision bootstrap bundles, not a digest of generated key bytes or an authenticated prior snapshot.
// zeroDigest — маркер предшественника из нулевого SHA-256 для первичных пакетов, а не дайджест созданных ключей или подтверждённый предыдущий снимок.
const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// revisionDigest records revision and lowercase digest text for bundle predecessor/history metadata. This JSON shape carries provenance references but does not validate their contents or authority itself.
// revisionDigest хранит ревизию и текст дайджеста для метаданных предшественника и истории пакета. JSON-форма несёт ссылки происхождения, но сама не проверяет содержимое или полномочия.
type revisionDigest struct {
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

// trustKey is the manifest trust bundle's nominal public-key metadata with a generic generation JSON tag, key status, public JWK, thumbprint and validity bounds. It contains no private scalar and performs no status or time validation merely by being constructed.
// trustKey — отдельный тип метаданных публичного ключа пакета доверия manifest с общим JSON-тегом generation, статусом, публичным JWK, отпечатком и границами действия. Приватного скаляра нет; создание структуры само не проверяет статус или время.
type trustKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// readbackManifestKey retains the readback manifest's distinct nominal type and manifest_signer_generation JSON tag while sharing the common metadata layout. A value is not interchangeable authority for credential or restore signing.
// readbackManifestKey сохраняет отдельный тип manifest readback и JSON-тег manifest_signer_generation при общей раскладке метаданных. Значение не является взаимозаменяемым полномочием подписания реквизитов или восстановления.
type readbackManifestKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"manifest_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// readbackCredentialKey is the readback-credential trust set's nominal key record with credential_signer_generation and public-only metadata. Shared field layout does not collapse its bundle audience, signing key or trust purpose.
// readbackCredentialKey — отдельная запись ключа набора доверия реквизитов readback с credential_signer_generation и только публичными метаданными. Общая раскладка полей не объединяет аудиторию пакета, ключ подписи или назначение доверия.
type readbackCredentialKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// restoreRoleTrustKey preserves a separate restore-role key type with credential_signer_generation, status, public JWK, thumbprint and validity fields. The surrounding restore-role bundle supplies its audience and signature; this DTO does not issue restore authority.
// restoreRoleTrustKey сохраняет отдельный тип ключа роли восстановления с credential_signer_generation, статусом, публичным JWK, отпечатком и полями действия. Аудиторию и подпись задаёт окружающий пакет роли восстановления; структура не выдаёт полномочие восстановления.
type restoreRoleTrustKey struct {
	Status     string          `json:"status"`
	Generation uint64          `json:"credential_signer_generation"`
	KeyID      string          `json:"kid"`
	PublicJWK  json.RawMessage `json:"public_jwk"`
	Thumbprint string          `json:"jwk_thumbprint_sha256"`
	NotBefore  int64           `json:"not_before"`
	NotAfter   int64           `json:"not_after"`
}

// main requires explicit manifest, readback and restore private-signer files and one output directory, loads those signers and generates offline bootstrap material at the current UTC second. It exits on missing flags or errors, without installing the output into a cluster or contacting a running authority service.
// main требует явные файлы приватных подписантов manifest, readback и restore и один выходной каталог, загружает подписантов и создаёт автономные первичные материалы на текущую секунду UTC. Отсутствующие флаги и ошибки завершают процесс; результат не устанавливается в кластер и работающий сервис authority не вызывается.
func main() {
	manifestSignerFile := flag.String("manifest-signer", "", "canonical publisher manifest signer private JWK")
	readbackSignerFile := flag.String("readback-signer", "", "canonical publisher readback signer private JWK")
	restoreSignerFile := flag.String("restore-signer", "", "canonical publisher restore signer private JWK")
	output := flag.String("output", "", "private output directory for the ceremony")
	flag.Parse()
	if *manifestSignerFile == "" || *readbackSignerFile == "" ||
		*restoreSignerFile == "" || *output == "" {
		fatal(errors.New("manifest-signer, readback-signer, restore-signer and output are required"))
	}
	manifestSigner := readPrivateKey(*manifestSignerFile)
	readbackSigner := readPrivateKey(*readbackSignerFile)
	restoreSigner := readPrivateKey(*restoreSignerFile)
	if err := generate(
		*output,
		manifestSigner,
		readbackSigner,
		restoreSigner,
		time.Now().UTC().Truncate(time.Second),
	); err != nil {
		fatal(err)
	}
}

// generate derives seven fresh root/next ES256 keys around the three supplied current signers, builds first-revision manifest/readback/restore trust documents with exact purpose, audience and signer bindings, and writes fifteen public, external and offline artifacts. Validity starts five minutes before now, ordinary bundles last three hundred sixty-five days and root metadata ten such years. Writes follow map iteration order and can leave partial or overwritten files; this is an offline ceremony, not atomic rotation, deployment or admission.
// generate создаёт семь новых корневых и следующих ключей ES256 вокруг трёх переданных текущих подписантов, формирует документы доверия manifest, readback и restore первой ревизии с точными назначением, аудиторией и привязками подписантов и записывает пятнадцать public, external и offline артефактов. Действие начинается за пять минут до now, обычные пакеты действуют триста шестьдесят пять дней, корневые метаданные — десять таких лет. Порядок записи определяется обходом map, возможны частичные или перезаписанные файлы; это автономная процедура, а не атомарная ротация, развёртывание или допуск.
func generate(
	output string,
	manifestSigner internalrpcauth.ES256Key,
	readbackSigner internalrpcauth.ES256Key,
	restoreSigner internalrpcauth.ES256Key,
	now time.Time,
) error {
	manifestRoot := mustGenerate("ira-manifest-root-g1")
	manifestNext := mustGenerate("ira-manifest-signer-g2")
	readbackRoot := mustGenerate("ira-readback-root-g1")
	readbackManifestCurrent := mustGenerate("ira-readback-manifest-g1")
	readbackManifestNext := mustGenerate("ira-readback-manifest-g2")
	readbackNext := mustGenerate("ira-readback-credential-g2")
	restoreNext := mustGenerate("ira-publisher-restore-g2")

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
		Audience: "urn:kodex:internal-rpc-authority:manifest-root", Thumbprint: manifestRootThumbprint,
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
		Audience:     "urn:kodex:internal-rpc-authority:manifest-bundle",
		BundleDigest: mustDigest(manifestKeys), BundleRevision: 1, History: []revisionDigest{}, Keys: manifestKeys,
		Predecessor: predecessor, PublishedAt: now.Unix(), Purpose: "AUTHORITY_SNAPSHOT_MANIFEST_VERIFICATION",
		RootGeneration: 1, RootID: "internal-rpc-authority-manifest-root-v1", ValidUntil: validUntil, Version: 1,
	}
	manifestBundleJWS := mustSign(manifestBundle, manifestRoot, "kodex-internal-rpc-manifest-trust+jws")

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
		Audience:   "urn:kodex:internal-rpc-authority-readback-attestor:root-verification",
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
		Audience:     "urn:kodex:internal-rpc-authority-readback-attestor:manifest-root",
		BundleDigest: mustDigest(readbackManifestKeys), BundleRevision: 1, History: []revisionDigest{},
		Issuer: "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-readback-trust-root-controller",
		Keys:   readbackManifestKeys, Predecessor: predecessor, PublishedAt: now.Unix(),
		Purpose: "NORMAL_READBACK_CREDENTIAL_TRUST_VERIFICATION",
		RootID:  "internal-rpc-authority-readback-manifest-root-v1", ValidUntil: validUntil, Version: 1,
	}
	readbackManifestJWS := mustSign(readbackManifestBundle, readbackRoot, "kodex-internal-rpc-readback-manifest-root+jws")

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
		Audience: "urn:kodex:internal-rpc-authority-readback-attestor", History: []revisionDigest{},
		Issuer:         "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher",
		KeySetRevision: 1, Keys: readbackCredentialKeys, ManifestSignerGeneration: 1,
		Predecessor: predecessor, PublishedAt: now.Unix(), ManifestBundleRevision: 1,
		RootFingerprint: readbackRootThumbprint, RootID: "internal-rpc-authority-readback-manifest-root-v1",
		ManifestSignerKeyID: readbackManifestCurrent.KeyID, SourceRevision: 1,
		TrustSetDigest: mustDigest(readbackCredentialKeys), ValidUntil: validUntil, Version: 1,
	}
	readbackCredentialJWS := mustSign(readbackCredentialTrust, readbackManifestCurrent, "kodex-internal-rpc-readback-credential-trust+jws")
	restoreKeys := []restoreRoleTrustKey{
		newRestoreRoleTrustKey("CURRENT", 1, restoreSigner, validFrom, validUntil),
		newRestoreRoleTrustKey("NEXT", 2, restoreNext, validFrom, validUntil),
	}
	restoreRoleTrust := struct {
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
	}{
		Version:          1,
		Issuer:           "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher",
		Audience:         "urn:kodex:internal-rpc-authority-restore-controller",
		SourceRevision:   1,
		KeySetRevision:   1,
		TrustSetDigest:   mustDigest(restoreKeys),
		Predecessor:      predecessor,
		History:          []revisionDigest{},
		SignerGeneration: 1,
		Keys:             restoreKeys,
		PublishedAt:      now.Unix(),
		ValidUntil:       validUntil,
	}
	restoreRoleTrustJWS := mustSign(
		restoreRoleTrust,
		manifestSigner,
		"kodex-internal-rpc-restore-role-trust+jws",
	)

	for path, data := range map[string][]byte{
		"public/manifest-root/bootstrap-public.jwk":     manifestRootPublic,
		"public/manifest-root/bootstrap-metadata.json":  mustCanonical(manifestRootMetadata),
		"public/readback-root/bootstrap-public.jwk":     readbackRootPublic,
		"public/readback-root/bootstrap-metadata.json":  mustCanonical(readbackRootMetadata),
		"external/publisher-manifest-trust.jws":         []byte(manifestBundleJWS),
		"external/readback-manifest-root.jws":           []byte(readbackManifestJWS),
		"external/readback-credential-trust.jws":        []byte(readbackCredentialJWS),
		"external/restore-role-trust.jws":               []byte(restoreRoleTrustJWS),
		"offline/manifest-root-private.jwk":             mustPrivate(manifestRoot),
		"offline/manifest-signer-next-private.jwk":      mustPrivate(manifestNext),
		"offline/readback-root-private.jwk":             mustPrivate(readbackRoot),
		"offline/readback-manifest-current-private.jwk": mustPrivate(readbackManifestCurrent),
		"offline/readback-manifest-next-private.jwk":    mustPrivate(readbackManifestNext),
		"offline/readback-credential-next-private.jwk":  mustPrivate(readbackNext),
		"offline/restore-signer-next-private.jwk":       mustPrivate(restoreNext),
	} {
		if err := writeFile(filepath.Join(output, path), data); err != nil {
			return err
		}
	}
	return nil
}

// newTrustKey builds the shared public-only metadata in field order from supplied status, generation, key identity and validity bounds, deriving public JWK before thumbprint. Failures follow the existing fatal helpers; it does not validate status, generation, validity or trust eligibility.
// newTrustKey создаёт общие только публичные метаданные в порядке полей из переданных статуса, поколения, идентичности ключа и границ действия, получая публичный JWK перед отпечатком. Ошибки идут через существующие fatal-помощники; статус, поколение, время и допустимость доверия не проверяются.
func newTrustKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) trustKey {
	return trustKey{status, generation, key.KeyID, mustPublic(key), mustThumbprint(key), notBefore, notAfter}
}

// newReadbackManifestKey reuses newTrustKey's exact metadata construction and converts to the distinct manifest type, retaining its manifest_signer_generation JSON tag and the original public-JWK-before-thumbprint evaluation order.
// newReadbackManifestKey использует точное создание метаданных newTrustKey и преобразует результат в отдельный тип manifest, сохраняя JSON-тег manifest_signer_generation и прежний порядок получения публичного JWK перед отпечатком.
func newReadbackManifestKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) readbackManifestKey {
	return readbackManifestKey(newTrustKey(status, generation, key, notBefore, notAfter))
}

// newReadbackCredentialKey reuses common public metadata construction and converts to the credential record whose own JSON tag is credential_signer_generation. The conversion changes the nominal type, not key selection, status, validity or signer authority.
// newReadbackCredentialKey использует общее создание публичных метаданных и преобразует результат в запись реквизита с собственным JSON-тегом credential_signer_generation. Преобразование меняет тип, но не выбор ключа, статус, срок действия или полномочия подписанта.
func newReadbackCredentialKey(status string, generation uint64, key internalrpcauth.ES256Key, notBefore, notAfter int64) readbackCredentialKey {
	return readbackCredentialKey(newTrustKey(status, generation, key, notBefore, notAfter))
}

// newRestoreRoleTrustKey delegates metadata construction to newTrustKey and converts to the separate restore-role record with its original credential_signer_generation tag. Restore signers remain explicitly supplied by the caller; no readback trust authority or private JWK is reused by this conversion.
// newRestoreRoleTrustKey передаёт создание метаданных newTrustKey и преобразует результат в отдельную запись роли восстановления с исходным тегом credential_signer_generation. Подписанты restore по-прежнему явно передаются вызывающим; преобразование не переиспользует полномочия доверия readback или приватный JWK.
func newRestoreRoleTrustKey(
	status string,
	generation uint64,
	key internalrpcauth.ES256Key,
	notBefore int64,
	notAfter int64,
) restoreRoleTrustKey {
	return restoreRoleTrustKey(newTrustKey(status, generation, key, notBefore, notAfter))
}

// readPrivateKey reads and parses the exact supplied private JWK file and exits through fatal on read or parse failure. It does not enforce securefile size/mode rules, install a key, perform rotation or explicitly clear the read buffer.
// readPrivateKey читает и разбирает точный переданный файл приватного JWK и завершает процесс через fatal при ошибке чтения или разбора. Правила размера и режима securefile, установка ключа, ротация и явная очистка прочитанного буфера здесь не выполняются.
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

// mustGenerate requests a fresh ES256 key with the supplied key ID and exits through fatal on generation failure; it returns the key only to the local ceremony, without publishing or registering it.
// mustGenerate запрашивает новый ключ ES256 с переданным идентификатором и завершает процесс через fatal при ошибке создания; ключ возвращается только локальной процедуре без публикации или регистрации.
func mustGenerate(keyID string) internalrpcauth.ES256Key {
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		fatal(err)
	}
	return key
}

// mustPublic serializes PublicOnly from the supplied ES256 key as public JWK JSON, exiting through fatal on error. It does not serialize the private scalar or validate that this public key belongs to an admitted trust set.
// mustPublic сериализует PublicOnly переданного ключа ES256 в публичный JWK JSON и завершает процесс через fatal при ошибке. Приватный скаляр не сериализуется, принадлежность публичного ключа допущенному набору доверия не проверяется.
func mustPublic(key internalrpcauth.ES256Key) json.RawMessage {
	raw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		fatal(err)
	}
	return raw
}

// mustPrivate serializes the supplied ES256 private JWK for explicit offline output, terminating through fatal on error. Returned bytes are sensitive ceremony material, not public bundle metadata.
// mustPrivate сериализует приватный JWK переданного ES256 для явного автономного вывода и завершает процесс через fatal при ошибке. Возвращённые байты — чувствительный материал процедуры, а не публичные метаданные пакета.
func mustPrivate(key internalrpcauth.ES256Key) []byte {
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		fatal(err)
	}
	return raw
}

// mustThumbprint derives the SHA-256 thumbprint of the key's public-only JWK and exits through fatal on error; the identifier is not a signature or proof of key ownership.
// mustThumbprint получает SHA-256-отпечаток только публичного JWK ключа и завершает процесс через fatal при ошибке; идентификатор не является подписью или доказательством владения ключом.
func mustThumbprint(key internalrpcauth.ES256Key) string {
	value, err := internalrpcauth.PublicJWKThumbprintSHA256(key.PublicOnly())
	if err != nil {
		fatal(err)
	}
	return value
}

// mustCanonical serializes a value with the shared canonical JSON routine or exits through fatal. Canonical bytes alone provide neither a digest nor a signature or authorization decision.
// mustCanonical сериализует значение общей процедурой канонического JSON либо завершает процесс через fatal. Канонические байты сами не дают дайджест, подпись или решение о полномочиях.
func mustCanonical(value any) []byte {
	raw, err := internalrpcauth.CanonicalJSON(value)
	if err != nil {
		fatal(err)
	}
	return raw
}

// mustDigest returns the shared canonical-JSON SHA-256 of the exact supplied value, terminating through fatal on failure. It hashes content rather than authenticating its provenance or granting trust.
// mustDigest возвращает SHA-256 канонического JSON точного переданного значения и завершает процесс через fatal при ошибке. Хешируется содержимое, а не подтверждается происхождение или выдаётся доверие.
func mustDigest(value any) string {
	digest, err := internalrpcauth.CanonicalJSONSHA256(value)
	if err != nil {
		fatal(err)
	}
	return digest
}

// mustSign signs canonical JSON with the exact supplied ES256 key and protected type/key-ID expectation, returning compact JWS or exiting through fatal. The caller owns payload audience, purpose and trust-chain construction; this helper does not verify the result against an external root.
// mustSign подписывает канонический JSON точным переданным ключом ES256 с ожидаемыми защищёнными типом и идентификатором ключа, возвращая компактный JWS либо завершая процесс через fatal. Аудитория, назначение и цепочка доверия payload принадлежат вызывающему; результат не проверяется по внешнему корню этим помощником.
func mustSign(value any, key internalrpcauth.ES256Key, typ string) string {
	compact, err := internalrpcauth.SignCanonicalJSON(value, key, internalrpcauth.ProtectedHeaderExpectation{Type: typ, KeyID: key.KeyID})
	if err != nil {
		fatal(err)
	}
	return compact
}

// sha256Hex returns lowercase hexadecimal SHA-256 of the exact byte slice without JSON canonicalization, signature verification or mutation of the input.
// sha256Hex возвращает SHA-256 точного среза байтов шестнадцатеричной строкой нижнего регистра без канонизации JSON, проверки подписи или изменения входа.
func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// writeFile creates missing output parents with requested mode 0700 and writes or truncates the target with requested creation mode 0600, wrapping filesystem failures. It does not make existing permissions exact, publish atomically, roll back earlier writes or sync files/directories.
// writeFile создаёт отсутствующие родительские каталоги с запрошенным режимом 0700 и записывает или обрезает целевой файл с режимом создания 0600, оборачивая файловые ошибки. Права существующих объектов не приводятся к точным, атомарная публикация, откат ранних записей и sync файлов или каталогов не выполняются.
func writeFile(path string, value []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, value, 0o600); err != nil {
		return fmt.Errorf("write ceremony output: %w", err)
	}
	return nil
}

// fatal prints a fixed bootstrap-generation prefix plus the returned error and exits with status one. It performs no retry or cleanup of partial output and does not itself print key bytes.
// fatal выводит фиксированный префикс ошибки подготовки и возвращённую ошибку и завершается с кодом один. Повтора и очистки частичного вывода нет; сам помощник не печатает байты ключей.
func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Authority bootstrap material generation failed: %v\n", err)
	os.Exit(1)
}
