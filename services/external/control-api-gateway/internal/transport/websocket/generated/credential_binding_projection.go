
package generated

type CredentialBindingProjection struct {
  Purpose string
  ImmutableSecretRef string
  PrincipalRef string
  Revision int
  ExpiresAt string
  ProviderEligible bool
  ProviderCapabilities []string
  ProviderObservationRevision int
  ProviderObservedAt string
  ContentSha256 string
  Ownership *ConfigurationOwnershipProjection
}