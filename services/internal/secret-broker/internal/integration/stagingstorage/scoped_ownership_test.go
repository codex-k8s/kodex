package stagingstorage

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScopedEncryptedMetadataRejectsCrossScopeReadAndDelete(t *testing.T) {
	for _, scope := range []runtimesecret.ScopeKind{runtimesecret.ScopeProject, runtimesecret.ScopeOrganization} {
		t.Run(string(scope), func(t *testing.T) {
			store, client, work, encrypted := storageFixture(t)
			work.Binding.ScopeKind = scope
			if scope == runtimesecret.ScopeOrganization {
				work.Binding.ProjectRef = ""
			}
			descriptor, err := store.Create(t.Context(), work, encrypted)
			if err != nil {
				t.Fatal(err)
			}
			for name, change := range map[string]func(*value.SecretDraftBinding){
				"foreign organization": func(b *value.SecretDraftBinding) { b.OrganizationRef = "org_foreign" },
				"cross scope": func(b *value.SecretDraftBinding) {
					if scope == runtimesecret.ScopeProject {
						b.ScopeKind = runtimesecret.ScopeOrganization
						b.ProjectRef = ""
					} else {
						b.ScopeKind = runtimesecret.ScopeProject
						b.ProjectRef = "prj_fixture"
					}
				},
			} {
				t.Run(name, func(t *testing.T) {
					foreign := work
					change(&foreign.Binding)
					client.ClearActions()
					if output, err := store.Read(t.Context(), foreign, descriptor); !errors.Is(err, secretdrafts.ErrConflict) || len(output.Ciphertext) != 0 {
						t.Fatal("foreign scope returned encrypted bytes")
					}
					if _, err := store.Lookup(t.Context(), foreign); !errors.Is(err, secretdrafts.ErrConflict) {
						t.Fatal("foreign scope returned exact descriptor")
					}
					if err := store.Delete(t.Context(), foreign, descriptor); !errors.Is(err, secretdrafts.ErrConflict) {
						t.Fatal("foreign scope deleted ciphertext")
					}
					for _, action := range client.Actions() {
						if action.GetVerb() != "get" {
							t.Fatal("foreign owner performed mutation")
						}
					}
				})
			}
			actual, err := client.CoreV1().Secrets(work.StagedNamespace).Get(t.Context(), work.StagedName, metav1.GetOptions{})
			if err != nil || string(actual.UID) != descriptor.UID || actual.ResourceVersion != descriptor.ResourceVersion {
				t.Fatal("foreign operations changed source")
			}
			var legacy map[string]any
			if err := json.Unmarshal([]byte(actual.Annotations[bindingAnnotation]), &legacy); err != nil {
				t.Fatal(err)
			}
			delete(legacy, "scope_kind")
			delete(legacy, "organization_ref")
			encoded, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			actual.Annotations[bindingAnnotation] = string(encoded)
			if _, err := client.CoreV1().Secrets(work.StagedNamespace).Update(t.Context(), actual, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Lookup(t.Context(), work); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("legacy V1 staging metadata accepted")
			}
		})
	}
}
