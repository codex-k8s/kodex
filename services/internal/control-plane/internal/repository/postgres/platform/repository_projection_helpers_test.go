package platform

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	digestfixture "github.com/codex-k8s/kodex/services/internal/control-plane/testdata/digests"
	"github.com/jackc/pgx/v5"
)

// TestRoleImageRecipeProjectionPreservesPinsAndCopiesSlices checks all six provenance fields,
// unrelated artifact state, independent platform/tool slices and the historical empty-to-nil shape.
// TestRoleImageRecipeProjectionPreservesPinsAndCopiesSlices проверяет шесть provenance fields,
// прочее состояние артефакта, независимость platform/tool slices и прежнюю empty-to-nil форму.
func TestRoleImageRecipeProjectionPreservesPinsAndCopiesSlices(t *testing.T) {
	for _, shape := range []string{"nil", "empty", "populated"} {
		t.Run(shape, func(t *testing.T) {
			recipe := entity.RoleImageRecipeInput{BaseImageDigest: "base", SourceSHA256: "source",
				ContextSHA256: "context", BuilderSHA256: "builder", FrontendSHA256: "frontend", ToolchainSHA256: "toolchain"}
			var platforms []entity.RoleImagePlatform
			var tools []entity.RoleImageTool
			if shape == "empty" {
				recipe.Platforms, recipe.Tools = []entity.RoleImagePlatform{}, []entity.RoleImageTool{}
			} else if shape == "populated" {
				recipe.Platforms = []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64", Variant: "fixture"}}
				recipe.Tools = []entity.RoleImageTool{{Name: "tool", Version: "version", SourceRef: "source", SHA256: "digest"}}
				platforms = []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64", Variant: "fixture"}}
				tools = []entity.RoleImageTool{{Name: "tool", Version: "version", SourceRef: "source", SHA256: "digest"}}
			}
			artifact := entity.ImageArtifact{Ref: "artifact", Version: 7, ToolInventorySHA256: "inventory"}
			copyRoleImageArtifactRecipe(&artifact, recipe)
			want := entity.ImageArtifact{Ref: "artifact", Version: 7, ToolInventorySHA256: "inventory",
				BaseImageDigest: "base", SourceSHA256: "source", ContextSHA256: "context", BuilderSHA256: "builder",
				FrontendSHA256: "frontend", ToolchainSHA256: "toolchain", Platforms: platforms, DeclaredTools: tools}
			if !reflect.DeepEqual(artifact, want) {
				t.Fatal("recipe provenance projection changed fields or empty slice shape")
			}
			if shape == "populated" {
				recipe.Platforms[0].OS = "changed"
				recipe.Tools[0].Name = "changed"
				if artifact.Platforms[0].OS != "linux" || artifact.DeclaredTools[0].Name != "tool" {
					t.Fatal("artifact projection aliases saved recipe slices")
				}
			}
		})
	}
}

// TestRoleImageAndProviderDigestGrammar checks literal digest boundaries through role-image,
// manifest and provider configuration paths, retaining exact accepted metadata and failure state.
// TestRoleImageAndProviderDigestGrammar проверяет буквальные digest boundaries через role-image,
// manifest и provider configuration paths, сохраняя точные принятые метаданные и состояние при отказе.
func TestRoleImageAndProviderDigestGrammar(t *testing.T) {
	cases := digestfixture.SHA256TextCases()
	if len(cases) != 14 {
		t.Fatal("synthetic digest corpus is incomplete")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			if exactSHA256(test.Text) != test.Valid || exactManifestDigest("sha256:"+test.Text) != test.Valid {
				t.Fatal("role-image digest grammar changed")
			}
			previous := ProviderCredentialConfig{SecretName: "bootstrap-provider", SecretUID: "00000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("9", 64)}
			repository := &Repository{providerCredential: previous}
			candidate := previous
			candidate.ContentSHA256 = test.Text
			err := repository.ConfigureProviderCredential(candidate)
			if (err == nil) != test.Valid {
				t.Fatal("provider configuration digest grammar changed")
			}
			if test.Valid {
				if repository.providerCredential != candidate {
					t.Fatal("accepted credential metadata changed")
				}
			} else if err.Error() != "provider credential metadata is invalid" || repository.providerCredential != previous {
				t.Fatal("invalid digest changed public error or stored credential metadata")
			}
		})
	}
}

// proofResolutionRow adapts a test scan callback to pgx.Row without opening a database connection.
// proofResolutionRow связывает test scan callback с pgx.Row без открытия соединения с БД.
type proofResolutionRow func(...any) error

// Scan forwards the exact destination list to the fixture's callback and returns its synthetic error.
// Scan передаёт точный список destinations callback оснастки и возвращает его синтетическую ошибку.
func (row proofResolutionRow) Scan(destinations ...any) error { return row(destinations...) }

// proofResolutionTx observes only the shared resolution tail's query and commit calls; the embedded
// unused transaction remains nil so unexpected database operations cannot silently succeed.
// proofResolutionTx наблюдает только query и commit общего resolution tail; вложенная неиспользуемая
// транзакция остаётся nil, чтобы неожиданные операции БД не могли незаметно завершиться успешно.
type proofResolutionTx struct {
	pgx.Tx
	t           *testing.T
	ctx         context.Context
	query       string
	args        []any
	row         pgx.Row
	commitError error
	queryCalls  int
	commitCalls int
}

// QueryRow checks the exact context, SQL identity and owner-scoped arguments before exposing the row.
// QueryRow проверяет точные context, SQL identity и owner-scoped arguments до выдачи строки.
func (tx *proofResolutionTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.t.Helper()
	tx.queryCalls++
	if ctx != tx.ctx || query != tx.query || !reflect.DeepEqual(args, tx.args) {
		tx.t.Fatal("proof resolution changed its query identity, context or owner arguments")
	}
	return tx.row
}

// Commit records the attempt using the original caller context and returns the configured outcome.
// Commit учитывает попытку с исходным caller context и возвращает заданный исход.
func (tx *proofResolutionTx) Commit(ctx context.Context) error {
	tx.t.Helper()
	tx.commitCalls++
	if ctx != tx.ctx {
		tx.t.Fatal("proof resolution changed its commit context")
	}
	return tx.commitError
}

// TestProofIdentityResolutionTailPreservesCommitAndAuthority checks both access modes and all
// success/denial/read/commit failures, including commit-before-denial and zero output on failure.
// TestProofIdentityResolutionTailPreservesCommitAndAuthority проверяет оба access modes и исходы
// success/denial/read/commit failure, включая commit перед отказом и нулевой результат при ошибке.
func TestProofIdentityResolutionTailPreservesCommitAndAuthority(t *testing.T) {
	readFailure := errors.New("synthetic read failure")
	commitFailure := errors.New("synthetic commit failure")
	for _, test := range []struct {
		name      string
		project   bool
		allowed   bool
		readErr   error
		commitErr error
		wantErr   error
		commits   int
	}{
		{"project allowed", true, true, nil, nil, nil, 1},
		{"project missing", true, false, pgx.ErrNoRows, nil, errs.ErrForbidden, 1},
		{"project read failure", true, false, readFailure, nil, errs.ErrUnavailable, 0},
		{"project allowed commit failure", true, true, nil, commitFailure, errs.ErrConflict, 1},
		{"project missing commit failure", true, false, pgx.ErrNoRows, commitFailure, errs.ErrConflict, 1},
		{"active binding", false, true, nil, nil, nil, 1},
		{"no active binding", false, false, nil, nil, errs.ErrForbidden, 1},
		{"binding read failure", false, false, readFailure, nil, errs.ErrUnavailable, 0},
		{"binding allowed commit failure", false, true, nil, commitFailure, errs.ErrConflict, 1},
		{"binding denied commit failure", false, false, nil, commitFailure, errs.ErrConflict, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			original := platformrepo.ProofAuthority{ActorID: "actor", OrganizationID: "organization", ActorVersion: 13, OrganizationVersion: 7,
				RuntimeExecution: &platformrepo.RuntimeExecutionProof{}}
			projectRef := ""
			tx := &proofResolutionTx{t: t, ctx: ctx, query: queryProofSubjectHasActiveBinding,
				args: []any{original.OrganizationID, original.ActorID}, commitError: test.commitErr}
			if test.project {
				projectRef = "public-project"
				tx.query = queryAuthorizeProjectMembership
				tx.args = []any{projectRef, original.OrganizationID, original.ActorID}
			}
			tx.row = proofResolutionRow(func(destinations ...any) error {
				if test.readErr != nil {
					return test.readErr
				}
				if test.project {
					if len(destinations) != 2 {
						t.Fatal("project resolution changed scan shape")
					}
					*destinations[0].(*string), *destinations[1].(*uint64) = "project-id", 5
				} else {
					if len(destinations) != 1 {
						t.Fatal("binding resolution changed scan shape")
					}
					*destinations[0].(*bool) = test.allowed
				}
				return nil
			})
			got, err := (&Repository{}).finishProofIdentityResolution(ctx, tx, projectRef, original)
			if !errors.Is(err, test.wantErr) || tx.queryCalls != 1 || tx.commitCalls != test.commits {
				t.Fatal("proof resolution changed failure precedence or transaction order")
			}
			if err != nil {
				if got != (platformrepo.ProofAuthority{}) {
					t.Fatal("failed resolution exposed partial authority")
				}
			} else {
				want := original
				want.ActorVersion = 1
				if test.project {
					want.ProjectID, want.ProjectVersion = "project-id", 5
				}
				if got != want {
					t.Fatal("successful resolution changed owner authority fields")
				}
			}
		})
	}
}
