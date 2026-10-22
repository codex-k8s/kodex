package callback

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// managedMCPTools advertises the two supported aliases using schemas derived
// from pinned owner grants, returning no aliases on schema failure. It exposes
// no caller-configurable connection, credential or second MCP transport;
// advertisement alone does not prove fresh owner health.
// managedMCPTools публикует два alias со схемами из pinned owner grants,
// не выдавая aliases при ошибке схемы. Caller не получает настройку connection,
// credential и второго MCP transport; каталог сам не доказывает свежесть health.
func managedMCPTools(input runtimecontract.RunnerInput) []map[string]any {
	schemas, err := runtimecontract.ManagedMCPToolSchemas(input)
	if err != nil {
		return nil
	}
	result := make([]map[string]any, 0, len(schemas))
	for _, tool := range []struct{ name, description string }{
		{runtimecontract.Context7ResolveTool, "Resolve an official library identifier using Context7. Copy its exact identifier to context7_query_docs."},
		{runtimecontract.Context7QueryTool, "Read current official library documentation through the owner-bound Context7 connection."},
	} {
		if schema, ok := schemas[tool.name]; ok {
			var shape map[string]any
			if json.Unmarshal([]byte(schema), &shape) != nil {
				return nil
			}
			result = append(result, map[string]any{"name": tool.name, "description": tool.description, "inputSchema": shape})
		}
	}
	return result
}

// managedMCPArguments checks one bound profile, rejects a future health timestamp
// and exact-name/UTF-8/rune-bound input violations, then selects its pinned grant.
// It preserves the original argument map and historical receipt, leaving current
// health freshness to owner invocation rather than extending receipt lifetime.
// managedMCPArguments проверяет один bound profile, отвергает future health time
// и нарушения имён/UTF-8/rune limits, затем выбирает pinned grant. Исходная map
// arguments и историческая receipt сохраняются; текущую свежесть health проверяет
// owner invocation, срок receipt локально не продлевается.
func managedMCPArguments(input runtimecontract.RunnerInput, tool string, arguments map[string]any) (map[string]any, error) {
	// Freshness проверяет CP при каждом ResolveIntegrationInvocation по текущему
	// owner ledger. Исторический immutable receipt не получает новый срок.
	if runtimecontract.ValidateManagedMCPProfiles(input) != nil || len(input.ManagedMCPProfiles) != 1 ||
		input.ManagedMCPProfiles[0].Health.CheckedAt.After(time.Now().UTC()) {
		return nil, errors.New("required managed MCP health proof is unavailable")
	}
	profile := input.ManagedMCPProfiles[0]
	var grantRef, field string
	maximumLength := 512
	switch tool {
	case runtimecontract.Context7ResolveTool:
		grantRef = profile.ResolveGrantRef
		field, maximumLength = "library_name", 200
	case runtimecontract.Context7QueryTool:
		grantRef = profile.QueryGrantRef
		field = "library_id"
	default:
		return nil, errors.New("managed MCP tool is not available")
	}
	if !onlyKeys(arguments, field, "query") || len(arguments) != 2 {
		return nil, errors.New("managed MCP input is invalid")
	}
	for name, maximum := range map[string]int{field: maximumLength, "query": 4096} {
		text, ok := arguments[name].(string)
		if !ok || strings.TrimSpace(text) == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > maximum {
			return nil, errors.New("managed MCP input is invalid")
		}
	}
	return map[string]any{"grant_ref": grantRef, "input": arguments}, nil
}
