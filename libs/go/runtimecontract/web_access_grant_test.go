package runtimecontract

import "testing"

// TestRuntimeWebAccessAllowsHostScopedWildcards distinguishes exact, single-level and recursive domain patterns and rejects apex, nesting or suffix confusion where inappropriate.
// TestRuntimeWebAccessAllowsHostScopedWildcards различает точные, одноуровневые и рекурсивные доменные шаблоны, отклоняя недопустимые основной домен, вложенность и сходный суффикс.
func TestRuntimeWebAccessAllowsHostScopedWildcards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pattern  string
		hostname string
		want     bool
	}{
		{name: "exact", pattern: "api.example.com", hostname: "api.example.com", want: true},
		{name: "single level", pattern: "*.example.com", hostname: "api.example.com", want: true},
		{name: "single level rejects apex", pattern: "*.example.com", hostname: "example.com", want: false},
		{name: "single level rejects nested", pattern: "*.example.com", hostname: "v1.api.example.com", want: false},
		{name: "recursive accepts apex", pattern: "**.example.com", hostname: "example.com", want: true},
		{name: "recursive accepts nested", pattern: "**.example.com", hostname: "v1.api.example.com", want: true},
		{name: "recursive rejects suffix confusion", pattern: "**.example.com", hostname: "notexample.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			access := RuntimeWebAccess{
				Mode:  RuntimeWebAccessAllowlistFull,
				Rules: []RuntimeWebAccessRule{{DomainPattern: tt.pattern}},
			}
			if got := RuntimeWebAccessAllowsHost(access, tt.hostname); got != tt.want {
				t.Fatalf("RuntimeWebAccessAllowsHost() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRuntimeWebAccessAllowsRequestHonorsMethodPolicy permits an exact read-only GET but rejects a write method and an unlisted hostname.
// TestRuntimeWebAccessAllowsRequestHonorsMethodPolicy разрешает точный GET для чтения, но отклоняет метод записи и неперечисленный узел.
func TestRuntimeWebAccessAllowsRequestHonorsMethodPolicy(t *testing.T) {
	readOnly := RuntimeWebAccess{Mode: RuntimeWebAccessAllowlistReadOnly, Rules: []RuntimeWebAccessRule{{
		DomainPattern: "example.com", Protocol: RuntimeWebProtocolHTTPS, Port: 443,
		HTTPMethods: []string{RuntimeHTTPMethodGet, RuntimeHTTPMethodHead, RuntimeHTTPMethodOptions},
	}}}
	if !RuntimeWebAccessAllowsRequest(readOnly, "example.com", "GET") {
		t.Fatal("read-only GET was rejected")
	}
	if RuntimeWebAccessAllowsRequest(readOnly, "example.com", "POST") {
		t.Fatal("read-only POST was accepted")
	}
	if RuntimeWebAccessAllowsRequest(readOnly, "other.example.com", "GET") {
		t.Fatal("foreign hostname was accepted")
	}
}
