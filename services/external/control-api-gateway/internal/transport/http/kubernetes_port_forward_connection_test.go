package httptransport

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

// portForwardHTTPFixture builds safe original-parent metadata only; native TCP and owner admission remain separate tests.
// portForwardHTTPFixture создаёт лишь safe original-parent metadata; native TCP и owner admission проверяются отдельно.
func portForwardHTTPFixture() (*cp.ManagedKubernetesPortForwardConnectionView, *cp.ManagedKubernetesStreamInputView, *cp.ManagedKubernetesStreamInputSubmission) {
	parent := inputHTTPViewFixture()
	parent.Operation = "kubernetes.port-forward"
	parent.InputChannels = []string{"PORT_FORWARD"}
	connection := &cp.ManagedKubernetesPortForwardConnectionView{ConnectionRef: "kpf_" + base64.RawURLEncoding.EncodeToString(make([]byte, 18)), InvocationRef: parent.InvocationRef, StreamRef: parent.StreamRef, StreamGeneration: parent.Generation, Generation: 1, Slot: 0, State: cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_PREPARED, InputVersion: parent.Version, HardDeadline: parent.HardDeadline}
	submission := &cp.ManagedKubernetesStreamInputSubmission{Ref: "kif_synthetic", InvocationRef: parent.InvocationRef, StreamRef: parent.StreamRef, Generation: parent.Generation, Sequence: 1, Channel: "PORT_FORWARD", ByteCount: 51, PayloadDigest: strings.Repeat("a", 64), State: cp.ManagedKubernetesStreamInputSubmissionState_MANAGED_KUBERNETES_STREAM_INPUT_SUBMISSION_STATE_QUEUED}
	return connection, parent, submission
}

// portForwardHTTPHandler uses the actual generated router/client method map without pretending that synthetic replies qualify owner storage or authorization.
// portForwardHTTPHandler использует actual generated router/client method map; synthetic replies не квалифицируют owner storage либо authorization.
func portForwardHTTPHandler(recorder *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{KubernetesPortForward: cp.NewManagedKubernetesPortForwardConnectionServiceClient(recorder)}})
}

// TestPortForwardHTTPRoutesKeepExplicitSelectorsAndSafeMetadata crosses actual route bindings for all four commands without any caller generation, target or raw OPEN envelope.
// TestPortForwardHTTPRoutesKeepExplicitSelectorsAndSafeMetadata проходит actual route bindings четырёх команд без caller generation, target либо raw OPEN envelope.
func TestPortForwardHTTPRoutesKeepExplicitSelectorsAndSafeMetadata(t *testing.T) {
	const base = "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic"
	connection, parent, submission := portForwardHTTPFixture()
	for _, name := range []string{"open", "get", "lost-open", "list", "close-owner-only", "close-staged"} {
		t.Run(name, func(t *testing.T) {
			own := proto.Clone(connection).(*cp.ManagedKubernetesPortForwardConnectionView)
			path, body, method, expected := base+"/port-forward-connections", `{"slot":0}`, http.MethodPost, 201
			var response proto.Message = &cp.OpenManagedKubernetesPortForwardConnectionResponse{Connection: own, Input: parent, Submission: submission}
			fullMethod := cp.ManagedKubernetesPortForwardConnectionService_OpenManagedKubernetesPortForwardConnection_FullMethodName
			switch name {
			case "get", "lost-open":
				method, expected, body = http.MethodGet, 200, ""
				path = base + "/port-forward-connection?connectionRef=" + own.ConnectionRef
				if name == "lost-open" {
					path = base + "/port-forward-connection?openIdempotencyKey=public-input-key"
				}
				response = &cp.GetManagedKubernetesPortForwardConnectionResponse{Connection: own, Submission: submission}
				fullMethod = cp.ManagedKubernetesPortForwardConnectionService_GetManagedKubernetesPortForwardConnection_FullMethodName
			case "list":
				method, expected, body = http.MethodGet, 200, ""
				path = base + "/port-forward-connections?pageSize=16"
				response = &cp.ListManagedKubernetesPortForwardConnectionsResponse{Items: []*cp.ManagedKubernetesPortForwardConnectionView{own}, Page: &cp.PageInfo{}, Profile: &cp.ManagedKubernetesPortForwardConnectionProfile{Ports: []*cp.ManagedKubernetesPortForwardMapping{{Port: 18080, TargetPort: 8080}}, TimeoutSeconds: 30, Protocol: "PORT_FORWARD_SPDY_V1", MaximumActiveConnections: 16, ActiveConnections: 1}}
				fullMethod = cp.ManagedKubernetesPortForwardConnectionService_ListManagedKubernetesPortForwardConnections_FullMethodName
			case "close-owner-only", "close-staged":
				path = base + "/port-forward-connections/" + own.ConnectionRef + "/close"
				body, expected = `{}`, 200
				own.State = cp.ManagedKubernetesPortForwardConnectionState_MANAGED_KUBERNETES_PORT_FORWARD_CONNECTION_STATE_CLOSED
				var stage *cp.ManagedKubernetesStreamInputSubmission
				if name == "close-staged" {
					stage = submission
				}
				response = &cp.CloseManagedKubernetesPortForwardConnectionResponse{Connection: own, Input: parent, Submission: stage}
				fullMethod = cp.ManagedKubernetesPortForwardConnectionService_CloseManagedKubernetesPortForwardConnection_FullMethodName
			}
			recorder := &catalogRPCRecorder{response: response}
			writer := httptest.NewRecorder()
			portForwardHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(method, path, body))
			if writer.Code != expected || recorder.method != fullMethod || writer.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("PF route lost exact typed command or response", name, writer.Code, writer.Body.String())
			}
			switch request := recorder.request.(type) {
			case *cp.OpenManagedKubernetesPortForwardConnectionRequest:
				if request.Slot == nil || *request.Slot != 0 || request.Mutation.ExpectedVersion == nil || *request.Mutation.ExpectedVersion != 3 {
					t.Fatal("explicit slot/OCC lost")
				}
			case *cp.GetManagedKubernetesPortForwardConnectionRequest:
				if name == "lost-open" && request.GetOpenIdempotencyKey() != "public-input-key" || name == "get" && request.GetConnectionRef() != own.ConnectionRef {
					t.Fatal("exact read selector lost")
				}
			case *cp.CloseManagedKubernetesPortForwardConnectionRequest:
				if request.ConnectionRef != own.ConnectionRef || request.Mutation.IdempotencyKey != "public-input-key" {
					t.Fatal("Close changed original incarnation or key")
				}
			}
			if name == "close-owner-only" && (strings.Contains(writer.Body.String(), "submission") || !strings.Contains(writer.Body.String(), `"nativeJoined":false`)) {
				t.Fatal("owner closure invented native admission or join")
			}
		})
	}
}

// TestPortForwardHTTPRejectsHostileBodiesBeforeRPC closes duplicate/case/null/oversize and caller authority fields before any typed owner request.
// TestPortForwardHTTPRejectsHostileBodiesBeforeRPC закрывает duplicate/case/null/oversize и caller authority fields до typed owner request.
func TestPortForwardHTTPRejectsHostileBodiesBeforeRPC(t *testing.T) {
	const path = "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/port-forward-connections"
	for _, body := range []string{`{}`, `{"slot":null}`, `{"Slot":0}`, `{"slot":0,"slot":1}`, `{"slot":16}`, `{"slot":-1}`, `{"slot":4294967296}`, `{"slot":0.5}`, `{"slot":0,"generation":1}`, `{"slot":0,"targetPort":22}`, `{"slot":0,"actor":"foreign"}`, "{\"slot\":0,\"x\":\"" + string([]byte{0xff}) + "\"}", `{"slot":0,"x":"` + strings.Repeat("x", 1024) + `"}`} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		portForwardHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodPost, path, body))
		if writer.Code != 400 || recorder.method != "" {
			t.Fatal("hostile Open body reached RPC", writer.Code)
		}
	}
	for _, query := range []string{"", "?connectionRef=kpf_bad", "?openIdempotencyKey=short", "?connectionRef=kpf_AAAAAAAAAAAAAAAAAAAAAAAA&openIdempotencyKey=public-input-key"} {
		recorder := &catalogRPCRecorder{}
		writer := httptest.NewRecorder()
		portForwardHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodGet, strings.TrimSuffix(path, "s")+query, ""))
		if writer.Code != 400 || recorder.method != "" {
			t.Fatal("ambiguous Get selector reached RPC", writer.Code)
		}
	}
}

// TestPortForwardHTTPRejectsMixedOrPromotedOwnerReplies refuses foreign origins, unknown states, fake join, oversized integers and invalid original-key metadata.
// TestPortForwardHTTPRejectsMixedOrPromotedOwnerReplies отклоняет foreign origins, unknown states, fake join, oversized integers и invalid original-key metadata.
func TestPortForwardHTTPRejectsMixedOrPromotedOwnerReplies(t *testing.T) {
	for _, mode := range []string{"origin", "state", "join", "precision", "slot", "submission", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			connection, _, submission := portForwardHTTPFixture()
			switch mode {
			case "origin":
				connection.InvocationRef = "inv_foreign"
			case "state":
				connection.State = 99
			case "join":
				connection.NativeJoined = true
			case "precision":
				connection.Generation = 9007199254740992
			case "slot":
				connection.Slot = 16
			case "submission":
				submission.Channel = "STDIN"
			case "deadline":
				connection.HardDeadline = nil
			}
			recorder := &catalogRPCRecorder{response: &cp.GetManagedKubernetesPortForwardConnectionResponse{Connection: connection, Submission: submission}}
			writer := httptest.NewRecorder()
			portForwardHTTPHandler(recorder).ServeHTTP(writer, inputHTTPRequest(http.MethodGet, "/api/v1/integration-invocations/inv_synthetic/kubernetes-streams/kst_synthetic/port-forward-connection?openIdempotencyKey=public-input-key", ""))
			if writer.Code != 502 {
				t.Fatal("malformed owner metadata escaped PF projection", mode, writer.Code)
			}
		})
	}
}
