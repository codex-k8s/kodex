package internalrpcauth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
	if err := ValidateTimes(now, now, now, now.Add(30*time.Second), 30*time.Second, 5*time.Second); err != nil {
		t.Fatalf("valid time rejected: %v", err)
	}
	for name, times := range map[string][3]time.Time{
		"future-issued-at": {
			now.Add(6 * time.Second),
			now.Add(6 * time.Second),
			now.Add(36 * time.Second),
		},
		"past-issued-at": {
			now.Add(-6 * time.Second),
			now.Add(-6 * time.Second),
			now.Add(24 * time.Second),
		},
		"not-before-differs": {
			now,
			now.Add(-time.Second),
			now.Add(30 * time.Second),
		},
		"shortened-lifetime": {
			now,
			now,
			now.Add(29 * time.Second),
		},
		"extended-lifetime": {
			now,
			now,
			now.Add(31 * time.Second),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTimes(
				now,
				times[0],
				times[1],
				times[2],
				30*time.Second,
				5*time.Second,
			); err == nil {
				t.Fatal("invalid time binding accepted")
			}
		})
	}
}

func TestParsePrivateJWKValidatesDerivedPublicKey(t *testing.T) {
	key := testJWK(t, "signer-g1")
	encoded := encodePrivateJWK(t, key)

	parsed, err := ParsePrivateJWK(encoded)
	if err != nil {
		t.Fatalf("parse private JWK: %v", err)
	}
	gotPublic, err := parsed.Public.Bytes()
	if err != nil {
		t.Fatalf("encode parsed public key: %v", err)
	}
	wantPublic, err := key.Public.Bytes()
	if err != nil {
		t.Fatalf("encode source public key: %v", err)
	}
	if !bytes.Equal(gotPublic, wantPublic) {
		t.Fatal("parsed public key differs from the JWK coordinates")
	}

	mismatched := key
	mismatched.Private = testJWK(t, key.KeyID).Private
	if _, err := ParsePrivateJWK(encodePrivateJWK(t, mismatched)); !errors.Is(err, ErrKey) {
		t.Fatalf("mismatched private/public JWK error = %v", err)
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

func encodePrivateJWK(t *testing.T, key ES256Key) []byte {
	t.Helper()
	publicBytes, err := key.Public.Bytes()
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	privateBytes, err := key.Private.Bytes()
	if err != nil {
		t.Fatalf("encode private key: %v", err)
	}
	encoded, err := json.Marshal(encodedJWK{
		KTY:    "EC",
		Curve:  "P-256",
		Use:    "sig",
		KeyOps: []string{"sign"},
		Alg:    AlgorithmES256,
		KeyID:  key.KeyID,
		X:      base64.RawURLEncoding.EncodeToString(publicBytes[1:33]),
		Y:      base64.RawURLEncoding.EncodeToString(publicBytes[33:65]),
		D:      base64.RawURLEncoding.EncodeToString(privateBytes),
	})
	if err != nil {
		t.Fatalf("marshal private JWK: %v", err)
	}
	return encoded
}
