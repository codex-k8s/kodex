package snapshot

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// TestCredentialSignerWireTypesRemainEquivalent pins the existing canonical JSON
// fields and proves both credential records remain distinct named types. A
// manifest-generation field is rejected instead of changing the wire contract.
// TestCredentialSignerWireTypesRemainEquivalent закрепляет прежние канонические
// поля JSON и раздельность именованных записей учётных данных. Поле поколения
// манифеста отклоняется вместо изменения транспортного контракта.
func TestCredentialSignerWireTypesRemainEquivalent(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"credential_signer_generation":7,"jwk_thumbprint_sha256":"fixture","kid":"credential-key","not_after":20,"not_before":10,"public_jwk":{"kty":"EC"},"status":"CURRENT"}`)
	var readback readbackCredentialKey
	var restore restoreRoleTrustKey
	if err := internalrpcauth.DecodeCanonicalJSON(raw, &readback); err != nil {
		t.Fatalf("decode readback signer fixture: %v", err)
	}
	if err := internalrpcauth.DecodeCanonicalJSON(raw, &restore); err != nil {
		t.Fatalf("decode restore signer fixture: %v", err)
	}
	if reflect.TypeOf(readback).Name() != "readbackCredentialKey" ||
		reflect.TypeOf(restore).Name() != "restoreRoleTrustKey" {
		t.Fatal("credential signer records lost nominal identity")
	}
	for _, record := range []any{readback, restore} {
		encoded, err := internalrpcauth.CanonicalJSON(record)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatal("credential signer wire fields changed")
		}
	}
	wrong := []byte(strings.Replace(string(raw), "credential_signer_generation", "manifest_signer_generation", 1))
	if err := internalrpcauth.DecodeCanonicalJSON(wrong, &readback); err == nil {
		t.Fatal("readback signer accepted manifest-generation field")
	}
	if err := internalrpcauth.DecodeCanonicalJSON(wrong, &restore); err == nil {
		t.Fatal("restore signer accepted manifest-generation field")
	}
}

// TestValidReadbackSignerMetadata covers both interval boundaries and the shared
// key/status/generation pins with a real local ES256 public key; synthetic
// metadata does not stand in for the enclosing document's signature checks.
// TestValidReadbackSignerMetadata проверяет обе границы срока и общие привязки
// ключа, статуса и поколения с настоящим локальным открытым ключом ES256;
// синтетические метаданные не заменяют проверку подписи внешнего документа.
func TestValidReadbackSignerMetadata(t *testing.T) {
	t.Parallel()
	key, err := internalrpcauth.GenerateES256Key("readback-metadata")
	if err != nil {
		t.Fatal("generate metadata fixture key")
	}
	public := key.PublicOnly()
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(public)
	if err != nil {
		t.Fatal("derive metadata fixture thumbprint")
	}
	now := time.Unix(1_786_507_482, 0).UTC()
	for _, testCase := range []struct {
		name       string
		key        internalrpcauth.ES256Key
		keyID      string
		thumbprint string
		generation uint64
		status     string
		notBefore  int64
		notAfter   int64
		now        time.Time
		want       bool
	}{
		{"current", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() - 1, now.Unix() + 1, now, true},
		{"next", public, public.KeyID, thumbprint, 1, "NEXT", now.Unix() - 1, now.Unix() + 1, now, true},
		{"previous", public, public.KeyID, thumbprint, 1, "PREVIOUS", now.Unix() - 1, now.Unix() + 1, now, true},
		{"activation boundary", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix(), now.Unix() + 1, now, true},
		{"same instant in another zone", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() - 1, now.Unix() + 1, now.In(time.FixedZone("fixture", 4*60*60)), true},
		{"unknown status", public, public.KeyID, thumbprint, 1, "ACTIVE", now.Unix() - 1, now.Unix() + 1, now, false},
		{"zero generation", public, public.KeyID, thumbprint, 0, "CURRENT", now.Unix() - 1, now.Unix() + 1, now, false},
		{"wrong key ID", public, "other", thumbprint, 1, "CURRENT", now.Unix() - 1, now.Unix() + 1, now, false},
		{"wrong fingerprint", public, public.KeyID, "other", 1, "CURRENT", now.Unix() - 1, now.Unix() + 1, now, false},
		{"not yet valid", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() + 1, now.Unix() + 2, now, false},
		{"expiry boundary", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() - 1, now.Unix(), now, false},
		{"expired", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() - 2, now.Unix() - 1, now, false},
		{"empty interval", public, public.KeyID, thumbprint, 1, "CURRENT", now.Unix(), now.Unix(), now, false},
		{"missing key", internalrpcauth.ES256Key{}, public.KeyID, thumbprint, 1, "CURRENT", now.Unix() - 1, now.Unix() + 1, now, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := validReadbackSignerMetadata(
				testCase.key, testCase.keyID, testCase.thumbprint, testCase.generation,
				testCase.status, testCase.notBefore, testCase.notAfter, testCase.now,
			)
			if got != testCase.want {
				t.Fatalf("readback signer metadata valid = %t, want %t", got, testCase.want)
			}
		})
	}
}
