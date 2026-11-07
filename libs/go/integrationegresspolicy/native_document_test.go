package integrationegresspolicy

import (
	"strings"
	"testing"
)

// TestNativeDocumentAdmitsOnlyExplicitOperatorPins keeps private unicast in the new opt-in
// document and rejects broad, ambiguous and special-purpose targets before rendering or dialing.
// TestNativeDocumentAdmitsOnlyExplicitOperatorPins допускает private unicast лишь в новом opt-in
// документе и отклоняет широкие, неоднозначные и специальные цели до render либо dial.
func TestNativeDocumentAdmitsOnlyExplicitOperatorPins(t *testing.T) {
	document := NativeDocument{Schema: NativeSchema, Generation: 1, GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []NativeDestination{{Hostname: "api.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32", "fd77::4/128"}}}}
	if document.Validate() != nil || len(document.SelectorValue()) > 63 {
		t.Fatal("explicit operator pins rejected")
	}
	for _, bad := range []string{"10.77.0.0/24", "0.0.0.0/0", "0.0.0.1/32", "127.0.0.1/32", "169.254.169.254/32", "224.0.0.1/32", "::/128", "::1/128", "fe80::1/128", "::ffff:10.77.0.4/128", "10.77.0.4", "10.77.0.4/032"} {
		changed := document
		changed.Destinations = []NativeDestination{{Hostname: "api.example.invalid", Port: 443, AddressCIDRs: []string{bad}}}
		if changed.Validate() == nil {
			t.Fatal("unsafe pin accepted", bad)
		}
	}
	for _, bad := range []string{"*.example.invalid", "https://api.example.invalid", "127.0.0.1", "api.example.invalid.", "API.example.invalid", "api.example.invalid/path", "api.example.invalid:443"} {
		changed := document
		changed.Destinations = []NativeDestination{{Hostname: bad, Port: 443, AddressCIDRs: []string{"10.77.0.4/32"}}}
		if changed.Validate() == nil {
			t.Fatal("unsafe hostname accepted", bad)
		}
	}
	changed := document
	changed.Destinations = []NativeDestination{document.Destinations[0], document.Destinations[0]}
	if changed.Validate() == nil {
		t.Fatal("duplicate host accepted")
	}
	changed = document
	changed.Destinations = []NativeDestination{{Hostname: "api.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32", "10.77.0.4/32"}}}
	if changed.Validate() == nil {
		t.Fatal("duplicate address accepted")
	}
	for _, missing := range []NativeDocument{{}, {Schema: NativeSchema, Generation: 1, GatewayPolicyDigest: strings.Repeat("a", 64)}} {
		if missing.Validate() == nil {
			t.Fatal("empty native profile opened a route")
		}
	}
}

// TestNativeDocumentDoesNotRelaxOpenAPIPublicPins proves the original public-DNS policy still
// rejects the same private address even when an independent operator native document accepts it.
// TestNativeDocumentDoesNotRelaxOpenAPIPublicPins доказывает, что прежняя public-DNS policy
// по-прежнему запрещает тот же private address, допустимый в независимом native документе.
func TestNativeDocumentDoesNotRelaxOpenAPIPublicPins(t *testing.T) {
	public := Document{Schema: Schema, Generation: 1, SourceDigest: SourceDigest([]string{"api.example.invalid"}), GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []Destination{{Hostname: "api.example.invalid", Port: 443, Addresses: []string{"10.77.0.4"}}}}
	if public.Validate() == nil {
		t.Fatal("native opt-in weakened public DNS validation")
	}
	native := NativeDocument{Schema: NativeSchema, Generation: 1, GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []NativeDestination{{Hostname: "api.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32"}}}}
	if native.Validate() != nil {
		t.Fatal("native exact private pin rejected")
	}
	before := native.Digest()
	native.Generation++
	if native.Digest() == before {
		t.Fatal("new generation retained old policy digest")
	}
}

// TestNativeDocumentCanonicalWireMatchesInstallerGolden fixes the cross-language field order
// and digest shared with the Python renderer, including IPv4/IPv6 operator pins.
// TestNativeDocumentCanonicalWireMatchesInstallerGolden закрепляет межъязыковой порядок
// полей и digest общего с Python renderer контракта, включая IPv4/IPv6 operator pins.
func TestNativeDocumentCanonicalWireMatchesInstallerGolden(t *testing.T) {
	document := NativeDocument{Schema: NativeSchema, Generation: 1, GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []NativeDestination{{Hostname: "gitlab.example.invalid", Port: 443, AddressCIDRs: []string{"10.77.0.4/32", "fd77::4/128"}}}}
	if document.Validate() != nil || document.Digest() != "a28747c10413d65f6618f38a15c06f2960f16d8e37f12ed1c5661a81df822c1f" {
		t.Fatal("native canonical wire changed without installer compatibility")
	}
}
