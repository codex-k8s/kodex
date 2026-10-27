package platform

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	revisionservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/revision"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// TestManagedRevisionScannersPreservePartialResults checks four real scanners against literal
// column/value oracles for valid/null/empty/malformed/partially decoded diagnostics, partial driver
// failures and missing rows. It preserves their intentionally different zero/partial projections
// and raw-versus-Unavailable error policies without a database or query execution.
// TestManagedRevisionScannersPreservePartialResults проверяет четыре настоящих scanner по буквальным
// column/value oracles для valid/null/empty/malformed/частично декодированных diagnostics, partial
// driver failures и missing rows. Сохраняет разные zero/partial projections и raw-versus-Unavailable
// error policies без БД либо исполнения запросов.
func TestManagedRevisionScannersPreservePartialResults(t *testing.T) {
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	validated, published, updated := created.Add(time.Hour), created.Add(2*time.Hour), created.Add(3*time.Hour)
	rowFailure := errors.New("synthetic managed scan failure")
	for _, scanner := range []string{"revision", "published", "copy", "binding"} {
		for _, test := range []struct {
			name, raw, failure string
			diagnostics        []string
			rowError           error
			assign             bool
		}{
			{"valid", `["one","two"]`, "", []string{"one", "two"}, nil, true},
			{"null", `null`, "", nil, nil, true},
			{"empty", `[]`, "", []string{}, nil, true},
			{"syntax", `{`, "syntax", nil, nil, true},
			{"partial JSON", `["one",1]`, "type", []string{"one", ""}, nil, true},
			{"partial driver", `not JSON`, "row", nil, rowFailure, true},
			{"missing", `not JSON`, "row", nil, pgx.ErrNoRows, false},
		} {
			t.Run(scanner+"/"+test.name, func(t *testing.T) {
				values := []any{"revision-id", "mrev_fixture01", int64(7), "VALID", "JSON", `{"fixture":true}`, "content-digest", "mrev_parent01", []byte(test.raw), created, &validated, &published}
				setValues := []any{"set-id", "mcfg_fixture01", "project-id", "prj_fixture01", "ROLE_IMAGE", "Synthetic set", "UI", "control-center", "source-revision", int64(9), updated}
				provenance := &entity.ManagedConfigurationCopyProvenance{Origin: "UI", SourceRef: "source-ref", SourceRevision: "source-revision", SourceVersion: 3, SourceDigest: "source-digest"}
				switch scanner {
				case "published":
					values = append(values, int64(12), updated)
				case "copy":
					values = append(setValues, values...)
				case "binding":
					prefix := append([]any{"binding-ref", int64(6), "RUNTIME_ENVIRONMENT", "env-ref"}, setValues...)
					prefix = append(prefix, "current-id")
					values = append(prefix, values...)
					values = append(values, true, provenance)
				}
				columns := map[string]int{"revision": 12, "published": 14, "copy": 23, "binding": 30}[scanner]
				row := proofResolutionRow(func(destinations ...any) error {
					if len(destinations) != columns || len(values) != columns {
						t.Fatal("managed scan column count changed")
					}
					if test.assign {
						for i, value := range values {
							target, source := reflect.ValueOf(destinations[i]), reflect.ValueOf(value)
							if target.Kind() != reflect.Pointer || target.IsNil() || target.Elem().Type() != source.Type() {
								t.Fatal("managed scan column type or destination changed")
							}
							target.Elem().Set(source)
						}
					}
					return test.rowError
				})
				wantRevision := managedRevisionScan{}
				wantSet := managedSet{}
				var wantVersion int64
				var wantUpdated time.Time
				if test.assign {
					wantRevision = managedRevisionScan{internalID: "revision-id", ManagedConfigurationRevision: entity.ManagedConfigurationRevision{Ref: "mrev_fixture01", Revision: 7, State: "VALID", ContentFormat: "JSON", Content: `{"fixture":true}`, Digest: "content-digest", ParentRevisionRef: "mrev_parent01", ValidationDiagnostics: test.diagnostics, CreatedAt: created, ValidatedAt: &validated, PublishedAt: &published}}
					wantSet = managedSet{id: "set-id", projectID: "project-id", ManagedConfigurationSet: entity.ManagedConfigurationSet{Ref: "mcfg_fixture01", ProjectRef: "prj_fixture01", Kind: "ROLE_IMAGE", Name: "Synthetic set", ManagedBy: "UI", Source: "control-center", SourceRevision: "source-revision", Version: 9, UpdatedAt: updated}}
					wantVersion, wantUpdated = 12, updated
				}
				var got, want any
				var err error
				switch scanner {
				case "revision":
					got, err = scanManagedRevision(row)
					if test.failure == "syntax" || test.failure == "type" {
						wantRevision = managedRevisionScan{}
					}
					want = wantRevision
				case "published":
					item, version, stamp, scanErr := scanPublishedManagedRevision(row)
					got, want, err = []any{item, version, stamp}, []any{wantRevision, wantVersion, wantUpdated}, scanErr
				case "copy":
					set, revision, scanErr := scanManagedCopy(row)
					got, want, err = []any{set, revision}, []any{wantSet, wantRevision.ManagedConfigurationRevision}, scanErr
				case "binding":
					got, err = scanManagedBinding(row)
					binding := entity.ManagedConfigurationBindingSnapshot{}
					if test.assign {
						wantSet.Archived, wantSet.CopyProvenance = true, provenance
						binding = entity.ManagedConfigurationBindingSnapshot{Ref: "binding-ref", Version: 6, ConsumerKind: "RUNTIME_ENVIRONMENT", ConsumerRef: "env-ref", Configuration: wantSet.ManagedConfigurationSet, Revision: wantRevision.ManagedConfigurationRevision}
					}
					want = binding
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("managed scanner changed positional projection, empty shape or partial-result policy")
				}
				switch test.failure {
				case "row":
					if !errors.Is(err, test.rowError) {
						t.Fatal("managed scanner changed the row error")
					}
				case "syntax", "type":
					if scanner == "copy" {
						var syntax *json.SyntaxError
						var mismatch *json.UnmarshalTypeError
						if test.failure == "syntax" && !errors.As(err, &syntax) || test.failure == "type" && !errors.As(err, &mismatch) || errors.Is(err, errs.ErrUnavailable) {
							t.Fatal("managed copy changed raw JSON error mapping")
						}
					} else if !errors.Is(err, errs.ErrUnavailable) {
						t.Fatal("managed scanner changed malformed diagnostics mapping")
					}
				default:
					if err != nil {
						t.Fatal("managed scanner rejected valid synthetic columns")
					}
				}
			})
		}
	}
}

// TestManagedContentFormatsPreserveKindPolicy uses seven literal kind/format allowlists including
// unknown and empty kinds, and rejects lowercase/padded/unknown formats. It checks only the common
// predicate, preserving the callers' separate CREATE/SAVE content and ownership validation.
// TestManagedContentFormatsPreserveKindPolicy использует семь буквальных kind/format allowlists,
// включая unknown и empty kinds, и отклоняет lowercase/padded/unknown formats. Проверяет только общий
// predicate, сохраняя отдельную CREATE/SAVE content и ownership validation у callers.
func TestManagedContentFormatsPreserveKindPolicy(t *testing.T) {
	for _, test := range []struct {
		kind    string
		formats []string
	}{
		{revisionservice.KindPromptTemplate, []string{"TEXT"}},
		{revisionservice.KindIntegrationDefinition, []string{"JSON", "YAML", "TOML", "OPENAPI_IMPORT"}},
		{revisionservice.KindRoleImage, []string{"JSON", "YAML", "TOML"}},
		{revisionservice.KindSystemSTT, []string{"JSON", "YAML", "TOML"}},
		{revisionservice.KindEmailMailbox, []string{"JSON", "YAML", "TOML"}},
		{"UNKNOWN", []string{"JSON", "YAML", "TOML"}},
		{"", []string{"JSON", "YAML", "TOML"}},
	} {
		for _, format := range []string{"TEXT", "JSON", "YAML", "TOML", "OPENAPI_IMPORT", "", "json", "text", " JSON", "JSON ", "openapi_import", "BINARY"} {
			t.Run(test.kind+"/"+format, func(t *testing.T) {
				if managedContentFormatAllowed(test.kind, format) != slices.Contains(test.formats, format) {
					t.Fatal("managed content format policy changed or normalized its input")
				}
			})
		}
	}
}
