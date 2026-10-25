package httptransport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// countedRuntimeReply makes getter access observable independently of RPC errors and returns only a synthetic runtime view. Error-only fixtures panic on inspection instead of allowing premature partial-response projection.
// countedRuntimeReply делает обращение к getter наблюдаемым независимо от RPC-ошибки и возвращает только синтетический runtime-вид. Fixtures отказа падают при чтении, обнаруживая преждевременную проекцию частичного ответа.
type countedRuntimeReply struct {
	// view is the synthetic owner snapshot returned after a successful getter call; the result boundary must not mutate it.
	// view — синтетический snapshot владельца для успешного чтения; граница результата не должна его изменять.
	view *controlplanev1.AgentRuntimeConfigurationView
	// reads counts actual getter invocations, including an unexpected invocation on an RPC failure.
	// reads считает реальные вызовы getter, включая недопустимый вызов при RPC-ошибке.
	reads *int
	// panicOnRead rejects inspection of a reply paired with an error, making the ordering invariant fail visibly.
	// panicOnRead отклоняет чтение ответа вместе с ошибкой, делая нарушение порядка наблюдаемым.
	panicOnRead bool
}

// GetRuntimeConfiguration records one access, optionally rejects it and returns the original synthetic view without cloning or modifying its fields.
// GetRuntimeConfiguration отмечает одно обращение, при необходимости отклоняет его и возвращает исходный синтетический вид без копирования и изменения полей.
func (reply countedRuntimeReply) GetRuntimeConfiguration() *controlplanev1.AgentRuntimeConfigurationView {
	*reply.reads++
	if reply.panicOnRead {
		panic("unexpected runtime reply inspection")
	}
	return reply.view
}

// TestWriteAgentRuntimeRPCResultPreservesBoundary compares the original error/getter/projector sequence for HTTP200/201, valid/nil/foreign/invalid-version views, typed-nil replies and every non-OK gRPC code. Errors must skip the getter; successful inspection is exactly once and preserves existing binding validation, headers, ETag, JSON and the input snapshot.
// TestWriteAgentRuntimeRPCResultPreservesBoundary сравнивает исходную последовательность error/getter/projector для HTTP200/201, допустимого/nil/чужого/неверсионированного вида, typed-nil ответа и всех gRPC-кодов кроме OK. Ошибка не вызывает getter; успешное чтение однократно и сохраняет проверку bindings, headers, ETag, JSON и исходный snapshot.
func TestWriteAgentRuntimeRPCResultPreservesBoundary(t *testing.T) {
	t.Parallel()
	for _, successStatus := range []int{http.StatusOK, http.StatusCreated} {
		invalid := runtimeContextFixture()
		invalid.AgentVersion = 0
		for _, current := range []struct {
			name, agentRef string
			view           *controlplanev1.AgentRuntimeConfigurationView
			valid          bool
		}{
			{"valid", "agt_fixture01", runtimeContextFixture(), true},
			{"nil view", "agt_fixture01", nil, false},
			{"foreign agent", "agt_other01", runtimeContextFixture(), false},
			{"invalid version", "agt_fixture01", invalid, false},
		} {
			t.Run(strconv.Itoa(successStatus)+"/"+current.name, func(t *testing.T) {
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				before := proto.Clone(current.view)
				reads := 0
				writeAgentRuntimeConfiguration(expected, successStatus, current.view, current.agentRef)
				writeAgentRuntimeRPCResult(actual, successStatus, countedRuntimeReply{view: current.view, reads: &reads}, nil, current.agentRef)
				assertReadRPCProjection(t, actual, expected)
				if reads != 1 || current.valid && (actual.Code != successStatus || actual.Header().Get("ETag") != `"9"`) {
					t.Fatal("runtime getter count, status or ETag changed")
				}
				if current.view != nil && !proto.Equal(before, current.view) {
					t.Fatal("input runtime snapshot changed")
				}
			})
		}
		for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
			t.Run(strconv.Itoa(successStatus)+"/grpc/"+code.String(), func(t *testing.T) {
				err := status.Error(code, "private runtime boundary detail")
				reads := 0
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				writeRPCProblem(expected, err)
				writeAgentRuntimeRPCResult(actual, successStatus, countedRuntimeReply{reads: &reads, panicOnRead: true}, err, "agt_fixture01")
				assertReadRPCProjection(t, actual, expected)
				if reads != 0 {
					t.Fatal("errored runtime reply was inspected")
				}
			})
		}
		t.Run(strconv.Itoa(successStatus)+"/ordinary error", func(t *testing.T) {
			err := errors.New("private runtime boundary detail")
			reads := 0
			actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
			writeRPCProblem(expected, err)
			writeAgentRuntimeRPCResult(actual, successStatus, countedRuntimeReply{reads: &reads, panicOnRead: true}, err, "agt_fixture01")
			assertReadRPCProjection(t, actual, expected)
			if reads != 0 {
				t.Fatal("errored runtime reply was inspected")
			}
		})
		t.Run(strconv.Itoa(successStatus)+"/typed nil success reply", func(t *testing.T) {
			var reply *controlplanev1.GetAgentRuntimeConfigurationResponse
			actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
			writeAgentRuntimeConfiguration(expected, successStatus, reply.GetRuntimeConfiguration(), "agt_fixture01")
			writeAgentRuntimeRPCResult(actual, successStatus, reply, nil, "agt_fixture01")
			assertReadRPCProjection(t, actual, expected)
		})
	}
}
