package authorityproof

import (
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/google/uuid"
)

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
		Revision: 7, JTI: uuid.NewString(), IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(workerGrantTTL).Unix(),
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
}

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

func testPolicy(producer proofProducer) policyDocument {
	document := policyDocument{Version: 1, PolicyRevision: 1}
	document.Policy.TrustDomain = "kodex.local"
	document.Policy.DefaultDecision = "DENY"
	document.Policy.ProofProducers = []proofProducer{producer}
	document.Policy.OperationBindings = []operationBinding{{
		OperationID: "platform.test", CallerWorkloadID: producer.CallerWorkloadID,
		CallerSPIFFEID: producer.CallerSPIFFEID, Audience: "urn:kodex:internal-rpc:control-plane",
		FullMethod: "/controlplane.v1.PlatformQueryService/GetBootstrapState",
		Permission: "platform.test", ProducerID: producer.ProducerID,
	}}
	return document
}
