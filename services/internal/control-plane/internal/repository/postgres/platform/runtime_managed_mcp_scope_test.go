package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestManagedMCPStartupScopePreservesNilAndConstrainedEmpty distinguishes unrestricted nil layers
// from constrained empty layers and checks the intersection that requires the complete Context7
// startup pair.
// TestManagedMCPStartupScopePreservesNilAndConstrainedEmpty различает неограниченные nil-слои и
// явно пустые ограничения и проверяет пересечение, требующее полную пару Context7 при запуске.
func TestManagedMCPStartupScopePreservesNilAndConstrainedEmpty(t *testing.T) {
	resolve, query := runtimecontract.Context7ResolveCapability, runtimecontract.Context7QueryCapability
	for _, scenario := range []struct {
		name     string
		layers   [][]string
		required bool
	}{
		{"без ограничений", nil, true},
		{"неприменимые слои", [][]string{nil, nil}, true},
		{"пустой Workflow", [][]string{{}, nil}, false},
		{"coordinator только делегирует", [][]string{{"platform.run.delegate"}, nil}, false},
		{"оба Context7 key", [][]string{{resolve, query}, nil}, true},
		{"только resolve требует прежнюю полную пару", [][]string{{resolve}, nil}, true},
		{"только query требует прежнюю полную пару", [][]string{{query}, nil}, true},
		{"пустой Human Gate", [][]string{nil, {}}, false},
		{"несовместимые слои", [][]string{{resolve}, {query}}, false},
		{"совпадающие слои", [][]string{{resolve, query}, {query}}, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if got := managedMCPStartupScopeRequired(scenario.layers...); got != scenario.required {
				t.Fatal("managed MCP startup scope changed")
			}
		})
	}
}

// TestManagedMCPExcludedScopeCannotAdoptContext7Grant skips excluded MCP dependencies without
// database access but rejects an attached Context7 grant in that excluded scope.
// TestManagedMCPExcludedScopeCannotAdoptContext7Grant пропускает исключённые зависимости MCP без
// доступа к БД, но отклоняет присоединённое разрешение Context7 в исключённой области.
func TestManagedMCPExcludedScopeCannotAdoptContext7Grant(t *testing.T) {
	blocked := []string{"platform.run.delegate"}
	if err := requireManagedMCPStartupDependencies(t.Context(), nil, "unused", "unused", nil, blocked); err != nil {
		t.Fatal("excluded dependency required a database read")
	}
	grant := runtimecontract.RunnerIntegrationGrant{DefinitionKey: "context7", CapabilityKey: runtimecontract.Context7QueryCapability}
	if err := requireManagedMCPStartupDependencies(t.Context(), nil, "unused", "unused", []runtimecontract.RunnerIntegrationGrant{grant}, blocked); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("excluded scope adopted a managed MCP grant")
	}
}
