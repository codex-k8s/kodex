package callback

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCompletionRejectionDiagnosticBoundary(t *testing.T) {
	for _, scenario := range []string{"malformed", "unknown field", "invalid payload", "revision", "attempt", "provider binding", "owner unavailable", "owner permission", "unauthorized", "header mismatch", "success", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			manager, kube, input, _, ticket := providerCredentialRefreshRouteFixture(t)
			var logs bytes.Buffer
			client := &diagnosticCompletionClient{before: func() {
				if logs.Len() != 0 {
					t.Fatal("observation preceded rejecting owner RPC")
				}
			}}
			payload := runtimecontract.RunnerCompletionRequest{RuntimeRevisionDigest: input.RuntimeRevisionDigest, Attempt: input.Attempt, Success: true, ResultSummary: "PRIVATE_PAYLOAD_SENTINEL"}
			wantStatus, wantStage, wantCode, calls := http.StatusBadRequest, "PAYLOAD", "InvalidArgument", 0
			switch scenario {
			case "invalid payload":
				payload.Usage = runtimecontract.TokenUsage{TotalTokens: 1, InputTokens: 2}
			case "revision":
				payload.RuntimeRevisionDigest = strings.Repeat("f", 64)
			case "attempt":
				payload.Attempt++
			case "provider binding":
				payload.Success, payload.SafeErrorCode = false, "PROVIDER_UNAVAILABLE"
				payload.ProviderDiagnostic = completionDiagnostic(input)
				payload.ProviderDiagnostic.InputDigest = strings.Repeat("f", 64)
			case "owner unavailable", "owner permission":
				code := codes.Unavailable
				wantStatus = http.StatusServiceUnavailable
				if scenario == "owner permission" {
					code, wantStatus = codes.PermissionDenied, http.StatusNotFound
				}
				client.err = status.Error(code, "PRIVATE_OWNER_SENTINEL")
				wantStage, wantCode, calls = "OWNER_RPC", code.String(), 1
			case "unauthorized", "header mismatch":
				wantStatus, wantStage = http.StatusNotFound, ""
			case "success", "replay":
				wantStatus, wantStage, calls = http.StatusNoContent, "", 1
				if scenario == "replay" {
					client.err = status.Error(codes.AlreadyExists, "PRIVATE_OWNER_SENTINEL")
				}
			}
			raw, _ := json.Marshal(payload)
			if scenario == "malformed" {
				raw = []byte(`{"private":"PRIVATE_BODY_SENTINEL"`)
			}
			if scenario == "unknown field" {
				raw = append(raw[:len(raw)-1], []byte(`,"private":"PRIVATE_BODY_SENTINEL"}`)...)
			}
			server := &Server{manager: manager, coordinator: NewCoordinator(), config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			request := httptest.NewRequest(http.MethodPost, "/v1/executions/"+input.LeaseRef+"/complete", bytes.NewReader(raw))
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "complete")
			if scenario == "unauthorized" {
				request.Header.Set("Authorization", "Bearer "+strings.Repeat("0", 64))
			}
			if scenario == "header mismatch" {
				request.Header.Set("X-Kodex-Run-Ref", "run_PRIVATE_REQUEST_SENTINEL")
			}
			response := httptest.NewRecorder()
			server.route(response, request)
			if response.Code != wantStatus || len(client.requests) != calls {
				t.Fatalf("status=%d calls=%d", response.Code, len(client.requests))
			}
			if wantStage == "" {
				if logs.Len() != 0 {
					t.Fatal("non-rejection acquired diagnostic")
				}
			} else {
				var record map[string]any
				if json.Unmarshal(logs.Bytes(), &record) != nil {
					t.Fatal("expected exactly one JSON observation")
				}
				if record["msg"] != "runtime completion rejected" || record["completion_stage"] != wantStage || record["grpc_code"] != wantCode {
					t.Fatal("incorrect closed classification")
				}
				for key, value := range map[string]any{"run_ref": input.RunRef, "node_ref": input.NodeRef, "session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": float64(input.Attempt), "runtime_revision_digest": input.RuntimeRevisionDigest, "input_digest": input.InputDigest, "execution_binding_digest": input.ExecutionBindingDigest} {
					if record[key] != value {
						t.Fatalf("trusted binding missing: %s", key)
					}
				}
				wantReason := "PROVENANCE"
				switch scenario {
				case "malformed", "unknown field":
					wantReason = "DECODE"
				case "invalid payload":
					wantReason = "VALIDATE"
				case "provider binding":
					wantReason = "PROVIDER_DIAGNOSTIC"
				case "owner unavailable", "owner permission":
					wantReason = "RPC_REJECTED"
				}
				if record["completion_reason"] != wantReason || len(record) != 14 {
					t.Fatal("unexpected diagnostic attributes")
				}
			}
			for _, secret := range []string{"PRIVATE_PAYLOAD_SENTINEL", "PRIVATE_OWNER_SENTINEL", "PRIVATE_BODY_SENTINEL", "PRIVATE_REQUEST_SENTINEL", ticket, input.LeaseFence} {
				if secret != "" && strings.Contains(logs.String(), secret) {
					t.Fatal("private data disclosed")
				}
			}
			for _, action := range kube.Actions() {
				if wantStage != "" && action.GetVerb() == "delete" {
					t.Fatal("rejected completion cleaned up workload")
				}
			}
		})
	}
}

func TestCompletionRejectionDiagnosticClosedEnums(t *testing.T) {
	input := runtimecontract.RunnerInput{RunRef: "run_fixture", NodeRef: "nod_fixture", SessionRef: "ses_fixture", TurnRef: "trn_fixture", Attempt: 1}
	for _, code := range []codes.Code{codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented, codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated, codes.Code(999999)} {
		var logs bytes.Buffer
		server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
		server.logCompletionRejection(t.Context(), input, completionOwnerRPCStage, completionRPCRejectedReason, code)
		var record map[string]any
		if json.Unmarshal(logs.Bytes(), &record) != nil {
			t.Fatal("missing normalized observation")
		}
		want := code
		if code == codes.OK || code == codes.Code(999999) {
			want = codes.Unknown
		}
		if record[completionCodeKey] != want.String() || strings.Contains(logs.String(), "999999") {
			t.Fatal("unknown code escaped closed set")
		}
	}
	for _, pair := range [][2]string{{"PRIVATE_STAGE_SENTINEL", completionRPCRejectedReason}, {completionOwnerRPCStage, "PRIVATE_REASON_SENTINEL"}, {completionPayloadStage, completionRPCRejectedReason}} {
		var logs bytes.Buffer
		server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
		server.logCompletionRejection(t.Context(), input, pair[0], pair[1], codes.Unknown)
		if logs.Len() != 0 {
			t.Fatal("unknown stage/reason escaped closed set")
		}
	}
	(&Server{}).logCompletionRejection(t.Context(), input, completionPayloadStage, completionDecodeReason, codes.InvalidArgument)
}
