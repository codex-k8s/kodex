package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestPersonalCredentialReadProbeHTTPBoundary exercises actual adapter selection and HTTP requests
// with synthetic transport: exact host/project, GET only, one-item pages,403 versus401/unknown,
// no write permission inference and no provider content in the returned verification metadata.
// TestPersonalCredentialReadProbeHTTPBoundary проверяет adapter selection и HTTP requests через
// synthetic transport: exact host/project, только GET, page1,403 против401/unknown, отсутствие
// вывода write permissions и provider content в возвращаемых verification metadata.
func TestPersonalCredentialReadProbeHTTPBoundary(t *testing.T) {
	for _, rejection := range []int{0, http.StatusForbidden, http.StatusUnauthorized, http.StatusNotFound} {
		t.Run(http.StatusText(rejection), func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-personal-token")
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Host != "gitlab.example.invalid" || !strings.HasPrefix(r.URL.EscapedPath(), "/api/v4/projects/group%2Fproject") {
					t.Fatal("probe crossed host, scope or method boundary")
				}
				body, code := "[]", http.StatusOK
				if r.URL.EscapedPath() == "/api/v4/projects/group%2Fproject" {
					body = `{"id":7,"path_with_namespace":"group/project","default_branch":"main","visibility":"private","archived":false}`
				} else {
					if r.URL.Query().Get("per_page") != "1" {
						t.Fatal("probe lost one-item pagination bound")
					}
					if strings.HasSuffix(r.URL.Path, "/issues") && rejection != 0 {
						code, body = rejection, `{"private":"synthetic-sensitive-response"}`
					}
				}
				return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			request := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.project.metadata.read", map[string]any{}, credential)
			request.Configuration["base_url"] = "https://gitlab.example.invalid"
			request.GrantRef, request.GrantVersion, request.Operation, request.CapabilityKey = "", 0, "", ""
			for _, probe := range integrationpackage.CredentialReadProbes(adapter.definitions["gitlab"]) {
				request.PersonalReadProbeCapabilityKeys = append(request.PersonalReadProbeCapabilityKeys, probe.Key)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, err := adapter.VerifyPersonalCredential(ctx, request)
			if rejection == http.StatusUnauthorized {
				if err == nil || calls != 2 || len(result.ConfirmedCapabilityKeys) != 0 {
					t.Fatal("401 did not discard entire observation and stop")
				}
				return
			}
			if err != nil || calls != 8 {
				t.Fatalf("bounded verification failed: calls=%d error=%v", calls, err)
			}
			if slices.Contains(result.DeniedCapabilityKeys, "gitlab.issue.list") != (rejection == http.StatusForbidden) {
				t.Fatal("only observed403 may deny a capability")
			}
			if slices.Contains(result.ConfirmedCapabilityKeys, "gitlab.issue.list") != (rejection == 0) {
				t.Fatal("unobserved or forbidden operation became confirmed")
			}
			for _, key := range result.ConfirmedCapabilityKeys {
				if strings.Contains(key, "create") || strings.Contains(key, "write") {
					t.Fatal("READ inferred write permission")
				}
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "sensitive") || strings.Contains(string(raw), "synthetic-personal-token") {
				t.Fatal("verification exposed provider content or credential")
			}
		})
	}
}

// TestPersonalCredentialServerIdentityProbe uses both typed Server adapters and their actual
// versioned wire fixtures; it confirms only health plus identity, not Cloud APIs or writes.
// TestPersonalCredentialServerIdentityProbe использует оба typed Server adapters и настоящие
// versioned wire fixtures; подтверждает только health и identity, не Cloud API или запись.
func TestPersonalCredentialServerIdentityProbe(t *testing.T) {
	for _, key := range []string{jiraServerDefinition, confluenceServerDefinition} {
		t.Run(key, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-server-token")
			op := "jira.project.read"
			if key == confluenceServerDefinition {
				op = "confluence.space.read"
			}
			request := atlassianServerInvocation(t, adapter, key, op, map[string]any{}, credential)
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				invocation := request
				invocation.Operation = op
				if strings.HasSuffix(r.URL.Path, "/myself") {
					invocation.Operation = "jira.identity.read"
				}
				if strings.HasSuffix(r.URL.Path, "/user/current") {
					invocation.Operation = "confluence.identity.read"
				}
				invocation.CapabilityKey = invocation.Operation
				return atlassianServerResponse(t, invocation, r), nil
			})}
			adapter.providerHTTPClient, adapter.openAPIHTTPClient = client, client
			request.GrantRef, request.GrantVersion, request.Operation, request.CapabilityKey = "", 0, "", ""
			for _, probe := range integrationpackage.CredentialReadProbes(adapter.definitions[key]) {
				request.PersonalReadProbeCapabilityKeys = append(request.PersonalReadProbeCapabilityKeys, probe.Key)
			}
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if err != nil || calls != 2 || len(result.ConfirmedCapabilityKeys) != 2 {
				t.Fatalf("Server identity probe failed: calls=%d error=%v metadata=%v", calls, err, result)
			}
		})
	}
}

// TestPersonalCredentialProbeOwnerPins preserves old-owner health-only admission and refuses
// unregistered, duplicate or health-omitting key sets before any HTTP call.
// TestPersonalCredentialProbeOwnerPins сохраняет health-only допуск прежнего owner и отклоняет
// неизвестные, повторные либо не содержащие health наборы до любого HTTP-запроса.
func TestPersonalCredentialProbeOwnerPins(t *testing.T) {
	for _, keys := range [][]string{nil, {"gitlab.issue.create"}, {"gitlab.issue.list"}, {"gitlab.project.metadata.read", "gitlab.project.metadata.read"}} {
		adapter := testAdapter(t)
		credential := testCredential(t, adapter, "synthetic-personal-token")
		calls := 0
		adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"id":7,"path_with_namespace":"group/project","default_branch":"main","visibility":"private","archived":false}`))}, nil
		})}
		request := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.project.metadata.read", map[string]any{}, credential)
		request.PersonalReadProbeCapabilityKeys = keys
		request.Configuration["base_url"] = "https://gitlab.example.invalid"
		result, err := adapter.VerifyPersonalCredential(t.Context(), request)
		if keys == nil {
			if err != nil || calls != 1 || len(result.ConfirmedCapabilityKeys) != 1 {
				t.Fatal("legacy owner authorized additional probes")
			}
		} else if err == nil || calls != 0 {
			t.Fatal("unregistered owner pins reached provider")
		}
	}
}
