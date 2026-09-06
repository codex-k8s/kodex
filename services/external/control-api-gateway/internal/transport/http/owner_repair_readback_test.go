package httptransport

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFailedProviderDeletionRemainsRetryableOnlyByOwnerAction(t *testing.T) {
	d := &cp.ProviderAccountDeletion{Ref: "pdel_fixture01", Version: 3,
		State:          cp.ProviderAccountDeletionState_PROVIDER_ACCOUNT_DELETION_STATE_FAILED,
		PendingCleanup: 1, RequestedAt: timestamppb.New(time.Now().UTC()), SafeReason: "CREDENTIAL_CLEANUP_FAILED"}
	for kind := cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_AGENT; kind <= cp.ProviderAccountBlockerKind_PROVIDER_ACCOUNT_BLOCKER_KIND_WARM_RUNTIME; kind++ {
		d.Blockers = append(d.Blockers, &cp.ProviderAccountBlockerCount{Kind: kind})
	}
	a := &cp.ProviderAccount{Ref: "pacc_primary01", Version: 8, State: cp.ProviderAccountState_PROVIDER_ACCOUNT_STATE_DELETING, Deletion: d}
	for _, actions := range [][]cp.NextAction{nil, {cp.NextAction_NEXT_ACTION_DELETE}} {
		a.NextActions = actions
		value, err := messageMap(a)
		if err != nil || value["state"] != "DELETING" || value["deletion"].(map[string]any)["state"] != "FAILED" || len(value["nextActions"].([]any)) != len(actions) {
			t.Fatal("failed cleanup changed terminal state or invented delete authority")
		}
	}
	command := &providerCommandStub{account: func() *cp.ProviderAccount { return a }}
	server := &Server{control: &controlplaneclient.Client{Command: command}}
	for _, input := range []struct {
		key, etag string
		version   int64
	}{
		{"unknown-original-key", `"4"`, 4}, {"explicit-new-delete-key", `"8"`, 8},
	} {
		w := httptest.NewRecorder()
		server.DeleteProviderAccount(w, httptest.NewRequest("DELETE", "/", nil), a.Ref, generated.DeleteProviderAccountParams{IfMatch: input.etag, IdempotencyKey: input.key})
		if w.Code != 200 || command.delete.GetMutation().GetIdempotencyKey() != input.key || command.delete.GetMutation().GetExpectedVersion() != input.version {
			t.Fatal("delete recovery altered caller key or exact owner version")
		}
	}
	for _, mutate := range []func(*cp.ProviderAccount){
		func(v *cp.ProviderAccount) { v.Deletion.CompletedAt = v.Deletion.RequestedAt },
		func(v *cp.ProviderAccount) { v.Deletion.State = cp.ProviderAccountDeletionState(99) },
		func(v *cp.ProviderAccount) { v.State = cp.ProviderAccountState_PROVIDER_ACCOUNT_STATE_DELETED },
	} {
		candidate := proto.Clone(a).(*cp.ProviderAccount)
		mutate(candidate)
		if _, err := messageMap(candidate); err == nil {
			t.Fatal("inconsistent failed cleanup accepted")
		}
	}
}

func TestRetryRunForwardsFreshOwnerDenialOnEveryExactReplay(t *testing.T) {
	client := &catalogRPCRecorder{failure: status.Error(codes.PermissionDenied, "private owner detail")}
	server := &Server{control: &controlplaneclient.Client{Command: cp.NewPlatformCommandServiceClient(client)}}
	for range 2 {
		w := httptest.NewRecorder()
		server.CommandRun(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"action":"RETRY"}`)), "run_fixture01", generated.CommandRunParams{IfMatch: `"3"`, IdempotencyKey: "exact-retry-key"})
		request, ok := client.request.(*cp.RetryRunRequest)
		if w.Code != 403 || !ok || request.RunRef != "run_fixture01" || request.Mutation.GetExpectedVersion() != 3 || request.Mutation.GetIdempotencyKey() != "exact-retry-key" || strings.Contains(w.Body.String(), "private owner detail") {
			t.Fatal("retry bypassed fresh owner denial or changed replay identity")
		}
	}
	for _, actions := range [][]cp.NextAction{nil, {cp.NextAction_NEXT_ACTION_OPEN}, {cp.NextAction_NEXT_ACTION_OPEN, cp.NextAction_NEXT_ACTION_RETRY}} {
		value, err := messageMap(&cp.Run{Ref: "run_fixture01", NextActions: actions})
		if err != nil || len(value["nextActions"].([]any)) != len(actions) {
			t.Fatal("run view invented retry authority")
		}
	}
}

func TestProviderVerificationAttemptSurvivesSingleListAndCommandReadback(t *testing.T) {
	a := providerTestAccount()
	a.Verification = &cp.ProviderAccountVerification{Ref: "pver_fixture01", AccountVersion: a.Version, CredentialRevision: 2,
		State:       cp.ProviderAccountVerificationState_PROVIDER_ACCOUNT_VERIFICATION_STATE_PENDING,
		Scope:       cp.ProviderAccountVerificationScope_PROVIDER_ACCOUNT_VERIFICATION_SCOPE_CREDENTIALED_CATALOG_REACHABILITY,
		RequestedAt: timestamppb.New(time.Now().UTC()), SafeReason: "VERIFICATION_PENDING"}
	for _, response := range []proto.Message{a, &cp.GetProviderAccountResponse{Account: a},
		&cp.ListProviderAccountsResponse{Accounts: []*cp.ProviderAccount{a}},
		&cp.VerifyProviderAccountDeviceAuthorizationResponse{Account: a},
		&cp.ReauthorizeProviderAccountDeviceCodeResponse{Account: a}} {
		w := httptest.NewRecorder()
		writeMessage(w, 200, response, "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"safeReason":"VERIFICATION_PENDING"`) || !strings.Contains(w.Body.String(), `"accountVersion":4`) {
			t.Fatal("fresh verification attempt lost in protected readback")
		}
	}
	a.Verification.State = cp.ProviderAccountVerificationState(99)
	if _, err := messageMap(&cp.ListProviderAccountsResponse{Accounts: []*cp.ProviderAccount{a}}); err == nil {
		t.Fatal("unknown nested verification state accepted")
	}
}
