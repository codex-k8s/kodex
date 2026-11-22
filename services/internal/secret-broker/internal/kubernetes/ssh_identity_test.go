package kubernetes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	sshrepository "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// sshMaterialFixture creates only synthetic bytes in the dedicated purpose Secret with all immutable owner tuple pins, without creating a runtime-secret descriptor.
// sshMaterialFixture создаёт только synthetic bytes в отдельном purpose Secret со всеми immutable owner tuple pins без runtime-secret descriptor.
func sshMaterialFixture() (value.SSHIdentityLocator, *corev1.Secret) {
	material := []byte("synthetic-private-material-for-storage-only")
	digest := sha256.Sum256(material)
	locator := value.SSHIdentityLocator{Purpose: value.SSHIdentityPurpose, IdentityRevisionRef: "broker_identity_ssh_example01", TargetRef: "ssh_target_example01", Revision: 1, Namespace: "synthetic-ssh", SecretUID: "opaque-uid-synthetic", SecretResourceVersion: "opaque-rv-7", ContentSHA256: hex.EncodeToString(digest[:]), AuthenticationSHA256: strings.Repeat("a", 64)}
	locator.SecretName = value.SSHIdentitySecretName(locator.IdentityRevisionRef, locator.Revision)
	immutable := true
	secret := &corev1.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Namespace: locator.Namespace, Name: locator.SecretName, UID: types.UID(locator.SecretUID), ResourceVersion: locator.SecretResourceVersion, Labels: map[string]string{sshIdentityPurposeLabel: value.SSHIdentityPurpose}, Annotations: map[string]string{sshIdentityRevisionAnnotation: locator.IdentityRevisionRef, sshIdentityTargetAnnotation: locator.TargetRef, sshIdentityGenerationAnnotation: "1", sshIdentityContentAnnotation: locator.ContentSHA256, sshIdentityPublicAnnotation: locator.AuthenticationSHA256}}, Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{sshIdentityMaterialKey: material}}
	return locator, secret
}

// TestSSHIdentityMaterialReadUsesExactPurposeObjectAndBytes proves the real Kubernetes adapter refuses replacement UIDs, stale resource versions, other purposes and data-map extensions.
// TestSSHIdentityMaterialReadUsesExactPurposeObjectAndBytes доказывает отказ actual Kubernetes adapter при replacement UID, stale resource version, другом purpose и data-map extensions.
func TestSSHIdentityMaterialReadUsesExactPurposeObjectAndBytes(t *testing.T) {
	locator, original := sshMaterialFixture()
	store, fixture, _ := newSSHMaterialWire(t)
	fixture.mutex.Lock()
	fixture.secret = original.DeepCopy()
	fixture.mutex.Unlock()
	material, err := store.ReadSSHIdentity(t.Context(), locator)
	if err != nil || !bytes.Equal(material, original.Data[sshIdentityMaterialKey]) {
		t.Fatal("exact SSH purpose material rejected")
	}
	clear(material)
	fixture.mutex.Lock()
	unchanged := bytes.Equal(fixture.secret.Data[sshIdentityMaterialKey], original.Data[sshIdentityMaterialKey])
	fixture.mutex.Unlock()
	if !unchanged {
		t.Fatal("consumer clearing mutated stored material")
	}
	mutations := []func(*corev1.Secret){
		func(secret *corev1.Secret) { secret.UID = "44444444-4444-4444-8444-444444444444" },
		func(secret *corev1.Secret) { secret.ResourceVersion = "8" },
		func(secret *corev1.Secret) { secret.Immutable = nil },
		func(secret *corev1.Secret) { *secret.Immutable = false },
		func(secret *corev1.Secret) { secret.Type = corev1.SecretTypeSSHAuth },
		func(secret *corev1.Secret) { secret.Labels[sshIdentityPurposeLabel] = "KUBERNETES_TARGET" },
		func(secret *corev1.Secret) {
			secret.Annotations[sshIdentityTargetAnnotation] = "44444444-4444-4444-8444-444444444444"
		},
		func(secret *corev1.Secret) { secret.Annotations[sshIdentityRevisionAnnotation] = locator.TargetRef },
		func(secret *corev1.Secret) { secret.Annotations[sshIdentityGenerationAnnotation] = "2" },
		func(secret *corev1.Secret) { secret.Annotations[sshIdentityPublicAnnotation] = strings.Repeat("b", 64) },
		func(secret *corev1.Secret) {
			secret.Annotations[sshIdentityContentAnnotation] = strings.Repeat("b", 64)
		},
		func(secret *corev1.Secret) { secret.Data[sshIdentityMaterialKey][0] ^= 1 },
		func(secret *corev1.Secret) { secret.Data["generic"] = []byte("extra") },
		func(secret *corev1.Secret) { secret.Data = map[string][]byte{"kubeconfig": []byte("wrong purpose")} },
		func(secret *corev1.Secret) { secret.StringData = map[string]string{"private_key": "alternate"} },
		func(secret *corev1.Secret) { secret.Finalizers = []string{"example.invalid/foreign"} },
		func(secret *corev1.Secret) {
			secret.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "synthetic", UID: "foreign-owner"}}
		},
		func(secret *corev1.Secret) { now := metav1.Now(); secret.DeletionTimestamp = &now },
	}
	for index, mutate := range mutations {
		secret := original.DeepCopy()
		mutate(secret)
		fixture.mutex.Lock()
		fixture.secret = secret
		fixture.mutex.Unlock()
		observed, err := store.ReadSSHIdentity(t.Context(), locator)
		if !errors.Is(err, sshrepository.ErrIdentityRejected) || len(observed) != 0 {
			t.Fatal("foreign or malformed SSH material admitted", index)
		}
	}
}

// TestSSHIdentityLocatorRejectsGenericAndForeignNamespaceBeforeGET checks the private owner boundary never uses a fallback generic descriptor or another namespace to resolve missing SSH material.
// TestSSHIdentityLocatorRejectsGenericAndForeignNamespaceBeforeGET проверяет, что private owner boundary не использует generic descriptor fallback либо другой namespace для missing SSH material.
func TestSSHIdentityLocatorRejectsGenericAndForeignNamespaceBeforeGET(t *testing.T) {
	locator, secret := sshMaterialFixture()
	mutations := []func(*value.SSHIdentityLocator){
		func(value *value.SSHIdentityLocator) { value.Purpose = "TOKEN" },
		func(value *value.SSHIdentityLocator) { value.Namespace = "foreign" },
		func(value *value.SSHIdentityLocator) { value.Namespace = "../synthetic-ssh" },
		func(value *value.SSHIdentityLocator) { value.SecretName = "runtime-secret-generic" },
		func(value *value.SSHIdentityLocator) { value.SecretResourceVersion = "" },
		func(value *value.SSHIdentityLocator) { value.SecretUID = "" },
		func(value *value.SSHIdentityLocator) { value.ContentSHA256 = "" },
	}
	for index, mutate := range mutations {
		client := fake.NewSimpleClientset(secret)
		store, err := New(client, locator.Namespace)
		if err != nil {
			t.Fatal(err)
		}
		wrong := locator
		mutate(&wrong)
		if material, err := store.ReadSSHIdentity(t.Context(), wrong); !errors.Is(err, sshrepository.ErrIdentityRejected) || len(material) != 0 || len(client.Actions()) != 0 {
			t.Fatal("invalid private locator reached Kubernetes", index)
		}
	}
}
