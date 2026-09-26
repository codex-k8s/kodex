package callback

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maximumIntegrationCatalogPage = 8

func integrationCatalogTool() map[string]any {
	inputSchema := objectSchema(nil, map[string]any{
		"query": stringSchema(0, 80), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 256},
		"connection_ref": opaqueRefSchema(), "capability_key": stringSchema(1, 255),
	})
	inputSchema["oneOf"] = []map[string]any{
		{"not": map[string]any{"anyOf": []map[string]any{{"required": []string{"connection_ref"}}, {"required": []string{"capability_key"}}}}},
		{"required": []string{"connection_ref", "capability_key"}, "not": map[string]any{"anyOf": []map[string]any{{"required": []string{"query"}}, {"required": []string{"offset"}}}}},
	}
	return map[string]any{
		"name":        "get_integration_catalog",
		"description": "Discover only integration grants bound to this RuntimeRevision. Use query and offset for a compact index. Supply exact connection_ref and capability_key together to read one input schema before invoke_integration. Names are display data, not authority.",
		"inputSchema": inputSchema,
		"outputSchema": objectSchema([]string{"grants"}, map[string]any{
			"grants":      map[string]any{"type": "array", "maxItems": maximumIntegrationCatalogPage, "items": map[string]any{"type": "object"}},
			"next_offset": map[string]any{"type": "integer", "minimum": 1, "maximum": 256},
		}),
	}
}

type integrationCatalogInputError struct{ reason string }

func (catalogErr *integrationCatalogInputError) Error() string {
	return "integration catalog input is invalid"
}

func (catalogErr *integrationCatalogInputError) GRPCStatus() *status.Status {
	return status.New(codes.InvalidArgument, catalogErr.Error())
}

func invalidIntegrationCatalog(reason string) error {
	return &integrationCatalogInputError{reason: reason}
}

func integrationCatalog(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if len(input.IntegrationGrants) == 0 {
		return nil, errors.New("integration catalog is not available")
	}
	if !onlyKeys(arguments, "query", "offset", "connection_ref", "capability_key") {
		return nil, invalidIntegrationCatalog("top_level_shape")
	}
	query, _ := arguments["query"].(string)
	if raw, exists := arguments["query"]; exists {
		if _, ok := raw.(string); !ok || utf8.RuneCountInString(query) > 80 {
			return nil, invalidIntegrationCatalog("query")
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
				return nil, invalidIntegrationCatalog("offset")
			}
			offset = int(value)
		default:
			return nil, invalidIntegrationCatalog("offset")
		}
		if offset < 0 || offset > 256 {
			return nil, invalidIntegrationCatalog("offset")
		}
	}
	connection, connectionValid := arguments["connection_ref"].(string)
	capability, capabilityValid := arguments["capability_key"].(string)
	_, querySelected := arguments["query"]
	_, offsetSelected := arguments["offset"]
	_, connectionSelected := arguments["connection_ref"]
	_, capabilitySelected := arguments["capability_key"]
	if connectionSelected && !connectionValid || capabilitySelected && !capabilityValid {
		return nil, invalidIntegrationCatalog("selection_shape")
	}
	if connectionSelected != capabilitySelected || connectionSelected && (connection == "" || capability == "" || querySelected || offsetSelected) {
		return nil, invalidIntegrationCatalog("selection_shape")
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
		return nil, invalidIntegrationCatalog("selection_missing")
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
