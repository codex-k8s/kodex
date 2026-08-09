
package generated

type ProviderPoolResourceProjection struct {
  StableKey string
  Policy *ProviderPoolPolicy
  PolicyRevision int
  ObservationMaxAgeSeconds int
  EligibleMembers int
  TotalMembers int
  EligibilitySnapshotSha256 string
  Ownership *ConfigurationOwnershipProjection
}