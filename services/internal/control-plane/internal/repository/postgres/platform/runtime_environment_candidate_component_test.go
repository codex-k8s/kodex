package platform

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeEnvironmentEffectiveVersionMatrix evaluates the actual production CASE with read-only VALUES and never mutates a binding to manufacture an invalid state.
// runtimeEnvironmentEffectiveVersionMatrix вычисляет настоящий production CASE на read-only VALUES и не изменяет binding ради создания недопустимого состояния.
//
//go:embed testdata/sql/runtime_environment_effective_version_matrix.sql
var runtimeEnvironmentEffectiveVersionMatrix string

// testRuntimeEnvironmentCandidateVersionFallback checks system-only null fallback and explicit historical pins using the production expression rather than a copied model.
// testRuntimeEnvironmentCandidateVersionFallback проверяет system-only null fallback и explicit historical pins настоящим production expression, а не скопированной моделью.
func testRuntimeEnvironmentCandidateVersionFallback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, tail, ok := strings.Cut(queryRuntimeClaimEnvironmentCandidate, "AND version.id=CASE")
	if !ok {
		t.Fatal("production effective-version CASE is missing")
	}
	body, _, ok := strings.Cut(tail, "END")
	if !ok {
		t.Fatal("production effective-version CASE is incomplete")
	}
	query := fmt.Sprintf(runtimeEnvironmentEffectiveVersionMatrix, "CASE"+body+"END")
	const project = "10000000-0000-4000-8000-000000000001"
	const current = "10000000-0000-4000-8000-000000000002"
	const historical = "10000000-0000-4000-8000-000000000003"
	for _, item := range []struct {
		name              string
		project, key, pin any
		want              string
	}{
		{name: "project null has no fallback", project: project},
		{name: "non-system organization null has no fallback", key: "other"},
		{name: "project cannot impersonate system fallback", project: project, key: "system-assistant"},
		{name: "canonical system null uses current", key: "system-assistant", want: current},
		{name: "project historical pin remains exact", project: project, pin: historical, want: historical},
		{name: "system historical pin remains exact", key: "system-assistant", pin: historical, want: historical},
	} {
		t.Run(item.name, func(t *testing.T) {
			var actual *string
			if err := pool.QueryRow(ctx, query, item.project, item.key, current, item.pin).Scan(&actual); err != nil {
				t.Fatal("read-only effective-version matrix failed")
			}
			if item.want == "" && actual != nil || item.want != "" && (actual == nil || *actual != item.want) {
				t.Fatal("effective-version fallback crossed the canonical profile boundary")
			}
		})
	}
}
