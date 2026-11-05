package value

import "testing"

// TestOIDCIdentityPolicyPinsIssuerAndOwner rejects ambiguous policy inputs and keeps canonical owner ordering independent of authority digest.
// TestOIDCIdentityPolicyPinsIssuerAndOwner отклоняет неоднозначные policy inputs и исключает порядок owners из authority digest.
func TestOIDCIdentityPolicyPinsIssuerAndOwner(t *testing.T) {
	policy := OIDCIdentityPolicy{Schema: "kodex.oidc-identity-policy/v1", Revision: 1, Issuer: "https://identity.example.invalid/realm/synthetic",
		BootstrapSubjects: []string{"20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002"}, SessionLifetimeSeconds: 3600}
	digest, err := policy.Digest()
	if err != nil || len(digest) != 64 || !policy.BootstrapOwner(policy.BootstrapSubjects[0]) || policy.BootstrapOwner("arbitrary-first-user") {
		t.Fatal("explicit bootstrap identity policy was not accepted")
	}
	policy.BootstrapSubjects[0], policy.BootstrapSubjects[1] = policy.BootstrapSubjects[1], policy.BootstrapSubjects[0]
	if repeated, err := policy.Digest(); err != nil || repeated != digest {
		t.Fatal("owner ordering changed canonical authority")
	}
	for _, scenario := range []string{"http", "query", "userinfo", "no-owner", "duplicate-owner", "bad-owner", "revision", "lifetime", "schema"} {
		t.Run(scenario, func(t *testing.T) {
			invalid := policy
			switch scenario {
			case "http":
				invalid.Issuer = "http://identity.example.invalid"
			case "query":
				invalid.Issuer += "?select=other"
			case "userinfo":
				invalid.Issuer = "https://user@identity.example.invalid"
			case "no-owner":
				invalid.BootstrapSubjects = nil
			case "duplicate-owner":
				invalid.BootstrapSubjects = []string{policy.BootstrapSubjects[0], policy.BootstrapSubjects[0]}
			case "bad-owner":
				invalid.BootstrapSubjects = []string{"first-login"}
			case "revision":
				invalid.Revision = 0
			case "lifetime":
				invalid.SessionLifetimeSeconds = 86400
			case "schema":
				invalid.Schema = "unknown"
			}
			if invalid.Validate() == nil {
				t.Fatal("invalid authority policy was accepted")
			}
		})
	}
}
