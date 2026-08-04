package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type occControlPlane struct {
	ControlPlane
	calls int
}

func (control *occControlPlane) UpdateResource(context.Context, *controlplanev1.UpdateResourceRequest, ...grpc.CallOption) (*controlplanev1.UpdateResourceResponse, error) {
	control.calls++
	return &controlplanev1.UpdateResourceResponse{}, nil
}

func (control *occControlPlane) TransitionResource(context.Context, *controlplanev1.TransitionResourceRequest, ...grpc.CallOption) (*controlplanev1.TransitionResourceResponse, error) {
	control.calls++
	return &controlplanev1.TransitionResourceResponse{}, nil
}

func (control *occControlPlane) DeleteResource(context.Context, *controlplanev1.DeleteResourceRequest, ...grpc.CallOption) (*controlplanev1.DeleteResourceResponse, error) {
	control.calls++
	return &controlplanev1.DeleteResourceResponse{}, nil
}

func (control *occControlPlane) DetachAccessResource(context.Context, *controlplanev1.DetachAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.DetachAccessResourceResponse, error) {
	control.calls++
	return &controlplanev1.DetachAccessResourceResponse{}, nil
}

func (control *occControlPlane) CopyAccessResource(context.Context, *controlplanev1.CopyAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.CopyAccessResourceResponse, error) {
	control.calls++
	return &controlplanev1.CopyAccessResourceResponse{}, nil
}

func (control *occControlPlane) RevokeOwnerSession(context.Context, *controlplanev1.RevokeOwnerSessionRequest, ...grpc.CallOption) (*controlplanev1.RevokeOwnerSessionResponse, error) {
	control.calls++
	return &controlplanev1.RevokeOwnerSessionResponse{}, nil
}

func (control *occControlPlane) ManageAccessResource(context.Context, *controlplanev1.ManageAccessResourceRequest, ...grpc.CallOption) (*controlplanev1.ManageAccessResourceResponse, error) {
	control.calls++
	return &controlplanev1.ManageAccessResourceResponse{}, nil
}

func TestRequireETagWritesTypedInvalidRequest(t *testing.T) {
	for _, raw := range []string{"", "1", "W/\"1\"", "\"0\"", "*"} {
		writer := httptest.NewRecorder()
		if _, ok := requireETag(writer, raw); ok {
			t.Fatalf("invalid If-Match accepted: %q", raw)
		}
		if writer.Code != http.StatusBadRequest || !strings.Contains(writer.Body.String(), `"code":"INVALID_REQUEST"`) {
			t.Fatalf("invalid If-Match response for %q: status=%d body=%s", raw, writer.Code, writer.Body.String())
		}
	}
	writer := httptest.NewRecorder()
	if version, ok := requireETag(writer, "\"42\""); !ok || version != 42 || writer.Code != http.StatusOK || writer.Body.Len() != 0 {
		t.Fatalf("valid If-Match rejected: version=%d ok=%v status=%d", version, ok, writer.Code)
	}
}

func TestMalformedIfMatchNeverCallsMutationRPC(t *testing.T) {
	resourceID := uuid.New()
	malformed := "1"
	for _, test := range []struct {
		name string
		call func(*Server, http.ResponseWriter, *http.Request)
	}{
		{name: "update", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.UpdateResource(writer, request, resourceID, generated.UpdateResourceParams{IfMatch: "1"})
		}},
		{name: "transition", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.TransitionResource(writer, request, resourceID, generated.TransitionResourceParams{IfMatch: "1"})
		}},
		{name: "delete", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.DeleteResource(writer, request, resourceID, generated.DeleteResourceParams{IfMatch: "1"})
		}},
		{name: "detach", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.DetachAccessResource(writer, request, resourceID, generated.DetachAccessResourceParams{IfMatch: "1"})
		}},
		{name: "copy", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.CopyAccessResource(writer, request, resourceID, generated.CopyAccessResourceParams{IfMatch: "1"})
		}},
		{name: "owner session revoke", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.DeleteOwnerSession(writer, request, generated.DeleteOwnerSessionParams{IfMatch: "1"})
		}},
		{name: "administrative update", call: func(server *Server, writer http.ResponseWriter, request *http.Request) {
			server.ManageAccessResource(writer, request, generated.ManageAccessResourceParams{IfMatch: &malformed})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &occControlPlane{}
			server := &Server{control: control}
			writer := httptest.NewRecorder()
			body := `{"action":"UPDATE","kind":"TEAM","resourceId":"` + resourceID.String() + `","name":"team","spec":{"team":{"stableKey":"team","memberActorIds":[],"roleIds":[]}}}`
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			test.call(server, writer, request)
			if writer.Code != http.StatusBadRequest || control.calls != 0 {
				t.Fatalf("malformed If-Match: status=%d rpcCalls=%d", writer.Code, control.calls)
			}
		})
	}
}

func TestMissingIfMatchNeverCallsMutationRPC(t *testing.T) {
	t.Parallel()

	control := &occControlPlane{}
	server := &Server{control: control}
	resourceID := uuid.New()
	writer := httptest.NewRecorder()
	server.DetachAccessResource(writer, httptest.NewRequest(http.MethodPost, "/", nil), resourceID,
		generated.DetachAccessResourceParams{})
	if writer.Code != http.StatusBadRequest || control.calls != 0 {
		t.Fatalf("missing required If-Match: status=%d rpcCalls=%d", writer.Code, control.calls)
	}

	writer = httptest.NewRecorder()
	body := `{"action":"UPDATE","kind":"TEAM","resourceId":"` + resourceID.String() + `","name":"team","spec":{"team":{"stableKey":"team","memberActorIds":[],"roleIds":[]}}}`
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.ManageAccessResource(writer, request, generated.ManageAccessResourceParams{})
	if writer.Code != http.StatusBadRequest || control.calls != 0 {
		t.Fatalf("missing conditional If-Match: status=%d rpcCalls=%d", writer.Code, control.calls)
	}
}
