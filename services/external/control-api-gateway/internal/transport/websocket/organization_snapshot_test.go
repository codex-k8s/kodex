package websockettransport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type organizationSnapshotRecorder struct {
	grpc.ClientConnInterface
	calls    []string
	requests []proto.Message
	secrets  *cp.ListOrganizationRuntimeSecretsResponse
	recipes  *cp.ListOrganizationRoleImageRecipesResponse
	failure  error
}

func organizationSnapshotSecret(project bool) *cp.RuntimeSecret {
	now := timestamppb.New(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	s := &cp.RuntimeSecret{ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_fixture01", Ref: "sec_orgfixture01", Name: "TOKEN", State: "ACTIVE", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, Version: 1, CurrentRevision: 1, CreatedAt: now, UpdatedAt: now}
	if project {
		s.ScopeKind, s.ProjectRef, s.Ref = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, "prj_fixture01", "sec_prjfixture01"
	}
	return s
}

func organizationSnapshotRecipe(project bool) *cp.RoleImageRecipe {
	now := timestamppb.New(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	r := &cp.RoleImageRecipe{ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, OrganizationRef: "org_fixture01", Ref: "imgrec_orgfixture01", Name: "Image", State: "ACTIVE", Version: 1, Generation: 1, CreatedAt: now, UpdatedAt: now, Environment: &cp.RoleEnvironmentSelection{EnvironmentKey: "standard"}}
	if project {
		r.ScopeKind, r.ProjectRef, r.Ref = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, "prj_fixture01", "imgrec_prjfixture01"
	}
	return r
}

func (c *organizationSnapshotRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	c.calls = append(c.calls, method)
	c.requests = append(c.requests, proto.Clone(request.(proto.Message)))
	switch out := response.(type) {
	case *cp.ListOrganizationRuntimeSecretsResponse:
		if c.failure != nil {
			return c.failure
		}
		if c.secrets == nil {
			c.secrets = &cp.ListOrganizationRuntimeSecretsResponse{Secrets: []*cp.RuntimeSecret{organizationSnapshotSecret(false)}, Page: &cp.PageInfo{NextPageToken: "org-secret-cursor"}}
		}
		proto.Merge(out, c.secrets)
	case *cp.ListOrganizationRoleImageRecipesResponse:
		if c.failure != nil {
			return c.failure
		}
		if c.recipes == nil {
			c.recipes = &cp.ListOrganizationRoleImageRecipesResponse{Recipes: []*cp.RoleImageRecipe{organizationSnapshotRecipe(false)}, Page: &cp.PageInfo{NextPageToken: "org-image-cursor"}}
		}
		proto.Merge(out, c.recipes)
	case *cp.ListRuntimeSecretsResponse:
		out.Secrets, out.Page = []*cp.RuntimeSecret{organizationSnapshotSecret(true)}, &cp.PageInfo{NextPageToken: "project-secret-cursor"}
	case *cp.ListRoleImageRecipesResponse:
		out.Recipes, out.Page = []*cp.RoleImageRecipe{organizationSnapshotRecipe(true)}, &cp.PageInfo{NextPageToken: "project-image-cursor"}
	case *cp.ListRoleEnvironmentsResponse:
		out.Environments = []*cp.RoleEnvironment{}
	default:
		return status.Error(codes.PermissionDenied, "synthetic unrelated catalog denied")
	}
	return nil
}

func organizationSnapshotServer(c *organizationSnapshotRecorder) *Server {
	return &Server{query: cp.NewPlatformQueryServiceClient(c), roleImages: cp.NewRoleImageServiceClient(c), assistant: cp.NewSystemAssistantServiceClient(c), access: cp.NewAccessServiceClient(c)}
}

func TestOrganizationSnapshotKeepsSeparateScopedCatalogsAndOwnerPages(t *testing.T) {
	for _, kind := range []string{"ROLE_IMAGE_RECIPE", "RUNTIME_SECRET"} {
		t.Run(kind, func(t *testing.T) {
			c := &organizationSnapshotRecorder{}
			raw, err := organizationSnapshotServer(c).projectPlatformSnapshot(t.Context(), kind, "prj_fixture01", func(v string) string { return v })
			if err != nil {
				t.Fatal(err)
			}
			typed, err := typedPlatformSnapshot(kind, raw)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "RUNTIME_SECRET" {
				if len(typed.Catalog.Secrets) != 1 || len(typed.Catalog.OrganizationSecrets) != 1 || typed.Catalog.Secrets[0].ProjectRef != "prj_fixture01" || typed.Catalog.OrganizationSecrets[0].ProjectRef != "" || typed.Catalog.OrganizationSecrets[0].ScopeKind != "ORGANIZATION" || *typed.Catalog.OrganizationSecretsPage.NextPageToken != "org-secret-cursor" || *typed.Catalog.Page.NextPageToken != "project-secret-cursor" {
					t.Fatal("organization/project secret catalogs or cursors merged")
				}
			} else {
				if len(typed.Catalog.Recipes) != 1 || len(typed.Catalog.OrganizationRecipes) != 1 || typed.Catalog.Recipes[0].ProjectRef != "prj_fixture01" || typed.Catalog.OrganizationRecipes[0].ProjectRef != "" || typed.Catalog.OrganizationRecipes[0].ScopeKind != "ORGANIZATION" || *typed.Catalog.OrganizationRecipesPage.NextPageToken != "org-image-cursor" || *typed.Catalog.Page.NextPageToken != "project-image-cursor" {
					t.Fatal("organization/project image catalogs or cursors merged")
				}
			}
			encoded, err := json.Marshal(typed)
			if err != nil || strings.Contains(string(encoded), "RUNTIME_RESOURCE_SCOPE_KIND_") {
				t.Fatal("snapshot leaked native scope enum")
			}
			organizationCalls := 0
			for i, input := range c.requests {
				switch p := input.(type) {
				case *cp.ListOrganizationRuntimeSecretsRequest:
					organizationCalls++
					if p.Page.PageSize != 50 || c.calls[i] != cp.PlatformQueryService_ListOrganizationRuntimeSecrets_FullMethodName {
						t.Fatal("organization secret preload budget changed")
					}
				case *cp.ListOrganizationRoleImageRecipesRequest:
					organizationCalls++
					if p.Page.PageSize != 50 || c.calls[i] != cp.RoleImageService_ListOrganizationRoleImageRecipes_FullMethodName {
						t.Fatal("organization image preload budget changed")
					}
				default:
					continue
				}
				for _, name := range []protoreflect.Name{"project_ref", "organization_ref", "scope_kind", "actor_ref"} {
					if input.ProtoReflect().Descriptor().Fields().ByName(name) != nil {
						t.Fatalf("organization snapshot authority acquired payload field: %s", name)
					}
				}
			}
			if organizationCalls != 1 {
				t.Fatalf("organization snapshot read was duplicated or skipped: %v", c.calls)
			}
		})
	}
}

func TestOrganizationSnapshotRevokedOwnerClearsOnlyOrganizationCatalog(t *testing.T) {
	for _, kind := range []string{"ROLE_IMAGE_RECIPE", "RUNTIME_SECRET"} {
		for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound} {
			c := &organizationSnapshotRecorder{failure: status.Error(code, "private ownership reason")}
			raw, err := organizationSnapshotServer(c).projectPlatformSnapshot(t.Context(), kind, "prj_fixture01", func(v string) string { return v })
			if err != nil {
				t.Fatal(err)
			}
			typed, err := typedPlatformSnapshot(kind, raw)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "RUNTIME_SECRET" && (len(typed.Catalog.Secrets) != 1 || len(typed.Catalog.OrganizationSecrets) != 0 || typed.Catalog.OrganizationSecretsPage == nil) || kind == "ROLE_IMAGE_RECIPE" && (len(typed.Catalog.Recipes) != 1 || len(typed.Catalog.OrganizationRecipes) != 0 || typed.Catalog.OrganizationRecipesPage == nil) {
				t.Fatal("owner revocation retained organizational data or cleared project catalog")
			}
		}
	}
}

func TestOrganizationSnapshotRejectsMalformedAndCrossScopeOwnerReadback(t *testing.T) {
	for _, kind := range []string{"ROLE_IMAGE_RECIPE", "RUNTIME_SECRET"} {
		for _, defect := range []string{"unspecified", "unknown", "project", "project locator", "missing organization", "foreign organization", "nil", "oversized"} {
			t.Run(kind+"/"+defect, func(t *testing.T) {
				c := &organizationSnapshotRecorder{secrets: &cp.ListOrganizationRuntimeSecretsResponse{Secrets: []*cp.RuntimeSecret{organizationSnapshotSecret(false), organizationSnapshotSecret(false)}}, recipes: &cp.ListOrganizationRoleImageRecipesResponse{Recipes: []*cp.RoleImageRecipe{organizationSnapshotRecipe(false), organizationSnapshotRecipe(false)}}}
				s, r := c.secrets.Secrets[1], c.recipes.Recipes[1]
				switch defect {
				case "unspecified":
					s.ScopeKind, r.ScopeKind = 0, 0
				case "unknown":
					s.ScopeKind, r.ScopeKind = 99, 99
				case "project":
					c.secrets.Secrets[1], c.recipes.Recipes[1] = organizationSnapshotSecret(true), organizationSnapshotRecipe(true)
				case "project locator":
					s.ProjectRef, r.ProjectRef = "prj_fixture01", "prj_fixture01"
				case "missing organization":
					s.OrganizationRef, r.OrganizationRef = "", ""
				case "foreign organization":
					s.OrganizationRef, r.OrganizationRef = "org_other01", "org_other01"
				case "nil":
					c.secrets.Secrets[1], c.recipes.Recipes[1] = nil, nil
				case "oversized":
					for len(c.secrets.Secrets) <= 50 {
						c.secrets.Secrets = append(c.secrets.Secrets, s)
						c.recipes.Recipes = append(c.recipes.Recipes, r)
					}
				}
				if _, err := organizationSnapshotServer(c).projectPlatformSnapshot(t.Context(), kind, "prj_fixture01", func(v string) string { return v }); err == nil {
					t.Fatal("invalid scope escaped as successful platform snapshot")
				}
			})
		}
	}
}

func TestOrganizationSnapshotDependencyFailureIsNotAnEmptySuccessfulCatalog(t *testing.T) {
	for _, kind := range []string{"ROLE_IMAGE_RECIPE", "RUNTIME_SECRET"} {
		c := &organizationSnapshotRecorder{failure: status.Error(codes.Unavailable, "synthetic dependency offline")}
		if _, err := organizationSnapshotServer(c).projectPlatformSnapshot(t.Context(), kind, "", func(v string) string { return v }); status.Code(err) != codes.Unavailable {
			t.Fatal("dependency failure was represented as owner revocation")
		}
	}
}

func TestOrganizationSnapshotBootstrapWithoutProjectIncludesBothScopedCatalogs(t *testing.T) {
	c := &organizationSnapshotRecorder{}
	outbound := make(chan outboundFrame, len(platformBootstrapKinds))
	m := &sessionMultiplexer{server: organizationSnapshotServer(c), ctx: t.Context(), localize: func(v string) string { return v }, outbound: outbound, platformAvailable: true, platformRequestRef: "req_fixture01", platformCursor: 3}
	kinds, err := m.sendPlatformBootstrap()
	if err != nil || len(kinds) != 2 {
		t.Fatalf("all-projects bootstrap omitted organization-capable catalogs: kinds=%v err=%v", kinds, err)
	}
	for range kinds {
		frame := <-outbound
		envelope, ok := frame.value.(generated.PlatformSnapshotEnvelope)
		if !ok || envelope.ProjectRef != nil || envelope.Mode != generated.PlatformSnapshotModeBootstrap {
			t.Fatal("projectless bootstrap acquired selected project scope")
		}
		if envelope.Kind == "ROLE_IMAGE_RECIPE" && len(envelope.Snapshot.Catalog.OrganizationRecipes) != 1 || envelope.Kind == "RUNTIME_SECRET" && (len(envelope.Snapshot.Catalog.OrganizationSecrets) != 1 || len(envelope.Snapshot.Catalog.Secrets) != 1) {
			t.Fatal("all-projects bootstrap lost organization/project readbacks")
		}
	}
}
