package session

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/google/uuid"
)

const (
	familyActive        = "ACTIVE"
	familyRefreshing    = "REFRESHING"
	familyTerminal      = "TERMINAL"
	absoluteSSOLifetime = 12 * time.Hour
	refreshBudget       = 15 * time.Second
	refreshAdvance      = 2 * time.Minute
)

var (
	ErrReauthentication = errors.New("session reauthentication is required")
	ErrRenewalPending   = errors.New("session renewal is in progress")
)

type FamilyStorage interface {
	Read(context.Context, string) (browserstate.Record, error)
	CompareAndSwap(context.Context, string, uint64, []byte) (browserstate.Record, error)
}

type TokenRefresher interface {
	Refresh(context.Context, string) (oidcverifier.BrowserTokens, error)
}

// Family никогда не сериализуется в HTTP или cookie. Tokens существуют только
// в зашифрованной owner-записи; Version и Sequence имеют разные назначения.
type Family struct {
	ID                string
	BrowserSessionID  string
	Version           uint64
	State             string
	Principal         oidcverifier.Principal
	AccessToken       string
	RefreshToken      string
	CSRFHash          string
	CreatedAt         time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RefreshExpiresAt  time.Time
	AttemptID         string
	RefreshDeadline   time.Time
	Elevation         *Elevation
	Sequence          uint64 `json:"-"`
}

type Families struct {
	store     FamilyStorage
	tokens    TokenRefresher
	keys      *Store
	lifecycle context.Context
	now       func() time.Time
}

func NewFamilies(lifecycle context.Context, store FamilyStorage, tokens TokenRefresher, keys *Store) (*Families, error) {
	if lifecycle == nil || store == nil || tokens == nil || keys == nil || keys.current == nil {
		return nil, errors.New("session family configuration is invalid")
	}
	return &Families{store: store, tokens: tokens, keys: keys, lifecycle: lifecycle, now: time.Now}, nil
}

func (families *Families) Create(ctx context.Context, tokens oidcverifier.BrowserTokens, csrfHash string, elevation *Elevation) (Family, error) {
	now := families.now().UTC()
	absolute := tokens.Principal.AuthenticatedAt.Add(absoluteSSOLifetime)
	if tokens.Principal.AuthenticatedAt.IsZero() || tokens.Principal.AuthenticatedAt.After(now) ||
		!absolute.After(now) || !tokens.RefreshExpiresAt.After(now) || !tokens.Principal.ExpiresAt.After(now) {
		return Family{}, ErrReauthentication
	}
	if tokens.RefreshExpiresAt.Before(absolute) {
		absolute = tokens.RefreshExpiresAt
	}
	family := Family{ID: uuid.NewString(), BrowserSessionID: uuid.NewString(), Version: 1, State: familyActive, Principal: tokens.Principal,
		AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, CSRFHash: csrfHash,
		CreatedAt: now, IdleExpiresAt: earlier(now.Add(families.keys.ttl), absolute), AbsoluteExpiresAt: absolute,
		RefreshExpiresAt: tokens.RefreshExpiresAt, Elevation: elevation}
	return families.write(ctx, family)
}

func (families *Families) CreateWithCSRF(ctx context.Context, tokens oidcverifier.BrowserTokens, elevation *Elevation) (Family, string, error) {
	csrf, err := randomOpaque()
	if err != nil {
		return Family{}, "", err
	}
	digest := sha256.Sum256([]byte(csrf))
	family, err := families.Create(ctx, tokens, hex.EncodeToString(digest[:]), elevation)
	return family, csrf, err
}

func (families *Families) RenewAfter(family Family) time.Time {
	renew := earlier(family.IdleExpiresAt.Add(-families.keys.ttl/3), family.Principal.ExpiresAt.Add(-refreshAdvance))
	if renew.Before(families.now()) {
		return families.now()
	}
	return renew
}

func (families *Families) Read(ctx context.Context, id string) (Family, error) {
	record, err := families.store.Read(ctx, id)
	if errors.Is(err, browserstate.ErrNotFound) {
		return Family{}, ErrReauthentication
	}
	if err != nil {
		return Family{}, err
	}
	family, err := families.decode(id, record)
	if err != nil {
		return Family{}, err
	}
	now := families.now()
	if family.State == familyTerminal || !family.IdleExpiresAt.After(now) || !family.AbsoluteExpiresAt.After(now) {
		return Family{}, ErrReauthentication
	}
	return family, nil
}

// Cookie несёт ссылку и browser binding, но не токены OIDC.
func (families *Families) Cookie(family Family) (Claims, string, error) {
	now := families.now().UTC()
	if !validFamily(family) || family.State != familyActive || !family.IdleExpiresAt.After(now) || !family.AbsoluteExpiresAt.After(now) {
		return Claims{}, "", ErrReauthentication
	}
	elevation := family.Elevation
	if elevation != nil && !time.Unix(elevation.ExpiresAt, 0).After(now) {
		elevation = nil
	}
	claims := Claims{Subject: family.Principal.Subject, OrganizationID: family.Principal.OrganizationID,
		OIDCSessionID: family.Principal.SessionID, SessionRevision: family.Principal.SessionRevision,
		SessionID: family.BrowserSessionID, FamilyID: family.ID, CSRFHash: family.CSRFHash,
		IssuedAt: now.Unix(), ExpiresAt: family.IdleExpiresAt.Unix(), Elevation: elevation}
	encoded, err := families.keys.seal(claims)
	return claims, encoded, err
}

// ConsumeElevation сохраняет refresh family, атомарно заменяя browser session
// и CSRF. Старый cookie перестаёт соответствовать текущему owner snapshot.
func (families *Families) ConsumeElevation(ctx context.Context, id, browserID string) (Family, string, error) {
	for range 3 {
		family, err := families.Read(ctx, id)
		if err != nil {
			return Family{}, "", err
		}
		if family.BrowserSessionID != browserID || family.Elevation == nil {
			return Family{}, "", ErrReauthentication
		}
		if family.State != familyActive {
			return Family{}, "", ErrRenewalPending
		}
		raw := make([]byte, csrfTokenBytes)
		if _, err := io.ReadFull(rand.Reader, raw); err != nil {
			return Family{}, "", browserstate.ErrUnavailable
		}
		csrf := base64.RawURLEncoding.EncodeToString(raw)
		digest := sha256.Sum256([]byte(csrf))
		family.CSRFHash, family.BrowserSessionID, family.Elevation = hex.EncodeToString(digest[:]), uuid.NewString(), nil
		family.Version++
		updated, err := families.write(ctx, family)
		if !errors.Is(err, browserstate.ErrConflict) {
			return updated, csrf, err
		}
	}
	return Family{}, "", browserstate.ErrConflict
}

// Renew получает право внешнего effect только после durable REFRESHING.
// Истёкшая attempt не захватывается повторно: rotation могла пройти у IdP.
func (families *Families) Renew(ctx context.Context, id, browserID, csrfHash string) (Family, error) {
	family, err := families.Read(ctx, id)
	if err != nil {
		return Family{}, err
	}
	if family.BrowserSessionID != browserID || family.CSRFHash != csrfHash {
		return Family{}, ErrReauthentication
	}
	now := families.now().UTC()
	if family.State == familyRefreshing {
		if family.RefreshDeadline.After(now) {
			return Family{}, ErrRenewalPending
		}
		_, _ = families.terminate(family)
		return Family{}, ErrReauthentication
	}
	if family.Principal.ExpiresAt.After(now.Add(refreshAdvance)) {
		family.IdleExpiresAt = earlier(now.Add(families.keys.ttl), family.AbsoluteExpiresAt)
		family.Version++
		return families.write(ctx, family)
	}
	if !family.RefreshExpiresAt.After(now) {
		_, _ = families.terminate(family)
		return Family{}, ErrReauthentication
	}
	family.State, family.AttemptID = familyRefreshing, uuid.NewString()
	family.RefreshDeadline = now.Add(refreshBudget)
	family.Version++
	family, err = families.write(ctx, family)
	if errors.Is(err, browserstate.ErrConflict) {
		return Family{}, ErrRenewalPending
	}
	if err != nil {
		return Family{}, err
	}
	// Независимый bounded context завершает захваченную attempt даже после
	// закрытия browser request. Shutdown не создаёт новый корневой context.
	effect, cancel := context.WithTimeout(families.lifecycle, refreshBudget)
	defer cancel()
	tokens, err := families.tokens.Refresh(effect, family.RefreshToken)
	completedAt := families.now().UTC()
	if err != nil || !sameSession(family.Principal, tokens.Principal) || tokens.AccessToken == "" || tokens.RefreshToken == "" ||
		!tokens.Principal.ExpiresAt.After(completedAt) || !tokens.RefreshExpiresAt.After(completedAt) ||
		!family.AbsoluteExpiresAt.After(completedAt) || !family.IdleExpiresAt.After(completedAt) {
		_, _ = families.terminate(family)
		return Family{}, ErrReauthentication
	}
	family.Principal, family.AccessToken, family.RefreshToken = tokens.Principal, tokens.AccessToken, tokens.RefreshToken
	family.RefreshExpiresAt = earlier(tokens.RefreshExpiresAt, family.AbsoluteExpiresAt)
	family.AbsoluteExpiresAt = earlier(family.AbsoluteExpiresAt, family.RefreshExpiresAt)
	family.IdleExpiresAt = earlier(completedAt.Add(families.keys.ttl), family.AbsoluteExpiresAt)
	family.State, family.AttemptID, family.RefreshDeadline = familyActive, "", time.Time{}
	family.Version++
	commit, commitCancel := context.WithTimeout(families.lifecycle, refreshBudget)
	defer commitCancel()
	return families.write(commit, family)
}

func (families *Families) Revoke(ctx context.Context, id string) error {
	for range 3 {
		record, err := families.store.Read(ctx, id)
		if errors.Is(err, browserstate.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		family, err := families.decode(id, record)
		if err != nil {
			return err
		}
		if family.State == familyTerminal {
			return nil
		}
		_, err = families.terminate(family)
		if !errors.Is(err, browserstate.ErrConflict) {
			return err
		}
	}
	return browserstate.ErrConflict
}

func (families *Families) terminate(family Family) (Family, error) {
	family.State, family.AccessToken, family.RefreshToken = familyTerminal, "", ""
	family.AttemptID, family.RefreshDeadline = "", time.Time{}
	family.Version++
	bounded, cancel := context.WithTimeout(families.lifecycle, refreshBudget)
	defer cancel()
	return families.write(bounded, family)
}

func (families *Families) write(ctx context.Context, family Family) (Family, error) {
	if !validFamily(family) {
		return Family{}, ErrReauthentication
	}
	plain, err := json.Marshal(family)
	if err != nil {
		return Family{}, ErrReauthentication
	}
	nonce := make([]byte, families.keys.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Family{}, browserstate.ErrUnavailable
	}
	sealed := families.keys.current.Seal(nonce, nonce, plain, []byte("browser-family-v1:"+family.ID))
	record, err := families.store.CompareAndSwap(ctx, family.ID, family.Sequence, sealed)
	if err != nil {
		return Family{}, err
	}
	family.Sequence = record.Sequence
	return family, nil
}

func (families *Families) decode(id string, record browserstate.Record) (Family, error) {
	decrypt := func(key cipher.AEAD) ([]byte, error) {
		if key == nil || len(record.Ciphertext) < key.NonceSize()+key.Overhead() {
			return nil, ErrReauthentication
		}
		return key.Open(nil, record.Ciphertext[:key.NonceSize()], record.Ciphertext[key.NonceSize():], []byte("browser-family-v1:"+id))
	}
	plain, err := decrypt(families.keys.current)
	if err != nil {
		plain, err = decrypt(families.keys.previous)
	}
	if err != nil {
		return Family{}, ErrReauthentication
	}
	var family Family
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&family) != nil || decoder.Decode(new(any)) != io.EOF || family.ID != id || !validFamily(family) || record.Sequence == 0 {
		return Family{}, ErrReauthentication
	}
	family.Sequence = record.Sequence
	return family, nil
}

func validFamily(f Family) bool {
	if uuid.Validate(f.ID) != nil || uuid.Validate(f.BrowserSessionID) != nil || uuid.Validate(f.Principal.Subject) != nil || uuid.Validate(f.Principal.OrganizationID) != nil ||
		uuid.Validate(f.Principal.SessionID) != nil || f.Principal.SessionRevision == 0 || f.Principal.Issuer == "" || f.Version == 0 ||
		len(f.CSRFHash) != 64 || f.CreatedAt.IsZero() || !f.AbsoluteExpiresAt.After(f.CreatedAt) ||
		f.AbsoluteExpiresAt.After(f.Principal.AuthenticatedAt.Add(absoluteSSOLifetime)) || f.IdleExpiresAt.After(f.AbsoluteExpiresAt) {
		return false
	}
	switch f.State {
	case familyActive:
		return f.AccessToken != "" && f.RefreshToken != "" && f.AttemptID == "" && f.RefreshDeadline.IsZero()
	case familyRefreshing:
		return f.AccessToken != "" && f.RefreshToken != "" && uuid.Validate(f.AttemptID) == nil && !f.RefreshDeadline.IsZero()
	case familyTerminal:
		return f.AccessToken == "" && f.RefreshToken == "" && f.AttemptID == "" && f.RefreshDeadline.IsZero()
	default:
		return false
	}
}

func sameSession(old, fresh oidcverifier.Principal) bool {
	return old.Issuer == fresh.Issuer && old.Subject == fresh.Subject && old.OrganizationID == fresh.OrganizationID &&
		old.SessionID == fresh.SessionID && old.SessionRevision == fresh.SessionRevision && old.AuthenticatedAt.Equal(fresh.AuthenticatedAt)
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
