package app

import "testing"

const (
	testKubernetesCAFile    = "/var/run/config/kubernetes.io/serviceaccount/ca.crt"
	testKubernetesTokenFile = "/var/run/secrets/tokens/kubernetes-api/token"
)

func TestSelectSecretBackendRequiresExactProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend string
		profile string
		want    secretBackend
		ok      bool
	}{
		{name: "обычный Vault", backend: "vault", profile: "production", want: secretBackendVault, ok: true},
		{name: "точный prototype", backend: "direct-production-kubernetes-file", profile: directProductionPrototypeProfile, want: secretBackendDirectProductionPrototype, ok: true},
		{name: "Vault запрещён в direct prototype", backend: "vault", profile: directProductionPrototypeProfile},
		{name: "prototype запрещён в другом профиле", backend: "direct-production-kubernetes-file", profile: "production"},
		{name: "неизвестный backend", backend: "kubernetes", profile: directProductionPrototypeProfile},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectSecretBackend(test.backend, test.profile)
			if test.ok && (err != nil || got != test.want) {
				t.Fatalf("selectSecretBackend() = %q, %v; want %q", got, err, test.want)
			}
			if !test.ok && err == nil {
				t.Fatalf("selectSecretBackend() accepted %q/%q", test.backend, test.profile)
			}
		})
	}
}

func TestValidatePrototypeKubernetesBoundaryRejectsAlternateTokenPath(t *testing.T) {
	t.Parallel()

	if err := validatePrototypeKubernetesBoundary(
		"https://kubernetes.default.svc:443",
		"kubernetes.default.svc",
		testKubernetesCAFile,
		testKubernetesTokenFile,
	); err != nil {
		t.Fatalf("exact Kubernetes boundary rejected: %v", err)
	}
	if err := validatePrototypeKubernetesBoundary(
		"https://kubernetes.default.svc:443",
		"kubernetes.default.svc",
		testKubernetesCAFile,
		"/var/run/secrets/kubernetes.io/serviceaccount/token",
	); err == nil {
		t.Fatal("alternate Kubernetes token path was accepted")
	}
}
