package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const writeBackBaseFixture = `{"name":"old"}`
const writeBackProposedFixture = `{"name":"new"}`

func writeBackFixture() *cp.ManagedConfigurationGitWriteBack {
	now := time.Unix(1000, 0)
	result := &cp.ManagedConfigurationGitWriteBack{Ref: "wb_fixture01", Version: 1, ConfigurationRef: "mcfg_fixture01", Kind: cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, ConfigurationVersion: 3, SourceRef: "src_fixture01", SourceVersion: 2, ConnectionRef: "conn_fixture01", ConnectionVersion: 4, RepositoryRef: "owner/repository", SourceRefName: "main", Path: "role.json", BaseCommitSha: strings.Repeat("a", 40), BaseContentSha256: writeBackContentDigest(writeBackBaseFixture), ProposedContentSha256: writeBackContentDigest(writeBackProposedFixture), ContentFormat: "JSON", ProposalBranch: "kodex/writeback/wb_fixture01", ApprovalDigest: strings.Repeat("b", 64), State: cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_WAITING_APPROVAL, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	for _, action := range []cp.ManagedConfigurationGitWriteBackAction{cp.ManagedConfigurationGitWriteBackAction_MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_APPROVE, cp.ManagedConfigurationGitWriteBackAction_MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_REJECT, cp.ManagedConfigurationGitWriteBackAction_MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_CANCEL} {
		result.NextActions = append(result.NextActions, &cp.ManagedConfigurationGitWriteBackActionAvailability{Action: action, Reason: cp.ManagedConfigurationGitWriteBackActionReason_MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_REASON_STATE})
	}
	return result
}

func TestWriteBackDedicatedHTTPCommandsAndReadback(t *testing.T) {
	prepare := `{"expectedSourceVersion":2,"content":"{\"name\":\"new\"}"}`
	decision := `{"approvalDigest":"` + strings.Repeat("b", 64) + `"}`
	for _, test := range []struct {
		method, path, body, rpc string
		status                  int
		response                func() proto.Message
	}{
		{"POST", "/api/v1/role-image-configurations/mcfg_fixture01/git-write-backs", prepare, "PrepareRoleImageGitWriteBack", 201, func() proto.Message { return &cp.PrepareRoleImageGitWriteBackResponse{Proposal: writeBackFixture()} }},
		{"POST", "/api/v1/integration-definition-configurations/mcfg_fixture01/git-write-backs", prepare, "PrepareIntegrationDefinitionGitWriteBack", 201, func() proto.Message {
			p := writeBackFixture()
			p.Kind = cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION
			return &cp.PrepareIntegrationDefinitionGitWriteBackResponse{Proposal: p}
		}},
		{"POST", "/api/v1/managed-configuration-git-write-backs/wb_fixture01/approve", decision, "ApproveManagedConfigurationGitWriteBack", 200, func() proto.Message {
			p := writeBackFixture()
			p.Version = 4
			p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED
			return &cp.ApproveManagedConfigurationGitWriteBackResponse{Proposal: p}
		}},
		{"POST", "/api/v1/managed-configuration-git-write-backs/wb_fixture01/reject", decision, "RejectManagedConfigurationGitWriteBack", 200, func() proto.Message {
			p := writeBackFixture()
			p.Version = 4
			p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_REJECTED
			return &cp.RejectManagedConfigurationGitWriteBackResponse{Proposal: p}
		}},
		{"POST", "/api/v1/managed-configuration-git-write-backs/wb_fixture01/cancel", "", "CancelManagedConfigurationGitWriteBack", 200, func() proto.Message {
			p := writeBackFixture()
			p.Version = 4
			p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_CANCELLED
			return &cp.CancelManagedConfigurationGitWriteBackResponse{Proposal: p}
		}},
		{"GET", "/api/v1/managed-configuration-git-write-backs/wb_fixture01", "", "GetManagedConfigurationGitWriteBack", 200, func() proto.Message {
			return &cp.GetManagedConfigurationGitWriteBackResponse{Proposal: writeBackFixture(), BaseContent: writeBackBaseFixture, ProposedContent: writeBackProposedFixture}
		}},
		{"GET", "/api/v1/managed-configurations/mcfg_fixture01/git-write-backs?pageSize=3&pageToken=owner-first", "", "ListManagedConfigurationGitWriteBacks", 200, func() proto.Message {
			return &cp.ListManagedConfigurationGitWriteBacksResponse{Proposals: []*cp.ManagedConfigurationGitWriteBack{writeBackFixture()}, Total: 17, Page: &cp.PageInfo{NextPageToken: "owner-next"}}
		}},
	} {
		t.Run(test.rpc, func(t *testing.T) {
			client := &catalogRPCRecorder{response: test.response()}
			handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client), Query: cp.NewPlatformQueryServiceClient(client)}})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, managedTestRequest(test.method, test.path, test.body))
			if w.Code != test.status || !strings.HasSuffix(client.method, "/"+test.rpc) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("writeback route: %d %s %s", w.Code, client.method, w.Body.String())
			}
			if test.method == "POST" {
				r := client.request.ProtoReflect()
				field := r.Descriptor().Fields().ByName("mutation")
				mutation := r.Get(field).Message().Interface().(*cp.MutationContext)
				if mutation.GetExpectedVersion() != 3 || mutation.IdempotencyKey != "managed-fixture-01" {
					t.Fatal("mutation lost")
				}
			}
			if test.rpc == "ListManagedConfigurationGitWriteBacks" {
				r := client.request.(*cp.ListManagedConfigurationGitWriteBacksRequest)
				if r.GetPage().GetPageToken() != "owner-first" || !strings.Contains(w.Body.String(), `"total":17`) || strings.Contains(w.Body.String(), `"baseContent"`) {
					t.Fatal("list scope or redaction changed")
				}
			}
		})
	}
}

func TestWriteBackProjectionRejectsUnsafeUpstream(t *testing.T) {
	for name, mutate := range map[string]func(*cp.ManagedConfigurationGitWriteBack){
		"unknown state":          func(v *cp.ManagedConfigurationGitWriteBack) { v.State = 999 },
		"unknown failure":        func(v *cp.ManagedConfigurationGitWriteBack) { v.FailureCode = 999 },
		"unknown action":         func(v *cp.ManagedConfigurationGitWriteBack) { v.NextActions[0].Action = 999 },
		"unknown reason":         func(v *cp.ManagedConfigurationGitWriteBack) { v.NextActions[0].Reason = 999 },
		"duplicate action":       func(v *cp.ManagedConfigurationGitWriteBack) { v.NextActions[1] = v.NextActions[0] },
		"false authority":        func(v *cp.ManagedConfigurationGitWriteBack) { v.NextActions[0].Enabled = true },
		"unsafe version":         func(v *cp.ManagedConfigurationGitWriteBack) { v.SourceVersion = maximumSafeJSONInteger + 1 },
		"source branch mutation": func(v *cp.ManagedConfigurationGitWriteBack) { v.ProposalBranch = v.SourceRefName },
		"path traversal":         func(v *cp.ManagedConfigurationGitWriteBack) { v.Path = "../secret" },
		"URL credential":         func(v *cp.ManagedConfigurationGitWriteBack) { v.PullRequestUrl = "https://token@github.com/o/r/pull/1" },
		"success without receipt": func(v *cp.ManagedConfigurationGitWriteBack) {
			v.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED
		},
		"unknown without reason": func(v *cp.ManagedConfigurationGitWriteBack) {
			v.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := writeBackFixture()
			mutate(v)
			if _, ok := configurationWriteBackView(v); ok {
				t.Fatal("invalid proposal accepted")
			}
		})
	}
	p := writeBackFixture()
	p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME
	p.FailureCode = cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_OUTCOME_UNCONFIRMED
	p.CandidateCommitSha = strings.Repeat("c", 40)
	p.BranchConfirmedAt = p.CreatedAt
	if _, ok := configurationWriteBackView(p); !ok {
		t.Fatal("branch receipt incorrectly required PR receipt")
	}
	p.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED
	p.FailureCode = cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNSPECIFIED
	p.PullRequestRef = "42"
	p.PullRequestUrl = "https://github.com/owner/repository/pull/42"
	p.PullRequestConfirmedAt = p.CreatedAt
	p.CompletedAt = p.CreatedAt
	if _, ok := configurationWriteBackView(p); !ok {
		t.Fatal("confirmed external PR rejected")
	}
}

func TestWriteBackInvalidInputStopsBeforeOwner(t *testing.T) {
	for _, body := range []string{`{"expectedSourceVersion":0,"content":"{}"}`, `{"expectedSourceVersion":2,"content":""}`, `{"expectedSourceVersion":2,"content":"{}","actorRef":"actor_caller"}`} {
		client := &catalogRPCRecorder{}
		handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, managedTestRequest("POST", "/api/v1/role-image-configurations/mcfg_fixture01/git-write-backs", body))
		if w.Code != 400 || client.method != "" {
			t.Fatalf("invalid writeback reached owner: %d", w.Code)
		}
	}
}

func TestWriteBackGetDoesNotEchoUnboundContent(t *testing.T) {
	client := &catalogRPCRecorder{response: &cp.GetManagedConfigurationGitWriteBackResponse{Proposal: writeBackFixture(), BaseContent: "unbound private source", ProposedContent: writeBackProposedFixture}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/managed-configuration-git-write-backs/wb_fixture01", nil))
	if w.Code != 502 || strings.Contains(w.Body.String(), "unbound private") {
		t.Fatalf("unbound source leaked: %d", w.Code)
	}
}
