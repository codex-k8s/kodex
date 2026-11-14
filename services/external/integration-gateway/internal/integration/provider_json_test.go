package integration

import "testing"

// TestTypedProviderJSONNullBoundaries distinguishes optional nil-capable values from required scalars and objects, preserving canonical keys and duplicate rejection before semantic decoding.
// TestTypedProviderJSONNullBoundaries различает optional nil-capable values и обязательные scalars/objects, сохраняя canonical keys и запрет duplicate keys до semantic decoding.
func TestTypedProviderJSONNullBoundaries(t *testing.T) {
	var nullable struct {
		Child *struct {
			ID string `json:"id"`
		} `json:"child"`
		Labels map[string]string `json:"labels"`
		Items  []string          `json:"items"`
	}
	if err := decodeTypedProviderJSON([]byte(`{"child":null,"labels":null,"items":null}`), &nullable); err != nil || nullable.Child != nil || nullable.Labels != nil || nullable.Items != nil {
		t.Fatal("explicit optional null was not preserved", err)
	}
	for _, target := range []any{new(int), new(string), new(bool), new(float64), &struct {
		ID string `json:"id"`
	}{}} {
		if decodeTypedProviderJSON([]byte("null"), target) == nil {
			t.Fatal("required scalar or object accepted null")
		}
	}
	for _, raw := range []string{`{"child":{"id":null}}`, `{"child":{"ID":"7"}}`, `{"labels":{"a":null}}`, `{"items":[null]}`, `{"child":null,"child":null}`} {
		if decodeTypedProviderJSON([]byte(raw), &nullable) == nil {
			t.Fatal("malformed nullable model accepted")
		}
	}
	if decodeTypedProviderJSON([]byte(`{}`), nil) == nil {
		t.Fatal("missing decode target accepted")
	}
}
