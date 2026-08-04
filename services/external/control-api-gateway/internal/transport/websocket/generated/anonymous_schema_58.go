package generated

type AnonymousSchema_58 struct {
	Policy                   *AnonymousSchema_59  `json:"policy" binding:"required"`
	PolicyRevision           int                  `json:"policyRevision" binding:"required"`
	ObservationMaxAgeSeconds int                  `json:"observationMaxAgeSeconds" binding:"required"`
	Bindings                 []AnonymousSchema_63 `json:"bindings" binding:"required"`
}
