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

// TestSignAndVerifyCanonicalJSON verifies that signing sorts input JSON into
// JCS form and verification returns those exact canonical payload bytes.
// RU: TestSignAndVerifyCanonicalJSON проверяет JCS-упорядочивание входного
// JSON при подписи и возврат точных канонических байтов после проверки.
func TestSignAndVerifyCanonicalJSON(t *testing.T) {
	key := testJWK(t, "signer-g1")
	expect := ProtectedHeaderExpectation{
		Type:  "kodex-internal-rpc-authorization-context+jws",
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

// TestVerifyRejectsHeaderAndPayloadMutation checks that a changed protected
// typ is rejected before it can select a different authority contract.
// RU: TestVerifyRejectsHeaderAndPayloadMutation проверяет отказ при замене
// защищённого typ до выбора другого контракта полномочий.
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

// TestStrictJSONRejectsDuplicateFields prevents ambiguous object keys from
// silently overriding a claim in Go's ordinary JSON decoder.
// RU: TestStrictJSONRejectsDuplicateFields запрещает неоднозначным ключам
// объекта незаметно перезаписать claim в обычном Go JSON-декодере.
func TestStrictJSONRejectsDuplicateFields(t *testing.T) {
	var value struct {
		A int `json:"a"`
	}
	if err := decodeStrictJSON([]byte(`{"a":1,"a":2}`), &value); err == nil {
		t.Fatal("duplicate field accepted")
	}
}

// TestDecodeStrictJSONAllowsNonCanonicalConfiguration distinguishes accepted
// unsigned configuration formatting from the stricter canonical JWS payload.
// RU: TestDecodeStrictJSONAllowsNonCanonicalConfiguration разделяет
// допускаемое форматирование неподписанной конфигурации и строгий JWS payload.
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

// TestExplicitCompactLimitDoesNotExpandDefaultBoundary verifies that a larger
// document requires an explicit limit at both signing and verification.
// RU: TestExplicitCompactLimitDoesNotExpandDefaultBoundary проверяет, что
// крупный документ требует явного лимита и при подписи, и при проверке.
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

// TestValidateTimes covers exact TTL, matching issued/not-before, future
// issuance and the expiry edge of the bounded clock-skew window.
// RU: TestValidateTimes покрывает точный TTL, совпадение issued/not-before,
// будущую выдачу и границу истечения с учётом расхождения часов.
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

// TestParsePrivateJWKValidatesDerivedPublicKey rejects a private scalar whose
// derived public point differs from the declared JWK coordinates.
// RU: TestParsePrivateJWKValidatesDerivedPublicKey отклоняет приватный скаляр,
// дающий публичную точку не из заявленных координат JWK.
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

// TestGenerateMarshalAndParseES256Key round-trips signer and verifier JWKs
// and compares the public thumbprint after private material is removed.
// RU: TestGenerateMarshalAndParseES256Key проверяет круговую сериализацию
// signer/verifier JWK и совпадение публичного thumbprint без приватного ключа.
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

// TestKeyGeneration accepts bounded canonical kid suffixes and rejects
// missing, zero, padded, negative or overflowing generations.
// RU: TestKeyGeneration допускает ограниченный канонический суффикс kid и
// отклоняет отсутствие, ноль, ведущий ноль, знак и переполнение поколения.
func TestKeyGeneration(t *testing.T) {
	t.Parallel()
	for keyID, expected := range map[string]uint64{
		"runtime-controller-platform-worker-g1": 1,
		"issuer-g42":                            42,
		"issuer-g9007199254740991":              9007199254740991,
	} {
		keyID, expected := keyID, expected
		t.Run(keyID, func(t *testing.T) {
			t.Parallel()
			actual, err := KeyGeneration(keyID)
			if err != nil || actual != expected {
				t.Fatalf("KeyGeneration(%q) = %d, %v; ожидалось %d", keyID, actual, err, expected)
			}
		})
	}
	for _, keyID := range []string{"", "g1", "issuer", "issuer-g", "issuer-g0", "issuer-g01", "issuer-g-1", "issuer-g9007199254740992"} {
		if _, err := KeyGeneration(keyID); !errors.Is(err, ErrKey) {
			t.Fatalf("неканонический kid %q принят: %v", keyID, err)
		}
	}
}

// testJWK creates a fresh P-256 signer fixture under the requested kid so
// tests do not rely on shared key material across cases.
// RU: testJWK создаёт новую fixture подписанта P-256 с заданным kid, чтобы
// тесты не разделяли ключевой материал между сценариями.
func testJWK(t *testing.T, keyID string) ES256Key {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return ES256Key{KeyID: keyID, Public: &key.PublicKey, Private: key}
}

// encodePrivateJWK serializes explicit matching or mismatching coordinates
// for parser tests without applying the production marshal validation first.
// RU: encodePrivateJWK сериализует согласованные либо расходящиеся части
// ключа для тестов parser без предварительной production-проверки marshal.
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
