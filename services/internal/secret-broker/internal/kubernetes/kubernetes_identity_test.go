package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// kubernetesIdentityStoreFixture creates only a synthetic immutable purpose object for local tests.
// The bytes intentionally test storage custody, not kubeconfig parsing or any live account.
// kubernetesIdentityStoreFixture создаёт synthetic immutable purpose object для локальных тестов.
// Байты проверяют storage custody, не kubeconfig parsing либо live account.
func kubernetesIdentityStoreFixture(t *testing.T) (*corev1.Secret, value.KubernetesIdentityLocator) {
	t.Helper()
	material := []byte("synthetic-only-private-kubeconfig-storage-fixture")
	digest := sha256.Sum256(material)
	name, err := value.KubernetesIdentitySecretName("kbi_synthetic_identity", 3)
	if err != nil {
		t.Fatal("construct synthetic purpose name")
	}
	locator := value.KubernetesIdentityLocator{IdentityRef: "kbi_synthetic_identity", TargetRef: "kbt_synthetic_target", Revision: 3,
		Namespace: "synthetic-broker", Name: name, UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37", ContentSHA256: hex.EncodeToString(digest[:])}
	immutable := true
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Namespace: locator.Namespace, Name: locator.Name, UID: types.UID(locator.UID), ResourceVersion: locator.ResourceVersion,
		Labels: map[string]string{kubernetesIdentityPurposeLabel: "KUBERNETES_TARGET"},
		Annotations: map[string]string{kubernetesIdentityRefAnnotation: locator.IdentityRef, kubernetesIdentityTargetAnnotation: locator.TargetRef,
			kubernetesIdentityRevisionAnnotation: "3", kubernetesIdentityDigestAnnotation: locator.ContentSHA256}},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{kubernetesIdentityDataKey: material}}
	return secret, locator
}

// TestReadKubernetesIdentityKeepsSeparatePurpose verifies exact custody and caller-owned byte copies;
// private locators do not appear through default JSON or Go diagnostic formatting.
// TestReadKubernetesIdentityKeepsSeparatePurpose проверяет exact custody и caller-owned byte copies;
// private locators не появляются через default JSON либо Go diagnostic formatting.
func TestReadKubernetesIdentityKeepsSeparatePurpose(t *testing.T) {
	secret, locator := kubernetesIdentityStoreFixture(t)
	store, wire, _, _ := newKubernetesIdentityWire(t, 0, false)
	wire.mu.Lock()
	wire.stored = secret.DeepCopy()
	wire.mu.Unlock()
	material, err := store.ReadKubernetesIdentity(context.Background(), locator)
	if err != nil || !bytes.Equal(material, secret.Data[kubernetesIdentityDataKey]) {
		t.Fatal("exact purpose credential was not resolved privately")
	}
	clear(material)
	material, err = store.ReadKubernetesIdentity(context.Background(), locator)
	if err != nil || !bytes.Equal(material, secret.Data[kubernetesIdentityDataKey]) {
		t.Fatal("caller mutation changed stored purpose bytes")
	}
	clear(material)
	raw, err := json.Marshal(locator)
	if err != nil || string(raw) != "{}" || strings.Contains(fmt.Sprintf("%v %#v", locator, locator), locator.Name) {
		t.Fatal("private credential locator escaped default formatting")
	}
	if posts, gets := wire.counts(); posts != 0 || gets != 2 {
		t.Fatal("private purpose read performed an unrelated HTTP effect")
	}
}

// TestReadKubernetesIdentityRejectsForeignOrMutableMaterial checks purpose, target, generation,
// object pins, immutable topology and content rather than accepting a platform-looking Secret.
// TestReadKubernetesIdentityRejectsForeignOrMutableMaterial проверяет purpose, target, generation,
// object pins, immutable topology и content вместо доверия похожему на платформенный Secret.
func TestReadKubernetesIdentityRejectsForeignOrMutableMaterial(t *testing.T) {
	for _, mutate := range []func(*corev1.Secret){
		func(secret *corev1.Secret) { secret.Labels[kubernetesIdentityPurposeLabel] = "SSH_TARGET" },
		func(secret *corev1.Secret) { secret.Labels[managedLabel] = "true" },
		func(secret *corev1.Secret) { secret.Labels[managedLabel] = "" },
		func(secret *corev1.Secret) {
			secret.Annotations[kubernetesIdentityRefAnnotation] = "kbi_foreign_identity"
		},
		func(secret *corev1.Secret) {
			secret.Annotations[kubernetesIdentityTargetAnnotation] = "kbt_foreign_target"
		},
		func(secret *corev1.Secret) { secret.Annotations[kubernetesIdentityRevisionAnnotation] = "4" },
		func(secret *corev1.Secret) {
			secret.Annotations[kubernetesIdentityDigestAnnotation] = strings.Repeat("b", 64)
		},
		func(secret *corev1.Secret) { secret.UID = "7fb0912f-a690-453f-be9a-3520d2e99a85" },
		func(secret *corev1.Secret) { secret.ResourceVersion = "38" },
		func(secret *corev1.Secret) { secret.Immutable = nil },
		func(secret *corev1.Secret) { flag := false; secret.Immutable = &flag },
		func(secret *corev1.Secret) { secret.Type = corev1.SecretTypeServiceAccountToken },
		func(secret *corev1.Secret) { stamp := metav1.Now(); secret.DeletionTimestamp = &stamp },
		func(secret *corev1.Secret) { secret.OwnerReferences = []metav1.OwnerReference{{Name: "foreign-owner"}} },
		func(secret *corev1.Secret) { secret.Data["extra"] = []byte("synthetic-extra") },
		func(secret *corev1.Secret) { secret.Data[kubernetesIdentityDataKey] = []byte("synthetic-changed") },
		func(secret *corev1.Secret) {
			secret.Data[kubernetesIdentityDataKey] = make([]byte, maximumKubernetesIdentityBytes+1)
		},
		func(secret *corev1.Secret) { secret.StringData = map[string]string{"extra": "synthetic-extra"} },
	} {
		secret, locator := kubernetesIdentityStoreFixture(t)
		mutate(secret)
		store, wire, _, _ := newKubernetesIdentityWire(t, 0, false)
		wire.mu.Lock()
		wire.stored = secret.DeepCopy()
		wire.mu.Unlock()
		material, err := store.ReadKubernetesIdentity(context.Background(), locator)
		posts, gets := wire.counts()
		if !errors.Is(err, ErrKubernetesIdentityUnavailable) || material != nil || posts != 0 || gets != 1 {
			t.Fatal("foreign or mutable private purpose material was returned")
		}
	}
}

// TestKubernetesIdentityCannotUseGenericProjection refuses generic Secret access and malformed
// private pins before API reads; cancellation does not resolve any private bytes.
// TestKubernetesIdentityCannotUseGenericProjection отвергает generic Secret access и malformed
// private pins до API reads; отмена не разрешает private bytes.
func TestKubernetesIdentityCannotUseGenericProjection(t *testing.T) {
	secret, locator := kubernetesIdentityStoreFixture(t)
	client := fake.NewClientset(secret)
	store, _ := New(client, locator.Namespace)
	_, material, err := store.ReadExactValue(context.Background(), ExactDescriptor{Namespace: locator.Namespace, Name: locator.Name,
		SecretRef: "sec_synthetic", Key: "kubeconfig", Revision: locator.Revision, UID: locator.UID, ResourceVersion: locator.ResourceVersion, ContentSHA256: locator.ContentSHA256})
	if err == nil || material != nil || len(client.Actions()) != 0 {
		t.Fatal("broker purpose identity entered generic runtime credential projection")
	}
	for _, mutate := range []func(*value.KubernetesIdentityLocator){
		func(locator *value.KubernetesIdentityLocator) { locator.Namespace = "foreign" },
		func(locator *value.KubernetesIdentityLocator) { locator.IdentityRef = "../foreign" },
		func(locator *value.KubernetesIdentityLocator) { locator.TargetRef = "" },
		func(locator *value.KubernetesIdentityLocator) { locator.Name = "kodex-integration-credentials" },
		func(locator *value.KubernetesIdentityLocator) { locator.Revision = 0 },
		func(locator *value.KubernetesIdentityLocator) { locator.UID = "invalid" },
		func(locator *value.KubernetesIdentityLocator) { locator.ResourceVersion = "\x1b" },
		func(locator *value.KubernetesIdentityLocator) { locator.ContentSHA256 = "" },
	} {
		changed := locator
		mutate(&changed)
		if material, err := store.ReadKubernetesIdentity(context.Background(), changed); !errors.Is(err, value.ErrKubernetesIdentityLocator) || material != nil || len(client.Actions()) != 0 {
			t.Fatal("invalid private purpose locator reached the Secret API")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if material, err := store.ReadKubernetesIdentity(ctx, locator); !errors.Is(err, context.Canceled) || material != nil || len(client.Actions()) != 0 {
		t.Fatal("canceled private identity read reached the Secret API")
	}
}
