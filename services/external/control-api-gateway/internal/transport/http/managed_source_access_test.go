package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
)

func TestManagedRoleImageSourceReadback(t *testing.T) {
	for _, available := range []bool{false, true} {
		c := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
		c.SourceEditable = proto.Bool(available)
		c.CurrentRevision.SourceAvailable = proto.Bool(available)
		if !available {
			c.CurrentRevision.Content = ""
		}
		view, err := managedConfigurationView(c)
		if err != nil || view.SourceEditable == nil || *view.SourceEditable != available || view.CurrentRevision.SourceAvailable == nil || *view.CurrentRevision.SourceAvailable != available {
			t.Fatalf("source presence lost: %v", err)
		}
		encoded, err := json.Marshal(view)
		if err != nil || !strings.Contains(string(encoded), `"sourceAvailable":`) || !strings.Contains(string(encoded), `"sourceEditable":`) {
			t.Fatal("explicit source access missing from JSON")
		}
		summary, err := managedConfigurationSummaryView(c)
		if err != nil || summary.SourceEditable == nil || *summary.SourceEditable != available {
			t.Fatalf("summary access lost: %v", err)
		}
	}
}

func TestManagedRoleImageSourceContradictions(t *testing.T) {
	for name, corrupt := range map[string]func(*cp.ManagedConfigurationSet){
		"missing-editability":  func(c *cp.ManagedConfigurationSet) { c.SourceEditable = nil },
		"missing-availability": func(c *cp.ManagedConfigurationSet) { c.CurrentRevision.SourceAvailable = nil },
		"redacted-content":     func(c *cp.ManagedConfigurationSet) { c.CurrentRevision.SourceAvailable = proto.Bool(false) },
		"redacted-diagnostics": func(c *cp.ManagedConfigurationSet) {
			c.CurrentRevision.SourceAvailable = proto.Bool(false)
			c.CurrentRevision.Content = ""
			c.CurrentRevision.ValidationDiagnostics = []string{"private source diagnostic"}
		},
		"foreign-kind": func(c *cp.ManagedConfigurationSet) {
			c.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
			corrupt(c)
			if _, err := managedConfigurationView(c); err == nil {
				t.Fatal("invalid owner source projection accepted")
			}
			w := httptest.NewRecorder()
			writeManagedResult(w, 200, &cp.CreateRoleImageRevisionDraftResponse{Configuration: c, Revision: c.CurrentRevision})
			if w.Code != 502 || strings.Contains(w.Body.String(), "private source diagnostic") || strings.Contains(w.Body.String(), managedFixtureContent) {
				t.Fatal("invalid command readback was not closed")
			}
		})
	}
}

func TestManagedRoleImageHistorySourceAccess(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		c := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
		old := proto.Clone(c.CurrentRevision).(*cp.ManagedConfigurationRevision)
		old.Ref = "mrev_previous01"
		old.SourceAvailable = proto.Bool(false)
		old.Content = ""
		if invalid {
			old.ValidationDiagnostics = []string{"private source diagnostic"}
		}
		client := &catalogRPCRecorder{response: &cp.ListManagedConfigurationHistoryResponse{Configuration: c, Revisions: []*cp.ManagedConfigurationRevision{old}, Total: 1}}
		w := httptest.NewRecorder()
		gitSourceHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/managed-configurations/mcfg_fixture01/revisions", nil))
		if invalid {
			if w.Code != 502 || strings.Contains(w.Body.String(), "private source diagnostic") {
				t.Fatal("history leaked invalid redacted diagnostics")
			}
		} else if w.Code != 200 || !strings.Contains(w.Body.String(), `"sourceAvailable":false`) {
			t.Fatal("history lost explicit redacted source access")
		}
	}
}
