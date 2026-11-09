package integration

import (
	"context"
	"encoding/json"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// TestGitLabPersonalScopeObservation exercises the actual verifier and bounded fixed HTTP reads.
// It distinguishes known denials from unsupported metadata, preserves401 and emits no WRITE proof.
// TestGitLabPersonalScopeObservation проверяет настоящий verifier и bounded fixed HTTP reads.
// Он отличает known denials от unsupported metadata, сохраняет401 и не выдаёт WRITE proof.
func TestGitLabPersonalScopeObservation(t *testing.T) {
	for _, scenario := range []string{"allow", "read only", "fine project", "fine missing action", "fine wrong project", "fine group", "fine missing detail", "fine changed token", "reporter", "archived", "deleting", "disabled feature", "missing role", "old version", "missing granular", "missing expiry", "inactive", "revoked", "expired", "foreign user", "wrong project", "duplicate", "401", "403", "404", "429", "timeout", "unrequested", "unknown profile", "extra key", "duplicate profile"} {
		t.Run(scenario, func(t *testing.T) {
			adapter := legacyGitLabTestAdapter(t)
			credential := testCredential(t, adapter, "synthetic-gitlab-scope-key")
			definition := adapter.definitions["gitlab"]
			request := invocationRequest(t, definition, "gitlab.project.metadata.read", map[string]any{}, credential)
			request.CapabilityKey, request.Operation, request.GrantRef, request.GrantVersion = "", "", "", 0
			request.PersonalObservationProfiles, request.PersonalDeniedWriteCapabilityKeys = integrationpackage.CredentialObservationRequests(definition)
			if len(request.PersonalObservationProfiles) != 1 || len(request.PersonalDeniedWriteCapabilityKeys) != 3 {
				t.Fatal("closed metadata requests missing")
			}
			switch scenario {
			case "unrequested":
				request.PersonalObservationProfiles, request.PersonalDeniedWriteCapabilityKeys = nil, nil
			case "unknown profile":
				request.PersonalObservationProfiles = []string{"UNKNOWN"}
			case "extra key":
				request.PersonalDeniedWriteCapabilityKeys = append(request.PersonalDeniedWriteCapabilityKeys, "gitlab.project.metadata.read")
			case "duplicate profile":
				request.PersonalObservationProfiles = append(request.PersonalObservationProfiles, request.PersonalObservationProfiles[0])
			}
			calls := 0
			adapter.providerHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Host != strings.TrimPrefix(request.Configuration["base_url"].(string), "https://") || r.Header.Get("Authorization") != "Bearer synthetic-gitlab-scope-key" {
					t.Fatal("scope verification crossed method/origin/key boundary")
				}
				if calls > 6 {
					t.Fatal("metadata verification retried or exceeded finite GET bound")
				}
				if scenario == "timeout" && r.URL.Path == "/api/v4/version" {
					return nil, context.DeadlineExceeded
				}
				fixture := request
				fixture.Operation = "gitlab.issue.create"
				responseScenario := scenario
				// The health call succeeds; the independently observed metadata may later disagree.
				// Health вызов успешен; independently observed metadata затем могут измениться.
				if calls == 1 {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(catalogResponse(t, "gitlab", "gitlab.project.metadata.read", r))), Request: r}, nil
				}
				return gitLabPermissionResponse(t, fixture, r, responseScenario), nil
			})}
			result, err := adapter.VerifyPersonalCredential(t.Context(), request)
			if scenario == "unknown profile" || scenario == "extra key" || scenario == "duplicate profile" {
				if err == nil || calls != 0 {
					t.Fatal("invalid request performed a provider read")
				}
				return
			}
			if slices.Contains([]string{"401", "inactive", "revoked", "expired"}, scenario) {
				if err == nil || len(result.ConfirmedCapabilityKeys) != 0 || len(result.ObservedPermissionProfiles) != 0 {
					t.Fatal("401 retained authentication observations")
				}
				return
			}
			if err != nil || !slices.Equal(result.ConfirmedCapabilityKeys, []string{"gitlab.project.metadata.read"}) {
				t.Fatal("scope observation lost authenticated health", err)
			}
			known := slices.Contains([]string{"allow", "read only", "fine project", "fine missing action", "fine wrong project", "reporter", "archived", "deleting", "disabled feature"}, scenario)
			if (len(result.ObservedPermissionProfiles) == 1) != known || (result.ExpiresAt != nil) != known {
				t.Fatal("unknown metadata became checked or fabricated expiry")
			}
			if known && (!result.ExpiresAt.Equal(time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)) || result.ObservedPermissionProfiles[0] != integrationpackage.CredentialObservationGitLabProjectWriteScopeV1) {
				t.Fatal("metadata profile/expiry changed")
			}
			expectedDenied := 0
			if known && scenario != "allow" {
				expectedDenied = 3
				if scenario == "fine project" {
					expectedDenied = 2
				}
			}
			if len(result.DeniedCapabilityKeys) != expectedDenied {
				t.Fatalf("wrong known denial count: %d", len(result.DeniedCapabilityKeys))
			}
			for _, key := range result.DeniedCapabilityKeys {
				if !integrationpackage.CredentialObservationMayDeny(integrationpackage.CredentialObservationGitLabProjectWriteScopeV1, key) || scenario == "fine project" && key == "gitlab.issue.create" {
					t.Fatal("metadata denied an unobserved operation")
				}
			}
			raw, _ := json.Marshal(result)
			for _, forbidden := range []string{"synthetic-gitlab-scope-key", "group/project", "user_id", "granular_scopes", "access_level"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatal("raw metadata or key escaped observation")
				}
			}
			if scenario == "unrequested" && calls != 1 {
				t.Fatal("legacy job invented metadata request")
			}
		})
	}
}

// TestGitLabScopeUnknownMetadata keeps future scopes and unknown Proto values unconfirmed,
// while documented api/read_api remain distinguishable; no provider call is needed for this case.
// TestGitLabScopeUnknownMetadata оставляет future scopes и unknown Proto values unconfirmed,
// различая documented api/read_api; provider call для этого случая не требуется.
func TestGitLabScopeUnknownMetadata(t *testing.T) {
	for _, value := range []string{"api", "read_api", "future_all_access"} {
		token := map[string]json.RawMessage{"granular": json.RawMessage("false"), "scopes": json.RawMessage(`["` + value + `"]`)}
		if gitLabObservedTokenScopeKnown(token) != (value != "future_all_access") {
			t.Fatal("future scope became an observed allow/denial")
		}
	}
	for _, value := range []cp.PersonalIntegrationCredentialObservationProfile{0, 1, 2, -1} {
		profiles := personalObservationProfilesFromProto([]cp.PersonalIntegrationCredentialObservationProfile{value})
		if len(profiles) != 1 || (profiles[0] == integrationpackage.CredentialObservationGitLabProjectWriteScopeV1) != (value == 1) {
			t.Fatal("typed profile was dropped or silently upgraded")
		}
	}
}
