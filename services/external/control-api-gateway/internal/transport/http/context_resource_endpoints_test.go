package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const contextSkillSpec = `{"name":"Fixture skill","description":"Fixture","files":[{"path":"SKILL.md","artifactRef":"art_fixture01","artifactRevision":2}]}`
const contextMemorySpec = `{"title":"Fixture memory","summary":"Fixture summary","sourceRunRef":"run_fixture01","retentionUntil":"2027-09-05T00:00:00Z"}`

func contextFixtures() (*controlplanev1.SkillBundle, *controlplanev1.KodexMemoryRecord, *controlplanev1.AgentContextBinding) {
	stamp := timestamppb.New(time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC))
	provenance := &controlplanev1.ContextProvenance{ActorRef: "usr_fixture01", SourceKind: "USER", SourceRef: "run_fixture01", SourceRevision: "1", Digest: strings.Repeat("a", 64), CreatedAt: stamp}
	skill := &controlplanev1.SkillBundle{Ref: "skl_fixture01", Version: 4, ProjectRef: "prj_fixture01", State: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE, CreatedAt: stamp, UpdatedAt: stamp,
		DraftRevision: &controlplanev1.SkillBundleRevision{Ref: "rev_fixture01", Revision: 2, State: controlplanev1.SkillRevisionState_SKILL_REVISION_STATE_DRAFT, Name: "Fixture skill", Description: "Fixture", Digest: strings.Repeat("a", 64), Provenance: provenance, ScanState: controlplanev1.SkillScanState_SKILL_SCAN_STATE_PENDING,
			Files: []*controlplanev1.SkillBundleFile{{Path: "SKILL.md", ArtifactRef: "art_fixture01", ArtifactRevision: 2, Digest: "sha256:" + strings.Repeat("b", 64), SizeBytes: 50}}}}
	memory := &controlplanev1.KodexMemoryRecord{Ref: "mem_fixture01", Version: 4, ProjectRef: "prj_fixture01", State: controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE, CreatedAt: stamp, UpdatedAt: stamp,
		CurrentRevision: &controlplanev1.MemoryRecordRevision{Ref: "rev_fixture01", Revision: 2, Title: "Fixture memory", Summary: "Fixture summary", Digest: strings.Repeat("a", 64), Provenance: provenance, RetentionUntil: timestamppb.New(time.Date(2027, 9, 5, 0, 0, 0, 0, time.UTC))}}
	binding := &controlplanev1.AgentContextBinding{Ref: "bind_fixture01", Version: 3, AgentRef: "agt_fixture01", ResourceRef: skill.Ref, RevisionRef: "rev_fixture01", Digest: strings.Repeat("a", 64)}
	return skill, memory, binding
}

type contextRPCRecorder struct {
	grpc.ClientConnInterface
	method  string
	request proto.Message
	failure error
	corrupt func(proto.Message)
}

func (client *contextRPCRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	client.method, client.request = method, proto.Clone(request.(proto.Message))
	if client.failure != nil {
		return client.failure
	}
	skill, memory, binding := contextFixtures()
	if strings.Contains(method, "MemoryRecord") {
		binding.ResourceRef = memory.Ref
	}
	if strings.Contains(method, "/Archive") {
		skill.State = controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ARCHIVED
		memory.State = skill.State
	}
	if strings.Contains(method, "/Purge") {
		skill.State = controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED
		skill.DraftRevision = nil
		memory.State = skill.State
		memory.CurrentRevision.Summary = ""
		memory.CurrentRevision.Redacted = true
	}
	target := response.(proto.Message).ProtoReflect()
	fields := target.Descriptor().Fields()
	set := func(name protoreflect.Name, value proto.Message) {
		if f := fields.ByName(name); f != nil {
			target.Set(f, protoreflect.ValueOfMessage(value.ProtoReflect()))
		}
	}
	set("bundle", skill)
	set("record", memory)
	set("binding", binding)
	var items []proto.Message
	var field protoreflect.FieldDescriptor
	switch {
	case strings.HasSuffix(method, "/ListSkillBundles"):
		field = fields.ByName("bundles")
		items = []proto.Message{skill}
	case strings.HasSuffix(method, "/ListMemoryRecords"):
		field = fields.ByName("records")
		items = []proto.Message{memory}
	case strings.HasSuffix(method, "/ListSkillBundleRevisions"):
		field = fields.ByName("revisions")
		items = []proto.Message{skill.DraftRevision}
	case strings.HasSuffix(method, "/ListMemoryRecordRevisions"):
		field = fields.ByName("revisions")
		items = []proto.Message{memory.CurrentRevision}
	}
	if field != nil {
		list := target.Mutable(field).List()
		for _, item := range items {
			list.Append(protoreflect.ValueOfMessage(item.ProtoReflect()))
		}
		target.Set(fields.ByName("total"), protoreflect.ValueOfInt64(1))
		set("page", &controlplanev1.PageInfo{NextPageToken: "next-fixture"})
	}
	if client.corrupt != nil {
		client.corrupt(response.(proto.Message))
	}
	return nil
}
func contextHandler(client *contextRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: controlplanev1.NewPlatformQueryServiceClient(client), Command: controlplanev1.NewPlatformCommandServiceClient(client)}})
}

// Формы CP 7df60ddef: USER_SUMMARY, optional source run и отдельная redaction каждой revision.
func TestMemoryOwnerProjectionHTTP(t *testing.T) {
	for _, state := range []controlplanev1.ContextResourceState{
		controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ACTIVE,
		controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_ARCHIVED,
		controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_EXPIRED,
		controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED,
	} {
		for _, sourceRun := range []bool{false, true} {
			for _, route := range []string{"single", "catalog", "history"} {
				t.Run(state.String()+"/"+route+"/source="+strconv.FormatBool(sourceRun), func(t *testing.T) {
					_, record, _ := contextFixtures()
					record.State = state
					revision := record.CurrentRevision
					revision.Provenance.SourceKind = "USER_SUMMARY"
					if !sourceRun {
						revision.Provenance.SourceRef = ""
						revision.Provenance.SourceRevision = ""
					}
					if state == controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_EXPIRED || state == controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_PURGED || route == "history" {
						revision.Redacted = true
						revision.Summary = ""
						revision.RetentionUntil = timestamppb.New(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
					}
					path := "/api/v1/memory-records/mem_fixture01"
					if route == "catalog" {
						path = "/api/v1/memory-records?state=" + strings.TrimPrefix(state.String(), "CONTEXT_RESOURCE_STATE_")
					} else if route == "history" {
						path += "/revisions"
					}
					client := &contextRPCRecorder{corrupt: func(response proto.Message) {
						switch response := response.(type) {
						case *controlplanev1.GetMemoryRecordResponse:
							response.Record = record
						case *controlplanev1.ListMemoryRecordsResponse:
							response.Records = []*controlplanev1.KodexMemoryRecord{record}
						case *controlplanev1.ListMemoryRecordRevisionsResponse:
							response.Revisions = []*controlplanev1.MemoryRecordRevision{revision}
						}
					}}
					w := httptest.NewRecorder()
					contextHandler(client).ServeHTTP(w, managedTestRequest("GET", path, ""))
					if w.Code != http.StatusOK {
						t.Fatalf("owner projection rejected: status=%d", w.Code)
					}
					var got generated.MemoryRecordRevision
					switch route {
					case "single":
						var result generated.KodexMemoryRecord
						if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
							t.Fatal(err)
						}
						got = result.CurrentRevision
					case "catalog":
						var result generated.MemoryRecordPage
						if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Items) != 1 {
							t.Fatal("invalid memory catalog")
						}
						got = result.Items[0].CurrentRevision
					case "history":
						var result generated.MemoryRecordRevisionPage
						if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Items) != 1 {
							t.Fatal("invalid memory history")
						}
						got = result.Items[0]
					}
					if got.Redacted != revision.Redacted || got.Summary != revision.Summary || got.Digest != revision.Digest || got.Provenance.SourceKind != "USER_SUMMARY" || !got.RetentionUntil.Equal(revision.RetentionUntil.AsTime()) {
						t.Fatal("owner revision metadata changed")
					}
					if sourceRun {
						if got.Provenance.SourceRef == nil || *got.Provenance.SourceRef != "run_fixture01" || got.Provenance.SourceRevision == nil || *got.Provenance.SourceRevision != "1" {
							t.Fatal("source run provenance lost")
						}
					} else if got.Provenance.SourceRef != nil || got.Provenance.SourceRevision != nil {
						t.Fatal("source run provenance fabricated")
					}
				})
			}
		}
	}
}

func TestContextResourceEveryTypedRoute(t *testing.T) {
	cases := []struct {
		method, path, body, rpc string
		code                    int
	}{
		{"GET", "/api/v1/skill-bundles?pageSize=7&pageToken=cursor-fixture", "", "ListSkillBundles", 200},
		{"GET", "/api/v1/skill-bundles/skl_fixture01", "", "GetSkillBundle", 200},
		{"GET", "/api/v1/skill-bundles/skl_fixture01/revisions?pageSize=7&pageToken=cursor-fixture", "", "ListSkillBundleRevisions", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/archive", "", "ArchiveSkillBundle", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/restoration", "", "RestoreSkillBundle", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/purge", "", "PurgeSkillBundle", 200},
		{"PUT", "/api/v1/agents/agt_fixture01/skill-bundles/skl_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":2}`, "BindAgentSkillBundle", 200},
		{"DELETE", "/api/v1/agents/agt_fixture01/skill-bundles/skl_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":2}`, "UnbindAgentSkillBundle", 200},
		{"GET", "/api/v1/memory-records?pageSize=7&pageToken=cursor-fixture", "", "ListMemoryRecords", 200},
		{"GET", "/api/v1/memory-records/mem_fixture01", "", "GetMemoryRecord", 200},
		{"GET", "/api/v1/memory-records/mem_fixture01/revisions?pageSize=7&pageToken=cursor-fixture", "", "ListMemoryRecordRevisions", 200},
		{"POST", "/api/v1/memory-records/mem_fixture01/archive", "", "ArchiveMemoryRecord", 200},
		{"POST", "/api/v1/memory-records/mem_fixture01/restoration", "", "RestoreMemoryRecord", 200},
		{"POST", "/api/v1/memory-records/mem_fixture01/purge", "", "PurgeMemoryRecord", 200},
		{"PUT", "/api/v1/agents/agt_fixture01/memory-records/mem_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":2}`, "BindAgentMemoryRecord", 200},
		{"DELETE", "/api/v1/agents/agt_fixture01/memory-records/mem_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":2}`, "UnbindAgentMemoryRecord", 200},
		{"POST", "/api/v1/projects/prj_fixture01/skill-bundle-drafts", `{"bundleRef":"skl_fixture01","specification":` + contextSkillSpec + `}`, "CreateSkillBundleDraft", 201},
		{"PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", contextSkillSpec, "SaveSkillBundleDraft", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/validation", `{"expectedDigest":"` + strings.Repeat("a", 64) + `"}`, "ValidateSkillBundleDraft", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/review", `{"expectedDigest":"` + strings.Repeat("a", 64) + `","decision":"APPROVE","comment":"fixture"}`, "ReviewSkillBundleDraft", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/publication", `{"expectedDigest":"` + strings.Repeat("a", 64) + `"}`, "PublishSkillBundleDraft", 200},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/discard", `{"expectedDigest":"` + strings.Repeat("a", 64) + `"}`, "DiscardSkillBundleDraft", 200},
		{"POST", "/api/v1/projects/prj_fixture01/memory-records", `{"specification":` + contextMemorySpec + `}`, "CreateMemoryRecord", 201},
		{"POST", "/api/v1/memory-records/mem_fixture01/revisions", contextMemorySpec, "ReviseMemoryRecord", 201},
	}
	for _, tc := range cases {
		t.Run(tc.rpc, func(t *testing.T) {
			client := &contextRPCRecorder{}
			w := httptest.NewRecorder()
			r := managedTestRequest(tc.method, tc.path, tc.body)
			if tc.rpc == "CreateMemoryRecord" {
				r.Header.Del("If-Match")
			}
			contextHandler(client).ServeHTTP(w, r)
			if w.Code != tc.code || !strings.HasSuffix(client.method, "/"+tc.rpc) {
				t.Fatalf("status=%d rpc=%s body=%s", w.Code, client.method, w.Body.String())
			}
			fields := client.request.ProtoReflect()
			descriptor := fields.Descriptor().Fields()
			for name, want := range map[protoreflect.Name]string{"bundle_ref": "skl_fixture01", "record_ref": "mem_fixture01", "revision_ref": "rev_fixture01", "expected_digest": strings.Repeat("a", 64)} {
				if f := descriptor.ByName(name); f != nil && fields.Get(f).String() != want {
					t.Fatalf("field %s mismatch", name)
				}
			}
			if f := descriptor.ByName("mutation"); f != nil {
				mutation := fields.Get(f).Message().Interface().(*controlplanev1.MutationContext)
				want := int64(3)
				if tc.rpc == "CreateMemoryRecord" {
					want = 0
				}
				if mutation.IdempotencyKey != "managed-fixture-01" || mutation.GetExpectedVersion() != want {
					t.Fatal("mutation mapping mismatch")
				}
			}
			if f := descriptor.ByName("page"); f != nil {
				page := fields.Get(f).Message().Interface().(*controlplanev1.PageRequest)
				if page.PageSize != 7 || page.PageToken != "cursor-fixture" {
					t.Fatal(page)
				}
			}
			if f := descriptor.ByName("expected_binding_version"); f != nil {
				if fields.Get(f).Int() != 2 || w.Header().Get("ETag") != "" {
					t.Fatal("agent OCC must not be replaced by binding ETag")
				}
			}
			var payload map[string]any
			if json.Unmarshal(w.Body.Bytes(), &payload) != nil {
				t.Fatal("invalid JSON")
			}
		})
	}
}

func TestContextSkillNameUsesUnicodeCharacters(t *testing.T) {
	for _, size := range []int{160, 161} {
		name := strings.Repeat("я", size)
		want := size == 160
		_, ok := skillSpecificationInput(generated.SkillBundleSpecification{Name: name, Files: []generated.SkillBundleFileInput{{Path: "SKILL.md", ArtifactRef: "art_fixture01", ArtifactRevision: 1}}})
		if ok != want {
			t.Fatalf("input: characters=%d accepted=%t", size, ok)
		}
		skill, _, _ := contextFixtures()
		skill.DraftRevision.Name = name
		view, ok := skillRevisionView(skill.DraftRevision)
		if ok != want || ok && view.Name != name {
			t.Fatalf("response: characters=%d accepted=%t", size, ok)
		}
	}
}

func TestContextResourceInputRejectsAuthorityAndMalformedBeforeRPC(t *testing.T) {
	for _, tc := range []struct{ name, method, path, body string }{
		{"provenance", "POST", "/api/v1/projects/prj_fixture01/memory-records", `{"specification":` + contextMemorySpec + `,"provenance":{"actorRef":"usr_forged01"}}`},
		{"scan", "PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", strings.TrimSuffix(contextSkillSpec, "}") + `,"scanState":"CLEAN"}`},
		{"traversal", "PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", strings.Replace(contextSkillSpec, "SKILL.md", "../SKILL.md", 1)},
		{"absolute", "PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", strings.Replace(contextSkillSpec, "SKILL.md", "/SKILL.md", 1)},
		{"unsafe-revision", "PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", strings.Replace(contextSkillSpec, `"artifactRevision":2`, `"artifactRevision":9007199254740992`, 1)},
		{"duplicate-path", "PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01", `{"name":"Fixture","description":"","files":[{"path":"SKILL.md","artifactRef":"art_fixture01","artifactRevision":1},{"path":"SKILL.md","artifactRef":"art_fixture02","artifactRevision":1}]}`},
		{"missing-retention", "POST", "/api/v1/memory-records/mem_fixture01/revisions", `{"title":"Fixture","summary":"Fixture"}`},
		{"unknown-state", "GET", "/api/v1/memory-records?state=UNSPECIFIED", ""},
		{"overflow-page", "GET", "/api/v1/skill-bundles?pageSize=4294967297", ""},
		{"review-decision", "POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/review", `{"expectedDigest":"` + strings.Repeat("a", 64) + `","decision":"BYPASS","comment":""}`},
		{"negative-binding-version", "PUT", "/api/v1/agents/agt_fixture01/memory-records/mem_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":-1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &contextRPCRecorder{}
			w := httptest.NewRecorder()
			contextHandler(client).ServeHTTP(w, managedTestRequest(tc.method, tc.path, tc.body))
			if w.Code != 400 || client.request != nil {
				t.Fatalf("status=%d called=%t body=%s", w.Code, client.request != nil, w.Body.String())
			}
		})
	}
}

func TestContextResourceNewDraftOCCAndBodyMapping(t *testing.T) {
	client := &contextRPCRecorder{}
	w := httptest.NewRecorder()
	r := managedTestRequest("POST", "/api/v1/projects/prj_fixture01/skill-bundle-drafts", `{"specification":`+contextSkillSpec+`}`)
	r.Header.Del("If-Match")
	contextHandler(client).ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	input := client.request.(*controlplanev1.CreateSkillBundleDraftRequest)
	if input.BundleRef != "" || input.ProjectRef != "prj_fixture01" || input.Mutation.ExpectedVersion != nil || len(input.Specification.Files) != 1 || input.Specification.Files[0].ArtifactRevision != 2 {
		t.Fatal(input)
	}
	for _, existing := range []bool{false, true} {
		client = &contextRPCRecorder{}
		w = httptest.NewRecorder()
		body := `{"specification":` + contextSkillSpec + `}`
		if existing {
			body = `{"bundleRef":"skl_fixture01","specification":` + contextSkillSpec + `}`
		}
		r = managedTestRequest("POST", "/api/v1/projects/prj_fixture01/skill-bundle-drafts", body)
		if existing {
			r.Header.Del("If-Match")
		}
		contextHandler(client).ServeHTTP(w, r)
		if w.Code != 400 || client.request != nil {
			t.Fatalf("invalid create OCC status=%d", w.Code)
		}
	}
}

func TestContextMemoryTitleUsesUnicodeCharacters(t *testing.T) {
	for _, length := range []int{160, 161} {
		title := strings.Repeat("я", length)
		client := &contextRPCRecorder{}
		w := httptest.NewRecorder()
		body := strings.Replace(contextMemorySpec, "Fixture memory", title, 1)
		contextHandler(client).ServeHTTP(w, managedTestRequest("POST", "/api/v1/memory-records/mem_fixture01/revisions", body))
		want := 201
		if length > 160 {
			want = 400
		}
		if w.Code != want {
			t.Fatalf("title length=%d status=%d", length, w.Code)
		}
		if length <= 160 && client.request.(*controlplanev1.ReviseMemoryRecordRequest).Specification.Title != title {
			t.Fatal("title changed during mapping")
		}
		_, record, _ := contextFixtures()
		record.CurrentRevision.Title = title
		_, ok := memoryRecordView(record)
		if ok != (length <= 160) {
			t.Fatalf("response title length=%d accepted=%t", length, ok)
		}
	}
}

func TestContextResourceAuthorityAndUnimplementedRemainFailures(t *testing.T) {
	for code, want := range map[codes.Code]int{codes.PermissionDenied: 403, codes.NotFound: 404, codes.Unauthenticated: 401, codes.Aborted: 412, codes.Unavailable: 503, codes.Unimplemented: 500} {
		for _, path := range []string{"/api/v1/skill-bundles/skl_fixture01/archive", "/api/v1/memory-records/mem_fixture01/purge"} {
			client := &contextRPCRecorder{failure: status.Error(code, "private-fixture-detail")}
			w := httptest.NewRecorder()
			contextHandler(client).ServeHTTP(w, managedTestRequest("POST", path, ""))
			if w.Code != want || strings.Contains(w.Body.String(), "private-fixture-detail") {
				t.Fatalf("status=%d want=%d", w.Code, want)
			}
		}
	}
}

func TestContextResourceRejectsCorruptOrUnredactedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		corrupt    func(proto.Message)
	}{
		{"foreign", "/api/v1/skill-bundles/skl_fixture01", func(m proto.Message) { m.(*controlplanev1.GetSkillBundleResponse).Bundle.Ref = "skl_foreign01" }},
		{"wrong-filter", "/api/v1/skill-bundles?projectRef=prj_other01", func(proto.Message) {}},
		{"unknown-state", "/api/v1/memory-records/mem_fixture01", func(m proto.Message) { m.(*controlplanev1.GetMemoryRecordResponse).Record.State = 99 }},
		{"expired-content", "/api/v1/memory-records/mem_fixture01", func(m proto.Message) {
			m.(*controlplanev1.GetMemoryRecordResponse).Record.State = controlplanev1.ContextResourceState_CONTEXT_RESOURCE_STATE_EXPIRED
		}},
		{"redacted-content", "/api/v1/memory-records/mem_fixture01", func(m proto.Message) {
			m.(*controlplanev1.GetMemoryRecordResponse).Record.CurrentRevision.Redacted = true
		}},
		{"invalid-provenance", "/api/v1/memory-records/mem_fixture01", func(m proto.Message) {
			m.(*controlplanev1.GetMemoryRecordResponse).Record.CurrentRevision.Provenance = nil
		}},
		{"manifest-traversal", "/api/v1/skill-bundles/skl_fixture01", func(m proto.Message) {
			m.(*controlplanev1.GetSkillBundleResponse).Bundle.DraftRevision.Files[0].Path = "../SKILL.md"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &contextRPCRecorder{corrupt: tc.corrupt}
			w := httptest.NewRecorder()
			contextHandler(client).ServeHTTP(w, managedTestRequest("GET", tc.path, ""))
			if w.Code != 502 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
