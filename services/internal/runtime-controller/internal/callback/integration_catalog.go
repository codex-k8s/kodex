package callback

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const maximumIntegrationCatalogPage = 8

func integrationCatalogTool() map[string]any {
	return map[string]any{
		"name":        "get_integration_catalog",
		"description": "Discover only integration grants bound to this RuntimeRevision. Use query and offset for a compact index. Supply exact connection_ref and capability_key together to read one input schema before invoke_integration. Names are display data, not authority.",
		"inputSchema": objectSchema(nil, map[string]any{
			"query": stringSchema(0, 80), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 256},
			"connection_ref": opaqueRefSchema(), "capability_key": stringSchema(1, 255),
		}),
		"outputSchema": objectSchema([]string{"grants"}, map[string]any{
			"grants":      map[string]any{"type": "array", "maxItems": maximumIntegrationCatalogPage, "items": map[string]any{"type": "object"}},
			"next_offset": map[string]any{"type": "integer", "minimum": 1, "maximum": 256},
		}),
	}
}

func integrationCatalog(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if len(input.IntegrationGrants) == 0 || !onlyKeys(arguments, "query", "offset", "connection_ref", "capability_key") {
		return nil, errors.New("integration catalog is not available")
	}
	query, _ := arguments["query"].(string)
	if raw, exists := arguments["query"]; exists {
		if _, ok := raw.(string); !ok || utf8.RuneCountInString(query) > 80 {
			return nil, errors.New("integration catalog query is invalid")
		}
	}
	query = strings.ToLower(strings.TrimSpace(query))
	offset := 0
	if raw, exists := arguments["offset"]; exists {
		switch value := raw.(type) {
		case int:
			offset = value
		case float64:
			if value != float64(int(value)) {
				return nil, errors.New("integration catalog offset is invalid")
			}
			offset = int(value)
		default:
			return nil, errors.New("integration catalog offset is invalid")
		}
		if offset < 0 || offset > 256 {
			return nil, errors.New("integration catalog offset is invalid")
		}
	}
	connection, connectionValid := arguments["connection_ref"].(string)
	capability, capabilityValid := arguments["capability_key"].(string)
	_, connectionSelected := arguments["connection_ref"]
	_, capabilitySelected := arguments["capability_key"]
	if connectionSelected && !connectionValid || capabilitySelected && !capabilityValid {
		return nil, errors.New("integration catalog selection is invalid")
	}
	if connectionSelected != capabilitySelected || connectionSelected && (connection == "" || capability == "" || query != "" || offset != 0) {
		return nil, errors.New("integration catalog selection is invalid")
	}
	grants := append([]runtimecontract.RunnerIntegrationGrant(nil), input.IntegrationGrants...)
	sort.Slice(grants, func(left, right int) bool {
		if grants[left].ConnectionName == grants[right].ConnectionName {
			if grants[left].CapabilityKey == grants[right].CapabilityKey {
				return grants[left].ConnectionRef < grants[right].ConnectionRef
			}
			return grants[left].CapabilityKey < grants[right].CapabilityKey
		}
		return grants[left].ConnectionName < grants[right].ConnectionName
	})
	entries := make([]map[string]any, 0, maximumIntegrationCatalogPage)
	if connectionSelected {
		for _, grant := range grants {
			if grant.ConnectionRef != connection || grant.CapabilityKey != capability {
				continue
			}
			var schema map[string]any
			if json.Unmarshal([]byte(grant.InputSchema), &schema) != nil {
				return nil, errors.New("integration catalog schema is invalid")
			}
			entry := integrationCatalogEntry(grant)
			entry["input_schema"] = schema
			return map[string]any{"grants": []map[string]any{entry}}, nil
		}
		return nil, errors.New("integration catalog selection is invalid")
	}
	matching := make([]runtimecontract.RunnerIntegrationGrant, 0, len(grants))
	for _, grant := range grants {
		if query != "" && !strings.Contains(strings.ToLower(grant.ConnectionName+" "+grant.CapabilityName+" "+grant.DefinitionKey+" "+grant.CapabilityKey), query) {
			continue
		}
		matching = append(matching, grant)
	}
	end := offset + maximumIntegrationCatalogPage
	if end > len(matching) {
		end = len(matching)
	}
	if offset < len(matching) {
		for _, grant := range matching[offset:end] {
			entries = append(entries, integrationCatalogEntry(grant))
		}
	}
	result := map[string]any{"grants": entries}
	if end < len(matching) {
		result["next_offset"] = end
	}
	return result, nil
}

func integrationCatalogEntry(grant runtimecontract.RunnerIntegrationGrant) map[string]any {
	return map[string]any{
		"connection_ref": grant.ConnectionRef, "connection_name": grant.ConnectionName,
		"definition_key": grant.DefinitionKey, "definition_version": grant.DefinitionVersion,
		"definition_digest": grant.DefinitionDigest, "capability_key": grant.CapabilityKey,
		"capability_name": grant.CapabilityName, "capability_description": truncateRunes(grant.CapabilityDescription, 500),
		"risk": grant.Risk, "operation": grant.Operation, "input_schema_sha256": grant.InputSchemaSHA256,
	}
}
