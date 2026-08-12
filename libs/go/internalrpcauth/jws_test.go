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

func TestDecodeStrictJSONAllowsNonCanonicalConfiguration(t *testing.T) {
	var value struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	raw := []byte("{\n  \"version\": 1,\n  \"name\": \"policy\"\n}")
	if err := DecodeStrictJSON(raw, &value); err != nil {
		t.Fatalf("decode strict non-canonical JSON: %v", err)
	}
	if value.Name != "policy" || value.Version != 1 {
		t.Fatalf("unexpected decoded configuration: %#v", value)
	}
	if err := DecodeCanonicalJSON(raw, &value); !errors.Is(err, ErrCanonicalPayload) {
		t.Fatalf("canonical decoder accepted non-canonical input: %v", err)
	}
}

func TestExplicitCompactLimitDoesNotExpandDefaultBoundary(t *testing.T) {
	key := testJWK(t, "large-signer-g1")
	expect := ProtectedHeaderExpectation{Type: "large-document+jws", KeyID: key.KeyID}
	payload := map[string]any{"data": strings.Repeat("x", MaxCompactJWSBytes)}
	if _, err := SignCanonicalJSON(payload, key, expect); !errors.Is(err, ErrMalformedJWS) {
		t.Fatalf("default compact boundary accepted a large document: %v", err)
	}
	compact, err := SignCanonicalJSONWithLimit(payload, key, expect, 1<<20)
	if err != nil {
		t.Fatalf("sign large service document: %v", err)
	}
	if _, err := VerifyCanonicalJSON(compact, key.PublicOnly(), expect); !errors.Is(err, ErrMalformedJWS) {
		t.Fatalf("default verifier boundary accepted a large document: %v", err)
	}
	if _, err := VerifyCanonicalJSONWithLimit(compact, key.PublicOnly(), expect, 1<<20); err != nil {
		t.Fatalf("verify large service document: %v", err)
	}
}

func TestValidateTimes(t *testing.T) {
	now := time.Unix(100, 0)
	if err := ValidateTimes(now, now, now, now.Add(30*time.Second), 30*time.Second, 5*time.Second); err != nil {
		t.Fatalf("valid time rejected: %v", err)
	}
	for name, times := range map[string][3]time.Time{
		"future-issued-at": {
			now.Add(time.Second),
			now.Add(time.Second),
			now.Add(31 * time.Second),
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
	if err := ValidateTimes(
		now,
		now.Add(-6*time.Second),
		now.Add(-6*time.Second),
		now.Add(24*time.Second),
		30*time.Second,
		5*time.Second,
	); err != nil {
		t.Fatalf("unexpired past-issued token rejected: %v", err)
	}
	if err := ValidateTimes(
		now,
		now.Add(-35*time.Second),
		now.Add(-35*time.Second),
		now.Add(-5*time.Second),
		30*time.Second,
		5*time.Second,
	); err == nil {
		t.Fatal("token at the expired skew boundary was accepted")
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

func TestGenerateMarshalAndParseES256Key(t *testing.T) {
	t.Parallel()
	generated, err := GenerateES256Key("generated-g1")
	if err != nil {
		t.Fatalf("GenerateES256Key() error = %v", err)
	}
	privateRaw, err := MarshalPrivateJWK(generated)
	if err != nil {
		t.Fatalf("MarshalPrivateJWK() error = %v", err)
	}
	parsedPrivate, err := ParsePrivateJWK(privateRaw)
	if err != nil {
		t.Fatalf("ParsePrivateJWK() error = %v", err)
	}
	publicRaw, err := MarshalPublicJWK(parsedPrivate.PublicOnly())
	if err != nil {
		t.Fatalf("MarshalPublicJWK() error = %v", err)
	}
	parsedPublic, err := ParsePublicJWK(publicRaw)
	if err != nil {
		t.Fatalf("ParsePublicJWK() error = %v", err)
	}
	expected, err := PublicJWKThumbprintSHA256(generated.PublicOnly())
	if err != nil {
		t.Fatalf("PublicJWKThumbprintSHA256(generated) error = %v", err)
	}
	actual, err := PublicJWKThumbprintSHA256(parsedPublic)
	if err != nil {
		t.Fatalf("PublicJWKThumbprintSHA256(parsed) error = %v", err)
	}
	if actual != expected {
		t.Fatalf("thumbprint = %q, expected %q", actual, expected)
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
