package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// scopedGitLabTestRequest creates a synthetic exact current-version claim; changed test inputs/configuration receive fresh digests rather than weakening production pin checks.
// scopedGitLabTestRequest создаёт synthetic exact current-version claim; изменённые test inputs/configuration получают свежие digests без ослабления production pin checks.
func scopedGitLabTestRequest(t *testing.T, adapter *Adapter, operation string, configuration map[string]string, input map[string]any) Request {
	t.Helper()
	definition := adapter.definitions["gitlab"]
	capability, exists := definition.Capability(operation)
	if !exists || capability.ResourceScope.Kind != "GITLAB_SCOPE" {
		t.Fatal("current scoped GitLab operation unavailable")
	}
	request := invocationRequest(t, definition, operation, input, testCredential(t, adapter, "synthetic-scoped-key"))
	request.Input = input
	request.Configuration = make(map[string]any, len(configuration))
	for key, value := range configuration {
		request.Configuration[key] = value
	}
	var err error
	request.ResourceScope, err = capability.ResourceScopeValues(configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		t.Fatal("scoped fixture configuration invalid", err)
	}
	rawScope, _ := json.Marshal(request.ResourceScope)
	rawInput, _ := json.Marshal(input)
	canonical, err := capability.ValidateInput(rawInput)
	if err != nil {
		t.Fatal("scoped fixture input invalid", err)
	}
	scopeDigest, inputDigest := sha256.Sum256(rawScope), sha256.Sum256(canonical)
	request.ResourceScopeDigest, request.InputDigest = hex.EncodeToString(scopeDigest[:]), hex.EncodeToString(inputDigest[:])
	return request
}

// TestGitLabScopedCredentialHealth never probes writes or claims broad object permission; all configured modes use the same selected HTTPS peer and personal key.
// TestGitLabScopedCredentialHealth не проверяет writes и не объявляет broad object permission; все modes используют один selected HTTPS peer и personal key.
func TestGitLabScopedCredentialHealth(t *testing.T) {
	for _, kind := range []string{"PROJECT", "GROUP", "INSTANCE"} {
		t.Run(kind, func(t *testing.T) {
			adapter := testAdapter(t)
			configuration := map[string]string{"base_url": "https://git.example.invalid", "scope_kind": kind}
			if kind == "PROJECT" {
				configuration["project_path"] = "team/service"
			}
			if kind == "GROUP" {
				configuration["group_path"] = "team"
			}
			request := scopedGitLabTestRequest(t, adapter, "gitlab.scope.health.read", configuration, map[string]any{})
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			for _, probe := range integrationpackage.CredentialReadProbes(adapter.definitions["gitlab"]) {
				request.PersonalReadProbeCapabilityKeys = append(request.PersonalReadProbeCapabilityKeys, probe.Key)
			}
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("scoped verification fell back to a different transport")
				return nil, errors.New("fixture transport forbidden")
			})}
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Host != "git.example.invalid" || r.Header.Get("Authorization") != "Bearer synthetic-scoped-key" {
					t.Fatal("scoped verification crossed host/key/method boundary")
				}
				body := ""
				switch r.URL.Path {
				case "/api/v4/user":
					body = `{"id":7,"private":"synthetic-sensitive-provider-content"}`
				case "/api/v4/projects/team/service":
					body = `{"id":20,"path_with_namespace":"team/service","namespace":{"id":10,"full_path":"team","kind":"group"}}`
				case "/api/v4/groups/team":
					body = `{"id":10,"full_path":"team"}`
				case "/api/v4/groups/10/projects", "/api/v4/projects":
					if r.URL.Query().Get("per_page") != "1" || r.URL.Query().Get("membership") != "" || kind == "GROUP" && r.URL.Query().Get("with_shared") != "false" {
						t.Fatal("project discovery lost its one-item/visibility boundary")
					}
					body = `[]`
				default:
					t.Fatal("unregistered scoped verification endpoint")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if err != nil || !slices.Equal(result.ConfirmedCapabilityKeys, []string{"gitlab.scope.health.read", "gitlab.project.list"}) || len(result.ObservedPermissionProfiles) != 0 || len(result.DeniedCapabilityKeys) != 0 {
				t.Fatal("scope health misrepresented personal credential permissions", err)
			}
			if calls != map[string]int{"PROJECT": 3, "GROUP": 4, "INSTANCE": 2}[kind] {
				t.Fatal("scope health exceeded its bounded observation set")
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "synthetic-sensitive") || strings.Contains(string(encoded), "synthetic-scoped-key") {
				t.Fatal("scope health exposed provider content or credential")
			}
		})
	}
}

// TestGitLabScopedInvocationFailureBeforeMutation never misclassifies an unsuccessful scope lookup as an external write; no mutation or receipt can exist yet.
// TestGitLabScopedInvocationFailureBeforeMutation не классифицирует unsuccessful scope lookup как external write; mutation либо receipt ещё отсутствуют.
func TestGitLabScopedInvocationFailureBeforeMutation(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			adapter := testAdapter(t)
			request := scopedGitLabTestRequest(t, adapter, "gitlab.merge_request.discussion.create",
				map[string]string{"base_url": "https://git.example.invalid", "scope_kind": "INSTANCE"},
				map[string]any{"project_path": "team/service", "project_id": 20, "merge_request_iid": 7, "body": "synthetic"})
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.EscapedPath() != "/api/v4/projects/team%2Fservice" {
					t.Fatal("failed scope lookup attempted an external mutation")
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			expectedCalls := 1
			if status == http.StatusServiceUnavailable {
				expectedCalls = 2
			}
			if err == nil || IsUnknownOutcome(err) || calls != expectedCalls || result.Receipt.EffectKey != "" {
				t.Fatal("failed preflight reported a mutation or exceeded its bounded read budget", err)
			}
		})
	}
}

// TestGitLabLargeGroupHealthReadsIdentityOnly keeps personal verification bounded when a provider embeds a large project catalog by default.
// TestGitLabLargeGroupHealthReadsIdentityOnly сохраняет ограниченную личную проверку, когда провайдер по умолчанию вкладывает большой каталог проектов.
func TestGitLabLargeGroupHealthReadsIdentityOnly(t *testing.T) {
	for _, ignoresProjection := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata-only", true: "oversized-response-rejected"}[ignoresProjection], func(t *testing.T) {
			adapter := testAdapter(t)
			request := scopedGitLabTestRequest(t, adapter, "gitlab.scope.health.read", map[string]string{
				"base_url": "https://git.example.invalid", "scope_kind": "GROUP", "group_path": "team/sample",
			}, map[string]any{})
			request.Credential = testCredential(t, adapter, "synthetic-gitlab-personal-key")
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			validateWire := gitLabWireValidator(t, request)
			groupCalls, identityCalls := 0, 0
			adapter.nativeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "git.example.invalid" || r.Header.Get("Authorization") != "Bearer synthetic-gitlab-personal-key" {
					t.Fatal("group identity lookup crossed its read-only transport boundary")
				}
				body := ""
				switch r.URL.Path {
				case "/api/v4/groups/team/sample":
					groupCalls++
					body = `{"id":10,"full_path":"team/sample"}`
					if ignoresProjection || r.URL.Query().Get("with_projects") != "false" {
						body = `{"id":10,"full_path":"team/sample","projects":[{"description":"` + strings.Repeat("x", maximumResponseBytes+1) + `"}]}`
					}
				case "/api/v4/user":
					identityCalls++
					body = `{"id":7,"state":"active"}`
				default:
					t.Fatal("group health requested project content or an unknown endpoint")
				}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
				if !ignoresProjection && len(body) <= maximumResponseBytes {
					validateWire(r, response)
				}
				return response, nil
			})}
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if ignoresProjection {
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || len(result.ConfirmedCapabilityKeys) != 0 || identityCalls != 0 {
					t.Fatal("oversized group response bypassed the existing body limit", err)
				}
			} else if err != nil || !slices.Equal(result.ConfirmedCapabilityKeys, []string{"gitlab.scope.health.read"}) || identityCalls != 1 {
				t.Fatal("large group failed its bounded identity-only health check", err)
			}
			if groupCalls != 1 {
				t.Fatal("group health repeated the lookup or followed embedded projects")
			}
		})
	}
}
