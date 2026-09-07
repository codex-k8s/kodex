package kubernetes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Оснастка использует настоящий producer, а не копию его labels и encoding.
// UID/RV назначает только fake Kubernetes: Python не имитирует их выдачу.
func bootstrapProviderFixture(t *testing.T) (*corev1.Secret, ProviderCredentialDescriptor) {
	t.Helper()
	producer, err := filepath.Abs("../../../../../tools/install/provider-bootstrap.py")
	if err != nil {
		t.Fatal("cannot locate bootstrap producer")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", "-B", "-c", `
import importlib.util, json, sys
spec = importlib.util.spec_from_file_location("provider_bootstrap", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
print(json.dumps(module.manifest("pacc_cleanup_Account9Z", "qa-bootstrap", sys.stdin.buffer.read())))
`, producer)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8"}
	command.Stdin = strings.NewReader(providerCleanupContent)
	output, err := command.Output()
	if err != nil {
		t.Fatal("bootstrap fixture producer failed")
	}
	defer clear(output)
	var secret corev1.Secret
	if json.Unmarshal(output, &secret) != nil {
		t.Fatal("bootstrap fixture is not a Kubernetes Secret")
	}
	if secret.Namespace != "kodex-runtime" {
		t.Fatal("bootstrap namespace does not match runtime")
	}
	secret.UID = types.UID("61000000-0000-4000-8000-000000000002")
	secret.ResourceVersion = "bootstrap-7"
	digest := sha256.Sum256([]byte(providerCleanupContent))
	return &secret, ProviderCredentialDescriptor{
		SecretName: secret.Name, SecretUID: string(secret.UID),
		SecretResourceVersion: secret.ResourceVersion, ContentSHA256: hex.EncodeToString(digest[:]),
	}
}

func TestBootstrapProviderCredentialExactProducerReadback(t *testing.T) {
	t.Parallel()
	secret, descriptor := bootstrapProviderFixture(t)
	defer clearSecretData(secret)
	store, client := newTestStore(t, secret)
	store.namespace = secret.Namespace
	for range 2 {
		value, err := store.ReadProviderCredentialExact(t.Context(), providerCleanupAccountRef, descriptor)
		if err != nil || !bytes.Equal(value, []byte(providerCleanupContent)) {
			clear(value)
			t.Fatal("canonical bootstrap exact readback failed")
		}
		// Очистка полученной копии не должна менять immutable объект или повторный read.
		clear(value)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" || action.GetNamespace() != secret.Namespace {
			t.Fatal("credential readback performed an unexpected action")
		}
	}
	stored, err := client.CoreV1().Secrets(secret.Namespace).Get(t.Context(), secret.Name, metav1.GetOptions{})
	if err != nil || !bytes.Equal(stored.Data[providerAuthJSONKey], []byte(providerCleanupContent)) {
		t.Fatal("readback modified the stored credential")
	}
	clearSecretData(stored)
}

func TestBootstrapProviderCredentialExactRejectsInvalidMaterialization(t *testing.T) {
	t.Parallel()
	canonical, descriptor := bootstrapProviderFixture(t)
	defer clearSecretData(canonical)
	cases := []struct {
		name   string
		change func(*corev1.Secret)
	}{
		{"digest LF", func(s *corev1.Secret) { s.Data[providerAuthSHA256Key] = append(s.Data[providerAuthSHA256Key], '\n') }},
		{"legacy publisher", func(s *corev1.Secret) { s.Labels[providerManagedByLabel] = "kodex-local-dev" }},
		{"installer publisher", func(s *corev1.Secret) { s.Labels[providerManagedByLabel] = "kodex-install" }},
		{"foreign publisher", func(s *corev1.Secret) { s.Labels[providerManagedByLabel] = "other-controller" }},
		{"missing publisher", func(s *corev1.Secret) { delete(s.Labels, providerManagedByLabel) }},
		{"wrong bootstrap version", func(s *corev1.Secret) { s.Labels[providerBootstrapLabel] = "v2" }},
		{"missing bootstrap version", func(s *corev1.Secret) { delete(s.Labels, providerBootstrapLabel) }},
		{"wrong part of", func(s *corev1.Secret) { s.Labels[providerPartOfLabel] = "other" }},
		{"wrong account", func(s *corev1.Secret) { s.Annotations[providerAccountRefAnnotation] = "pacc_otherAccount9" }},
		{"missing account", func(s *corev1.Secret) { delete(s.Annotations, providerAccountRefAnnotation) }},
		{"wrong digest annotation", func(s *corev1.Secret) { s.Annotations[providerContentSHAAnnotation] = strings.Repeat("a", 64) }},
		{"missing digest annotation", func(s *corev1.Secret) { delete(s.Annotations, providerContentSHAAnnotation) }},
		{"changed auth bytes", func(s *corev1.Secret) {
			s.Data[providerAuthJSONKey] = []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"other-synthetic"}`)
		}},
		{"wrong UID", func(s *corev1.Secret) { s.UID = types.UID("61000000-0000-4000-8000-000000000003") }},
		{"missing UID", func(s *corev1.Secret) { s.UID = "" }},
		{"wrong resource version", func(s *corev1.Secret) { s.ResourceVersion = "bootstrap-8" }},
		{"missing resource version", func(s *corev1.Secret) { s.ResourceVersion = "" }},
		{"additional data key", func(s *corev1.Secret) { s.Data["unexpected"] = []byte("synthetic") }},
		{"missing digest key", func(s *corev1.Secret) { delete(s.Data, providerAuthSHA256Key) }},
		{"mutable", func(s *corev1.Secret) { *s.Immutable = false }},
		{"missing immutable", func(s *corev1.Secret) { s.Immutable = nil }},
		{"wrong type", func(s *corev1.Secret) { s.Type = corev1.SecretTypeTLS }},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			secret := canonical.DeepCopy()
			defer clearSecretData(secret)
			scenario.change(secret)
			store, client := newTestStore(t, secret)
			store.namespace = secret.Namespace
			value, err := store.ReadProviderCredentialExact(t.Context(), providerCleanupAccountRef, descriptor)
			defer clear(value)
			if !errors.Is(err, ErrProviderCredentialConflict) || len(value) != 0 {
				t.Fatal("invalid bootstrap credential did not fail closed")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("rejection mutated the credential")
				}
			}
		})
	}
}

func TestBootstrapProviderCredentialExactRejectsWrongRequestedPins(t *testing.T) {
	t.Parallel()
	secret, descriptor := bootstrapProviderFixture(t)
	defer clearSecretData(secret)
	store, _ := newTestStore(t, secret)
	store.namespace = secret.Namespace
	for _, field := range []string{"account", "UID", "resourceVersion", "digest", "name"} {
		t.Run(field, func(t *testing.T) {
			wanted, account := descriptor, providerCleanupAccountRef
			switch field {
			case "account":
				account = "pacc_otherAccount9"
			case "UID":
				wanted.SecretUID = "61000000-0000-4000-8000-000000000003"
			case "resourceVersion":
				wanted.SecretResourceVersion = "bootstrap-8"
			case "digest":
				wanted.ContentSHA256 = strings.Repeat("b", 64)
			case "name":
				wanted.SecretName = "provider-bootstrap-absent"
			}
			value, err := store.ReadProviderCredentialExact(t.Context(), account, wanted)
			defer clear(value)
			if !errors.Is(err, ErrProviderCredentialConflict) || len(value) != 0 {
				t.Fatal("wrong requested credential pins did not fail closed")
			}
		})
	}
}

func TestBootstrapProviderCredentialCleanupExactTarget(t *testing.T) {
	t.Parallel()
	canonical, descriptor := bootstrapProviderFixture(t)
	defer clearSecretData(canonical)
	if canonical.Labels[providerCredentialLabel] != "" {
		t.Fatal("bootstrap must not claim broker credential discovery provenance")
	}
	for _, variant := range []string{"exact", "wrong UID", "wrong resourceVersion"} {
		t.Run(variant, func(t *testing.T) {
			secret := canonical.DeepCopy()
			defer clearSecretData(secret)
			store, client := newTestStore(t, secret)
			store.namespace = secret.Namespace
			wanted := descriptor
			if variant == "wrong UID" {
				wanted.SecretUID = "61000000-0000-4000-8000-000000000003"
			}
			if variant == "wrong resourceVersion" {
				wanted.SecretResourceVersion = "bootstrap-8"
			}
			receipt, err := store.CleanupProviderCredential(t.Context(), providerCleanupTaskRef,
				providerCleanupAccountRef, providerCleanupGeneration, wanted)
			if variant == "exact" {
				if err != nil || receipt.TerminalReceipt == "" || receipt.ProducedCredential != nil {
					t.Fatal("exact bootstrap cleanup did not produce a terminal receipt")
				}
				assertProviderSecretDeletePreconditions(t, client, descriptor.SecretName, descriptor.SecretUID, descriptor.SecretResourceVersion)
				fence, getErr := client.CoreV1().Secrets(secret.Namespace).Get(t.Context(), secret.Name, metav1.GetOptions{})
				if getErr != nil || len(fence.Data[providerAuthJSONKey]) != 0 {
					t.Fatal("cleanup retained credential material")
				}
				if _, parseErr := store.parseProviderCleanupFence(fence, "CREDENTIAL", providerCleanupAccountRef); parseErr != nil {
					t.Fatal("cleanup did not fence the bootstrap name")
				}
				return
			}
			if providerSecretActionCount(client, "delete", secret.Name) != 0 {
				t.Fatal("stale cleanup deleted bootstrap credential")
			}
			if variant == "wrong resourceVersion" && !errors.Is(err, ErrProviderCredentialCleanupConflict) {
				t.Fatal("wrong resourceVersion did not conflict")
			}
			// Смена UID сообщает владельцу отдельную replacement revision, не удаляет её.
			if variant == "wrong UID" && (err != nil || receipt.ProducedCredential == nil || *receipt.ProducedCredential != descriptor) {
				t.Fatal("replacement descriptor was not preserved")
			}
			value, readErr := store.ReadProviderCredentialExact(t.Context(), providerCleanupAccountRef, descriptor)
			defer clear(value)
			if readErr != nil || !bytes.Equal(value, []byte(providerCleanupContent)) {
				t.Fatal("stale cleanup changed the current credential")
			}
		})
	}
}

func TestBootstrapProviderCredentialExactPreservesCanonicalPublishers(t *testing.T) {
	t.Parallel()
	for _, manager := range []string{providerSecretBrokerManager, providerRuntimeManager} {
		t.Run(manager, func(t *testing.T) {
			secret, descriptor := providerCleanupFixture(manager)
			defer clearSecretData(secret)
			store, _ := newTestStore(t, secret)
			value, err := store.ReadProviderCredentialExact(t.Context(), providerCleanupAccountRef, descriptor)
			defer clear(value)
			if err != nil || !bytes.Equal(value, []byte(providerCleanupContent)) {
				t.Fatal("existing canonical publisher was rejected")
			}
			wrong := descriptor
			wrong.SecretResourceVersion = "different-version"
			denied, err := store.ReadProviderCredentialExact(t.Context(), providerCleanupAccountRef, wrong)
			defer clear(denied)
			if !errors.Is(err, ErrProviderCredentialConflict) || len(denied) != 0 {
				t.Fatal("existing publisher bypassed exact descriptor checks")
			}
		})
	}
}
