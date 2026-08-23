package app

import "testing"

func TestSelectSecretBackendAcceptsOnlyVault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend string
		want    secretBackend
		ok      bool
	}{
		{name: "Vault", backend: "vault", want: secretBackendVault, ok: true},
		{name: "удалённый prototype", backend: "direct-production-kubernetes-file"},
		{name: "неизвестный backend", backend: "kubernetes"},
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
