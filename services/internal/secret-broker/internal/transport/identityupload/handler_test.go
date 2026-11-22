package identityupload

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/service/kubernetesidentity"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// uploadFixture supplies owner admission and purpose receipts without a live cluster or private input.
// It records actual HTTP workflow effects; SQL authority and private-store TLS are qualified separately.
// uploadFixture даёт owner admission и purpose receipts без live cluster либо private input.
// Считает actual HTTP workflow effects; SQL authority и private-store TLS проверяются отдельно.
type uploadFixture struct {
	snapshot value.KubernetesIdentityUploadSnapshot
	identity value.KubernetesIdentityLocator
	view     value.KubernetesIdentitySetupView
	creates  atomic.Int32
	claims   atomic.Int32
}

// ResolveKubernetesIdentityUpload returns the exact immutable fixture only after permit hashing.
// ResolveKubernetesIdentityUpload возвращает exact immutable fixture лишь после permit hashing.
func (fixture *uploadFixture) ResolveKubernetesIdentityUpload(_ context.Context, ref, digest string) (value.KubernetesIdentityUploadSnapshot, error) {
	hash := sha256.Sum256(make([]byte, 32))
	if ref != fixture.snapshot.SetupRef || digest != hex.EncodeToString(hash[:]) {
		return value.KubernetesIdentityUploadSnapshot{}, targetrepo.ErrSetupRejected
	}
	return fixture.snapshot, nil
}

// ClaimKubernetesIdentityUpload permits one fixture write so an HTTP replay cannot duplicate material.
// ClaimKubernetesIdentityUpload допускает одну fixture write, исключая material duplicate при HTTP replay.
func (fixture *uploadFixture) ClaimKubernetesIdentityUpload(_ context.Context, input targetrepo.IdentityUploadClaim) (value.KubernetesIdentityUploadAdmission, error) {
	if fixture.claims.Add(1) != 1 {
		return value.KubernetesIdentityUploadAdmission{}, targetrepo.ErrSetupRejected
	}
	return value.KubernetesIdentityUploadAdmission{SetupRef: input.SetupRef, AttemptRef: "ksa_fixture01", Fence: "fence_fixture01", Generation: 1,
		ExpiresAt: time.Now().Add(20 * time.Second), Provision: fixture.snapshot.Provision}, nil
}

// CompleteKubernetesIdentityUpload accepts only the exact created fixture UID and safe owner view.
// CompleteKubernetesIdentityUpload принимает лишь exact created fixture UID и safe owner view.
func (fixture *uploadFixture) CompleteKubernetesIdentityUpload(_ context.Context, input targetrepo.IdentityUploadCompletion) (value.KubernetesIdentitySetupView, error) {
	if input.Identity == nil || *input.Identity != fixture.identity || input.Unknown || input.Rejected != nil {
		return value.KubernetesIdentitySetupView{}, targetrepo.ErrSetupRejected
	}
	return fixture.view, nil
}

// VerifyKubernetesIdentity checks the fixture content without network or elevated credentials.
// VerifyKubernetesIdentity проверяет fixture content без сети либо elevated credentials.
func (fixture *uploadFixture) VerifyKubernetesIdentity(_ context.Context, raw []byte, target value.KubernetesTarget) error {
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != fixture.snapshot.Provision.ContentSHA256 || target.ID != fixture.snapshot.Target.ID {
		return targetrepo.ErrSetupRejected
	}
	return nil
}

// CreateKubernetesIdentity records the single downstream effect after HTTP and owner admission.
// CreateKubernetesIdentity считает единственный downstream effect после HTTP и owner admission.
func (fixture *uploadFixture) CreateKubernetesIdentity(context.Context, value.KubernetesIdentityProvision, []byte) (value.KubernetesIdentityLocator, error) {
	fixture.creates.Add(1)
	return fixture.identity, nil
}

// ObserveKubernetesIdentity denies fixture observation because no explicit UNKNOWN was reported.
// ObserveKubernetesIdentity отклоняет fixture observation без явно reported UNKNOWN.
func (*uploadFixture) ObserveKubernetesIdentity(context.Context, value.KubernetesIdentityProvision) (value.KubernetesIdentityLocator, error) {
	return value.KubernetesIdentityLocator{}, targetrepo.ErrSetupRejected
}

// ReadKubernetesIdentity denies all reads on the first-write fixture.
// ReadKubernetesIdentity отклоняет все reads в first-write fixture.
func (*uploadFixture) ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error) {
	return nil, targetrepo.ErrSetupRejected
}

// newUploadFixture builds a full local HTTP workflow while keeping cluster effects disposable.
// newUploadFixture собирает полный local HTTP workflow с одноразовыми cluster-effect fixtures.
func newUploadFixture(t *testing.T) (*uploadFixture, *kubernetesidentity.Service, []byte) {
	t.Helper()
	material := []byte("synthetic-private-upload-not-for-output")
	hash := sha256.Sum256(material)
	provision := value.KubernetesIdentityProvision{IdentityRef: "kbi_fixture01", TargetRef: "target_fixture01", Namespace: "broker-private", Revision: 1, ContentSHA256: hex.EncodeToString(hash[:])}
	name, err := value.KubernetesIdentitySecretName(provision.IdentityRef, provision.Revision)
	if err != nil {
		t.Fatal("derive fixture purpose name")
	}
	expires := time.Now().UTC().Add(time.Minute)
	fixture := &uploadFixture{snapshot: value.KubernetesIdentityUploadSnapshot{SetupRef: "ksu_fixture01", Digest: hex.EncodeToString(hash[:]), ContentBytes: uint32(len(material)), ExpiresAt: expires, Provision: provision, Target: value.KubernetesTarget{ID: provision.TargetRef}},
		identity: value.KubernetesIdentityLocator{IdentityRef: provision.IdentityRef, TargetRef: provision.TargetRef, Namespace: provision.Namespace, Revision: 1, Name: name, UID: "76b0b43a-84c2-44d6-81a4-7c477a92ef2e", ResourceVersion: "42", ContentSHA256: provision.ContentSHA256},
		view:     value.KubernetesIdentitySetupView{CanonicalContext: "fixture-context", SetupRef: "ksu_fixture01", ConnectionRef: "int_fixture01", ConnectionVersion: 1, IdentityRef: provision.IdentityRef, IdentityRevision: 1, TargetRef: provision.TargetRef, State: "COMPLETED", Policy: "PROJECT_SHARED", Version: 2, ExpiresAt: expires}}
	service, err := kubernetesidentity.New(fixture, fixture, fixture, fixture, "broker_fixture01", "broker-private")
	if err != nil {
		t.Fatal("construct local upload workflow")
	}
	return fixture, service, material
}

// TestKubernetesIdentitySetupHTTPRejectsBrowserAndAmbientAuthority covers protocol ingress negatives.
// None can call the owner or material store; plaintext and browser Origin never become a CLI lane.
// TestKubernetesIdentitySetupHTTPRejectsBrowserAndAmbientAuthority проверяет protocol ingress negatives.
// Они не вызывают owner либо material store; plaintext и browser Origin не становятся CLI lane.
func TestKubernetesIdentitySetupHTTPRejectsBrowserAndAmbientAuthority(t *testing.T) {
	fixture, service, _ := newUploadFixture(t)
	handler, err := New(service, "https://broker.example.test:9444")
	if err != nil {
		t.Fatal("construct direct upload handler")
	}
	for _, scenario := range []string{"plaintext", "browser", "foreign-host", "query", "redirect-path", "missing-permit", "material-observe"} {
		request := httptest.NewRequest(http.MethodPost, "https://broker.example.test:9444/v1/kubernetes-identity-setups/ksu_fixture01/upload", strings.NewReader("synthetic-invalid-content"))
		request.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
		switch scenario {
		case "plaintext":
			request.TLS = nil
		case "browser":
			request.Header.Set("Origin", "https://browser.example.test")
		case "foreign-host":
			request.Host = "foreign.example.test"
		case "query":
			request.URL.RawQuery = "target=foreign"
		case "redirect-path":
			request.URL.Path = "/v1/kubernetes-identity-setups/ksu_fixture01/upload/other"
		case "missing-permit":
			request.Header.Del("Authorization")
		case "material-observe":
			request.URL.Path = "/v1/kubernetes-identity-setups/ksu_fixture01/observe"
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 400 || fixture.claims.Load() != 0 || fixture.creates.Load() != 0 || strings.Contains(response.Body.String(), "synthetic-invalid-content") {
			t.Fatal("invalid ingress gained owner credit or echoed material")
		}
	}
}

// TestKubernetesIdentitySetupCLIToActualBrokerTLS joins the real Python CLI, TLS handler and workflow.
// Owner/storage ports remain disposable fixtures; this is not installed-cluster or SQL qualification.
// TestKubernetesIdentitySetupCLIToActualBrokerTLS соединяет настоящий Python CLI, TLS handler и workflow.
// Owner/storage ports — disposable fixtures; это не installed-cluster либо SQL qualification.
func TestKubernetesIdentitySetupCLIToActualBrokerTLS(t *testing.T) {
	fixture, service, material := newUploadFixture(t)
	backend := httptest.NewUnstartedServer(http.NotFoundHandler())
	origin := "https://" + backend.Listener.Addr().String()
	handler, err := New(service, origin)
	if err != nil {
		t.Fatal("construct actual broker TLS handler")
	}
	backend.Config.Handler = handler
	backend.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	backend.StartTLS()
	defer backend.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})
	hash := sha256.Sum256(ca)
	firstView := fixture.view
	firstView.State, firstView.Version = "PREPARED", 1
	admission := map[string]any{"setup": firstView, "reconcileOnly": false, "uploadPermit": base64.StdEncoding.EncodeToString(make([]byte, 32)), "endpoint": map[string]any{
		"serverOrigin": origin, "tlsServerName": "example.com", "certificateAuthorityPem": base64.StdEncoding.EncodeToString(ca), "certificateAuthoritySHA256": hex.EncodeToString(hash[:]), "addressIPs": []string{"127.0.0.1"}}}
	encoded, err := json.Marshal(admission)
	if err != nil {
		t.Fatal("encode safe synthetic admission")
	}
	directory := t.TempDir()
	admissionFile, materialFile := filepath.Join(directory, "admission.json"), filepath.Join(directory, "kubeconfig")
	if os.WriteFile(admissionFile, encoded, 0600) != nil || os.WriteFile(materialFile, material, 0600) != nil {
		t.Fatal("write owned private fixtures")
	}
	cli, err := filepath.Abs("../../../../../../tools/dev/managed-kubernetes-identity.py")
	if err != nil {
		t.Fatal("resolve canonical CLI path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-I", cli, "upload", "--admission-file", admissionFile, "--kubeconfig", materialFile)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C", "HTTPS_PROXY=http://unreachable.invalid:1"}
	output, err := command.CombinedOutput()
	if err != nil || fixture.creates.Load() != 1 || fixture.claims.Load() != 1 || !strings.Contains(string(output), `"state":"COMPLETED"`) || strings.Contains(string(output), string(material)) || strings.Contains(string(output), fixture.identity.UID) {
		t.Fatal("actual CLI/broker TLS workflow failed or exposed private material pins")
	}
}
