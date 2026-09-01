package runtimesecret

import "testing"

func TestVersionedKubernetesName(t *testing.T) {
	name, err := VersionedKubernetesName("sec_12345678", 17)
	if err != nil {
		t.Fatalf("build name: %v", err)
	}
	if name != "runtime-secret-d8a967ce972fd72a-r17" {
		t.Fatalf("name = %q", name)
	}
	if _, err := VersionedKubernetesName("other", 1); err == nil {
		t.Fatal("invalid reference was accepted")
	}
	if _, err := VersionedKubernetesName("sec_12345678", 0); err == nil {
		t.Fatal("invalid revision was accepted")
	}
}
