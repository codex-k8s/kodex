package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	authorityv1 "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const warmDiagnosticSentinel = "PRIVATE-JWS-credential-ref-sentinel"

type warmDiagnosticOwner struct {
	controlplanev1.RuntimeWorkServiceClient
	response *controlplanev1.ReconcileWarmRuntimeResponse
	err      error
	calls    int
}

func (owner *warmDiagnosticOwner) ReconcileWarmRuntime(ctx context.Context, request *controlplanev1.ReconcileWarmRuntimeRequest, _ ...grpc.CallOption) (*controlplanev1.ReconcileWarmRuntimeResponse, error) {
	owner.calls++
	if _, ok := ctx.Deadline(); !ok || request.GetWorkloadInstance() != "test-pod" {
		panic("warm request contract changed")
	}
	return owner.response, owner.err
}

func (owner *warmDiagnosticOwner) ReportWarmRuntime(ctx context.Context, request *controlplanev1.ReportWarmRuntimeRequest, _ ...grpc.CallOption) (*controlplanev1.ReportWarmRuntimeResponse, error) {
	owner.calls++
	if _, ok := ctx.Deadline(); !ok || request.GetWorkloadInstance() != "test-pod" || request.GetRuntimeRevision() != "test-revision" || request.GetState() != controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY {
		panic("warm report contract changed")
	}
	return &controlplanev1.ReportWarmRuntimeResponse{}, owner.err
}

func warmDiagnosticRuntime(owner *warmDiagnosticOwner) *runtime {
	return &runtime{control: owner, config: Config{PodUID: "test-pod", RequestTimeout: time.Second}}
}

func assertWarmDiagnostic(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || err.Error() != want || errors.Unwrap(err) != nil {
		t.Fatal("unexpected warm diagnostic or retained upstream cause")
	}
	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).Warn("system assistant warm runtime reconciliation failed", "error", err)
	if strings.Contains(output.String(), warmDiagnosticSentinel) {
		t.Fatal("upstream data escaped in warm log")
	}
}

func TestWarmRPCDiagnosticsDropMessagesAndDetails(t *testing.T) {
	for _, code := range []codes.Code{codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded, codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented, codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated, codes.Code(999)} {
		t.Run(code.String(), func(t *testing.T) {
			detail := &authorityv1.AuthorizationErrorDetail{CorrelationId: warmDiagnosticSentinel}
			detail.ProtoReflect().SetUnknown(append([]byte{0xa2, 0x06, byte(len(warmDiagnosticSentinel))}, []byte(warmDiagnosticSentinel)...))
			st, err := status.New(code, warmDiagnosticSentinel).WithDetails(detail)
			if err != nil {
				t.Fatal("build synthetic status")
			}
			owner := &warmDiagnosticOwner{err: fmt.Errorf("%s: %w", warmDiagnosticSentinel, st.Err())}
			runtime := warmDiagnosticRuntime(owner)
			wantCode := code
			if code > codes.Unauthenticated {
				wantCode = codes.Unknown
			}
			assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmReconcileRPCFailure+": grpc_code="+wantCode.String())
			assertWarmDiagnostic(t, runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""), warmReportRPCFailure+": grpc_code="+wantCode.String())
			if owner.calls != 2 {
				t.Fatal("diagnostics repeated owner effect")
			}
		})
	}
	owner := &warmDiagnosticOwner{err: errors.New(warmDiagnosticSentinel)}
	assertWarmDiagnostic(t, warmDiagnosticRuntime(owner).reconcileWarm(t.Context()), warmReconcileRPCFailure+": grpc_code=Unknown")
}

func TestWarmMissingDesiredRevisionIsDistinctFromRPCFailure(t *testing.T) {
	for _, response := range []*controlplanev1.ReconcileWarmRuntimeResponse{nil, {}} {
		owner := &warmDiagnosticOwner{response: response}
		runtime := warmDiagnosticRuntime(owner)
		assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmDesiredRevisionMissing)
		if owner.calls != 1 {
			t.Fatal("missing revision repeated owner effect")
		}
		if err := runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""); err != nil {
			t.Fatal("successful report changed")
		}
	}
}

type warmDiagnosticAuthority struct {
	authorityv1.AuthorizationIssuerServiceClient
	stage authorityclient.DiagnosticStage
	cause error
}

func (authority warmDiagnosticAuthority) OperationID(string) (string, bool) {
	return "test-operation", true
}
func (authority warmDiagnosticAuthority) AuthorityProof(context.Context, string, string) (string, string, error) {
	if authority.stage != authorityclient.StageContextIssue {
		return "", warmDiagnosticSentinel, authorityclient.NewProofFailure(authority.stage, authority.cause)
	}
	return "synthetic-proof", warmDiagnosticSentinel, nil
}
func (authority warmDiagnosticAuthority) IssueAuthorizationContext(context.Context, *authorityv1.IssueAuthorizationContextRequest, ...grpc.CallOption) (*authorityv1.IssueAuthorizationContextResponse, error) {
	return nil, authority.cause
}

func TestWarmLocalAuthorityDiagnosticPreservesOnlyClosedStages(t *testing.T) {
	for _, stage := range []authorityclient.DiagnosticStage{authorityclient.StageGrantMissing, authorityclient.StageGrantRead, authorityclient.StageProofOperation, authorityclient.StageProofResolve, authorityclient.StageProofResponse, authorityclient.StageContextIssue} {
		t.Run(string(stage), func(t *testing.T) {
			st, err := status.New(codes.PermissionDenied, warmDiagnosticSentinel).WithDetails(&authorityv1.AuthorizationErrorDetail{Reason: authorityv1.AuthorizationErrorReason_AUTHORIZATION_ERROR_REASON_PERMISSION_MISMATCH, Stage: authorityv1.AuthorizationFailureStage_AUTHORIZATION_FAILURE_STAGE_POLICY, CorrelationId: warmDiagnosticSentinel})
			if err != nil {
				t.Fatal("build synthetic authority status")
			}
			authority := warmDiagnosticAuthority{stage: stage, cause: st.Err()}
			err = authorityclient.IssuerUnaryClientInterceptor(authority, authority, authority)(t.Context(), "/test", &controlplanev1.ReconcileWarmRuntimeRequest{}, nil, nil, func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				t.Fatal("downstream effect reached after authority rejection")
				return nil
			})
			var local *authorityclient.LocalAuthorityError
			if !errors.As(err, &local) || local.Diagnostic().Stage != stage {
				t.Fatal("local authority fixture failed")
			}
			owner := &warmDiagnosticOwner{err: fmt.Errorf("%s: %w", warmDiagnosticSentinel, err)}
			runtime := warmDiagnosticRuntime(owner)
			want := ": grpc_code=Unauthenticated [" + local.Diagnostic().String() + "]"
			assertWarmDiagnostic(t, runtime.reconcileWarm(t.Context()), warmReconcileRPCFailure+want)
			assertWarmDiagnostic(t, runtime.reportWarm(t.Context(), "test-revision", controlplanev1.AssistantRuntimeState_ASSISTANT_RUNTIME_STATE_READY, ""), warmReportRPCFailure+want)
			if owner.calls != 2 {
				t.Fatal("diagnostics repeated owner effect")
			}
		})
	}
}
