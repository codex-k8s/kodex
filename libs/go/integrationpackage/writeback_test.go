package integrationpackage

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestGitHubWriteBackBoundsAndManagedNetworkNarrowing(t *testing.T) {
	definitions, err := LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	baseline := definitions["github"]
	git := NetworkDestination{Key: "github_git", Source: "STATIC", Hostname: "github.com", Port: 443, TLS: "REQUIRED"}
	if !baseline.HasNetworkDestination(git) {
		t.Fatal("shipped Git destination missing")
	}
	capability, ok := baseline.Capability("github.repository.content.update")
	if !ok {
		t.Fatal("update capability missing")
	}
	input := func(size int) []byte {
		raw, err := json.Marshal(map[string]any{"path": "configuration.yaml", "branch": "kodex/writeback/test", "message": "Update", "sha": strings.Repeat("a", 40), "content_base64": base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", size)))})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if _, err := capability.ValidateInput(input(256 << 10)); err != nil {
		t.Fatalf("256 KiB raw content: %v", err)
	}
	if _, err := capability.ValidateInput(input((256 << 10) + 3)); err == nil {
		t.Fatal("oversize base64 accepted")
	}
	if _, err := decodeJSONObject([]byte(`{"value":"` + strings.Repeat("x", 512<<10) + `"}`)); err == nil {
		t.Fatal("oversize carrier accepted")
	}
	old := baseline
	old.Metadata.Origin = OriginGit
	old.Spec.NetworkDestinations = append([]NetworkDestination(nil), baseline.Spec.NetworkDestinations[:1]...)
	old.Spec.Capabilities = append([]Capability(nil), baseline.Spec.Capabilities...)
	for i := range old.Spec.Capabilities {
		if old.Spec.Capabilities[i].Key != capability.Key {
			continue
		}
		old.Spec.Capabilities[i].InputFields = append([]Field(nil), capability.InputFields...)
		for j := range old.Spec.Capabilities[i].InputFields {
			if old.Spec.Capabilities[i].InputFields[j].Key == "content_base64" {
				old.Spec.Capabilities[i].InputFields[j].MaximumLength = 32768
			}
		}
	}
	old = reparseManaged(t, old)
	if err := ValidateExecutableRevision(old, baseline); err != nil {
		t.Fatalf("old API-only narrowing: %v", err)
	}
	if old.HasNetworkDestination(git) {
		t.Fatal("API-only package acquired Git destination")
	}
	oldUpdate, _ := old.Capability(capability.Key)
	if _, err := oldUpdate.ValidateInput(input(256 << 10)); err == nil {
		t.Fatal("managed field narrowing bypassed")
	}
	for _, mutate := range []func(*NetworkDestination){
		func(d *NetworkDestination) { d.Hostname = "other.example" },
		func(d *NetworkDestination) { d.Port = 8443 },
		func(d *NetworkDestination) { d.TLS = "NONE" },
		func(d *NetworkDestination) { d.Key = "other" },
	} {
		changed := old.Spec.NetworkDestinations[0]
		mutate(&changed)
		if narrowNetworkDestinations([]NetworkDestination{changed}, baseline.Spec.NetworkDestinations) {
			t.Fatal("altered tuple accepted")
		}
	}
}
