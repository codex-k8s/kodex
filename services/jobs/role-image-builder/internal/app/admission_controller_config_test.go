package app

import "testing"

func TestAdmissionControllerConfigRejectsImplicitOrUnknownRPCProfile(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, test := range []struct {
		name    string
		profile string
		valid   bool
	}{
		{name: "protected", profile: "", valid: true},
		{name: "trusted cluster", profile: "trusted-cluster", valid: true},
		{name: "unknown", profile: "insecure", valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("KODEX_RPC_PROFILE", test.profile)
			_, err := loadAdmissionControllerConfig()
			if (err == nil) != test.valid {
				t.Fatalf("RPC profile %q validity mismatch: %v", test.profile, err)
			}
		})
	}
}
