package authorityproof

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

// TestVerifySignerTrustAcceptsCompletePublisherDocument writes a complete canonical synthetic trust
// document and requires its matching current public key/generation to be selected; it does not prove
// root delivery or mount security.
// TestVerifySignerTrustAcceptsCompletePublisherDocument записывает полный канонический синтетический
// документ доверия и требует выбора совпадающих текущего открытого ключа и поколения; доставка корня и
// защита монтирования не доказываются.
func TestVerifySignerTrustAcceptsCompletePublisherDocument(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	key, err := internalrpcauth.GenerateES256Key("control-plane-authority-proof-g7")
	if err != nil {
		t.Fatalf("создать тестовый ключ: %v", err)
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("закодировать открытый JWK: %v", err)
	}
	document := internalrpcauth.AuthorityProofTrustDocument{
		Version:        internalrpcauth.ContractVersion,
		Purpose:        internalrpcauth.AuthorityProofTrustPurpose,
		SourceRevision: 1, SourceDigest: strings.Repeat("a", 64),
		Predecessor: internalrpcauth.TrustRevisionDigest{
			DigestSHA256: strings.Repeat("0", 64),
		},
		History: []internalrpcauth.TrustRevisionDigest{},
		Keys: []internalrpcauth.AuthorityProofTrustKey{{
			Issuer: controlPlaneSPIFFE, Generation: 7, Status: "CURRENT",
			Purpose:   "AUTHORITY_PROOF",
			Audiences: []string{"urn:kodex:internal-rpc-authority-issuer:control-api-gateway"},
			NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(time.Hour).Unix(),
			JWK: json.RawMessage(publicJWK),
		}},
	}
	raw, err := internalrpcauth.CanonicalJSON(document)
	if err != nil {
		t.Fatalf("создать canonical trust document: %v", err)
	}
	path := t.TempDir() + "/authority-proof-trust.json"
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("записать trust document: %v", err)
	}
	generation, err := verifySignerTrust(path, key, now)
	if err != nil {
		t.Fatalf("полный publisher trust document отклонён: %v", err)
	}
	if generation != 7 {
		t.Fatalf("получено поколение %d, ожидалось 7", generation)
	}
}

// TestVerifySignerTrustRejectsLegacyJWKS rejects a bare public JWKS without the required trust-document
// envelope, even when it contains a matching public key.
// TestVerifySignerTrustRejectsLegacyJWKS отклоняет голый открытый JWKS без обязательного конверта
// документа доверия даже с совпадающим открытым ключом.
func TestVerifySignerTrustRejectsLegacyJWKS(t *testing.T) {
	t.Parallel()

	key, err := internalrpcauth.GenerateES256Key("legacy-control-plane-proof")
	if err != nil {
		t.Fatalf("создать тестовый ключ: %v", err)
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("закодировать открытый JWK: %v", err)
	}
	raw, err := internalrpcauth.CanonicalJSON(struct {
		Keys []json.RawMessage `json:"keys"`
	}{Keys: []json.RawMessage{publicJWK}})
	if err != nil {
		t.Fatalf("создать legacy JWKS: %v", err)
	}
	path := t.TempDir() + "/legacy-jwks.json"
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("записать legacy JWKS: %v", err)
	}
	if _, err := verifySignerTrust(path, key, time.Now().UTC()); err == nil {
		t.Fatal("legacy JWKS без provenance был принят")
	}
}

// TestIndexPolicyBindsOIDCProducerToInstallationConfiguration accepts exact configured OIDC
// issuer/audience then rejects another installation's issuer using synthetic policy data only.
// TestIndexPolicyBindsOIDCProducerToInstallationConfiguration принимает точные настроенные issuer и
// audience OIDC, затем отклоняет issuer другой установки только на синтетической политике.
func TestIndexPolicyBindsOIDCProducerToInstallationConfiguration(t *testing.T) {
	t.Parallel()

	producer := testProducer("control-api-gateway", "OIDC_BEARER")
	producer.ApplicationCredentialIssuer = "https://identity.example.test/realms/acme"
	producer.ApplicationCredentialAudience = "kodex-control-api"
	service := &Service{
		oidcIssuer:   producer.ApplicationCredentialIssuer,
		oidcAudience: producer.ApplicationCredentialAudience,
		policy:       testPolicy(producer),
	}
	if err := service.indexPolicy(); err != nil {
		t.Fatalf("корректная OIDC policy отклонена: %v", err)
	}

	service.oidcIssuer = "https://identity.example.test/realms/another-installation"
	if err := service.indexPolicy(); err == nil {
		t.Fatal("OIDC issuer другой установки был принят")
	}
}

// TestIndexPolicyAcceptsPermissionDistinctFromOperation accepts a nonempty RBAC permission distinct from
// operation ID and rejects an empty one; it does not grant that permission to an actor.
// TestIndexPolicyAcceptsPermissionDistinctFromOperation принимает непустое право RBAC, отличное от ID
// операции, и отклоняет пустое, не выдавая это право исполнителю.
func TestIndexPolicyAcceptsPermissionDistinctFromOperation(t *testing.T) {
	t.Parallel()

	producer := testProducer("runtime-controller", "PLATFORM_WORKER_GRANT")
	document := testPolicy(producer)
	document.Policy.OperationBindings[0].Permission = "runtime.session.start"
	service := &Service{policy: document}
	if err := service.indexPolicy(); err != nil {
		t.Fatalf("отдельное RBAC-разрешение отклонено: %v", err)
	}

	document.Policy.OperationBindings[0].Permission = ""
	service.policy = document
	if err := service.indexPolicy(); err == nil {
		t.Fatal("пустое RBAC-разрешение было принято")
	}
}

// TestWorkerGrantRequiresExactWorkloadBinding signs local synthetic grants and rejects mismatched/absent
// credential generation, wrong audience and missing ABI while preserving the valid baseline.
// TestWorkerGrantRequiresExactWorkloadBinding подписывает локальные синтетические grant и отклоняет
// несовпадающее или отсутствующее поколение, чужую аудиторию и отсутствие ABI, сохраняя допустимый
// исходный вариант.
func TestWorkerGrantRequiresExactWorkloadBinding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	key, err := internalrpcauth.GenerateES256Key("runtime-controller-platform-worker-g1")
	if err != nil {
		t.Fatalf("создать тестовый ключ: %v", err)
	}
	producer := testProducer("runtime-controller", "PLATFORM_WORKER_GRANT")
	service := &Service{
		workerKeys: map[string]internalrpcauth.ES256Key{"runtime-controller": key.PublicOnly()},
		now:        func() time.Time { return now },
	}
	claims := workerGrantClaims{
		Version: 1, Issuer: producer.ApplicationCredentialIssuer,
		Audience: producer.ApplicationCredentialAudience,
		Subject:  "kodex-system-subject", CallerSPIFFEID: producer.CallerSPIFFEID,
		WorkloadID: producer.CallerWorkloadID, OrganizationID: "kodex-installation",
		Revision: 7, CredentialGeneration: 1, JTI: uuid.NewString(), IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(workerGrantTTL).Unix(), AuthorityABIVersion: internalrpcauth.AuthorityABIVersion,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{
		Type: workerGrantType, KeyID: key.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать тестовый grant: %v", err)
	}
	if _, err := service.verifyWorkerGrant(compact, producer); err != nil {
		t.Fatalf("корректный worker grant отклонён: %v", err)
	}

	claims.CredentialGeneration = 2
	compact, err = internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{
		Type: workerGrantType, KeyID: key.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать grant с неверным поколением: %v", err)
	}
	if _, err := service.verifyWorkerGrant(compact, producer); err == nil {
		t.Fatal("worker grant с поколением другого ключа был принят")
	}
	claims.CredentialGeneration = 0
	compact, err = internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{
		Type: workerGrantType, KeyID: key.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать grant без поколения credential: %v", err)
	}
	if _, err := service.verifyWorkerGrant(compact, producer); err == nil {
		t.Fatal("worker grant без поколения credential был принят")
	}
	claims.CredentialGeneration = 1

	claims.Audience = "urn:kodex:platform-worker:another-workload"
	compact, err = internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{
		Type: workerGrantType, KeyID: key.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать grant с неверной аудиторией: %v", err)
	}
	if _, err := service.verifyWorkerGrant(compact, producer); err == nil {
		t.Fatal("worker grant с неверной аудиторией был принят")
	}

	claims.Audience = producer.ApplicationCredentialAudience
	claims.AuthorityABIVersion = 0
	compact, err = internalrpcauth.SignCanonicalJSON(claims, key, internalrpcauth.ProtectedHeaderExpectation{
		Type: workerGrantType, KeyID: key.KeyID,
	})
	if err != nil {
		t.Fatalf("подписать grant без ABI: %v", err)
	}
	if _, err := service.verifyWorkerGrant(compact, producer); err == nil {
		t.Fatal("worker grant без текущей ABI был принят")
	}
}

// testProducer constructs a synthetic workload producer with fixed owner/proof identity,
// workload-specific credential issuer/audience and one allowed operation; it performs no registration or
// credential issuance.
// testProducer создаёт синтетического производителя нагрузки с фиксированными владельцем и подписантом,
// аудиторией и issuer нагрузки и одной допустимой операцией без регистрации и выпуска учётных данных.
func testProducer(workloadID, credentialType string) proofProducer {
	return proofProducer{
		ProducerID: "control-plane." + workloadID, CallerWorkloadID: workloadID,
		CallerSPIFFEID:  "spiffe://kodex.local/ns/kodex-system/sa/" + workloadID,
		OwnerWorkloadID: "control-plane", OwnerSPIFFEID: controlPlaneSPIFFE,
		ApplicationCredential: credentialType, ApplicationCredentialMetadata: "authorization",
		ApplicationCredentialIssuer:   "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/" + workloadID,
		ApplicationCredentialAudience: "urn:kodex:platform-worker:" + workloadID,
		AuthorityProofIssuer:          controlPlaneSPIFFE,
		AuthorityProofAudience:        "urn:kodex:internal-rpc-authority-issuer:" + workloadID,
		AuthorityProofMaxAgeSeconds:   15,
		AllowedOperationIDs:           []string{"platform.test"},
	}
}

// testPolicy wraps the supplied producer in a minimal current-ABI DENY policy with one exact unary
// binding and forbidden optional pins; it is test data, not an installed policy.
// testPolicy помещает производителя в минимальную политику DENY текущего ABI с одной точной
// unary-привязкой и запрещёнными необязательными измерениями; это тестовые данные, не установленная
// политика.
func testPolicy(producer proofProducer) policyDocument {
	document := policyDocument{Version: 1, PolicyRevision: 1}
	document.Policy.AuthorityABIVersion = internalrpcauth.AuthorityABIVersion
	document.Policy.TrustDomain = "kodex.local"
	document.Policy.DefaultDecision = "DENY"
	document.Policy.ProofProducers = []proofProducer{producer}
	document.Policy.OperationBindings = []operationBinding{{
		OperationID: "platform.test", CallerWorkloadID: producer.CallerWorkloadID,
		CallerSPIFFEID: producer.CallerSPIFFEID, Audience: "urn:kodex:internal-rpc:control-plane",
		FullMethod: "/controlplane.v1.PlatformQueryService/GetBootstrapState",
		Permission: "platform.test", ProducerID: producer.ProducerID,
		RequestProfile: requestProfile{
			Mode:     internalrpcauth.RequestBindingUnary,
			Resource: internalrpcauth.ProfileBindingForbidden, Version: internalrpcauth.ProfileBindingForbidden,
			Attempt: internalrpcauth.ProfileBindingForbidden, Idempotency: internalrpcauth.ProfileBindingForbidden,
		},
	}}
	return document
}
