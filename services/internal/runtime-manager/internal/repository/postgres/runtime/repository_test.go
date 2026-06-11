package runtime

import (
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}
	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestJobClaimQueryRequiresStrictBuildDeploySpecShape(t *testing.T) {
	t.Parallel()

	disallowed := []string{
		"job_input_json ? 'build_execution_spec'",
		"job_input_json ? 'deploy_execution_spec'",
	}
	for _, fragment := range disallowed {
		if strings.Contains(queryJobClaim, fragment) {
			t.Fatalf("job claim query still uses weak presence guard %q", fragment)
		}
	}

	required := []string{
		"jsonb_typeof(build_spec) = 'object'",
		"AND job_type <> 'deploy'",
		"jsonb_typeof(deploy_spec) = 'object'",
		"build_spec->>'source_ref' <> ''",
		"(build_spec->>'source_commit_sha') ~* '^([0-9a-f]{40}|[0-9a-f]{64})$'",
		"(build_spec->>'build_context_digest') ~* '^sha256:[0-9a-f]{64}$'",
		"(build_spec->>'build_plan_fingerprint') ~* '^sha256:[0-9a-f]{64}$'",
		"deploy_spec->>'source_ref' <> ''",
		"(deploy_spec->>'image_digest') ~* '^sha256:[0-9a-f]{64}$'",
		"(deploy_spec->>'manifest_digest') ~* '^sha256:[0-9a-f]{64}$'",
		"(deploy_spec->>'deploy_plan_fingerprint') ~* '^sha256:[0-9a-f]{64}$'",
		"deploy_spec->>'manifest_bundle_ref' <> ''",
		"(deploy_spec->>'manifest_bundle_digest') ~* '^sha256:[0-9a-f]{64}$'",
		"jsonb_array_length(deploy_spec->'rollout_targets') > 0",
		"jsonb_array_length(deploy_spec->'expected_image_refs') > 0",
	}
	for _, fragment := range required {
		if !strings.Contains(queryJobClaim, fragment) {
			t.Fatalf("job claim query is missing strict spec guard %q", fragment)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
			var pgErr *pgconn.PgError
			if errors.As(tc.err, &pgErr) && !errors.As(wrapError("test operation", tc.err), &pgErr) {
				t.Fatalf("wrapError() lost postgres cause")
			}
		})
	}
}
