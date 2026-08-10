package app

import "testing"

func TestSelectCredentialBackendRequiresExactProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile, backend string
		ok               bool
	}{
		{profile: "production", backend: "vault", ok: true},
		{profile: directProductionPrototypeProfile, backend: "direct-production-kubernetes-file", ok: true},
		{profile: directProductionPrototypeProfile, backend: "vault"},
		{profile: "production", backend: "direct-production-kubernetes-file"},
		{profile: directProductionPrototypeProfile, backend: "kubernetes"},
	}
	for _, test := range tests {
		_, err := selectCredentialBackend(test.profile, test.backend)
		if (err == nil) != test.ok {
			t.Fatalf("selectCredentialBackend(%q, %q) error=%v, want ok=%v", test.profile, test.backend, err, test.ok)
		}
	}
}
