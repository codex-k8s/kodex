package integrationpackage

import "testing"

// TestConnectionConfigurationHonorsDeclaredScalars proves valid bounded INTEGER/BOOLEAN configuration is executable and aliases or wrong types remain closed.
// TestConnectionConfigurationHonorsDeclaredScalars доказывает исполнимость bounded INTEGER/BOOLEAN configuration и закрытый отказ для aliases либо неверных типов.
func TestConnectionConfigurationHonorsDeclaredScalars(t *testing.T) {
	definition := Package{Spec: Spec{ConfigurationFields: []Field{{Key: "count", Type: "INTEGER", Required: true, Minimum: 1, Maximum: 20}, {Key: "enabled", Type: "BOOLEAN", Required: true}}}}
	if err := definition.ValidateConfiguration(map[string]string{"count": "3", "enabled": "false"}); err != nil {
		t.Fatal("valid typed configuration rejected")
	}
	for _, values := range []map[string]string{{"count": "0", "enabled": "true"}, {"count": "21", "enabled": "true"}, {"count": "03", "enabled": "true"},
		{"count": "+3", "enabled": "true"}, {"count": "3.0", "enabled": "true"}, {"count": "3", "enabled": "TRUE"}, {"count": "3", "enabled": "1"}} {
		if err := definition.ValidateConfiguration(values); err == nil {
			t.Fatal("invalid typed configuration accepted")
		}
	}
}
