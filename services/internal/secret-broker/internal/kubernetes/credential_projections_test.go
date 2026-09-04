package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const projectionNamespace = "kodex-runtime"

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

func stringsOfHex(value byte) string {
	raw := make([]byte, sha256.Size)
	for index := range raw {
		raw[index] = value
	}
	return hex.EncodeToString(raw)
}

func sortedDataKeys(data map[string][]byte) []string {
	result := make([]string, 0, len(data))
	for key := range data {
		result = append(result, key)
	}
	return result
}
