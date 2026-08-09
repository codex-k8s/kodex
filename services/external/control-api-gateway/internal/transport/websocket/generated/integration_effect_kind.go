
package generated

type IntegrationEffectKind uint

const (
  IntegrationEffectKindMcpTool IntegrationEffectKind = iota
  IntegrationEffectKindCli
  IntegrationEffectKindEnvironment
)

// Value returns the value of the enum.
func (op IntegrationEffectKind) Value() any {
	if op >= IntegrationEffectKind(len(IntegrationEffectKindValues)) {
		return nil
	}
	return IntegrationEffectKindValues[op]
}

var IntegrationEffectKindValues = []any{"MCP_TOOL","CLI","ENVIRONMENT"}
var ValuesToIntegrationEffectKind = map[any]IntegrationEffectKind{
  IntegrationEffectKindValues[IntegrationEffectKindMcpTool]: IntegrationEffectKindMcpTool,
  IntegrationEffectKindValues[IntegrationEffectKindCli]: IntegrationEffectKindCli,
  IntegrationEffectKindValues[IntegrationEffectKindEnvironment]: IntegrationEffectKindEnvironment,
}
