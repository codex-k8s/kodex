package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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

// loginLifetime fixes the server-owned authorization-code transaction window at five minutes; it does not extend the identity provider's code, access-token or SSO lifetime.
// loginLifetime задаёт пятиминутное окно server-owned authorization-code transaction без продления identity-provider code, access-token и SSO lifetime.
const loginLifetime = 5 * time.Minute

// LoginPurpose records an optional closed sensitive-operation target for fresh login, separating runtime-secret owner scope from exact email receipt binding. These fields are input to validation, not an authorization decision.
// LoginPurpose хранит необязательную закрытую sensitive-operation target свежего входа, разделяя runtime-secret owner scope и exact email receipt binding. Поля являются вводом validation, а не authorization decision.
type LoginPurpose struct {
	Kind            string
	ScopeKind       string
	OrganizationRef string
	ProjectRef      string
	SecretRef       string
	ReceiptRef      string
	ReceiptVersion  int64
	ReceiptDigest   string
}

// LoginProvider is the trusted OIDC adapter port for authorization URL construction and code exchange; Logins owns browser binding and durable one-use transaction state around these operations.
// LoginProvider — trusted OIDC adapter port построения authorization URL и code exchange; Logins владеет browser binding и durable one-use transaction state вокруг этих операций.
type LoginProvider interface {
	// AuthorizationURL constructs the configured provider URL from state, nonce, S256 challenge and fresh-login intent; it is not permission to follow arbitrary URLs from an untrusted payload.
	// AuthorizationURL строит настроенный provider URL из state, nonce, S256 challenge и fresh-login intent, не разрешая переходы по произвольным URL недоверенного payload.
	AuthorizationURL(string, string, string, bool) (string, error)
	// ExchangeCode exchanges one code using the stored PKCE verifier and nonce under a supplied context; provider token/signature validation belongs to the adapter and an uncertain result must not be blindly replayed.
	// ExchangeCode обменивает один code с сохранёнными PKCE verifier и nonce и переданным context; provider token и signature validation принадлежит адаптеру, а uncertain result нельзя слепо повторять.
	ExchangeCode(context.Context, string, string, string) (oidcverifier.BrowserTokens, error)
}

// LoginTransaction is an encrypted server-only OAuth state/PKCE/nonce and browser-binding record, later holding completed family/CSRF references. Sequence is storage-only; the separate HttpOnly binding secret is not the redirect-visible OAuth state.
// LoginTransaction — encrypted server-only record OAuth state, PKCE, nonce и browser binding, позднее содержащая completed family и CSRF references. Sequence относится только к storage; отдельный HttpOnly binding secret не равен видимому в redirect OAuth state.
type LoginTransaction struct {
	ID          string
	State       string
	BindingHash string
	Nonce       string
	Verifier    string
	Phase       string
	ExpiresAt   time.Time
	Purpose     *LoginPurpose
	FamilyID    string
	CSRF        string
	Sequence    uint64 `json:"-"`
}

// Logins coordinates encrypted CAS login transactions, a trusted provider adapter and the process lifecycle. It does not own browser cookie transport or create a background worker.
// Logins согласует encrypted CAS login transactions, trusted provider adapter и process lifecycle без владения browser cookie transport и создания фонового worker.
type Logins struct {
	store     FamilyStorage
	provider  LoginProvider
	keys      *Store
	lifecycle context.Context
	now       func() time.Time
}

// NewLogins checks lifecycle/storage/provider/current-key dependencies and retains them with the wall clock without reading a record or contacting the provider.
// NewLogins проверяет lifecycle, storage, provider и current-key dependencies и сохраняет их с wall clock без чтения record и обращения к provider.
func NewLogins(lifecycle context.Context, store FamilyStorage, provider LoginProvider, keys *Store) (*Logins, error) {
	if lifecycle == nil || store == nil || provider == nil || keys == nil || keys.current == nil {
		return nil, errors.New("OIDC login configuration is invalid")
	}
	return &Logins{store: store, provider: provider, keys: keys, lifecycle: lifecycle, now: time.Now}, nil
}

// Start validates optional purpose, generates separate state/nonce/verifier/binding secrets, requests a PKCE-S256 URL with fresh login for any purpose, then persists PENDING before returning URL plus ID/binding cookie value. A failed persistence returns neither usable redirect nor binding.
// Start проверяет optional purpose, создаёт отдельные state, nonce, verifier и binding secrets, запрашивает PKCE-S256 URL со свежим входом для любой purpose, затем сохраняет PENDING до возврата URL и ID/binding cookie value. При persistence failure пригодные redirect и binding не выдаются.
func (logins *Logins) Start(ctx context.Context, purpose *LoginPurpose, fresh bool) (string, string, error) {
	if !validLoginPurpose(purpose) {
		return "", "", errors.New("OIDC login purpose is invalid")
	}
	state, err := randomOpaque()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomOpaque()
	if err != nil {
		return "", "", err
	}
	verifier, err := randomOpaque()
	if err != nil {
		return "", "", err
	}
	binding, err := randomOpaque()
	if err != nil {
		return "", "", err
	}
	bindingHash := sha256.Sum256([]byte(binding))
	transaction := LoginTransaction{ID: uuid.NewString(), State: state, BindingHash: hex.EncodeToString(bindingHash[:]),
		Nonce: nonce, Verifier: verifier, Phase: "PENDING", ExpiresAt: logins.now().Add(loginLifetime), Purpose: purpose}
	challenge := sha256.Sum256([]byte(verifier))
	authorization, err := logins.provider.AuthorizationURL(state, nonce, base64.RawURLEncoding.EncodeToString(challenge[:]), fresh || purpose != nil)
	if err != nil {
		return "", "", err
	}
	if _, err := logins.write(ctx, transaction); err != nil {
		return "", "", err
	}
	// The UUID grants nothing without the second random half; callback binding
	// is checked against the encrypted owner record before CAS and provider I/O.
	// UUID ничего не разрешает без второй случайной половины; callback binding
	// сверяется с encrypted owner record до CAS и provider I/O.
	return authorization, transaction.ID + "." + binding, nil
}

// Exchange verifies protected browser binding/state/expiry, returns safe completed references on replay or CAS-reserves EXCHANGING before one lifecycle-bounded provider call. Failed or uncertain exchange leaves the consumed phase and returns reauthentication; completion and family creation are separate caller steps.
// Exchange проверяет protected browser binding, state и expiry, возвращает safe completed references при replay либо через CAS резервирует EXCHANGING до одного lifecycle-bounded provider call. Failed и uncertain exchange оставляет потреблённую фазу и возвращает reauthentication; completion и family creation выполняет caller отдельно.
func (logins *Logins) Exchange(ctx context.Context, id, binding, state, code string) (LoginTransaction, oidcverifier.BrowserTokens, error) {
	transaction, err := logins.read(ctx, id)
	if err != nil {
		return LoginTransaction{}, oidcverifier.BrowserTokens{}, err
	}
	actual := sha256.Sum256([]byte(binding))
	if len(binding) != 43 || subtle.ConstantTimeCompare([]byte(transaction.BindingHash), []byte(hex.EncodeToString(actual[:]))) != 1 ||
		subtle.ConstantTimeCompare([]byte(transaction.State), []byte(state)) != 1 || !transaction.ExpiresAt.After(logins.now()) {
		return LoginTransaction{}, oidcverifier.BrowserTokens{}, ErrReauthentication
	}
	if transaction.Phase == "COMPLETED" {
		return transaction, oidcverifier.BrowserTokens{}, nil
	}
	if transaction.Phase != "PENDING" {
		return LoginTransaction{}, oidcverifier.BrowserTokens{}, ErrReauthentication
	}
	transaction.Phase = "EXCHANGING"
	transaction, err = logins.write(ctx, transaction)
	if err != nil {
		return LoginTransaction{}, oidcverifier.BrowserTokens{}, err
	}
	effect, cancel := context.WithTimeout(logins.lifecycle, refreshBudget)
	defer cancel()
	tokens, err := logins.provider.ExchangeCode(effect, code, transaction.Verifier, transaction.Nonce)
	if err != nil {
		return LoginTransaction{}, oidcverifier.BrowserTokens{}, ErrReauthentication
	}
	// Persisted EXCHANGING survives process loss; an uncertain outcome or crash
	// before Complete does not permit a second use of the authorization code.
	// Сохранённое EXCHANGING переживает потерю процесса; uncertain outcome и crash
	// до Complete не разрешают повторное применение authorization code.
	return transaction, tokens, nil
}

// Complete requires an EXCHANGING snapshot with storage sequence, UUID-shaped family ID and forty-three-character CSRF, then CAS-stores COMPLETED references while clearing verifier/nonce. It does not create the family or repeat the provider exchange.
// Complete требует EXCHANGING snapshot со storage sequence, UUID-подобным family ID и 43-символьным CSRF, затем через CAS сохраняет COMPLETED references и очищает verifier и nonce. Family не создаётся и provider exchange не повторяется.
func (logins *Logins) Complete(ctx context.Context, transaction LoginTransaction, familyID, csrf string) error {
	if transaction.Phase != "EXCHANGING" || transaction.Sequence == 0 || uuid.Validate(familyID) != nil || len(csrf) != 43 {
		return ErrReauthentication
	}
	transaction.Phase, transaction.FamilyID, transaction.CSRF = "COMPLETED", familyID, csrf
	transaction.Verifier, transaction.Nonce = "", ""
	_, err := logins.write(ctx, transaction)
	return err
}

// write JSON-encodes the transaction, encrypts it under the current key and exact login-ID AAD, then compare-and-swaps its storage sequence. It does not independently validate every phase/input field or retry a conflict.
// write кодирует transaction в JSON, шифрует current key с exact login-ID AAD, затем compare-and-swap-ит storage sequence. Каждая phase и input field независимо не проверяются и conflict не повторяется.
func (logins *Logins) write(ctx context.Context, transaction LoginTransaction) (LoginTransaction, error) {
	plain, err := json.Marshal(transaction)
	if err != nil {
		return LoginTransaction{}, ErrReauthentication
	}
	sealed, err := logins.keys.sealState(plain, "browser-login-v1:"+transaction.ID)
	if err != nil {
		return LoginTransaction{}, err
	}
	record, err := logins.store.CompareAndSwap(ctx, transaction.ID, transaction.Sequence, sealed)
	if err != nil {
		return LoginTransaction{}, err
	}
	transaction.Sequence = record.Sequence
	return transaction, nil
}

// read requires a UUID-shaped ID, authenticates the stored ciphertext with current/previous keys and exact login AAD, then rejects unknown/trailing JSON, mismatched ID, zero sequence and malformed state/binding/purpose shape. Phase and expiry are checked later by Exchange; duplicate keys are not rejected here.
// read требует UUID-подобный ID, аутентифицирует сохранённый ciphertext current и previous keys с exact login AAD, затем отклоняет unknown и trailing JSON, чужой ID, zero sequence и неверную форму state, binding и purpose. Phase и expiry позднее проверяет Exchange; duplicate keys здесь не отклоняются.
func (logins *Logins) read(ctx context.Context, id string) (LoginTransaction, error) {
	if uuid.Validate(id) != nil {
		return LoginTransaction{}, ErrReauthentication
	}
	record, err := logins.store.Read(ctx, id)
	if err != nil {
		return LoginTransaction{}, err
	}
	plain, err := logins.keys.openState(record.Ciphertext, "browser-login-v1:"+id)
	if err != nil {
		return LoginTransaction{}, ErrReauthentication
	}
	var transaction LoginTransaction
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&transaction) != nil || decoder.Decode(new(any)) != io.EOF || transaction.ID != id || record.Sequence == 0 ||
		len(transaction.State) != 43 || len(transaction.BindingHash) != 64 || !validLoginPurpose(transaction.Purpose) {
		return LoginTransaction{}, ErrReauthentication
	}
	transaction.Sequence = record.Sequence
	return transaction, nil
}

// randomOpaque fills thirty-two cryptographic random bytes and returns unpadded URL-safe Base64, mapping entropy failure to the fixed unavailable sentinel without exposing partial bytes.
// randomOpaque заполняет 32 cryptographic random bytes и возвращает unpadded URL-safe Base64, переводя entropy failure в фиксированный unavailable sentinel без раскрытия частичных байтов.
func randomOpaque() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", browserstate.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// validLoginPurpose accepts no purpose or exactly one supported secret-reveal/receipt-reconciliation binding and rejects mixed fields. It validates local grammar, not user freshness, ownership or a human approval.
// validLoginPurpose допускает отсутствие purpose либо ровно одну поддержанную secret-reveal или receipt-reconciliation binding и отклоняет mixed fields. Проверяется local grammar, а не user freshness, ownership и human approval.
func validLoginPurpose(p *LoginPurpose) bool {
	if p == nil {
		return true
	}
	switch p.Kind {
	case ElevationKindRuntimeSecretReveal:
		return ValidRuntimeSecretBinding(p.ScopeKind, p.OrganizationRef, p.ProjectRef, p.SecretRef) && p.ReceiptRef == "" && p.ReceiptVersion == 0 && p.ReceiptDigest == ""
	case ElevationKindEmailReconciliation:
		return ValidEmailReceiptBinding(p.ReceiptRef, p.ReceiptVersion, p.ReceiptDigest) && p.ProjectRef == "" && p.SecretRef == "" && p.ScopeKind == "" && p.OrganizationRef == ""
	default:
		return false
	}
}
