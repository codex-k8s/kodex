package platform

import "testing"

// TestWarmSpecificationFingerprint excludes runtime revision identity from the warm specification
// fingerprint while binding the listed content, model, credential, environment and session
// dependencies without mutating the source.
// TestWarmSpecificationFingerprint исключает идентичность runtime-ревизии из отпечатка тёплой
// спецификации, привязывая перечисленные зависимости содержимого, модели, credential, окружения и
// сессии без изменения источника.
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
