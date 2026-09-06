package stagingstorage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func storageFixture(t *testing.T) (*Store, *fake.Clientset, value.DraftWork, value.EncryptedSecretDraft) {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		secret.UID, secret.ResourceVersion = types.UID("fixture-staged-uid"), "31"
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace)
		return true, secret, err
	})
	store, err := New(client.CoreV1().Secrets("kodex-system"), "kodex-system")
	if err != nil {
		t.Fatal("fixture store initialization failed")
	}
	digest := sha256.Sum256([]byte("synthetic draft value"))
	work := value.DraftWork{StagedNamespace: "kodex-system", StagedName: "draft-fixture", StagedKey: "ciphertext",
		Binding: value.SecretDraftBinding{ProjectRef: "prj_fixture", SecretRef: "sec_fixture", DraftRef: "drf_fixture",
			DraftGeneration: 2, ValueType: "STRING", ContentSHA256: hex.EncodeToString(digest[:])}}
	encrypted := value.EncryptedSecretDraft{Key: value.DraftEncryptionKey{ID: strings.Repeat("a", 64), Generation: 3}, Ciphertext: bytes.Repeat([]byte{0x29}, 64)}
	return store, client, work, encrypted
}

func TestEncryptedStoreReadbackAndExactDelete(t *testing.T) {
	store, client, work, encrypted := storageFixture(t)
	descriptor, err := store.Create(t.Context(), work, encrypted)
	if err != nil {
		t.Fatal("encrypted create failed")
	}
	actual, err := store.Lookup(t.Context(), work)
	if err != nil || actual != descriptor {
		t.Fatal("exact lookup failed")
	}
	read, err := store.Read(t.Context(), work, descriptor)
	if err != nil || !bytes.Equal(read.Ciphertext, encrypted.Ciphertext) {
		t.Fatal("exact encrypted read failed")
	}
	clear(read.Ciphertext)
	read, err = store.Read(t.Context(), work, descriptor)
	if err != nil || read.Ciphertext[0] == 0 {
		t.Fatal("response aliases immutable store")
	}
	if err := store.Delete(t.Context(), work, descriptor); err != nil {
		t.Fatal("exact encrypted delete failed")
	}
	if err := store.Delete(t.Context(), work, descriptor); err != nil {
		t.Fatal("repeated NotFound cleanup failed")
	}
	deletes := 0
	for _, action := range client.Actions() {
		if action.GetVerb() != "delete" {
			continue
		}
		deletes++
		options := action.(k8stesting.DeleteAction).GetDeleteOptions()
		if options.Preconditions == nil || options.Preconditions.UID == nil || options.Preconditions.ResourceVersion == nil ||
			string(*options.Preconditions.UID) != descriptor.UID || *options.Preconditions.ResourceVersion != descriptor.ResourceVersion {
			t.Fatal("delete omitted exact UID/RV preconditions")
		}
	}
	if deletes != 1 {
		t.Fatal("cleanup repeated an external delete")
	}
}

func TestEncryptedStoreRejectsEveryForeignDescriptorAndBinding(t *testing.T) {
	store, _, work, encrypted := storageFixture(t)
	descriptor, err := store.Create(t.Context(), work, encrypted)
	if err != nil {
		t.Fatal("fixture encrypted create failed")
	}
	for name, alter := range map[string]func(*value.DraftEncryptedDescriptor){
		"namespace":      func(d *value.DraftEncryptedDescriptor) { d.Namespace = "foreign" },
		"name":           func(d *value.DraftEncryptedDescriptor) { d.Name = "foreign" },
		"key":            func(d *value.DraftEncryptedDescriptor) { d.DataKey = "foreign" },
		"uid":            func(d *value.DraftEncryptedDescriptor) { d.UID = "foreign" },
		"rv":             func(d *value.DraftEncryptedDescriptor) { d.ResourceVersion = "99" },
		"digest":         func(d *value.DraftEncryptedDescriptor) { d.CiphertextSHA256 = strings.Repeat("b", 64) },
		"key-id":         func(d *value.DraftEncryptedDescriptor) { d.EncryptionKey.ID = strings.Repeat("b", 64) },
		"key-generation": func(d *value.DraftEncryptedDescriptor) { d.EncryptionKey.Generation++ },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := descriptor
			alter(&foreign)
			if read, err := store.Read(t.Context(), work, foreign); !errors.Is(err, secretdrafts.ErrConflict) || len(read.Ciphertext) != 0 {
				t.Fatal("foreign descriptor returned content")
			}
			if err := store.Delete(t.Context(), work, foreign); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("foreign descriptor allowed delete")
			}
		})
	}
	for name, alter := range map[string]func(*value.SecretDraftBinding){
		"project":    func(b *value.SecretDraftBinding) { b.ProjectRef = "prj_other" },
		"secret":     func(b *value.SecretDraftBinding) { b.SecretRef = "sec_other" },
		"draft":      func(b *value.SecretDraftBinding) { b.DraftRef = "drf_other" },
		"generation": func(b *value.SecretDraftBinding) { b.DraftGeneration++ },
		"type":       func(b *value.SecretDraftBinding) { b.ValueType = "BINARY" },
		"digest":     func(b *value.SecretDraftBinding) { b.ContentSHA256 = strings.Repeat("c", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			foreign := work
			alter(&foreign.Binding)
			if _, err := store.Lookup(t.Context(), foreign); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("foreign owner binding resolved")
			}
		})
	}
}

func TestEncryptedStoreRejectsCorruptionAndReplacement(t *testing.T) {
	for _, kind := range []string{"mutable", "deleting", "corrupt-data", "extra-data", "wrong-label", "wrong-uid", "wrong-rv", "wrong-key-generation"} {
		t.Run(kind, func(t *testing.T) {
			store, client, work, encrypted := storageFixture(t)
			descriptor, err := store.Create(t.Context(), work, encrypted)
			if err != nil {
				t.Fatal("fixture encrypted create failed")
			}
			object, _ := client.CoreV1().Secrets(work.StagedNamespace).Get(t.Context(), work.StagedName, metav1.GetOptions{})
			switch kind {
			case "mutable":
				object.Immutable = nil
			case "deleting":
				now := metav1.Now()
				object.DeletionTimestamp = &now
			case "corrupt-data":
				object.Data[work.StagedKey][0] ^= 1
			case "extra-data":
				object.Data["unexpected"] = []byte("synthetic")
			case "wrong-label":
				object.Labels[managedLabel] = "false"
			case "wrong-uid":
				object.UID = "replacement"
			case "wrong-rv":
				object.ResourceVersion = "replacement"
			case "wrong-key-generation":
				object.Annotations[keyGenerationAnnotation] = "5"
			}
			if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), object, work.StagedNamespace); err != nil {
				t.Fatal("fixture mutation failed")
			}
			if err := store.Delete(t.Context(), work, descriptor); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("corrupt or replaced object accepted")
			}
			if _, err := client.CoreV1().Secrets(work.StagedNamespace).Get(t.Context(), work.StagedName, metav1.GetOptions{}); err != nil {
				t.Fatal("unexpected deletion")
			}
		})
	}
}

func TestEncryptedStoreDeleteRaceAndUnknownCreateDoNotRetry(t *testing.T) {
	store, client, work, encrypted := storageFixture(t)
	descriptor, err := store.Create(t.Context(), work, encrypted)
	if err != nil {
		t.Fatal("fixture encrypted create failed")
	}
	client.PrependReactor("delete", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(corev1.Resource("secrets"), work.StagedName, errors.New("synthetic conflict"))
	})
	if err := store.Delete(t.Context(), work, descriptor); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("delete race did not fail closed")
	}
	newCipher := value.EncryptedSecretDraft{Key: encrypted.Key, Ciphertext: bytes.Repeat([]byte{0x52}, 64)}
	if _, err := store.Create(t.Context(), work, newCipher); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("same immutable name accepted different ciphertext")
	}
	read, err := store.Read(t.Context(), work, descriptor)
	if err != nil || !bytes.Equal(read.Ciphertext, encrypted.Ciphertext) {
		t.Fatal("existing ciphertext overwritten")
	}
}
