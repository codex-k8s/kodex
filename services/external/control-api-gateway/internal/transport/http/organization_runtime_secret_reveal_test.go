package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

type scopedRevealCommand struct {
	controlplanev1.PlatformCommandServiceClient
	calls int
}

func (client *scopedRevealCommand) PrepareRevealRuntimeSecret(context.Context, *controlplanev1.PrepareRevealRuntimeSecretRequest, ...grpc.CallOption) (*controlplanev1.PrepareRevealRuntimeSecretResponse, error) {
	client.calls++
	return &controlplanev1.PrepareRevealRuntimeSecretResponse{Operation: &controlplanev1.RuntimeSecretOperationReceipt{OperationGrant: "synthetic-scoped-grant"}}, nil
}

type scopedRevealBroker struct {
	secretbrokerv1.SecretBrokerServiceClient
	calls int
}

func (client *scopedRevealBroker) RevealSecret(context.Context, *secretbrokerv1.RevealSecretRequest, ...grpc.CallOption) (*secretbrokerv1.RevealSecretResponse, error) {
	client.calls++
	return &secretbrokerv1.RevealSecretResponse{Value: []byte("synthetic-scoped-value"), ValueType: secretbrokerv1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING}, nil
}

func TestOrganizationRevealUsesAuthoritativeScopeBeforeOneTimeElevation(t *testing.T) {
	for _, test := range []struct {
		name, organization, project, reference, header string
		scope                                          controlplanev1.RuntimeResourceScopeKind
		status                                         int
	}{
		{"organization", "org_example", "", "sec_example", "", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, http.StatusOK},
		{"other organization", "org_other", "", "sec_example", "", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, http.StatusForbidden},
		{"other secret", "org_example", "", "sec_other", "", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, http.StatusForbidden},
		{"missing scope", "org_example", "", "sec_example", "", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_UNSPECIFIED, http.StatusForbidden},
		{"project header on organization", "org_example", "", "sec_example", "prj_project_sales", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION, http.StatusForbidden},
		{"project tuple against organization elevation", "org_example", "prj_project_sales", "sec_example", "prj_project_sales", controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			csrf := strings.Repeat("c", 43)
			digest := sha256.Sum256([]byte(csrf))
			claims := session.Claims{Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(), SessionRevision: 3,
				SessionID: uuid.NewString(), Bearer: "bearer", CSRFHash: hex.EncodeToString(digest[:]), IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
				Elevation: &session.Elevation{Kind: session.ElevationKindRuntimeSecretReveal, ScopeKind: "ORGANIZATION", OrganizationRef: "org_example", SecretRef: "sec_example", ExpiresAt: now.Add(time.Minute).Unix()}}
			replacement := claims
			replacement.SessionID, replacement.Elevation = uuid.NewString(), nil
			revocations := &runtimeSecretRevocationStoreStub{}
			security, err := boundary.New(boundary.Config{Origins: []string{"https://control.example.test"},
				Verifier: runtimeSecretOIDCStub{principal: oidcauth.Principal{Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID, SessionRevision: 3, ExpiresAt: now.Add(time.Hour)}},
				Sessions: runtimeSecretSessionStoreStub{claims: claims, replacement: replacement}, Revocations: revocations,
				Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 2, GlobalHTTPConcurrency: 4, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 2}), Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			command, broker := &scopedRevealCommand{}, &scopedRevealBroker{}
			server := &Server{boundary: security, control: &controlplaneclient.Client{Command: command, Query: &runtimeSecretQueryStub{get: &controlplanev1.GetRuntimeSecretResponse{Secret: &controlplanev1.RuntimeSecret{
				Ref: test.reference, ScopeKind: test.scope, OrganizationRef: test.organization, ProjectRef: test.project}}}}, secrets: broker}
			perform := func() *httptest.ResponseRecorder {
				request := httptest.NewRequest(http.MethodPost, "https://control.example.test/api/v1/runtime-secrets/sec_example/reveal", nil)
				request.Header.Set("Origin", "https://control.example.test")
				request.Header.Set("X-CSRF-Token", csrf)
				if test.header != "" {
					request.Header.Set(boundary.ProjectReferenceHeader, test.header)
				}
				request.AddCookie(&http.Cookie{Name: boundary.SessionCookieName, Value: "encoded-session"})
				request.AddCookie(&http.Cookie{Name: boundary.CSRFCookieName, Value: csrf})
				response := httptest.NewRecorder()
				security.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					server.RevealRuntimeSecret(w, r, "sec_example", generated.RevealRuntimeSecretParams{IdempotencyKey: "synthetic-scoped-reveal"})
				})).ServeHTTP(response, request)
				return response
			}
			response := perform()
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("scoped reveal outcome = %d", response.Code)
			}
			if test.status != http.StatusOK {
				if command.calls != 0 || broker.calls != 0 || revocations.consumed {
					t.Fatal("scope mismatch consumed elevation or reached the secret effect")
				}
				return
			}
			if command.calls != 1 || broker.calls != 1 || !revocations.consumed || len(response.Header().Values("Set-Cookie")) != 2 {
				t.Fatal("organization reveal did not use exact one-time purpose")
			}
			if response = perform(); response.Code != http.StatusForbidden || command.calls != 1 || broker.calls != 1 {
				t.Fatal("organization elevation replay reached the secret effect")
			}
		})
	}
}
