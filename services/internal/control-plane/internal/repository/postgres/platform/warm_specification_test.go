package platform

import "testing"

func TestWarmSpecificationFingerprint(t *testing.T) {
	base := map[string]any{"runtimeRevisionRef": "rrev_original", "runtimeRevisionVersion": int64(71),
		"providerSecretName": "secret", "providerSecretUID": "uid", "providerSecretResourceVersion": "1"}
	before, err := warmSpecificationDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if base["runtimeRevisionRef"] != "rrev_original" || base["runtimeRevisionVersion"] != int64(71) {
		t.Fatal("historical snapshot changed")
	}
	base["runtimeRevisionRef"], base["runtimeRevisionVersion"] = "rrev_lifecycle", int64(999)
	if after, err := warmSpecificationDigest(base); err != nil || after != before {
		t.Fatal("revision identity entered specification fingerprint")
	}
	for _, field := range []string{"instructions", "imageReference", "runtimeModel", "providerAccountRef", "providerCredentialSHA256", "providerSecretResourceVersion", "configOverlay", "runtimeEnvironmentDigest", "environmentBindingDigest", "sessionRef"} {
		candidate := make(map[string]any)
		for key, value := range base {
			candidate[key] = value
		}
		candidate[field] = "changed"
		if after, err := warmSpecificationDigest(candidate); err != nil || after == before {
			t.Fatalf("dependency omitted: %s", field)
		}
	}
}
