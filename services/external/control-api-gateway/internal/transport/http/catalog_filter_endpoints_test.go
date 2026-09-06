package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCatalogFiltersReachOwner(t *testing.T) {
	for _, tc := range []struct {
		path, field, want string
		response          proto.Message
	}{
		{"/api/v1/agents?state=ARCHIVED", "state", "AGENT_STATE_ARCHIVED", &cp.ListAgentsResponse{}},
		{"/api/v1/projects/prj_fixture01/agents?state=DISABLED", "state", "AGENT_STATE_DISABLED", &cp.ListAgentsResponse{}},
		{"/api/v1/workflows?state=VALID", "state", "WORKFLOW_STATE_VALID", &cp.ListWorkflowsResponse{}},
		{"/api/v1/projects/prj_fixture01/workflows?state=ARCHIVED", "state", "WORKFLOW_STATE_ARCHIVED", &cp.ListWorkflowsResponse{}},
		{"/api/v1/provider-accounts?state=REAUTHORIZATION_REQUIRED", "state", "PROVIDER_ACCOUNT_STATE_REAUTHORIZATION_REQUIRED", &cp.ListProviderAccountsResponse{}},
		{"/api/v1/provider-accounts?state=DELETED", "state", "PROVIDER_ACCOUNT_STATE_DELETED", &cp.ListProviderAccountsResponse{}},
		{"/api/v1/integration-connections?definitionKey=email", "definition_key", "email", &cp.ListIntegrationConnectionsResponse{}},
		{"/api/v1/audit-events?action=EXACT_ACTION", "action", "EXACT_ACTION", &cp.ListAuditEventsResponse{}},
		{"/api/v1/audit-events?outcome=DENIED", "outcome", "DENIED", &cp.ListAuditEventsResponse{}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			client := &catalogRPCRecorder{response: tc.response}
			h := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client)}})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", tc.path+"&query=literal%25&pageSize=2&pageToken=owner-cursor", nil))
			if w.Code != 200 || client.request == nil {
				t.Fatalf("filter request: %d %s", w.Code, w.Body.String())
			}
			m := client.request.ProtoReflect()
			f := m.Descriptor().Fields().ByName(protoreflect.Name(tc.field))
			got := m.Get(f).String()
			if f.Kind() == protoreflect.EnumKind {
				got = string(f.Enum().Values().ByNumber(m.Get(f).Enum()).Name())
			}
			if got != tc.want || m.Get(m.Descriptor().Fields().ByName("query")).String() != "literal%" {
				t.Fatalf("filter lost: %s %#v", got, client.request)
			}
		})
	}
}

func TestCatalogFiltersRejectInvalidBeforeOwner(t *testing.T) {
	for _, path := range []string{"/api/v1/agents?state=UNKNOWN", "/api/v1/projects/prj_fixture01/agents?state=UNSPECIFIED", "/api/v1/workflows?state=READY", "/api/v1/projects/prj_fixture01/workflows?state=", "/api/v1/provider-accounts?state=UNKNOWN", "/api/v1/integration-connections?definitionKey=", "/api/v1/audit-events?action=" + strings.Repeat("x", 161), "/api/v1/audit-events?outcome=" + strings.Repeat("x", 81)} {
		client := &catalogRPCRecorder{}
		h := generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client)}})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid filter reached owner: %s status=%d", path, w.Code)
		}
	}
}
