package websockettransport

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
)

// emptySnapshotRecorder decorates organizational fixtures with an explicitly empty project-secret response, keeping absent and empty wire collections distinguishable.
// emptySnapshotRecorder дополняет организационную оснастку явно пустым ответом проектных секретов, сохраняя различие отсутствующих и пустых wire-коллекций.
type emptySnapshotRecorder struct{ organizationSnapshotRecorder }

// Invoke returns an explicit empty secret slice/page for the project listing and delegates other requests to the synthetic organizational recorder.
// Invoke возвращает явные пустые список секретов и страницу проектного чтения, передавая остальные запросы тестовому организационному recorder.
func (client *emptySnapshotRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	if out, ok := response.(*cp.ListRuntimeSecretsResponse); ok {
		out.Secrets, out.Page = []*cp.RuntimeSecret{}, &cp.PageInfo{}
		return nil
	}
	return client.organizationSnapshotRecorder.Invoke(ctx, method, request, response, options...)
}

// TestSnapshotOptionalCollectionsKeepEmptyDistinctFromAbsent enumerates generated catalog slice fields via reflection and requires an explicit empty slice to serialize as [] without synthesizing absent collection fields.
// TestSnapshotOptionalCollectionsKeepEmptyDistinctFromAbsent через reflection перечисляет slice-поля generated-каталога и требует сериализовать явный пустой slice как [] без появления отсутствующих коллекций.
func TestSnapshotOptionalCollectionsKeepEmptyDistinctFromAbsent(t *testing.T) {
	t.Parallel()
	fields := reflect.TypeFor[generated.PlatformCatalogSnapshot]()
	for index := 0; index < fields.NumField(); index++ {
		field := fields.Field(index)
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		t.Run(name, func(t *testing.T) {
			snapshot, err := typedPlatformSnapshot("WORKFLOW", map[string]any{"catalog": map[string]any{name: []any{}}})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Catalog map[string]json.RawMessage `json:"catalog"`
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if string(wire.Catalog[name]) != "[]" || len(wire.Catalog) != 1 {
				t.Fatal("empty collection vanished or absent collections appeared")
			}
		})
	}
}

// TestOrganizationSnapshotEmptyCatalogsSurviveWireSerialization checks empty project and organization recipe/secret catalogs remain explicit [] through owner-fixture projection, typed conversion and JSON serialization.
// TestOrganizationSnapshotEmptyCatalogsSurviveWireSerialization проверяет сохранение пустых проектных и организационных рецептов и секретов как явных [] через owner-оснастку, типизированную проекцию и JSON.
func TestOrganizationSnapshotEmptyCatalogsSurviveWireSerialization(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ kind, projectItems, organizationItems string }{
		{"ROLE_IMAGE_RECIPE", "recipes", "organizationRecipes"},
		{"RUNTIME_SECRET", "secrets", "organizationSecrets"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			client := &emptySnapshotRecorder{organizationSnapshotRecorder: organizationSnapshotRecorder{
				recipes: &cp.ListOrganizationRoleImageRecipesResponse{Page: &cp.PageInfo{}},
				secrets: &cp.ListOrganizationRuntimeSecretsResponse{Page: &cp.PageInfo{}},
			}}
			server := &Server{
				query:      cp.NewPlatformQueryServiceClient(client),
				roleImages: cp.NewRoleImageServiceClient(client),
			}
			value, err := server.projectPlatformSnapshot(t.Context(), test.kind, "", func(value string) string { return value })
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := typedPlatformSnapshot(test.kind, value)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Catalog map[string]json.RawMessage `json:"catalog"`
			}
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{test.projectItems, test.organizationItems} {
				if string(wire.Catalog[key]) != "[]" {
					t.Fatalf("empty scoped collection is missing: %s", key)
				}
			}
		})
	}
}
