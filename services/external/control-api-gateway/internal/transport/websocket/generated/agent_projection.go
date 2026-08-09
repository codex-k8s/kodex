
package generated

type AgentProjection struct {
  StableKey string
  RoleDefinitionRef string
  InstructionSetRef string
  ProviderPoolRef string
  RuntimeProfileRef string
  Capabilities []string
  BotUsername string
  BotMaskedStatus string
  Enabled bool
  Ownership *ConfigurationOwnershipProjection
}