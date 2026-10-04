package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	store "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Authority здесь синтетическая; PostgreSQL eligibility доказывается отдельной
// component suite CP. Этот путь проверяет реальные AEAD/Kubernetes adapters и
// передачу точных опубликованных pins в runtime consumer без исполнения модели.
func testScopedPublishedRuntimeConsumer(t *testing.T, runtimeStore *store.Store, client *fake.Clientset, scope cp.RuntimeResourceScopeKind, source *cp.RuntimeSecretMaterialization, raw []byte) {
	t.Helper()
	providerRaw := []byte(`{"OPENAI_API_KEY":"synthetic-scope-consumer","auth_mode":"apikey"}`)
	provider, err := runtimeStore.CreateProviderCredential(t.Context(), "pauth_scope111", "pacc_scope111", providerRaw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manifest := store.CredentialProjectionManifest{
		Authority: store.ProjectionAuthority{RPCProfile: transportprofile.TrustedCluster,
			ActorID: "c20ac176-c0ca-499f-91a4-6fc65c4ef30e", TenantID: "71adb021-5229-4903-9f75-9fd34797665a", ProjectID: "e92277a1-c5d0-4d40-af73-54c34a256ef5",
			SourceRevision: 3, SourceDigestSHA256: strings.Repeat("a", 64), CallerCredentialRevision: 1,
			CallerWorkloadID: "runtime-controller", CallerFullMethod: "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials", ExpiresAt: now.Add(time.Minute)},
		WorkloadInstance: "workload_scope111", LeaseRef: "lease_scope111", Generation: 3,
		RuntimeRevisionRef: "rtrev_scope111", RuntimeRevisionDigest: strings.Repeat("a", 64), SessionRef: "ses_scope111", TurnRef: "turn_scope111", Attempt: 1, InputDigest: strings.Repeat("b", 64),
		ProviderCredential: store.ProviderProjectionBinding{AccountRef: "pacc_scope111", CredentialRevisionRef: "pcred_scope111", CredentialRevision: 1,
			SecretName: provider.SecretName, SecretUID: provider.SecretUID, SecretResourceVersion: provider.SecretResourceVersion, ContentSHA256: provider.ContentSHA256},
		RuntimeSecrets: []store.RuntimeSecretProjectionBinding{{Name: "SYNTHETIC_KEY", SecretRef: "sec_fixture01", Revision: 1,
			Namespace: source.Namespace, SecretName: source.SecretName, SecretKey: source.SecretKey,
			SecretUID: source.SecretUid, SecretResourceVersion: source.SecretResourceVersion, ContentSHA256: source.ContentSha256}},
		ExpiresAt: now.Add(30 * time.Second),
	}
	if scope == cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION {
		manifest.Authority.ProjectID = ""
		manifest.Authority.CallerFullMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
	}
	projection, err := runtimeStore.MaterializeRuntimeCredentialProjection(t.Context(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := client.CoreV1().Secrets("kodex-runtime").Get(t.Context(), projection.SecretName, metav1.GetOptions{})
	if err != nil || !bytes.Equal(actual.Data["SYNTHETIC_KEY"], raw) || !bytes.Equal(actual.Data["provider-auth.json"], providerRaw) {
		t.Fatal("runtime consumer lost published scoped values")
	}
	defer func() {
		for _, content := range actual.Data {
			clear(content)
		}
	}()
	listed, err := runtimeStore.ListRuntimeCredentialProjections(t.Context())
	if err != nil || len(listed) != 1 || listed[0].SecretUID != projection.SecretUID || listed[0].SecretResourceVersion != projection.SecretResourceVersion {
		t.Fatal("consumer recovery lost immutable projection pins")
	}
	if err := runtimeStore.DeleteRuntimeCredentialProjection(t.Context(), projection); err != nil {
		t.Fatal(err)
	}
	survived, err := client.CoreV1().Secrets("kodex-runtime").Get(t.Context(), source.SecretName, metav1.GetOptions{})
	if err != nil || string(survived.UID) != source.SecretUid || survived.ResourceVersion != source.SecretResourceVersion || !bytes.Equal(survived.Data["value"], raw) {
		t.Fatal("consumer cleanup deleted or changed published source")
	}
	for _, content := range survived.Data {
		clear(content)
	}
}
