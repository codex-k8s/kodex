package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// runtimeSecretCommandStub provides only the create/reveal preparation responses needed by HTTP
// tests, delegating create to a configured callback and rejecting unexpected create use. It
// performs no secret persistence.
// runtimeSecretCommandStub предоставляет только нужные HTTP тестам ответы подготовки
// create/reveal, передавая create заданному callback и отклоняя неожиданный create. Секреты не
// сохраняются.
type runtimeSecretCommandStub struct {
	controlplanev1.PlatformCommandServiceClient
	create func(*controlplanev1.PrepareCreateRuntimeSecretRequest) (*controlplanev1.PrepareCreateRuntimeSecretResponse, error)
}

// runtimeSecretQueryStub stores configured list/get metadata responses and the last list request
// so tests can inspect exact project/filter/pagination mapping without reading live secret state.
// runtimeSecretQueryStub хранит заданные ответы metadata list/get и последний list request для
// проверки точных project/filter/pagination без чтения живого состояния секретов.
type runtimeSecretQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
	list    *controlplanev1.ListRuntimeSecretsResponse
	get     *controlplanev1.GetRuntimeSecretResponse
	request *controlplanev1.ListRuntimeSecretsRequest
}

// ListRuntimeSecrets records the typed list request pointer and returns the configured metadata
// page without applying authorization, filtering or pagination of its own.
// ListRuntimeSecrets сохраняет указатель typed list request и возвращает заданную страницу
// metadata без собственной авторизации, фильтрации и пагинации.
func (stub *runtimeSecretQueryStub) ListRuntimeSecrets(_ context.Context, request *controlplanev1.ListRuntimeSecretsRequest, _ ...grpc.CallOption) (*controlplanev1.ListRuntimeSecretsResponse, error) {
	stub.request = request
	return stub.list, nil
}

// GetRuntimeSecret returns the configured synthetic metadata read response; tenant eligibility and
// secret lookup are not simulated by this stub.
// GetRuntimeSecret возвращает заданный синтетический ответ чтения metadata; tenant допуск и поиск
// секрета этим stub не моделируются.
func (stub *runtimeSecretQueryStub) GetRuntimeSecret(context.Context, *controlplanev1.GetRuntimeSecretRequest, ...grpc.CallOption) (*controlplanev1.GetRuntimeSecretResponse, error) {
	return stub.get, nil
}

// PrepareCreateRuntimeSecret invokes the configured callback with the exact typed create input, or
// fails if no create was expected, allowing content-digest and terminal-receipt assertions.
// PrepareCreateRuntimeSecret вызывает заданный callback с точным typed create входом либо
// отказывает при неожиданном create, позволяя проверить content digest и terminal receipt.
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

// PrepareRevealRuntimeSecret returns a synthetic one-time operation grant for the reveal scenario
// without issuing real owner authority or reading a stored secret.
// PrepareRevealRuntimeSecret возвращает синтетический одноразовый operation grant для reveal
// сценария без выдачи реальных полномочий владельца и чтения сохранённого секрета.
func (runtimeSecretCommandStub) PrepareRevealRuntimeSecret(
	context.Context,
	*controlplanev1.PrepareRevealRuntimeSecretRequest,
	...grpc.CallOption,
) (*controlplanev1.PrepareRevealRuntimeSecretResponse, error) {
	return &controlplanev1.PrepareRevealRuntimeSecretResponse{Operation: &controlplanev1.RuntimeSecretOperationReceipt{OperationGrant: "synthetic-one-time-operation-grant"}}, nil
}

// runtimeSecretBrokerStub embeds the unused generated broker API and supplies the reveal method
// needed by the HTTP cache/elevation test. It has no live broker connection.
// runtimeSecretBrokerStub встраивает неиспользуемый generated broker API и задаёт reveal метод для
// HTTP теста cache/elevation. Соединения с живым broker нет.
type runtimeSecretBrokerStub struct {
	secretbrokerv1.SecretBrokerServiceClient
}

// runtimeSecretOIDCStub holds one synthetic principal for token verification in the real boundary
// test; it intentionally does not implement real signature or issuer verification.
// runtimeSecretOIDCStub хранит один синтетический principal для проверки token в тесте настоящей
// boundary; реальные подписи и issuer намеренно не проверяются.
type runtimeSecretOIDCStub struct{ principal oidcauth.Principal }

// VerifyAuthorization rejects the unexpected authorization-start verification path, ensuring the
// reveal test uses its intended existing-session route.
// VerifyAuthorization отклоняет неожиданный путь проверки начала авторизации, сохраняя intended
// маршрут существующей сессии reveal теста.
func (stub runtimeSecretOIDCStub) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return oidcauth.Principal{}, "", errors.New("unexpected authorization verification")
}

// VerifyToken returns the configured synthetic principal regardless of token text, isolating
// boundary session/elevation behavior from external OIDC verification.
// VerifyToken возвращает заданный синтетический principal независимо от текста token, изолируя
// session/elevation поведение boundary от внешней OIDC проверки.
func (stub runtimeSecretOIDCStub) VerifyToken(context.Context, string) (oidcauth.Principal, error) {
	return stub.principal, nil
}

// runtimeSecretSessionStoreStub holds existing and replacement synthetic claims so the reveal
// scenario can verify session rotation after consuming elevation without a durable session
// backend.
// runtimeSecretSessionStoreStub хранит существующие и replacement синтетические claims для
// проверки смены сессии после потребления elevation без устойчивого session backend.
type runtimeSecretSessionStoreStub struct {
	claims      session.Claims
	replacement session.Claims
}

// Issue returns the configured replacement claims and fixed synthetic encoded-session/CSRF values,
// without deriving them from production credentials or mutating durable state.
// Issue возвращает заданные replacement claims и фиксированные синтетические encoded-session/CSRF
// значения без получения production credentials и изменения устойчивого состояния.
func (stub runtimeSecretSessionStoreStub) Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error) {
	return stub.replacement, "replacement-session", strings.Repeat("d", 43), nil
}

// IssueWithElevation fails on an unexpected elevated-session issuance call because the reveal
// fixture already carries its scoped elevation and should consume it rather than reissue it.
// IssueWithElevation отказывает при неожиданной выдаче elevated сессии, поскольку reveal fixture
// уже содержит scoped elevation и должна потребить его без повторной выдачи.
func (runtimeSecretSessionStoreStub) IssueWithElevation(string, string, string, uint64, string, time.Time, *session.Elevation) (session.Claims, string, string, error) {
	return session.Claims{}, "", "", errors.New("unexpected elevated session issue")
}

// Open returns the configured existing session claims without decoding a real cookie, leaving HTTP
// boundary checks to the code under test.
// Open возвращает заданные claims существующей сессии без декодирования реального cookie, оставляя
// HTTP boundary проверки тестируемому коду.
func (stub runtimeSecretSessionStoreStub) Open(string) (session.Claims, error) {
	return stub.claims, nil
}

// Renew returns unchanged claims and reports no renewal, keeping the reveal test focused on
// elevation consumption and replacement issuance.
// Renew возвращает неизменные claims и отсутствие renewal, сохраняя фокус reveal теста на
// потреблении elevation и выдаче replacement.
func (runtimeSecretSessionStoreStub) Renew(claims session.Claims, _ time.Time) (session.Claims, string, bool, error) {
	return claims, "", false, nil
}

// runtimeSecretRevocationStoreStub tracks one in-memory consumed flag for the synthetic one-time
// elevation path; revocation lookup itself is deliberately inert.
// runtimeSecretRevocationStoreStub хранит один in-memory consumed флаг синтетического одноразового
// elevation пути; сам поиск revocation намеренно инертен.
type runtimeSecretRevocationStoreStub struct{ consumed bool }

// Revoke accepts the test revocation call without storing revocation state; only ConsumeOnce
// models the one-use behavior exercised by this fixture.
// Revoke принимает тестовый revocation вызов без сохранения состояния отзыва; только ConsumeOnce
// моделирует нужное fixture одноразовое поведение.
func (*runtimeSecretRevocationStoreStub) Revoke(context.Context, string) error { return nil }

// Revoked always reports not revoked without external state, so the fixture exercises the
// elevation consume path rather than revocation lookup failures.
// Revoked всегда возвращает отсутствие отзыва без внешнего состояния, чтобы fixture проверяла
// elevation consume путь вместо отказов revocation lookup.
func (*runtimeSecretRevocationStoreStub) Revoked(context.Context, string) (bool, error) {
	return false, nil
}

// ConsumeOnce succeeds once per stub instance, then returns false for later attempts, simulating
// the one-use guard without performing persistent replay protection.
// ConsumeOnce успешен один раз на экземпляр stub, затем возвращает false, моделируя одноразовую
// защиту без устойчивого replay protection.
func (stub *runtimeSecretRevocationStoreStub) ConsumeOnce(context.Context, string) (bool, error) {
	if stub.consumed {
		return false, nil
	}
	stub.consumed = true
	return true, nil
}

// RevealSecret returns a fixed synthetic string value and its typed value kind for response/cache
// assertions, never loading a real credential.
// RevealSecret возвращает фиксированное синтетическое строковое значение и его typed вид для
// проверок ответа и кеширования без загрузки реального credential.
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

// TestRevealRuntimeSecretDisablesCaching uses the real session boundary with scoped synthetic
// elevation and verifies a successful reveal has no-store/no-cache/expiry headers plus two
// replacement cookies after elevation consumption.
// TestRevealRuntimeSecretDisablesCaching использует настоящую session boundary со scoped
// синтетическим elevation и проверяет no-store/no-cache/expiry заголовки успешного reveal и два
// replacement cookie после потребления elevation.
func TestRevealRuntimeSecretDisablesCaching(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	projectRef := "prj_project_sales"
	csrf := strings.Repeat("c", 43)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{
		Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(), SessionRevision: 3,
		SessionID: uuid.NewString(), Bearer: "bearer", CSRFHash: hex.EncodeToString(csrfDigest[:]), IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		Elevation: &session.Elevation{Kind: session.ElevationKindRuntimeSecretReveal, ScopeKind: "PROJECT", OrganizationRef: "org_fixture01", ProjectRef: projectRef, SecretRef: "secret_main", ExpiresAt: now.Add(time.Minute).Unix()},
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
		control:  &controlplaneclient.Client{Command: runtimeSecretCommandStub{}, Query: &runtimeSecretQueryStub{get: &controlplanev1.GetRuntimeSecretResponse{Secret: &controlplanev1.RuntimeSecret{Ref: "secret_main", ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01", ProjectRef: projectRef}}}},
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

// TestRuntimeSecretReadEndpointsReturnPublicMetadataShape checks list/get responses expose
// normalized metadata, exact revision and optional display hint while omitting raw value, internal
// secret UID and an extra secret wrapper.
// TestRuntimeSecretReadEndpointsReturnPublicMetadataShape проверяет выдачу нормализованных
// metadata, точной revision и optional display hint в list/get без raw value, внутреннего secret
// UID и лишнего secret wrapper.
func TestRuntimeSecretReadEndpointsReturnPublicMetadataShape(t *testing.T) {
	t.Parallel()
	now := timestamppb.Now()
	secret := &controlplanev1.RuntimeSecret{
		ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01",
		Ref: "sec_metadata123", ProjectRef: "prj_project_sales", Name: "CRM_TOKEN", Description: "CRM",
		ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: "ACTIVE",
		Version: 3, CurrentRevision: 2, DisplayHint: &controlplanev1.RuntimeSecretDisplayHint{Prefix: "syn", Suffix: "key"},
		CreatedAt: now, UpdatedAt: now,
	}
	query := &runtimeSecretQueryStub{
		list: &controlplanev1.ListRuntimeSecretsResponse{Secrets: []*controlplanev1.RuntimeSecret{secret}, Page: &controlplanev1.PageInfo{NextPageToken: "next-page"}},
		get:  &controlplanev1.GetRuntimeSecretResponse{Secret: secret},
	}
	server := &Server{control: &controlplaneclient.Client{Query: query}}

	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_project_sales/runtime-secrets", nil)
	listRequest.Header.Set(boundary.ProjectReferenceHeader, "prj_project_sales")
	server.ListRuntimeSecrets(listResponse, listRequest, "prj_project_sales", generated.ListRuntimeSecretsParams{})
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d", listResponse.Code)
	}
	var page map[string]any
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	items, ok := page["items"].([]any)
	if !ok || len(items) != 1 || page["nextPageToken"] != "next-page" || page["secrets"] != nil {
		t.Fatalf("unexpected runtime secret page shape: %s", listResponse.Body.String())
	}
	item := items[0].(map[string]any)
	if item["ref"] != "sec_metadata123" || item["currentRevision"] != float64(2) || item["displayHint"] == nil || item["secretUid"] != nil || item["value"] != nil {
		t.Fatalf("unsafe or incomplete runtime secret metadata: %v", item)
	}

	getResponse := httptest.NewRecorder()
	server.GetRuntimeSecret(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/runtime-secrets/sec_metadata123", nil), "sec_metadata123")
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"currentRevision":2`) || strings.Contains(getResponse.Body.String(), `"secret"`) {
		t.Fatalf("unexpected runtime secret get shape: status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

// TestCastRuntimeSecretMetadataKeepsNextActionsAsArray requires revoked broker metadata to
// serialize a nonnull empty nextActions array, preserving the public terminal-secret response
// shape.
// TestCastRuntimeSecretMetadataKeepsNextActionsAsArray требует сериализации revoked broker
// metadata с непустым по типу, но пустым по содержимому nextActions массивом, сохраняя публичную
// форму terminal секрета.
func TestCastRuntimeSecretMetadataKeepsNextActionsAsArray(t *testing.T) {
	t.Parallel()
	now := timestamppb.Now()
	result := castRuntimeSecretMetadata(&secretbrokerv1.RuntimeSecretMetadata{
		ScopeKind: secretbrokerv1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01",
		SecretRef: "sec_terminal123", ProjectRef: "prj_project_sales", Name: "CRM_TOKEN",
		ValueType: secretbrokerv1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
		Status:    secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED,
		Version:   3, Revision: 1, CreatedAt: now, UpdatedAt: now,
	})
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"nextActions":[]`) {
		t.Fatalf("terminal secret response must contain a non-null nextActions array: %s", body)
	}
}

// TestCreateRuntimeSecretBindsDigestAndReturnsTerminalReceipt checks create preparation binds
// SHA-256 of exact synthetic value bytes and a saved terminal receipt returns normalized metadata
// without plaintext or null nextActions.
// TestCreateRuntimeSecretBindsDigestAndReturnsTerminalReceipt проверяет связь подготовки create с
// SHA-256 точных байтов синтетического значения и выдачу сохранённого terminal receipt как
// нормализованных metadata без plaintext и null nextActions.
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
				ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01",
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
	if !strings.Contains(response.Body.String(), `"valueType":"STRING"`) || strings.Contains(response.Body.String(), "RUNTIME_SECRET_VALUE_TYPE_") {
		t.Fatalf("create response is not normalized: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"nextActions":[]`) {
		t.Fatalf("create response must contain a non-null nextActions array: %s", response.Body.String())
	}
}

// TestListRuntimeSecretsNormalizesPublicShapeAndPagination checks project/page selectors reach the
// owner request unchanged and the public page retains STRING/ROTATE enums and continuation token
// without protobuf prefixes.
// TestListRuntimeSecretsNormalizesPublicShapeAndPagination проверяет неизменную передачу
// project/page selectors запросу владельца и сохранение STRING/ROTATE enum и токена продолжения в
// публичной странице без protobuf префиксов.
func TestListRuntimeSecretsNormalizesPublicShapeAndPagination(t *testing.T) {
	now := timestamppb.Now()
	query := &runtimeSecretQueryStub{list: &controlplanev1.ListRuntimeSecretsResponse{
		Secrets: []*controlplanev1.RuntimeSecret{{
			ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture01",
			Ref: "sec_database01", ProjectRef: "prj_project_sales", Name: "DATABASE_TOKEN", Description: "Database",
			ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: "ACTIVE",
			Version: 2, CurrentRevision: 2, CreatedAt: now, UpdatedAt: now,
			NextActions: []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_ROTATE},
		}},
		Page: &controlplanev1.PageInfo{NextPageToken: "next-page"},
	}}
	server := &Server{control: &controlplaneclient.Client{Query: query}}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/prj_project_sales/runtime-secrets?query=data&pageSize=10&pageToken=cursor", nil)
	search, pageSize, pageToken := "data", 10, "cursor"

	server.ListRuntimeSecrets(response, request, "prj_project_sales", generated.ListRuntimeSecretsParams{
		Query: &search, PageSize: &pageSize, PageToken: &pageToken,
	})

	if response.Code != http.StatusOK || query.request.GetProjectRef() != "prj_project_sales" || query.request.GetPage().GetPageSize() != 10 || query.request.GetPage().GetPageToken() != "cursor" {
		t.Fatalf("list mapping failed: status=%d request=%v body=%s", response.Code, query.request, response.Body.String())
	}
	body := response.Body.String()
	for _, value := range []string{`"valueType":"STRING"`, `"nextActions":["ROTATE"]`, `"nextPageToken":"next-page"`} {
		if !strings.Contains(body, value) {
			t.Fatalf("list response misses %s: %s", value, body)
		}
	}
	if strings.Contains(body, "RUNTIME_SECRET_VALUE_TYPE_") {
		t.Fatalf("list response leaked protobuf enum: %s", body)
	}
}
