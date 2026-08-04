package generated

type AnonymousSchema_191 struct {
	Id              string               `json:"id" binding:"required"`
	Action          *AnonymousSchema_193 `json:"action" binding:"required"`
	ResourceId      string               `json:"resourceId" binding:"required"`
	ResourceKind    *AnonymousSchema_195 `json:"resourceKind" binding:"required"`
	ResourceVersion int                  `json:"resourceVersion" binding:"required"`
	Outcome         *AnonymousSchema_197 `json:"outcome" binding:"required"`
	ActorId         string               `json:"actorId" binding:"required"`
	CorrelationId   string               `json:"correlationId" binding:"required"`
	PolicyRevision  int                  `json:"policyRevision" binding:"required"`
	OccurredAt      string               `json:"occurredAt" binding:"required"`
}
