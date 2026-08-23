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

	"github.com/codex-k8s/matter-codex/libs/go/securefile"
	"github.com/google/uuid"
)

const (
	formatVersion  = "v1"
	maximumFile    = 256
	maximumToken   = 4096
	maximumBearer  = 2300
	csrfTokenBytes = 32
)

type Config struct {
	CurrentKeyFile  string
	PreviousKeyFile string
	TTL             time.Duration
}

type Store struct {
	current  cipher.AEAD
	previous cipher.AEAD
	ttl      time.Duration
	now      func() time.Time
}

type Claims struct {
	Subject         string `json:"sub"`
	OrganizationID  string `json:"organization_id"`
	OIDCSessionID   string `json:"oidc_session_id"`
	SessionRevision uint64 `json:"session_revision"`
	SessionID       string `json:"session_id"`
	Bearer          string `json:"bearer"`
	CSRFHash        string `json:"csrf_sha256"`
	IssuedAt        int64  `json:"issued_at"`
	ExpiresAt       int64  `json:"expires_at"`
}

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

func (store *Store) Issue(subject, organizationID, oidcSessionID string, revision uint64, bearer string, tokenExpiry time.Time) (Claims, string, string, error) {
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
	csrfRaw := make([]byte, csrfTokenBytes)
	if _, err := io.ReadFull(rand.Reader, csrfRaw); err != nil {
		return Claims{}, "", "", errors.New("generate CSRF token")
	}
	csrf := base64.RawURLEncoding.EncodeToString(csrfRaw)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := Claims{
		Subject: subject, OrganizationID: organizationID, OIDCSessionID: oidcSessionID, SessionRevision: revision,
		SessionID: uuid.NewString(), Bearer: bearer, CSRFHash: hex.EncodeToString(csrfDigest[:]),
		IssuedAt: now.Unix(), ExpiresAt: expires.Unix(),
	}
	encoded, err := store.seal(claims)
	if err != nil {
		return Claims{}, "", "", err
	}
	return claims, encoded, csrf, nil
}

func (store *Store) Open(encoded string) (Claims, error) {
	if store == nil || encoded == "" || len(encoded) > maximumToken || strings.TrimSpace(encoded) != encoded {
		return Claims{}, errors.New("session token is invalid")
	}
	parts := strings.Split(encoded, ".")
	if len(parts) != 2 || parts[0] != formatVersion {
		return Claims{}, errors.New("session token is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(raw) < store.current.NonceSize()+store.current.Overhead() {
		return Claims{}, errors.New("session token is invalid")
	}
	plaintext, err := open(store.current, raw)
	if err != nil && store.previous != nil {
		plaintext, err = open(store.previous, raw)
	}
	if err != nil {
		return Claims{}, errors.New("session token is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var claims Claims
	if decoder.Decode(&claims) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		uuid.Validate(claims.Subject) != nil || uuid.Validate(claims.OrganizationID) != nil || uuid.Validate(claims.OIDCSessionID) != nil ||
		uuid.Validate(claims.SessionID) != nil || claims.SessionRevision == 0 ||
		claims.Bearer == "" || len(claims.Bearer) > maximumBearer ||
		len(claims.CSRFHash) != sha256.Size*2 || claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt ||
		!store.now().UTC().Before(time.Unix(claims.ExpiresAt, 0)) {
		return Claims{}, errors.New("session token is invalid")
	}
	return claims, nil
}

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

func open(aead cipher.AEAD, raw []byte) ([]byte, error) {
	if len(raw) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("session token is invalid")
	}
	return aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(formatVersion))
}

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
