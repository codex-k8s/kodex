package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTelegramProfileStaysIsolatedFromCore proves the registered listener grants only fixed Bot API reachability and leaves existing core destinations unchanged.
// TestTelegramProfileStaysIsolatedFromCore доказывает доступ registered listener лишь к fixed Bot API и сохранение прежнего набора core destinations.
func TestTelegramProfileStaysIsolatedFromCore(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "egress-gateway", "telegram-policy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	active, err := Load(raw, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := active.ForProfile(TelegramProfileName)
	if err != nil || !profile.Allows("api.telegram.org", 443) || profile.Allows("api.github.com", 443) || profile.Allows("api.telegram.org", 80) || active.Allows("api.telegram.org", 443) {
		t.Fatal("Telegram profile expanded core or provider scope", err)
	}
	name, workload, operation := profile.ProfileIdentity()
	if name != TelegramProfileName || workload != TelegramWorkload || operation != TelegramOperation || profile.Digest() != active.Digest() {
		t.Fatal("Telegram profile lost server identity or digest binding")
	}
	for _, mutation := range []func(*Document){
		func(value *Document) { value.Spec.Profiles[1].Destinations[0].Hostname = "evil.example.test" },
		func(value *Document) { value.Spec.Profiles[1].Workload = "integration-gateway" },
		func(value *Document) { value.Spec.Profiles[1].Operation = "telegram.arbitrary" },
		func(value *Document) {
			value.Spec.Profiles[1].Destinations = append(value.Spec.Profiles[1].Destinations, Destination{Hostname: "api.github.com", Port: 443})
		},
		func(value *Document) { value.Spec.Profiles[1] = value.Spec.Profiles[0] },
		func(value *Document) {
			value.Spec.Destinations = append(value.Spec.Destinations, Destination{Hostname: "api.telegram.org", Port: 443})
		},
	} {
		var changed Document
		if json.Unmarshal(raw, &changed) != nil {
			t.Fatal("decode canonical profile")
		}
		mutation(&changed)
		candidate, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := parseAndDigest(candidate); err == nil {
			t.Fatal("Telegram profile accepted a scope expansion")
		}
	}
}
