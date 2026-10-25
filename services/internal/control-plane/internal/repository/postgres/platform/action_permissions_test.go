package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// actionPermissionsFixture implements only the QueryRow/Scan seam of an embedded transaction for
// presentation-permission unit tests. It records query use and supplies synthetic memberships or
// an error; unoverridden transaction methods are not usable and no PostgreSQL connection exists.
// actionPermissionsFixture реализует лишь QueryRow/Scan seam встроенной transaction для
// unit-тестов presentation permissions. Фиксирует вызов запроса и возвращает synthetic memberships
// либо ошибку; прочие transaction methods непригодны, PostgreSQL connection нет.
type actionPermissionsFixture struct {
	pgx.Tx
	t           *testing.T
	failure     error
	permissions []string
	called      bool
}

// QueryRow marks the fixture queried, asserts the exact presentation SQL and
// organization/project/actor argument tuple, and returns its own Scan implementation. Unexpected
// query shape fails the test; no SQL executes.
// QueryRow отмечает fixture queried, проверяет точные presentation SQL и
// organization/project/actor arguments и возвращает собственный Scan. Неожиданная query shape
// завершает тест ошибкой; SQL не исполняется.
func (fixture *actionPermissionsFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	fixture.t.Helper()
	fixture.called = true
	if sql != queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef || len(args) != 3 ||
		args[0] != "organization-fixture" || args[1] != "project-fixture" || args[2] != "actor-fixture" {
		fixture.t.Fatal("presentation query lost exact organization/project/actor pins")
	}
	return fixture
}

// Scan returns the configured failure or assigns the configured permission slice to the expected
// first destination. It deliberately implements one fixed row shape without cloning data or
// validating arbitrary Scan arguments.
// Scan возвращает настроенную ошибку либо назначает permission slice первому ожидаемому
// destination. Намеренно реализует одну фиксированную row shape без копирования данных и проверки
// произвольных Scan arguments.
func (fixture *actionPermissionsFixture) Scan(dest ...any) error {
	if fixture.failure != nil {
		return fixture.failure
	}
	*dest[0].(*[]string) = fixture.permissions
	return nil
}

// TestProjectActionPermissionsPresentation table-checks missing/wrapped membership absence,
// backend failure, empty/unknown/cancel-only permissions and owner/admin/no-project shortcuts. It
// verifies both exact permission flags/error and whether the synthetic query ran; this exercises
// presentation hints rather than actual command authorization or DB policy.
// TestProjectActionPermissionsPresentation таблично проверяет отсутствие membership с wrapping,
// backend failure, empty/unknown/cancel-only permissions и owner/admin/no-project shortcuts.
// Сверяет точные permission flags/error и факт synthetic query; проверяет presentation hints, а не
// реальную command authorization и DB policy.
func TestProjectActionPermissionsPresentation(t *testing.T) {
	t.Parallel()
	all := actorActionPermissions{true, true, true, true, true, true, true}
	for name, test := range map[string]struct {
		failure     error
		permissions []string
		role        string
		project     string
		want        actorActionPermissions
		wantError   error
		queried     bool
	}{
		"no-membership":         {failure: pgx.ErrNoRows, project: "project-fixture", queried: true},
		"wrapped-no-membership": {failure: fmt.Errorf("synthetic wrapped absence: %w", pgx.ErrNoRows), project: "project-fixture", queried: true},
		"database-failure":      {failure: errors.New("synthetic database failure"), project: "project-fixture", wantError: errs.ErrUnavailable, queried: true},
		"empty-permissions":     {project: "project-fixture", queried: true},
		"cancel-only":           {project: "project-fixture", permissions: []string{"CANCEL_RUNS"}, want: actorActionPermissions{canCancelRuns: true}, queried: true},
		"unknown-permission":    {project: "project-fixture", permissions: []string{"UNKNOWN"}, queried: true},
		"no-project":            {},
		"owner":                 {role: "OWNER", project: "project-fixture", want: all},
		"administrator":         {role: "ADMINISTRATOR", project: "project-fixture", want: all},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := &actionPermissionsFixture{t: t, failure: test.failure, permissions: test.permissions}
			got, err := (&Repository{}).projectActionPermissions(t.Context(), fixture,
				scope{organizationID: "organization-fixture", actorID: "actor-fixture", role: test.role}, test.project)
			if got != test.want || !errors.Is(err, test.wantError) || fixture.called != test.queried {
				t.Fatalf("presentation outcome: permissions=%+v error=%v queried=%t", got, err, fixture.called)
			}
		})
	}
}
