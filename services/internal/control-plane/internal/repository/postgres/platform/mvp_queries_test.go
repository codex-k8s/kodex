package platform

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestProviderAccountActionsUseCanonicalNextActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		item entity.ProviderAccount
		want []string
	}{
		{name: "pending", item: entity.ProviderAccount{State: "PENDING_AUTHORIZATION", Enabled: true}, want: []string{"OPEN", "REVOKE"}},
		{name: "active", item: entity.ProviderAccount{State: "AUTHORIZED", Enabled: true}, want: []string{"OPEN", "TEST", "REVOKE", "DISABLE"}},
		{name: "disabled", item: entity.ProviderAccount{State: "AUTHORIZED", Enabled: false}, want: []string{"OPEN", "TEST", "REVOKE", "ENABLE"}},
		{name: "configure", item: entity.ProviderAccount{State: "REAUTHORIZATION_REQUIRED", Enabled: true}, want: []string{"OPEN", "CONFIGURE_CREDENTIAL", "DISABLE"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := providerAccountActions(test.item, true, true, true); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("provider account actions = %v, want %v", got, test.want)
			}
		})
	}
}

func TestProviderAccountActionsForViewOnlyMember(t *testing.T) {
	t.Parallel()
	item := entity.ProviderAccount{State: "AUTHORIZED", Enabled: true}
	if got := providerAccountActions(item, false, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("view-only provider account actions = %v, want [OPEN]", got)
	}
}
