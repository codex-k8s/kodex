package platform

import "testing"

func TestFinancialCallbackReceiptIdentityIgnoresTransportCorrelation(t *testing.T) {
	original := financialCallbackKey("lease_fixture", 1, 2)
	if original != financialCallbackKey("lease_fixture", 1, 2) || original == financialCallbackKey("lease_fixture", 2, 2) || original == financialCallbackKey("lease_foreign", 1, 2) || original == financialCallbackKey("lease_fixture", 1, 3) {
		t.Fatal("financial receipt identity is not bound to the exact execution sequence")
	}
}
