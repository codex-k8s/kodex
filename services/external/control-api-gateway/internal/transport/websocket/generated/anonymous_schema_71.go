package generated

type AnonymousSchema_71 struct {
	Purpose                     string              `json:"purpose" binding:"required"`
	ImmutableSecretRef          string              `json:"immutableSecretRef" binding:"required"`
	PrincipalRef                string              `json:"principalRef" binding:"required"`
	Revision                    int                 `json:"revision" binding:"required"`
	ExpiresAt                   string              `json:"expiresAt,omitempty"`
	ProviderEligible            bool                `json:"providerEligible" binding:"required"`
	ProviderCapabilities        []string            `json:"providerCapabilities" binding:"required"`
	ProviderObservationRevision int                 `json:"providerObservationRevision" binding:"required"`
	ProviderObservedAt          string              `json:"providerObservedAt,omitempty"`
	ContentSha256               string              `json:"contentSha256" binding:"required"`
	Ownership                   *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
