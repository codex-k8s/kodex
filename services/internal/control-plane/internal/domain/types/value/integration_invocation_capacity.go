package value

// IntegrationInvocationDeliveryCapacity narrows one Runtime claim to already-reserved local slots.
// These counts grant neither purpose readiness nor actor, resource, identity or lease authority.
// IntegrationInvocationDeliveryCapacity сужает один Runtime claim до уже зарезервированных local slots.
// Counts не выдают purpose readiness либо actor/resource/identity/lease authority.
type IntegrationInvocationDeliveryCapacity struct {
	Ordinary, Kubernetes, SSH uint32
}

// Valid bounds each finite delivery class and its sum against the caller's existing total batch limit.
// Zero means no lease of that class may be committed; missing wire capacity is handled separately.
// Valid ограничивает каждый delivery class и сумму существующим total batch limit.
// Zero запрещает commit lease этого class; missing wire capacity обрабатывается отдельно.
func (capacity IntegrationInvocationDeliveryCapacity) Valid(limit int32) bool {
	return limit >= 1 && limit <= 32 && capacity.Ordinary <= 32 && capacity.Kubernetes <= 4 && capacity.SSH <= 4 && capacity.Ordinary+capacity.Kubernetes+capacity.SSH <= uint32(limit)
}
