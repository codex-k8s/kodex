package platform

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestManagedRoleImageSourcePreservesValidationOrder checks catalog availability before parsing,
// strict parse before resolution, exact resolved input and existing error mapping through the new
// helper and the real validation entrypoint, without database or registry access.
// TestManagedRoleImageSourcePreservesValidationOrder проверяет доступность каталога до parsing,
// строгий parse до resolution, точный resolved input и прежние ошибки через новый helper и
// настоящий validation entrypoint без доступа к БД или registry.
func TestManagedRoleImageSourcePreservesValidationOrder(t *testing.T) {
	catalog, recipe := promotionComponentCatalog(t)
	const content = `{"name":"Synthetic managed image","roleImage":{"roleDefinitionRef":"role_fixture123","environment":{"environmentKey":"promotion"}}}`
	selection := entity.RoleEnvironmentSelection{EnvironmentKey: "promotion"}
	for _, test := range []struct {
		name, body, project      string
		missing, reject, corrupt bool
		want                     error
		calls                    int
	}{
		{name: "missing catalog before parse", body: "{", project: "prj_fixture123", missing: true, want: errs.ErrUnavailable},
		{name: "malformed source", body: "{", project: "prj_fixture123", want: errs.ErrInvalid},
		{name: "unknown field", body: strings.TrimSuffix(content, "}") + `,"unknown":true}`, project: "prj_fixture123", want: errs.ErrInvalid},
		{name: "resolver failure", body: content, project: "prj_fixture123", reject: true, want: errs.ErrInvalid, calls: 1},
		{name: "resolved recipe invalid", body: content, project: "prj_fixture123", corrupt: true, want: errs.ErrInvalid, calls: 1},
		{name: "project required", body: content, want: errs.ErrInvalid, calls: 1},
		{name: "role required", body: strings.Replace(content, "role_fixture123", "invalid", 1), project: "prj_fixture123", want: errs.ErrInvalid, calls: 1},
		{name: "valid exact input", body: content, project: "prj_fixture123", calls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &Repository{}
			calls := 0
			if !test.missing {
				repository.roleImageCatalogResolver = func(got entity.RoleEnvironmentSelection) (entity.RoleImageRecipeInput, error) {
					calls++
					if !reflect.DeepEqual(got, selection) {
						t.Fatal("source catalog selection changed")
					}
					if test.reject {
						return entity.RoleImageRecipeInput{}, errors.New("synthetic resolver failure")
					}
					gotRecipe, err := catalog.Resolve(got)
					if test.corrupt {
						gotRecipe.BaseImageDigest = ""
					}
					return gotRecipe, err
				}
			}
			set := managedSet{}
			set.ProjectRef = test.project
			got, err := repository.sourceRoleImageInput(set, "JSON", test.body)
			if !errors.Is(err, test.want) || calls != test.calls {
				t.Fatal("source validation order or error mapping changed")
			}
			want := roleimagerepo.ManageInput{}
			if test.want == nil {
				want = roleimagerepo.ManageInput{ScopeKind: "PROJECT", Action: "CREATE", ProjectRef: test.project, RoleDefinitionRef: "role_fixture123", Name: "Synthetic managed image", Recipe: recipe, Environment: selection}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("source input fields or zero result on failure changed")
			}
			if err := repository.validateSourceRoleImage(set, "JSON", test.body); !errors.Is(err, test.want) || calls != 2*test.calls {
				t.Fatal("real validation entrypoint changed source behavior")
			}
		})
	}
}

// TestAttachmentItemScannerPreservesTupleAndFailure checks all ten positional values and INPUT
// scope through the existing callback-row fixture, rejecting partial projections on row failure.
// TestAttachmentItemScannerPreservesTupleAndFailure проверяет все десять позиционных значений и
// INPUT scope существующей callback-row оснасткой и отсутствие частичной проекции при row failure.
func TestAttachmentItemScannerPreservesTupleAndFailure(t *testing.T) {
	want := attachmentSetItem{ArtifactID: "artifact-id", RunnerInputArtifact: runtimecontract.RunnerInputArtifact{Ref: "artifact-ref", Revision: 3, Version: 7, FileName: "file.txt", MediaType: "text/plain", SizeBytes: 42, Digest: "digest", Source: "source", Position: 5, Scope: runtimecontract.AttachmentScopeInput}}
	failure := errors.New("synthetic attachment row failure")
	for _, failed := range []bool{false, true} {
		row := proofResolutionRow(func(dest ...any) error {
			if len(dest) != 10 {
				t.Fatal("attachment scan column count changed")
			}
			*dest[0].(*string) = want.ArtifactID
			*dest[1].(*string) = want.Ref
			*dest[2].(*int64) = want.Revision
			*dest[3].(*int64) = want.Version
			*dest[4].(*string) = want.FileName
			*dest[5].(*string) = want.MediaType
			*dest[6].(*int64) = want.SizeBytes
			*dest[7].(*string) = want.Digest
			*dest[8].(*string) = want.Source
			*dest[9].(*int64) = want.Position
			if failed {
				return failure
			}
			return nil
		})
		got, err := scanAttachmentSetItem(row)
		if failed {
			if !errors.Is(err, failure) || !reflect.DeepEqual(got, attachmentSetItem{}) {
				t.Fatal("failed attachment scan exposed a partial result or changed its error")
			}
		} else if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("attachment scan changed positional fields or INPUT scope")
		}
	}
}

// TestImageRiskArtifactLookupPreservesOwnerAndScope checks exact tenant/query/context arguments,
// distinct missing/driver/decode failures and project/organization scope matching without commit.
// TestImageRiskArtifactLookupPreservesOwnerAndScope проверяет точные tenant/query/context args,
// различие missing/driver/decode failures и project/organization scope без commit.
func TestImageRiskArtifactLookupPreservesOwnerAndScope(t *testing.T) {
	failure := errors.New("synthetic image row failure")
	for _, test := range []struct {
		name, recipe, kind, project, requestedKind, requestedProject string
		rowError                                                     error
		badJSON                                                      bool
		want                                                         error
	}{
		{name: "project", recipe: "recipe", kind: "PROJECT", project: "project", requestedKind: "PROJECT", requestedProject: "project"},
		{name: "organization", recipe: "recipe", kind: "ORGANIZATION", requestedKind: "ORGANIZATION"},
		{name: "missing", rowError: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "driver failure", rowError: failure, want: errs.ErrUnavailable},
		{name: "decode failure", badJSON: true, want: errs.ErrUnavailable},
		{name: "wrong recipe", recipe: "other", kind: "PROJECT", project: "project", requestedKind: "PROJECT", requestedProject: "project", want: errs.ErrNotFound},
		{name: "wrong scope", recipe: "recipe", kind: "ORGANIZATION", requestedKind: "PROJECT", want: errs.ErrNotFound},
		{name: "wrong project", recipe: "recipe", kind: "PROJECT", project: "other", requestedKind: "PROJECT", requestedProject: "project", want: errs.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			tx := &proofResolutionTx{t: t, ctx: ctx, query: queryRoleImagesLockArtifact, args: []any{"owner-id", "artifact-ref"}}
			tx.row = proofResolutionRow(func(dest ...any) error {
				if len(dest) != 51 {
					t.Fatal("locked artifact tuple changed")
				}
				if test.rowError != nil {
					return test.rowError
				}
				*dest[0].(*string) = "artifact-id"
				*dest[1].(*string) = "artifact-ref"
				*dest[2].(*string) = test.recipe
				*dest[9].(*[]byte) = []byte("{}")
				if test.badJSON {
					*dest[9].(*[]byte) = []byte("{")
				}
				*dest[48].(*string) = test.kind
				*dest[49].(*string) = "organization-ref"
				*dest[50].(*string) = test.project
				return nil
			})
			got, err := lockImageRiskArtifact(ctx, tx, scope{organizationID: "owner-id"}, "artifact-ref", "recipe", test.requestedKind, test.requestedProject)
			if !errors.Is(err, test.want) || tx.queryCalls != 1 || tx.commitCalls != 0 {
				t.Fatal("image risk owner query or failure mapping changed")
			}
			want := lockedArtifact{}
			if test.want == nil {
				want = lockedArtifact{ID: "artifact-id", Artifact: entity.ImageArtifact{Ref: "artifact-ref", RecipeRef: test.recipe, ScopeKind: test.kind, OrganizationRef: "organization-ref", ProjectRef: test.project}}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("image risk lookup changed projection or leaked partial owner state")
			}
		})
	}
}
