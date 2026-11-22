package managedkubernetes

import (
	"strings"
	"testing"
)

// TestKubeconfigSourceOriginsAreExactMetadata rejects alternate authorities and URL features.
// It performs no network access and grants no trust to an otherwise well-formed alias.
// TestKubeconfigSourceOriginsAreExactMetadata отклоняет alternate authorities и URL features.
// Сетевых обращений нет; корректная форма alias сама не даёт trust.
func TestKubeconfigSourceOriginsAreExactMetadata(t *testing.T) {
	for _, valid := range []string{"https://127.0.0.1:40123", "https://[::1]:40123", "https://0.0.0.0:40123", "https://[::]:40123", "https://api.synthetic.invalid:6443"} {
		if !ValidKubeconfigSourceOrigins([]string{valid}) || !AllowsKubeconfigSourceOrigin([]string{valid}, valid) {
			t.Fatal("explicit canonical source alias was rejected")
		}
	}
	for _, invalid := range []string{"http://127.0.0.1:40123", "https://127.0.0.1:040123", "https://user@127.0.0.1:40123",
		"https://127.0.0.1:40123/", "https://127.0.0.1:40123?", "https://127.0.0.1:40123#", "https://127.0.0.1:",
		"https://api.synthetic.invalid/path", "https://bad_host.invalid", "https://API.synthetic.invalid",
		"https://" + strings.Repeat("a", 64) + ".invalid", "https://[::ffff:127.0.0.1]:40123"} {
		if ValidKubeconfigSourceOrigins([]string{invalid}) {
			t.Fatal("ambiguous source coordinate crossed the operator boundary")
		}
	}
	alias := "https://127.0.0.1:40123"
	if ValidKubeconfigSourceOrigins(nil) || ValidKubeconfigSourceOrigins([]string{alias, alias}) ||
		AllowsKubeconfigSourceOrigin([]string{alias}, "https://127.0.0.1:40124") {
		t.Fatal("missing, duplicate or unregistered source alias was inferred")
	}
}
