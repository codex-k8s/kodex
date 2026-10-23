package oidcverifier

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const (
	// lastKnownGoodWindow limits how long previously verified keys remain usable after a transient refresh failure.
	// lastKnownGoodWindow ограничивает использование ранее проверенных ключей после временного сбоя обновления.
	lastKnownGoodWindow = 2 * time.Minute
	// maximumJWKSBytes bounds the entire remote JWKS response before parsing.
	// maximumJWKSBytes ограничивает полный ответ удалённого JWKS до разбора.
	maximumJWKSBytes = 1 << 20
)

// boundedKeySet retains the verified signing snapshot and rollback history under a lock; fatal failures block it.
// boundedKeySet хранит проверенный снимок ключей и историю отката под блокировкой; критический отказ блокирует его.
type boundedKeySet struct {
	client   *http.Client
	endpoint string
	now      func() time.Time

	mutex       sync.RWMutex
	keys        jose.JSONWebKeySet
	digest      string
	seenDigests map[string]struct{}
	lastSuccess time.Time
	degraded    bool
	blocked     bool
}

// newBoundedKeySet creates an empty, fail-closed snapshot with a fresh digest history.
// newBoundedKeySet создаёт пустой закрытый снимок со своей историей дайджестов.
func newBoundedKeySet(client *http.Client, endpoint string) *boundedKeySet {
	return &boundedKeySet{client: client, endpoint: endpoint, now: time.Now, seenDigests: make(map[string]struct{})}
}

// Refresh fetches a bounded snapshot and atomically updates keys, degradation or permanent block state.
// Refresh получает ограниченный снимок и атомарно обновляет ключи, деградацию либо состояние блокировки.
func (set *boundedKeySet) Refresh(ctx context.Context) error {
	keys, digest, fatal, err := set.fetch(ctx)
	set.mutex.Lock()
	defer set.mutex.Unlock()
	if set.blocked {
		return errors.New("OIDC signing keys are blocked")
	}
	if err != nil {
		set.degraded = true
		if fatal {
			set.blocked = true
		}
		return err
	}
	if set.digest != "" && digest != set.digest {
		if _, rollback := set.seenDigests[digest]; rollback || reusedKeyID(set.keys, keys) {
			set.blocked = true
			set.degraded = true
			return errors.New("OIDC signing-key rollback or conflict rejected")
		}
	}
	set.keys = keys
	set.digest = digest
	set.seenDigests[digest] = struct{}{}
	set.lastSuccess = set.now().UTC()
	set.degraded = false
	return nil
}

// fetch accepts a bounded, complete JWKS response and returns its canonical supported-key digest and failure class.
// fetch принимает ограниченный полный JWKS и возвращает дайджест поддерживаемых ключей и класс отказа.
func (set *boundedKeySet) fetch(ctx context.Context) (jose.JSONWebKeySet, string, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, set.endpoint, nil)
	if err != nil {
		return jose.JSONWebKeySet{}, "", true, errors.New("create OIDC JWKS request")
	}
	request.Header.Set("Accept", "application/json")
	response, err := set.client.Do(request)
	if err != nil {
		return jose.JSONWebKeySet{}, "", false, errors.New("fetch OIDC JWKS")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fatal := response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests
		return jose.JSONWebKeySet{}, "", fatal, errors.New("OIDC JWKS response status is invalid")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumJWKSBytes+1))
	if err != nil {
		return jose.JSONWebKeySet{}, "", false, errors.New("read OIDC JWKS")
	}
	if len(raw) == 0 || len(raw) > maximumJWKSBytes {
		return jose.JSONWebKeySet{}, "", true, errors.New("OIDC JWKS size is invalid")
	}
	var keys jose.JSONWebKeySet
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&keys) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return jose.JSONWebKeySet{}, "", true, errors.New("OIDC JWKS is invalid")
	}
	keys, valid := supportedSigningKeySet(keys, set.now().UTC())
	if !valid {
		return jose.JSONWebKeySet{}, "", true, errors.New("OIDC JWKS is invalid")
	}
	canonical, err := json.Marshal(keys)
	if err != nil {
		return jose.JSONWebKeySet{}, "", true, errors.New("canonicalize OIDC JWKS")
	}
	digest := sha256.Sum256(canonical)
	return keys, hex.EncodeToString(digest[:]), false, nil
}

// supportedSigningKeySet selects unique RS256 public signing keys and rejects malformed or expired supported keys.
// supportedSigningKeySet отбирает уникальные открытые ключи подписи RS256 и отвергает некорректные или просроченные.
func supportedSigningKeySet(keys jose.JSONWebKeySet, now time.Time) (jose.JSONWebKeySet, bool) {
	if len(keys.Keys) == 0 || len(keys.Keys) > 16 {
		return jose.JSONWebKeySet{}, false
	}
	seen := make(map[string]struct{}, len(keys.Keys))
	signingKeys := make([]jose.JSONWebKey, 0, len(keys.Keys))
	for _, key := range keys.Keys {
		if key.KeyID == "" {
			return jose.JSONWebKeySet{}, false
		}
		if _, duplicate := seen[key.KeyID]; duplicate {
			return jose.JSONWebKeySet{}, false
		}
		seen[key.KeyID] = struct{}{}
		if key.Algorithm != string(jose.RS256) || key.Use != "sig" {
			continue
		}
		_, rsaKey := key.Key.(*rsa.PublicKey)
		if !rsaKey || !key.Valid() || !key.IsPublic() || key.CertificatesURL != nil {
			return jose.JSONWebKeySet{}, false
		}
		for _, certificate := range key.Certificates {
			if certificate == nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
				return jose.JSONWebKeySet{}, false
			}
		}
		signingKeys = append(signingKeys, key)
	}
	if len(signingKeys) == 0 {
		return jose.JSONWebKeySet{}, false
	}
	return jose.JSONWebKeySet{Keys: signingKeys}, true
}

// reusedKeyID detects a changed key published under an existing identifier; an unchanged overlap is allowed.
// reusedKeyID находит изменённый ключ под прежним идентификатором; неизменённое пересечение разрешено.
func reusedKeyID(previous, next jose.JSONWebKeySet) bool {
	for _, oldKey := range previous.Keys {
		values := next.Key(oldKey.KeyID)
		if len(values) != 1 {
			continue
		}
		oldRaw, oldErr := json.Marshal(oldKey)
		newRaw, newErr := json.Marshal(values[0])
		if oldErr != nil || newErr != nil || !bytes.Equal(oldRaw, newRaw) {
			return true
		}
	}
	return false
}

// VerifySignature checks snapshot availability, the exact RS256 key and current certificate validity before verification.
// VerifySignature проверяет доступность снимка, точный ключ RS256 и срок сертификата до проверки подписи.
func (set *boundedKeySet) VerifySignature(_ context.Context, raw string) ([]byte, error) {
	set.mutex.RLock()
	keys := set.keys
	blocked := set.blocked
	degraded := set.degraded
	deadline := set.lastSuccess.Add(lastKnownGoodWindow)
	now := set.now().UTC()
	set.mutex.RUnlock()
	if blocked || len(keys.Keys) == 0 || degraded && !now.Before(deadline) {
		return nil, ErrSigningKeysUnavailable
	}
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return nil, errors.New("OIDC signature envelope is invalid")
	}
	header := signed.Signatures[0].Header
	if header.Algorithm != string(jose.RS256) || header.KeyID == "" {
		return nil, errors.New("OIDC signing key is not registered")
	}
	matching := keys.Key(header.KeyID)
	if len(matching) != 1 || matching[0].Algorithm != string(jose.RS256) || matching[0].Use != "sig" || !validKeyTime(matching[0], now) {
		return nil, errors.New("OIDC signing key is not registered")
	}
	payload, err := signed.Verify(matching[0].Key)
	if err != nil {
		return nil, errors.New("OIDC signature is invalid")
	}
	return payload, nil
}

// verificationError reports missing, permanently blocked or expired degraded key material before token verification.
// verificationError сообщает об отсутствии, блокировке либо истечении периода деградации ключей до проверки токена.
func (set *boundedKeySet) verificationError() error {
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	if set.blocked || len(set.keys.Keys) == 0 || set.degraded && !set.now().UTC().Before(set.lastSuccess.Add(lastKnownGoodWindow)) {
		return ErrSigningKeysUnavailable
	}
	return nil
}

// validKeyTime accepts only keys whose attached certificates are all valid at the supplied instant.
// validKeyTime принимает ключ, только если все его сертификаты действуют в указанный момент.
func validKeyTime(key jose.JSONWebKey, now time.Time) bool {
	for _, certificate := range key.Certificates {
		if certificate == nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return false
		}
	}
	return true
}

// DegradedDeadline returns the last successful refresh deadline and whether verification is degraded or blocked.
// DegradedDeadline возвращает срок от последнего успешного обновления и признак деградации либо блокировки.
func (set *boundedKeySet) DegradedDeadline() (time.Time, bool) {
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	if set.blocked {
		return time.Time{}, true
	}
	return set.lastSuccess.Add(lastKnownGoodWindow), set.degraded
}
