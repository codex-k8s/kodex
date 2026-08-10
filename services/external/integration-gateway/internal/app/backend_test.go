package app

import "testing"

func TestSelectBackendsRequiresExactProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		profile, secrets, oidc string
		ok                     bool
	}{
		{profile: "production", secrets: "vault", oidc: "network", ok: true},
		{profile: directProductionPrototypeProfile, secrets: "direct-production-kubernetes-file", oidc: "direct-production-file", ok: true},
		{profile: directProductionPrototypeProfile, secrets: "vault", oidc: "direct-production-file"},
		{profile: directProductionPrototypeProfile, secrets: "direct-production-kubernetes-file", oidc: "network"},
		{profile: "production", secrets: "direct-production-kubernetes-file", oidc: "network"},
		{profile: "production", secrets: "kubernetes", oidc: "network"},
	}
	for _, test := range tests {
		_, _, err := selectBackends(test.profile, test.secrets, test.oidc)
		if (err == nil) != test.ok {
			t.Fatalf("selectBackends(%q, %q, %q) error=%v, want ok=%v", test.profile, test.secrets, test.oidc, err, test.ok)
		}
	}
}
