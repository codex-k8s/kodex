package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// identityMaterialReaderFixture supplies only caller-owned disposable bytes and records reads.
// It deliberately permits partial error results to test unconditional buffer clearing.
// identityMaterialReaderFixture возвращает только caller-owned disposable bytes и считает reads.
// Намеренно допускает partial error results для проверки безусловного очищения буферов.
type identityMaterialReaderFixture struct {
	material []byte
	err      error
	calls    int
}

// ReadKubernetesIdentity transfers the fixture slice without making a copy, exposing any leak.
// ReadKubernetesIdentity передаёт fixture slice без копии, выявляя любое отсутствие очищения.
func (reader *identityMaterialReaderFixture) ReadKubernetesIdentity(context.Context, value.KubernetesIdentityLocator) ([]byte, error) {
	reader.calls++
	return reader.material, reader.err
}

// identityLocatorFixture binds a synthetic byte digest to the parser fixture's exact target.
// It does not create a live identity, owner permission or Kubernetes object.
// identityLocatorFixture связывает synthetic byte digest с exact target parser fixture.
// Не создаёт live identity, owner permission либо Kubernetes object.
func identityLocatorFixture(t *testing.T, material []byte, target Target) value.KubernetesIdentityLocator {
	t.Helper()
	name, err := value.KubernetesIdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("construct synthetic private identity name")
	}
	digest := sha256.Sum256(material)
	return value.KubernetesIdentityLocator{IdentityRef: "kbi_synthetic_identity", TargetRef: target.ID, Revision: 3,
		Namespace: "synthetic-broker", Name: name, UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37", ContentSHA256: hex.EncodeToString(digest[:])}
}

// TestLoadCredentialJoinsPurposeStoreAndStrictParser uses the actual private storage adapter and
// parser together, while the Kubernetes client remains local and the identity fully disposable.
// TestLoadCredentialJoinsPurposeStoreAndStrictParser соединяет actual private storage adapter и
// parser; Kubernetes client остаётся локальным, identity полностью одноразовая.
func TestLoadCredentialJoinsPurposeStoreAndStrictParser(t *testing.T) {
	config, target, now := credentialFixture(t)
	material := encodeCredential(t, config)
	defer clear(material)
	locator := identityLocatorFixture(t, material, target)
	immutable := true
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Namespace: locator.Namespace, Name: locator.Name, UID: types.UID(locator.UID), ResourceVersion: locator.ResourceVersion,
		Labels: map[string]string{"broker-credentials.kodex.dev/purpose": "KUBERNETES_TARGET"},
		Annotations: map[string]string{"broker-credentials.kodex.dev/identity-ref": locator.IdentityRef,
			"broker-credentials.kodex.dev/target-ref": locator.TargetRef, "broker-credentials.kodex.dev/revision": "3",
			"broker-credentials.kodex.dev/content-sha256": locator.ContentSHA256}},
		Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: map[string][]byte{"kubeconfig": material}}
	var reads atomic.Int32
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/"+locator.Namespace+"/secrets/"+locator.Name || r.Header.Get("Authorization") != "Bearer synthetic-storage-auth" {
			http.Error(w, "invalid synthetic private read", http.StatusForbidden)
			return
		}
		reads.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(secret)
	}))
	defer backend.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: backend.URL, BearerToken: "synthetic-storage-auth", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})}})
	if err != nil {
		t.Fatal("construct actual synthetic TLS private storage client")
	}
	store, err := kubernetesstore.New(client, locator.Namespace)
	if err != nil {
		t.Fatal("construct synthetic private identity store")
	}
	credential, err := LoadCredential(context.Background(), store, locator, target, now)
	if err != nil || credential.target.ID != target.ID || len(credential.cert.Certificate) != 1 || credential.token != "" || reads.Load() != 1 {
		t.Fatal("private purpose store did not reach the strict selected-target parser")
	}
}

// TestLoadCredentialClearsPartialErrorsAndNeverFallsBack verifies that changed material, unsafe
// kubeconfig and storage errors cannot select another identity or leak the original byte buffer.
// TestLoadCredentialClearsPartialErrorsAndNeverFallsBack проверяет, что changed material, unsafe
// kubeconfig и storage errors не выбирают другую identity и не оставляют исходный byte buffer.
func TestLoadCredentialClearsPartialErrorsAndNeverFallsBack(t *testing.T) {
	config, target, now := credentialFixture(t)
	for _, scenario := range []string{"success", "partial-error", "digest-mismatch", "unsafe-config"} {
		material := encodeCredential(t, config)
		if scenario == "unsafe-config" {
			material = append(material, []byte("unexpected-secret-field: synthetic-private-value\n")...)
		}
		locator := identityLocatorFixture(t, material, target)
		reader := &identityMaterialReaderFixture{material: material}
		if scenario == "partial-error" {
			reader.err = errors.New("synthetic upstream diagnostic must stay private")
		}
		if scenario == "digest-mismatch" {
			locator.ContentSHA256 = strings.Repeat("a", 64)
		}
		_, err := LoadCredential(context.Background(), reader, locator, target, now)
		if (scenario == "success") != (err == nil) || err != nil && !errors.Is(err, ErrPrivateIdentityRejected) || reader.calls != 1 {
			t.Fatal("private identity loader changed outcome or retried another source")
		}
		for _, octet := range reader.material {
			if octet != 0 {
				t.Fatal("private identity bytes survived parser or partial-error completion")
			}
		}
	}
	material := encodeCredential(t, config)
	defer clear(material)
	locator := identityLocatorFixture(t, material, target)
	locator.TargetRef = "kbt_foreign_target"
	reader := &identityMaterialReaderFixture{material: material}
	if _, err := LoadCredential(context.Background(), reader, locator, target, now); !errors.Is(err, ErrPrivateIdentityRejected) || reader.calls != 0 {
		t.Fatal("foreign target resolved private identity bytes before validation")
	}
}
