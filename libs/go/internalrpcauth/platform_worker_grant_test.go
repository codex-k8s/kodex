package internalrpcauth

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestPlatformWorkerGrantClaimsPreserveWire pins the former zero-field order,
// optional instance encoding and integer widths. Canonical fixtures use the
// existing JCS numeric range; maximum uint64 values use ordinary JSON only.
// Both roundtrips preserve values; no semantic credential is accepted by
// this data-shape test and no provider or external service is contacted.
// TestPlatformWorkerGrantClaimsPreserveWire закрепляет прежний порядок нулевых
// полей, необязательный экземпляр и ширину целых. Канонические примеры находятся
// в прежнем числовом диапазоне JCS; максимум uint64 проверяется лишь обычным JSON.
// Оба прохода сохраняют значения; тест формы данных не принимает полномочия
// credential и не обращается к провайдеру или внешнему сервису.
func TestPlatformWorkerGrantClaimsPreserveWire(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		value PlatformWorkerGrantClaims
		want  string
	}{
		{
			name: "zero fields retain required keys",
			want: `{"v":0,"iss":"","aud":"","sub":"","caller_spiffe_id":"","workload_id":"","organization_id":"","project_id":"","tenant_owner":false,"revision":0,"credential_generation":0,"authority_abi_version":0,"jti":"","iat":0,"nbf":0,"exp":0}`,
		},
		{
			name: "populated instance",
			value: PlatformWorkerGrantClaims{
				Version: 2, InstanceID: "00000000-0000-4000-8000-000000000001",
				Issuer: "fixture:issuer", Audience: "fixture:audience", Subject: "subject",
				CallerSPIFFEID: "spiffe://fixture/worker", WorkloadID: "worker",
				OrganizationID: "organization", ProjectID: "project", TenantOwner: true,
				Revision: 9007199254740991, CredentialGeneration: 42, AuthorityABIVersion: ^uint32(0),
				JTI: "identifier", IssuedAt: -1, NotBefore: 2, ExpiresAt: 3,
			},
			want: `{"v":2,"instance_id":"00000000-0000-4000-8000-000000000001","iss":"fixture:issuer","aud":"fixture:audience","sub":"subject","caller_spiffe_id":"spiffe://fixture/worker","workload_id":"worker","organization_id":"organization","project_id":"project","tenant_owner":true,"revision":9007199254740991,"credential_generation":42,"authority_abi_version":4294967295,"jti":"identifier","iat":-1,"nbf":2,"exp":3}`,
		},
		{
			name: "empty instance remains omitted",
			value: PlatformWorkerGrantClaims{
				Version: 1, Issuer: "fixture:issuer", Audience: "fixture:audience",
				Subject: "subject", CallerSPIFFEID: "spiffe://fixture/worker", WorkloadID: "worker",
				OrganizationID: "organization", Revision: 4, CredentialGeneration: 5,
				AuthorityABIVersion: 6, JTI: "identifier", IssuedAt: 7, NotBefore: 8, ExpiresAt: 9,
			},
			want: `{"v":1,"iss":"fixture:issuer","aud":"fixture:audience","sub":"subject","caller_spiffe_id":"spiffe://fixture/worker","workload_id":"worker","organization_id":"organization","project_id":"","tenant_owner":false,"revision":4,"credential_generation":5,"authority_abi_version":6,"jti":"identifier","iat":7,"nbf":8,"exp":9}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			raw, err := json.Marshal(testCase.value)
			if err != nil || !bytes.Equal(raw, []byte(testCase.want)) {
				t.Fatal("platform worker grant JSON layout changed")
			}
			var decoded PlatformWorkerGrantClaims
			if err := DecodeStrictJSON(raw, &decoded); err != nil || decoded != testCase.value {
				t.Fatal("platform worker grant ordinary roundtrip changed values")
			}
			canonical, err := CanonicalJSON(testCase.value)
			if err != nil {
				t.Fatal("canonicalize platform worker grant fixture")
			}
			decoded = PlatformWorkerGrantClaims{}
			if err := DecodeCanonicalJSON(canonical, &decoded); err != nil || decoded != testCase.value {
				t.Fatal("platform worker grant canonical roundtrip changed values")
			}
		})
	}
	maximum := PlatformWorkerGrantClaims{Revision: ^uint64(0), CredentialGeneration: ^uint64(0)}
	rawMaximum, err := json.Marshal(maximum)
	var decodedMaximum PlatformWorkerGrantClaims
	if err != nil || DecodeStrictJSON(rawMaximum, &decodedMaximum) != nil || decodedMaximum != maximum {
		t.Fatal("platform worker grant ordinary JSON integer width changed")
	}
	for _, raw := range []string{
		`{"extra":true}`,
		`{"v":1,"v":2}`,
		`{"instance_id":1}`,
		`{"tenant_owner":"true"}`,
		`{"revision":-1}`,
		`{"revision":18446744073709551616}`,
		`{"credential_generation":-1}`,
		`{"authority_abi_version":-1}`,
		`{"authority_abi_version":4294967296}`,
	} {
		var value PlatformWorkerGrantClaims
		if err := DecodeStrictJSON([]byte(raw), &value); err == nil {
			t.Fatal("malformed platform worker grant shape accepted")
		}
	}
}
