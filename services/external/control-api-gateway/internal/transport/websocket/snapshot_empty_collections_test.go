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

type emptySnapshotRecorder struct{ organizationSnapshotRecorder }

func (client *emptySnapshotRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	if out, ok := response.(*cp.ListRuntimeSecretsResponse); ok {
		out.Secrets, out.Page = []*cp.RuntimeSecret{}, &cp.PageInfo{}
		return nil
	}
	return client.organizationSnapshotRecorder.Invoke(ctx, method, request, response, options...)
}

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
