package authorityclaims_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclaims"
)

// wireProvenance preserves a local nominal provenance type for compatibility fixtures.
// wireProvenance сохраняет локальный номинальный тип provenance для оснастки совместимости.
type wireProvenance authorityclaims.Provenance

// wireIdentity retains the local provenance field type while reusing the identity wire layout.
// wireIdentity сохраняет локальный тип поля provenance при использовании общей wire формы identity.
type wireIdentity authorityclaims.Identity[wireProvenance]

// wireWorkload keeps the fixture workload distinct from the shared technical JSON shape.
// wireWorkload сохраняет отличие workload оснастки от общей технической JSON формы.
type wireWorkload authorityclaims.Workload

// wireAuthority retains local actor, tenant and project types for proof encoding compatibility checks.
// wireAuthority сохраняет локальные типы actor, tenant и project для проверки совместимости proof encoding.
type wireAuthority authorityclaims.Authority[wireIdentity]

// wireCredentialAuthentication keeps local authentication metadata distinct without changing its wire tags.
// wireCredentialAuthentication сохраняет отличие локальных метаданных аутентификации без изменения их wire tags.
type wireCredentialAuthentication authorityclaims.CredentialAuthentication

// wireProof uses the same nominal instantiation pattern as production proof producer and consumer.
// wireProof использует тот же способ номинальной инстанциации, что production producer и consumer proof.
type wireProof authorityclaims.Proof[wireWorkload, wireAuthority, wireCredentialAuthentication]

// otherWireProof models a second nominal proof domain even when its instantiated underlying form matches.
// otherWireProof моделирует второй номинальный proof домен даже при совпадающей инстанциированной underlying форме.
type otherWireProof authorityclaims.Proof[wireWorkload, wireAuthority, wireCredentialAuthentication]

// proofWireFixture supplies deterministic noncredential values, wide integer pins and every
// optional branch; the fixture tests serialization shape and does not assert semantic validity.
// proofWireFixture предоставляет детерминированные значения без credentials, широкие целые pins и
// все optional ветви; оснастка проверяет сериализацию, не утверждая семантическую допустимость.
func proofWireFixture() wireProof {
	actor := wireIdentity{ID: "actor-fixture", Provenance: wireProvenance{Source: "OIDC_SESSION", Reference: "session-fixture", Revision: 7, DigestSHA256: "actor-digest"}}
	tenant := wireIdentity{ID: "tenant-fixture", Provenance: wireProvenance{Source: "DOMAIN_STATE", Reference: "tenant-fixture", Revision: 8, DigestSHA256: "tenant-digest"}}
	project := wireIdentity{ID: "project-fixture", Provenance: wireProvenance{Source: "DOMAIN_STATE", Reference: "project-fixture", Revision: 9, DigestSHA256: "project-digest"}}
	return wireProof{Version: 1, Issuer: "issuer-fixture", Audience: "proof-audience-fixture", Caller: wireWorkload{WorkloadID: "caller-fixture", SPIFFEID: "spiffe-fixture"},
		OperationID: "fixture.read", AuthorizationContextAudience: "context-audience-fixture", Authority: wireAuthority{ActorKind: "HUMAN", Actor: actor, Tenant: tenant, Project: &project},
		ProofRevision: 9007199254740991, SignerGeneration: 11, CallerCredentialRevision: 12,
		CredentialAuthentication: &wireCredentialAuthentication{AuthenticatedAt: 100, ACR: "interactive-fixture", AMR: []string{"pwd", "otp"}}, JTI: "jti-fixture",
		IssuedAt: 101, NotBefore: 102, ExpiresAt: 253402300799, RequestDigestSHA256: "request-digest-fixture", RequestBindingMode: "UNARY_PROTO_SHA256", AuthorityABIVersion: 2}
}

// TestProofJSONMatchesOriginalWire checks exact bytes and omissions against frozen fixtures from
// the pre-extraction model, including required zero fields, empty AMR and wide integer values.
// TestProofJSONMatchesOriginalWire проверяет точные байты и пропуски по закреплённой исходной модели
// до выноса, включая обязательные нулевые поля, пустой AMR и широкие целые значения.
func TestProofJSONMatchesOriginalWire(t *testing.T) {
	t.Parallel()
	full := proofWireFixture()
	stream := full
	stream.Authority.Project = nil
	stream.CredentialAuthentication = &wireCredentialAuthentication{AMR: []string{}}
	stream.RequestDigestSHA256 = ""
	stream.RequestBindingMode = "STREAM_SESSION"

	cases := []struct {
		name  string
		value wireProof
		want  string
	}{
		{"full", full, `{"v":1,"iss":"issuer-fixture","aud":"proof-audience-fixture","caller":{"workload_id":"caller-fixture","spiffe_id":"spiffe-fixture"},"operation_id":"fixture.read","authorization_context_audience":"context-audience-fixture","authority":{"actor_kind":"HUMAN","actor":{"id":"actor-fixture","provenance":{"source":"OIDC_SESSION","reference":"session-fixture","revision":7,"digest_sha256":"actor-digest"}},"tenant":{"id":"tenant-fixture","provenance":{"source":"DOMAIN_STATE","reference":"tenant-fixture","revision":8,"digest_sha256":"tenant-digest"}},"project":{"id":"project-fixture","provenance":{"source":"DOMAIN_STATE","reference":"project-fixture","revision":9,"digest_sha256":"project-digest"}}},"proof_revision":9007199254740991,"signer_generation":11,"caller_credential_revision":12,"credential_authentication":{"auth_time":100,"acr":"interactive-fixture","amr":["pwd","otp"]},"jti":"jti-fixture","iat":101,"nbf":102,"exp":253402300799,"request_digest_sha256":"request-digest-fixture","request_binding_mode":"UNARY_PROTO_SHA256","authority_abi_version":2}`},
		{"stream-empty-optionals", stream, `{"v":1,"iss":"issuer-fixture","aud":"proof-audience-fixture","caller":{"workload_id":"caller-fixture","spiffe_id":"spiffe-fixture"},"operation_id":"fixture.read","authorization_context_audience":"context-audience-fixture","authority":{"actor_kind":"HUMAN","actor":{"id":"actor-fixture","provenance":{"source":"OIDC_SESSION","reference":"session-fixture","revision":7,"digest_sha256":"actor-digest"}},"tenant":{"id":"tenant-fixture","provenance":{"source":"DOMAIN_STATE","reference":"tenant-fixture","revision":8,"digest_sha256":"tenant-digest"}}},"proof_revision":9007199254740991,"signer_generation":11,"caller_credential_revision":12,"credential_authentication":{"auth_time":0},"jti":"jti-fixture","iat":101,"nbf":102,"exp":253402300799,"request_binding_mode":"STREAM_SESSION","authority_abi_version":2}`},
		{"zero-required-fields", wireProof{}, `{"v":0,"iss":"","aud":"","caller":{"workload_id":"","spiffe_id":""},"operation_id":"","authorization_context_audience":"","authority":{"actor_kind":"","actor":{"id":"","provenance":{"source":"","reference":"","revision":0,"digest_sha256":""}},"tenant":{"id":"","provenance":{"source":"","reference":"","revision":0,"digest_sha256":""}}},"proof_revision":0,"signer_generation":0,"caller_credential_revision":0,"jti":"","iat":0,"nbf":0,"exp":0,"request_binding_mode":"","authority_abi_version":0}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.value)
			if err != nil || string(raw) != test.want {
				t.Fatal("authority proof JSON layout changed")
			}
			var decoded wireProof
			if json.Unmarshal(raw, &decoded) != nil {
				t.Fatal("authority proof JSON cannot be decoded")
			}
			// EN: Empty AMR is intentionally omitted and decodes as nil, so compare the encoded form.
			// RU: Пустой AMR намеренно пропускается и декодируется как nil, поэтому сравнивается encoded форма.
			again, err := json.Marshal(decoded)
			if err != nil || string(again) != test.want {
				t.Fatal("authority proof JSON round-trip changed")
			}
		})
	}
}

// TestProofFormsPreserveNominalFieldTypes checks the shared layout retains each local nested
// domain type and does not turn two named proof domains into assignable aliases.
// TestProofFormsPreserveNominalFieldTypes проверяет сохранение локальных вложенных доменных типов
// общей формой без превращения двух именованных proof доменов в присваиваемые aliases.
func TestProofFormsPreserveNominalFieldTypes(t *testing.T) {
	t.Parallel()
	first, second := reflect.TypeFor[wireProof](), reflect.TypeFor[otherWireProof]()
	if first == second || first.AssignableTo(second) || second.AssignableTo(first) {
		t.Fatal("nominal proof domains collapsed")
	}
	for _, test := range []struct {
		record reflect.Type
		name   string
		want   reflect.Type
	}{
		{reflect.TypeFor[wireIdentity](), "Provenance", reflect.TypeFor[wireProvenance]()},
		{reflect.TypeFor[wireAuthority](), "Actor", reflect.TypeFor[wireIdentity]()},
		{reflect.TypeFor[wireAuthority](), "Project", reflect.TypeFor[*wireIdentity]()},
		{first, "Caller", reflect.TypeFor[wireWorkload]()},
		{first, "Authority", reflect.TypeFor[wireAuthority]()},
		{first, "CredentialAuthentication", reflect.TypeFor[*wireCredentialAuthentication]()},
	} {
		field, ok := test.record.FieldByName(test.name)
		if !ok || field.Type != test.want {
			t.Fatal("local nominal field type changed")
		}
	}
}
