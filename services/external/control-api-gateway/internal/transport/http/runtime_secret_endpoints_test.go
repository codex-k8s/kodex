package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type runtimeSecretCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	create func(*controlplanev1.PrepareCreateRuntimeSecretRequest) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error)
}

func (stub runtimeSecretCommandStub) PrepareCreateRuntimeSecret(
	_ context.Context,
	request *controlplanev1.PrepareCreateRuntimeSecretRequest,
	_ ...grpc.CallOption,
) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error) {
	if stub.create == nil {
		return nil, errors.New("unexpected create runtime secret")
	}
	return stub.create(request)
}

func (runtimeSecretCommandStub) PrepareRevealRuntimeSecret(
	context.Context,
	*controlplanev1.PrepareRevealRuntimeSecretRequest,
	...grpc.CallOption,
) (*controlplanev1.PrepareRevealRuntimeSecretResponse, error) {
	return &controlplanev1.PrepareRevealRuntimeSecretResponse{Operation: &controlplanev1.RuntimeSecretOperationReceipt{OperationGrant: "synthetic-one-time-operation-grant"}}, nil
}

type runtimeSecretBrokerStub struct {
	secretbrokerv1.SecretBrokerServiceClient
}

type runtimeSecretOIDCStub struct{ principal oidcauth.Principal }

func (stub runtimeSecretOIDCStub) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return oidcauth.Principal{}, "", errors.New("unexpected authorization verification")
}

func (stub runtimeSecretOIDCStub) VerifyToken(context.Context, string) (oidcauth.Principal, error) {
	return stub.principal, nil
}

type runtimeSecretSessionStoreStub struct {
	claims      session.Claims
	replacement session.Claims
}

func (stub runtimeSecretSessionStoreStub) Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error) {
	return stub.replacement, "replacement-session", strings.Repeat("d", 43), nil
}

func (runtimeSecretSessionStoreStub) IssueWithElevation(string, string, string, uint64, string, time.Time, *session.Elevation) (session.Claims, string, string, error) {
	return session.Claims{}, "", "", errors.New("unexpected elevated session issue")
}

func (stub runtimeSecretSessionStoreStub) Open(string) (session.Claims, error) {
	return stub.claims, nil
}

func (runtimeSecretSessionStoreStub) Renew(claims session.Claims, _ time.Time) (session.Claims, string, bool, error) {
	return claims, "", false, nil
}

type runtimeSecretRevocationStoreStub struct{ consumed bool }

func (*runtimeSecretRevocationStoreStub) Revoke(context.Context, string) error { return nil }
func (*runtimeSecretRevocationStoreStub) Revoked(context.Context, string) (bool, error) {
	return false, nil
}
func (stub *runtimeSecretRevocationStoreStub) ConsumeOnce(context.Context, string) (bool, error) {
	if stub.consumed {
		return false, nil
	}
	stub.consumed = true
	return true, nil
}

func (runtimeSecretBrokerStub) RevealSecret(
	context.Context,
	*secretbrokerv1.RevealSecretRequest,
	...grpc.CallOption,
) (*secretbrokerv1.RevealSecretResponse, error) {
	return &secretbrokerv1.RevealSecretResponse{
		Value:     []byte("synthetic-value"),
		ValueType: secretbrokerv1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
	}, nil
}

func TestRevealRuntimeSecretDisablesCaching(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	projectRef := "prj_project_sales"
	csrf := strings.Repeat("c", 43)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{
		Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(), SessionRevision: 3,
		SessionID: uuid.NewString(), Bearer: "bearer", CSRFHash: hex.EncodeToString(csrfDigest[:]), IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		Elevation: &session.Elevation{Kind: session.ElevationKindRuntimeSecretReveal, ProjectRef: projectRef, SecretRef: "secret_main", ExpiresAt: now.Add(time.Minute).Unix()},
	}
	replacement := claims
	replacement.SessionID = uuid.NewString()
	replacement.Elevation = nil
	principal := oidcauth.Principal{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour),
	}
	security, err := boundary.New(boundary.Config{
		Origins: []string{"https://control.example.test"}, Verifier: runtimeSecretOIDCStub{principal: principal},
		Sessions: runtimeSecretSessionStoreStub{claims: claims, replacement: replacement}, Revocations: &runtimeSecretRevocationStoreStub{},
		Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 2, GlobalHTTPConcurrency: 4, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 2}),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new boundary: %v", err)
	}
	server := &Server{
		control:  &controlplaneclient.Client{Command: runtimeSecretCommandStub{}},
		secrets:  runtimeSecretBrokerStub{},
		boundary: security,
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "https://control.example.test/api/v1/runtime-secrets/secret_main/reveal", nil)
	request.Header.Set("Origin", "https://control.example.test")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set(boundary.ProjectReferenceHeader, projectRef)
	request.AddCookie(&http.Cookie{Name: boundary.SessionCookieName, Value: "encoded-session"})
	request.AddCookie(&http.Cookie{Name: boundary.CSRFCookieName, Value: csrf})
	security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.RevealRuntimeSecret(writer, request, "secret_main", generated.RevealRuntimeSecretParams{IdempotencyKey: "idem-key-123"})
	})).ServeHTTP(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
	if got := response.Header().Get("Expires"); got != "0" {
		t.Fatalf("Expires = %q, want 0", got)
	}
	if len(response.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("replacement session cookies = %v", response.Header().Values("Set-Cookie"))
	}
}

func TestCreateRuntimeSecretBindsDigestAndReturnsTerminalReceipt(t *testing.T) {
	value := "synthetic-secret-value"
	expected := sha256.Sum256([]byte(value))
	now := timestamppb.Now()
	var captured *controlplanev1.PrepareCreateRuntimeSecretRequest
	command := runtimeSecretCommandStub{create: func(request *controlplanev1.PrepareCreateRuntimeSecretRequest) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error) {
		captured = request
		return &controlplanev1.PrepareCreateRuntimeSecretResponse{Operation: &controlplanev1.RuntimeSecretOperationReceipt{
			State: controlplanev1.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED,
			TerminalSecret: &controlplanev1.RuntimeSecret{
				Ref: "sec_terminal123", ProjectRef: "prj_project_sales", Name: "CRM_TOKEN", Description: "CRM",
				ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: "ACTIVE",
				Version: 1, CurrentRevision: 1, CreatedAt: now, UpdatedAt: now,
			},
		}}, nil
	}}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/prj_project_sales/runtime-secrets", strings.NewReader(`{"name":"CRM_TOKEN","description":"CRM","valueType":"STRING","value":"`+value+`"}`))
	request.Header.Set("Content-Type", "application/json")

	server.CreateRuntimeSecret(response, request, "prj_project_sales", generated.CreateRuntimeSecretParams{IdempotencyKey: "runtime-secret-create-terminal"})

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if captured == nil || captured.GetExpectedContentSha256() != hex.EncodeToString(expected[:]) || captured.GetValueType() != controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING {
		t.Fatalf("prepare request does not bind exact content: %#v", captured)
	}
	if strings.Contains(response.Body.String(), value) {
		t.Fatal("secret plaintext leaked into terminal response")
	}
}
