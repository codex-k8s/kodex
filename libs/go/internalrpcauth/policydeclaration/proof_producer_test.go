package policydeclaration

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

// proofProducerZeroJSON pins the original field order, tags and null slices from
// the three pre-extraction declarations rather than deriving expected keys from
// the shared type under test.
// proofProducerZeroJSON закрепляет прежние порядок полей, теги и null-срезы трёх
// исходных деклараций, не выводя ожидаемые ключи из проверяемого общего типа.
const proofProducerZeroJSON = `{"producer_id":"","caller_workload_id":"","caller_spiffe_id":"","owner_workload_id":"","owner_spiffe_id":"","full_method":"","tls_server_name":"","transport_trust_bundle_id":"","application_credential":"","application_credential_metadata":"","application_credential_issuer":"","application_credential_audience":"","application_credential_trust_bundle_id":"","authority_proof_issuer":"","authority_proof_audience":"","authority_proof_trust_bundle_id":"","authority_proof_max_age_seconds":0,"deadline_milliseconds":0,"max_attempts":0,"retryable_grpc_codes":null,"idempotency_scope":"","authority_sources":null,"allowed_operation_ids":null,"server_resolved_fields":null}`

// TestProofProducerPreservesWireEncoding pins zero-value JSON bytes
// and roundtrips every populated field, preserving nil versus empty lists through
// both ordinary and canonical encoding without any live credential or policy.
// TestProofProducerPreservesWireEncoding закрепляет JSON-байты нулевого
// значения и проходит все заполненные поля в обе стороны, сохраняя nil и пустые
// списки при обычной и канонической сериализации без живых полномочий или политики.
func TestProofProducerPreservesWireEncoding(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(ProofProducer{})
	if err != nil || !bytes.Equal(raw, []byte(proofProducerZeroJSON)) {
		t.Fatal("proof producer zero-value wire layout changed")
	}
	for _, testCase := range []struct {
		name  string
		value ProofProducer
	}{
		{"nil lists", ProofProducer{}},
		{"empty lists", ProofProducer{
			RetryableGRPCCodes: []string{}, AuthoritySources: []string{},
			AllowedOperationIDs: []string{}, ServerResolvedFields: []string{},
		}},
		{"all fields", ProofProducer{
			ProducerID: "fixture.producer", CallerWorkloadID: "caller", CallerSPIFFEID: "spiffe://fixture/caller",
			OwnerWorkloadID: "owner", OwnerSPIFFEID: "spiffe://fixture/owner", FullMethod: "/fixture.Proof/Resolve",
			TLSServerName: "owner.example.test", TransportTrustBundleID: "transport-trust",
			ApplicationCredential: "FIXTURE_CREDENTIAL_KIND", ApplicationCredentialMetadata: "authorization",
			ApplicationCredentialIssuer: "https://identity.example.test", ApplicationCredentialAudience: "urn:fixture:application",
			ApplicationCredentialTrustBundleID: "application-trust", AuthorityProofIssuer: "spiffe://fixture/proof",
			AuthorityProofAudience: "urn:fixture:proof", AuthorityProofTrustBundleID: "proof-trust",
			AuthorityProofMaxAgeSeconds: 15, DeadlineMilliseconds: 2000, MaxAttempts: 2,
			RetryableGRPCCodes: []string{"UNAVAILABLE", "DEADLINE_EXCEEDED"}, IdempotencyScope: "fixture-scope",
			AuthoritySources: []string{"FIXTURE_STATE"}, AllowedOperationIDs: []string{"fixture.operation"},
			ServerResolvedFields: []string{"actor", "tenant"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			ordinary, err := json.Marshal(testCase.value)
			if err != nil {
				t.Fatal("encode proof producer fixture")
			}
			var decoded ProofProducer
			if err := internalrpcauth.DecodeStrictJSON(ordinary, &decoded); err != nil || !reflect.DeepEqual(decoded, testCase.value) {
				t.Fatal("ordinary proof producer roundtrip changed fields")
			}
			canonical, err := internalrpcauth.CanonicalJSON(testCase.value)
			if err != nil {
				t.Fatal("canonicalize proof producer fixture")
			}
			decoded = ProofProducer{}
			if err := internalrpcauth.DecodeCanonicalJSON(canonical, &decoded); err != nil || !reflect.DeepEqual(decoded, testCase.value) {
				t.Fatal("canonical proof producer roundtrip changed fields")
			}
		})
	}
}

// TestProofProducerUsesExistingStrictDecoder checks syntax/shape
// refusal through the existing decoder, then shows that semantic policy values
// remain unverified data for the consuming domain rather than new library rules.
// TestProofProducerUsesExistingStrictDecoder проверяет отказ существующего
// декодера при неверных синтаксисе и форме, затем показывает, что смысловые значения
// политики остаются непроверенными данными домена, а не новыми правилами библиотеки.
func TestProofProducerUsesExistingStrictDecoder(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name string
		raw  string
	}{
		{"unknown field", `{"unknown":true}`},
		{"duplicate field", `{"producer_id":"first","producer_id":"second"}`},
		{"wrong integer type", `{"max_attempts":"2"}`},
		{"fractional integer", `{"deadline_milliseconds":1.5}`},
		{"wrong list type", `{"allowed_operation_ids":"operation"}`},
		{"trailing object", `{} {}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var decoded ProofProducer
			if err := internalrpcauth.DecodeStrictJSON([]byte(testCase.raw), &decoded); err == nil {
				t.Fatal("malformed proof producer declaration accepted")
			}
		})
	}
	var unverified ProofProducer
	if err := internalrpcauth.DecodeStrictJSON([]byte(`{"owner_workload_id":"unverified","max_attempts":-1}`), &unverified); err != nil ||
		unverified.OwnerWorkloadID != "unverified" || unverified.MaxAttempts != -1 {
		t.Fatal("data declaration unexpectedly acquired semantic admission rules")
	}
}
