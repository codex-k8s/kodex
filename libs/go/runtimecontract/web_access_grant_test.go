package runtimecontract

import "testing"

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
