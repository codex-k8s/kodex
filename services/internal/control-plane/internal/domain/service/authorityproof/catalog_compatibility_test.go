package authorityproof

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

// catalogCompatibilityOwner records constructor/readiness/old-proof owner calls without external state.
// catalogCompatibilityOwner фиксирует owner calls конструктора, readiness и старого proof без внешнего состояния.
type catalogCompatibilityOwner struct {
	readyCalls, grantCalls, resolveCalls int
	revision                             uint64
}

// Ready represents the disposable owner's available dependency, without accepting a policy revision.
// Ready представляет доступную зависимость одноразового owner без назначения policy revision.
func (owner *catalogCompatibilityOwner) Ready(context.Context) error { owner.readyCalls++; return nil }

// AcceptWorkerGrant records each verified grant reaching the durable-owner port.
// AcceptWorkerGrant фиксирует каждый проверенный grant, достигший порта устойчивого owner.
func (owner *catalogCompatibilityOwner) AcceptWorkerGrant(context.Context, platformrepo.WorkerGrantInput) error {
	owner.grantCalls++
	return nil
}

// ResolveProofAuthority supplies synthetic domain lineage for one pre-existing service operation.
// ResolveProofAuthority выдаёт synthetic domain lineage для прежней service operation.
func (owner *catalogCompatibilityOwner) ResolveProofAuthority(context.Context, platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error) {
	owner.resolveCalls++
	return platformrepo.ProofAuthority{ActorID: "00000000-0000-4000-8000-000000000001", ActorVersion: 1,
		OrganizationID: "00000000-0000-4000-8000-000000000002", OrganizationVersion: 1}, nil
}

// NextAuthorityProofRevision models a monotonic owner allocation for this isolated fixture.
// NextAuthorityProofRevision моделирует монотонное назначение owner в изолированной fixture.
func (owner *catalogCompatibilityOwner) NextAuthorityProofRevision(context.Context) (uint64, error) {
	owner.revision++
	return owner.revision, nil
}

// TestCatalogBinaryStartsAndIssuesLegacyProofWithPolicy105 runs the real constructor and readiness
// using pinned policy105, synthetic keys and the existing worker-grant path. The legacy resolver
// excludes discovery until106; the separate service-v1 reader still requires API maintenance.
// TestCatalogBinaryStartsAndIssuesLegacyProofWithPolicy105 запускает настоящий constructor/readiness
// с pinned policy105, synthetic keys и прежним worker-grant path. Legacy resolver закрывает discovery
// до106; отдельный service-v1 reader по-прежнему требует API maintenance.
func TestCatalogBinaryStartsAndIssuesLegacyProofWithPolicy105(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	raw, err := os.ReadFile("../../../../../../../tools/release/fixtures/authority-policy-revision105.json")
	if err != nil {
		t.Fatal("read pinned policy105 fixture")
	}
	sourceHash := sha256.Sum256(raw)
	if hex.EncodeToString(sourceHash[:]) != "c63153741d7ed867cba59ac87fcfda3e2ca50e9c5d82bb1bc064ed62926332cf" {
		t.Fatal("policy105 fixture provenance changed")
	}
	const issuer = "https://identity.example.invalid/realms/synthetic"
	raw = []byte(strings.ReplaceAll(string(raw), "__KODEX_OIDC_ISSUER__", issuer))
	var document policyDocument
	if internalrpcauth.DecodeStrictJSON(raw, &document) != nil || document.PolicyRevision != 105 {
		t.Fatal("decode pinned policy105")
	}
	directory := t.TempDir()
	write := func(name string, value []byte) string {
		path := filepath.Join(directory, name)
		if os.WriteFile(path, value, 0o600) != nil {
			t.Fatal("write synthetic private constructor fixture")
		}
		return path
	}
	signer, err := internalrpcauth.GenerateES256Key("control-plane-authority-proof-g1")
	if err != nil {
		t.Fatal("generate disposable proof signer")
	}
	privateJWK, err := internalrpcauth.MarshalPrivateJWK(signer)
	if err != nil {
		t.Fatal("encode disposable proof signer")
	}
	publicJWK, err := internalrpcauth.MarshalPublicJWK(signer.PublicOnly())
	if err != nil {
		t.Fatal("encode disposable signer trust")
	}
	trust, err := internalrpcauth.CanonicalJSON(internalrpcauth.AuthorityProofTrustDocument{
		Version: 1, Purpose: internalrpcauth.AuthorityProofTrustPurpose, SourceRevision: 1, SourceDigest: strings.Repeat("a", 64),
		Predecessor: internalrpcauth.TrustRevisionDigest{DigestSHA256: strings.Repeat("0", 64)}, History: []internalrpcauth.TrustRevisionDigest{},
		Keys: []internalrpcauth.AuthorityProofTrustKey{{Issuer: controlPlaneSPIFFE, Generation: 1, Status: "CURRENT", Purpose: "AUTHORITY_PROOF",
			Audiences: []string{"urn:kodex:internal-rpc-authority-issuer:control-api-gateway", "urn:kodex:internal-rpc-authority-issuer:control-plane"}, NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(time.Hour).Unix(), JWK: publicJWK}},
	})
	if err != nil {
		t.Fatal("encode disposable trust document")
	}
	worker, err := internalrpcauth.GenerateES256Key("control-plane-platform-worker-g1")
	if err != nil {
		t.Fatal("generate disposable worker signer")
	}
	workerPublic, err := internalrpcauth.MarshalPublicJWK(worker.PublicOnly())
	if err != nil {
		t.Fatal("encode disposable worker trust")
	}
	const oldOperation = "platform.provider-credentials.readiness.check"
	var producer proofProducer
	for _, binding := range document.Policy.OperationBindings {
		if binding.OperationID == oldOperation {
			for _, candidate := range document.Policy.ProofProducers {
				if candidate.ProducerID == binding.ProducerID {
					producer = candidate
				}
			}
		}
	}
	grant, err := internalrpcauth.SignCanonicalJSON(workerGrantClaims{
		Version: 2, InstanceID: uuid.NewString(), Issuer: producer.ApplicationCredentialIssuer,
		Audience: producer.ApplicationCredentialAudience, Subject: "kodex-system-subject", CallerSPIFFEID: producer.CallerSPIFFEID,
		WorkloadID: "control-plane", OrganizationID: "kodex-installation", Revision: uint64(now.Unix()), CredentialGeneration: 1,
		AuthorityABIVersion: internalrpcauth.AuthorityABIVersion, JTI: uuid.NewString(), IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(workerGrantTTL).Unix(),
	}, worker, internalrpcauth.ProtectedHeaderExpectation{Type: workerGrantType, KeyID: worker.KeyID})
	if err != nil {
		t.Fatal("sign disposable worker grant")
	}
	// A generated local CA prevents any existing loopback service from supplying trusted JWKS.
	// Сгенерированный локальный CA не позволяет существующему loopback service выдать доверенный JWKS.
	caServer := httptest.NewTLSServer(http.NotFoundHandler())
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caServer.Certificate().Raw})
	caServer.Close()
	owner := &catalogCompatibilityOwner{}
	service, err := New(t.Context(), owner, Config{
		PolicyFile: write("policy105.json", raw), SignerPrivateJWKFile: write("signer.json", privateJWK), SignerTrustFile: write("trust.json", trust),
		WorkerGrantTrustFiles: map[string]string{"control-plane": write("worker.json", workerPublic)}, ReadinessWorkerGrantFile: write("grant.jws", []byte(grant)),
		OIDC: oidcverifier.Config{Issuer: issuer, Audience: "kodex-control-api", JWKSURL: "https://identity.example.invalid/jwks",
			ConnectAddress: "localhost:443", TLSServerName: "identity.example.invalid", CAFile: write("ca.pem", ca), Timeout: time.Second},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal("current catalog constructor rejected policy105:", err)
	}
	defer service.Close()
	ready, err := service.Ready(t.Context())
	if err != nil || ready.PolicyRevision != 105 || owner.readyCalls != 1 || owner.grantCalls != 1 {
		t.Fatal("current catalog resolver was not legitimately ready on policy105")
	}
	input := ResolveInput{PeerSPIFFEID: producer.CallerSPIFFEID, Authorization: "Bearer " + grant, OperationID: oldOperation,
		IdempotencyKey: uuid.NewString(), CorrelationID: uuid.NewString(), RequestDigestSHA256: strings.Repeat("b", 64)}
	proof, err := service.Resolve(t.Context(), input)
	if err != nil || proof.PolicyRevision != 105 || owner.resolveCalls != 1 || owner.revision != 1 || proof.CompactJWS == "" {
		t.Fatal("current catalog resolver failed pre-existing policy105 operation")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(proof.CompactJWS, signer.PublicOnly(), internalrpcauth.ProtectedHeaderExpectation{Type: internalrpcauth.AuthorityProofProtectedType, KeyID: signer.KeyID})
	var claims proofClaims
	if err != nil || json.Unmarshal(verified.CanonicalPayload, &claims) != nil || claims.OperationID != oldOperation {
		t.Fatal("pre-existing operation proof was not independently verifiable")
	}
	input.OperationID = "platform.query.model-discovery.list"
	if _, err := service.Resolve(t.Context(), input); err == nil || owner.resolveCalls != 1 || owner.revision != 1 {
		t.Fatal("discovery escaped policy105 legacy resolver before forward publication")
	}
}
