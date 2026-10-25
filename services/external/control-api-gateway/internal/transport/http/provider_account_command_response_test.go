package httptransport

import (
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

// assertProviderAccountCommandProjection compares all public status, headers and JSON with the
// preexisting projector or closed local failure, using an independently specified binding
// outcome. It also checks that projection did not mutate the synthetic protobuf reply.
// assertProviderAccountCommandProjection сравнивает все публичные статус, заголовки и JSON с
// прежней проекцией либо закрытым локальным отказом по независимо заданному исходу привязки.
// Также проверяет, что проекция не изменила синтетический protobuf-ответ.
func assertProviderAccountCommandProjection[R providerAccountCommandResponse](t *testing.T, response R, statusCode int, bound bool) {
	t.Helper()
	before := proto.Clone(response)
	actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
	writeProviderAccountCommandResponse(actual, statusCode, response, "pacc_primary01")
	if bound {
		writeMessage(expected, statusCode, response, "account", "")
	} else {
		writeLocalProblem(expected, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
	}
	assertReadRPCProjection(t, actual, expected)
	if !proto.Equal(before, response) {
		t.Fatal("provider response projection mutated its input")
	}
}

// TestProviderAccountCommandProjectionPreservesBinding checks all three closed reply types with
// typed nil, missing, foreign and exact accounts. A zero version is passed through the unchanged
// public projector rather than gaining a new lifecycle condition in this extraction.
// TestProviderAccountCommandProjectionPreservesBinding проверяет три закрытых типа ответа с
// типизированным nil, отсутствующим, чужим и точным аккаунтом. Нулевая версия передаётся прежней
// публичной проекции без добавления нового условия жизненного цикла при выносе кода.
func TestProviderAccountCommandProjectionPreservesBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                                           string
		nilReply, missing, foreign, zeroVersion, bound bool
	}{
		{name: "exact", bound: true},
		{name: "typed nil", nilReply: true},
		{name: "missing", missing: true},
		{name: "foreign", foreign: true},
		{name: "zero version projection", zeroVersion: true, bound: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := providerTestAccount()
			if test.missing {
				account = nil
			}
			if test.foreign {
				account.Ref = "pacc_foreign01"
				account.Name = "private foreign fixture"
			}
			if test.zeroVersion {
				account.Version = 0
			}
			var verify *cp.VerifyProviderAccountDeviceAuthorizationResponse
			var reauthorize *cp.ReauthorizeProviderAccountDeviceCodeResponse
			var deleted *cp.DeleteProviderAccountResponse
			if !test.nilReply {
				verify = &cp.VerifyProviderAccountDeviceAuthorizationResponse{Account: account}
				reauthorize = &cp.ReauthorizeProviderAccountDeviceCodeResponse{Account: account}
				deleted = &cp.DeleteProviderAccountResponse{Account: account}
			}
			assertProviderAccountCommandProjection(t, verify, http.StatusOK, test.bound)
			assertProviderAccountCommandProjection(t, reauthorize, http.StatusAccepted, test.bound)
			assertProviderAccountCommandProjection(t, deleted, http.StatusOK, test.bound)
		})
	}
}

// TestProviderAccountCommandsKeepRPCErrorProfiles exercises the generated routes for every
// non-OK canonical gRPC code, preserving each dedicated method and the reauthorization-specific
// retry profile. Missing mutation/CSRF headers must stop before RPC; no real provider is called.
// TestProviderAccountCommandsKeepRPCErrorProfiles проверяет generated-маршруты для каждого
// канонического non-OK gRPC-кода, сохраняя отдельные методы и особый профиль повторной авторизации.
// Отсутствие mutation/CSRF заголовков останавливает запрос до RPC; настоящий провайдер не вызывается.
func TestProviderAccountCommandsKeepRPCErrorProfiles(t *testing.T) {
	t.Parallel()
	for _, route := range []struct {
		name, verb, suffix, method string
		device                     bool
	}{
		{"verify", http.MethodPost, "/device-authorization/verification", cp.PlatformCommandService_VerifyProviderAccountDeviceAuthorization_FullMethodName, false},
		{"reauthorize", http.MethodPost, "/device-reauthorizations", cp.PlatformCommandService_ReauthorizeProviderAccountDeviceCode_FullMethodName, true},
		{"delete", http.MethodDelete, "", cp.PlatformCommandService_DeleteProviderAccount_FullMethodName, false},
	} {
		request := func() *http.Request {
			r := httptest.NewRequest(route.verb, "/api/v1/provider-accounts/pacc_primary01"+route.suffix, nil)
			r.Header.Set("Idempotency-Key", "provider-response-fixture")
			r.Header.Set("If-Match", `"4"`)
			r.Header.Set("X-CSRF-Token", "fixture-csrf")
			return r
		}
		for code := codes.Canceled; code <= codes.Unauthenticated; code++ {
			t.Run(route.name+"/"+code.String(), func(t *testing.T) {
				err := status.Error(code, "private-provider-response")
				client := &catalogRPCRecorder{failure: err}
				handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
				actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
				if route.device {
					writeProviderDeviceProblem(expected, err)
				} else {
					writeRPCProblem(expected, err)
				}
				handler.ServeHTTP(actual, request())
				if client.method != route.method || strings.Contains(actual.Body.String(), "private-provider-response") {
					t.Fatal("provider command changed its RPC or exposed private diagnostics")
				}
				assertReadRPCProjection(t, actual, expected)
			})
		}
		for _, header := range []string{"Idempotency-Key", "If-Match", "X-CSRF-Token"} {
			t.Run(route.name+"/missing "+header, func(t *testing.T) {
				client := &catalogRPCRecorder{}
				handler := generated.Handler(&Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}})
				r := request()
				r.Header.Del(header)
				actual := httptest.NewRecorder()
				handler.ServeHTTP(actual, r)
				if actual.Code != http.StatusBadRequest || client.method != "" {
					t.Fatal("missing provider mutation header reached RPC")
				}
			})
		}
	}
}
