package callback

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maximumIntegrationCatalogPage caps compact integration index pages at eight
// grants; exact selection returns only its single matched grant schema.
// maximumIntegrationCatalogPage ограничивает compact integration index восемью
// grants; exact selection выдаёт лишь одну matched grant schema.
const maximumIntegrationCatalogPage = 8

// integrationCatalogTool describes three disjoint discovery modes: query/offset
// index, exact grant ref or exact connection/capability pair. Schema advertisement
// exposes only snapshot-bound grants and no connection credentials or invocation authority.
// integrationCatalogTool описывает три отдельных discovery modes:query/offset index,
// exact grant ref либо exact connection/capability pair. Схема публикует лишь snapshot-
// bound grants без connection credentials и invocation authority.
func integrationCatalogTool() map[string]any {
	inputSchema := objectSchema(nil, map[string]any{
		"query": stringSchema(0, 80), "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 256},
		"grant_ref":      opaqueRefSchema(),
		"connection_ref": opaqueRefSchema(), "capability_key": map[string]any{
			"type": "string", "minLength": 1, "maxLength": 255,
			"description": "Copy this value exactly from the compact catalog index. It is not the OpenAPI operationId or a display name.",
		},
	})
	inputSchema["oneOf"] = []map[string]any{
		{"not": map[string]any{"anyOf": []map[string]any{{"required": []string{"grant_ref"}}, {"required": []string{"connection_ref"}}, {"required": []string{"capability_key"}}}}},
		{"required": []string{"grant_ref"}, "not": map[string]any{"anyOf": []map[string]any{{"required": []string{"query"}}, {"required": []string{"offset"}}, {"required": []string{"connection_ref"}}, {"required": []string{"capability_key"}}}}},
		{"required": []string{"connection_ref", "capability_key"}, "not": map[string]any{"anyOf": []map[string]any{{"required": []string{"query"}}, {"required": []string{"offset"}}, {"required": []string{"grant_ref"}}}}},
	}
	return map[string]any{
		"name":        "get_integration_catalog",
		"description": "Discover only grants bound to this RuntimeRevision. Call with {} or query and offset for a compact index. Copy grant_ref from one index entry and call again with that grant_ref to read its input schema. Pass the same grant_ref to invoke_integration. Do not guess identifiers from names or OpenAPI operationId.",
		"inputSchema": inputSchema,
		"outputSchema": objectSchema([]string{"grants"}, map[string]any{
			"grants":      map[string]any{"type": "array", "maxItems": maximumIntegrationCatalogPage, "items": map[string]any{"type": "object"}},
			"next_offset": map[string]any{"type": "integer", "minimum": 1, "maximum": 256},
		}),
	}
}

// integrationCatalogInputError records a local closed selection reason without
// retaining query text, remote causes or an owner permission decision.
// integrationCatalogInputError хранит local closed selection reason без query text,
// remote causes и owner permission decision.
type integrationCatalogInputError struct{ reason string }

// Error returns fixed safe catalog-input text independent of rejected arguments.
// Error возвращает fixed safe catalog-input text независимо от rejected arguments.
func (catalogErr *integrationCatalogInputError) Error() string {
	return "integration catalog input is invalid"
}

// GRPCStatus maps the local typed marker to InvalidArgument with fixed text,
// without attaching arbitrary reason metadata or private input.
// GRPCStatus отображает local typed marker в InvalidArgument с fixed text без
// arbitrary reason metadata и private input.
func (catalogErr *integrationCatalogInputError) GRPCStatus() *status.Status {
	return status.New(codes.InvalidArgument, catalogErr.Error())
}

// invalidIntegrationCatalog creates a typed local reason marker only; it does not
// make a failed owner invocation retryable or issue new grants.
// invalidIntegrationCatalog создаёт лишь typed local reason marker,не превращая failed
// owner invocation в retryable и не выдавая grants.
func invalidIntegrationCatalog(reason string) error {
	return &integrationCatalogInputError{reason: reason}
}

// integrationCatalog validates mutually exclusive local selectors, sorts a copied
// grant slice by connection name/capability/ref and returns a filtered eight-entry
// index or one selected schema. Only existing immutable input grants are searched;
// schema JSON is decoded but not rehashed here, and current connection health/owner
// eligibility/approval are not inferred from discovery.
// integrationCatalog проверяет exclusive local selectors,сортирует copy grant slice
// по connection name/capability/ref и выдаёт filtered eight-entry index либо selected
// schema. Поиск ограничен existing immutable input grants; schema JSON декодируется,
// но не хешируется заново. Current connection health/owner eligibility/approval
// из discovery не выводятся.
func integrationCatalog(input runtimecontract.RunnerInput, arguments map[string]any) (any, error) {
	if len(input.IntegrationGrants) == 0 {
		return nil, errors.New("integration catalog is not available")
	}
	if !onlyKeys(arguments, "query", "offset", "grant_ref", "connection_ref", "capability_key") {
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
	grantRef, grantRefValid := arguments["grant_ref"].(string)
	_, querySelected := arguments["query"]
	_, offsetSelected := arguments["offset"]
	_, grantSelected := arguments["grant_ref"]
	_, connectionSelected := arguments["connection_ref"]
	_, capabilitySelected := arguments["capability_key"]
	if connectionSelected && !connectionValid || capabilitySelected && !capabilityValid || grantSelected && !grantRefValid {
		return nil, invalidIntegrationCatalog("selection_shape")
	}
	if connectionSelected != capabilitySelected ||
		grantSelected && (grantRef == "" || connectionSelected || capabilitySelected || querySelected || offsetSelected) ||
		connectionSelected && (connection == "" || capability == "" || querySelected || offsetSelected) {
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
	if grantSelected || connectionSelected {
		for _, grant := range grants {
			if grantSelected && grant.Ref != grantRef || connectionSelected && (grant.ConnectionRef != connection || grant.CapabilityKey != capability) {
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

// integrationCatalogEntry projects grant/definition/capability pins and risk/operation
// metadata with a 500-rune description, omitting full schema until exact selection.
// It contains no credential values and neither tests nor invokes the integration.
// integrationCatalogEntry проецирует grant/definition/capability pins,risk/operation
// metadata с description до 500 runes,не выдавая full schema до exact selection.
// Credential values отсутствуют,integration не тестируется и не вызывается.
func integrationCatalogEntry(grant runtimecontract.RunnerIntegrationGrant) map[string]any {
	entry := map[string]any{
		"grant_ref":      grant.Ref,
		"connection_ref": grant.ConnectionRef, "connection_name": grant.ConnectionName,
		"definition_key": grant.DefinitionKey, "definition_version": grant.DefinitionVersion,
		"definition_digest": grant.DefinitionDigest, "capability_key": grant.CapabilityKey,
		"capability_name": grant.CapabilityName, "capability_description": truncateRunes(grant.CapabilityDescription, 500),
		"risk": grant.Risk, "operation": grant.Operation, "input_schema_sha256": grant.InputSchemaSHA256,
	}
	if grant.Operation == repositorybundle.Operation {
		entry["tool_name"] = repositorybundle.ToolName
	}
	return entry
}
