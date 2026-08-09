
package generated

type ProviderConnectionProjection struct {
  ConnectionRef string
  StableKey string
  ProviderRef string
  DisplayName string
  Version int
  Generation int
  State *ProviderConnectionState
  MaskedLabel string
  MaskedAccount string
  Capabilities []string
  CapabilityDigestSha256 string
  ObservationDigestSha256 string
  ObservedAt string
  UpdatedAt string
  ActiveCredentialGeneration int
  Capacity *ProviderCapacity
}