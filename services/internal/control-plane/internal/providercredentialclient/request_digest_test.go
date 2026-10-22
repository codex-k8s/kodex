package providercredentialclient

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

// TestDeterministicRequestDigestPreservesWireAndInput compares a typed request against literal protobuf bytes, including an unknown-field suffix.
// Repeated hashes must leave both known and unknown input fields unchanged; nil retains protobuf's empty-message encoding.
// TestDeterministicRequestDigestPreservesWireAndInput сравнивает типизированный запрос с буквальными байтами protobuf, включая суффикс неизвестного поля.
// Повторные хеши должны сохранять известные и неизвестные входные поля; nil сохраняет пустое кодирование сообщения protobuf.
func TestDeterministicRequestDigestPreservesWireAndInput(t *testing.T) {
	request := &controlplanev1.RenewManagedProviderCredentialRequest{Binding: &controlplanev1.ProviderCredentialRenewalBinding{TaskRef: "pcrt_abcdefgh"}}
	for _, includeUnknown := range []bool{false, true} {
		message := proto.Clone(request)
		wire := []byte("\x0a\x0f\x0a\x0dpcrt_abcdefgh")
		if includeUnknown {
			unknown := []byte{0xa2, 0x06, 0x01, 0x07}
			message.ProtoReflect().SetUnknown(unknown)
			wire = append(wire, unknown...)
		}
		before := proto.Clone(message)
		want := sha256.Sum256(wire)
		for range 2 {
			got, err := deterministicRequestDigest(message)
			if err != nil || got != hex.EncodeToString(want[:]) || !proto.Equal(before, message) {
				t.Fatal("request digest changed exact wire bytes or input fields")
			}
		}
	}
	want := sha256.Sum256(nil)
	got, err := deterministicRequestDigest(nil)
	if err != nil || got != hex.EncodeToString(want[:]) {
		t.Fatal("nil protobuf request no longer hashes the empty encoding")
	}
}

// TestDeterministicRequestDigestRejectsMarshalFailure uses an invalid UTF-8 field to force protobuf serialization failure.
// No hash of partial bytes may be returned; this does not bypass the callers' stricter task-binding validation.
// TestDeterministicRequestDigestRejectsMarshalFailure задаёт неверное UTF-8 поле для ошибки сериализации protobuf.
// Хеш частичных байтов не должен возвращаться; более строгая проверка привязки задачи у потребителей этим не обходится.
func TestDeterministicRequestDigestRejectsMarshalFailure(t *testing.T) {
	request := &controlplanev1.RenewManagedProviderCredentialRequest{Binding: &controlplanev1.ProviderCredentialRenewalBinding{TaskRef: string([]byte{0xff})}}
	if digest, err := deterministicRequestDigest(request); err == nil || digest != "" {
		t.Fatal("invalid protobuf request yielded a usable digest")
	}
}
