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
	lastKnownGoodWindow = 2 * time.Minute
	maximumJWKSBytes    = 1 << 20
)

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

func newBoundedKeySet(client *http.Client, endpoint string) *boundedKeySet {
	return &boundedKeySet{client: client, endpoint: endpoint, now: time.Now, seenDigests: make(map[string]struct{})}
}

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
	if decoder.Decode(&keys) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validJWKS(keys, set.now().UTC()) {
		return jose.JSONWebKeySet{}, "", true, errors.New("OIDC JWKS is invalid")
	}
	canonical, err := json.Marshal(keys)
	if err != nil {
		return jose.JSONWebKeySet{}, "", true, errors.New("canonicalize OIDC JWKS")
	}
	digest := sha256.Sum256(canonical)
	return keys, hex.EncodeToString(digest[:]), false, nil
}

func validJWKS(keys jose.JSONWebKeySet, now time.Time) bool {
	if len(keys.Keys) == 0 || len(keys.Keys) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(keys.Keys))
	for _, key := range keys.Keys {
		_, rsaKey := key.Key.(*rsa.PublicKey)
		if !rsaKey || !key.Valid() || !key.IsPublic() || key.KeyID == "" || key.Algorithm != string(jose.RS256) || key.Use != "sig" || key.CertificatesURL != nil {
			return false
		}
		if _, duplicate := seen[key.KeyID]; duplicate {
			return false
		}
		seen[key.KeyID] = struct{}{}
		for _, certificate := range key.Certificates {
			if certificate == nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
				return false
			}
		}
	}
	return true
}

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

func (set *boundedKeySet) verificationError() error {
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	if set.blocked || len(set.keys.Keys) == 0 || set.degraded && !set.now().UTC().Before(set.lastSuccess.Add(lastKnownGoodWindow)) {
		return ErrSigningKeysUnavailable
	}
	return nil
}

func validKeyTime(key jose.JSONWebKey, now time.Time) bool {
	for _, certificate := range key.Certificates {
		if certificate == nil || now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return false
		}
	}
	return true
}

func (set *boundedKeySet) DegradedDeadline() (time.Time, bool) {
	set.mutex.RLock()
	defer set.mutex.RUnlock()
	if set.blocked {
		return time.Time{}, true
	}
	return set.lastSuccess.Add(lastKnownGoodWindow), set.degraded
}
