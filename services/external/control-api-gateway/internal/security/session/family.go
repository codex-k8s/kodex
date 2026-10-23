package session

import (
	"bytes"
	"context"
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
	// familyActive denotes a usable encrypted owner record with access/refresh tokens and no in-progress refresh attempt; current expiry and browser bindings are checked on access.
	// familyActive обозначает пригодную encrypted owner record с access и refresh tokens без текущей refresh attempt; актуальные expiry и browser bindings проверяются при доступе.
	familyActive = "ACTIVE"
	// familyRefreshing marks the CAS-reserved external refresh attempt. Another caller may observe pending state but must not reuse its refresh token after an uncertain or expired attempt.
	// familyRefreshing обозначает зарезервированную через CAS external refresh attempt. Другой caller может наблюдать pending state, но не должен повторять refresh token после uncertain либо expired attempt.
	familyRefreshing = "REFRESHING"
	// familyTerminal is the persisted closed family state with access/refresh credentials cleared; reads reject it rather than rebuilding a live family from a cookie.
	// familyTerminal — сохранённое закрытое family state с очищенными access и refresh credentials; чтения отклоняют его без восстановления живого family из cookie.
	familyTerminal = "TERMINAL"
	// absoluteSSOLifetime caps a family at twelve hours from the authenticated principal's original authentication time, additionally bounded by refresh-token expiry. It does not configure the identity provider's own session policy.
	// absoluteSSOLifetime ограничивает family двенадцатью часами от исходного authentication time principal, дополнительно сроком refresh token. Политика сессий identity provider этой константой не настраивается.
	absoluteSSOLifetime = 12 * time.Hour
	// refreshBudget bounds the reserved attempt's deadline and each independent lifecycle-owned refresh/commit/termination context to fifteen seconds; it is not permission to retry an uncertain external effect.
	// refreshBudget ограничивает deadline зарезервированной attempt и каждый независимый lifecycle-owned refresh, commit и termination context пятнадцатью секундами; это не разрешение повторять uncertain external effect.
	refreshBudget = 15 * time.Second
	// refreshAdvance schedules access-token renewal up to two minutes before expiry when that credential expires before the immutable absolute family ceiling.
	// refreshAdvance планирует access-token renewal за две минуты до expiry, когда credential истекает раньше immutable absolute family ceiling.
	refreshAdvance = 2 * time.Minute
)

var (
	// ErrReauthentication is the closed-session sentinel used for absent/expired/invalid family data and unsafe or failed refresh outcomes, without exposing credentials or provider details.
	// ErrReauthentication — closed-session sentinel отсутствующих, expired и invalid family data и unsafe либо failed refresh outcomes без раскрытия credentials и provider details.
	ErrReauthentication = errors.New("session reauthentication is required")
	// ErrRenewalPending signals an existing refresh attempt, bounded CAS contention or ticket capacity exhaustion; it grants no right to repeat an externally consumed credential.
	// ErrRenewalPending обозначает существующую refresh attempt, ограниченную CAS contention либо исчерпание ticket capacity без права повторять внешне потреблённый credential.
	ErrRenewalPending = errors.New("session renewal is in progress")
)

// FamilyStorage is the owner-record port for opaque ciphertext and storage sequence CAS; logical family Version is separate from its storage Sequence. Implementations own durability, replication and access control.
// FamilyStorage — owner-record port непрозрачного ciphertext и storage sequence CAS; логическая family Version отличается от storage Sequence. Durability, replication и access control обеспечивает реализация.
type FamilyStorage interface {
	// Read returns the exact record for the caller-selected owner key or a typed storage error; this port does not infer user authority from the key alone.
	// Read возвращает точную record выбранного caller owner key либо typed storage error; порт не выводит user authority из одного ключа.
	Read(context.Context, string) (browserstate.Record, error)
	// CompareAndSwap atomically replaces a record only at the expected storage sequence, with zero used for creation, and returns the resulting sequence or conflict. It does not interpret encrypted family/login/ticket contents.
	// CompareAndSwap атомарно заменяет record лишь при ожидаемом storage sequence, используя ноль для создания, и возвращает результирующий sequence либо conflict. Зашифрованные family, login и ticket contents не интерпретируются.
	CompareAndSwap(context.Context, string, uint64, []byte) (browserstate.Record, error)
}

// TokenRefresher supplies the external OIDC refresh operation; the trusted provider adapter owns token-response verification, while Families owns attempt reservation, immutable identity checks and commit.
// TokenRefresher предоставляет внешнюю OIDC refresh operation; доверенный provider adapter проверяет token response, а Families владеет attempt reservation, immutable identity checks и commit.
type TokenRefresher interface {
	// Refresh exchanges one supplied refresh token under the caller's bounded context and returns tokens or an error. This interface alone does not make an uncertain exchange safe to repeat.
	// Refresh обменивает один переданный refresh token с ограниченным контекстом caller и возвращает tokens либо ошибку. Сам интерфейс не делает повтор uncertain exchange безопасным.
	Refresh(context.Context, string) (oidcverifier.BrowserTokens, error)
}

// Family holds server-side identity/tokens, immutable absolute and idle bounds, attempt state and optional elevation inside an encrypted owner record. It is never a public cookie payload; Version tracks logical changes and Sequence tracks storage CAS.
// Family хранит server-side identity и tokens, immutable absolute и idle bounds, attempt state и optional elevation в encrypted owner record. Это не публичный cookie payload; Version отражает логические изменения, Sequence — storage CAS.
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

// Families coordinates encrypted owner records with the token adapter and process lifecycle, using a replaceable clock. It creates no background worker and relies on CAS rather than process-local serialization across replicas.
// Families согласует encrypted owner records с token adapter и process lifecycle, используя заменяемые часы. Фоновый worker не создаётся; межрепличная сериализация основана на CAS, а не process-local lock.
type Families struct {
	store     FamilyStorage
	tokens    TokenRefresher
	keys      *Store
	lifecycle context.Context
	now       func() time.Time
}

// NewFamilies requires lifecycle, storage, token adapter and a current encryption key and records them without reading storage or contacting the identity provider.
// NewFamilies требует lifecycle, storage, token adapter и current encryption key и сохраняет их без чтения storage и обращения к identity provider.
func NewFamilies(lifecycle context.Context, store FamilyStorage, tokens TokenRefresher, keys *Store) (*Families, error) {
	if lifecycle == nil || store == nil || tokens == nil || keys == nil || keys.current == nil {
		return nil, errors.New("session family configuration is invalid")
	}
	return &Families{store: store, tokens: tokens, keys: keys, lifecycle: lifecycle, now: time.Now}, nil
}

// Create checks authentication/access/refresh times, derives the immutable twelve-hour ceiling clipped by refresh expiry and creates an ACTIVE record with fresh family/browser IDs. write enforces record shape; supplied tokens, CSRF hash and elevation remain trusted caller inputs rather than independently authenticated here.
// Create проверяет authentication, access и refresh times, выводит immutable twelve-hour ceiling с ограничением refresh expiry и создаёт ACTIVE record со свежими family и browser IDs. write проверяет форму record; переданные tokens, CSRF hash и elevation остаются доверенным вводом caller без независимой аутентификации здесь.
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

// CreateWithCSRF generates a random encoded CSRF value, hashes it and delegates family creation, returning the value alongside the result/error. The CSRF value alone cannot establish a usable family after a failed write.
// CreateWithCSRF создаёт random encoded CSRF value, хеширует её и передаёт создание family, возвращая значение вместе с результатом и ошибкой. Одна CSRF value не создаёт пригодное family после failed write.
func (families *Families) CreateWithCSRF(ctx context.Context, tokens oidcverifier.BrowserTokens, elevation *Elevation) (Family, string, error) {
	csrf, err := randomOpaque()
	if err != nil {
		return Family{}, "", err
	}
	digest := sha256.Sum256([]byte(csrf))
	family, err := families.Create(ctx, tokens, hex.EncodeToString(digest[:]), elevation)
	return family, csrf, err
}

// RenewAfter chooses the earliest genuinely useful idle/access renewal boundary or absolute expiry and clamps a past result to now. It does not validate the supplied family, mutate state or invoke the provider.
// RenewAfter выбирает ближайшую действительно полезную idle и access renewal boundary либо absolute expiry и ограничивает прошлый результат текущим временем. Переданное family не проверяется, state не меняется и provider не вызывается.
func (families *Families) RenewAfter(family Family) time.Time {
	// An idle deadline already at the absolute ceiling cannot be renewed later.
	// Idle deadline у absolute ceiling не допускает дальнейшего продления.
	renew := family.AbsoluteExpiresAt
	if family.IdleExpiresAt.Before(family.AbsoluteExpiresAt) {
		renew = earlier(renew, family.IdleExpiresAt.Add(-families.keys.ttl/3))
	}
	if family.Principal.ExpiresAt.Before(family.AbsoluteExpiresAt) {
		renew = earlier(renew, family.Principal.ExpiresAt.Add(-refreshAdvance))
	}
	if renew.Before(families.now()) {
		return families.now()
	}
	return renew
}

// Read maps absent storage to reauthentication, decrypts and validates the exact owner record and rejects terminal or expired idle/absolute state. A still-live REFRESHING record may be returned for the caller to classify as pending.
// Read переводит отсутствие storage в reauthentication, расшифровывает и проверяет точную owner record и отклоняет terminal либо expired idle и absolute state. Ещё живой REFRESHING record может возвращаться для классификации caller как pending.
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

// Cookie validates an active unexpired family and seals only identity/family/browser/CSRF bindings, omitting OIDC tokens and dropping expired elevation from the returned cookie. It does not persist that projection or alter the server record.
// Cookie проверяет active unexpired family и шифрует лишь identity, family, browser и CSRF bindings без OIDC tokens, исключая expired elevation из возвращаемой cookie. Проекция не сохраняется и server record не меняется.
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

// ConsumeElevation requires the supplied browser ID, active family and present elevation, then CAS-rotates browser/CSRF bindings and clears elevation within three attempts. The HTTP/domain caller must already check its exact purpose, resource and current freshness; this method does not revalidate those fields.
// ConsumeElevation требует переданный browser ID, active family и наличие elevation, затем в пределах трёх попыток CAS заменяет browser и CSRF bindings и очищает elevation. HTTP и domain caller предварительно проверяет exact purpose, resource и current freshness — метод повторно их не проверяет.
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

// Renew validates browser/CSRF binding, performs no-op or idle-only renewal when possible, otherwise CAS-reserves REFRESHING before one lifecycle-bounded IdP exchange and commits only matching identity and live deadlines. Uncertain/expired attempts require reauthentication; late commit can fail after the external effect and is not blindly replayed.
// Renew проверяет browser и CSRF binding, при возможности выполняет no-op либо idle-only renewal, иначе через CAS резервирует REFRESHING до одного lifecycle-bounded IdP exchange и сохраняет лишь matching identity и live deadlines. Uncertain и expired attempts требуют reauthentication; поздний commit может отказать после external effect без слепого повтора.
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
	if !family.Principal.ExpiresAt.Before(family.AbsoluteExpiresAt) || family.Principal.ExpiresAt.After(now.Add(refreshAdvance)) {
		expires := earlier(now.Add(families.keys.ttl), family.AbsoluteExpiresAt)
		if !expires.After(family.IdleExpiresAt) {
			return family, nil
		}
		family.IdleExpiresAt = expires
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
	// The process lifecycle, not the browser request, bounds the reserved effect;
	// shutdown cancellation still applies and no new root context is created.
	// Зарезервированный effect ограничен process lifecycle, а не browser request;
	// отмена shutdown сохраняется, новый root context не создаётся.
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

// Revoke retries up to three CAS conflicts, treats absent or already terminal records as success and persists termination without calling IdP logout. It reads raw owner state so expired families can still be closed.
// Revoke повторяет до трёх CAS conflicts, считает отсутствующие и уже terminal records успешными и сохраняет termination без IdP logout. Читается raw owner state, позволяя закрыть и expired families.
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

// terminate clears access/refresh credentials and attempt fields, advances the logical version and writes TERMINAL using an independent bounded lifecycle context. Other bindings remain as tombstone data; a storage failure is returned rather than assumed committed.
// terminate очищает access и refresh credentials и attempt fields, увеличивает logical version и пишет TERMINAL с независимым ограниченным lifecycle context. Остальные bindings сохраняются как tombstone data; storage failure возвращается, а commit не предполагается.
func (families *Families) terminate(family Family) (Family, error) {
	family.State, family.AccessToken, family.RefreshToken = familyTerminal, "", ""
	family.AttemptID, family.RefreshDeadline = "", time.Time{}
	family.Version++
	bounded, cancel := context.WithTimeout(families.lifecycle, refreshBudget)
	defer cancel()
	return families.write(bounded, family)
}

// write validates family shape, JSON-encodes it without storage Sequence, encrypts with the current key and exact family-ID AAD, then CAS-writes at the supplied sequence. Returned storage Sequence is assigned only after success; signing/OIDC and domain eligibility are outside this adapter.
// write проверяет family shape, кодирует JSON без storage Sequence, шифрует current key с exact family-ID AAD и пишет CAS по переданному sequence. Возвращённый storage Sequence назначается лишь после успеха; signing, OIDC и domain eligibility за пределами адаптера.
func (families *Families) write(ctx context.Context, family Family) (Family, error) {
	if !validFamily(family) {
		return Family{}, ErrReauthentication
	}
	plain, err := json.Marshal(family)
	if err != nil {
		return Family{}, ErrReauthentication
	}
	sealed, err := families.keys.sealState(plain, "browser-family-v1:"+family.ID)
	if err != nil {
		return Family{}, err
	}
	record, err := families.store.CompareAndSwap(ctx, family.ID, family.Sequence, sealed)
	if err != nil {
		return Family{}, err
	}
	family.Sequence = record.Sequence
	return family, nil
}

// decode authenticates current/previous-key ciphertext under the selected family ID, rejects unknown/trailing JSON and mismatched identity/shape or zero sequence, then adopts the storage sequence. Expiry-at-now is checked by Read; duplicate JSON keys are not rejected here.
// decode аутентифицирует current и previous-key ciphertext выбранным family ID, отклоняет unknown и trailing JSON, несовпадение identity и shape и нулевой sequence, затем принимает storage sequence. Expiry относительно now проверяет Read; duplicate JSON keys здесь не отклоняются.
func (families *Families) decode(id string, record browserstate.Record) (Family, error) {
	plain, err := families.keys.openState(record.Ciphertext, "browser-family-v1:"+id)
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

// validFamily checks UUID/identity/revision/time-bound shape and closed ACTIVE/REFRESHING/TERMINAL credential-attempt combinations. It does not validate JWT signatures, every CSRF/elevation field or whether the record is currently unexpired.
// validFamily проверяет форму UUID, identity, revision и time bounds и закрытые сочетания credentials и attempt для ACTIVE, REFRESHING и TERMINAL. JWT signatures и каждое CSRF и elevation field не проверяются; истечение срока относительно текущего времени проверяется отдельно.
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

// sameSession requires exact issuer, subject, organization, OIDC session ID/revision and original authentication time across refresh. Access-token expiry is intentionally allowed to change.
// sameSession требует exact issuer, subject, organization, OIDC session ID и revision и исходное authentication time при refresh. Access-token expiry намеренно может меняться.
func sameSession(old, fresh oidcverifier.Principal) bool {
	return old.Issuer == fresh.Issuer && old.Subject == fresh.Subject && old.OrganizationID == fresh.OrganizationID &&
		old.SessionID == fresh.SessionID && old.SessionRevision == fresh.SessionRevision && old.AuthenticatedAt.Equal(fresh.AuthenticatedAt)
}

// earlier returns the earlier timestamp without normalization; equal instants return the second value, preserving its representation.
// earlier возвращает более раннюю timestamp без нормализации; при одинаковых моментах возвращается второе значение с сохранением его представления.
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
