package value

import "testing"

// TestIntegrationInvocationCapacityBounds proves zero exclusion and bounded sums without integer overflow.
// TestIntegrationInvocationCapacityBounds проверяет zero exclusion и bounded sums без integer overflow.
func TestIntegrationInvocationCapacityBounds(t *testing.T) {
	for _, test := range []struct {
		capacity IntegrationInvocationDeliveryCapacity
		limit    int32
		valid    bool
	}{
		{IntegrationInvocationDeliveryCapacity{}, 1, true},
		{IntegrationInvocationDeliveryCapacity{Ordinary: 1, Kubernetes: 1, SSH: 1}, 3, true},
		{IntegrationInvocationDeliveryCapacity{Kubernetes: 4}, 4, true},
		{IntegrationInvocationDeliveryCapacity{Kubernetes: 5}, 8, false},
		{IntegrationInvocationDeliveryCapacity{SSH: 5}, 8, false},
		{IntegrationInvocationDeliveryCapacity{Ordinary: 33}, 32, false},
		{IntegrationInvocationDeliveryCapacity{Ordinary: 32, Kubernetes: 1}, 32, false},
		{IntegrationInvocationDeliveryCapacity{Ordinary: ^uint32(0)}, 32, false},
		{IntegrationInvocationDeliveryCapacity{}, 0, false},
	} {
		if test.capacity.Valid(test.limit) != test.valid {
			t.Fatal("capacity boundary changed", test)
		}
	}
}
