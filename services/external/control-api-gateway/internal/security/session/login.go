package session

import (
	"bytes"
	"context"
	"crypto/cipher"
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

const loginLifetime = 5 * time.Minute

type LoginPurpose struct {
	Kind           string
	ProjectRef     string
	SecretRef      string
	ReceiptRef     string
	ReceiptVersion int64
	ReceiptDigest  string
}

type LoginProvider interface {
	AuthorizationURL(string, string, string, bool) (string, error)
	ExchangeCode(context.Context, string, string, string) (oidcverifier.BrowserTokens, error)
}

// LoginTransaction хранится только server-side. Binding приходит из отдельного
// HttpOnly cookie и не совпадает с OAuth state, доступным browser redirect.
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

type Logins struct {
	store     FamilyStorage
	provider  LoginProvider
	keys      *Store
	lifecycle context.Context
	now       func() time.Time
}

func NewLogins(lifecycle context.Context, store FamilyStorage, provider LoginProvider, keys *Store) (*Logins, error) {
	if lifecycle == nil || store == nil || provider == nil || keys == nil || keys.current == nil {
		return nil, errors.New("OIDC login configuration is invalid")
	}
	return &Logins{store: store, provider: provider, keys: keys, lifecycle: lifecycle, now: time.Now}, nil
}

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
	// UUID не даёт доступа без второй случайной половины; значение cookie
	// сверяется с hash внутри AEAD owner-записи до CAS и внешнего запроса.
	return authorization, transaction.ID + "." + binding, nil
}

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
	// EXCHANGING переживает crash. Код никогда не применяется повторно даже
	// при неизвестном внешнем outcome или утрате процесса до Complete.
	return transaction, tokens, nil
}

func (logins *Logins) Complete(ctx context.Context, transaction LoginTransaction, familyID, csrf string) error {
	if transaction.Phase != "EXCHANGING" || transaction.Sequence == 0 || uuid.Validate(familyID) != nil || len(csrf) != 43 {
		return ErrReauthentication
	}
	transaction.Phase, transaction.FamilyID, transaction.CSRF = "COMPLETED", familyID, csrf
	transaction.Verifier, transaction.Nonce = "", ""
	_, err := logins.write(ctx, transaction)
	return err
}

func (logins *Logins) write(ctx context.Context, transaction LoginTransaction) (LoginTransaction, error) {
	plain, err := json.Marshal(transaction)
	if err != nil {
		return LoginTransaction{}, ErrReauthentication
	}
	nonce := make([]byte, logins.keys.current.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return LoginTransaction{}, browserstate.ErrUnavailable
	}
	sealed := logins.keys.current.Seal(nonce, nonce, plain, []byte("browser-login-v1:"+transaction.ID))
	record, err := logins.store.CompareAndSwap(ctx, transaction.ID, transaction.Sequence, sealed)
	if err != nil {
		return LoginTransaction{}, err
	}
	transaction.Sequence = record.Sequence
	return transaction, nil
}

func (logins *Logins) read(ctx context.Context, id string) (LoginTransaction, error) {
	if uuid.Validate(id) != nil {
		return LoginTransaction{}, ErrReauthentication
	}
	record, err := logins.store.Read(ctx, id)
	if err != nil {
		return LoginTransaction{}, err
	}
	decrypt := func(key cipher.AEAD) ([]byte, error) {
		if key == nil || len(record.Ciphertext) < key.NonceSize()+key.Overhead() {
			return nil, ErrReauthentication
		}
		return key.Open(nil, record.Ciphertext[:key.NonceSize()], record.Ciphertext[key.NonceSize():], []byte("browser-login-v1:"+id))
	}
	plain, err := decrypt(logins.keys.current)
	if err != nil {
		plain, err = decrypt(logins.keys.previous)
	}
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

func randomOpaque() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", browserstate.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validLoginPurpose(p *LoginPurpose) bool {
	if p == nil {
		return true
	}
	switch p.Kind {
	case ElevationKindRuntimeSecretReveal:
		return validOpaqueReference(p.ProjectRef) && validOpaqueReference(p.SecretRef) && p.ReceiptRef == "" && p.ReceiptVersion == 0 && p.ReceiptDigest == ""
	case ElevationKindEmailReconciliation:
		return ValidEmailReceiptBinding(p.ReceiptRef, p.ReceiptVersion, p.ReceiptDigest) && p.ProjectRef == "" && p.SecretRef == ""
	default:
		return false
	}
}
