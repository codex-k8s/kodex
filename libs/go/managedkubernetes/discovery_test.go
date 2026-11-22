package managedkubernetes

import (
	"strings"
	"testing"
)

// TestDiscoveryAddressUsesClosedNonResourcePaths rejects caller URLs and mixed coordinates.
// It checks the actual constructed path/query, independently of resource-address formatting.
// TestDiscoveryAddressUsesClosedNonResourcePaths отвергает caller URLs и mixed coordinates.
// Проверяет actual path/query независимо от форматирования resource address.
func TestDiscoveryAddressUsesClosedNonResourcePaths(t *testing.T) {
	for _, test := range []struct {
		address DiscoveryAddress
		path    string
	}{
		{DiscoveryAddress{Kind: DiscoveryVersion}, "/version"},
		{DiscoveryAddress{Kind: DiscoveryCoreVersions}, "/api"},
		{DiscoveryAddress{Kind: DiscoveryGroups}, "/apis"},
		{DiscoveryAddress{Kind: DiscoveryGroup, Group: "apps"}, "/apis/apps"},
		{DiscoveryAddress{Kind: DiscoveryResources, Version: "v1"}, "/api/v1"},
		{DiscoveryAddress{Kind: DiscoveryResources, Group: "apps", Version: "v1"}, "/apis/apps/v1"},
		{DiscoveryAddress{Kind: DiscoveryOpenAPIIndex}, "/openapi/v3"},
		{DiscoveryAddress{Kind: DiscoveryOpenAPISchema, Version: "v1", ImmutableSchemaHash: strings.Repeat("a", 128)}, "/openapi/v3/api/v1"},
		{DiscoveryAddress{Kind: DiscoveryOpenAPISchema, Group: "apps", Version: "v1", ImmutableSchemaHash: strings.Repeat("b", 64)}, "/openapi/v3/apis/apps/v1"},
	} {
		location, err := test.address.RelativeURL()
		if err != nil || location.Path != test.path || location.IsAbs() || location.Host != "" || location.User != nil || location.Fragment != "" {
			t.Fatal("registered discovery address changed path or authority")
		}
		if test.address.ImmutableSchemaHash == "" && location.RawQuery != "" || test.address.ImmutableSchemaHash != "" &&
			(location.Query().Get("hash") != test.address.ImmutableSchemaHash || len(location.Query()) != 1) {
			t.Fatal("discovery address gained arbitrary query or lost immutable hash")
		}
	}
	for _, address := range []DiscoveryAddress{
		{}, {Kind: "RAW"}, {Kind: DiscoveryVersion, Group: "apps"}, {Kind: DiscoveryVersion, Version: "v1"},
		{Kind: DiscoveryGroups, ImmutableSchemaHash: strings.Repeat("a", 128)},
		{Kind: DiscoveryGroup}, {Kind: DiscoveryGroup, Group: "apps", Version: "v1"},
		{Kind: DiscoveryResources}, {Kind: DiscoveryResources, Group: "apps/../secrets", Version: "v1"},
		{Kind: DiscoveryResources, Group: "apps", Version: "v1?raw=true"},
		{Kind: DiscoveryResources, Version: "v1", ImmutableSchemaHash: strings.Repeat("a", 128)},
		{Kind: DiscoveryOpenAPISchema, Version: "v1"},
		{Kind: DiscoveryOpenAPISchema, Version: "v1", ImmutableSchemaHash: strings.Repeat("a", 65)},
		{Kind: DiscoveryOpenAPISchema, Version: "v1", ImmutableSchemaHash: strings.Repeat("z", 128)},
		{Kind: DiscoveryOpenAPISchema, Version: "v1", ImmutableSchemaHash: "https://foreign.invalid/schema"},
	} {
		if _, err := address.RelativeURL(); err == nil {
			t.Fatal("unknown or ambiguous discovery location accepted")
		}
	}
}
