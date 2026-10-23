package grpc

import (
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// TestDomainContextResourceStatePreservesWireFilter checks the shared filter mapping for every known enum value and rejects unknown numbers.
// TestDomainContextResourceStatePreservesWireFilter проверяет общий перевод фильтра для всех известных значений enum и отклоняет неизвестные числа.
func TestDomainContextResourceStatePreservesWireFilter(t *testing.T) {
	for _, test := range []struct {
		name  string
		input controlplanev1.ContextResourceState
		want  string
		ok    bool
	}{
		{name: "unspecified", input: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_UNSPECIFIED, want: "", ok: true},
		{name: "active", input: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE, want: "ACTIVE", ok: true},
		{name: "archived", input: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ARCHIVED, want: "ARCHIVED", ok: true},
		{name: "expired", input: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_EXPIRED, want: "EXPIRED", ok: true},
		{name: "purged", input: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED, want: "PURGED", ok: true},
		{name: "unknown", input: controlplanev1.ContextResourceState(99), ok: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := domainContextResourceState(test.input)
			if got != test.want || ok != test.ok {
				t.Fatalf("domain context state %d = (%q, %t), want (%q, %t)", test.input, got, ok, test.want, test.ok)
			}
		})
	}
}
