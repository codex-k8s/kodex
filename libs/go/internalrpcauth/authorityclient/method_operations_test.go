package authorityclient

import "testing"

// TestMethodOperationsPreservesExactLookup pins nil/unknown/exact/case-sensitive
// and empty-key/value lookup semantics through OperationResolver, then checks
// ordinary map aliasing. It does not issue or verify an authorization context.
// TestMethodOperationsPreservesExactLookup закрепляет семантику nil, unknown,
// exact, case-sensitive и empty-key/value через OperationResolver, затем
// проверяет обычное map aliasing без выдачи и проверки authorization context.
func TestMethodOperationsPreservesExactLookup(t *testing.T) {
	operations := MethodOperations{
		"/example.v1.Service/Read":  "example.read",
		"/example.v1.Service/Empty": "",
		"":                          "empty.method",
	}
	for _, test := range []struct {
		name     string
		resolver MethodOperations
		method   string
		want     string
		present  bool
	}{
		{name: "nil map", method: "/example.v1.Service/Read"},
		{name: "unknown method", resolver: operations, method: "/example.v1.Service/Other"},
		{name: "exact method", resolver: operations, method: "/example.v1.Service/Read", want: "example.read", present: true},
		{name: "case sensitive", resolver: operations, method: "/example.v1.Service/read"},
		{name: "empty value", resolver: operations, method: "/example.v1.Service/Empty", present: true},
		{name: "empty key", resolver: operations, want: "empty.method", present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var resolver OperationResolver = test.resolver
			value, present := resolver.OperationID(test.method)
			if value != test.want || present != test.present {
				t.Fatalf("lookup result = (%q, %t), want (%q, %t)", value, present, test.want, test.present)
			}
		})
	}
	alias := operations
	alias["/example.v1.Service/Read"] = "changed.read"
	if value, present := operations.OperationID("/example.v1.Service/Read"); value != "changed.read" || !present {
		t.Fatal("method operation map no longer preserves ordinary aliasing")
	}
}
