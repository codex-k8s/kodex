package platform

import "testing"

// TestFinancialCallbackReceiptIdentityIgnoresTransportCorrelation checks stable callback receipt
// keys bind the lease, generation and execution sequence; no transport correlation input
// participates in these calls.
// TestFinancialCallbackReceiptIdentityIgnoresTransportCorrelation проверяет устойчивую привязку
// ключа квитанции callback к lease, поколению и последовательности исполнения; транспортная
// корреляция не участвует во входных данных этих вызовов.
func TestFinancialCallbackReceiptIdentityIgnoresTransportCorrelation(t *testing.T) {
	original := financialCallbackKey("lease_fixture", 1, 2)
	if original != financialCallbackKey("lease_fixture", 1, 2) || original == financialCallbackKey("lease_fixture", 2, 2) || original == financialCallbackKey("lease_foreign", 1, 2) || original == financialCallbackKey("lease_fixture", 1, 3) {
		t.Fatal("financial receipt identity is not bound to the exact execution sequence")
	}
}
