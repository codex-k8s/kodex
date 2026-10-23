package policydeclaration

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// TestBindingProfilesPreserveWireEncoding pins the original fields and integer
// widths through ordinary/canonical roundtrips of zero and populated records.
// Negative syntax/shape cases exercise the existing strict decoder, not semantic
// authority, transport identity or parent-lineage admission.
// TestBindingProfilesPreserveWireEncoding закрепляет прежние поля и ширину целых
// обычным и каноническим проходом нулевых и заполненных записей в обе стороны.
// Отрицательные случаи проверяют существующий строгий декодер, не смысловые
// полномочия, личность транспорта или допуск происхождения от родителя.
func TestBindingProfilesPreserveWireEncoding(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		value   any
		want    string
		invalid []string
	}{
		{
			name: "zero request", value: RequestProfile{},
			want:    `{"mode":"","resource":"","version":"","attempt":"","idempotency":""}`,
			invalid: []string{`{"mode":7}`, `{"mode":"first","mode":"second"}`, `{"extra":true}`},
		},
		{
			name: "request", value: RequestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"},
			want: `{"mode":"UNARY_PROTO_SHA256","resource":"REQUIRED","version":"FORBIDDEN","attempt":"FORBIDDEN","idempotency":"REQUIRED"}`,
		},
		{
			name: "zero continuation", value: ContinuationProfile{},
			want:    `{"parent_operation_id":"","parent_full_method":""}`,
			invalid: []string{`{"parent_operation_id":7}`, `{"parent_full_method":"first","parent_full_method":"second"}`, `{"extra":true}`},
		},
		{
			name: "continuation", value: ContinuationProfile{ParentOperationID: "fixture.parent", ParentFullMethod: "/fixture.Parent/Run"},
			want: `{"parent_operation_id":"fixture.parent","parent_full_method":"/fixture.Parent/Run"}`,
		},
		{
			name: "zero local peer", value: LocalPeer{},
			want:    `{"uid":0,"primary_gid":0,"shared_fs_gid":0}`,
			invalid: []string{`{"uid":-1}`, `{"uid":4294967296}`, `{"shared_fs_gid":"1"}`, `{"extra":true}`},
		},
		{
			name: "local peer", value: LocalPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000},
			want: `{"uid":10001,"primary_gid":10001,"shared_fs_gid":29000}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ordinary, err := json.Marshal(testCase.value)
			if err != nil || !bytes.Equal(ordinary, []byte(testCase.want)) {
				t.Fatal("binding profile JSON fields changed")
			}
			decoded := reflect.New(reflect.TypeOf(testCase.value)).Interface()
			if err := internalrpcauth.DecodeStrictJSON(ordinary, decoded); err != nil ||
				!reflect.DeepEqual(reflect.ValueOf(decoded).Elem().Interface(), testCase.value) {
				t.Fatal("binding profile ordinary roundtrip changed values")
			}
			canonical, err := internalrpcauth.CanonicalJSON(testCase.value)
			if err != nil {
				t.Fatal("canonicalize binding profile fixture")
			}
			decoded = reflect.New(reflect.TypeOf(testCase.value)).Interface()
			if err := internalrpcauth.DecodeCanonicalJSON(canonical, decoded); err != nil ||
				!reflect.DeepEqual(reflect.ValueOf(decoded).Elem().Interface(), testCase.value) {
				t.Fatal("binding profile canonical roundtrip changed values")
			}
			for _, raw := range testCase.invalid {
				decoded = reflect.New(reflect.TypeOf(testCase.value)).Interface()
				if err := internalrpcauth.DecodeStrictJSON([]byte(raw), decoded); err == nil {
					t.Fatal("malformed binding profile accepted")
				}
			}
		})
	}
}
