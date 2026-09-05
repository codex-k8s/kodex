package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const mailboxConnectionPath = "/api/v1/integration-connections/conn_fixture01/email-mailbox"
const mailboxRevisionPath = "/api/v1/email-mailbox-configurations/mcfg_fixture01/revisions/mrev_fixture01"

func mailboxViewFixture() *cp.EmailMailboxConfigurationView {
	configuration, revision := managedFixture()
	configuration.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_EMAIL_MAILBOX
	revision.Ref = "mrev_fixture01"
	revision.ContentFormat = "JSON"
	revision.Content = "{}"
	configuration.Ref = "mcfg_fixture01"
	return &cp.EmailMailboxConfigurationView{ConnectionRef: "conn_fixture01", ConnectionVersion: 9, MailboxRef: "mbx_fixture01", Configuration: configuration, Revision: revision, Specification: &cp.EmailMailboxSpecification{}, NextActions: mailboxActionsFixture()}
}

func mailboxActionsFixture() []*cp.EmailMailboxActionAvailability {
	result := make([]*cp.EmailMailboxActionAvailability, 0, 9)
	for action := cp.EmailMailboxAction_EMAIL_MAILBOX_ACTION_CREATE_DRAFT; action <= cp.EmailMailboxAction_EMAIL_MAILBOX_ACTION_COPY; action++ {
		result = append(result, &cp.EmailMailboxActionAvailability{Action: action, Reason: cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_STATE})
	}
	result[0].Enabled, result[0].Reason = true, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_NONE
	return result
}
func mailboxPublicationFixture() *cp.EmailMailboxPublication {
	return &cp.EmailMailboxPublication{Ref: "mpub_fixture01", Revision: 2, Digest: strings.Repeat("b", 64), State: cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_PENDING, ConfigurationRevisionRef: "mrev_fixture01", CreatedAt: timestamppb.New(time.Now().Add(-time.Minute))}
}

type mailboxRecorder struct {
	grpc.ClientConnInterface
	method         string
	request        proto.Message
	failure        error
	mutate         func(*cp.EmailMailboxConfigurationView)
	publication    *cp.EmailMailboxPublication
	preview        *cp.PreviewEmailMailboxConfigurationResponse
	credentials    []*cp.EmailMailboxCredential
	configurations *cp.ListEmailMailboxConfigurationsResponse
}

func (c *mailboxRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	c.method = method
	c.request = proto.Clone(request.(proto.Message))
	if c.failure != nil {
		return c.failure
	}
	view := mailboxViewFixture()
	if strings.HasSuffix(method, "/SaveEmailMailboxDraft") {
		view.Revision.Ref = "mrev_saved01"
	}
	if strings.HasSuffix(method, "/BindEmailMailboxConfiguration") {
		view.Publication = mailboxPublicationFixture()
	}
	if c.mutate != nil {
		c.mutate(view)
	}
	credential := &cp.EmailMailboxCredential{Name: "cred_fixture01", Generation: 9, Kind: cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_AUTH_SECRET, ConnectionRef: "conn_fixture01", ConnectionVersion: 9}
	switch out := response.(type) {
	case *cp.ListEmailMailboxConfigurationsResponse:
		if c.configurations != nil {
			proto.Merge(out, c.configurations)
			return nil
		}
		out.Items = []*cp.EmailMailboxConfigurationView{view}
		out.NextActions = mailboxActionsFixture()[:1]
		out.Total = 2
		out.Page = &cp.PageInfo{NextPageToken: "fixture-page"}
	case *cp.ListEmailMailboxCredentialsResponse:
		out.Items = []*cp.EmailMailboxCredential{credential}
		if c.credentials != nil {
			out.Items = c.credentials
		}
		out.Total = 2
		out.Page = &cp.PageInfo{NextPageToken: "fixture-page"}
	case *cp.GetEmailMailboxCredentialReceiptResponse:
		out.Credential = credential
	case *cp.PreviewEmailMailboxConfigurationResponse:
		if c.preview != nil {
			proto.Merge(out, c.preview)
			return nil
		}
		out.Specification = view.Specification
		out.CanonicalYaml = "{}\n"
		out.Valid = true
	case *cp.UnbindEmailMailboxConfigurationResponse:
		out.Publication = mailboxPublicationFixture()
		out.Publication.ConfigurationRevisionRef = ""
		out.ConnectionVersion = 9
		if c.publication != nil {
			out.Publication = c.publication
		}
	default:
		message := response.(proto.Message).ProtoReflect()
		message.Set(message.Descriptor().Fields().ByName("configuration"), protoreflect.ValueOfMessage(view.ProtoReflect()))
	}
	return nil
}
func mailboxHandler(c *mailboxRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c), Command: cp.NewPlatformCommandServiceClient(c)}})
}

func TestMailboxCredentialIdentityIncludesGeneration(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		first := &cp.EmailMailboxCredential{Name: "cred_fixture01", Generation: 9, Kind: cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_AUTH_SECRET, ConnectionRef: "conn_fixture01", ConnectionVersion: 9}
		second := proto.Clone(first).(*cp.EmailMailboxCredential)
		if !duplicate {
			second.Generation = 10
			second.ConnectionVersion = 10
		}
		c := &mailboxRecorder{credentials: []*cp.EmailMailboxCredential{first, second}}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("GET", mailboxConnectionPath+"/credentials", ""))
		expected := 200
		if duplicate {
			expected = 502
		}
		if w.Code != expected {
			t.Fatalf("credential identity: duplicate=%v status=%d", duplicate, w.Code)
		}
	}
}

func TestMailboxPublicationClosedFailureCodesAndIndependentHistory(t *testing.T) {
	for _, code := range []string{"", "EMAIL_MAILBOX_DELIVERY_EXPIRED", "EMAIL_MAILBOX_CONNECTION_CHANGED", "EMAIL_MAILBOX_DELIVERY_REJECTED", "FUTURE_FAILURE", "private failure text"} {
		for _, state := range []cp.EmailMailboxPublicationState{cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_FAILED, cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_PENDING, cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_SUPERSEDED} {
			v := mailboxPublicationFixture()
			v.State, v.FailureCode = state, code
			_, ok := mailboxPublicationView(v)
			expected := state != cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_FAILED && code == "" || state == cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_FAILED && (code == "EMAIL_MAILBOX_DELIVERY_EXPIRED" || code == "EMAIL_MAILBOX_CONNECTION_CHANGED" || code == "EMAIL_MAILBOX_DELIVERY_REJECTED")
			if ok != expected {
				t.Fatalf("publication failure registry mismatch: state=%s", state)
			}
		}
	}
	view := mailboxViewFixture()
	view.Publication = mailboxPublicationFixture()
	view.Publication.ConfigurationRevisionRef = "mrev_published01"
	view.BoundRevisionRef = "mrev_published01"
	result, ok := mailboxConfigurationView(view)
	if !ok || result.Revision.Ref != "mrev_fixture01" || result.Publication.ConfigurationRevisionRef != "mrev_published01" {
		t.Fatal("selected draft replaced connection publication history")
	}
}

func TestMailboxActionsPreserveOwnerAvailabilityAndEmptyListCreate(t *testing.T) {
	for _, reason := range []cp.EmailMailboxActionReason{cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_NONE, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_STATE, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_GIT_MANAGED, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_DELIVERY_PENDING, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_NO_BINDING, cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_CONNECTION_DISABLED} {
		actions := mailboxActionsFixture()
		actions[0].Reason, actions[0].Enabled = reason, reason == cp.EmailMailboxActionReason_EMAIL_MAILBOX_ACTION_REASON_NONE
		views, ok := mailboxActionViews(actions, false)
		if !ok || len(views) != 9 || views[0].Enabled != actions[0].Enabled || string(views[0].Reason) != strings.TrimPrefix(reason.String(), "EMAIL_MAILBOX_ACTION_REASON_") {
			t.Fatal("owner availability changed")
		}
		c := &mailboxRecorder{configurations: &cp.ListEmailMailboxConfigurationsResponse{NextActions: actions[:1]}}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("GET", mailboxConnectionPath+"/configurations?query=empty", ""))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"nextActions":[{"action":"CREATE_DRAFT"`) || !strings.Contains(w.Body.String(), `"items":[]`) {
			t.Fatal("empty list lost connection create availability")
		}
	}
}

func TestMailboxActionsRejectMissingUnknownAndContradictoryProjection(t *testing.T) {
	for _, mutate := range []func([]*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability{
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability { return nil },
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability { return a[:8] },
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[1] = a[0]
			return a
		},
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[0] = nil
			return a
		},
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[0].Action = cp.EmailMailboxAction(999)
			return a
		},
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[0].Reason = cp.EmailMailboxActionReason(999)
			return a
		},
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[0].Enabled = false
			return a
		},
		func(a []*cp.EmailMailboxActionAvailability) []*cp.EmailMailboxActionAvailability {
			a[1].Enabled = true
			return a
		},
	} {
		c := &mailboxRecorder{mutate: func(v *cp.EmailMailboxConfigurationView) { v.NextActions = mutate(v.NextActions) }}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("GET", mailboxConnectionPath+"/configuration", ""))
		if w.Code != 502 {
			t.Fatal("invalid action projection returned")
		}
	}
	if _, ok := mailboxActionViews(mailboxActionsFixture()[1:2], true); ok {
		t.Fatal("non-create list action accepted")
	}
}

func TestMailboxRoutesPreserveTypedRPCAndIndependentOCC(t *testing.T) {
	for _, test := range []struct {
		method, path, body, rpc string
		code                    int
	}{
		{"GET", mailboxConnectionPath + "/configurations?query=needle&pageSize=7&pageToken=previous", "", "ListEmailMailboxConfigurations", 200},
		{"GET", mailboxConnectionPath + "/configuration?configurationRef=mcfg_fixture01&revisionRef=mrev_fixture01", "", "GetEmailMailboxConfiguration", 200},
		{"GET", mailboxConnectionPath + "/credentials?kind=AUTH_SECRET&pageSize=7&pageToken=previous", "", "ListEmailMailboxCredentials", 200},
		{"GET", mailboxConnectionPath + "/credential-receipt?idempotencyKey=lost-write-key", "", "GetEmailMailboxCredentialReceipt", 200},
		{"POST", mailboxConnectionPath + "/preview", `{"specification":{}}`, "PreviewEmailMailboxConfiguration", 200},
		{"POST", mailboxConnectionPath + "/drafts", `{"name":"Fixture","content":{"yaml":"enabled: false\n"}}`, "CreateEmailMailboxDraft", 201},
		{"POST", mailboxRevisionPath + "/saves", `{"specification":{}}`, "SaveEmailMailboxDraft", 200},
		{"POST", mailboxRevisionPath + "/validation", "", "ValidateEmailMailboxDraft", 200},
		{"POST", mailboxRevisionPath + "/publication", "", "PublishEmailMailboxDraft", 200},
		{"POST", mailboxRevisionPath + "/discard", "", "DiscardEmailMailboxDraft", 200},
		{"POST", mailboxRevisionPath + "/binding", `{"connectionRef":"conn_fixture01","expectedConnectionVersion":8}`, "BindEmailMailboxConfiguration", 200},
		{"DELETE", mailboxConnectionPath + "/binding", "", "UnbindEmailMailboxConfiguration", 200},
	} {
		t.Run(test.rpc, func(t *testing.T) {
			c := &mailboxRecorder{}
			w := httptest.NewRecorder()
			r := managedTestRequest(test.method, test.path, test.body)
			if test.rpc == "CreateEmailMailboxDraft" {
				r.Header.Del("If-Match")
			}
			mailboxHandler(c).ServeHTTP(w, r)
			if w.Code != test.code || !strings.HasSuffix(c.method, "/"+test.rpc) {
				t.Fatalf("wrong mailbox mapping: %d %s %s", w.Code, c.method, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("mailbox metadata cached")
			}
			if input, ok := c.request.(interface{ GetMutation() *cp.MutationContext }); ok {
				expected := int64(3)
				if test.rpc == "CreateEmailMailboxDraft" {
					expected = 0
				}
				if input.GetMutation().IdempotencyKey != "managed-fixture-01" || input.GetMutation().GetExpectedVersion() != expected {
					t.Fatal("owner OCC/idempotency lost")
				}
			}
			switch input := c.request.(type) {
			case *cp.ListEmailMailboxConfigurationsRequest:
				if input.ConnectionRef != "conn_fixture01" || input.Query != "needle" || input.Page.PageSize != 7 || input.Page.PageToken != "previous" {
					t.Fatal("server query lost")
				}
			case *cp.GetEmailMailboxConfigurationRequest:
				if input.ConfigurationRef != "mcfg_fixture01" || input.RevisionRef != "mrev_fixture01" {
					t.Fatal("selected revision pin lost")
				}
			case *cp.ListEmailMailboxCredentialsRequest:
				if input.Kind != cp.EmailMailboxCredentialKind_EMAIL_MAILBOX_CREDENTIAL_KIND_AUTH_SECRET || input.Page.PageToken != "previous" {
					t.Fatal("descriptor filter/cursor lost")
				}
			case *cp.GetEmailMailboxCredentialReceiptRequest:
				if input.IdempotencyKey != "lost-write-key" {
					t.Fatal("receipt key lost")
				}
			case *cp.CreateEmailMailboxDraftRequest:
				if input.Content.GetYaml() != "enabled: false\n" || input.ConfigurationRef != "" {
					t.Fatal("YAML or create lineage changed")
				}
			case *cp.BindEmailMailboxConfigurationRequest:
				if input.ExpectedConnectionVersion != 8 || input.Mutation.GetExpectedVersion() != 3 || input.ConnectionRef != "conn_fixture01" {
					t.Fatal("connection and set OCC mixed")
				}
				if !strings.Contains(w.Body.String(), `"state":"PENDING"`) {
					t.Fatal("bind fabricated ready")
				}
			}
		})
	}
}

func TestMailboxRejectsCallerAuthorityUnknownContentAndMissingFences(t *testing.T) {
	for _, body := range []string{`{"specification":{},"yaml":"{}"}`, `{}`, `{"specification":{"tenant":"org_other"}}`, `{"specification":{"smtp":{"value":"forbidden"}}}`, `{"specification":{"receiveProtocol":"SMTP"}}`} {
		c := &mailboxRecorder{}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("POST", mailboxRevisionPath+"/saves", body))
		if w.Code != 400 || c.request != nil {
			t.Fatal("unsafe content reached owner")
		}
	}
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		c := &mailboxRecorder{}
		w := httptest.NewRecorder()
		r := managedTestRequest("POST", mailboxRevisionPath+"/publication", "")
		r.Header.Del(header)
		mailboxHandler(c).ServeHTTP(w, r)
		if w.Code < 400 || c.request != nil {
			t.Fatal("missing mutation fence reached owner")
		}
	}
	c := &mailboxRecorder{}
	w := httptest.NewRecorder()
	mailboxHandler(c).ServeHTTP(w, managedTestRequest("POST", mailboxConnectionPath+"/drafts", `{"configurationRef":"mcfg_fixture01","name":"Fixture","content":{"specification":{}}}`))
	if w.Code != 201 || c.request.(*cp.CreateEmailMailboxDraftRequest).Mutation.GetExpectedVersion() != 3 {
		t.Fatal("new revision lost set OCC")
	}
}

func TestMailboxRejectsBrokenReadbackAndMapsConcreteOwnerErrors(t *testing.T) {
	for _, mutate := range []func(*cp.EmailMailboxConfigurationView){
		func(v *cp.EmailMailboxConfigurationView) { v.ConnectionRef = "conn_other01" },
		func(v *cp.EmailMailboxConfigurationView) {
			v.Configuration.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT
		},
		func(v *cp.EmailMailboxConfigurationView) { v.Specification = nil },
		func(v *cp.EmailMailboxConfigurationView) { v.Revision.Ref = "mrev_other01" },
		func(v *cp.EmailMailboxConfigurationView) { v.ConnectionVersion = 0 },
		func(v *cp.EmailMailboxConfigurationView) {
			v.Diagnostics = []*cp.EmailMailboxDiagnostic{{Code: "EMAIL_MAILBOX_SYNTAX_INVALID", Message: "private input echo"}}
		},
		func(v *cp.EmailMailboxConfigurationView) {
			v.Publication = mailboxPublicationFixture()
			v.Publication.State = cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_READY
		},
	} {
		c := &mailboxRecorder{mutate: mutate}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("GET", mailboxConnectionPath+"/configuration?revisionRef=mrev_fixture01", ""))
		if w.Code != 502 {
			t.Fatalf("bad owner readback accepted: %d", w.Code)
		}
	}
	for code, expected := range map[codes.Code]int{codes.InvalidArgument: 400, codes.Unauthenticated: 401, codes.PermissionDenied: 403, codes.NotFound: 404, codes.Aborted: 412, codes.FailedPrecondition: 409, codes.AlreadyExists: 409, codes.Unavailable: 503, codes.DeadlineExceeded: 504} {
		for _, path := range []string{mailboxConnectionPath + "/credential-receipt?idempotencyKey=lost", mailboxConnectionPath + "/configurations"} {
			c := &mailboxRecorder{failure: status.Error(code, "private upstream")}
			w := httptest.NewRecorder()
			mailboxHandler(c).ServeHTTP(w, managedTestRequest("GET", path, ""))
			if w.Code != expected || strings.Contains(w.Body.String(), "private upstream") {
				t.Fatal("owner error mapping changed")
			}
		}
	}
}

func TestMailboxPublicationAndDiagnosticsNeverInventReadinessOrEchoValues(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		response := &cp.PreviewEmailMailboxConfigurationResponse{Valid: false, Diagnostics: []*cp.EmailMailboxDiagnostic{{Code: "EMAIL_MAILBOX_SYNTAX_INVALID", Message: "Mailbox document syntax or fields are invalid", Line: 2, Column: 3}}}
		if invalid {
			response.CanonicalYaml = "caller-secret-value"
		}
		c := &mailboxRecorder{preview: response}
		w := httptest.NewRecorder()
		mailboxHandler(c).ServeHTTP(w, managedTestRequest("POST", mailboxConnectionPath+"/preview", `{"yaml":"broken input"}`))
		if invalid && w.Code != 502 || !invalid && w.Code != 200 || strings.Contains(w.Body.String(), "caller-secret-value") {
			t.Fatal("syntax failure leaked restored input or lost diagnostics")
		}
	}
	for _, state := range []cp.EmailMailboxPublicationState{cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_PENDING, cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_READY, cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_SUPERSEDED, cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_FAILED} {
		v := mailboxPublicationFixture()
		v.State = state
		if state == cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_FAILED {
			v.FailureCode = "EMAIL_MAILBOX_DELIVERY_REJECTED"
		}
		if state == cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_READY || state == cp.EmailMailboxPublicationState_EMAIL_MAILBOX_PUBLICATION_STATE_SUPERSEDED {
			v.ReadyAt = timestamppb.New(time.Now())
		}
		view, ok := mailboxPublicationView(v)
		if !ok || view.State == "READY" && view.ReadyAt == nil {
			t.Fatal("publication state lost")
		}
	}
	for _, diag := range []*cp.EmailMailboxDiagnostic{{Code: "EMAIL_MAILBOX_SYNTAX_INVALID", Message: "Mailbox document syntax or fields are invalid"}, {Code: "EMAIL_MAILBOX_CONFIGURATION_INVALID", Message: "Mailbox configuration is incomplete or invalid"}, {Code: "EMAIL_MAILBOX_CREDENTIAL_MISMATCH", Message: "Mailbox credential reference is unavailable"}} {
		if _, ok := mailboxDiagnostics([]*cp.EmailMailboxDiagnostic{diag}); !ok {
			t.Fatal("closed safe diagnostic rejected")
		}
		bad := proto.Clone(diag).(*cp.EmailMailboxDiagnostic)
		bad.Path = "private-value"
		if _, ok := mailboxDiagnostics([]*cp.EmailMailboxDiagnostic{bad}); ok {
			t.Fatal("caller content escaped in path")
		}
	}
}
