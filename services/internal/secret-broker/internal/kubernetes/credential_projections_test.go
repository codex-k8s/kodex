package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// projectionNamespace uses the validator's fixed runtime namespace in fake projection tests, without
// contacting an existing cluster.
// projectionNamespace задаёт фиксированный runtime namespace validator для fake тестов проекции без
// обращения к существующему кластеру.
const projectionNamespace = "kodex-runtime"

// TestTrustedProjectionManifestRequiresExactExecutionSnapshot accepts the trusted execution snapshot and
// rejects fake proof JTI, divergent generation/digest and unsupported profiles; it tests structure, not
// a signed transport.
// TestTrustedProjectionManifestRequiresExactExecutionSnapshot допускает trusted execution snapshot и
// отклоняет подставленный proof JTI, другие generation/digest и неподдерживаемые профили; проверяется
// структура, не подписанный транспорт.
func TestTrustedProjectionManifestRequiresExactExecutionSnapshot(t *testing.T) {
	manifest := projectionManifest(
		ProviderCredentialDescriptor{SecretName: "provider-trusted", SecretUID: "provider-uid", SecretResourceVersion: "41", ContentSHA256: stringsOfHex('d')},
		Materialization{Namespace: projectionNamespace, Name: "runtime-trusted", SecretRef: "sec_trusted", Key: "TOKEN", Revision: 1, UID: "runtime-uid", ResourceVersion: "42", ContentSHA256: stringsOfHex('e')})
	manifest.Authority.RPCProfile = transportprofile.TrustedCluster
	manifest.Authority.ProofJTI = ""
	manifest.Authority.SourceRevision = uint64(manifest.Generation)
	manifest.Authority.SourceDigestSHA256 = manifest.RuntimeRevisionDigest
	if err := validateCredentialProjectionManifest(manifest, projectionNamespace); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*CredentialProjectionManifest){
		"fake proof":      func(m *CredentialProjectionManifest) { m.Authority.ProofJTI = "fake-proof" },
		"generation":      func(m *CredentialProjectionManifest) { m.Generation++ },
		"digest":          func(m *CredentialProjectionManifest) { m.RuntimeRevisionDigest = stringsOfHex('f') },
		"unknown profile": func(m *CredentialProjectionManifest) { m.Authority.RPCProfile = "insecure" },
		"missing profile": func(m *CredentialProjectionManifest) { m.Authority.RPCProfile = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := manifest
			mutate(&candidate)
			if validateCredentialProjectionManifest(candidate, projectionNamespace) == nil {
				t.Fatal("mismatched trusted snapshot accepted")
			}
		})
	}
}

// TestRuntimeCredentialProjectionMaterializesExactSourcesAndDeletesWithReadback creates synthetic
// provider/runtime sources, checks projected bytes and idempotent retries, then verifies metadata
// listing and deletion absence in a fake client.
// TestRuntimeCredentialProjectionMaterializesExactSourcesAndDeletesWithReadback создаёт синтетические
// provider/runtime источники, проверяет байты проекции и идемпотентный повтор, затем список metadata и
// отсутствие после удаления в fake клиенте.
func TestRuntimeCredentialProjectionMaterializesExactSourcesAndDeletesWithReadback(t *testing.T) {
	t.Parallel()
	store, client := newCredentialProjectionTestStore(t)
	providerRaw := []byte(`{"OPENAI_API_KEY":"synthetic-projection-key","auth_mode":"apikey"}`)
	provider, err := store.CreateProviderCredential(context.Background(), "pauth_projection1", "pacc_projection1", providerRaw)
	if err != nil {
		t.Fatal(err)
	}
	effect, runtimeValue := testEffect("secop_projection1", 4, "sec_projection1", 2, "synthetic-runtime-secret")
	runtimeSecret, err := store.CreateImmutableForEffect(context.Background(), effect, runtimeValue)
	if err != nil {
		t.Fatal(err)
	}
	manifest := projectionManifest(provider, runtimeSecret)
	first, err := store.MaterializeRuntimeCredentialProjection(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.MaterializeRuntimeCredentialProjection(context.Background(), manifest)
	if err != nil || !sameCredentialProjection(first, second) {
		t.Fatalf("exact retry is not idempotent: first=%#v second=%#v err=%v", first, second, err)
	}
	secret, err := client.CoreV1().Secrets(projectionNamespace).Get(context.Background(), first.SecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data[providerProjectionKey]) != string(providerRaw) || string(secret.Data["CRM_TOKEN"]) != string(runtimeValue) || len(secret.Data) != 2 {
		t.Fatalf("projection data does not match exact sources: keys=%v", sortedDataKeys(secret.Data))
	}
	clearSecretData(secret)
	listed, err := store.ListRuntimeCredentialProjections(context.Background())
	if err != nil || len(listed) != 1 || !sameCredentialProjection(listed[0], first) {
		t.Fatalf("projection list lost exact descriptor: %#v err=%v", listed, err)
	}
	if err := store.DeleteRuntimeCredentialProjection(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CoreV1().Secrets(projectionNamespace).Get(context.Background(), first.SecretName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("projection deletion did not reach absence readback: %v", err)
	}
}

// TestRuntimeCredentialProjectionRejectsChangedExactBinding verifies conflict when a deterministic
// projection name is reused with another workload and refusal of a stale provider resourceVersion before
// reuse.
// TestRuntimeCredentialProjectionRejectsChangedExactBinding проверяет конфликт повторного использования
// детерминированного имени проекции другим workload и отказ при устаревшем provider resourceVersion до
// повторного использования.
func TestRuntimeCredentialProjectionRejectsChangedExactBinding(t *testing.T) {
	t.Parallel()
	store, _ := newCredentialProjectionTestStore(t)
	providerRaw := []byte(`{"OPENAI_API_KEY":"synthetic-projection-key","auth_mode":"apikey"}`)
	provider, err := store.CreateProviderCredential(context.Background(), "pauth_projection2", "pacc_projection1", providerRaw)
	if err != nil {
		t.Fatal(err)
	}
	effect, runtimeValue := testEffect("secop_projection2", 5, "sec_projection2", 3, "synthetic-runtime-secret")
	runtimeSecret, err := store.CreateImmutableForEffect(context.Background(), effect, runtimeValue)
	if err != nil {
		t.Fatal(err)
	}
	manifest := projectionManifest(provider, runtimeSecret)
	if _, err := store.MaterializeRuntimeCredentialProjection(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	changed := manifest
	changed.WorkloadInstance = "different-workload-instance"
	if _, err := store.MaterializeRuntimeCredentialProjection(context.Background(), changed); !errors.Is(err, ErrCredentialProjectionConflict) {
		t.Fatalf("same deterministic name with changed binding must fail closed, got %v", err)
	}
	changed = manifest
	changed.ProviderCredential.SecretResourceVersion = "stale-version"
	if _, err := store.MaterializeRuntimeCredentialProjection(context.Background(), changed); !errors.Is(err, ErrProviderCredentialConflict) {
		t.Fatalf("stale provider credential binding must fail closed, got %v", err)
	}
}

// TestRuntimeCredentialProjectionListsExpiredRevisionForCleanup seeds an expired immutable projection to
// verify recovery listing still works while new materialization with that manifest is rejected.
// TestRuntimeCredentialProjectionListsExpiredRevisionForCleanup заранее помещает просроченную immutable
// проекцию и проверяет доступность списка recovery при отказе новой materialization с тем же manifest.
func TestRuntimeCredentialProjectionListsExpiredRevisionForCleanup(t *testing.T) {
	t.Parallel()
	store, client := newCredentialProjectionTestStore(t)
	manifest := projectionManifest(
		ProviderCredentialDescriptor{SecretName: "provider-expired", SecretUID: "provider-expired-uid", SecretResourceVersion: "41", ContentSHA256: stringsOfHex('d')},
		Materialization{Namespace: projectionNamespace, Name: "runtime-expired", SecretRef: "sec_expired", Key: "TOKEN", Revision: 1, UID: "runtime-expired-uid", ResourceVersion: "42", ContentSHA256: stringsOfHex('e')},
	)
	manifest.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	manifest.Authority.ExpiresAt = time.Now().UTC().Add(-30 * time.Second)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data := map[string][]byte{providerProjectionKey: []byte(`{"OPENAI_API_KEY":"expired-synthetic-key","auth_mode":"apikey"}`), "CRM_TOKEN": []byte("expired-synthetic-runtime-secret")}
	immutable := true
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "runtime-credentials-expired", Namespace: projectionNamespace,
		Labels:      map[string]string{credentialProjectionLabel: "true", providerManagedByLabel: providerSecretBrokerManager, providerPartOfLabel: "kodex"},
		Annotations: map[string]string{credentialProjectionAnnotation: string(manifestJSON), credentialProjectionDigest: projectionDataDigest(data)}},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data}
	if _, err := client.CoreV1().Secrets(projectionNamespace).Create(context.Background(), secret, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListRuntimeCredentialProjections(context.Background())
	if err != nil || len(listed) != 1 || !listed[0].Manifest.ExpiresAt.Equal(manifest.ExpiresAt) {
		t.Fatalf("expired projection must remain readable for exact cleanup: listed=%#v err=%v", listed, err)
	}
	if _, err := store.MaterializeRuntimeCredentialProjection(context.Background(), manifest); !errors.Is(err, ErrCredentialProjectionInvalid) {
		t.Fatalf("new expired projection must be rejected, got %v", err)
	}
}

// projectionManifest constructs synthetic signed-profile provenance, one provider binding and one
// CRM_TOKEN runtime binding with a short future expiry; it is data for validators, not a real verified
// proof.
// projectionManifest строит синтетическое происхождение signed-профиля, одну provider-привязку и одну
// runtime-привязку CRM_TOKEN с коротким будущим сроком; это данные для validator, не реальный
// проверенный proof.
func projectionManifest(provider ProviderCredentialDescriptor, runtimeSecret Materialization) CredentialProjectionManifest {
	now := time.Now().UTC()
	return CredentialProjectionManifest{
		Authority: ProjectionAuthority{
			ActorID: "c20ac176-c0ca-499f-91a4-6fc65c4ef30e", TenantID: "71adb021-5229-4903-9f75-9fd34797665a",
			ProjectID: "e92277a1-c5d0-4d40-af73-54c34a256ef5", SourceRevision: 8,
			SourceDigestSHA256: stringsOfHex('a'), ProofJTI: "9671137c-0288-4446-803e-f3c2d13dcbe8",
			CallerWorkloadID: "runtime-controller", CallerFullMethod: "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials",
			CallerCredentialRevision: 3, ExpiresAt: now.Add(time.Minute),
		},
		WorkloadInstance: "runtime-instance-1", LeaseRef: "lease_projection1", Generation: 4,
		RuntimeRevisionRef: "rtrev_projection1", RuntimeRevisionDigest: stringsOfHex('b'),
		SessionRef: "ses_projection1", TurnRef: "turn_projection1", Attempt: 1, InputDigest: stringsOfHex('c'),
		ProviderCredential: ProviderProjectionBinding{
			AccountRef: "pacc_projection1", CredentialRevisionRef: "pcred_projection1", CredentialRevision: 4,
			SecretName: provider.SecretName, SecretUID: provider.SecretUID, SecretResourceVersion: provider.SecretResourceVersion,
			ContentSHA256: provider.ContentSHA256,
		},
		RuntimeSecrets: []RuntimeSecretProjectionBinding{{
			Name: "CRM_TOKEN", SecretRef: runtimeSecret.SecretRef, Revision: runtimeSecret.Revision, Namespace: runtimeSecret.Namespace,
			SecretName: runtimeSecret.Name, SecretKey: runtimeSecret.Key, SecretUID: runtimeSecret.UID,
			SecretResourceVersion: runtimeSecret.ResourceVersion, ContentSHA256: runtimeSecret.ContentSHA256,
		}},
		ExpiresAt: now.Add(30 * time.Second),
	}
}

// newCredentialProjectionTestStore creates an in-memory client whose create reactor assigns synthetic
// object pins. Its local version counter assumes sequential use and the tracker does not enforce real
// API validation or immutability.
// newCredentialProjectionTestStore создаёт клиент в памяти, чей create reactor назначает синтетические
// привязки объекта. Локальный счётчик версий предполагает последовательное использование; tracker не
// обеспечивает реальные проверки API или immutability.
func newCredentialProjectionTestStore(t *testing.T) (*Store, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset()
	var resourceVersion int64 = 100
	client.Fake.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		created := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		resourceVersion++
		created.UID = types.UID("uid-" + created.Name)
		created.ResourceVersion = strconv.FormatInt(resourceVersion, 10)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), created, created.Namespace)
		return true, created, err
	})
	store, err := New(client, projectionNamespace)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

// stringsOfHex repeats the supplied byte across SHA-256-sized input and hex-encodes it to produce a
// deterministic digest-shaped fixture, not a hash of real credential data.
// stringsOfHex повторяет заданный байт во входе размера SHA-256 и кодирует его в hex для
// детерминированной fixture формы digest, а не хеша реальных учётных данных.
func stringsOfHex(value byte) string {
	raw := make([]byte, sha256.Size)
	for index := range raw {
		raw[index] = value
	}
	return hex.EncodeToString(raw)
}

// sortedDataKeys collects data-map keys for failure diagnostics without sorting them; callers must not
// rely on a stable order despite the historical helper name.
// sortedDataKeys собирает ключи Data map для диагностики отказа без сортировки; несмотря на историческое
// имя helper, устойчивый порядок не гарантируется.
func sortedDataKeys(data map[string][]byte) []string {
	result := make([]string, 0, len(data))
	for key := range data {
		result = append(result, key)
	}
	return result
}
