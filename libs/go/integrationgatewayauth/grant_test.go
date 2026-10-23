package integrationgatewayauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// TestContinuationGrantRoundTripAndDeterministicTamperRejection exercises a
// valid signed continuation and rejects changed signature bytes and expiry.
// RU: TestContinuationGrantRoundTripAndDeterministicTamperRejection проверяет
// допустимое подписанное продолжение и отказ после подмены подписи и истечения срока.
func TestContinuationGrantRoundTripAndDeterministicTamperRejection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key := testKey(t, 3)
	privateFile := writePrivate(t, key)
	keysetFile := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 3, HighWatermark: 3, ServedGeneration: 3,
		Keys: []PublicKeyRef{{Generation: 3, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	config := testConfig(3)
	signer, err := NewSigner(config, privateFile, keysetFile)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	verifier, err := NewVerifier(config, keysetFile)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }
	claims := testClaims(config, now)
	compact, err := signer.Sign(context.Background(), claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verified, err := verifier.Verify(context.Background(), compact)
	if err != nil || verified.InvocationID != claims.InvocationID || verified.ContinuationID != claims.ContinuationID {
		t.Fatalf("Verify() = %#v, %v", verified, err)
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		t.Fatal("compact JWS has invalid shape")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) == 0 {
		t.Fatal("compact JWS signature is invalid")
	}
	signature[0] ^= 0x01
	tampered := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(signature)
	if tampered == compact {
		t.Fatal("tamper helper did not change signed bytes")
	}
	if _, err := verifier.Verify(context.Background(), tampered); err == nil {
		t.Fatal("tampered grant was accepted")
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := verifier.Verify(context.Background(), compact); err == nil {
		t.Fatal("expired grant was accepted")
	}
}

// TestVerifierRotationClosedStates accepts PREVIOUS only during overlap and
// rejects NEXT, RETIRED, unknown and expired generations.
// RU: TestVerifierRotationClosedStates принимает PREVIOUS лишь в период
// перекрытия и отклоняет NEXT, RETIRED, неизвестное и просроченное поколения.
func TestVerifierRotationClosedStates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	previous, current, next, retired := testKey(t, 2), testKey(t, 3), testKey(t, 4), testKey(t, 1)
	keyset := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 3, HighWatermark: 3, ServedGeneration: 3,
		Keys: []PublicKeyRef{
			{Generation: 1, Status: keyStatusRetired, JWK: publicRaw(t, retired)},
			{Generation: 2, Status: keyStatusPrevious, AcceptUntil: now.Add(time.Hour).Unix(), JWK: publicRaw(t, previous)},
			{Generation: 3, Status: keyStatusCurrent, JWK: publicRaw(t, current)},
			{Generation: 4, Status: keyStatusNext, JWK: publicRaw(t, next)},
		},
	})
	verifier, err := NewVerifier(testConfig(3), keyset)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return now }
	previousToken := signForGeneration(t, previous, 2, now)
	if _, err := verifier.Verify(context.Background(), previousToken); err != nil {
		t.Fatalf("PREVIOUS overlap token rejected: %v", err)
	}
	for name, token := range map[string]string{
		"retired": signForGeneration(t, retired, 1, now),
		"next":    signForGeneration(t, next, 4, now),
		"unknown": signForGeneration(t, testKey(t, 7), 7, now),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), token); err == nil {
				t.Fatal("closed rotation state was accepted")
			}
		})
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := verifier.Verify(context.Background(), previousToken); err == nil {
		t.Fatal("expired PREVIOUS overlap was accepted")
	}
}

// TestVerifierRejectsRollbackSnapshot proves a verifier configured for a newer
// generation does not load an older served keyset.
// RU: TestVerifierRejectsRollbackSnapshot проверяет, что конфигурация нового
// поколения не принимает старый обслуживаемый набор ключей.
func TestVerifierRejectsRollbackSnapshot(t *testing.T) {
	key := testKey(t, 2)
	rollback := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 2, HighWatermark: 2, ServedGeneration: 2,
		Keys: []PublicKeyRef{{Generation: 2, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	if _, err := NewVerifier(testConfig(3), rollback); err == nil {
		t.Fatal("rollback keyset was accepted for configured generation")
	}
}

// signForGeneration builds a matching keyset and signs one fixture grant for
// rotation tests; it never reuses a different generation's private key.
// RU: signForGeneration создаёт соответствующий набор ключей и подписывает
// fixture для тестов ротации без подмены поколения закрытого ключа.
func signForGeneration(t *testing.T, key internalrpcauth.ES256Key, generation uint64, now time.Time) string {
	t.Helper()
	keyset := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: generation, HighWatermark: generation, ServedGeneration: generation,
		Keys: []PublicKeyRef{{Generation: generation, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	signer, err := NewSigner(testConfig(generation), writePrivate(t, key), keyset)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(context.Background(), testClaims(testConfig(generation), now))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// testConfig provides exact test identities and an eight-day maximum grant
// lifetime for a selected signer generation.
// RU: testConfig задаёт точные тестовые идентификаторы и предельный срок
// полномочия в восемь дней для выбранного поколения подписи.
func testConfig(generation uint64) Config {
	return Config{
		Issuer:   "https://control-plane.test/authority/integration-continuation",
		Audience: "urn:kodex:integration-continuation", WorkloadID: "integration-gateway",
		CallerSPIFFEID: "spiffe://kodex.test/ns/system/sa/integration-gateway",
		Generation:     generation, MaximumTTL: 8 * 24 * time.Hour,
	}
}

// testClaims pins a fixture to one session, turn, attempt, runtime revision and
// operation with a one-hour expiry; tests can vary the key generation.
// RU: testClaims закрепляет fixture за сессией, ходом, попыткой, ревизией среды
// и операцией со сроком в один час; тесты могут менять поколение ключа.
func testClaims(config Config, now time.Time) Claims {
	return Claims{
		Version: 1, Issuer: config.Issuer, Audience: config.Audience,
		Purpose: PurposeTransition, Subject: "actor-1", OrganizationID: "tenant-1", ProjectID: "project-1",
		WorkloadID: config.WorkloadID, CallerSPIFFEID: config.CallerSPIFFEID,
		SessionID: "session-1", TurnID: "turn-2", Attempt: 2, InputSHA256: digest64("a"),
		RuntimeRevisionID: "revision-2", RuntimeRevisionVersion: 2, RuntimeRevisionSHA256: digest64("b"),
		GrantGeneration: 7, ContinuationID: "continuation-1", ContinuationVersion: 4,
		ContinuationFence: 4, InvocationID: "invocation-1",
		AllowedOperationIDs: []string{"control.integration-execution.complete"},
		JTI:                 "grant-1", IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
}

// testKey generates a fresh ES256 key with a generation-labelled key ID for
// one rotation fixture; its material stays inside the test process.
// RU: testKey создаёт новый ключ ES256 с ID выбранного поколения для fixture
// ротации; его материал остаётся внутри тестового процесса.
func testKey(t *testing.T, generation uint64) internalrpcauth.ES256Key {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key("integration-continuation-g" + string(rune('0'+generation)))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// publicRaw serializes only the public JWK of a fixture key for the keyset.
// RU: publicRaw сериализует для набора ключей лишь открытый JWK тестового ключа.
func publicRaw(t *testing.T, key internalrpcauth.ES256Key) json.RawMessage {
	t.Helper()
	raw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// writePrivate serializes a fixture private JWK into a restricted temporary
// file to exercise the production secure-file loader.
// RU: writePrivate записывает закрытый JWK fixture во временный файл с
// ограниченными правами, чтобы проверить штатную безопасную загрузку.
func writePrivate(t *testing.T, key internalrpcauth.ES256Key) string {
	t.Helper()
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "private.jwk", raw)
}

// writeKeyset stores a canonical public-key snapshot as a restricted temporary
// file, preserving the same decoder boundary used by production.
// RU: writeKeyset сохраняет канонический снимок открытых ключей во временном
// файле с ограниченными правами и проверяет тот же декодер, что и production.
func writeKeyset(t *testing.T, keyset PublicKeySet) string {
	t.Helper()
	raw, err := internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "public-keyset.json", raw)
}

// writeTestFile writes one fixture below a new test-owned temporary directory
// with read-only permissions and fails the test on an I/O error.
// RU: writeTestFile записывает fixture в новый временный каталог теста с
// правами только на чтение и завершает тест при ошибке ввода-вывода.
func writeTestFile(t *testing.T, name string, raw []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if os.WriteFile(file, raw, 0o400) != nil {
		t.Fatal("write test file")
	}
	return file
}

// digest64 repeats a fixture character 64 times to make a SHA-256-shaped
// input value; it does not calculate a cryptographic digest.
// RU: digest64 повторяет символ fixture 64 раза для значения формы SHA-256;
// криптографический дайджест эта функция не вычисляет.
func digest64(symbol string) string {
	value := ""
	for range 64 {
		value += symbol
	}
	return value
}
