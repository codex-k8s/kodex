package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func configurationCopyHandler(c *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(c)}})
}

func TestConfigurationCopyAndArchiveTypedBoundary(t *testing.T) {
	configuration, revision := managedFixture()
	configuration.Archived = true
	configuration.CopyProvenance = &cp.ManagedConfigurationCopyProvenance{Origin: cp.ManagedConfigurationCopyOrigin_MANAGED_CONFIGURATION_COPY_ORIGIN_SHIPPED, SourceRef: "source-fixture", SourceRevision: "1.0.0", SourceVersion: 3, SourceDigest: strings.Repeat("a", 64)}
	cases := []struct {
		path, body, rpc string
		response        proto.Message
		code            int
	}{
		{"/api/v1/role-image-configurations/copies", `{"name":"Копия","projectRef":"prj_fixture01","recipeRef":"recipe_fixture01"}`, "CopyRoleImageConfiguration", &cp.CopyRoleImageConfigurationResponse{Configuration: configuration, Revision: revision}, 201},
		{"/api/v1/role-image-configurations/copies", `{"name":"Копия","projectRef":"prj_fixture01","configurationRef":"mcfg_fixture01"}`, "CopyRoleImageConfiguration", &cp.CopyRoleImageConfigurationResponse{Configuration: configuration, Revision: revision}, 201},
		{"/api/v1/integration-definition-configurations/copies", `{"name":"Копия","shipped":{"key":"github","definitionVersion":"1.0.0","digest":"` + strings.Repeat("b", 64) + `"}}`, "CopyIntegrationDefinitionConfiguration", &cp.CopyIntegrationDefinitionConfigurationResponse{Configuration: configuration, Revision: revision}, 201},
		{"/api/v1/integration-definition-configurations/copies", `{"name":"Копия","configurationRef":"mcfg_fixture01"}`, "CopyIntegrationDefinitionConfiguration", &cp.CopyIntegrationDefinitionConfigurationResponse{Configuration: configuration, Revision: revision}, 201},
		{"/api/v1/role-image-configurations/mcfg_fixture01/archive", "", "ArchiveRoleImageConfiguration", &cp.ArchiveRoleImageConfigurationResponse{Configuration: configuration}, 200},
		{"/api/v1/integration-definition-configurations/mcfg_fixture01/archive", "", "ArchiveIntegrationDefinitionConfiguration", &cp.ArchiveIntegrationDefinitionConfigurationResponse{Configuration: configuration}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.rpc+tc.body, func(t *testing.T) {
			c := &catalogRPCRecorder{response: tc.response}
			w := httptest.NewRecorder()
			configurationCopyHandler(c).ServeHTTP(w, managedTestRequest("POST", tc.path, tc.body))
			if w.Code != tc.code || !strings.HasSuffix(c.method, "/"+tc.rpc) || w.Header().Get("ETag") != `"3"` || !strings.Contains(w.Body.String(), `"copyProvenance"`) || !strings.Contains(w.Body.String(), `"archived":true`) {
				t.Fatalf("typed route failed code=%d", w.Code)
			}
			fields := c.request.ProtoReflect()
			m := fields.Get(fields.Descriptor().Fields().ByName("mutation")).Message().Interface().(*cp.MutationContext)
			if m.GetExpectedVersion() != 3 || m.GetIdempotencyKey() != "managed-fixture-01" {
				t.Fatal("mutation pins changed")
			}
			switch q := c.request.(type) {
			case *cp.CopyRoleImageConfigurationRequest:
				if q.GetProjectRef() != "prj_fixture01" || (q.GetRecipeRef() == "") == (q.GetConfigurationRef() == "") {
					t.Fatal("source selector changed")
				}
			case *cp.CopyIntegrationDefinitionConfigurationRequest:
				if s := q.GetShipped(); s != nil {
					if s.GetKey() != "github" || s.GetDefinitionVersion() != "1.0.0" || s.GetDigest() != strings.Repeat("b", 64) {
						t.Fatal("shipped pins changed")
					}
				} else if q.GetConfigurationRef() != "mcfg_fixture01" {
					t.Fatal("managed selector changed")
				}
			}
			for _, header := range []string{"If-Match", "Idempotency-Key"} {
				c.method = ""
				r := managedTestRequest("POST", tc.path, tc.body)
				r.Header.Del(header)
				w := httptest.NewRecorder()
				configurationCopyHandler(c).ServeHTTP(w, r)
				if w.Code != 400 || c.method != "" {
					t.Fatal("missing mutation header reached owner")
				}
			}
			for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503, codes.DeadlineExceeded: 504} {
				c.failure = status.Error(code, "private fixture sentinel")
				w := httptest.NewRecorder()
				configurationCopyHandler(c).ServeHTTP(w, managedTestRequest("POST", tc.path, tc.body))
				if w.Code != want || strings.Contains(w.Body.String(), "private fixture sentinel") {
					t.Fatal("owner failure leaked or succeeded")
				}
			}
		})
	}
}

func TestConfigurationCopyRejectsAmbiguousAndAuthorityFields(t *testing.T) {
	for _, tc := range []struct {
		path   string
		bodies []string
	}{
		{"/api/v1/role-image-configurations/copies", []string{`{}`, `{"name":"x","projectRef":"prj_fixture01","recipeRef":"recipe_fixture01","configurationRef":"mcfg_fixture01"}`, `{"name":"x","projectRef":"prj_fixture01","recipeRef":null}`, `{"name":"x","projectRef":"prj_fixture01","recipeRef":"recipe_fixture01","actorRef":"usr_fixture01"}`, `{"name":"x","projectRef":"prj_fixture01","recipeRef":"recipe_fixture01","organizationRef":"org_fixture01"}`}},
		{"/api/v1/integration-definition-configurations/copies", []string{`{}`, `{"name":"x","configurationRef":"mcfg_fixture01","shipped":{}}`, `{"name":"x","configurationRef":null}`, `{"name":"x","configurationRef":"mcfg_fixture01","copyProvenance":{}}`, `{"name":"x","shipped":{"key":"github","definitionVersion":"1.0.0","digest":"` + strings.Repeat("a", 64) + `","actorRef":"usr_fixture01"}}`, `{"name":"x","shipped":{"key":"github","definitionVersion":"bad","digest":"bad"}}`}},
		{"/api/v1/role-image-configurations/mcfg_fixture01/archive", []string{`{"actorRef":"usr_fixture01"}`}},
		{"/api/v1/integration-definition-configurations/mcfg_fixture01/archive", []string{`{"organizationRef":"org_fixture01"}`}},
	} {
		for _, body := range tc.bodies {
			c := &catalogRPCRecorder{}
			w := httptest.NewRecorder()
			configurationCopyHandler(c).ServeHTTP(w, managedTestRequest("POST", tc.path, body))
			if w.Code != 400 || c.method != "" {
				t.Fatalf("invalid selector accepted path=%s status=%d", tc.path, w.Code)
			}
		}
	}
}

func TestConfigurationCopyProvenanceProjection(t *testing.T) {
	for _, origin := range []cp.ManagedConfigurationCopyOrigin{cp.ManagedConfigurationCopyOrigin_MANAGED_CONFIGURATION_COPY_ORIGIN_SHIPPED, cp.ManagedConfigurationCopyOrigin_MANAGED_CONFIGURATION_COPY_ORIGIN_UI, cp.ManagedConfigurationCopyOrigin_MANAGED_CONFIGURATION_COPY_ORIGIN_GIT} {
		c, r := managedFixture()
		c.CurrentRevision = r
		c.CopyProvenance = &cp.ManagedConfigurationCopyProvenance{Origin: origin, SourceRef: "mcfg_fixture00", SourceRevision: "mrev_fixture00", SourceVersion: 2, SourceDigest: strings.Repeat("c", 64)}
		v, err := managedConfigurationSummaryView(c)
		if err != nil || v.CopyProvenance == nil {
			t.Fatal("summary provenance absent")
		}
		for _, bad := range []func(*cp.ManagedConfigurationCopyProvenance){func(p *cp.ManagedConfigurationCopyProvenance) { p.Origin = 99 }, func(p *cp.ManagedConfigurationCopyProvenance) { p.SourceVersion = 0 }, func(p *cp.ManagedConfigurationCopyProvenance) { p.SourceDigest = "bad" }, func(p *cp.ManagedConfigurationCopyProvenance) { p.SourceRef = "" }, func(p *cp.ManagedConfigurationCopyProvenance) { p.SourceRevision = "" }} {
			d := proto.Clone(c).(*cp.ManagedConfigurationSet)
			bad(d.CopyProvenance)
			if _, err := managedConfigurationSummaryView(d); err == nil {
				t.Fatal("corrupt provenance accepted")
			}
		}
	}
}

func TestConfigurationAvailableActionsAndCatalogVersion(t *testing.T) {
	for _, actions := range [][]string{nil, {"COPY"}, {"COPY", "ARCHIVE"}, {"ARCHIVE"}} {
		c, _ := managedFixture()
		c.NextActions = actions
		v, err := managedConfigurationSummaryView(c)
		if err != nil || v.NextActions == nil || len(v.NextActions) != len(actions) {
			t.Fatal("owner actions changed")
		}
	}
	for _, actions := range [][]string{{"COPY", "COPY"}, {"DELETE"}, {"NEXT_ACTION_COPY"}} {
		c, _ := managedFixture()
		c.NextActions = actions
		if _, err := managedConfigurationView(c); err == nil {
			t.Fatal("unknown owner actions accepted")
		}
	}
	for _, version := range []int64{0, -1, 9007199254740992} {
		if _, err := messageMap(&cp.IntegrationDefinition{Version: version}); err == nil {
			t.Fatal("invalid catalog OCC version accepted")
		}
	}
	for _, actions := range [][]string{nil, {"COPY"}} {
		v, err := messageMap(&cp.IntegrationDefinition{Version: 3, NextActions: actions})
		if err != nil || v["version"] != float64(3) || v["nextActions"] == nil {
			t.Fatal("catalog OCC or actions missing")
		}
	}
	for _, actions := range [][]string{{"ARCHIVE"}, {"COPY", "COPY"}} {
		if _, err := messageMap(&cp.IntegrationDefinition{Version: 3, NextActions: actions}); err == nil {
			t.Fatal("invalid catalog action accepted")
		}
	}
}
