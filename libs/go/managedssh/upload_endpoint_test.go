package managedssh

import "testing"

// TestIdentityUploadEndpointCanonicalOrigins pins one shared CP/broker origin contract, including IPv6 and rejection of ambiguous empty/default port spellings.
// TestIdentityUploadEndpointCanonicalOrigins закрепляет один CP/broker origin contract, включая IPv6 и отказ ambiguous empty/default port spellings.
func TestIdentityUploadEndpointCanonicalOrigins(t *testing.T) {
	for _, endpoint := range []IdentityUploadEndpoint{{Origin: "https://broker.example.invalid", TLSServerName: "broker.example.invalid"}, {Origin: "https://broker.example.invalid:18443", TLSServerName: "broker.example.invalid"}, {Origin: "https://192.0.2.1:18443", TLSServerName: "192.0.2.1"}, {Origin: "https://[2001:db8::1]:18443", TLSServerName: "2001:db8::1"}} {
		if !endpoint.Valid() {
			t.Fatal("canonical origin rejected")
		}
	}
	for _, origin := range []string{"https://broker.example.invalid:", "https://broker.example.invalid:443", "https://broker.example.invalid:00443", "https://broker.example.invalid:0", "https://broker.example.invalid:65536", "https://broker.example.invalid/", "https://broker.example.invalid?", "https://user@broker.example.invalid", "http://broker.example.invalid", "https://BROKER.example.invalid", "https://broker.example.invalid/path", "https://broker.example.invalid#fragment"} {
		if (IdentityUploadEndpoint{Origin: origin, TLSServerName: "broker.example.invalid"}).Valid() {
			t.Fatal("noncanonical origin admitted")
		}
	}
	if (IdentityUploadEndpoint{Origin: "https://[2001:db8::1]:", TLSServerName: "2001:db8::1"}).Valid() {
		t.Fatal("empty IPv6 port admitted")
	}
}
