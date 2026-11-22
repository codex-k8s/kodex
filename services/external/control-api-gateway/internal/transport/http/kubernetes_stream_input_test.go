package httptransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// inputHTTPRecorder captures the borrowed private request buffer through the real generated adapter so clearing can be verified after the handler returns.
// inputHTTPRecorder фиксирует borrowed private request buffer через actual generated adapter для проверки очистки после handler return.
type inputHTTPRecorder struct {
	*catalogRPCRecorder
	borrowed []byte
}

// Invoke records one actual typed RPC without inventing owner authorization, storage durability or a hidden retry.
// Invoke фиксирует один actual typed RPC без выдуманных owner authorization, storage durability либо hidden retry.
func (recorder *inputHTTPRecorder) Invoke(ctx context.Context, method string, in, out any, options ...grpc.CallOption) error {
	if request, ok := in.(*cp.SubmitManagedKubernetesStreamInputRequest); ok {
		recorder.borrowed = request.Payload
	}
	return recorder.catalogRPCRecorder.Invoke(ctx, method, in, out, options...)
}

// inputHTTPHandler wires the production generated router and new protected typed client; browser authentication and actual owner persistence are separate qualification axes.
// inputHTTPHandler соединяет production generated router и новый protected typed client; browser authentication и actual owner persistence проверяются отдельно.
func inputHTTPHandler(recorder *inputHTTPRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesInputs: cp.NewManagedKubernetesStreamInputServiceClient(recorder)}})
}

// inputHTTPViewFixture creates synthetic safe current metadata with a private-free pending tuple.
// inputHTTPViewFixture создаёт synthetic safe current metadata с pending tuple без private bytes.
func inputHTTPViewFixture() *cp.ManagedKubernetesStreamInputView {
	return &cp.ManagedKubernetesStreamInputView{Resource: &cp.ManagedKubernetesResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "example", Name: "web", Uid: "30000000-0000-4000-8000-000000000001", ResourceVersion: "18"}, TargetRef: "target_review", TargetRevision: 2, ConnectionRef: "int_synthetic", InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", Generation: 7, Version: 4, Operation: "kubernetes.exec", Namespace: "example", PodName: "web", Container: "app", State: cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE, InputChannels: []string{"STDIN", "TERMINAL_RESIZE"}, MaximumFrameBytes: 32768, MaximumTotalBytes: 65536, MaximumStagedBytes: 1 << 30, MaximumStagedObjects: 32768, HardDeadline: timestamppb.New(time.Now().Add(time.Minute))}
}

// inputHTTPBody encodes only synthetic source data while retaining the exact private digest and explicit EOF flag.
// inputHTTPBody кодирует лишь synthetic source data с exact private digest и explicit EOF flag.
func inputHTTPBody(channel, payload string, eof bool) string {
	digest := sha256.Sum256([]byte(payload))
	body, _ := json.Marshal(map[string]any{"channel": channel, "payloadBase64": base64.StdEncoding.EncodeToString([]byte(payload)), "payloadDigest": hex.EncodeToString(digest[:]), "endOfInput": eof})
	return string(body)
}

// inputHTTPRequest supplies canonical mutation headers to the actual generated HTTP path.
// inputHTTPRequest задаёт canonical mutation headers настоящему generated HTTP path.
func inputHTTPRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "public-input-key")
	request.Header.Set("If-Match", "\"3\"")
	request.Header.Set("X-CSRF-Token", "synthetic-csrf")
	return request
}

// TestKubernetesStreamInputHTTPActualRoutes preserves explicit stream selection, server-assigned sequence, original mutation and metadata-only output through real router/client glue.
// TestKubernetesStreamInputHTTPActualRoutes сохраняет explicit stream selection, server-assigned sequence, original mutation и metadata-only output через actual router/client glue.
func TestKubernetesStreamInputHTTPActualRoutes(t *testing.T) {
	const inputPath = "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/input"
	payload := "private-synthetic-input"
	digest := sha256.Sum256([]byte(payload))
	view := inputHTTPViewFixture()
	submission := &cp.ManagedKubernetesStreamInputSubmission{Ref: "kif_synthetic", InvocationRef: view.InvocationRef, StreamRef: view.StreamRef, Generation: view.Generation, Sequence: 1, Channel: "STDIN", PayloadDigest: hex.EncodeToString(digest[:]), ByteCount: uint32(len(payload)), State: cp.ManagedKubernetesStreamInputSubmissionState_MANAGED_KUBERNETES_STREAM_INPUT_SUBMISSION_STATE_QUEUED}
	for _, item := range []struct {
		name, method, path, body, operation string
		response                            proto.Message
		status                              int
	}{
		{"discover", http.MethodGet, "/api/v1/runs/run_synthetic/nodes/nod_synthetic/kubernetes-stream-inputs?pageSize=20", "", cp.ManagedKubernetesStreamInputService_ListManagedKubernetesStreamInputs_FullMethodName, &cp.ListManagedKubernetesStreamInputsResponse{Items: []*cp.ManagedKubernetesStreamInputView{view}, Page: &cp.PageInfo{NextPageToken: "next.cursor"}}, 200},
		{"exact read", http.MethodGet, inputPath, "", cp.ManagedKubernetesStreamInputService_GetManagedKubernetesStreamInput_FullMethodName, &cp.GetManagedKubernetesStreamInputResponse{View: view}, 200},
		{"original key read", http.MethodGet, inputPath + "?submissionIdempotencyKey=public-input-key", "", cp.ManagedKubernetesStreamInputService_GetManagedKubernetesStreamInput_FullMethodName, &cp.GetManagedKubernetesStreamInputResponse{View: view, Submission: submission}, 200},
		{"submit", http.MethodPost, inputPath, inputHTTPBody("STDIN", payload, false), cp.ManagedKubernetesStreamInputService_SubmitManagedKubernetesStreamInput_FullMethodName, &cp.SubmitManagedKubernetesStreamInputResponse{View: view, Submission: submission}, 201},
	} {
		t.Run(item.name, func(t *testing.T) {
			recorder := &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: item.response}}
			writer := httptest.NewRecorder()
			inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(item.method, item.path, item.body))
			if writer.Code != item.status || recorder.method != item.operation || writer.Header().Get("Cache-Control") != "no-store" || strings.Contains(writer.Body.String(), payload) || strings.Contains(writer.Body.String(), base64.StdEncoding.EncodeToString([]byte(payload))) {
				t.Fatal("actual input route lost typed command or leaked private bytes", writer.Code)
			}
			if !strings.Contains(writer.Body.String(), `"maximumStagedBytes":1073741824`) || !strings.Contains(writer.Body.String(), `"maximumStagedObjects":32768`) {
				t.Fatal("physical staging budget was hidden from the public input view")
			}
			switch request := recorder.request.(type) {
			case *cp.ListManagedKubernetesStreamInputsRequest:
				if request.RunRef != view.RunRef || request.NodeRef != view.NodeRef || request.Page.PageSize != 20 {
					t.Fatal("list lost exact node scope")
				}
			case *cp.GetManagedKubernetesStreamInputRequest:
				if request.InvocationRef != view.InvocationRef || request.StreamRef != view.StreamRef || strings.Contains(item.path, "Idempotency") && request.SubmissionIdempotencyKey != "public-input-key" {
					t.Fatal("get adopted another stream/key")
				}
			case *cp.SubmitManagedKubernetesStreamInputRequest:
				if request.Mutation.GetExpectedVersion() != 3 || request.Mutation.IdempotencyKey != "public-input-key" || request.Channel != "STDIN" || string(request.Payload) != payload || !bytes.Equal(recorder.borrowed, make([]byte, len(payload))) {
					t.Fatal("submit lost private ownership or owner OCC")
				}
			default:
				t.Fatal("unexpected generated request")
			}
			if item.method == http.MethodPost && writer.Header().Get("ETag") != "\"4\"" {
				t.Fatal("owner version was inferred from caller OCC")
			}
		})
	}
}

// TestKubernetesStreamInputHTTPRejectsAmbiguousPrivateBodies closes malformed JSON, source substitution, digest mismatch and noncanonical encoding before the owner RPC.
// TestKubernetesStreamInputHTTPRejectsAmbiguousPrivateBodies закрывает malformed JSON, source substitution, digest mismatch и noncanonical encoding до owner RPC.
func TestKubernetesStreamInputHTTPRejectsAmbiguousPrivateBodies(t *testing.T) {
	valid := inputHTTPBody("STDIN", "input", false)
	cases := []string{
		"null", "[]", valid + "{}", valid + strings.Repeat(" ", 65536),
		strings.Replace(valid, `"channel"`, `"Channel"`, 1), strings.Replace(valid, `"channel":"STDIN"`, `"channel":"STDIN","channel":"STDIN"`, 1),
		strings.Replace(valid, `"endOfInput":false`, `"endOfInput":null`, 1), strings.Replace(valid, `"endOfInput":false,`, "", 1),
		strings.TrimSuffix(valid, "}") + `,"actorRef":"usr_foreign"}`, strings.TrimSuffix(valid, "}") + `,"sequence":3}`,
		strings.Replace(valid, `"STDIN"`, `"FILE_CONTENT"`, 1), strings.Replace(valid, `"STDIN"`, `"CONTROL"`, 1),
		strings.Replace(valid, `"aW5wdXQ="`, `"aW5w\ndXQ="`, 1), strings.Replace(valid, `"aW5wdXQ="`, `"aW5wdXR="`, 1),
		strings.Replace(valid, `"payloadDigest":"`, `"payloadDigest":"f`, 1), strings.Replace(valid, `"channel":"`, `"channel":"`+string([]byte{0xff}), 1),
		inputHTTPBody("STDIN", strings.Repeat("x", 32769), false), inputHTTPBody("STDIN", "", false), inputHTTPBody("STDIN", "input", true),
		inputHTTPBody("TERMINAL_RESIZE", `{"Columns":80,"rows":10}`, false), inputHTTPBody("TERMINAL_RESIZE", `{"columns":80,"rows":10,"rows":20}`, false),
	}
	for _, body := range cases {
		recorder := &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{}}
		writer := httptest.NewRecorder()
		inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodPost, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/input", body))
		if writer.Code != 400 || recorder.request != nil {
			t.Fatal("invalid private body reached owner", writer.Code)
		}
	}
}

// TestKubernetesStreamInputHTTPRejectsForeignProjection refuses unknown/nil or cross-node metadata without exposing a partial page or claiming accepted input.
// TestKubernetesStreamInputHTTPRejectsForeignProjection отвергает unknown/nil либо cross-node metadata без выдачи partial page либо утверждения accepted input.
func TestKubernetesStreamInputHTTPRejectsForeignProjection(t *testing.T) {
	for _, variant := range []string{"foreign node", "duplicate stream", "unknown timestamp", "unknown top", "missing page", "missing staging bytes", "excessive staging bytes", "missing staging objects", "excessive staging objects"} {
		t.Run(variant, func(t *testing.T) {
			view := inputHTTPViewFixture()
			reply := &cp.ListManagedKubernetesStreamInputsResponse{Items: []*cp.ManagedKubernetesStreamInputView{view}, Page: &cp.PageInfo{}}
			switch variant {
			case "foreign node":
				view.NodeRef = "nod_foreign01"
			case "duplicate stream":
				reply.Items = append(reply.Items, proto.Clone(view).(*cp.ManagedKubernetesStreamInputView))
			case "unknown timestamp":
				view.HardDeadline.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "unknown top":
				reply.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "missing page":
				reply.Page = nil
			case "missing staging bytes":
				view.MaximumStagedBytes = 0
			case "excessive staging bytes":
				view.MaximumStagedBytes = 1<<30 + 1
			case "missing staging objects":
				view.MaximumStagedObjects = 0
			case "excessive staging objects":
				view.MaximumStagedObjects = 32769
			}
			recorder := &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: reply}}
			writer := httptest.NewRecorder()
			inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodGet, "/api/v1/runs/run_synthetic/nodes/nod_synthetic/kubernetes-stream-inputs", ""))
			if writer.Code != 502 || strings.Contains(writer.Body.String(), "kst_synthetic") {
				t.Fatal("foreign/unknown owner metadata returned as useful input view")
			}
		})
	}
}

// TestKubernetesStreamInputHTTPStopIsOneToolOwnerClosure uses the actual dedicated route without constructing worker fences or canceling a whole Run; terminal metadata proves no remote-process termination.
// TestKubernetesStreamInputHTTPStopIsOneToolOwnerClosure использует actual dedicated route без создания worker fences либо отмены всего Run; terminal metadata не доказывает remote-process termination.
func TestKubernetesStreamInputHTTPStopIsOneToolOwnerClosure(t *testing.T) {
	const path = "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/stop"
	view := inputHTTPViewFixture()
	view.State = cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_CANCELED
	recorder := &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: &cp.StopManagedKubernetesStreamResponse{View: view}}}
	writer := httptest.NewRecorder()
	inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodPost, path, "{}"))
	request, ok := recorder.request.(*cp.StopManagedKubernetesStreamRequest)
	if writer.Code != 200 || !ok || request.InvocationRef != view.InvocationRef || request.StreamRef != view.StreamRef || request.Mutation.GetExpectedVersion() != 3 || request.Mutation.IdempotencyKey != "public-input-key" || recorder.method != cp.ManagedKubernetesStreamInputService_StopManagedKubernetesStream_FullMethodName || writer.Header().Get("ETag") != "\"4\"" || !strings.Contains(writer.Body.String(), `"state":"CANCELED"`) {
		t.Fatal("tool-scoped stop lost owner metadata or became generic cancellation")
	}
	for _, body := range []string{"null", "[]", `{"fence":"caller-fence"}`, `{"actorRef":"usr_foreign"}`, `{"runRef":"run_foreign"}`, `{"sequence":3}`, strings.Repeat(" ", 1025)} {
		recorder = &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{}}
		writer = httptest.NewRecorder()
		inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodPost, path, body))
		if writer.Code != 400 || recorder.request != nil {
			t.Fatal("caller authority/whole-Run selector reached stop owner")
		}
	}
	for _, state := range []cp.ManagedKubernetesOwnerStreamState{cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_ACTIVE, cp.ManagedKubernetesOwnerStreamState_MANAGED_KUBERNETES_OWNER_STREAM_STATE_PREPARED} {
		view = inputHTTPViewFixture()
		view.State = state
		recorder = &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: &cp.StopManagedKubernetesStreamResponse{View: view}}}
		writer = httptest.NewRecorder()
		inputHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodPost, path, "{}"))
		if writer.Code != 502 {
			t.Fatal("nonterminal stop response became closure proof")
		}
	}
}

// TestKubernetesStreamInputHTTPWatchProjectsExactTargetAndCollection tests safe metadata through the actual public router, including Stop for input-free watches.
// TestKubernetesStreamInputHTTPWatchProjectsExactTargetAndCollection проверяет safe metadata через actual public router, включая Stop для input-free watch.
func TestKubernetesStreamInputHTTPWatchProjectsExactTargetAndCollection(t *testing.T) {
	for _, scope := range []string{"namespace", "cluster", "all"} {
		t.Run(scope, func(t *testing.T) {
			view := inputHTTPViewFixture()
			view.Operation = "kubernetes.watch"
			view.PodName = ""
			view.Container = ""
			view.InputChannels = nil
			view.Resource.Name = ""
			view.Resource.Uid = ""
			view.Resource.ResourceVersion = ""
			if scope == "cluster" {
				view.Namespace = "@cluster"
				view.Resource.Namespace = ""
				view.Resource.Namespaced = false
			}
			if scope == "all" {
				view.Operation = "kubernetes.watch.all-namespaces"
				view.Namespace = "@all-namespaces"
				view.Resource.Namespace = ""
				view.Resource.AllNamespaces = true
			}
			recorder := &inputHTTPRecorder{catalogRPCRecorder: &catalogRPCRecorder{response: &cp.GetManagedKubernetesStreamInputResponse{View: view}}}
			response := httptest.NewRecorder()
			inputHTTPHandler(recorder).ServeHTTP(response, inputHTTPRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/input", ""))
			if response.Code != 200 || !strings.Contains(response.Body.String(), `"targetRef":"target_review"`) || !strings.Contains(response.Body.String(), `"podName":""`) || !strings.Contains(response.Body.String(), `"inputChannels":[]`) {
				t.Fatal("watch collection hidden or falsely exposed input", response.Code)
			}
			for _, variant := range []string{"nil resource", "unknown resource", "namespace", "scope", "target", "connection", "revision"} {
				t.Run(variant, func(t *testing.T) {
					candidate := proto.Clone(view).(*cp.ManagedKubernetesStreamInputView)
					switch variant {
					case "nil resource":
						candidate.Resource = nil
					case "unknown resource":
						candidate.Resource.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1})
					case "namespace":
						candidate.Namespace = "other"
					case "scope":
						candidate.Resource.Name = "web"
					case "target":
						candidate.TargetRef = "bad target"
					case "connection":
						candidate.ConnectionRef = "foreign_ref"
					case "revision":
						candidate.TargetRevision = 0
					}
					if _, ok := kubernetesInputPublicView(candidate); ok {
						t.Fatal("mixed target/collection metadata accepted")
					}
				})
			}
		})
	}
}

// TestKubernetesStreamInputPublicSequenceUsesJSONRepresentation preserves useful owner cursors after
// the former count limit and refuses values that JSON/browser numbers cannot represent exactly.
// TestKubernetesStreamInputPublicSequenceUsesJSONRepresentation сохраняет useful owner cursors после
// прежнего count limit и отвергает значения без exact JSON/browser representation.
func TestKubernetesStreamInputPublicSequenceUsesJSONRepresentation(t *testing.T) {
	for _, sequence := range []uint64{4097, 1 << 20, uint64(maximumSafeJSONInteger)} {
		view := inputHTTPViewFixture()
		view.AcknowledgedSequence = sequence - 1
		view.Pending = &cp.ManagedKubernetesStreamInputSubmission{Ref: "kif_synthetic", InvocationRef: view.InvocationRef, StreamRef: view.StreamRef, Generation: view.Generation, Sequence: sequence, Channel: "STDIN", PayloadDigest: strings.Repeat("a", 64), ByteCount: 1, State: cp.ManagedKubernetesStreamInputSubmissionState_MANAGED_KUBERNETES_STREAM_INPUT_SUBMISSION_STATE_CLAIMED}
		result, valid := kubernetesInputPublicView(view)
		if !valid || result.Pending == nil || uint64(result.Pending.Sequence) != sequence {
			t.Fatal("representable owner sequence lost", sequence)
		}
		view.Pending.Sequence = uint64(maximumSafeJSONInteger) + 1
		if _, valid := kubernetesInputPublicView(view); valid {
			t.Fatal("inexact public integer projected")
		}
	}
	view := inputHTTPViewFixture()
	view.Operation = "kubernetes.logs"
	view.Container = ""
	view.InputChannels = []string{}
	if _, valid := kubernetesInputPublicView(view); !valid {
		t.Fatal("unknown HTTP logs container hidden")
	}
}
