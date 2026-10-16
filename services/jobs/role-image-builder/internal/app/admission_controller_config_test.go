package app

import "testing"

func TestAdmissionControllerConfigRejectsImplicitOrUnknownRPCProfile(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("POD_UID", "11111111-1111-4111-8111-111111111111")
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

func TestAdmissionControllerRequiresDownwardAPIPodUID(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, identity := range []string{"", "pod-name", "../foreign", "11111111111141118111111111111111"} {
		t.Setenv("POD_UID", identity)
		if _, err := loadAdmissionControllerConfig(); err == nil {
			t.Fatal("invalid Pod UID accepted")
		}
	}
}
