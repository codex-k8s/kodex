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
	"google.golang.org/protobuf/reflect/protoreflect"
)

// panicRPCResultMessage detects any attempt to inspect an error-bearing reply. It is only used on failure paths and is deliberately not a serializable success fixture.
// panicRPCResultMessage обнаруживает попытку разобрать ответ, сопровождаемый ошибкой. Используется только на отказах и намеренно не является сериализуемым успешным fixture.
type panicRPCResultMessage struct{}

// ProtoReflect panics instead of exposing a descriptor, making error-before-projection ordering observable even when an upstream client returns a nonnil message with an error.
// ProtoReflect вызывает panic вместо выдачи дескриптора, делая порядок error-before-projection наблюдаемым даже при ненулевом сообщении вместе с ошибкой внешнего клиента.
func (*panicRPCResultMessage) ProtoReflect() protoreflect.Message {
	panic("unexpected protobuf inspection")
}

// TestWriteRPCResultPreservesProjectionAndStatus compares the shared result boundary with the original branch for HTTP200/201/202, whole/nested/page data, invalid upstream shapes and every non-OK gRPC code. Error-bearing nil, typed-nil, partial and panic-on-inspection replies must not be projected; success preserves headers, localization, ETag, JSON and the input protobuf.
// TestWriteRPCResultPreservesProjectionAndStatus сравнивает общую границу результата с исходной ветвью для HTTP200/201/202, полного/вложенного/страничного ответа, неверных форм и всех gRPC-кодов кроме OK. Nil, typed-nil, частичный и падающий при разборе ответ с ошибкой не проецируются; успех сохраняет headers, локализацию, ETag, JSON и исходный protobuf.
func TestWriteRPCResultPreservesProjectionAndStatus(t *testing.T) {
	t.Parallel()
	for _, successStatus := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted} {
		for _, current := range []struct {
			name              string
			message           proto.Message
			err               error
			field, pageField  string
			wantSuccessStatus bool
		}{
			{name: "whole", message: &controlplanev1.PermissionDefinition{Key: "agent.launch", Risk: controlplanev1.PermissionRisk_PERMISSION_RISK_WRITE}, wantSuccessStatus: true},
			{name: "nested", message: &controlplanev1.GetAgentResponse{Agent: &controlplanev1.Agent{Ref: "agt_fixture01", Name: "TYPE_literal", Version: 7}}, field: "agent", wantSuccessStatus: true},
			{name: "terminal page", message: &controlplanev1.ListSchedulesResponse{}, pageField: "schedules", wantSuccessStatus: true},
			{name: "invalid public shape", message: &controlplanev1.AgentAvatar{}},
			{name: "unsafe integer", message: &controlplanev1.AssistantPlan{Version: 1 << 53}},
			{name: "nil reply on error", err: status.Error(codes.Unavailable, "private upstream detail")},
			{name: "typed nil reply on error", message: (*controlplanev1.CreateProjectResponse)(nil), err: status.Error(codes.PermissionDenied, "private upstream detail")},
			{name: "partial reply on error", message: &controlplanev1.CreateProjectResponse{Project: &controlplanev1.Project{Name: "private partial reply"}}, err: status.Error(codes.NotFound, "private upstream detail"), field: "project"},
			{name: "uninspectable reply on error", message: &panicRPCResultMessage{}, err: status.Error(codes.ResourceExhausted, "private upstream detail")},
			{name: "ordinary error", err: errors.New("private upstream detail")},
		} {
			t.Run(strconv.Itoa(successStatus)+"/"+current.name, func(t *testing.T) {
				actual := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
				expected := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
				var before proto.Message
				if current.err != nil {
					writeRPCProblem(expected, current.err)
				} else {
					before = proto.Clone(current.message)
					writeMessage(expected, successStatus, current.message, current.field, current.pageField)
				}
				writeRPCResult(actual, successStatus, current.message, current.err, current.field, current.pageField)
				assertReadRPCProjection(t, actual.ResponseRecorder, expected.ResponseRecorder)
				if current.wantSuccessStatus && actual.Code != successStatus {
					t.Fatal("explicit success status changed")
				}
				if current.err == nil && !proto.Equal(before, current.message) {
					t.Fatal("input protobuf changed")
				}
			})
		}
		for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
			t.Run(strconv.Itoa(successStatus)+"/grpc/"+code.String(), func(t *testing.T) {
				err := status.Error(code, "private upstream detail")
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				writeRPCProblem(expected, err)
				writeRPCResult(actual, successStatus, &panicRPCResultMessage{}, err, "ignored", "ignored")
				assertReadRPCProjection(t, actual, expected)
			})
		}
	}
}
