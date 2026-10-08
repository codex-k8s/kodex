package platform

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

type actionPermissionsFixture struct {
	pgx.Tx
	t           *testing.T
	failure     error
	permissions []string
	called      bool
}

func (fixture *actionPermissionsFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	fixture.t.Helper()
	fixture.called = true
	if sql != queryQueriesProjectActionPermissionsSelectMembershipsOrganizationIdRef || len(args) != 3 ||
		args[0] != "organization-fixture" || args[1] != "project-fixture" || args[2] != "actor-fixture" {
		fixture.t.Fatal("presentation query lost exact organization/project/actor pins")
	}
	return fixture
}

func (fixture *actionPermissionsFixture) Scan(dest ...any) error {
	if fixture.failure != nil {
		return fixture.failure
	}
	*dest[0].(*[]string) = fixture.permissions
	return nil
}

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
