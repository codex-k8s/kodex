
package generated

type IntegrationConfigurationProjection struct {
  ConfigurationRef string
  StableKey string
  Version int
  DigestSha256 string
  DefinitionRef string
  DefinitionVersion int
  DefinitionDigestSha256 string
  ConnectionRef string
  ConnectionVersion int
  ConnectionGeneration int
  Capabilities []string
  CapabilityDigestSha256 string
  EffectKind *IntegrationEffectKind
  State *IntegrationConfigurationState
  UpdatedAt string
}