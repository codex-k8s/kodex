// Package session owns protected browser cookie formats and encrypted owner
// records for login, renewal, scoped elevation and one-use WebSocket tickets.
// Provider verification and HTTP/domain authorization remain at their boundaries.
// Пакет session владеет protected browser cookie formats и encrypted owner
// records входа, renewal, scoped elevation и одноразовых WebSocket tickets.
// Provider verification и HTTP/domain authorization остаются у своих boundaries.
package session

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/google/uuid"
)

const (
	// formatVersion pins the cookie envelope prefix and authenticated additional-data label; decoding supports this one format and tries configured keys without a wire key-generation identifier.
	// formatVersion закрепляет префикс cookie envelope и authenticated additional-data label; декодируется один формат с перебором настроенных ключей без wire key-generation identifier.
	formatVersion = "v1"
	// maximumFile caps a protected hexadecimal session-key file at 256 bytes before trimming and decoding; the decoded AES key must separately be exactly thirty-two bytes.
	// maximumFile ограничивает protected hexadecimal session-key file 256 байтами до обрезки и декодирования; декодированный AES key отдельно должен иметь ровно 32 байта.
	maximumFile = 256
	// maximumToken bounds the complete encoded cookie token to 4096 bytes on both sealing and opening, independently of the server-side family/ledger record formats.
	// maximumToken ограничивает полный encoded cookie token 4096 байтами при seal и open независимо от server-side family и ledger record formats.
	maximumToken = 4096
	// maximumBearer limits the bearer carried by the bearer-backed cookie to 2300 bytes; backend-family cookies instead carry a family reference and no bearer.
	// maximumBearer ограничивает bearer внутри bearer-backed cookie 2300 байтами; backend-family cookies вместо bearer содержат family reference.
	maximumBearer = 2300
	// csrfTokenBytes fixes the cryptographically random CSRF secret at thirty-two bytes before raw-URL Base64 encoding; claims retain SHA-256 of the encoded token rather than the raw secret.
	// csrfTokenBytes задаёт 32 байта cryptographically random CSRF secret до raw-URL Base64 encoding; claims хранят SHA-256 encoded token, а не raw secret.
	csrfTokenBytes = 32

	// ElevationKindRuntimeSecretReveal selects the closed runtime-secret reveal purpose with organization/project/secret bindings; the kind string alone does not authorize disclosure.
	// ElevationKindRuntimeSecretReveal выбирает закрытую runtime-secret reveal purpose с organization, project и secret bindings; одна kind string не разрешает раскрытие.
	ElevationKindRuntimeSecretReveal = "RUNTIME_SECRET_REVEAL"
	// ElevationKindEmailReconciliation selects the email-effect reconciliation purpose bound to one receipt reference/version/digest, excluding runtime-secret scope fields.
	// ElevationKindEmailReconciliation выбирает email-effect reconciliation purpose, привязанную к одним receipt reference, version и digest, с запретом runtime-secret scope fields.
	ElevationKindEmailReconciliation = "EMAIL_EFFECT_RECONCILIATION"
	// MaximumElevationLifetime caps elevation duration at two minutes relative to the validation reference time; renewal cannot extend its original absolute expiry.
	// MaximumElevationLifetime ограничивает elevation duration двумя минутами относительно validation reference time; renewal не продлевает первоначальный absolute expiry.
	MaximumElevationLifetime = 2 * time.Minute
	// maximumReceiptVersion is the largest exactly representable positive JSON integer admitted for receipt-version binding, distinct from resource existence or owner authority.
	// maximumReceiptVersion задаёт наибольший точно представимый положительный JSON integer receipt-version binding отдельно от существования ресурса и owner authority.
	maximumReceiptVersion = 1<<53 - 1
)

// Config selects the current and optional previous protected key files and cookie idle TTL. New validates paths and TTL and loads keys; it does not set OIDC or durable family retention policy.
// Config выбирает current и необязательный previous protected key files и cookie idle TTL. New проверяет paths и TTL и загружает ключи без назначения OIDC и durable family retention policy.
type Config struct {
	CurrentKeyFile  string
	PreviousKeyFile string
	TTL             time.Duration
}

// Store owns current/previous AEADs, cookie TTL and a replaceable local clock for cookie protection and server-record cryptography. It performs no domain lookup, OIDC signature verification or durable revocation itself.
// Store владеет current и previous AEADs, cookie TTL и заменяемыми локальными часами защиты cookie и server-record cryptography. Domain lookup, OIDC signature verification и durable revocation самостоятельно не выполняются.
type Store struct {
	current  cipher.AEAD
	previous cipher.AEAD
	ttl      time.Duration
	now      func() time.Time
}

// Claims is the cookie JSON payload carrying immutable identity/session/CSRF bindings and either a bounded bearer or family reference, with optional scoped elevation. Decryption and shape checks do not replace authoritative bearer/family/domain validation.
// Claims — cookie JSON payload с immutable identity, session и CSRF bindings и ограниченным bearer либо family reference, с необязательным scoped elevation. Decryption и shape checks не заменяют authoritative bearer, family и domain validation.
type Claims struct {
	Subject         string     `json:"sub"`
	OrganizationID  string     `json:"organization_id"`
	OIDCSessionID   string     `json:"oidc_session_id"`
	SessionRevision uint64     `json:"session_revision"`
	SessionID       string     `json:"session_id"`
	Bearer          string     `json:"bearer,omitempty"`
	FamilyID        string     `json:"family_id,omitempty"`
	CSRFHash        string     `json:"csrf_sha256"`
	IssuedAt        int64      `json:"issued_at"`
	ExpiresAt       int64      `json:"expires_at"`
	Elevation       *Elevation `json:"elevation,omitempty"`
}

// Elevation carries exact short-lived sensitive-operation bindings inside a protected session; it is not a standalone bearer grant. Single use requires the authoritative server-owned session/family consumption path.
// Elevation передаёт точные short-lived sensitive-operation bindings внутри protected session, не являясь самостоятельным bearer grant. Одноразовость требует authoritative server-owned session либо family consumption path.
type Elevation struct {
	Kind            string `json:"kind"`
	ScopeKind       string `json:"scope_kind,omitempty"`
	OrganizationRef string `json:"organization_ref,omitempty"`
	ProjectRef      string `json:"project_ref,omitempty"`
	SecretRef       string `json:"secret_ref,omitempty"`
	ReceiptRef      string `json:"receipt_ref,omitempty"`
	ReceiptVersion  int64  `json:"receipt_version,omitempty"`
	ReceiptDigest   string `json:"receipt_digest,omitempty"`
	ExpiresAt       int64  `json:"expires_at"`
}

// New requires an absolute current-key path and one-minute-to-one-hour TTL, loads its AES-GCM key and optionally an absolute previous-key path, then installs the wall clock. It does not require distinct key generations or start a worker.
// New требует absolute current-key path и TTL от минуты до часа, загружает AES-GCM key и необязательный absolute previous-key path, затем устанавливает wall clock. Разные key generations не требуются и worker не запускается.
func New(config Config) (*Store, error) {
	if !filepath.IsAbs(config.CurrentKeyFile) || config.TTL < time.Minute || config.TTL > time.Hour {
		return nil, errors.New("session store configuration is invalid")
	}
	current, err := loadAEAD(config.CurrentKeyFile)
	if err != nil {
		return nil, err
	}
	var previous cipher.AEAD
	if config.PreviousKeyFile != "" {
		if !filepath.IsAbs(config.PreviousKeyFile) {
			return nil, errors.New("previous session key path is invalid")
		}
		previous, err = loadAEAD(config.PreviousKeyFile)
		if err != nil {
			return nil, err
		}
	}
	return &Store{current: current, previous: previous, ttl: config.TTL, now: time.Now}, nil
}

// Issue delegates bearer-backed cookie issuance with no elevation, preserving the same identity, bearer-ceiling and CSRF validation performed by IssueWithElevation.
// Issue передаёт bearer-backed cookie issuance без elevation, сохраняя те же identity, bearer-ceiling и CSRF validation, которые выполняет IssueWithElevation.
func (store *Store) Issue(subject, organizationID, oidcSessionID string, revision uint64, bearer string, tokenExpiry time.Time) (Claims, string, string, error) {
	return store.IssueWithElevation(subject, organizationID, oidcSessionID, revision, bearer, tokenExpiry, nil)
}

// IssueWithElevation validates identity/bearer/elevation shape, caps expiry by the supplied bearer deadline and requires more than a minute remaining, then creates a session ID and random CSRF before sealing claims. It does not verify the bearer, persist a session or deep-copy the elevation pointer in returned claims.
// IssueWithElevation проверяет форму identity, bearer и elevation, ограничивает expiry переданным bearer deadline и требует остаток свыше минуты, затем создаёт session ID и random CSRF до seal claims. Bearer не проверяется, session не сохраняется и elevation pointer возвращаемых claims не копируется глубоко.
func (store *Store) IssueWithElevation(subject, organizationID, oidcSessionID string, revision uint64, bearer string, tokenExpiry time.Time, elevation *Elevation) (Claims, string, string, error) {
	if store == nil || uuid.Validate(subject) != nil || uuid.Validate(organizationID) != nil || uuid.Validate(oidcSessionID) != nil || revision == 0 ||
		bearer == "" || len(bearer) > maximumBearer || strings.TrimSpace(bearer) != bearer {
		return Claims{}, "", "", errors.New("session input is invalid")
	}
	now := store.now().UTC()
	expires := now.Add(store.ttl)
	if tokenExpiry.Before(expires) {
		expires = tokenExpiry.UTC()
	}
	if !expires.After(now.Add(time.Minute)) {
		return Claims{}, "", "", errors.New("OIDC bearer lifetime is too short")
	}
	if !validElevation(elevation, now, expires) {
		return Claims{}, "", "", errors.New("session elevation is invalid")
	}
	csrfRaw := make([]byte, csrfTokenBytes)
	if _, err := io.ReadFull(rand.Reader, csrfRaw); err != nil {
		return Claims{}, "", "", errors.New("generate CSRF token")
	}
	csrf := base64.RawURLEncoding.EncodeToString(csrfRaw)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := Claims{
		Subject: subject, OrganizationID: organizationID, OIDCSessionID: oidcSessionID, SessionRevision: revision,
		SessionID: uuid.NewString(), Bearer: bearer, CSRFHash: hex.EncodeToString(csrfDigest[:]),
		IssuedAt: now.Unix(), ExpiresAt: expires.Unix(), Elevation: elevation,
	}
	encoded, err := store.seal(claims)
	if err != nil {
		return Claims{}, "", "", err
	}
	return claims, encoded, csrf, nil
}

// Renew expects validated non-family claims, renews only in the final TTL third when the supplied bearer ceiling permits a later expiry, and preserves identity/bearer/CSRF bindings. Expired elevation is removed only on an actual renewal; the method returns encoded data rather than setting a browser cookie.
// Renew ожидает проверенные non-family claims, продлевает срок лишь в последней трети TTL при допускающем поздний expiry переданном bearer ceiling и сохраняет identity, bearer и CSRF bindings. Истёкшее elevation снимается лишь при фактическом renewal; метод возвращает encoded data, а не устанавливает browser cookie.
func (store *Store) Renew(claims Claims, tokenExpiry time.Time) (Claims, string, bool, error) {
	if store == nil || claims.FamilyID != "" || tokenExpiry.IsZero() || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt {
		return Claims{}, "", false, errors.New("session renewal input is invalid")
	}
	now := store.now().UTC()
	currentExpiry := time.Unix(claims.ExpiresAt, 0).UTC()
	if !now.Before(currentExpiry) {
		return Claims{}, "", false, errors.New("session token is expired")
	}
	if currentExpiry.Sub(now) > store.ttl/3 {
		return claims, "", false, nil
	}
	expires := now.Add(store.ttl)
	if tokenExpiry.Before(expires) {
		expires = tokenExpiry.UTC()
	}
	if expires.Unix() <= claims.ExpiresAt {
		return claims, "", false, nil
	}
	if claims.Elevation != nil && !now.Before(time.Unix(claims.Elevation.ExpiresAt, 0).UTC()) {
		claims.Elevation = nil
	}
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = expires.Unix()
	encoded, err := store.seal(claims)
	if err != nil {
		return Claims{}, "", false, err
	}
	return claims, encoded, true, nil
}

// Open authenticates/decrypts the cookie, rejects unknown/trailing JSON and checks required identity/carrier/time/elevation shapes before returning claims. Elevation bounds use issue time, not consumption time; no duplicate-key guard, live bearer validation or durable replay/revocation check occurs here.
// Open аутентифицирует и расшифровывает cookie, отклоняет unknown и trailing JSON и проверяет обязательную форму identity, carrier, time и elevation до возврата claims. Elevation bounds используют issue time, а не consumption time; duplicate-key guard, live bearer validation и durable replay и revocation check здесь отсутствуют.
func (store *Store) Open(encoded string) (Claims, error) {
	plaintext, err := store.open(encoded)
	if err != nil {
		return Claims{}, errors.New("session token is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var claims Claims
	if decoder.Decode(&claims) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		uuid.Validate(claims.Subject) != nil || uuid.Validate(claims.OrganizationID) != nil || uuid.Validate(claims.OIDCSessionID) != nil ||
		uuid.Validate(claims.SessionID) != nil || claims.SessionRevision == 0 ||
		!validCredentialCarrier(claims) ||
		len(claims.CSRFHash) != sha256.Size*2 || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt ||
		!store.now().UTC().Before(time.Unix(claims.ExpiresAt, 0)) ||
		!validElevation(claims.Elevation, time.Unix(claims.IssuedAt, 0).UTC(), time.Unix(claims.ExpiresAt, 0).UTC()) {
		return Claims{}, errors.New("session token is invalid")
	}
	return claims, nil
}

// validCredentialCarrier requires a UUID-shaped family reference with no bearer, or a nonempty bounded bearer when no family is present. This is carrier-shape validation, not credential authenticity or expiry verification.
// validCredentialCarrier требует UUID-подобный family reference без bearer либо непустой ограниченный bearer при отсутствии family. Проверяется carrier shape, а не подлинность credential и его expiry.
func validCredentialCarrier(claims Claims) bool {
	if claims.FamilyID != "" {
		return uuid.Validate(claims.FamilyID) == nil && claims.Bearer == ""
	}
	return claims.Bearer != "" && len(claims.Bearer) <= maximumBearer
}

// validElevation accepts absence or one closed purpose with future expiry within the supplied session/reference-time limits and no mixed-purpose fields. Resource eligibility and one-time consumption remain outside this structural predicate.
// validElevation допускает отсутствие либо одну closed purpose с будущим expiry в переданных session и reference-time limits без mixed-purpose fields. Resource eligibility и одноразовое consumption остаются за пределами structural predicate.
func validElevation(value *Elevation, now, sessionExpiry time.Time) bool {
	if value == nil {
		return true
	}
	expires := time.Unix(value.ExpiresAt, 0).UTC()
	if !expires.After(now) || expires.After(sessionExpiry) || expires.Sub(now) > MaximumElevationLifetime {
		return false
	}
	switch value.Kind {
	case ElevationKindRuntimeSecretReveal:
		return ValidRuntimeSecretBinding(value.ScopeKind, value.OrganizationRef, value.ProjectRef, value.SecretRef) &&
			value.ReceiptRef == "" && value.ReceiptVersion == 0 && value.ReceiptDigest == ""
	case ElevationKindEmailReconciliation:
		return value.ProjectRef == "" && value.SecretRef == "" && value.ScopeKind == "" && value.OrganizationRef == "" && ValidEmailReceiptBinding(value.ReceiptRef, value.ReceiptVersion, value.ReceiptDigest)
	default:
		return false
	}
}

// ValidRuntimeSecretBinding checks bounded reference grammar and exact ORGANIZATION-without-project or PROJECT-with-project shape. It does not resolve ownership, enforce resource-specific prefixes or authorize a secret read.
// ValidRuntimeSecretBinding проверяет bounded reference grammar и точную форму ORGANIZATION без project либо PROJECT с project. Ownership не разрешается, resource-specific prefixes и разрешение secret read не проверяются.
func ValidRuntimeSecretBinding(kind, organizationRef, projectRef, secretRef string) bool {
	if !validOpaqueReference(organizationRef) || !validOpaqueReference(secretRef) {
		return false
	}
	switch kind {
	case "ORGANIZATION":
		return projectRef == ""
	case "PROJECT":
		return validOpaqueReference(projectRef)
	default:
		return false
	}
}

// ValidEmailReceiptBinding requires a bounded opaque reference, positive safe JSON version and exactly thirty-two bytes of lowercase hexadecimal digest. It does not load the receipt or prove the digest matches stored content.
// ValidEmailReceiptBinding требует bounded opaque reference, положительную safe JSON version и ровно 32 байта lowercase hexadecimal digest. Receipt не загружается и соответствие digest сохранённому content не доказывается.
func ValidEmailReceiptBinding(ref string, version int64, digest string) bool {
	if !validOpaqueReference(ref) || version < 1 || version > maximumReceiptVersion || len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

// validOpaqueReference accepts eight to 128 ASCII alphanumeric/hyphen/underscore characters without a prefix requirement. It is the session-purpose grammar, not the narrower domain opaque-reference contract or a resource lookup.
// validOpaqueReference допускает от восьми до 128 ASCII alphanumeric, hyphen и underscore символов без требования префикса. Это session-purpose grammar, а не более узкий domain opaque-reference contract и не поиск ресурса.
func validOpaqueReference(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

// VerifyCSRF bounds the submitted encoded token, decodes the expected SHA-256 and compares the submitted token's hash in constant time. It neither reads cookies/headers nor proves Origin; the HTTP boundary supplies those checks.
// VerifyCSRF ограничивает переданный encoded token, декодирует ожидаемый SHA-256 и сравнивает хеш переданного token за постоянное время. Cookies и headers не читаются и Origin не доказывается — это проверяет HTTP boundary.
func VerifyCSRF(claims Claims, token string) bool {
	if len(token) < 43 || len(token) > 64 {
		return false
	}
	expected, err := hex.DecodeString(claims.CSRFHash)
	if err != nil || len(expected) != sha256.Size {
		return false
	}
	actual := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(expected, actual[:]) == 1
}

// seal JSON-encodes claims and encrypts them with the current AEAD and fresh nonce under the fixed cookie AAD, then prefixes canonical raw-URL Base64 and enforces the cookie size limit. It does not persist revocation state or validate claims again.
// seal кодирует claims в JSON и шифрует current AEAD со свежим nonce и фиксированным cookie AAD, затем добавляет префикс canonical raw-URL Base64 и проверяет cookie size limit. Revocation state не сохраняется и claims повторно не проверяются.
func (store *Store) seal(claims Claims) (string, error) {
	plaintext, err := json.Marshal(claims)
	if err != nil {
		return "", errors.New("encode session token")
	}
	nonce := make([]byte, store.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", errors.New("generate session nonce")
	}
	sealed := store.current.Seal(nonce, nonce, plaintext, []byte(formatVersion))
	encoded := formatVersion + "." + base64.RawURLEncoding.EncodeToString(sealed)
	if len(encoded) > maximumToken {
		return "", errors.New("session token exceeds cookie limit")
	}
	return encoded, nil
}

// open validates envelope/version/size and canonical raw-URL Base64, then tries current and optional previous keys with the cookie AAD. It returns authenticated plaintext without JSON/claim validation or automatic key promotion.
// open проверяет envelope, version, size и canonical raw-URL Base64, затем пробует current и необязательный previous keys с cookie AAD. Возвращается authenticated plaintext без JSON и claim validation и автоматического key promotion.
func (store *Store) open(encoded string) ([]byte, error) {
	if store == nil || encoded == "" || len(encoded) > maximumToken || strings.TrimSpace(encoded) != encoded {
		return nil, errors.New("session token is invalid")
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 || parts[0] != formatVersion {
		return nil, errors.New("session token is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[1] ||
		len(raw) < store.current.NonceSize()+store.current.Overhead() {
		return nil, errors.New("session token is invalid")
	}
	plaintext, err := open(store.current, raw)
	if err != nil && store.previous != nil {
		plaintext, err = open(store.previous, raw)
	}
	return plaintext, err
}

// open checks the nonce-prefix/ciphertext minimum length and calls the supplied nonnil AEAD with fixed cookie AAD. It is a byte-level helper and does not select keys, decode JSON or convert authentication errors.
// open проверяет минимальную длину nonce-prefix и ciphertext и вызывает переданный ненулевой AEAD с фиксированным cookie AAD. Это byte-level helper без выбора ключей, декодирования JSON и преобразования authentication errors.
func open(aead cipher.AEAD, raw []byte) ([]byte, error) {
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("session token is invalid")
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(formatVersion))
}

// loadAEAD reads a bounded protected file through securefile, trims and hex-decodes exactly thirty-two key bytes and constructs AES-GCM. Fixed errors omit paths/content; this function does not rotate files or explicitly clear its local key buffers.
// loadAEAD читает ограниченный protected file через securefile, обрезает и hex-декодирует ровно 32 key bytes и создаёт AES-GCM. Фиксированные ошибки не раскрывают paths и content; files не ротируются и local key buffers явно не очищаются.
func loadAEAD(path string) (cipher.AEAD, error) {
	raw, err := securefile.Read(path, maximumFile)
	if err != nil {
		return nil, errors.New("session key file is unsafe")
	}
	trimmed := strings.TrimSpace(string(raw))
	key, err := hex.DecodeString(trimmed)
	if err != nil || len(key) != 32 {
		return nil, errors.New("session key must be a 32-byte hex value")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("construct session cipher")
	}
	return cipher.NewGCM(block)
}
