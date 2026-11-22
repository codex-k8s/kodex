package managedkubernetes

import (
	"encoding/hex"
	"net/url"

	"k8s.io/apimachinery/pkg/util/validation"
)

// DiscoveryKind selects a registered non-resource read, never a caller-supplied URL.
// DiscoveryKind выбирает registered non-resource read, не caller-supplied URL.
type DiscoveryKind string

const (
	// DiscoveryVersion reads public server version metadata at the selected target.
	// DiscoveryVersion читает public server version metadata выбранного target.
	DiscoveryVersion DiscoveryKind = "VERSION"
	// DiscoveryCoreVersions reads the core API version index.
	// DiscoveryCoreVersions читает индекс версий core API.
	DiscoveryCoreVersions DiscoveryKind = "CORE_VERSIONS"
	// DiscoveryGroups reads the named API group index.
	// DiscoveryGroups читает индекс именованных API groups.
	DiscoveryGroups DiscoveryKind = "GROUPS"
	// DiscoveryGroup reads one exact registered group without a version wildcard.
	// DiscoveryGroup читает одну exact registered group без version wildcard.
	DiscoveryGroup DiscoveryKind = "GROUP"
	// DiscoveryResources reads one exact core or named-group version's resource list.
	// DiscoveryResources читает resource list одной exact core либо named-group version.
	DiscoveryResources DiscoveryKind = "RESOURCES"
	// DiscoveryOpenAPIIndex reads the immutable-description index without following its URLs.
	// DiscoveryOpenAPIIndex читает immutable-description index без переходов по его URLs.
	DiscoveryOpenAPIIndex DiscoveryKind = "OPENAPI_V3_INDEX"
	// DiscoveryOpenAPISchema reads only an owner-pinned group/version and immutable hash.
	// DiscoveryOpenAPISchema читает только owner-pinned group/version и immutable hash.
	DiscoveryOpenAPISchema DiscoveryKind = "OPENAPI_V3_SCHEMA"
)

// DiscoveryAddress contains finite typed path coordinates. The schema hash is obtained from
// validated target discovery and pinned by the owner; a remote serverRelativeURL is never executed.
// DiscoveryAddress содержит finite typed path coordinates. Schema hash выводится из проверенного
// target discovery и закрепляется owner; remote serverRelativeURL никогда не исполняется.
type DiscoveryAddress struct {
	Kind                                DiscoveryKind
	Group, Version, ImmutableSchemaHash string
}

// RelativeURL constructs one closed API path without credentials, origin or arbitrary query.
// Non-resource RBAC uses its Path only; the schema hash narrows content, not authorization.
// RelativeURL строит один closed API path без credentials, origin либо arbitrary query.
// Non-resource RBAC использует только Path; schema hash ограничивает content, не authorization.
func (address DiscoveryAddress) RelativeURL() (*url.URL, error) {
	if address.Group != "" && len(validation.IsDNS1123Subdomain(address.Group)) != 0 {
		return nil, ErrInvalidResourceAddress
	}
	if address.Version != "" && len(validation.IsDNS1035Label(address.Version)) != 0 {
		return nil, ErrInvalidResourceAddress
	}
	result := &url.URL{}
	switch address.Kind {
	case DiscoveryVersion, DiscoveryCoreVersions, DiscoveryGroups, DiscoveryOpenAPIIndex:
		if address.Group != "" || address.Version != "" || address.ImmutableSchemaHash != "" {
			return nil, ErrInvalidResourceAddress
		}
		switch address.Kind {
		case DiscoveryVersion:
			result.Path = "/version"
		case DiscoveryCoreVersions:
			result.Path = "/api"
		case DiscoveryGroups:
			result.Path = "/apis"
		case DiscoveryOpenAPIIndex:
			result.Path = "/openapi/v3"
		}
	case DiscoveryGroup:
		if address.Group == "" || address.Version != "" || address.ImmutableSchemaHash != "" {
			return nil, ErrInvalidResourceAddress
		}
		result.Path = "/apis/" + address.Group
	case DiscoveryResources, DiscoveryOpenAPISchema:
		if address.Version == "" {
			return nil, ErrInvalidResourceAddress
		}
		result.Path = "/api/" + address.Version
		if address.Group != "" {
			result.Path = "/apis/" + address.Group + "/" + address.Version
		}
		if address.Kind == DiscoveryResources {
			if address.ImmutableSchemaHash != "" {
				return nil, ErrInvalidResourceAddress
			}
		} else {
			if len(address.ImmutableSchemaHash) != 64 && len(address.ImmutableSchemaHash) != 128 {
				return nil, ErrInvalidResourceAddress
			}
			if _, err := hex.DecodeString(address.ImmutableSchemaHash); err != nil {
				return nil, ErrInvalidResourceAddress
			}
			result.Path = "/openapi/v3" + result.Path
			result.RawQuery = url.Values{"hash": []string{address.ImmutableSchemaHash}}.Encode()
		}
	default:
		return nil, ErrInvalidResourceAddress
	}
	return result, nil
}
