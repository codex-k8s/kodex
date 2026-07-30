package internalrpcauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyCanonicalJSON(t *testing.T) {
	key := testJWK(t, "signer-g1")
	expect := ProtectedHeaderExpectation{
		Type:  "mattercodex-internal-rpc-authorization-context+jws",
		KeyID: key.KeyID,
	}
	compact, err := SignCanonicalJSON(
		map[string]any{"z": 2, "a": "first"},
		key,
		expect,
	)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	verified, err := VerifyCanonicalJSON(compact, key.PublicOnly(), expect)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if string(verified.CanonicalPayload) != `{"a":"first","z":2}` {
		t.Fatalf("payload is not canonical: %s", verified.CanonicalPayload)
	}
}

func TestVerifyRejectsHeaderAndPayloadMutation(t *testing.T) {
	key := testJWK(t, "signer-g1")
	expect := ProtectedHeaderExpectation{Type: "expected+jws", KeyID: key.KeyID}
	compact, err := SignCanonicalJSON(map[string]any{"a": 1}, key, expect)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	parts := strings.Split(compact, ".")
	parts[0] = base64.RawURLEncoding.EncodeToString(
		[]byte(`{"alg":"ES256","crit":["mcxv"],"kid":"signer-g1","mcxv":1,"typ":"wrong+jws"}`),
	)
	if _, err := VerifyCanonicalJSON(strings.Join(parts, "."), key.PublicOnly(), expect); !errors.Is(err, ErrProtectedHeader) {
		t.Fatalf("wrong typ error = %v", err)
	}
}

func TestStrictJSONRejectsDuplicateFields(t *testing.T) {
	var value struct {
		A int `json:"a"`
	}
	if err := decodeStrictJSON([]byte(`{"a":1,"a":2}`), &value); err == nil {
		t.Fatal("duplicate field accepted")
	}
}

func TestValidateTimes(t *testing.T) {
	now := time.Unix(100, 0)
	if err := ValidateTimes(now, now.Add(-time.Second), now.Add(-time.Second), now.Add(20*time.Second), 30*time.Second, 5*time.Second); err != nil {
		t.Fatalf("valid time rejected: %v", err)
	}
	if err := ValidateTimes(now, now, now, now.Add(31*time.Second), 30*time.Second, 5*time.Second); err == nil {
		t.Fatal("overlong lifetime accepted")
	}
}

func testJWK(t *testing.T, keyID string) ES256Key {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return ES256Key{KeyID: keyID, Public: &key.PublicKey, Private: key}
}
