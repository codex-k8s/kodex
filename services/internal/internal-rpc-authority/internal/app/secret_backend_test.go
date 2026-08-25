package app

import "testing"

func TestSelectSecretBackendAcceptsOnlyKubernetes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend string
		want    secretBackend
		ok      bool
	}{
		{name: "Kubernetes", backend: "kubernetes", want: secretBackendKubernetes, ok: true},
		{name: "удалённый prototype", backend: "direct-production-kubernetes-file"},
		{name: "удалённый Vault", backend: "vault"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectSecretBackend(test.backend)
			if test.ok && (err != nil || got != test.want) {
				t.Fatalf("selectSecretBackend() = %q, %v; want %q", got, err, test.want)
			}
			if !test.ok && err == nil {
				t.Fatalf("selectSecretBackend() accepted %q", test.backend)
			}
		})
	}
}
