package query

// ExecutionIntegrationOutcomes selects an immutable immediate predecessor snapshot;
// lease pins resolve the actor and source rather than accepting caller ownership.
// ExecutionIntegrationOutcomes выбирает неизменяемую историю непосредственного предшественника;
// lease pins разрешают actor и источник без caller ownership.
type ExecutionIntegrationOutcomes struct {
	LeaseRef, Fence, SourceInvocationRef, SourceSHA256 string
	Generation, OffsetBytes                            int64
	MaximumBytes                                       int32
}
