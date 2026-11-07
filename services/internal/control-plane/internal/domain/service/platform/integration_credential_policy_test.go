package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestIntegrationCredentialPolicySwitchRejectsLiveDependencies covers independently unsettled
// tests/effects, grants and retained root pins; unrelated organization work is not an input.
// TestIntegrationCredentialPolicySwitchRejectsLiveDependencies проверяет отдельные незавершённые
// tests/effects, grants и сохранённые root pins; посторонняя работа организации не входит во входы.
func TestIntegrationCredentialPolicySwitchRejectsLiveDependencies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                   string
		enabled                bool
		state                  string
		effects, grants, roots int64
	}{
		{name: "enabled", enabled: true, state: "DISABLED"},
		{name: "testing", state: "TESTING"},
		{name: "effect", state: "DISABLED", effects: 1},
		{name: "grant", state: "DISABLED", grants: 1},
		{name: "root", state: "DISABLED", roots: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateIntegrationCredentialPolicyChange(entity.IntegrationCredentialProjectShared, entity.IntegrationCredentialInitiatorRequired,
				test.enabled, test.state, test.effects, test.grants, test.roots); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("live credential selection changed", err)
			}
		})
	}
	if err := ValidateIntegrationCredentialPolicyChange(entity.IntegrationCredentialProjectShared, entity.IntegrationCredentialInitiatorRequired, false, "DISABLED", 0, 0, 0); err != nil {
		t.Fatal("idle explicit policy switch rejected", err)
	}
	if err := ValidateIntegrationCredentialPolicyChange(entity.IntegrationCredentialInitiatorRequired, entity.IntegrationCredentialProjectShared, false, "DISABLED", 0, 0, 0); err != nil {
		t.Fatal("idle shared policy restoration rejected", err)
	}
	if err := ValidateIntegrationCredentialPolicyChange(entity.IntegrationCredentialProjectShared, "", false, "DISABLED", 0, 0, 0); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("unspecified policy became shared fallback", err)
	}
	if err := ValidateIntegrationCredentialPolicyChange(entity.IntegrationCredentialProjectShared, entity.IntegrationCredentialProjectShared, true, "CONNECTED", 1, 1, 1); err != nil {
		t.Fatal("same-policy configuration readback changed lifecycle", err)
	}
}
