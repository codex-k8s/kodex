package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type managedDraftCase struct {
	path, method, format string
	kind                 cp.ManagedConfigurationKind
	save                 bool
}

func managedDraftCases() []managedDraftCase {
	var result []managedDraftCase
	for _, kind := range []struct {
		path, name, format string
		kind               cp.ManagedConfigurationKind
	}{
		{"prompt-template-configurations", "PromptTemplate", "TEXT", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE},
		{"role-image-configurations", "RoleImageRevision", "JSON", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE},
		{"integration-definition-configurations", "IntegrationDefinition", "YAML", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION},
		{"system-stt-configurations", "SystemSTTConfiguration", "TOML", cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT},
	} {
		prefix := "/api/v1/" + kind.path + "/mcfg_fixture01/revisions/mrev_fixture01/"
		result = append(result, managedDraftCase{prefix + "saves", "Save" + kind.name + "Draft", kind.format, kind.kind, true})
		result = append(result, managedDraftCase{prefix + "discard", "Discard" + kind.name + "Draft", kind.format, kind.kind, false})
	}
	return result
}

type managedDraftRecorder struct {
	grpc.ClientConnInterface
	test    managedDraftCase
	method  string
	request proto.Message
	failure error
	corrupt func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
}

func (client *managedDraftRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	if method != "/controlplane.v1.PlatformCommandService/"+client.test.method {
		return status.Error(codes.Unimplemented, "unexpected test method")
	}
	configuration, revision := managedFixture()
	configuration.Version = 4
	configuration.Kind = client.test.kind
	if client.test.kind == cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE {
		configuration.SourceEditable = proto.Bool(true)
		revision.SourceAvailable = proto.Bool(true)
	}
	revision.State = cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED
	revision.ContentFormat = client.test.format
	if client.test.save || strings.HasPrefix(client.test.method, "Create") {
		input := request.(interface {
			GetContent() string
			GetContentFormat() string
		})
		if client.test.save {
			revision.Ref = "mrev_fixture02"
			revision.ParentRevisionRef = "mrev_fixture01"
			revision.Revision = 3
		}
		revision.State = cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT
		revision.Content = strings.TrimSpace(input.GetContent())
		revision.ContentFormat = input.GetContentFormat()
		if revision.ContentFormat == "OPENAPI_IMPORT" {
			definition, err := integrationpackage.DraftOpenAPIPackageFromJSON(context.Background(), []byte(revision.Content))
			if err != nil {
				return status.Error(codes.InvalidArgument, "OpenAPI import is invalid")
			}
			encoded, err := json.Marshal(definition)
			if err != nil {
				return status.Error(codes.Internal, "OpenAPI import serialization failed")
			}
			revision.Content, revision.ContentFormat = string(encoded), "JSON"
		}
		digest := sha256.Sum256([]byte(revision.Content))
		revision.Digest = hex.EncodeToString(digest[:])
	}
	if client.corrupt != nil {
		client.corrupt(configuration, revision)
	}
	target := response.(proto.Message).ProtoReflect()
	target.Set(target.Descriptor().Fields().ByName("configuration"), protoreflect.ValueOfMessage(configuration.ProtoReflect()))
	target.Set(target.Descriptor().Fields().ByName("revision"), protoreflect.ValueOfMessage(revision.ProtoReflect()))
	return nil
}

func managedDraftHandler(client *managedDraftRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
}
func managedSaveBody(format, content string) string {
	body, _ := json.Marshal(map[string]string{"contentFormat": format, "content": content})
	return string(body)
}

func TestManagedDraftExactCommands(t *testing.T) {
	for _, tc := range managedDraftCases() {
		t.Run(tc.method, func(t *testing.T) {
			client := &managedDraftRecorder{test: tc}
			w := httptest.NewRecorder()
			content := "  Незавершённый текст\n  поле: ["
			body := ""
			if tc.save {
				body = managedSaveBody(tc.format, content)
			}
			managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", tc.path, body))
			var result generated.ManagedConfigurationResult
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || w.Header().Get("ETag") != `"4"` || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			input := client.request.(interface {
				GetMutation() *cp.MutationContext
				GetConfigurationRef() string
				GetRevisionRef() string
			})
			if input.GetMutation().GetExpectedVersion() != 3 || input.GetMutation().IdempotencyKey != "managed-fixture-01" || input.GetConfigurationRef() != "mcfg_fixture01" || input.GetRevisionRef() != "mrev_fixture01" {
				t.Fatalf("incorrect request: %v", client.request)
			}
			if tc.save {
				raw := client.request.(interface {
					GetContent() string
					GetContentFormat() string
				})
				if raw.GetContent() != content || raw.GetContentFormat() != tc.format || result.Revision.Ref != "mrev_fixture02" ||
					result.Revision.ParentRevisionRef == nil || *result.Revision.ParentRevisionRef != "mrev_fixture01" || result.Revision.State != "DRAFT" || result.Revision.Content != strings.TrimSpace(content) {
					t.Fatalf("save lineage changed: %v %+v", client.request, result.Revision)
				}
			} else if result.Revision.Ref != "mrev_fixture01" || result.Revision.State != "DISCARDED" || result.Revision.Content != managedFixtureContent {
				t.Fatalf("discard rewrote source: %+v", result.Revision)
			}
		})
	}
}

func TestManagedDraftOpenAPIImportCanonicalReadback(t *testing.T) {
	const source = `openapi: 3.1.0
info: {title: Заявки, version: 1.0.0}
servers: [{url: https://api.example.test}]
paths:
  /health:
    get:
      operationId: getHealth
      responses: {'200': {description: OK}}
`
	payload, err := json.Marshal(integrationpackage.OpenAPIImportPayload{Source: source,
		Options: integrationpackage.OpenAPIImportOptions{Version: "1.0.0", HealthOperationID: "getHealth",
			Choices: []integrationpackage.OpenAPIImportChoice{{OperationID: "getHealth", Risk: "READ", ApprovalPolicy: "NONE"}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/saves"
	for _, tc := range []struct {
		name    string
		corrupt func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
		want    int
	}{
		{name: "canonical", want: http.StatusOK},
		{name: "changed-content", corrupt: func(_ *cp.ManagedConfigurationSet, revision *cp.ManagedConfigurationRevision) {
			revision.Content += " "
		}, want: http.StatusBadGateway},
		{name: "changed-format", corrupt: func(_ *cp.ManagedConfigurationSet, revision *cp.ManagedConfigurationRevision) {
			revision.ContentFormat = "OPENAPI_IMPORT"
		}, want: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &managedDraftRecorder{test: managedDraftCase{method: "SaveIntegrationDefinitionDraft", kind: cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, save: true}, corrupt: tc.corrupt}
			w := httptest.NewRecorder()
			managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", path, managedSaveBody("OPENAPI_IMPORT", string(payload))))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusOK && (strings.Contains(w.Body.String(), "openapi: 3.1.0") || !strings.Contains(w.Body.String(), `"contentFormat":"JSON"`)) {
				t.Fatal("import source was returned or canonical format was lost")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		corrupt func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
		want    int
	}{
		{name: "create", want: http.StatusCreated},
		{name: "create-changed-content", corrupt: func(_ *cp.ManagedConfigurationSet, revision *cp.ManagedConfigurationRevision) {
			revision.Content += " "
		}, want: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &managedDraftRecorder{test: managedDraftCase{method: "CreateIntegrationDefinitionDraft", kind: cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION}, corrupt: tc.corrupt}
			body, err := json.Marshal(map[string]string{"name": "Заявки", "contentFormat": "OPENAPI_IMPORT", "content": string(payload)})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/integration-definition-configurations/drafts", string(body)))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	for _, kind := range []string{"prompt-template-configurations", "role-image-configurations", "system-stt-configurations"} {
		client := &managedDraftRecorder{}
		w := httptest.NewRecorder()
		path := "/api/v1/" + kind + "/mcfg_fixture01/revisions/mrev_fixture01/saves"
		managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", path, managedSaveBody("OPENAPI_IMPORT", string(payload))))
		if w.Code != http.StatusBadRequest || client.request != nil {
			t.Fatalf("%s accepted integration import: %d", kind, w.Code)
		}
	}
}

func TestManagedDraftEmptyAndByteBoundary(t *testing.T) {
	for _, tc := range managedDraftCases() {
		if !tc.save {
			continue
		}
		for _, content := range []string{"", "\n  ", strings.Repeat("я", 128<<10)} {
			client := &managedDraftRecorder{test: tc}
			w := httptest.NewRecorder()
			managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", tc.path, managedSaveBody(tc.format, content)))
			if w.Code != 200 {
				t.Fatalf("%s content bytes=%d status=%d body=%s", tc.method, len(content), w.Code, w.Body.String())
			}
		}
	}
}

func TestManagedDraftDiscardedReadPaths(t *testing.T) {
	client := &managedRPCRecorder{revisionState: cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED}
	w := httptest.NewRecorder()
	managedTestHandler(client).ServeHTTP(w, managedTestRequest("GET", "/api/v1/managed-configurations/mcfg_fixture01/revisions", ""))
	var history generated.ManagedConfigurationHistory
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &history) != nil || len(history.Items) != 1 || history.Items[0].State != "DISCARDED" || history.Items[0].Content != managedFixtureContent {
		t.Fatalf("discarded history lost: %d %s", w.Code, w.Body.String())
	}
	configuration, revision := managedFixture()
	revision.State = cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED
	configuration.CurrentRevision = revision
	summary, err := managedConfigurationSummaryView(configuration)
	if err != nil || summary.CurrentRevision == nil || summary.CurrentRevision.State != "DISCARDED" {
		t.Fatalf("discarded summary lost: %+v %v", summary, err)
	}
	raw, err := json.Marshal(summary)
	if err != nil || strings.Contains(string(raw), "content") {
		t.Fatal("catalog summary exposed source content")
	}
}

func TestManagedDraftRejectsInvalidRequests(t *testing.T) {
	for _, tc := range managedDraftCases() {
		body := ""
		if tc.save {
			body = managedSaveBody(tc.format, "source")
		}
		cases := []struct{ name, body, header, value, path string }{
			{"missing-occ", body, "If-Match", "", tc.path},
			{"weak-occ", body, "If-Match", `W/"3"`, tc.path},
			{"unsafe-occ", body, "If-Match", `"9007199254740992"`, tc.path},
			{"missing-idempotency", body, "Idempotency-Key", "", tc.path},
			{"wrong-configuration", body, "", "", strings.Replace(tc.path, "mcfg_fixture01", "short", 1)},
			{"wrong-revision", body, "", "", strings.Replace(tc.path, "mrev_fixture01", "short", 1)},
		}
		if tc.save {
			cases = append(cases,
				struct{ name, body, header, value, path string }{"missing-content", `{"contentFormat":"` + tc.format + `"}`, "", "", tc.path},
				struct{ name, body, header, value, path string }{"null-content", `{"contentFormat":"` + tc.format + `","content":null}`, "", "", tc.path},
				struct{ name, body, header, value, path string }{"missing-format", `{"content":""}`, "", "", tc.path},
				struct{ name, body, header, value, path string }{"unknown-format", managedSaveBody("XML", "x"), "", "", tc.path},
				struct{ name, body, header, value, path string }{"byte-overflow", managedSaveBody(tc.format, strings.Repeat("я", 128<<10)+"я"), "", "", tc.path},
				struct{ name, body, header, value, path string }{"nul", managedSaveBody(tc.format, "\x00"), "", "", tc.path},
				struct{ name, body, header, value, path string }{"forged-owner", strings.TrimSuffix(body, "}") + `,"managedBy":"GIT"}`, "", "", tc.path},
				struct{ name, body, header, value, path string }{"forged-project", strings.TrimSuffix(body, "}") + `,"projectRef":"prj_forged01"}`, "", "", tc.path},
			)
			wrongFormat := "TEXT"
			if tc.format == "TEXT" {
				wrongFormat = "JSON"
			}
			cases = append(cases, struct{ name, body, header, value, path string }{"wrong-kind-format", managedSaveBody(wrongFormat, "x"), "", "", tc.path})
		}
		for _, input := range cases {
			t.Run(tc.method+"/"+input.name, func(t *testing.T) {
				client := &managedDraftRecorder{test: tc}
				r := managedTestRequest("POST", input.path, input.body)
				if input.header != "" {
					r.Header.Del(input.header)
					if input.value != "" {
						r.Header.Set(input.header, input.value)
					}
				}
				w := httptest.NewRecorder()
				managedDraftHandler(client).ServeHTTP(w, r)
				if w.Code != 400 || client.request != nil {
					t.Fatalf("status=%d called=%t body=%s", w.Code, client.request != nil, w.Body.String())
				}
			})
		}
	}
}

func TestManagedDraftOwnerErrorsRemainClosed(t *testing.T) {
	for _, tc := range managedDraftCases() {
		for _, code := range []codes.Code{codes.PermissionDenied, codes.NotFound, codes.Aborted, codes.FailedPrecondition, codes.AlreadyExists, codes.Unavailable} {
			client := &managedDraftRecorder{test: tc, failure: status.Error(code, "private owner data")}
			w := httptest.NewRecorder()
			body := ""
			if tc.save {
				body = managedSaveBody(tc.format, "")
			}
			managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", tc.path, body))
			want := map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.AlreadyExists: 409, codes.Unavailable: 503}[code]
			if w.Code != want || strings.Contains(w.Body.String(), "private owner data") {
				t.Fatalf("%s %s: %d %s", tc.method, code, w.Code, w.Body.String())
			}
		}
	}
}

func TestManagedDraftRejectsWrongOwnerResult(t *testing.T) {
	for _, tc := range managedDraftCases() {
		corruptions := []struct {
			name   string
			change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
		}{
			{"wrong-configuration", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { c.Ref = "mcfg_other01" }},
			{"wrong-version", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { c.Version = 3 }},
			{"wrong-kind", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) {
				c.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_UNSPECIFIED
			}},
			{"git-owner", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) {
				c.ManagedBy = cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_GIT
			}},
			{"wrong-state", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) {
				r.State = cp.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED
			}},
			{"invalid-digest", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { r.Digest = "bad" }},
			{"missing-created", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { r.CreatedAt = nil }},
			{"replaced-published", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { c.CurrentRevision = r }},
		}
		if tc.save {
			corruptions = append(corruptions,
				struct {
					name   string
					change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
				}{"mutable-save", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { r.Ref = "mrev_fixture01" }},
				struct {
					name   string
					change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
				}{"wrong-parent", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) {
					r.ParentRevisionRef = "mrev_other01"
				}},
				struct {
					name   string
					change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
				}{"changed-content", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { r.Content = "different" }},
				struct {
					name   string
					change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
				}{"changed-digest", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) {
					r.Digest = strings.Repeat("b", 64)
				}},
			)
		} else {
			corruptions = append(corruptions, struct {
				name   string
				change func(*cp.ManagedConfigurationSet, *cp.ManagedConfigurationRevision)
			}{"wrong-discard", func(c *cp.ManagedConfigurationSet, r *cp.ManagedConfigurationRevision) { r.Ref = "mrev_other01" }})
		}
		for _, corruption := range corruptions {
			t.Run(tc.method+"/"+corruption.name, func(t *testing.T) {
				client := &managedDraftRecorder{test: tc, corrupt: corruption.change}
				w := httptest.NewRecorder()
				body := ""
				if tc.save {
					body = managedSaveBody(tc.format, "unchanged")
				}
				managedDraftHandler(client).ServeHTTP(w, managedTestRequest("POST", tc.path, body))
				if w.Code != 502 || w.Header().Get("ETag") != "" {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
			})
		}
	}
}
