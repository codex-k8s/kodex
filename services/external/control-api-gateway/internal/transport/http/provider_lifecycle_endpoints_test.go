package httptransport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func providerLifecycleHandler(client *catalogRPCRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(client), Command: cp.NewPlatformCommandServiceClient(client)}})
}

func TestProviderBlockersPreserveHiddenCountAndOwnerPins(t *testing.T) {
	response := &cp.ListProviderAccountBlockersResponse{AccountVersion: 3, ContextDigest: strings.Repeat("a", 64), Total: 12, HiddenCount: 2,
		Page: &cp.PageInfo{NextPageToken: "next"}, Items: []*cp.ProviderAccountBlocker{{Kind: cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_QUEUED_TURN,
			Ref: "run_fixture01", Version: 5, Name: "TYPE_literal", CanCancel: true}}}
	client := &catalogRPCRecorder{response: response}
	w := httptest.NewRecorder()
	providerLifecycleHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/provider-accounts/pacc_fixture01/blockers?kind=QUEUED_TURN&query=literal&pageSize=10&pageToken=before", nil))
	var body generated.ProviderAccountBlockerPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Total != 12 || body.HiddenCount != 2 || body.DeletionIntentVersion != 0 || body.Items[0].Name != "TYPE_literal" || !body.Items[0].CanCancel || body.NextPageToken == nil || *body.NextPageToken != "next" {
		t.Fatal("blocker projection lost owner counts, source text or pins")
	}
	request := client.request.(*cp.ListProviderAccountBlockersRequest)
	if request.AccountRef != "pacc_fixture01" || request.Query != "literal" || request.Kind != cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_QUEUED_TURN || request.Page.PageSize != 10 || request.Page.PageToken != "before" {
		t.Fatal("blocker selection changed before owner")
	}
	for _, mutate := range []func(*cp.ListProviderAccountBlockersResponse){
		func(v *cp.ListProviderAccountBlockersResponse) { v.HiddenCount = -1 },
		func(v *cp.ListProviderAccountBlockersResponse) { v.ContextDigest = "unknown" },
		func(v *cp.ListProviderAccountBlockersResponse) {
			v.Items[0].Kind = cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_ACTIVE_TURN
		},
		func(v *cp.ListProviderAccountBlockersResponse) { v.Items = append(v.Items, v.Items[0]) },
		func(v *cp.ListProviderAccountBlockersResponse) { v.Page.NextPageToken = "before" },
	} {
		candidate := proto.Clone(response).(*cp.ListProviderAccountBlockersResponse)
		mutate(candidate)
		w = httptest.NewRecorder()
		providerLifecycleHandler(&catalogRPCRecorder{response: candidate}).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/provider-accounts/pacc_fixture01/blockers?pageToken=before", nil))
		if w.Code != 502 || strings.Contains(w.Body.String(), "TYPE_literal") {
			t.Fatal("inconsistent blocker response escaped owner boundary")
		}
	}
}

func TestProviderQueuedCancellationPreservesExactSelectedOutcomes(t *testing.T) {
	response := &cp.CancelProviderAccountQueuedWorkResponse{Account: &cp.ProviderAccount{Ref: "pacc_fixture01", Version: 4},
		Outcomes: []*cp.ProviderAccountQueuedWorkResult{{RunRef: "run_fixture01", Outcome: cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_CANCELLED}, {RunRef: "run_fixture02", Outcome: cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_PERMISSION_REQUIRED}}}
	client := &catalogRPCRecorder{response: response}
	invoke := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/provider-accounts/pacc_fixture01/queued-work/cancellation", strings.NewReader(`{"selectedRunRefs":["run_fixture01","run_fixture02"],"blockersDigest":"`+strings.Repeat("a", 64)+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", `"3"`)
		r.Header.Set("Idempotency-Key", "cancel-provider-01")
		r.Header.Set("X-CSRF-Token", "csrf-fixture")
		providerLifecycleHandler(client).ServeHTTP(w, r)
		return w
	}
	w := invoke()
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"PERMISSION_REQUIRED"`) {
		t.Fatal("partial queued cancellation outcome was lost")
	}
	request := client.request.(*cp.CancelProviderAccountQueuedWorkRequest)
	if request.Mutation.GetExpectedVersion() != 3 || request.AccountRef != "pacc_fixture01" || len(request.SelectedRunRefs) != 2 || request.BlockersDigest != strings.Repeat("a", 64) {
		t.Fatal("queued cancellation lost exact mutation pins")
	}
	for outcome := cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_CANCELLED; outcome <= cp.ProviderAccountQueuedWorkOutcome_PROVIDER_ACCOUNT_QUEUED_WORK_OUTCOME_NOT_FOUND; outcome++ {
		response.Outcomes[0].Outcome = outcome
		if invoke().Code != 200 {
			t.Fatal("closed owner outcome was lost")
		}
	}
	for _, mutate := range []func(*cp.CancelProviderAccountQueuedWorkResponse){
		func(v *cp.CancelProviderAccountQueuedWorkResponse) { v.Account = nil },
		func(v *cp.CancelProviderAccountQueuedWorkResponse) { v.Account.Ref = "pacc_other001" },
		func(v *cp.CancelProviderAccountQueuedWorkResponse) { v.Outcomes[0] = nil },
		func(v *cp.CancelProviderAccountQueuedWorkResponse) { v.Outcomes[0].RunRef = "run_other0001" },
		func(v *cp.CancelProviderAccountQueuedWorkResponse) {
			v.Outcomes[0].Outcome = cp.ProviderAccountQueuedWorkOutcome(99)
		},
		func(v *cp.CancelProviderAccountQueuedWorkResponse) { v.Outcomes = v.Outcomes[:1] },
		func(v *cp.CancelProviderAccountQueuedWorkResponse) {
			v.Outcomes[0], v.Outcomes[1] = v.Outcomes[1], v.Outcomes[0]
		},
	} {
		candidate := proto.Clone(response).(*cp.CancelProviderAccountQueuedWorkResponse)
		mutate(candidate)
		client.response = candidate
		if invoke().Code != 502 {
			t.Fatal("inconsistent cancellation result accepted")
		}
	}
}

func TestProviderLifecycleRejectsFalseTerminalAndStaleVerificationPins(t *testing.T) {
	now := time.Now().UTC()
	d := &cp.ProviderAccountDeletion{Ref: "pdel_fixture01", Version: 2, State: cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_DELETED,
		RequestedAt: timestamppb.New(now.Add(-time.Minute)), CompletedAt: timestamppb.New(now), SafeReason: "ACCOUNT_DELETED"}
	for kind := cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_AGENT; kind <= cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_WARM_RUNTIME; kind++ {
		d.Blockers = append(d.Blockers, &cp.ProviderAccountBlockerCount{Kind: kind})
	}
	a := &cp.ProviderAccount{Ref: "pacc_fixture01", Version: 5, State: cp.ProviderAccountState_PROVIDER_ACCOUNT_STATE_DELETED, Deletion: d,
		Verification: &cp.ProviderAccountVerification{Ref: "pver_fixture01", AccountVersion: 3, CredentialRevision: 2,
			State: cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_STALE, Scope: cp.ProviderAccountVerificationScope_PROVIDER_ACCOUNT_VERIFICATION_SCOPE_CREDENTIALED_CATALOG_REACHABILITY,
			RequestedAt: timestamppb.New(now.Add(-time.Minute)), CompletedAt: timestamppb.New(now), SafeReason: "VERIFICATION_SOURCE_CHANGED"}}
	value, err := messageMap(a)
	if err != nil || value["state"] != "DELETED" || value["deletion"].(map[string]any)["pendingCleanup"] != float64(0) {
		t.Fatal("terminal tombstone or historical verification pin was lost")
	}
	for _, mutate := range []func(*cp.ProviderAccount){
		func(v *cp.ProviderAccount) { v.Deletion = nil },
		func(v *cp.ProviderAccount) { v.Deletion.Blockers[0].Total = 1 },
		func(v *cp.ProviderAccount) { v.Deletion.PendingCleanup = 1 },
		func(v *cp.ProviderAccount) { v.Deletion.CompletedAt = nil },
		func(v *cp.ProviderAccount) { v.Verification.AccountVersion = 6 },
		func(v *cp.ProviderAccount) { v.Verification.SafeReason = "private error" },
		func(v *cp.ProviderAccount) { v.Verification.State = cp.ProviderAccountVerificationState(99) },
		func(v *cp.ProviderAccount) { v.State = cp.ProviderAccountState(99) },
	} {
		candidate := proto.Clone(a).(*cp.ProviderAccount)
		mutate(candidate)
		if _, err := messageMap(candidate); err == nil {
			t.Fatal("inconsistent provider lifecycle snapshot accepted")
		}
	}
}
