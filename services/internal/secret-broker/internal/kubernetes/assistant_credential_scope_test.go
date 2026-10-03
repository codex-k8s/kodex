package kubernetes

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScopedAssistantProjectionMaterializesExactOwnerDescriptors(t *testing.T) {
	for _, scope := range []string{"SYSTEM", "PROJECT"} {
		t.Run(scope, func(t *testing.T) {
			ctx := t.Context()
			store, client := newCredentialProjectionTestStore(t)
			providerRaw := []byte(`{"OPENAI_API_KEY":"synthetic-scoped-provider-key","auth_mode":"apikey"}`)
			provider, err := store.CreateProviderCredential(ctx, "pauth_scope111", "pacc_projection1", providerRaw)
			if err != nil {
				t.Fatal(err)
			}
			effect, runtimeValue := testEffect("secop_scope111", 1, "sec_scope111", 1, "synthetic-scoped-secret")
			source, err := store.CreateImmutableForEffect(ctx, effect, runtimeValue)
			if err != nil {
				t.Fatal(err)
			}
			manifest := projectionManifest(provider, source)
			manifest.Authority.RPCProfile = transportprofile.TrustedCluster
			manifest.Authority.ProofJTI = ""
			manifest.Authority.SourceRevision = uint64(manifest.Generation)
			manifest.Authority.SourceDigestSHA256 = manifest.RuntimeRevisionDigest
			if scope == "SYSTEM" {
				manifest.Authority.ProjectID = ""
				manifest.Authority.CallerFullMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
			}
			projection, err := store.MaterializeRuntimeCredentialProjection(ctx, manifest)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := store.MaterializeRuntimeCredentialProjection(ctx, manifest)
			if err != nil || !sameCredentialProjection(projection, repeated) {
				t.Fatalf("scoped exact replay diverged: %v", err)
			}
			secret, err := client.CoreV1().Secrets(projectionNamespace).Get(ctx, projection.SecretName, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(secret.Data) != 2 || string(secret.Data[providerProjectionKey]) != string(providerRaw) || string(secret.Data["CRM_TOKEN"]) != string(runtimeValue) {
				clearSecretData(secret)
				t.Fatal("scoped projection lost exact source values")
			}
			clearSecretData(secret)
			for name, mutate := range map[string]func(*CredentialProjectionManifest){
				"cross method": func(m *CredentialProjectionManifest) {
					if scope == "SYSTEM" {
						m.Authority.CallerFullMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials"
					} else {
						m.Authority.CallerFullMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
					}
				},
				"wrong authority project": func(m *CredentialProjectionManifest) {
					if scope == "SYSTEM" {
						m.Authority.ProjectID = "e92277a1-c5d0-4d40-af73-54c34a256ef5"
					} else {
						m.Authority.ProjectID = ""
					}
				},
				"wrong namespace":               func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].Namespace = "foreign-runtime" },
				"wrong source name":             func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].SecretName = "foreign-runtime-secret" },
				"wrong source key":              func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].SecretKey = "foreign-key" },
				"wrong source ref":              func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].SecretRef = "sec_foreign111" },
				"wrong source revision":         func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].Revision++ },
				"wrong source uid":              func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].SecretUID += "foreign" },
				"wrong source resource version": func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].SecretResourceVersion += "foreign" },
				"wrong source digest":           func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].ContentSHA256 = stringsOfHex('f') },
				"invalid key name":              func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].Name = "../TOKEN" },
				"duplicate key": func(m *CredentialProjectionManifest) {
					m.RuntimeSecrets = append(m.RuntimeSecrets, m.RuntimeSecrets[0])
				},
				"provider key collision": func(m *CredentialProjectionManifest) { m.RuntimeSecrets[0].Name = providerProjectionKey },
			} {
				t.Run(name, func(t *testing.T) {
					changed := manifest
					changed.RuntimeSecrets = append([]RuntimeSecretProjectionBinding(nil), manifest.RuntimeSecrets...)
					mutate(&changed)
					if value, err := store.MaterializeRuntimeCredentialProjection(ctx, changed); err == nil || value.SecretName != "" {
						t.Fatalf("detached scoped descriptor was materialized: %v", err)
					}
				})
			}
			listed, err := store.ListRuntimeCredentialProjections(ctx)
			if err != nil || len(listed) != 1 || !sameCredentialProjection(listed[0], projection) {
				t.Fatalf("scoped projection recovery readback diverged: %v", err)
			}
			if err := store.DeleteRuntimeCredentialProjection(ctx, projection); err != nil {
				t.Fatal(err)
			}
			listed, err = store.ListRuntimeCredentialProjections(ctx)
			if err != nil || len(listed) != 0 {
				t.Fatalf("scoped projection delete failed absence readback: %v", err)
			}
			remaining, err := client.CoreV1().Secrets(projectionNamespace).Get(ctx, source.Name, metav1.GetOptions{})
			if err != nil || remaining.Type != corev1.SecretTypeOpaque {
				t.Fatalf("projection cleanup deleted its source: %v", err)
			}
			clearSecretData(remaining)
		})
	}
}
