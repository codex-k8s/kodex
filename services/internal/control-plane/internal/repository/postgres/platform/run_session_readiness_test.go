package platform

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

type sessionReadinessFixture struct {
	values  []any
	failure error
	called  bool
}

func (fixture *sessionReadinessFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("unexpected unbounded query")
}
func (fixture *sessionReadinessFixture) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	fixture.called = true
	if sql != queryRunSessionReadiness || len(args) != 1 {
		panic("unexpected readiness query")
	}
	pins := args[0].(pgx.StrictNamedArgs)
	if pins["organization_id"] != "organization-fixture" || pins["run_ref"] != "run_fixture01" || pins["session_ref"] != "ses_fixture01" {
		panic("readiness owner binding lost")
	}
	return fixture
}
func (fixture *sessionReadinessFixture) Scan(dest ...any) error {
	if fixture.failure != nil {
		return fixture.failure
	}
	for i, v := range fixture.values {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

func TestRunSessionReadinessProjectionClosed(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		state            string
		account, earlier bool
		reason           string
	}{
		"untracked":        {"UNTRACKED", true, false, "NO_SESSION_BLOCKER"},
		"live":             {"LIVE", true, false, "NO_SESSION_BLOCKER"},
		"archive":          {"ARCHIVED", true, false, "STORAGE_NOT_LIVE"},
		"terminal failure": {"ERROR", true, false, "STORAGE_NOT_LIVE"},
		"purged":           {"PURGED", true, false, "STORAGE_NOT_LIVE"},
		"account":          {"LIVE", false, false, "SESSION_ACCOUNT_UNAVAILABLE"},
		"predecessor":      {"LIVE", true, true, "EARLIER_EXECUTION"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := &sessionReadinessFixture{values: []any{"ses_fixture01", c.state, c.account, c.earlier, "sat_fixture01", "SNAPSHOT", "DEAD_LETTER", int32(5), int32(5), "private sentinel password=secret"}}
			run := entity.Run{Ref: "run_fixture01", SessionRef: "ses_fixture01"}
			if err := attachRunSessionReadiness(t.Context(), fixture, scope{organizationID: "organization-fixture"}, &run); err != nil {
				t.Fatal(err)
			}
			if run.SessionReadiness.Reason != c.reason || run.SessionReadiness.LatestArchiveTask.Ref != "sat_fixture01" || run.SessionReadiness.LatestArchiveTask.SafeErrorCode != "UNKNOWN" {
				t.Fatal("closed session projection mismatch")
			}
			raw, _ := json.Marshal(run.SessionReadiness)
			if strings.Contains(string(raw), "private sentinel") || strings.Contains(string(raw), "password") || strings.Contains(string(raw), "secret") {
				t.Fatal("private error leaked")
			}
		})
	}
}

func TestRunSessionReadinessUnknownFailsClosed(t *testing.T) {
	t.Parallel()
	for _, mutate := range []func(*sessionReadinessFixture){
		func(f *sessionReadinessFixture) { f.values[0] = "ses_foreign01" },
		func(f *sessionReadinessFixture) { f.values[1] = "private-sentinel" },
		func(f *sessionReadinessFixture) { f.values[4] = "private/payload" },
		func(f *sessionReadinessFixture) { f.values[5] = "DELETE_OBJECT" },
		func(f *sessionReadinessFixture) { f.values[6] = "private-sentinel" },
		func(f *sessionReadinessFixture) { f.values[7] = int32(6) },
		func(f *sessionReadinessFixture) { f.values[8] = int32(6) },
		func(f *sessionReadinessFixture) { f.failure = errors.New("private database sentinel") },
	} {
		fixture := &sessionReadinessFixture{values: []any{"ses_fixture01", "LIVE", true, false, "sat_fixture01", "SNAPSHOT", "READY", int32(0), int32(5), ""}}
		mutate(fixture)
		run := entity.Run{Ref: "run_fixture01", SessionRef: "ses_fixture01"}
		err := attachRunSessionReadiness(t.Context(), fixture, scope{organizationID: "organization-fixture"}, &run)
		if !errors.Is(err, errs.ErrUnavailable) || run.SessionReadiness != nil || strings.Contains(err.Error(), "sentinel") {
			t.Fatal("invalid session projection was not closed")
		}
	}
	fixture := &sessionReadinessFixture{}
	run := entity.Run{}
	if err := attachRunSessionReadiness(t.Context(), fixture, scope{}, &run); err != nil || fixture.called || run.SessionReadiness != nil {
		t.Fatal("missing session guessed")
	}
}
