
package generated

type ProviderConnectionReferenceProjection struct {
  StableKey string
  Provider string
  ReferenceVersion int
  MaskedLabel string
  MaskedStatus *ProviderConnectionMaskedStatus
  Capabilities []string
  Eligible bool
  ObservedAt string
  ObservedUsage int
  ObservedLimit int
  ObservationExpiresAt string
}