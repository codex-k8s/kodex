package controlplane

import (
	"regexp"
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/jackc/pgx/v5"
)

var namedPlaceholderPattern = regexp.MustCompile(`@([a-z][a-z0-9_]*)`)

func TestRuntimeContinuationStrictNamedArgumentsMatchSQL(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		args pgx.StrictNamedArgs
	}{
		{
			name: "runtime insert", sql: sqlRuntimeExecutionInsert,
			args: runtimeExecutionArgs(domainrepo.RuntimeExecution{}),
		},
		{
			name: "runtime update", sql: sqlRuntimeExecutionUpdate,
			args: runtimeExecutionUpdateArgs(domainrepo.RuntimeExecution{}, 1, 1),
		},
		{
			name: "integration insert", sql: sqlIntegrationContinuationInsert,
			args: integrationContinuationArgs(domainrepo.IntegrationContinuation{}),
		},
		{
			name: "integration update", sql: sqlIntegrationContinuationUpdate,
			args: integrationContinuationUpdateArgs(domainrepo.IntegrationContinuation{}, 1, 1),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			placeholders := make(map[string]struct{})
			for _, match := range namedPlaceholderPattern.FindAllStringSubmatch(test.sql, -1) {
				placeholders[match[1]] = struct{}{}
			}
			for name := range placeholders {
				if _, ok := test.args[name]; !ok {
					t.Fatalf("placeholder has no argument: %s", name)
				}
			}
			for name := range test.args {
				if _, ok := placeholders[name]; !ok {
					t.Fatalf("strict argument is unused: %s", name)
				}
			}
		})
	}
}
