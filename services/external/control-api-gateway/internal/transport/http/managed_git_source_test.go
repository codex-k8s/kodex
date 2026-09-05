package httptransport

import (
	"encoding/json"
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

const gitSourceBody = `{"connectionRef":"conn_fixture01","expectedConnectionVersion":7,"repositoryRef":"owner/repository","refName":"refs/heads/main","path":"kodex/source.yaml","contentFormat":"YAML"}`

func gitSourceFixture(kind cp.ManagedConfigurationKind) *cp.ManagedConfigurationSet {
	configuration, revision := managedFixture()
	configuration.Kind = kind
	configuration.Version = 4
	configuration.ManagedBy = cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_GIT
	configuration.Source = "mcsrc_fixture01"
	configuration.CurrentRevision = revision
	configuration.GitSource = &cp.ManagedConfigurationGitSource{Ref: configuration.Source, Version: 2, Generation: 3, ConnectionRef: "conn_fixture01", ProviderKey: "github", RepositoryRef: "owner/repository", RefName: "refs/heads/main", Path: "kodex/source.yaml", State: cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED}
	return configuration
}
func gitSourceHandler(client *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client), Command: cp.NewPlatformCommandServiceClient(client)}})
}

type gitSourceRoute struct {
	path, method, body string
	kind               cp.ManagedConfigurationKind
	response           func(*cp.ManagedConfigurationSet) proto.Message
}

func gitSourceRoutes() []gitSourceRoute {
	return []gitSourceRoute{
		{"role-image", "ConfigureRoleImageGitSource", gitSourceBody, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, func(c *cp.ManagedConfigurationSet) proto.Message {
			return &cp.ConfigureRoleImageGitSourceResponse{Configuration: c}
		}},
		{"role-image", "RefreshRoleImageGitSource", "", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, func(c *cp.ManagedConfigurationSet) proto.Message {
			return &cp.RefreshRoleImageGitSourceResponse{Configuration: c}
		}},
		{"integration-definition", "ConfigureIntegrationDefinitionGitSource", gitSourceBody, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, func(c *cp.ManagedConfigurationSet) proto.Message {
			return &cp.ConfigureIntegrationDefinitionGitSourceResponse{Configuration: c}
		}},
		{"integration-definition", "RefreshIntegrationDefinitionGitSource", "", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, func(c *cp.ManagedConfigurationSet) proto.Message {
			return &cp.RefreshIntegrationDefinitionGitSourceResponse{Configuration: c}
		}},
	}
}
func (value gitSourceRoute) url() string {
	result := "/api/v1/" + value.path + "-configurations/mcfg_fixture01/git-source"
	if value.body == "" {
		result += "/refresh"
	}
	return result
}

func TestGitSourceCommandsPreserveExactIntentAndSafeMetadata(t *testing.T) {
	for _, route := range gitSourceRoutes() {
		t.Run(route.method, func(t *testing.T) {
			client := &catalogRPCRecorder{response: route.response(gitSourceFixture(route.kind))}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, managedTestRequest("POST", route.url(), route.body))
			if w.Code != 200 || !strings.HasSuffix(client.method, "/"+route.method) {
				t.Fatalf("command mapping failed: %d", w.Code)
			}
			request := client.request.(interface {
				GetConfigurationRef() string
				GetMutation() *cp.MutationContext
			})
			if request.GetConfigurationRef() != "mcfg_fixture01" || request.GetMutation().GetExpectedVersion() != 3 || request.GetMutation().GetIdempotencyKey() != "managed-fixture-01" {
				t.Fatal("configuration OCC or idempotency lost")
			}
			if route.body != "" {
				input := client.request.(interface {
					GetSource() *cp.ManagedConfigurationGitSourceInput
				}).GetSource()
				if input.ConnectionRef != "conn_fixture01" || input.ExpectedConnectionVersion != 7 || input.RepositoryRef != "owner/repository" || input.RefName != "refs/heads/main" || input.Path != "kodex/source.yaml" || input.ContentFormat != "YAML" {
					t.Fatal("source intent changed")
				}
			}
			var result generated.ManagedConfigurationSummary
			if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.GitSource == nil || result.GitSource.Generation != 3 || result.GitSource.State != "QUEUED" || result.GitSource.FailureCode != nil || w.Header().Get("ETag") != "\"4\"" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("safe source receipt lost")
			}
			for _, forbidden := range []string{"TYPE_остается", `"content":`, "credential", "workRef", "lease", "definitionPackage"} {
				if strings.Contains(w.Body.String(), forbidden) {
					t.Fatal("private source content leaked")
				}
			}
		})
	}
}

func TestGitSourceCommandsRejectInvalidInputBeforeRPC(t *testing.T) {
	for _, route := range gitSourceRoutes() {
		for _, header := range []string{"If-Match", "Idempotency-Key"} {
			client := &catalogRPCRecorder{}
			r := managedTestRequest("POST", route.url(), route.body)
			r.Header.Del(header)
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, r)
			if w.Code != 400 || client.method != "" {
				t.Fatal("missing mutation boundary reached RPC")
			}
		}
		if route.body == "" {
			continue
		}
		for _, replacement := range []struct{ old, new string }{
			{`"expectedConnectionVersion":7`, `"expectedConnectionVersion":0`}, {`"expectedConnectionVersion":7`, `"expectedConnectionVersion":9007199254740992`}, {`"expectedConnectionVersion":7`, `"expectedConnectionVersion":1.5`},
			{`"contentFormat":"YAML"`, `"contentFormat":"TOML"`}, {`"contentFormat":"YAML"`, `"contentFormat":"TEXT"`}, {`"connectionRef":"conn_fixture01"`, `"connectionRef":"bad!"`},
			{`"path":"kodex/source.yaml"`, `"path":"../source.yaml"`}, {`"path":"kodex/source.yaml"`, `"path":"/source.yaml"`}, {`"path":"kodex/source.yaml"`, `"path":"a//source.yaml"`},
			{`"refName":"refs/heads/main"`, `"refName":"main.lock"`}, {`"refName":"refs/heads/main"`, `"refName":"main..other"`}, {`"repositoryRef":"owner/repository"`, `"repositoryRef":"` + strings.Repeat("я", 129) + `"`},
			{`"contentFormat":"YAML"`, `"contentFormat":"YAML","credentialValue":"fixture-not-a-secret"`},
		} {
			client := &catalogRPCRecorder{}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, managedTestRequest("POST", route.url(), strings.Replace(route.body, replacement.old, replacement.new, 1)))
			if w.Code != 400 || client.method != "" {
				t.Fatal("invalid source input reached RPC")
			}
		}
	}
}

func TestGitSourceCommandsPreserveSafeRPCErrors(t *testing.T) {
	for _, route := range gitSourceRoutes() {
		for code, want := range map[codes.Code]int{codes.InvalidArgument: 400, codes.Unauthenticated: 401, codes.PermissionDenied: 403, codes.NotFound: 404, codes.AlreadyExists: 409, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.Unavailable: 503, codes.DeadlineExceeded: 504} {
			client := &catalogRPCRecorder{failure: status.Error(code, "private upstream details")}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, managedTestRequest("POST", route.url(), route.body))
			if w.Code != want || strings.Contains(w.Body.String(), "private upstream details") {
				t.Fatalf("RPC error mapping failed: %s %d", code, w.Code)
			}
		}
	}
}

func TestGitSourceCommandsRejectUnboundReceipt(t *testing.T) {
	for _, route := range gitSourceRoutes() {
		for _, change := range []func(*cp.ManagedConfigurationSet){
			func(c *cp.ManagedConfigurationSet) { c.Ref = "mcfg_other01" },
			func(c *cp.ManagedConfigurationSet) {
				c.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT
			},
			func(c *cp.ManagedConfigurationSet) { c.Version = 3 },
			func(c *cp.ManagedConfigurationSet) { c.GitSource = nil },
		} {
			c := gitSourceFixture(route.kind)
			change(c)
			client := &catalogRPCRecorder{response: route.response(c)}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, managedTestRequest("POST", route.url(), route.body))
			if w.Code != 502 {
				t.Fatal("unbound command receipt accepted")
			}
		}
	}
}

func TestGitSourceHistoryRejectsForeignOrMalformedPage(t *testing.T) {
	for _, change := range []func(*cp.ListManagedConfigurationHistoryResponse){
		func(r *cp.ListManagedConfigurationHistoryResponse) { r.Configuration.Ref = "mcfg_other01" },
		func(r *cp.ListManagedConfigurationHistoryResponse) { r.Total = 0 },
		func(r *cp.ListManagedConfigurationHistoryResponse) {
			r.Total = 2
			r.Revisions = append(r.Revisions, proto.Clone(r.Revisions[0]).(*cp.ManagedConfigurationRevision))
		},
		func(r *cp.ListManagedConfigurationHistoryResponse) {
			r.Page = &cp.PageInfo{NextPageToken: strings.Repeat("x", 513)}
		},
	} {
		c := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
		response := &cp.ListManagedConfigurationHistoryResponse{Configuration: c, Revisions: []*cp.ManagedConfigurationRevision{c.CurrentRevision}, Total: 1}
		change(response)
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		gitSourceHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/managed-configurations/mcfg_fixture01/revisions", nil))
		if w.Code != 502 || strings.Contains(w.Body.String(), "mcsrc_fixture01") {
			t.Fatal("invalid history snapshot exposed")
		}
	}
}

func TestGitSourceReadbackPreservesStatesAndPins(t *testing.T) {
	for state := cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED; state <= cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_DETACHED; state++ {
		configuration := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
		source := configuration.GitSource
		source.State = state
		source.AcceptedCommitSha = strings.Repeat("a", 40)
		source.AcceptedContentSha256 = strings.Repeat("b", 64)
		source.AcceptedRevisionRef = "mrev_accepted01"
		source.SyncedAt = configuration.UpdatedAt
		configuration.SourceRevision = source.AcceptedCommitSha
		if state == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_DETACHED {
			configuration.ManagedBy = cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI
		}
		if state == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED {
			source.FailureCode = cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_CREDENTIAL_REJECTED
		}
		for _, history := range []bool{false, true} {
			client := &catalogRPCRecorder{}
			url := "/api/v1/managed-configurations"
			if history {
				client.response = &cp.ListManagedConfigurationHistoryResponse{Configuration: configuration, Revisions: []*cp.ManagedConfigurationRevision{configuration.CurrentRevision}, Total: 1}
				url += "/mcfg_fixture01/revisions"
			} else {
				client.response = &cp.ListManagedConfigurationsResponse{Configurations: []*cp.ManagedConfigurationSet{configuration}, Total: 1}
			}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, httptest.NewRequest("GET", url, nil))
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"acceptedCommitSha":"`+source.AcceptedCommitSha+`"`) || !strings.Contains(w.Body.String(), `"acceptedRevisionRef":"mrev_accepted01"`) {
				t.Fatal("source projection lost accepted pins")
			}
			if !history && strings.Contains(w.Body.String(), `"content":`) {
				t.Fatal("catalog leaked source content")
			}
		}
	}
}

func TestGitSourceReadbackRejectsMalformedProjection(t *testing.T) {
	for name, change := range map[string]func(*cp.ManagedConfigurationSet){
		"unknown state": func(c *cp.ManagedConfigurationSet) { c.GitSource.State = cp.ManagedConfigurationSourceState(99) },
		"zero state":    func(c *cp.ManagedConfigurationSet) { c.GitSource.State = 0 },
		"zero version":  func(c *cp.ManagedConfigurationSet) { c.GitSource.Version = 0 },
		"bad provider":  func(c *cp.ManagedConfigurationSet) { c.GitSource.ProviderKey = "unknown" },
		"wrong owner": func(c *cp.ManagedConfigurationSet) {
			c.ManagedBy = cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI
		},
		"missing ready pins": func(c *cp.ManagedConfigurationSet) {
			c.GitSource.State = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY
		},
		"partial pin": func(c *cp.ManagedConfigurationSet) {
			c.GitSource.AcceptedCommitSha = strings.Repeat("a", 40)
			c.SourceRevision = c.GitSource.AcceptedCommitSha
		},
		"unexpected failure": func(c *cp.ManagedConfigurationSet) {
			c.GitSource.FailureCode = cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNAVAILABLE
		},
		"missing failure": func(c *cp.ManagedConfigurationSet) {
			c.GitSource.State = cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED
		},
		"mismatched source": func(c *cp.ManagedConfigurationSet) { c.Source = "mcsrc_other01" },
	} {
		t.Run(name, func(t *testing.T) {
			c := gitSourceFixture(cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
			change(c)
			client := &catalogRPCRecorder{response: &cp.ListManagedConfigurationsResponse{Configurations: []*cp.ManagedConfigurationSet{c}, Total: 1}}
			w := httptest.NewRecorder()
			gitSourceHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/managed-configurations", nil))
			if w.Code != 502 || strings.Contains(w.Body.String(), "mcsrc_fixture01") {
				t.Fatal("malformed source projection exposed")
			}
		})
	}
}
